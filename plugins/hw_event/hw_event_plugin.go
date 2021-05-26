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
	"fmt"
	"time"

	"github.com/redhat-cne/cloud-event-proxy/pkg/common"
	restapi "github.com/redhat-cne/rest-api"
	ceevent "github.com/redhat-cne/sdk-go/pkg/event"
	"github.com/redhat-cne/sdk-go/pkg/types"

	"sync"

	log "github.com/sirupsen/logrus"

	"github.com/redhat-cne/sdk-go/pkg/pubsub"
	v1pubsub "github.com/redhat-cne/sdk-go/v1/pubsub"
)

var (
	resourceAddress string        = "/cluster/node/hw"
	eventInterval   time.Duration = time.Second * 5
	config          *common.SCConfiguration
)

// Start hw event plugin to process events,metrics and status, expects rest api available to create publisher and subscriptions
func Start(wg *sync.WaitGroup, configuration *common.SCConfiguration, fn func(e ceevent.Event) error) error { //nolint:deadcode,unused
	// 1. Create event Publication
	var pub pubsub.PubSub
	var err error
	config = configuration
	if pub, err = createPublisher(resourceAddress); err != nil {
		log.Errorf("failed to create a publisher %v", err)
		return err
	}
	log.Printf("Created publisher %v", pub)
	// 2.Create Webhook
	_, err = StartWebhook(pub)
	if err != nil {
		log.Fatal("HW event webhook failed to start.")
	}
	return nil
}

// StartWebhook starts rest api service to provide webhook for incoming hw events
// input: pub - created publisher to forward CNE hw events to
func StartWebhook(pub pubsub.PubSub) (*restapi.Webhook, error) {
	// init
	scConfig := &common.SCConfiguration{
		EventInCh:        nil,
		EventOutCh:       nil,
		CloseCh:          make(chan struct{}),
		APIPort:          8990,
		APIPath:          "/api/hw/",
		PubSubAPI:        v1pubsub.GetAPIInstance("../.."),
		StorePath:        "../..",
		AMQPHost:         "amqp:localhost:5672",
		BaseURL:          nil,
		WebhookTargetPub: &pub,
	}

	server := restapi.InitWebhook(scConfig.APIPort, scConfig.APIPath, scConfig.WebhookTargetPub)
	server.Start()
	err := server.EndPointHealthChk()
	if err == nil {
		scConfig.BaseURL = server.GetHostPath()
		scConfig.APIPort = server.Port()
	}
	return server, err
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
