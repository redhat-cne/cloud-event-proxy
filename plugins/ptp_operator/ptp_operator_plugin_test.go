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

package main

import (
	"fmt"
	"log"
	"os"
	"path"
	"sync"
	"testing"

	"github.com/redhat-cne/cloud-event-proxy/pkg/common"
	"github.com/redhat-cne/cloud-event-proxy/pkg/plugins"
	ptpTypes "github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/types"
	restapi "github.com/redhat-cne/rest-api"
	"github.com/redhat-cne/sdk-go/pkg/channel"
	ptpEvent "github.com/redhat-cne/sdk-go/pkg/event/ptp"
	"github.com/redhat-cne/sdk-go/pkg/types"
	v1event "github.com/redhat-cne/sdk-go/v1/event"
	"github.com/stretchr/testify/assert"

	v1pubsub "github.com/redhat-cne/sdk-go/v1/pubsub"
)

var (
	wg                sync.WaitGroup
	server            *restapi.Server
	scConfig          *common.SCConfiguration
	channelBufferSize int = 10
	storePath             = "../../.."
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
		APIVersion: "1.0",
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
	common.StartPubSubService(scConfig)
	pubsubTypes = InitPubSubTypes()
	cleanUP()
	os.Exit(m.Run())
}
func cleanUP() {
	_ = scConfig.PubSubAPI.DeleteAllPublishers()
	_ = scConfig.PubSubAPI.DeleteAllSubscriptions()
}

// Test_StartWithHTTP ...
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
		sub := v1pubsub.NewPubSub(types.ParseURI(endpointURL), path.Join(resourcePrefix, "test_node", string(pTypes.Resource)), scConfig.APIVersion)
		sub, _ = common.CreateSubscription(scConfig, sub)
		assert.NotEmpty(t, sub.ID)
		assert.NotEmpty(t, sub.URILocation)
		pTypes.PubID = sub.ID
		pTypes.Pub = &sub
	}
	log.Printf("created subscriptions")

	// start ptp plugin
	//err = Start(&wg, scConfig, nil)
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
	assert.Equal(t, 7, len(pubs))
	subs := scConfig.PubSubAPI.GetSubscriptions()
	assert.Equal(t, 7, len(subs))
}

// ProcessInChannel will be  called if Transport is disabled
func ProcessInChannel() {
	for { //nolint:gosimple
		select {
		case d := <-scConfig.EventInCh:
			if d.Type == channel.SUBSCRIBER {
				log.Printf("transport disabled,no action taken: request to create listener address %s was called,but transport is not enabled", d.Address)
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
