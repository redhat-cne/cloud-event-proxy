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

	v2 "github.com/cloudevents/sdk-go/v2"
	"github.com/redhat-cne/cloud-event-proxy/pkg/common"
	ptp_socket "github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/socket"
	ceevent "github.com/redhat-cne/sdk-go/pkg/event"
	v1amqp "github.com/redhat-cne/sdk-go/v1/amqp"
	log "github.com/sirupsen/logrus"

	ptp_metrics "github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/metrics"
	"github.com/redhat-cne/sdk-go/pkg/pubsub"
	"github.com/redhat-cne/sdk-go/pkg/types"
	v1pubsub "github.com/redhat-cne/sdk-go/v1/pubsub"
)

var (
	resourceAddress  string = "/cluster/node/ptp"
	config           *common.SCConfiguration
	metricsProcessor *ptp_metrics.Metric
)

// Start ptp plugin to process events,metrics and status, expects rest api available to create publisher and subscriptions
func Start(wg *sync.WaitGroup, configuration *common.SCConfiguration, fn func(e ceevent.Event) error) error { //nolint:deadcode,unused
	// The name of NodePtpDevice CR for this node is equal to the node name
	nodeName := os.Getenv("NODE_NAME")
	metricsProcessor = &ptp_metrics.Metric{Stats: make(map[string]*ptp_metrics.Stats)}
	if nodeName == "" {
		log.Error("cannot find NODE_NAME environment variable")
		return fmt.Errorf("cannot find NODE_NAME environment variable")
	}
	// register metrics type
	ptp_metrics.RegisterMetrics(nodeName)
	go listenToSocket()
	// 1. Create event Publication
	var pub pubsub.PubSub
	var err error
	config = configuration
	if pub, err = createPublisher(resourceAddress); err != nil {
		log.Errorf("failed to create a publisher %v", err)
		return err
	}
	log.Printf("Created publisher %v", pub)
	// 2.Create Status Listener
	onStatusRequestFn := func(e v2.Event) error {
		log.Info("got status check call,fire events for above publisher")
		event, _ := createPTPEvent(pub)
		_ = common.PublishEvent(config, event)
		return nil
	}
	v1amqp.CreateNewStatusListener(config.EventInCh, fmt.Sprintf("%s/%s", pub.Resource, "status"), onStatusRequestFn, fn)
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

func createPTPEvent(pub pubsub.PubSub) (ceevent.Event, error) {
	// create an event
	data := ceevent.Data{
		Version: "v1",
		Values: []ceevent.DataValue{{
			Resource:  "/cluster/node/ptp",
			DataType:  ceevent.NOTIFICATION,
			ValueType: ceevent.ENUMERATION,
			Value:     ceevent.FREERUN,
		},
		},
	}
	e, err := common.CreateEvent(pub.ID, "PTP_EVENT", data)
	return e, err
}

func listenToSocket() {
	log.Info("establishing socket connection for metrics and events")
	l, err := ptp_socket.Listen("/tmp/metrics.sock")
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
		metricsProcessor.ExtractMetrics(msg)
	}
}
