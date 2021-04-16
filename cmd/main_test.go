package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"github.com/redhat-cne/cloud-event-proxy/pkg/restclient"

	"log"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/redhat-cne/cloud-event-proxy/pkg/common"
	"github.com/redhat-cne/cloud-event-proxy/pkg/plugins"
	"github.com/redhat-cne/sdk-go/pkg/channel"
	cneevent "github.com/redhat-cne/sdk-go/pkg/event"
	"github.com/redhat-cne/sdk-go/pkg/types"
	"github.com/stretchr/testify/assert"

	v1amqp "github.com/redhat-cne/sdk-go/v1/amqp"
	v1event "github.com/redhat-cne/sdk-go/v1/event"
	v1pubsub "github.com/redhat-cne/sdk-go/v1/pubsub"
)

var (
	restHost string = fmt.Sprintf("%s:%d", "localhost", restPort)
	pl       plugins.Handler
)

func init() {
	//amqp channels
	eventOutCh = make(chan *channel.DataChan, 10)
	eventInCh = make(chan *channel.DataChan, 10)
	closeCh = make(chan bool)
	pl = plugins.Handler{Path: "../plugins"}
}
func storeCleanUp() {
	_ = pubSubInstance.DeleteAllPublishers()
	_ = pubSubInstance.DeleteAllSubscriptions()
}

func TestSidecar_Main(t *testing.T) {
	//set env variables
	os.Setenv("STORE_PATH", "..")
	os.Setenv("AMQP_PLUGIN", "true")
	os.Setenv("REST_PLUGIN", "false")
	os.Setenv("PTP_PLUGIN", "false")
	if sPath, ok := os.LookupEnv("STORE_PATH"); ok && sPath != "" {
		storePath = sPath
	}
	wg := &sync.WaitGroup{}
	pl := plugins.Handler{Path: "../plugins"}
	go processOutChannel()
	//plugins := []string{"plugins/amqp_plugin.so", "plugins/rest_api_plugin.so"}
	amqAvailable := true
	baseURL := &types.URI{URL: url.URL{Scheme: "http", Host: fmt.Sprintf("%s%d", "localhost", restPort), Path: "/test/"}}
	pubSubInstance = v1pubsub.GetAPIInstance(storePath, baseURL)

	//base con configuration we should be able to build this plugin
	if common.GetBoolEnv("AMQP_PLUGIN") {
		log.Printf("loading amqp with host %s", amqpHost)
		_, err := pl.LoadAMQPPlugin(wg, amqpHost, eventInCh, eventOutCh, closeCh)
		if err != nil {
			amqAvailable = false
			log.Printf("skipping amqp usage, test will be reading dirctly from in channel. reason: %v", err)
		}
	}
	if common.GetBoolEnv("REST_PLUGIN") {
		_, err := pl.LoadRestPlugin(wg, restPort, apiPath, storePath, eventOutCh, closeCh)
		assert.Nil(t, err)
		err = common.EndPointHealthChk(fmt.Sprintf("http://%s%shealth", restHost, apiPath))
		assert.Nil(t, err)
		if err != nil {
			t.Skipf("failed rest service skipping rest of the test %v", err)
		}
	}
	if common.GetBoolEnv("PTP_PLUGIN") {
		err := pl.LoadPTPPlugin(wg, pubSubInstance, eventInCh, closeCh, nil)
		if err != nil {
			log.Fatalf("error loading ptp plugin %v", err)
		}
	}
	defer storeCleanUp()
	//create publisher
	endpointURL := &types.URI{URL: url.URL{Scheme: "http", Host: restHost, Path: fmt.Sprintf("%s%s", apiPath, "dummy")}}
	pub, err := pubSubInstance.CreatePublisher(v1pubsub.NewPubSub(endpointURL, "test/test"))
	//if you are using amqp then need to create sender (note rest api by default creates sender)
	if err != nil {
		log.Printf("failed to create publisher transport")
	} else if amqAvailable { // skip creating transport
		v1amqp.CreateSender(eventInCh, pub.GetResource())
		log.Printf("publisher %v\n", pub)
	}

	// CREATE Subscription and listeners
	endpointURL = &types.URI{URL: url.URL{Scheme: "http", Host: restHost, Path: fmt.Sprintf("%s%s", apiPath, "log")}}
	sub, err := pubSubInstance.CreateSubscription(v1pubsub.NewPubSub(endpointURL, "test/test"))
	if err != nil {
		log.Printf("failed to create subscription transport")
	} else if amqAvailable { // skip creating transport
		v1amqp.CreateListener(eventInCh, sub.GetResource())
		log.Printf("subscription:%v\n", sub)
	}
	// create an event
	event := v1event.CloudNativeEvent()
	event.SetID(pub.ID)
	event.Type = "ptp_status_type"
	event.SetTime(types.Timestamp{Time: time.Now().UTC()}.Time)
	event.SetDataContentType(cneevent.ApplicationJSON)
	data := cneevent.Data{
		Version: "v1",
		Values: []cneevent.DataValue{{
			Resource:  "/cluster/node/ptp",
			DataType:  cneevent.NOTIFICATION,
			ValueType: cneevent.ENUMERATION,
			Value:     cneevent.ACQUIRING_SYNC,
		},
		},
	}
	data.SetVersion("v1") //nolint:errcheck
	event.SetData(data)

	// post this to API

	//or using methods to post here you don't have uriLocation data
	cloudEvent, _ := v1event.CreateCloudEvents(event, pub)
	if amqAvailable {
		v1event.SendNewEventToDataChannel(eventInCh, pub.Resource, cloudEvent)
	} else { // skip amqp and send data directly to out channel
		v1event.SendNewEventToDataChannel(eventOutCh, pub.Resource, cloudEvent)
	}
	time.Sleep(2 * time.Second)
	close(closeCh)
}

