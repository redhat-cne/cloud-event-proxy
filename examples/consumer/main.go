package main

import (
	"encoding/json"
	"fmt"
	"github.com/redhat-cne/cloud-event-proxy/pkg/restclient"
	"github.com/redhat-cne/sdk-go/pkg/pubsub"
	"github.com/redhat-cne/sdk-go/pkg/types"
	v1pubsub "github.com/redhat-cne/sdk-go/v1/pubsub"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"sync"
)

var (
	apiHost                string = ":8080"
	apiPath                string = "/api/cloudNotifications/v1/"
	resourceAddressSports  string = "/news-service/sports"
	resourceAddressFinance string = "/news-service/finance"
	localAPIHost           string = ":9089"
)

func init() {
	if envAPIPath, ok := os.LookupEnv("API_PATH"); ok && envAPIPath != "" {
		apiPath = envAPIPath
	}
	if sAPIHost, ok := os.LookupEnv("API_HOST"); ok && sAPIHost != "" {
		apiHost = sAPIHost
	}
	if envLocalAPIHost, ok := os.LookupEnv("LOCAL_HOST"); ok && envLocalAPIHost != "" {
		localAPIHost = envLocalAPIHost
	}
}
func main() {
	var wg sync.WaitGroup
	wg.Add(1)
	go server()

	subs := []*pubsub.PubSub{&pubsub.PubSub{
		Resource: resourceAddressSports,
	}, &pubsub.PubSub{
		Resource: resourceAddressFinance,
	}}
	for _, sub := range subs {
		result := createSubscription(sub.Resource)
		if result != nil {
			if err := json.Unmarshal(result, sub); err != nil {
				log.Printf("failed to create a publisher object %#v\n", err)
			}
		}
		log.Printf("created publisher : %#v\n", sub)
	}

	log.Printf("waiting for events")
	wg.Wait()
}

func server() {
	http.HandleFunc("/event", getEvent)
	http.ListenAndServe(localAPIHost, nil)
}

func getEvent(w http.ResponseWriter, req *http.Request) {
	defer req.Body.Close()
	bodyBytes, err := ioutil.ReadAll(req.Body)
	if err != nil {
		log.Printf("error reading event %v", err)
	}
	e := string(bodyBytes)
	if e != "" {
		log.Printf("recieved event %s", string(bodyBytes))
	} else {
		w.WriteHeader(http.StatusNoContent)
	}
}

func createSubscription(resourceAddress string) []byte {
	//create publisher
	subURL := &types.URI{URL: url.URL{Scheme: "http", Host: apiHost, Path: fmt.Sprintf("%s%s", apiPath, "subscriptions")}}
	// this is loopback on server itself. Since current pod does not create any server
	endpointURL := &types.URI{URL: url.URL{Scheme: "http", Host: localAPIHost, Path: fmt.Sprintf("%s", "event")}}
	pub := v1pubsub.NewPubSub(endpointURL, resourceAddress)
	if b, err := json.Marshal(&pub); err == nil {
		rc := restclient.New()
		if status, b := rc.PostWithReturn(subURL, b); status == 201 {
			return b
		}
	} else {
		log.Printf("failed to create subscription ")
	}
	return nil
}
