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
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"
)

var (
	apiHost                string = ":8080"
	apiPath                string = "/api/cloudNotifications/v1/"
	resourceAddressSports  string = "/news-service/sports"
	resourceAddressFinance string = "/news-service/finance"
	localAPIHost           string = ":9087"
)

func init() {
	if envAPIPath, ok := os.LookupEnv("API_PATH"); ok && envAPIPath != "" {
		apiPath = envAPIPath
	}
	if envAPIHost, ok := os.LookupEnv("API_HOST"); ok && envAPIHost != "" {
		apiHost = envAPIHost
	}
	if envLocalAPIHost, ok := os.LookupEnv("LOCAL_HOST"); ok && envLocalAPIHost != "" {
		localAPIHost = envLocalAPIHost
	}
}

func main() {
	var wg sync.WaitGroup
	wg.Add(1)
	go server()

	pubs := []*pubsub.PubSub{&pubsub.PubSub{
		Resource: resourceAddressSports,
	}, &pubsub.PubSub{
		Resource: resourceAddressFinance,
	}}
	for _, pub := range pubs {
		result := createPublisher(pub.Resource)
		if result != nil {
			if err := json.Unmarshal(result, pub); err != nil {
				log.Printf("failed to create a publisher object %#v\n", err)
			}
		}
		log.Printf("created publisher : %#v\n", pub)
	}

	// create events periodically
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range time.Tick(2 * time.Second) {
			// create an event
			for _, pub := range pubs {
				event := v1event.CloudNativeEvent()
				event.SetID(pub.ID)
				event.Type = "ptp_status_type"
				event.SetTime(types.Timestamp{Time: time.Now().UTC()}.Time)
				event.SetDataContentType(cneevent.ApplicationJSON)
				data := cneevent.Data{
					Version: "v1",
					Values: []cneevent.DataValue{{
						Resource:  pub.Resource,
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

		}
	}()
	wg.Wait()
}

func createPublisher(resourceAddress string) []byte {
	//create publisher
	publisherURL := &types.URI{URL: url.URL{Scheme: "http", Host: apiHost, Path: fmt.Sprintf("%s%s", apiPath, "publishers")}}
	endpointURL := &types.URI{URL: url.URL{Scheme: "http", Host: localAPIHost, Path: fmt.Sprintf("%s", "ack/event")}}
	pub := v1pubsub.NewPubSub(endpointURL, resourceAddress)
	if b, err := json.Marshal(&pub); err == nil {
		rc := restclient.New()
		if status, b := rc.PostWithReturn(publisherURL, b); status == http.StatusCreated {
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
	err := rc.PostEvent(url, e)
	if err != nil {
		log.Printf("error publishing events %v", err)
	} else {
		log.Printf("Published event %s", e.String())
	}
}

func server() {
	http.HandleFunc("/ack/event", ackEvent)
	http.ListenAndServe(localAPIHost, nil)
}

func ackEvent(w http.ResponseWriter, req *http.Request) {
	defer req.Body.Close()
	bodyBytes, err := ioutil.ReadAll(req.Body)
	if err != nil {
		log.Printf("error reading acknowledgment  %v", err)
	}
	e := string(bodyBytes)
	if e != "" {
		log.Printf("recieved ack %s", string(bodyBytes))
	} else {
		w.WriteHeader(http.StatusNoContent)
	}
}