func TestSidecar_MainRestApi(t *testing.T) {
	defer storeCleanUp()
	// set env variables
	os.Setenv("STORE_PATH", "..")
	os.Setenv("AMQP_PLUGIN", "false")
	os.Setenv("REST_PLUGIN", "true")
	if sPath, ok := os.LookupEnv("STORE_PATH"); ok && sPath != "" {
		storePath = sPath
	}
	wg := &sync.WaitGroup{}
	pl := plugins.Handler{Path: "../plugins"}
	go processOutChannel()
	//plugins := []string{"plugins/amqp_plugin.so", "plugins/rest_api_plugin.so"}

	baseURL := &types.URI{URL: url.URL{Scheme: "http", Host: fmt.Sprintf("%s%d", "localhost", restPort), Path: apiPath}}
	pubSubInstance = v1pubsub.GetAPIInstance(storePath, baseURL)

	//base con configuration we should be able to build this plugin
	if common.GetBoolEnv("AMQP_PLUGIN") {
		log.Printf("loading amqp with host %s", amqpHost)
		_, err := pl.LoadAMQPPlugin(wg, amqpHost, eventInCh, eventOutCh, closeCh)
		if err != nil {
			log.Printf("skipping amqp usage, test will be reading dirctly from in channel. reason: %v", err)
		}
	}
	if common.GetBoolEnv("REST_PLUGIN") {
		_, err := pl.LoadRestPlugin(wg, restPort, apiPath, storePath, eventOutCh, closeCh)
		assert.Nil(t, err)
		err = common.EndPointHealthChk(fmt.Sprintf("http://%s%shealth", restHost, apiPath))
		assert.Nil(t, err)
		if err != nil {
			t.Skipf("failed rest service skipping rest of the test %v", err)
		}
	}
	//create publisher
	publisherURL := &types.URI{URL: url.URL{Scheme: "http", Host: restHost, Path: fmt.Sprintf("%s%s", apiPath, "publishers")}}
	// this is loopback on server itself. Since current pod does not create any server
	endpointURL := &types.URI{URL: url.URL{Scheme: "http", Host: restHost, Path: fmt.Sprintf("%s%s", apiPath, "dummy")}}
	pub := v1pubsub.NewPubSub(endpointURL, "test/test2")
	b, err := json.Marshal(pub)
	assert.Nil(t, err)
	rc := restclient.New()
	log.Printf("posting event to %s", publisherURL.String())
	status, b := rc.PostWithReturn(publisherURL.String(), b)
	assert.Equal(t, http.StatusCreated, status)
	assert.NotEmpty(t, b)
	err = json.Unmarshal(b, &pub)
	assert.Nil(t, err)
	assert.NotEmpty(t, pub.ID)
	assert.NotEmpty(t, pub.Resource)
	assert.NotEmpty(t, pub.EndPointURI)
	assert.NotEmpty(t, pub.URILocation)
	log.Printf("Publisher \n%s:", pub.String())

	//Test subscription
	//create publisher
	subscriptionURL := &types.URI{URL: url.URL{Scheme: "http", Host: restHost, Path: fmt.Sprintf("%s%s", apiPath, "subscriptions")}}
	// this is loopback on server itself. Since current pod does not create any server
	endpointURL = &types.URI{URL: url.URL{Scheme: "http", Host: restHost, Path: fmt.Sprintf("%s%s", apiPath, "dummy")}}
	sub := v1pubsub.NewPubSub(endpointURL, "test/test2")
	b, err = json.Marshal(sub)
	assert.Nil(t, err)
	status, b = rc.PostWithReturn(subscriptionURL.String(), b)
	assert.Equal(t, http.StatusCreated, status)
	assert.NotEmpty(t, b)
	err = json.Unmarshal(b, &sub)
	assert.Nil(t, err)
	assert.NotEmpty(t, sub.ID)
	assert.NotEmpty(t, sub.Resource)
	assert.NotEmpty(t, sub.EndPointURI)
	assert.NotEmpty(t, sub.URILocation)
	log.Printf("Subscription \n%s:", sub.String())
}
