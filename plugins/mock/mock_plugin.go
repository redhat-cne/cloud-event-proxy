package main

import (
	"fmt"
	"sync"
	"time"

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
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

var (
	resourceAddress string = "/cluster/node/mock"
	config          *common.SCConfiguration
)

// Start mock plugin to process events,metrics and status, expects rest api available to create publisher and subscriptions
func Start(wg *sync.WaitGroup, configuration *common.SCConfiguration, fn func(e interface{}) error) error { //nolint:deadcode,unused

	config = configuration

	// 1. Create event Publication
	var pub pubsub.PubSub
	var err error
	if pub, err = createPublisher(resourceAddress); err != nil {
		log.Errorf("failed to create a publisher %v", err)
		return err
	}
	log.Printf("Created publisher %v", pub)

	// 2.Create Status Listener : This listener will create events
	// method to execute when ping is received
	onStatusRequestFn := func(e v2.Event, d *channel.DataChan) error {
		log.Infof("got status check call,fire events for publisher %s", pub.Resource)
		re, err := createMockEvent(pub) // create a mock event
		if err != nil {
			log.Errorf("failed sending mock event on status pings %s", err)
		} else {
			_ = common.PublishEventViaAPI(config, re)
		}
		d.Type = channel.STATUS
		return nil
	}
	// create amqp listener
	v1amqp.CreateNewStatusListener(config.EventInCh, fmt.Sprintf("%s/%s", pub.Resource, "status"), onStatusRequestFn, fn)

	//create events periodically
	time.Sleep(5 * time.Second)
	// create periodical events
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range time.Tick(100 * time.Millisecond) {
			// create an event
			if mEvent, err := createMockEvent(pub); err == nil {
				mEvent.Type = string(ptp.PtpStateChange)
				mEvent.Data.Values[0].Value = ptp.LOCKED
				mEvent.Data.Values[1].Value = -200
				if err = common.PublishEventViaAPI(config, mEvent); err != nil {
					log.Errorf("error publishing events %s", err)
				}
			} else {
				log.Errorf("error creating mock event")
			}
		}
	}()

	return nil
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
			Resource:  pub.Resource,
			DataType:  ceEvent.NOTIFICATION,
			ValueType: ceEvent.ENUMERATION,
			Value:     ptp.ACQUIRING_SYNC,
		},
			{
				Resource:  pub.Resource,
				DataType:  ceEvent.METRIC,
				ValueType: ceEvent.DECIMAL,
				Value:     "99.6",
			},
		},
	}
	e, err := common.CreateEvent(pub.ID, string(ptp.PtpStateChange), data)
	return e, err
}
