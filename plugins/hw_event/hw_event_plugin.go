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
	"os"
	"sync"
	"time"

	"github.com/redhat-cne/cloud-event-proxy/pkg/common"
	cneevent "github.com/redhat-cne/sdk-go/pkg/event"
	"github.com/redhat-cne/sdk-go/pkg/pubsub"
	"github.com/redhat-cne/sdk-go/pkg/types"
	v1amqp "github.com/redhat-cne/sdk-go/v1/amqp"
	v1pubsub "github.com/redhat-cne/sdk-go/v1/pubsub"
	log "github.com/sirupsen/logrus"
)

var (
	resourceAddress string        = "/hw-event"
	eventInterval   time.Duration = time.Second * 5

	// TODO : pass these from flag.StringVar in main.go
	apiPath      string   = "/api/cloudNotifications/v1/"
	apiAddr      string   = "localhost:8080"
	hwSubUrl     string   = "https://10.19.109.249/redfish/v1/EventService/Subscriptions/"
	hwEventTypes []string = []string{"StatusChange", "ResourceUpdated", "ResourceAdded", "ResourceRemoved", "Alert"}
	pub          pubsub.PubSub
)

type HttpHeader struct {
	ContentType     string `json:"Content-Type" example:"application/json"`
	ContentLanguage string `json:"Content-Language" example:"en-US"`
}

type HwSubPayload struct {
	Protocol    string       `json:"Protocol" example:"Redfish"`
	Context     string       `json:"Context"`
	Destination string       `json:"Destination" example:"https://www.hw-event-proxy-host.com/webhook"`
	EventTypes  []string     `json:"EventTypes" example:["StatusChange", "ResourceUpdated", "ResourceAdded", "ResourceRemoved", "Alert"]`
	HttpHeaders []HttpHeader `json:"HttpHeaders"`
}

type RedfishEvent struct {
	OdataContext string `json:"@odata.context"`
	OdataType    string `json:"@odata.type"`
	Events       []struct {
		Eventid        string    `json:"EventId"`
		Eventtimestamp time.Time `json:"EventTimestamp"`
		Eventtype      string    `json:"EventType"`
		Memberid       string    `json:"MemberId"`
		Message        string    `json:"Message"`
		Messageargs    []string  `json:"MessageArgs"`
		Messageid      string    `json:"MessageId"`
		Oem            struct {
			Hpe struct {
				OdataContext string `json:"@odata.context"`
				OdataType    string `json:"@odata.type"`
				Resource     string `json:"Resource"`
			} `json:"Hpe"`
		} `json:"Oem"`
		Originofcondition string `json:"OriginOfCondition"`
		Severity          string `json:"Severity"`
	} `json:"Events"`
	Name string `json:"Name"`
}

// Start hw event plugin to process events,metrics and status, expects rest api available to create publisher and subscriptions
func Start(wg *sync.WaitGroup, scConfig *common.SCConfiguration, fn func(e cneevent.Event) error) error { //nolint:deadcode,unused

	webhookUrl := getWebhookAddr()
	status, _ := createHwEventSubscription(hwEventTypes, webhookUrl)
	if status != http.StatusCreated {
		err := fmt.Errorf("hw event subscription creation api at %s, returned status %d", hwSubUrl, status)
		return err
	}

	//channel for the transport handler subscribed to get and set events
	//eventInCh := make(chan *channel.DataChan, 10)
	pubSubInstance := v1pubsub.GetAPIInstance(".")
	endpointURL := &types.URI{URL: url.URL{Scheme: "http", Host: apiAddr, Path: apiPath}}
	// create publisher
	pub, err := pubSubInstance.CreatePublisher(v1pubsub.NewPubSub(endpointURL, resourceAddress))

	if err != nil {
		log.Errorf("failed to create a publisher %v", err)
		return err
	}
	log.Printf("Created publisher %v", pub)

	// once the publisher response is received, create a transport sender object to send events.
	v1amqp.CreateSender(scConfig.EventInCh, pub.GetResource())
	log.Printf("Created sender %v", pub.GetResource())

	startWebhook(scConfig)

	return nil
}

func getWebhookAddr() string {
	// TODO: use template to pass route name
	// oc get route hw-event-proxy -o jsonpath='{.spec.host}'
	// hw-event-proxy-cloud-native-events.apps.cnfdt15.lab.eng.tlv2.redhat.com

	routeName := "hw-event-proxy"
	myNamespace, _ := os.LookupEnv("MY_POD_NAMESPACE")
	myNode, _ := os.LookupEnv("MY_NODE_NAME")
	myHost := fmt.Sprintf("%s-%s.apps.%s", routeName, myNamespace, myNode)
	return fmt.Sprintf("https://%s/webhook", myHost)
}

// createHwEventSubscription creates a subscription for Redfish HW events
func createHwEventSubscription(eventTypes []string, webhookUrl string) (int, []byte) {

	header := HttpHeader{ContentType: "application/json", ContentLanguage: "en-US"}
	data := HwSubPayload{
		Protocol: "Redfish",
		Context:  "any string is valid",
		// must be https
		Destination: webhookUrl,
		EventTypes:  eventTypes,
		HttpHeaders: []HttpHeader{header},
	}

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

func startWebhook(scConfig *common.SCConfiguration) {
	http.HandleFunc("/ack/event", ackEvent)
	http.HandleFunc("/webhook", publishHwEvent)
	go http.ListenAndServe(fmt.Sprintf(":%d", scConfig.HwEventPort), nil)
}

func ackEvent(w http.ResponseWriter, req *http.Request) {
	defer req.Body.Close()
	bodyBytes, err := ioutil.ReadAll(req.Body)
	if err != nil {
		log.Errorf("error reading acknowledgment %v", err)
	}
	e := string(bodyBytes)
	if e != "" {
		log.Debugf("received ack %s", string(bodyBytes))
	} else {
		w.WriteHeader(http.StatusNoContent)
	}
}

// publishHwEvent gets redfish HW events and converts it to cloud native event and publishes to the hw publisher
func publishHwEvent(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	bodyBytes, err := ioutil.ReadAll(r.Body)
	if err != nil {
		log.Errorf("error reading hw event %v", err)
		return
	}

	data := RedfishEvent{}
	if err = json.Unmarshal(bodyBytes, &data); err != nil {
		log.Errorf("error decoding hw event data %v", err)
		return
	}
	out, _ := json.Marshal(data)
	log.Printf("Received Webhook event:\n%s\n", out)
}
