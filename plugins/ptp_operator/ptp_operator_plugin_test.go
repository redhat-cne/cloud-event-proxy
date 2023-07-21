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

//go:build unittests
// +build unittests

package main_test

import (
	"fmt"
	"log"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/redhat-cne/cloud-event-proxy/pkg/common"
	"github.com/redhat-cne/cloud-event-proxy/pkg/plugins"
	ptpTypes "github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/types"
	restapi "github.com/redhat-cne/rest-api"
	"github.com/redhat-cne/sdk-go/pkg/channel"
	ptpEvent "github.com/redhat-cne/sdk-go/pkg/event/ptp"
	"github.com/redhat-cne/sdk-go/pkg/types"
	v1amqp "github.com/redhat-cne/sdk-go/v1/amqp"
	v1event "github.com/redhat-cne/sdk-go/v1/event"
	"github.com/stretchr/testify/assert"

	ptpPlugin "github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator"
	v1pubsub "github.com/redhat-cne/sdk-go/v1/pubsub"
)

var (
	wg                sync.WaitGroup
	server            *restapi.Server
	scConfig          *common.SCConfiguration
	channelBufferSize int = 10
	storePath             = "../../.."
	resourcePrefix        = "/cluster/node/%s%s"
	apiPort           int = 8990
	c                 chan os.Signal
	pubsubTypes       map[ptpEvent.EventType]*ptpTypes.EventPublisherType
)

func TestMain(m *testing.M) {
	scConfig = &common.SCConfiguration{
		EventInCh:  make(chan *channel.DataChan, channelBufferSize),
		EventOutCh: make(chan *channel.DataChan, channelBufferSize),
		CloseCh:    make(chan struct{}),
		APIPort:    apiPort,
		APIPath:    "/api/test-cloud/",
		PubSubAPI:  v1pubsub.GetAPIInstance(storePath),
		StorePath:  storePath,
		TransportHost: &common.TransportHost{
			Type: common.HTTP,
			URL:  "localhost:8089",
			Host: "localhost",
			Port: 8089,
			Err:  nil,
		},
		BaseURL: nil,
	}

	c = make(chan os.Signal)
	server, _ = common.StartPubSubService(scConfig)
	scConfig.APIPort = server.Port()
	scConfig.BaseURL = server.GetHostPath()
	pubsubTypes = ptpPlugin.InitPubSubTypes()
	cleanUP()
	os.Exit(m.Run())
}
func cleanUP() {
	_ = scConfig.PubSubAPI.DeleteAllPublishers()
	_ = scConfig.PubSubAPI.DeleteAllSubscriptions()
}

