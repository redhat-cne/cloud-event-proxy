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
	"io"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"

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

// PublisherHealthOk ... check if publisher is oka assuming nly one publisher available
var PublisherHealthOk bool

const (
	// PTP consumer
	PTP ConsumerTypeEnum = "PTP"
	// HW Consumer
	HW ConsumerTypeEnum = "HW"
	// MOCK consumer
	MOCK ConsumerTypeEnum = "MOCK"
	// StatusCheckInterval for consumer to pull for status
	StatusCheckInterval = 60
)

var (
	apiAddr            = "localhost:8089"
	apiPath            = "/api/ocloudNotifications/v1/"
	localAPIAddr       = "localhost:8989"
	resourcePrefix     = "/cluster/node/%s%s"
	mockResource       = "/mock"
	mockResourceKey    = "mock"
	httpEventPublisher string
	eventPublishers    = make(map[string]bool)
	subs               []*pubsub.PubSub
)

func main() {
	common.InitLogger()
	flag.StringVar(&localAPIAddr, "local-api-addr", "localhost:8989", "The address the local api binds to .")
	flag.StringVar(&apiPath, "api-path", "/api/ocloudNotifications/v1/", "The rest api path.")
	flag.StringVar(&apiAddr, "api-addr", "localhost:8089", "The address the framework api endpoint binds to.")
	flag.StringVar(&httpEventPublisher, "http-event-publishers", "", "Comma separated address of the publishers available.")
	flag.Parse()

	nodeIP := os.Getenv("NODE_IP")
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

	// check consumer sidecar api health
	healthURL := &types.URI{URL: url.URL{Scheme: "http",
		Host: apiAddr,
		Path: fmt.Sprintf("%s%s", apiPath, "health")}}
RETRY:
	if ok, _ := common.APIHealthCheck(healthURL, 2*time.Second); !ok {
		goto RETRY
	}

	for _, resource := range subscribeTo {
		subs = append(subs, &pubsub.PubSub{
			ID:       getUUID(fmt.Sprintf(resourcePrefix, nodeName, resource)).String(),
			Resource: fmt.Sprintf(resourcePrefix, nodeName, resource),
		})
	}

	updateHTTPPublishers(nodeIP, nodeName, httpEventPublisher)

	// ping for status every n secs
	// no amq support
	wg.Add(1)
	go func() { // in this example there will be only one publisher
		defer wg.Done()
		for range time.Tick(StatusCheckInterval * time.Second) {
			for p, currenStatus := range eventPublishers {
				newStatus := publisherHealthCheck(p)
				PublisherHealthOk = newStatus
				eventPublishers[p] = newStatus
				if newStatus && !currenStatus {
					log.Info("subscribing to events")
					subscribeToEvents()
				} else if !newStatus && currenStatus {
					deleteAllSubscriptions()
					log.Info("delete subscription.")
				}
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		if enableStatusCheck {
			time.Sleep(5 * time.Second)
			if PublisherHealthOk {
				for _, s := range subs {
					getCurrentState(s.Resource)
				}
			}
			for range time.Tick(StatusCheckInterval * time.Second) {
				if PublisherHealthOk {
					for _, s := range subs {
						getCurrentState(s.Resource)
					}
				} else {
					log.Info("skipping since publisher not available ")
				}
			}
		}
	}()

	log.Info("waiting for events")
	wg.Wait()
	deleteAllSubscriptions()
	time.Sleep(3 * time.Second)
}

func deleteAllSubscriptions() {
	deleteURL := &types.URI{URL: url.URL{Scheme: "http",
		Host: apiAddr,
		Path: fmt.Sprintf("%s%s", apiPath, "subscriptions")}}
	rc := restclient.New()
	rc.Delete(deleteURL)
}

func subscribeToEvents() {
	// if AMQ enabled the subscription will create an AMQ listener client
	// IF HTTP enabled, the subscription will post a subscription  requested to all
	// publishers that are defined in http-event-publisher variable
	for _, status := range eventPublishers {
		if status {
			for _, s := range subs {
				su, e := createSubscription(s.Resource)
				if e != nil {
					log.Errorf("failed to create a subscription object %v", e)
				} else {
					log.Infof("created subscription: %s", su.String())
					s.URILocation = su.URILocation
					s.ID = su.ID
				}
			}
		}
	}
}
func createSubscription(resourceAddress string) (sub pubsub.PubSub, err error) {
	var status int
	subURL := &types.URI{URL: url.URL{Scheme: "http",
		Host: apiAddr,
		Path: fmt.Sprintf("%s%s", apiPath, "subscriptions")}}
	endpointURL := &types.URI{URL: url.URL{Scheme: "http",
		Host: localAPIAddr,
		Path: "event"}}

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

// getCurrentState get event state for the resource
func getCurrentState(resource string) {
	//create publisher
	url := &types.URI{URL: url.URL{Scheme: "http",
		Host: apiAddr,
		Path: fmt.Sprintf("%s%s", apiPath, fmt.Sprintf("%s/CurrentState", resource[1:]))}}
	rc := restclient.New()
	status, cloudEvent := rc.Get(url)
	if status != http.StatusOK {
		log.Errorf("CurrentState:error %d from url %s, %s", status, url.String(), cloudEvent)
	} else {
		log.Debugf("Got CurrentState: %s ", cloudEvent)
	}
}

// Consumer webserver
func server() {
	http.HandleFunc("/event", getEvent)
	http.HandleFunc("/ack/event", ackEvent)
	err := http.ListenAndServe(localAPIAddr, nil)
	if err != nil {
		log.Errorf("error creating event server %s", err)
	}
}

func getEvent(w http.ResponseWriter, req *http.Request) {
	defer req.Body.Close()
	bodyBytes, err := io.ReadAll(req.Body)
	if err != nil {
		log.Errorf("error reading event %v", err)
	}
	e := string(bodyBytes)
	if e != "" {
		processEvent(bodyBytes)
		log.Infof("received event %s", string(bodyBytes))
	} else {
		w.WriteHeader(http.StatusNoContent)
	}
}

func ackEvent(w http.ResponseWriter, req *http.Request) {
	defer req.Body.Close()
	bodyBytes, err := io.ReadAll(req.Body)
	if err != nil {
		log.Errorf("error reading acknowledgment  %v", err)
	}
	e := string(bodyBytes)
	if e != "" {
		log.Infof("received ack %s", string(bodyBytes))
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
	log.Infof("Latency for the event: %v ms", latency)
}

func initSubscribers(cType ConsumerTypeEnum) map[string]string {
	subscribeTo := make(map[string]string)
	switch cType {
	case PTP:
		subscribeTo[string(ptpEvent.OsClockSyncStateChange)] = string(ptpEvent.OsClockSyncState)
		subscribeTo[string(ptpEvent.PtpClockClassChange)] = string(ptpEvent.PtpClockClass)
		subscribeTo[string(ptpEvent.PtpStateChange)] = string(ptpEvent.PtpLockState)
		subscribeTo[string(ptpEvent.GnssStateChange)] = string(ptpEvent.GnssSyncStatus)
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

func getUUID(s string) uuid.UUID {
	var namespace = uuid.NameSpaceURL
	var url = []byte(s)
	return uuid.NewMD5(namespace, url)
}

func publisherHealthCheck(apiAddr string) bool {
	// check consumer sidecar api health
	healthURL := &types.URI{URL: url.URL{Scheme: "http",
		Host: apiAddr,
		Path: "health"}}
	ok, _ := common.APIHealthCheck(healthURL, 2*time.Second)
	return ok
}

func updateHTTPPublishers(nodeIP, nodeName string, addr ...string) {
	for _, s := range addr {
		if s == "" {
			continue
		}
		publisherServiceName := common.SanitizeTransportHost(s, nodeIP, nodeName)
		PublisherHealthOk = publisherHealthCheck(publisherServiceName)
		eventPublishers[publisherServiceName] = PublisherHealthOk
		if PublisherHealthOk {
			log.Info("healthy publisher; subscribing to events")
			subscribeToEvents()
		}
		log.Infof("publisher endpoint updated from %s to %s healthStatusOk %t", s, publisherServiceName, PublisherHealthOk)
	}
}
