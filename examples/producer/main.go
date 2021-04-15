package main

import (
	"encoding/json"
	"fmt"
	"github.com/redhat-cne/cloud-event-proxy/pkg/restclient"
	cneevent "github.com/redhat-cne/sdk-go/pkg/event"
	"github.com/redhat-cne/sdk-go/pkg/pubsub"
	"github.com/redhat-cne/sdk-go/pkg/types"
	v1event "github.com/redhat-cne/sdk-go/v1/event"
	v1pubsub "github.com/redhat-cne/sdk-go/v1/pubsub"
	"log"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"
)

var (
	apiHost        string = "localhost:8080"
	apiPath        string = "/api/cloudNotifications/v1/"
	pubSubInstance *v1pubsub.API
)

func init() {
	if sPath, ok := os.LookupEnv("API_HOST"); ok && sPath != "" {
		apiHost = sPath
	}
}

func main() {
	var pub pubsub.PubSub
	var wg sync.WaitGroup
	result := createPublisher()
	if result != nil {
		if err := json.Unmarshal(result, &pub); err != nil {
			log.Printf("Failed to create poublisher object %v", err)
		}
	}
	fmt.Printf("publisher:\n%s", pub.String())
	// create events periodically
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range time.Tick(2 * time.Second) {
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
			publishEvent(event)
		}
	}()
	wg.Wait()
}

func createPublisher() []byte {
	//create publisher
	publisherURL := &types.URI{URL: url.URL{Scheme: "http", Host: apiHost, Path: fmt.Sprintf("%s%s", apiPath, "publishers")}}
	// this is loopback on server itself. Since current pod does not create any server
	endpointURL := &types.URI{URL: url.URL{Scheme: "http", Host: apiHost, Path: fmt.Sprintf("%s%s", apiPath, "dummy")}}
	pub := v1pubsub.NewPubSub(endpointURL, "test/test/test")
	if b, err := json.Marshal(pub); err == nil {
		rc := restclient.New()
		if status, b := rc.PostWithReturn(publisherURL.String(), b); status == http.StatusCreated {
			return b
		}
	} else {
		log.Printf("failed to create publisher ")
	}
	return nil
}

func publishEvent(e cneevent.Event) {
	//create publisher
	url := &types.URI{URL: url.URL{Scheme: "http", Host: apiHost, Path: fmt.Sprintf("%s%s", apiPath, "create/event")}}
	rc := restclient.New()
	err := rc.PostEvent(url.String(), e)
	if err != nil {
		log.Printf("error postign events %v", err)
	} else {
		log.Printf("Published event %s", e.String())
	}
}