// Test_StartWithAMQP this is integration test skips if QDR is not connected
func Test_StartWithAMQP(t *testing.T) {
	os.Setenv("NODE_NAME", "test_node")
	defer cleanUP()
	scConfig.CloseCh = make(chan struct{})
	scConfig.PubSubAPI.EnableTransport()
	scConfig.TransportHost = &common.TransportHost{
		Type:   common.AMQ,
		URL:    "amqp:localhost:5672",
		Host:   "",
		Port:   0,
		Scheme: "",
		Err:    nil,
	}
	scConfig.TransportHost.ParseTransportHost()
	amqInitTimeout := 1 * time.Second
	log.Printf("loading amqp with host %s, amqInitTimeout set to %v", scConfig.TransportHost.URL, amqInitTimeout)
	amqpInstance, err := v1amqp.GetAMQPInstance(scConfig.TransportHost.URL, scConfig.EventInCh, scConfig.EventOutCh, scConfig.CloseCh, amqInitTimeout)
	if err != nil {
		t.Skipf("ampq.Dial(%#v): %v", amqpInstance, err)
	}
	wg.Add(1)
	amqpInstance.Start(&wg)
	// build your client
	// SUBSCRIPTION: create a subscription to consume events
	endpointURL := fmt.Sprintf("%s%s", scConfig.BaseURL, "dummy")
	for _, pTypes := range pubsubTypes {
		sub := v1pubsub.NewPubSub(types.ParseURI(endpointURL), fmt.Sprintf(resourcePrefix, "test_node", string(pTypes.Resource)))
		sub, _ = common.CreateSubscription(scConfig, sub)
		assert.NotEmpty(t, sub.ID)
		assert.NotEmpty(t, sub.URILocation)
		//create a status ping sender object
		v1amqp.CreateSender(scConfig.EventInCh, fmt.Sprintf("%s/%s", sub.Resource, "status"))
		pTypes.PubID = sub.ID
		pTypes.Pub = &sub
	}

	// start ptp plugin
	err = ptpPlugin.Start(&wg, scConfig, nil)
	assert.Nil(t, err)

	//status sender object, this wast you can send data to that channel
	for _, pTypes := range pubsubTypes {
		e := v1event.CloudNativeEvent()
		ce, _ := v1event.CreateCloudEvents(e, *pTypes.Pub)
		ce.SetSource(pTypes.Pub.Resource)
		v1event.SendNewEventToDataChannel(scConfig.EventInCh, fmt.Sprintf("%s/%s", pTypes.Pub.Resource, "status"), ce)
	}

	statusEvent := <-scConfig.EventOutCh // status updated
	assert.Equal(t, channel.SUCCESS, statusEvent.Status)
	log.Printf("got events from channel statusEvent 1%#v\n", statusEvent)
	log.Printf("Closing the channels")
	close(scConfig.CloseCh) // close the channel
	pubs := scConfig.PubSubAPI.GetPublishers()
	assert.Equal(t, 4, len(pubs))
}

func Test_StartWithOutAMQP(t *testing.T) {
	os.Setenv("NODE_NAME", "test_node")
	defer cleanUP()
	scConfig.CloseCh = make(chan struct{})
	scConfig.PubSubAPI.DisableTransport()
	scConfig.TransportHost = &common.TransportHost{
		Type:   common.AMQ,
		URL:    "amqp://nohup",
		Host:   "",
		Port:   0,
		Scheme: "",
		Err:    nil,
	}
	scConfig.TransportHost.ParseTransportHost()
	log.Printf("loading amqp with host %s", scConfig.TransportHost.Host)
	go ProcessInChannel()

	// build your client
	//CLIENT SUBSCRIPTION: create a subscription to consume events
	endpointURL := fmt.Sprintf("%s%s", scConfig.BaseURL, "dummy")
	for _, pTypes := range pubsubTypes {
		sub := v1pubsub.NewPubSub(types.ParseURI(endpointURL), fmt.Sprintf(resourcePrefix, "test_node", string(pTypes.Resource)))
		sub, _ = common.CreateSubscription(scConfig, sub)
		assert.NotEmpty(t, sub.ID)
		assert.NotEmpty(t, sub.URILocation)
		//create a status ping sender object
		v1amqp.CreateSender(scConfig.EventInCh, fmt.Sprintf("%s/%s", sub.Resource, "status"))
		pTypes.PubID = sub.ID
		pTypes.Pub = &sub
	}

	// start ptp plugin
	err := ptpPlugin.Start(&wg, scConfig, nil)
	assert.Nil(t, err)
	time.Sleep(2 * time.Second)

	//status sender object, client requesting for an event
	for _, pTypes := range pubsubTypes {
		e := v1event.CloudNativeEvent()
		ce, _ := v1event.CreateCloudEvents(e, *pTypes.Pub)
		ce.SetSource(pTypes.Pub.Resource)
		log.Printf("sending event to data channel %v", ce)
		v1event.SendNewEventToDataChannel(scConfig.EventInCh, fmt.Sprintf("%s/%s", pTypes.Pub.Resource, "status"), ce)
	}

	EventData := <-scConfig.EventOutCh // status updated
	assert.Equal(t, channel.EVENT, EventData.Type)
	log.Printf("got events from channel publisherData %v\n", EventData)

	log.Printf("Closing the channels")
	close(scConfig.CloseCh) // close the channel
	pubs := scConfig.PubSubAPI.GetPublishers()
	assert.Equal(t, 4, len(pubs))
	subs := scConfig.PubSubAPI.GetSubscriptions()
	assert.Equal(t, 4, len(subs))

}

