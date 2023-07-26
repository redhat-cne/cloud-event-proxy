package main

import (
	"fmt"
	"os"
	"sync"
	"time"

	v1http "github.com/redhat-cne/sdk-go/v1/http"
	"k8s.io/utils/pointer"

	v2 "github.com/cloudevents/sdk-go/v2"
	"github.com/redhat-cne/cloud-event-proxy/pkg/common"
	"github.com/redhat-cne/sdk-go/pkg/channel"
	ceEvent "github.com/redhat-cne/sdk-go/pkg/event"
	"github.com/redhat-cne/sdk-go/pkg/event/ptp"
	"github.com/redhat-cne/sdk-go/pkg/pubsub"
	"github.com/redhat-cne/sdk-go/pkg/types"
	v1amqp "github.com/redhat-cne/sdk-go/v1/amqp"
	v1pubsub "github.com/redhat-cne/sdk-go/v1/pubsub"
	log "github.com/sirupsen/logrus"
)

// Copyright 2020 The Cloud Native Events Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
const (
	eventInterval = 45
)

var (
	resourceAddress      = "/cluster/node/%s/mock"
	config               *common.SCConfiguration
	mockResourceName             = "/mock"
	mockEventType                = "mock"
	mockEventStateLocked         = "LOCKED"
	mockEventValue       float64 = -200
)

// Start mock plugin to process events,metrics and status, expects rest api available to create publisher and subscriptions
func Start(wg *sync.WaitGroup, configuration *common.SCConfiguration, fn func(e interface{}) error) error { //nolint:deadcode,unused
	config = configuration
	nodeName := os.Getenv("NODE_NAME")
	if nodeName == "" {
		log.Error("cannot find NODE_NAME environment variable")
		return fmt.Errorf("cannot find NODE_NAME environment variable %s", nodeName)
	}
	// 1. Create event Publication
	var pub pubsub.PubSub
	var err error
	if pub, err = createPublisher(fmt.Sprintf(resourceAddress, nodeName)); err != nil {
		log.Errorf("failed to create a publisher %v", err)
		return err
	}
	log.Printf("Created publisher %v", pub)

	// 2.Create Status Listener : This listener will create events
	// method to execute when ping is received
	onCurrentStateFn := func(e v2.Event, d *channel.DataChan) error {
		if e.Source() != "" {
			log.Infof("setting return address to %s", e.Source())
			d.ReturnAddress = pointer.String(e.Source())
		}
		log.Infof("got status check call,fire events returning to %s", *d.ReturnAddress)
		re, mErr := createMockEvent(pub) // create a mock event
		if mErr != nil {
			log.Errorf("failed sending mock event on status pings %s", err)
		} else {
			if ceEvent, ceErr := common.GetPublishingCloudEvent(config, re); ceErr == nil {
				d.Data = ceEvent
			}
		}
		return nil
	}
	// create amqp listener
	if config.TransportHost.Type == common.AMQ {
		v1amqp.CreateNewStatusListener(config.EventInCh, fmt.Sprintf("%s/%s", pub.Resource, "status"), onCurrentStateFn, fn)
	} else if httpInstance, ok := config.TransPortInstance.(*v1http.HTTP); ok {
		httpInstance.SetOnStatusReceiveOverrideFn(onCurrentStateFn)
	} else {
		log.Error("could not set receiver for http ")
	}

	// create events periodically
	time.Sleep(5 * time.Second)
	// create periodical events
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range time.Tick(eventInterval * time.Second) {
			// create an event
			sendEvent(pub)
		}
	}()
	return nil
}
func sendEvent(pub pubsub.PubSub) {
	if mEvent, mockEventErr := createMockEvent(pub); mockEventErr == nil {
		mEvent.Type = mockEventType
		mEvent.Data.Values[0].Value = mockEventStateLocked
		mEvent.Data.Values[1].Value = mockEventValue
		if mockEventErr = common.PublishEventViaAPI(config, mEvent); mockEventErr != nil {
			log.Errorf("error publishing events %s", mockEventErr)
		}
	} else {
		log.Errorf("error creating mock event")
	}
}
func createPublisher(address string) (pub pubsub.PubSub, err error) {
	// this is loopback on server itself. Since current pod does not create any server
	returnURL := fmt.Sprintf("%s%s", config.BaseURL, "dummy")
	pubToCreate := v1pubsub.NewPubSub(types.ParseURI(returnURL), address)
	pub, err = common.CreatePublisher(config, pubToCreate)
	if err != nil {
		log.Errorf("failed to create publisher %v", pub)
	}
	return pub, err
}

func createMockEvent(pub pubsub.PubSub) (ceEvent.Event, error) {
	// create an event
	data := ceEvent.Data{
		Version: "v1",
		Values: []ceEvent.DataValue{{
			Resource:  mockResourceName,
			DataType:  ceEvent.NOTIFICATION,
			ValueType: ceEvent.ENUMERATION,
			Value:     ptp.ACQUIRING_SYNC,
		},
			{
				Resource:  mockResourceName,
				DataType:  ceEvent.METRIC,
				ValueType: ceEvent.DECIMAL,
				Value:     "99.6",
			},
		},
	}
	e, err := common.CreateEvent(pub.ID, pub.Resource, mockEventType, data)
	return e, err
}
