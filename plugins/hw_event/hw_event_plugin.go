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
	"os"
	"sync"

	"github.com/redhat-cne/cloud-event-proxy/pkg/common"
	hwevent "github.com/redhat-cne/sdk-go/pkg/hwevent"

	"github.com/redhat-cne/sdk-go/pkg/pubsub"
	"github.com/redhat-cne/sdk-go/pkg/types"

	v1amqp "github.com/redhat-cne/sdk-go/v1/amqp"
	v1hwevent "github.com/redhat-cne/sdk-go/v1/hwevent"
	v1pubsub "github.com/redhat-cne/sdk-go/v1/pubsub"
	log "github.com/sirupsen/logrus"
)

var (
	resourceAddress string   = "/hw-event"
	hwSubURL        string   = "https://10.19.109.249/redfish/v1/EventService/Subscriptions/"
	hwEventTypes    []string = []string{"StatusChange", "ResourceUpdated", "ResourceAdded", "ResourceRemoved", "Alert"}

	// used by the webhook handlers
	scConfig *common.SCConfiguration
	pub      pubsub.PubSub
)

// HTTPHeader temporary code for redfish subscription
type HTTPHeader struct {
	ContentType     string `json:"Content-Type" example:"application/json"`
	ContentLanguage string `json:"Content-Language" example:"en-US"`
}

// HwSubPayload temporary code for redfish subscription
type HwSubPayload struct {
	Protocol    string       `json:"Protocol" example:"Redfish"`
	Context     string       `json:"Context"`
	Destination string       `json:"Destination" example:"https://www.hw-event-proxy-host.com/webhook"`
	EventTypes  []string     `json:"EventTypes"`
	HTTPHeaders []HTTPHeader `json:"HttpHeaders"`
}

// Start hw event plugin to process events,metrics and status, expects rest api available to create publisher and subscriptions
func Start(wg *sync.WaitGroup, config *common.SCConfiguration, fn func(e interface{}) error) error { //nolint:deadcode,unused
	webhookURL := getWebhookAddr()
	status, _ := createHwEventSubscription(hwEventTypes, webhookURL)
	if status != http.StatusCreated {
		log.Errorf("hw event subscription creation api at %s, returned status %d", hwSubURL, status)
		// if failed, create a subsciption manually
	}
	scConfig = config

	// create publisher
	var err error

	returnURL := fmt.Sprintf("%s%s", config.BaseURL, "dummy")
	//	pub, err = scConfig.PubSubAPI.CreatePublisher(v1pubsub.NewPubSub(scConfig.BaseURL, resourceAddress))
	pub, err = scConfig.PubSubAPI.CreatePublisher(v1pubsub.NewPubSub(types.ParseURI(returnURL), resourceAddress))

	if err != nil {
		log.Errorf("failed to create a publisher %v", err)
		return err
	}
	log.Printf("Created publisher %v", pub)

	// once the publisher response is received, create a transport sender object to send events.
	v1amqp.CreateSender(scConfig.EventInCh, pub.GetResource())
	log.Printf("Created sender %v", pub.GetResource())

	startWebhook()

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

// createHwEventSubscription temporary code for redfish subscription
func createHwEventSubscription(eventTypes []string, webhookURL string) (int, []byte) {

	header := HTTPHeader{ContentType: "application/json", ContentLanguage: "en-US"}
	data := HwSubPayload{
		Protocol: "Redfish",
		Context:  "any string is valid",
		// must be https
		Destination: webhookURL,
		EventTypes:  eventTypes,
		HTTPHeaders: []HTTPHeader{header},
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

	req, err := http.NewRequest("POST", hwSubURL, body)
	if err != nil {
		log.Errorf("error creating post request %v", err)
		return http.StatusBadRequest, nil
	}
	req.SetBasicAuth("Administrator", "password")
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		log.Errorf("error in post response %v to %s ", err, hwSubURL)
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

func startWebhook() {
	http.HandleFunc("/ack/event", ackEvent)
	http.HandleFunc("/webhook", publishHwEvent)
	go func() {
		err := http.ListenAndServe(fmt.Sprintf(":%d", common.GetIntEnv("HW_EVENT_PORT")), nil)
		if err != nil {
			log.Errorf("error with webhook server %s\n", err.Error())
		}
	}()
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

	redfishEvent := hwevent.RedfishEvent{}
	err = json.Unmarshal(bodyBytes, &redfishEvent)
	if err != nil {
		log.Errorf("failed to unmarshal hw event: %v", err)
		return
	}

	data := v1hwevent.CloudNativeData()
	data.SetVersion("v1") //nolint:errcheck
	data.SetData(&redfishEvent)

	event, _ := common.CreateHwEvent(pub.ID, "HW_EVENT", data)
	_ = common.PublishHwEvent(scConfig, event)
}
