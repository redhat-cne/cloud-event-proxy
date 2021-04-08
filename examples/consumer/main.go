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
	apiHost string = "localhost:8080"
	apiPath string = "/api/cloudNotifications/v1/"
)

func init() {
	if sPath, ok := os.LookupEnv("API_HOST"); ok && sPath != "" {
		apiHost = sPath
	}
}
func main() {
	var wg sync.WaitGroup
	wg.Add(1)
	go server()

	var sub pubsub.PubSub
	result := createSubscription()
	if result != nil {
		if err := json.Unmarshal(result, &sub); err != nil {
			log.Printf("Failed to create poublisher object %v", err)
		}
	}
	log.Printf("subsripton done %v", sub)
	log.Printf("waiting for events")
	wg.Wait()
}

func server() {
	http.HandleFunc("/event", getEvent)
	http.ListenAndServe(":8090", nil)
}

func getEvent(w http.ResponseWriter, req *http.Request) {
	defer req.Body.Close()
	bodyBytes, err := ioutil.ReadAll(req.Body)
	if err != nil {
		log.Printf("error reading event %v", err)
	} else {
		log.Printf("Recieved event %s", string(bodyBytes))
	}
	if string(bodyBytes)==""{ // make sure to return no conent for endPointURI
		w.WriteHeader(http.StatusNoContent)
	}

}

func createSubscription() []byte {
	//create publisher
	subURL := &types.URI{URL: url.URL{Scheme: "http", Host: apiHost, Path: fmt.Sprintf("%s%s", apiPath, "subscriptions")}}
	// this is loopback on server itself. Since current pod does not create any server
	endpointURL := &types.URI{URL: url.URL{Scheme: "http", Host: "localhost:8090", Path: fmt.Sprintf("%s", "event")}}
	pub := v1pubsub.NewPubSub(endpointURL, "test/test/test")
	if b, err := json.Marshal(pub); err == nil {
		rc := restclient.New()
		if status, b := rc.PostWithReturn(subURL.String(), b); status == 201 {
			return b
		}
	} else {
		log.Printf("failed to create subscription ")
	}
	return nil
}
