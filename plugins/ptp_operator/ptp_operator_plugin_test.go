package main_test

import (
	"fmt"
	"log"
	"net/url"
	"os"
	"testing"

	"github.com/redhat-cne/cloud-event-proxy/pkg/common"
	restapi "github.com/redhat-cne/rest-api"
	"github.com/redhat-cne/sdk-go/pkg/channel"
	"github.com/redhat-cne/sdk-go/pkg/types"
	v1amqp "github.com/redhat-cne/sdk-go/v1/amqp"
	v1event "github.com/redhat-cne/sdk-go/v1/event"
	"github.com/stretchr/testify/assert"

	"sync"

	ptpPlugin "github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator"
	v1pubsub "github.com/redhat-cne/sdk-go/v1/pubsub"
)

var (
	pubSubAPI  *v1pubsub.API
	eventInCh  chan *channel.DataChan
	eventOutCh chan *channel.DataChan
	closeCh    chan bool
	wg         sync.WaitGroup
	apiPath    string = "/api/test-cloud/"
	apiPort    int    = 8081
	storePath  string = "../.."
	amqpHost   string = "amqp://localhost:5672"
	server     *restapi.Server
	baseURL    string
)

func TestMain(m *testing.M) {
	pubSubAPI = v1pubsub.GetAPIInstance(storePath, nil)
	pubSubAPI.DisableTransport()
	eventInCh = make(chan *channel.DataChan, 10)
	eventOutCh = make(chan *channel.DataChan, 10)
	closeCh = make(chan bool, 1)
	pubSubAPI.DisableTransport()
	server = restapi.InitServer(apiPort, apiPath, storePath, eventInCh, closeCh)
	wg.Add(1)
	go server.Start(&wg)
	err := common.EndPointHealthChk(fmt.Sprintf("http://localhost:%d%shealth", apiPort, apiPath))
	if err != nil {
		log.Fatalf("failed rest service skipping rest of the test %v", err)
	}
	cleanUP()
	baseURL = pubSubAPI.GetBaseURI().String()
	os.Exit(m.Run())
}
func cleanUP() {
	_ = pubSubAPI.DeleteAllPublishers()
	_ = pubSubAPI.DeleteAllSubscriptions()
}

func Test_StartWithOutRestAPI(t *testing.T) {
	defer cleanUP()
	pubSubAPI.SetBaseURI(nil) // disable rest call
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = ptpPlugin.Start(&wg, pubSubAPI, eventInCh, closeCh, nil)
	}()
	log.Printf("waiting to receive intital event in out channel")
	event1 := <-eventInCh // initial event
	assert.Equal(t, channel.NEW, event1.Status)
	event2 := <-eventInCh // after 5 secs
	assert.Equal(t, channel.NEW, event2.Status)
	closeCh <- true // close the channel
	pubs := pubSubAPI.GetPublishers()
	assert.Equal(t, len(pubs), 1)
}

//Test_StartWithAMQP this is integration test skips if QDR is not connected
func Test_StartWithRest(t *testing.T) {
	defer cleanUP()
	pubSubAPI.SetBaseURI(types.ParseURI(baseURL))
	// create Event consumers -Client
	v1amqp.CreateListener(eventInCh, "/cluster/node/ptp")
	// status sender
	v1amqp.CreateSender(eventInCh, "/cluster/node/ptp/status")
	err := ptpPlugin.Start(&wg, pubSubAPI, eventInCh, closeCh, nil)
	assert.Nil(t, err)
	log.Printf("waiting to receive intital event in out channel")
	event1 := <-eventInCh // initial event
	assert.Equal(t, channel.NEW, event1.Status)
	log.Printf("Event 1%#v\n", event1)

	// ping for status
	sub := v1pubsub.NewPubSub(nil, "/cluster/node/ptp")
	sub, _ = pubSubAPI.CreateSubscription(sub)
	e := v1event.CloudNativeEvent()
	ce, _ := v1event.CreateCloudEvents(e, sub)
	v1event.SendNewEventToDataChannel(eventInCh, "/cluster/node/ptp/status", ce)

	statusEvent := <-eventInCh // status updated
	assert.Equal(t, channel.NEW, statusEvent.Status)
	log.Printf("%v\n", statusEvent)

	event2 := <-eventInCh // after 5 secs
	assert.Equal(t, channel.NEW, event2.Status)
	log.Printf("event2 %#v\n", event2)
	closeCh <- true // close the channel
	pubs := pubSubAPI.GetPublishers()
	assert.Equal(t, len(pubs), 1)
}

