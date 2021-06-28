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

package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"sync"

	"github.com/redhat-cne/sdk-go/pkg/channel"
	"github.com/redhat-cne/sdk-go/pkg/event"

	v2 "github.com/cloudevents/sdk-go/v2"
	"github.com/redhat-cne/cloud-event-proxy/pkg/common"
	ptpSocket "github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/socket"
	v1amqp "github.com/redhat-cne/sdk-go/v1/amqp"
	log "github.com/sirupsen/logrus"

	ptpMetrics "github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/metrics"
	"github.com/redhat-cne/sdk-go/pkg/pubsub"
	"github.com/redhat-cne/sdk-go/pkg/types"
	v1pubs "github.com/redhat-cne/sdk-go/v1/pubsub"
)

var (
	resourceAddress string = "/cluster/node/%s/ptp"
	config          *common.SCConfiguration
	eventProcessor  *ptpMetrics.PTPEventManager
)

// Start ptp plugin to process events,metrics and status, expects rest api available to create publisher and subscriptions
func Start(wg *sync.WaitGroup, configuration *common.SCConfiguration, fn func(e interface{}) error) error { //nolint:deadcode,unused
	// The name of NodePtpDevice CR for this node is equal to the node name
	nodeName := os.Getenv("NODE_NAME")
	if nodeName == "" {
		log.Error("cannot find NODE_NAME environment variable")
		return fmt.Errorf("cannot find NODE_NAME environment variable %s", nodeName)
	}
	config = configuration
	// register metrics type
	ptpMetrics.RegisterMetrics(nodeName)

	// 1. Create event Publication
	var pub pubsub.PubSub
	var err error
	if pub, err = createPublisher(fmt.Sprintf(resourceAddress, nodeName)); err != nil {
		log.Errorf("failed to create a publisher %v", err)
		return err
	}
	log.Printf("Created publisher %v", pub)
	eventProcessor = ptpMetrics.NewPTPEventManager(pub.ID, nodeName, config)

	wg.Add(1)
	go listenToSocket(wg)

	// 2.Create Status Listener
	// method to be called when ping received
	onReceiveOverrideFn := func(e v2.Event, d *channel.DataChan) error {
		log.Info("got status check call,fire events for above publisher")
		if len(eventProcessor.Stats) == 0 {
			eventProcessor.PublishEvent(event.FREERUN, 0, "ptp-not-set", "PTP_STATUS")
		} else {
			for i, s := range eventProcessor.Stats {
				eventProcessor.PublishEvent(s.ClockState(), s.Offset(), i, "PTP_STATUS")
			}
		}
		d.Type = channel.STATUS
		return nil
	}
	v1amqp.CreateNewStatusListener(config.EventInCh, fmt.Sprintf("%s/%s", pub.Resource, "status"), onReceiveOverrideFn, fn)
	return nil
}
func createPublisher(address string) (pub pubsub.PubSub, err error) {
	// this is loopback on server itself. Since current pod does not create any server
	returnURL := fmt.Sprintf("%s%s", config.BaseURL, "dummy")
	pubToCreate := v1pubs.NewPubSub(types.ParseURI(returnURL), address)
	pub, err = common.CreatePublisher(config, pubToCreate)
	if err != nil {
		log.Errorf("failed to create publisher %v", pub)
	}
	return pub, err
}

func listenToSocket(wg *sync.WaitGroup) {
	log.Info("establishing socket connection for metrics and events")
	defer wg.Done()
	l, err := ptpSocket.Listen("/tmp/metrics.sock")
	if err != nil {
		log.Errorf("error setting up socket %s", err)
		return
	}
	log.Info("connection established successfully")
	for {
		fd, err := l.Accept()
		if err != nil {
			log.Errorf("accept error: %s", err)
		} else {
			go processMessages(fd)
		}
	}
}

func processMessages(c net.Conn) {
	scanner := bufio.NewScanner(c)
	for {
		ok := scanner.Scan()
		if !ok {
			log.Error("error reading socket input")
			break
		}
		msg := scanner.Text()
		eventProcessor.ExtractMetrics(msg)
	}
}
