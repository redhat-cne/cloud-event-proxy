// Copyright 2020 The Cloud Native Events Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/redhat-cne/cloud-event-proxy/pkg/common"
	"github.com/redhat-cne/cloud-event-proxy/pkg/restclient"
	"github.com/redhat-cne/sdk-go/pkg/pubsub"
	"github.com/redhat-cne/sdk-go/pkg/types"
	v1pubsub "github.com/redhat-cne/sdk-go/v1/pubsub"
	log "github.com/sirupsen/logrus"
)

var (
	apiAddr                string = "localhost:8080"
	apiPath                string = "/api/cloudNotifications/v1/"
	resourceAddressSports  string = "/news-service/sports"
	resourceAddressFinance string = "/news-service/finance"
	resourceAddressHwEvent string = "/hw-event"
	localAPIAddr           string = "localhost:9089"
)

func main() {
	common.InitLogger()
	flag.StringVar(&localAPIAddr, "local-api-addr", "localhost:9089", "The address the local api binds to .")
	flag.StringVar(&apiPath, "api-path", "/api/cloudNotifications/v1/", "The rest api path.")
	flag.StringVar(&apiAddr, "api-addr", "localhost:8080", "The address the framework api endpoint binds to.")
	flag.Parse()

	var wg sync.WaitGroup
	wg.Add(1)
	go server()

	subs := []*pubsub.PubSub{&pubsub.PubSub{
		Resource: resourceAddressSports,
	}, &pubsub.PubSub{
		Resource: resourceAddressFinance,
	}, &pubsub.PubSub{
		Resource: resourceAddressHwEvent,
	}}
	healthURL := &types.URI{URL: url.URL{Scheme: "http",
		Host: apiAddr,
		Path: fmt.Sprintf("%s%s", apiPath, "health")}}
RETRY:
	if ok, _ := common.APIHealthCheck(healthURL, 2*time.Second); !ok {
		goto RETRY
	}
	for _, sub := range subs {
		result := createSubscription(sub.Resource)
		if result != nil {
			if err := json.Unmarshal(result, sub); err != nil {
				log.Errorf("failed to create a subscription object %v\n", err)
			}
		}
		log.Infof("created subscription: %s\n", sub.String())
	}

	log.Info("waiting for events")
	wg.Wait()

}

func server() {
	http.HandleFunc("/event", getEvent)
	http.ListenAndServe(localAPIAddr, nil)
}

func getEvent(w http.ResponseWriter, req *http.Request) {
	defer req.Body.Close()
	bodyBytes, err := ioutil.ReadAll(req.Body)
	if err != nil {
		log.Errorf("error reading event %v", err)
	}
	e := string(bodyBytes)
	if e != "" {
		log.Debugf("received event %s", string(bodyBytes))
	} else {
		w.WriteHeader(http.StatusNoContent)
	}
}

func createSubscription(resourceAddress string) []byte {
	subURL := &types.URI{URL: url.URL{Scheme: "http",
		Host: apiAddr,
		Path: fmt.Sprintf("%s%s", apiPath, "subscriptions")}}
	endpointURL := &types.URI{URL: url.URL{Scheme: "http",
		Host: localAPIAddr,
		Path: fmt.Sprintf("%s", "event")}}

	pub := v1pubsub.NewPubSub(endpointURL, resourceAddress)
	if b, err := json.Marshal(&pub); err == nil {
		rc := restclient.New()
		if status, b := rc.PostWithReturn(subURL, b); status == http.StatusCreated {
			return b
		}
		log.Errorf("subscription create returned error %s", string(b))
	} else {
		log.Errorf("failed to create subscription ")
	}
	return nil
}
