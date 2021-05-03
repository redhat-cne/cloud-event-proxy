package main

import (
	"encoding/json"
	"fmt"
	"github.com/redhat-cne/cloud-event-proxy/pkg/common"
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
	"time"
)

var (
	apiPort                int    = 8080
	apiPath                string = "/api/cloudNotifications/v1/"
	resourceAddressSports  string = "/news-service/sports"
	resourceAddressFinance string = "/news-service/finance"
	localAPIPort           int    = 9089
)

func init() {
	if envAPIPath, ok := os.LookupEnv("API_PATH"); ok && envAPIPath != "" {
		apiPath = envAPIPath
	}

	if envAPIPort := common.GetIntEnv("API_PORT"); envAPIPort > 0 {
		apiPort = envAPIPort
	}

	if envLocalAPIPort := common.GetIntEnv("LOCAL_API_PORT"); envLocalAPIPort > 0 {
		localAPIPort = envLocalAPIPort
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
	healthURL := &types.URI{URL: url.URL{Scheme: "http",
		Host: fmt.Sprintf("%s:%d", "localhost", apiPort),
		Path: fmt.Sprintf("%s%s", apiPath, "health")}}
RETRY:
	if ok, _ := common.APIHealthCheck(healthURL, 2*time.Second); !ok {
		goto RETRY
	}
	for _, sub := range subs {
		result := createSubscription(sub.Resource)
		if result != nil {
			if err := json.Unmarshal(result, sub); err != nil {
				log.Printf("failed to create a subscription object %v\n", err)
			}
		}
		log.Printf("created subscription : %s\n", sub.String())
	}

	log.Printf("waiting for events")
	wg.Wait()

}

func server() {
	http.HandleFunc("/event", getEvent)
	http.ListenAndServe(fmt.Sprintf("localhost:%d", localAPIPort), nil)
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
	subURL := &types.URI{URL: url.URL{Scheme: "http",
		Host: fmt.Sprintf("%s:%d", "localhost", apiPort),
		Path: fmt.Sprintf("%s%s", apiPath, "subscriptions")}}
	endpointURL := &types.URI{URL: url.URL{Scheme: "http",
		Host: fmt.Sprintf("%s:%d", "localhost", localAPIPort),
		Path: fmt.Sprintf("%s", "event")}}

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
