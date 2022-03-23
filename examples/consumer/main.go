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
	"github.com/redhat-cne/sdk-go/pkg/event"
	ptpEvent "github.com/redhat-cne/sdk-go/pkg/event/ptp"
	redfishEvent "github.com/redhat-cne/sdk-go/pkg/event/redfish"
	"github.com/redhat-cne/sdk-go/pkg/pubsub"
	"github.com/redhat-cne/sdk-go/pkg/types"
	v1pubsub "github.com/redhat-cne/sdk-go/v1/pubsub"
	log "github.com/sirupsen/logrus"
)

// ConsumerTypeEnum enum to choose consumer type
type ConsumerTypeEnum string

var consumerType ConsumerTypeEnum

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
	apiAddr         string = "localhost:8089"
	apiPath         string = "/api/cloudNotifications/v1/"
	localAPIAddr    string = "localhost:8989"
	resourcePrefix  string = "/cluster/node/%s%s"
	mockResource    string = "/mock"
	mockResourceKey string = "mock"
)

func main() {
	common.InitLogger()
	flag.StringVar(&localAPIAddr, "local-api-addr", "localhost:8989", "The address the local api binds to .")
	flag.StringVar(&apiPath, "api-path", "/api/cloudNotifications/v1/", "The rest api path.")
	flag.StringVar(&apiAddr, "api-addr", "localhost:8089", "The address the framework api endpoint binds to.")
	flag.Parse()

	nodeName := os.Getenv("NODE_NAME")
	if nodeName == "" {
		log.Error("cannot find NODE_NAME environment variable,setting to default `mock` node")
		nodeName = mockResourceKey
	}

	enableStatusCheck := common.GetBoolEnv("ENABLE_STATUS_CHECK")

	consumerTypeEnv := os.Getenv("CONSUMER_TYPE")
	if consumerTypeEnv == "" {
		consumerType = MOCK
	} else {
		consumerType = ConsumerTypeEnum(consumerTypeEnv)
	}
	subscribeTo := initSubscribers(consumerType)
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
	var subs []*pubsub.PubSub
	for _, resource := range subscribeTo {
		subs = append(subs, &pubsub.PubSub{
			Resource: fmt.Sprintf(resourcePrefix, nodeName, resource),
		})
	}

	if enableStatusCheck {
		for _, s := range subs {
			createPublisherForStatusPing(s.Resource) // ptp // disable this for testing else you will see context deadline error
		}
	}
	for _, s := range subs {
		su, e := createSubscription(s.Resource)
		if e != nil {
			log.Errorf("failed to create a subscription object %v\n", e)
		} else {
			log.Infof("created subscription: %s\n", su.String())
			s.URILocation = su.URILocation
		}

	}

	// ping  for status every n secs

	if enableStatusCheck {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range time.Tick(StatusCheckInterval * time.Second) {
				for _, s := range subs {
					pingForStatus(s.ID)
				}
			}
		}()
	}

	log.Info("waiting for events")
	wg.Wait()

}

func createSubscription(resourceAddress string) (sub pubsub.PubSub, err error) {
	var status int
	subURL := &types.URI{URL: url.URL{Scheme: "http",
		Host: apiAddr,
		Path: fmt.Sprintf("%s%s", apiPath, "subscriptions")}}
	endpointURL := &types.URI{URL: url.URL{Scheme: "http",
		Host: localAPIAddr,
		Path: fmt.Sprintf("%s", "event")}}

	sub = v1pubsub.NewPubSub(endpointURL, resourceAddress)
	var subB []byte

	if subB, err = json.Marshal(&sub); err == nil {
		rc := restclient.New()
		if status, subB = rc.PostWithReturn(subURL, subB); status != http.StatusCreated {
			err = fmt.Errorf("error subscription creation api at %s, returned status %d", subURL, status)
		} else {
			err = json.Unmarshal(subB, &sub)
		}
	} else {
		err = fmt.Errorf("failed to marshal subscription or %s", resourceAddress)
	}
	return
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
		processEvent(bodyBytes)
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

func processEvent(data []byte) {
	var e event.Event
	json.Unmarshal(data, &e)
	// Note that there is no UnixMillis, so to get the
	// milliseconds since epoch you'll need to manually
	// divide from nanoseconds.
	latency := (time.Now().UnixNano() - e.Time.UnixNano()) / 1000000
	// set log to Info level for performance measurement
	log.Infof("Latency for the event: %v ms\n", latency)
}

func initSubscribers(cType ConsumerTypeEnum) map[string]string {
	subscribeTo := make(map[string]string)
	switch cType {
	case PTP:
		subscribeTo[string(ptpEvent.OsClockSyncStateChange)] = string(ptpEvent.OsClockSyncState)
		subscribeTo[string(ptpEvent.PtpClockClassChange)] = string(ptpEvent.PtpClockClass)
		subscribeTo[string(ptpEvent.PtpStateChange)] = string(ptpEvent.PtpLockState)
	case MOCK:
		subscribeTo[mockResourceKey] = mockResource
	case HW:
		subscribeTo[string(redfishEvent.Alert)] = string(redfishEvent.Systems)
		subscribeTo[string(redfishEvent.ResourceAdded)] = string(redfishEvent.Systems)
		subscribeTo[string(redfishEvent.ResourceUpdated)] = string(redfishEvent.Systems)
		subscribeTo[string(redfishEvent.ResourceRemoved)] = string(redfishEvent.Systems)
		subscribeTo[string(redfishEvent.StatusChange)] = string(redfishEvent.Systems)
	default:
		subscribeTo[mockResourceKey] = mockResource
	}
	return subscribeTo
}
