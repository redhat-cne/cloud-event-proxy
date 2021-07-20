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
	"os"
	"sync"
	"time"

	"github.com/redhat-cne/cloud-event-proxy/pkg/common"
	"github.com/redhat-cne/cloud-event-proxy/pkg/restclient"
	"github.com/redhat-cne/sdk-go/pkg/pubsub"
	"github.com/redhat-cne/sdk-go/pkg/types"
	v1pubsub "github.com/redhat-cne/sdk-go/v1/pubsub"
	log "github.com/sirupsen/logrus"
)

// ConsumerTypeEnum enum to choose consumer type
type ConsumerTypeEnum string

const (
	// PTP consumer
	PTP ConsumerTypeEnum = "PTP"
	// HW Consumer
	HW ConsumerTypeEnum = "HW"
	// MOCK consumer
	MOCK ConsumerTypeEnum = "MOCK"
	// StatusCheckInterval for consumer to pull for status
	StatusCheckInterval = 30
)

var (
	apiAddr      string = "localhost:8080"
	apiPath      string = "/api/cloudNotifications/v1/"
	localAPIAddr string = "localhost:8989"

	resourceAddressMock    string = "/cluster/node/mock"
	resourceAddressPTP     string = "/cluster/node/%s/ptp"
	resourceAddressHwEvent string = "/hw-event"
)

func main() {
	common.InitLogger()
	flag.StringVar(&localAPIAddr, "local-api-addr", "localhost:8989", "The address the local api binds to .")
	flag.StringVar(&apiPath, "api-path", "/api/cloudNotifications/v1/", "The rest api path.")
	flag.StringVar(&apiAddr, "api-addr", "localhost:8080", "The address the framework api endpoint binds to.")
	flag.Parse()

	nodeName := os.Getenv("NODE_NAME")
	if nodeName == "" {
		log.Error("cannot find NODE_NAME environment variable,setting to default `mock` node")
		nodeName = "mock"
	}

	enableStatusCheck := common.GetBoolEnv("ENABLE_STATUS_CHECK")

	var consumerType ConsumerTypeEnum
	consumerTypeEnv := os.Getenv("CONSUMER_TYPE")
	if consumerTypeEnv == "" {
		consumerType = MOCK
	} else {
		consumerType = ConsumerTypeEnum(consumerTypeEnv)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go server() // spin local api
	time.Sleep(5 * time.Second)

	// check sidecar api health
	healthURL := &types.URI{URL: url.URL{Scheme: "http",
		Host: apiAddr,
		Path: fmt.Sprintf("%s%s", apiPath, "health")}}
RETRY:
	if ok, _ := common.APIHealthCheck(healthURL, 2*time.Second); !ok {
		goto RETRY
	}

	subs := []*pubsub.PubSub{&pubsub.PubSub{
		Resource: resourceAddressHwEvent,
	}, &pubsub.PubSub{
		Resource: resourceAddressMock,
	}, &pubsub.PubSub{
		Resource: fmt.Sprintf(resourceAddressPTP, nodeName),
	},
	}

	if enableStatusCheck {
		if consumerType == PTP {
			createPublisherForStatusPing(subs[2].Resource) // ptp // disable this for testing else you will see context deadline error
		} else if consumerType == MOCK {
			createPublisherForStatusPing(subs[1].Resource) // mock
		}
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

	// ping  for status every n secs

	if enableStatusCheck {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range time.Tick(StatusCheckInterval * time.Second) {
				//only for PTP testing
				if consumerType == PTP {
					pingForStatus(subs[2].ID) // ptp // disable this for testing else you will see context deadline error
				} else if consumerType == MOCK {
					pingForStatus(subs[1].ID) // mock
				}
			}
		}()
	}

	log.Info("waiting for events")
	wg.Wait()

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

func createPublisherForStatusPing(resourceAddress string) []byte {
	//create publisher
	log.Infof("creating sender for Status %s", fmt.Sprintf("%s/%s", resourceAddress, "status"))
	publisherURL := &types.URI{URL: url.URL{Scheme: "http",
		Host: apiAddr,
		Path: fmt.Sprintf("%s%s", apiPath, "publishers")}}
	endpointURL := &types.URI{URL: url.URL{Scheme: "http",
		Host: localAPIAddr,
		Path: fmt.Sprintf("%s", "ack/event")}}
	log.Infof("publisher endpoint %s", endpointURL.String())
	pub := v1pubsub.NewPubSub(endpointURL, fmt.Sprintf("%s/%s", resourceAddress, "status"))
	if b, err := json.Marshal(&pub); err == nil {
		rc := restclient.New()
		if status, b := rc.PostWithReturn(publisherURL, b); status == http.StatusCreated {
			log.Infof("create status ping publisher %s", string(b))
			return b
		}
		log.Errorf("status ping publisher create returned error %s", string(b))
	} else {
		log.Errorf("failed to create publisher for status ping ")
	}
	return nil
}

// pingForStatus sends pings to fetch events status
func pingForStatus(resourceID string) {
	//create publisher
	url := &types.URI{URL: url.URL{Scheme: "http",
		Host: apiAddr,
		Path: fmt.Sprintf("%s%s", apiPath, fmt.Sprintf("subscriptions/status/%s", resourceID))}}
	rc := restclient.New()
	status := rc.Put(url)
	if status != http.StatusAccepted {
		log.Errorf("error pinging for status check %d to url %s", status, url.String())
	} else {
		log.Debugf("ping check submitted (%d)", status)
	}
}

// Consumer webserver
func server() {
	http.HandleFunc("/event", getEvent)
	http.HandleFunc("/ack/event", ackEvent)
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

func ackEvent(w http.ResponseWriter, req *http.Request) {
	defer req.Body.Close()
	bodyBytes, err := ioutil.ReadAll(req.Body)
	if err != nil {
		log.Errorf("error reading acknowledgment  %v", err)
	}
	e := string(bodyBytes)
	if e != "" {
		log.Debugf("received ack %s", string(bodyBytes))
	} else {
		w.WriteHeader(http.StatusNoContent)
	}
}