// Test_StartWithAMQP this is integration test skips if QDR is not connected
func Test_StartWithHTTP(t *testing.T) {
	os.Setenv("NODE_NAME", "test_node")
	scConfig.TransportHost = &common.TransportHost{
		Type:   0,
		URL:    "http://localhost:9096",
		Host:   "",
		Port:   0,
		Scheme: "",
		Err:    nil,
	}
	scConfig.TransportHost.ParseTransportHost()
	pl := plugins.Handler{Path: "../../plugins"}

	defer cleanUP()
	scConfig.CloseCh = make(chan struct{})
	scConfig.PubSubAPI.EnableTransport()
	log.Printf("loading http with host %s", scConfig.TransportHost.Host)
	wg := sync.WaitGroup{}
	httpTransportInstance, err := pl.LoadHTTPPlugin(&wg, scConfig, nil, nil)
	if err != nil {
		t.Skipf("http.Dial(%#v): %v", httpTransportInstance, err)
	}

	// build your client
	//CLIENT SUBSCRIPTION: create a subscription to consume events
	endpointURL := fmt.Sprintf("%s%s", scConfig.BaseURL, "dummy")
	for _, pTypes := range pubsubTypes {
		sub := v1pubsub.NewPubSub(types.ParseURI(endpointURL), fmt.Sprintf(resourcePrefix, "test_node", string(pTypes.Resource)))
		sub, _ = common.CreateSubscription(scConfig, sub)
		assert.NotEmpty(t, sub.ID)
		assert.NotEmpty(t, sub.URILocation)
		pTypes.PubID = sub.ID
		pTypes.Pub = &sub
	}
	log.Printf("created subscriptions")

	// start ptp plugin
	//err = ptpPlugin.Start(&wg, scConfig, nil)
	err = pl.LoadPTPPlugin(&wg, scConfig, nil)
	assert.Nil(t, err)
	log.Printf("started ptpPlugin")
	for _, pTypes := range pubsubTypes {
		e := v1event.CloudNativeEvent()
		ce, _ := v1event.CreateCloudEvents(e, *pTypes.Pub)
		ce.SetSource(pTypes.Pub.Resource)
		v1event.SendNewEventToDataChannel(scConfig.EventInCh, fmt.Sprintf("%s", pTypes.Pub.Resource), ce)
	}
	log.Printf("waiting for Event Chan")
	//EventData := <-scConfig.EventOutCh // status updated
	//assert.Equal(t, channel.EVENT, EventData.Type)

	close(scConfig.CloseCh) // close the channel
	pubs := scConfig.PubSubAPI.GetPublishers()
	assert.Equal(t, 4, len(pubs))
	subs := scConfig.PubSubAPI.GetSubscriptions()
	assert.Equal(t, 4, len(subs))
}

// ProcessInChannel will be  called if Transport is disabled
func ProcessInChannel() {
	for { //nolint:gosimple
		select {
		case d := <-scConfig.EventInCh:
			if d.Type == channel.SUBSCRIBER {
				log.Printf("amqp disabled,no action taken: request to create listener address %s was called,but transport is not enabled", d.Address)
			} else if d.Type == channel.PUBLISHER {
				log.Printf("no action taken: request to create sender for address %s was called,but transport is not enabled", d.Address)
			} else if d.Type == channel.EVENT && d.Status == channel.NEW {
				out := channel.DataChan{
					Address:        d.Address,
					Data:           d.Data,
					Status:         channel.SUCCESS,
					Type:           channel.EVENT,
					ProcessEventFn: d.ProcessEventFn,
				}
				if d.OnReceiveOverrideFn != nil {
					if err := d.OnReceiveOverrideFn(*d.Data, &out); err != nil {
						out.Status = channel.FAILED
					} else {
						out.Status = channel.SUCCESS
					}
				}
				scConfig.EventOutCh <- &out
			}
		case <-scConfig.CloseCh:
			return
		}
	}
}
