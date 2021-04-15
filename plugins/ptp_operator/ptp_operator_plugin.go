package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	v2 "github.com/cloudevents/sdk-go/v2"
	"github.com/redhat-cne/cloud-event-proxy/pkg/restclient"
	"github.com/redhat-cne/sdk-go/pkg/channel"
	v1amqp "github.com/redhat-cne/sdk-go/v1/amqp"

	ceevent "github.com/redhat-cne/sdk-go/pkg/event"

	"log"
	"sync"

	"github.com/redhat-cne/sdk-go/pkg/pubsub"
	"github.com/redhat-cne/sdk-go/pkg/types"
	v1event "github.com/redhat-cne/sdk-go/v1/event"
	v1pubsub "github.com/redhat-cne/sdk-go/v1/pubsub"
)

var (
	resourceAddress string        = "/cluster/node/ptp"
	ptpEventType    string        = "ptp_status_type"
	eventInterval   time.Duration = time.Second * 5
)

// Start amqp  services to process events,metrics and status
func Start(wg *sync.WaitGroup, api *v1pubsub.API, eventInCh chan<- *channel.DataChan, closeCh <-chan bool, fn func(e ceevent.Event) error) error { //nolint:deadcode,unused
	// 1. Create event Publication

	pub, err := createPublisher(api, resourceAddress, eventInCh, fn)
	if err != nil {
		log.Printf("failed to create publisher %v", err)
		return err
	}
	// 3. Fire initial Event
	log.Printf("sending initial events ( probably not needed until consumer asks for it in initial state)")
	event := createPTPEvent(pub)
	_ = publishEvent(api, pub, event, eventInCh)
	// event handler
	log.Printf("spinning event loop")
	wg.Add(1)
	go func(wg *sync.WaitGroup, pub pubsub.PubSub) {
		ticker := time.NewTicker(eventInterval)
		defer ticker.Stop()
		defer wg.Done()
		for {
			select {
			case <-ticker.C:
				log.Printf("sending events")
				event := createPTPEvent(pub)
				_ = publishEvent(api, pub, event, eventInCh)
			case <-closeCh:
				fmt.Println("done")
				return
			}
		}
	}(wg, pub)
	return nil
}

func createPublisher(api *v1pubsub.API, address string, eventInCh chan<- *channel.DataChan, fn func(e ceevent.Event) error) (pub pubsub.PubSub, err error) {
	// this is loopback on server itself. Since current pod does not create any server
	publisherURL := fmt.Sprintf("%s%s", api.GetBaseURI().String(), "dummy")
	if api.GetBaseURI() == nil { // don't have rest api
		pub = v1pubsub.NewPubSub(types.ParseURI(publisherURL), address)
		pub, err = api.CreatePublisher(pub)
		if err != nil {
			log.Printf("failed to create publisher %v", err)
			return
		}
		// need to create this since we are not using rest api
		if api.HasTransportEnabled() {
			log.Printf("creating sender for resource %s\n", pub.Resource)
			v1amqp.CreateSender(eventInCh, pub.Resource)
		}
	} else { // using rest api
		apiURL := fmt.Sprintf("%s%s", api.GetBaseURI().String(), "publishers")
		// cluster/node should be  value from ENV
		newPub := v1pubsub.NewPubSub(types.ParseURI(publisherURL), address)
		var pubB []byte
		var status int
		if pubB, err = json.Marshal(newPub); err == nil {
			rc := restclient.New()
			if status, pubB = rc.PostWithReturn(apiURL, pubB); status != http.StatusCreated {
				err = fmt.Errorf("publisher creation api at %s, returned status %d", apiURL, status)
				return
			}
		} else {
			log.Printf("failed to marshal publisher ")
		}
		if err = json.Unmarshal(pubB, &pub); err != nil {
			return
		}
	}
	// 2.Create Status Listener
	if api.HasTransportEnabled() {
		onStatusRequestFn := func(e v2.Event) error {
			log.Printf("fire events for above publisher")
			event := createPTPEvent(pub)
			_ = publishEvent(api, pub, event, eventInCh)
			return nil
		}
		v1amqp.CreateNewStatusListener(eventInCh, fmt.Sprintf("%s/%s", pub.Resource, "status"), onStatusRequestFn, fn)
	}
	return pub, err
}

func createPTPEvent(pub pubsub.PubSub) ceevent.Event {
	// create an event
	event := v1event.CloudNativeEvent()
	event.ID = pub.ID
	event.Type = ptpEventType
	event.SetTime(types.Timestamp{Time: time.Now().UTC()}.Time)
	event.SetDataContentType(ceevent.ApplicationJSON)
	data := ceevent.Data{
		Version: "v1",
		Values: []ceevent.DataValue{{
			Resource:  "/cluster/node/ptp",
			DataType:  ceevent.NOTIFICATION,
			ValueType: ceevent.ENUMERATION,
			Value:     ceevent.ACQUIRING_SYNC,
		},
		},
	}
	event.SetData(data)
	return event
}

// publish events
func publishEvent(api *v1pubsub.API, pub pubsub.PubSub, e ceevent.Event, eventInCh chan<- *channel.DataChan) error {
	//create publisher
	if api.GetBaseURI() == nil { // don't have rest api
		ce, err := v1event.CreateCloudEvents(e, pub)
		if err != nil {
			log.Printf("failed to publish events %v", err)
			return err
		}
		v1event.SendNewEventToDataChannel(eventInCh, pub.Resource, ce)
	} else {
		url := api.GetBaseURI().String()
		url = fmt.Sprintf("%s%s", url, "create/event")
		rc := restclient.New()
		err := rc.PostEvent(url, e)
		if err != nil {
			log.Printf("error postign events %v", err)
			return err
		}
		log.Printf("Published event %s", e.String())
	}
	return nil
}
