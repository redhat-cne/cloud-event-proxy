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
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"sync"

	"github.com/redhat-cne/cloud-event-proxy/pkg/common"
	hwevent "github.com/redhat-cne/sdk-go/pkg/hwevent"

	"github.com/redhat-cne/sdk-go/pkg/pubsub"
	"github.com/redhat-cne/sdk-go/pkg/types"

	"github.com/redhat-cne/cloud-event-proxy/plugins/hw_event/pb"
	"google.golang.org/grpc"

	v1amqp "github.com/redhat-cne/sdk-go/v1/amqp"
	v1hwevent "github.com/redhat-cne/sdk-go/v1/hwevent"
	v1pubsub "github.com/redhat-cne/sdk-go/v1/pubsub"
	log "github.com/sirupsen/logrus"
)

var (
	resourceAddress string = "/hw-event"
	// used by the webhook handlers
	scConfig *common.SCConfiguration
	pub      pubsub.PubSub
)

// Start hw event plugin to process events,metrics and status, expects rest api available to create publisher and subscriptions
func Start(wg *sync.WaitGroup, config *common.SCConfiguration, fn func(e interface{}) error) error { //nolint:deadcode,unused
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
	log.Infof("Created publisher %v", pub)

	// once the publisher response is received, create a transport sender object to send events.
	v1amqp.CreateSender(scConfig.EventInCh, pub.GetResource())
	log.Infof("Created sender %v", pub.GetResource())

	startWebhook()

	return nil
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
		log.Errorf("error reading hw event: %v", err)
		return
	}

	redfishEvent := hwevent.RedfishEvent{}
	err = json.Unmarshal(bodyBytes, &redfishEvent)
	if err != nil {
		log.Errorf("failed to unmarshal hw event: %v", err)
		return
	}

	for i, e := range redfishEvent.Events {
		if e.Message == "" {
			parsed, err := parseMessage(e)
			if err == nil {
				redfishEvent.Events[i] = parsed
			} else {
				// ignore error
				log.Debugf("error parsing message: %v", err)
			}

		}
	}

	data := v1hwevent.CloudNativeData()
	data.SetVersion("v1") //nolint:errcheck
	data.SetData(&redfishEvent)

	event, _ := common.CreateHwEvent(pub.ID, "HW_EVENT", data)
	_ = common.PublishHwEventViaAPI(scConfig, event)
}

func parseMessage(m hwevent.EventRecord) (hwevent.EventRecord, error) {
	addr := "localhost:9999"
	conn, err := grpc.Dial(addr, grpc.WithInsecure(), grpc.WithBlock())
	if err != nil {
		return hwevent.EventRecord{}, err
	}
	defer conn.Close()

	client := pb.NewMessageParserClient(conn)
	req := &pb.ParserRequest{
		MessageId:   m.MessageID,
		MessageArgs: m.MessageArgs,
	}

	resp, err := client.Parse(context.Background(), req)
	if err != nil {
		return hwevent.EventRecord{}, err
	}

	m.Message = resp.Message
	m.Severity = resp.Severity
	m.Resolution = resp.Resolution
	return m, nil
}
