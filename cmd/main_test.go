package main_test

import (
	"fmt"
	"github.com/redhat-cne/cloud-event-proxy/pkg/common"
	"github.com/redhat-cne/sdk-go/pkg/types"
	"os"

	main "github.com/redhat-cne/cloud-event-proxy/cmd"
	"github.com/redhat-cne/cloud-event-proxy/pkg/plugins"
	"github.com/redhat-cne/sdk-go/pkg/channel"
	"github.com/stretchr/testify/assert"
	"log"
	"sync"
	"testing"

	v1pubsub "github.com/redhat-cne/sdk-go/v1/pubsub"
)

var (
	channelBufferSize int = 10
	scConfig          *common.SCConfiguration
	resourceAddress   string = "/test/main"
)

func storeCleanUp() {
	_ = scConfig.PubSubAPI.DeleteAllPublishers()
	_ = scConfig.PubSubAPI.DeleteAllSubscriptions()
}

func TestSidecar_MainWithAMQP(t *testing.T) {
	defer storeCleanUp()
	wg := &sync.WaitGroup{}
	pl := plugins.Handler{Path: "../plugins"}
	// set env variables
	os.Setenv("STORE_PATH", "..")
	var storePath = "."
	if sPath, ok := os.LookupEnv("STORE_PATH"); ok && sPath != "" {
		storePath = sPath
	}
	scConfig = &common.SCConfiguration{
		EventInCh:  make(chan *channel.DataChan, channelBufferSize),
		EventOutCh: make(chan *channel.DataChan, channelBufferSize),
		CloseCh:    make(chan bool),
		APIPort:    0,
		APIPath:    "/api/cloudNotifications/v1/",
		PubSubAPI:  v1pubsub.GetAPIInstance(storePath),
		StorePath:  storePath,
		AMQPHost:   "amqp:localhost:5672",
	}
	log.Printf("Configuration set to %#v", scConfig)

	//start rest service
	server, err := common.StartPubSubService(wg, scConfig)
	assert.Nil(t, err)

	// imitate main process
	go main.ProcessOutChannel()

	log.Printf("loading amqp with host %s", scConfig.AMQPHost)
	_, err = pl.LoadAMQPPlugin(wg, scConfig)
	if err != nil {
		t.Skipf("skipping amqp usage, test will be reading dirctly from in channel. reason: %v", err)
	}

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
	log.Printf("Publisher \n%s:", pub.String())

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
	server.Shutdown()

}

func TestSidecar_MainWithOutAMQP(t *testing.T) {
	defer storeCleanUp()
	wg := &sync.WaitGroup{}
	// set env variables
	os.Setenv("STORE_PATH", "..")
	var storePath = "."
	if sPath, ok := os.LookupEnv("STORE_PATH"); ok && sPath != "" {
		storePath = sPath
	}
	scConfig = &common.SCConfiguration{
		EventInCh:  make(chan *channel.DataChan, channelBufferSize),
		EventOutCh: make(chan *channel.DataChan, channelBufferSize),
		CloseCh:    make(chan bool),
		APIPort:    0,
		APIPath:    "/api/cloudNotifications/v1/",
		PubSubAPI:  v1pubsub.GetAPIInstance(storePath),
		StorePath:  storePath,
		AMQPHost:   "amqp:localhost:5672",
	}
	log.Printf("Configuration set to %#v", scConfig)

	//disable AMQP
	scConfig.PubSubAPI.DisableTransport()

	//start rest service
	server, err := common.StartPubSubService(wg, scConfig)
	assert.Nil(t, err)

	// imitate main process
	go main.ProcessOutChannel()
	go main.ProcessInChannel(wg)

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
	log.Printf("Publisher \n%s:", pub.String())

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
	server.Shutdown()

}
