//go:build unittests
// +build unittests

package main

import (
	"fmt"
	"os"
	"k8s.io/utils/pointer"
	v2 "github.com/cloudevents/sdk-go/v2"

	"github.com/redhat-cne/cloud-event-proxy/pkg/common"
	"github.com/redhat-cne/sdk-go/pkg/event/ptp"
	"github.com/redhat-cne/sdk-go/pkg/types"
	ceEvent "github.com/redhat-cne/sdk-go/pkg/event"

	"sync"
	"testing"

	"github.com/redhat-cne/sdk-go/pkg/channel"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"

	v1pubsub "github.com/redhat-cne/sdk-go/v1/pubsub"
	subscriberApi "github.com/redhat-cne/sdk-go/v1/subscriber"
)

var (
	resourceAddress string = "/test/main"
)

func storeCleanUp() {
	_ = scConfig.PubSubAPI.DeleteAllPublishers()
	_ = scConfig.PubSubAPI.DeleteAllSubscriptions()
}

func TestSidecar_Main(t *testing.T) {
	apiPort = 8990
	defer storeCleanUp()
	wg := &sync.WaitGroup{}
	var storePath = "."
	if sPath, ok := os.LookupEnv("STORE_PATH"); ok && sPath != "" {
		storePath = sPath
	}
	scConfig = &common.SCConfiguration{
		EventInCh:  make(chan *channel.DataChan, channelBufferSize),
		EventOutCh: make(chan *channel.DataChan, channelBufferSize),
		CloseCh:    make(chan struct{}),
		APIPort:    apiPort,
		APIPath:    "/api/ocloudNotifications/v2/",
		PubSubAPI:  v1pubsub.GetAPIInstance(storePath),
		SubscriberAPI: subscriberApi.GetAPIInstance(storePath),
		StorePath:  storePath,
		TransportHost: &common.TransportHost{
			Type: common.HTTP,
			URL:  "localhost:8089",
			Host: "localhost",
			Port: 8089,
			Err:  nil,
		},
	}
	log.Infof("Configuration set to %#v", scConfig)

	//start rest service
	err := common.StartPubSubService(scConfig)
	assert.Nil(t, err)

	// imitate main process
	wg.Add(1)
	go ProcessOutChannel(wg, scConfig)

	//create publisher
	// this is loopback on server itself. Since current pod does not create any server
	endpointURL := fmt.Sprintf("%s%s", scConfig.BaseURL, "dummy")
	createPub := v1pubsub.NewPubSub(types.ParseURI(endpointURL), resourceAddress)
	pub, err := common.CreatePublisher(scConfig, createPub)
	assert.Nil(t, err)
	assert.NotEmpty(t, pub.ID)
	assert.NotEmpty(t, pub.Resource)
	assert.NotEmpty(t, pub.EndPointURI)
	assert.NotEmpty(t, pub.URILocation)

	onCurrentStateFn := func(e v2.Event, d *channel.DataChan) error {
		if e.Source() != "" {
			log.Infof("setting return address to %s", e.Source())
			d.ReturnAddress = pointer.String(e.Source())
		}
		log.Infof("got status check call, fire events returning to %s", *d.ReturnAddress)
		data := ceEvent.Data{
			Version: ceEvent.APISchemaVersion,
			Values: []ceEvent.DataValue{{
				Resource:  "/mock",
				DataType:  ceEvent.NOTIFICATION,
				ValueType: ceEvent.ENUMERATION,
				Value:     ptp.ACQUIRING_SYNC,
			},
				{
					Resource:  "/mock",
					DataType:  ceEvent.METRIC,
					ValueType: ceEvent.DECIMAL,
					Value:     "99.6",
				},
			},
		}
		re, mErr := common.CreateEvent(pub.ID, pub.Resource, "mock", data)
		if mErr != nil {
			log.Errorf("failed sending mock event on status pings %s", err)
		} else {
			if ceEvent, ceErr := common.GetPublishingCloudEvent(scConfig, re); ceErr == nil {
				d.Data = ceEvent
			}
		}
		return nil
	}
	scConfig.RestAPI.SetOnStatusReceiveOverrideFn(onCurrentStateFn)
	log.Infof("Publisher \n%s:", pub.String())

	//Test subscription
	createSub := v1pubsub.NewPubSub(types.ParseURI(endpointURL), resourceAddress)
	sub, err := common.CreateSubscription(scConfig, createSub)
	assert.Nil(t, err)
	assert.NotEmpty(t, sub.ID)
	assert.NotEmpty(t, sub.Resource)
	assert.NotEmpty(t, sub.EndPointURI)
	assert.NotEmpty(t, sub.URILocation)
	log.Printf("Subscription \n%s:", sub.String())
	close(scConfig.CloseCh)
}
