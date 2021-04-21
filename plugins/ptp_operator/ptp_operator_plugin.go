package main

import (
	"fmt"
	v2 "github.com/cloudevents/sdk-go/v2"
	"github.com/redhat-cne/cloud-event-proxy/pkg/common"
	v1amqp "github.com/redhat-cne/sdk-go/v1/amqp"
	"time"

	ceevent "github.com/redhat-cne/sdk-go/pkg/event"

	"log"
	"sync"

	"github.com/redhat-cne/sdk-go/pkg/pubsub"
	"github.com/redhat-cne/sdk-go/pkg/types"
	v1pubsub "github.com/redhat-cne/sdk-go/v1/pubsub"
)

var (
	resourceAddress string        = "/cluster/node/ptp"
	eventInterval   time.Duration = time.Second * 5
	config          *common.SCConfiguration
)

// Start ptp plugin to process events,metrics and status, ecpects rest api availble to create publisher and subscriptions
func Start(wg *sync.WaitGroup, configuration *common.SCConfiguration, fn func(e ceevent.Event) error) error { //nolint:deadcode,unused
	// 1. Create event Publication
	var pub pubsub.PubSub
	var err error
	var e ceevent.Event
	config = configuration

	if pub, err = createPublisher(resourceAddress); err != nil {
		log.Printf("failed to create a publisher %v", err)
		return err
	}
	log.Printf("Created publisher %v", pub)
	// 2.Create Status Listener
	onStatusRequestFn := func(e v2.Event) error {
		log.Printf("got status check call,fire events for above publisher")
		event, _ := createPTPEvent(pub)
		_ = common.PublishEvent(config, event)
		return nil
	}
	v1amqp.CreateNewStatusListener(config.EventInCh, fmt.Sprintf("%s/%s", pub.Resource, "status"), onStatusRequestFn, fn)

	// 3. Fire initial Event
	log.Printf("sending initial events ( probably not needed until consumer asks for it in initial state)")
	e, _ = createPTPEvent(pub)
	_ = common.PublishEvent(config, e)

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
				e, _ := createPTPEvent(pub)
				_ = common.PublishEvent(config, e)
			case <-config.CloseCh:
				fmt.Println("done")
				return
			}
		}
	}(wg, pub)
	return nil
}

func createPublisher(address string) (pub pubsub.PubSub, err error) {
	// this is loopback on server itself. Since current pod does not create any server
	returnURL := fmt.Sprintf("%s%s", config.BaseURL, "dummy")
	pubToCreate := v1pubsub.NewPubSub(types.ParseURI(returnURL), address)
	pub, err = common.CreatePublisher(config, pubToCreate)
	if err != nil {
		log.Printf("failed to create publisher %v", pub)
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
			Value:     ceevent.ACQUIRING_SYNC,
		},
		},
	}
	e, err := common.CreateEvent(pub.ID, "PTP_EVENT", data)
	return e, err
}
