package main

import (
	"fmt"
	"os"

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

func init() {
	//amqp channels
	eventOutCh = make(chan *channel.DataChan, 10)
	eventInCh = make(chan *channel.DataChan, 10)
	closeCh = make(chan bool)

}

func TestSidecar_Main(t *testing.T) {
	//set env variables
	os.Setenv("STORE_PATH", "..")
	os.Setenv("AMQP_PLUGIN", "true")
	os.Setenv("REST_PLUGIN", "true")
	if sPath, ok := os.LookupEnv("STORE_PATH"); ok && sPath != "" {
		storePath = sPath
	}
	wg := &sync.WaitGroup{}
	pl := plugins.Handler{Path: "../plugins"}
	go processOutChannel(wg)
	//plugins := []string{"plugins/amqp_plugin.so", "plugins/rest_api_plugin.so"}
	amqAvailable := true
	pubSubInstance = v1pubsub.GetAPIInstance(storePath)

	//base con configuration we should be able to build this plugin
	if common.GetBoolEnv("AMQP_PLUGIN") {
		err := pl.LoadAMQPPlugin(wg, amqpHost, eventInCh, eventOutCh, closeCh)
		if err != nil {
			amqAvailable = false
			log.Printf("skipping amqp usage, test will be reading dirctly from in channel. reason: %v", err)
		}
	}
	if common.GetBoolEnv("REST_PLUGIN") {
		_, err := pl.LoadRestPlugin(wg, restPort, apiPath, storePath, eventOutCh, closeCh)
		assert.Nil(t, err)

		common.EndPointHealthChk(fmt.Sprintf("http://%s:%d%shealth", "localhost", restPort, apiPath))
	}
	//create publisher
	endpointURL := &types.URI{URL: url.URL{Scheme: "http", Host: "localhost:8080", Path: fmt.Sprintf("%s%s", apiPath, "dummy")}}
	pub, err := pubSubInstance.CreatePublisher(v1pubsub.NewPubSub(endpointURL, "test/test"))
	//if you are using amqp then need to create sender (note rest api by default creates sender)
	if err != nil {
		log.Printf("failed to create publisher transport")
	} else if amqAvailable { // skip creating transport
		v1amqp.CreateSender(eventInCh, pub.GetResource())
		log.Printf("publisher %v\n", pub)
	}

	// CREATE Subscription and listeners
	endpointURL = &types.URI{URL: url.URL{Scheme: "http", Host: "localhost:8080", Path: fmt.Sprintf("%s%s", apiPath, "log")}}
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
			Value:     cneevent.GNSS_ACQUIRING_SYNC,
		},
		},
	}
	data.SetVersion("v1") //nolint:errcheck
	event.SetData(data)

	// post this to API

	//or using methods to post here you don't have urilocation data
	cloudEvent, _ := v1event.CreateCloudEvents(event, pub)
	if amqAvailable {
		v1event.SendNewEventToDataChannel(eventInCh, pub.Resource, cloudEvent)
	} else { // skip amqp and send data directly to out channel
		v1event.SendNewEventToDataChannel(eventOutCh, pub.Resource, cloudEvent)
	}

	log.Printf("waiting for the data ////")

	time.Sleep(2 * time.Second)
	close(closeCh)
}