//Test_StartWithAMQP this is integration test skips if QDR is not connected
func Test_StartWithAMQPAndRest(t *testing.T) {
	defer cleanUP()
	pubSubAPI.EnableTransport()
	pubSubAPI.SetBaseURI(types.ParseURI(baseURL))
	log.Printf("loading amqp with host %s", amqpHost)
	amqpInstance, err := v1amqp.GetAMQPInstance(amqpHost, eventInCh, eventOutCh, closeCh)
	if err != nil {
		t.Skipf("ampq.Dial(%#v): %v", amqpInstance, err)
	}
	wg.Add(1)
	amqpInstance.Start(&wg)

	// create Event consumers -Client
	v1amqp.CreateListener(eventInCh, "/cluster/node/ptp")
	//status sender
	v1amqp.CreateSender(eventInCh, "/cluster/node/ptp/status")

	err = ptpPlugin.Start(&wg, pubSubAPI, eventInCh, closeCh, nil)
	assert.Nil(t, err)
	log.Printf("waiting to receive intital event in out channel")
	event1 := <-eventOutCh // initial event
	assert.Equal(t, channel.NEW, event1.Status)
	log.Printf("%v\n", event1)

	//ping for status
	endpointURL := &types.URI{URL: url.URL{Scheme: "http", Host: "localhost", Path: fmt.Sprintf("%s%s", apiPath, "dummy")}}
	sub := v1pubsub.NewPubSub(endpointURL, "/cluster/node/ptp")
	sub, _ = pubSubAPI.CreateSubscription(sub)
	e := v1event.CloudNativeEvent()
	ce, _ := v1event.CreateCloudEvents(e, sub)
	v1event.SendNewEventToDataChannel(eventInCh, "/cluster/node/ptp/status", ce)

	statusEvent := <-eventOutCh // status updated
	assert.Equal(t, channel.SUCCEED, statusEvent.Status)
	log.Printf("%v\n", statusEvent)

	event2 := <-eventOutCh // after 5 secs
	assert.Equal(t, channel.NEW, event2.Status)
	log.Printf("%v\n", event2)
	closeCh <- true // close the channel
	pubs := pubSubAPI.GetPublishers()
	assert.Equal(t, len(pubs), 1)
}

//Test_StartWithAMQP this is integration test skips if QDR is not connected
func Test_StartWithAMQP(t *testing.T) {
	defer cleanUP()
	pubSubAPI.EnableTransport()
	pubSubAPI.SetBaseURI(nil)
	log.Printf("loading amqp with host %s", amqpHost)
	amqpInstance, err := v1amqp.GetAMQPInstance(amqpHost, eventInCh, eventOutCh, closeCh)
	if err != nil {
		t.Skipf("ampq.Dial(%#v): %v", amqpInstance, err)
	}
	wg.Add(1)
	amqpInstance.Start(&wg)

	// create Event consumers -Client
	v1amqp.CreateListener(eventInCh, "/cluster/node/ptp")
	//status sender
	v1amqp.CreateSender(eventInCh, "/cluster/node/ptp/status")

	err = ptpPlugin.Start(&wg, pubSubAPI, eventInCh, closeCh, nil)
	assert.Nil(t, err)
	log.Printf("waiting to receive intital event in out channel")
	event1 := <-eventOutCh // initial event
	assert.Equal(t, channel.NEW, event1.Status)
	log.Printf("%v\n", event1)

	//ping for status
	endpointURL := &types.URI{URL: url.URL{Scheme: "http", Host: "localhost", Path: fmt.Sprintf("%s%s", apiPath, "dummy")}}
	sub := v1pubsub.NewPubSub(endpointURL, "/cluster/node/ptp")
	sub, _ = pubSubAPI.CreateSubscription(sub)
	e := v1event.CloudNativeEvent()
	ce, _ := v1event.CreateCloudEvents(e, sub)
	v1event.SendNewEventToDataChannel(eventInCh, "/cluster/node/ptp/status", ce)

	statusEvent := <-eventOutCh // status updated
	assert.Equal(t, channel.SUCCEED, statusEvent.Status)
	log.Printf("%v\n", statusEvent)

	event2 := <-eventOutCh // after 5 secs
	assert.Equal(t, channel.NEW, event2.Status)
	log.Printf("%v\n", event2)
	closeCh <- true // close the channel
	pubs := pubSubAPI.GetPublishers()
	assert.Equal(t, len(pubs), 1)
}
