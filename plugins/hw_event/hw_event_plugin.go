// Copyright 2021 The Cloud Native Events Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/redhat-cne/cloud-event-proxy/pkg/common"
	"github.com/redhat-cne/cloud-event-proxy/pkg/restclient"
	cneevent "github.com/redhat-cne/sdk-go/pkg/event"
	"github.com/redhat-cne/sdk-go/pkg/pubsub"
	"github.com/redhat-cne/sdk-go/pkg/types"
	v1amqp "github.com/redhat-cne/sdk-go/v1/amqp"
	v1event "github.com/redhat-cne/sdk-go/v1/event"
	v1pubsub "github.com/redhat-cne/sdk-go/v1/pubsub"
	log "github.com/sirupsen/logrus"
)

var (
	resourceAddress string        = "/hw-event/cpu"
	eventInterval   time.Duration = time.Second * 5
	apiPath         string        = "/api/cloudNotifications/v1/"
	apiAddr         string        = "localhost:8080"
	localAPIAddr    string        = "localhost:9088"
	localAPIPath    string        = "ack/event"
	hwSubUrl        string        = "https://10.19.109.249/redfish/v1/EventService/Subscriptions/"
	hwEventTypes    []string      = []string{"StatusChange", "ResourceUpdated", "ResourceAdded", "ResourceRemoved", "Alert"}
)

type HttpHeader struct {
	ContentType     string `json:"Content-Type" example:"application/json"`
	ContentLanguage string `json:"Content-Language" example:"en-US"`
}

type HwSubPayload struct {
	Protocol    string       `json:"Protocol" example:"Redfish"`
	Context     string       `json:"Context"`
	Destination string       `json:"Destination" example:"http://localhost:8080/api/cloudNotifications/v1/create/event"`
	EventTypes  []string     `json:"EventTypes" example:["StatusChange", "ResourceUpdated", "ResourceAdded", "ResourceRemoved", "Alert"]`
	HttpHeaders []HttpHeader `json:"HttpHeaders"`
}

// Start hw event plugin to process events,metrics and status, expects rest api available to create publisher and subscriptions
func Start(wg *sync.WaitGroup, scConfig *common.SCConfiguration, fn func(e cneevent.Event) error) error { //nolint:deadcode,unused

	status, _ := createHwEventSubscription(hwEventTypes)
	if status != http.StatusCreated {
		err := fmt.Errorf("hw event subscription creation api at %s, returned status %d", hwSubUrl, status)
		return err
	}

	//channel for the transport handler subscribed to get and set events
	//eventInCh := make(chan *channel.DataChan, 10)
	pubSubInstance := v1pubsub.GetAPIInstance(".")
	endpointURL := &types.URI{URL: url.URL{Scheme: "http", Host: localAPIAddr, Path: localAPIPath}}
	// create publisher
	pub, err := pubSubInstance.CreatePublisher(v1pubsub.NewPubSub(endpointURL, resourceAddress))

	if err != nil {
		log.Errorf("failed to create a publisher %v", err)
		return err
	}
	log.Printf("Created publisher %v", pub)

	// once the publisher response is received, create a transport sender object to send events.
	v1amqp.CreateSender(scConfig.EventInCh, pub.GetResource())

	// serve ack/event event response
	go server()

	log.Printf("Created sender %v", pub.GetResource())
	go createTestEvents(pub)
	return nil
}

// createHwEventSubscription creates a subscription for Redfish HW events
func createHwEventSubscription(eventTypes []string) (int, []byte) {

	header := HttpHeader{ContentType: "application/json", ContentLanguage: "en-US"}
	data := HwSubPayload{
		Protocol: "Redfish",
		Context:  "any string is valid",
		// must be https
		Destination: "https://localhost:8080/api/cloudNotifications/v1/create/event",
		EventTypes:  eventTypes,
		HttpHeaders: []HttpHeader{header},
	}

	// TODO: This is insecure; use only in dev environments.
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: tr}

	payloadBytes, err := json.Marshal(data)
	log.Debugf("hw event sub request json data %s", payloadBytes)

	if err != nil {
		log.Errorf("error encoding data %v", err)
		return http.StatusBadRequest, nil
	}
	body := bytes.NewReader(payloadBytes)

	req, err := http.NewRequest("POST", hwSubUrl, body)
	if err != nil {
		log.Errorf("error creating post request %v", err)
		return http.StatusBadRequest, nil
	}
	req.SetBasicAuth("Administrator", "password")
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		log.Errorf("error in post response %v to %s ", err, hwSubUrl)
		return http.StatusBadRequest, nil
	}
	if resp.Body != nil {
		defer resp.Body.Close()
	}

	respbody, readErr := ioutil.ReadAll(resp.Body)
	if readErr != nil {
		log.Errorf("error reading response body %v", readErr)
		return http.StatusBadRequest, nil
	}
	log.Debugf("hw event sub response, status %v body %s", resp.StatusCode, respbody)
	return resp.StatusCode, respbody

}

// createTestEvents create hw events periodically
func createTestEvents(pub pubsub.PubSub) {
	for range time.Tick(eventInterval) {
		// create an event
		event := v1event.CloudNativeEvent()
		event.SetID(pub.ID)
		event.Type = "hw_event_type"
		event.SetTime(types.Timestamp{Time: time.Now().UTC()}.Time)
		event.SetDataContentType(cneevent.ApplicationJSON)
		data := cneevent.Data{
			Version: "v1",
			Values: []cneevent.DataValue{{
				Resource:  pub.Resource,
				DataType:  cneevent.NOTIFICATION,
				ValueType: cneevent.ENUMERATION,
				Value:     cneevent.ACQUIRING_SYNC,
			},
			},
		}
		data.SetVersion("v1") //nolint:errcheck
		event.SetData(data)
		publishEvent(event)
	}
}

func publishEvent(e cneevent.Event) {
	//create publisher
	url := &types.URI{URL: url.URL{Scheme: "http",
		Host: apiAddr,
		Path: fmt.Sprintf("%s%s", apiPath, "create/event")}}
	rc := restclient.New()
	err := rc.PostEvent(url, e)
	if err != nil {
		log.Errorf("error publishing events %v to url %s", err, url.String())
	} else {
		log.Debugf("published event %s", e.ID)
	}
}

func server() {
	http.HandleFunc("/ack/event", ackEvent)
	http.ListenAndServe(localAPIAddr, nil)
}

func ackEvent(w http.ResponseWriter, req *http.Request) {
	defer req.Body.Close()
	bodyBytes, err := ioutil.ReadAll(req.Body)
	if err != nil {
		log.Errorf("error reading acknowledgment  %v", err)
	}
	e := string(bodyBytes)
	if e != "" {
		log.Debugf("received ack %s", string(bodyBytes))
	} else {
		w.WriteHeader(http.StatusNoContent)
	}
}
