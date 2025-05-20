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
	"net"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"

	ce "github.com/cloudevents/sdk-go/v2/event"
	"github.com/redhat-cne/cloud-event-proxy/pkg/common"
	"github.com/redhat-cne/cloud-event-proxy/pkg/restclient"
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
	// HealthCheckInterval interval in seconds to ping publisher API health status
	HealthCheckInterval = 30
	// HealthCheckRetryInterval for each ping to publisher API health status, retry at this interval in seconds
	HealthCheckRetryInterval = 2
	// eventPullInterval interval in seconds for pulling events
	eventPullInterval = 60
	defaultAPIAddr    = "localhost:8089"
)

var (
	apiAddr            string
	apiPath            = "/api/ocloudNotifications/v2/"
	apiVersion         string
	localAPIAddr       = "localhost:8989"
	resourcePrefix     = "/cluster/node/%s%s"
	mockResource       = "/mock"
	mockResourceKey    = "mock"
	httpEventPublisher string
	// map to track if subscriptions were created successfully for each publisher service
	subscribed = make(map[string]bool)
	subs       []*pubsub.PubSub
	// Git commit of current build set at build time
	GitCommit = "Undefined"
)

func main() {
	fmt.Printf("Git commit: %s\n", GitCommit)
	common.InitLogger()
	flag.StringVar(&localAPIAddr, "local-api-addr", "localhost:8989", "The address the local api binds to .")
	flag.StringVar(&apiPath, "api-path", "/api/ocloudNotifications/v2/", "The rest api path.")
	flag.StringVar(&apiAddr, "api-addr", "", "Obsolete. The publisher API address is retrieved from httpEventPublisher flag")
	flag.StringVar(&apiVersion, "api-version", "", "Obsolete. The version of event REST API is set to 2.0.")
	flag.StringVar(&httpEventPublisher, "http-event-publishers", "", "Comma separated address of the publishers available.")
	flag.Parse()

	if apiAddr != "" {
		log.Warn("api-addr flag is obsolete. Publisher API address is retrieved from httpEventPublisher flag")
	}

	if apiVersion != "" {
		log.Warn("api-version flag is obsolete. Event REST API version is set to 2.0")
	}

	nodeIP := os.Getenv("NODE_IP")
	nodeName := os.Getenv("NODE_NAME")
	if nodeName == "" {
		log.Error("cannot find NODE_NAME environment variable,setting to default `mock` node")
		nodeName = mockResourceKey
	}

	enableEventPull := common.GetBoolEnv("ENABLE_STATUS_CHECK")

	consumerTypeEnv := os.Getenv("CONSUMER_TYPE")
	if consumerTypeEnv == "" {
		consumerType = MOCK
	} else {
		consumerType = ConsumerTypeEnum(consumerTypeEnv)
	}

	// get the first publisher and replace the apiAddr
	apiAddr = getFirstHTTPPublishers(nodeIP, nodeName, httpEventPublisher)
	if apiAddr == "" {
		log.Errorf("cannot find publisher, setting to default address %s", defaultAPIAddr)
		apiAddr = defaultAPIAddr
	}
	log.Infof("updated apiAddr=%s, apiPath=%s", apiAddr, apiPath)

	subscribeTo := initSubscribers(consumerType)
	var wg sync.WaitGroup
	wg.Add(1)
	go server() // spin local api
	time.Sleep(5 * time.Second)

	for _, resource := range subscribeTo {
		subs = append(subs, &pubsub.PubSub{
			ID:       getUUID(fmt.Sprintf(resourcePrefix, nodeName, resource)).String(),
			Resource: fmt.Sprintf(resourcePrefix, nodeName, resource),
		})
	}

	updateHTTPPublishers(nodeIP, nodeName, httpEventPublisher)
	subscribeToEvents()

	// ping publisher API health status
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range time.Tick(HealthCheckInterval * time.Second) {
			subscribeToEvents()
		}
	}()

	// if enabled, pull events periodically
	wg.Add(1)
	go func() {
		defer wg.Done()
		if enableEventPull {
			time.Sleep(5 * time.Second)
			pullEvents()
			for range time.Tick(eventPullInterval * time.Second) {
				pullEvents()
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
		Path: apiPath + "subscriptions"}}
	rc := restclient.New()
	rc.Delete(deleteURL)
	for p := range subscribed {
		subscribed[p] = false
	}
}

// checkSubscriptions gets all subscriptions
// and returns true if there are any subscriptions
func checkSubscriptions() bool {
	url := &types.URI{URL: url.URL{Scheme: "http",
		Host: apiAddr,
		Path: apiPath + "subscriptions"}}
	rc := restclient.New()

	var subs = []pubsub.PubSub{}
	var subB []byte
	status, subB, err := rc.Get(url)
	if status != http.StatusOK {
		log.Errorf("failed to list subscriptions, status %d", status)
		if err != nil {
			log.Error(err)
		}
		return false
	}
	if err = json.Unmarshal(subB, &subs); err != nil {
		log.Errorf("failed to unmarshal subscriptions, %v", err)
		return false
	}
	return len(subs) > 0
}

func subscribeToEvents() {
RETRY:
	for p, subOK := range subscribed {
		healthOK := publisherHealthCheck(p)
		if subOK {
			if !healthOK {
				deleteAllSubscriptions()
				log.Info("delete all subscriptions due to publisher health failure")
				goto RETRY
			}
			if !checkSubscriptions() {
				// re-subscribe in case of non-recoverable error at server side, for example
				// lost of persist data in configmap
				log.Infof("subscription not found for %s, resubscribe", p)
				subscribed[p] = false
				goto RETRY
			}
			continue
		}
		if !healthOK {
			continue
		}
		allSubscribed := true
		for _, s := range subs {
			su, status, e := createSubscription(s.Resource)
			if status == http.StatusConflict {
				// if subscription for the resource already exist, current O-RAN REST API
				// does not return subscription info. In order to find out which subscription
				// has this resource we have to retrieve all subscriptions and search against
				// the resource to find out the subscription ID.
				// Delete all subscriptions and resubscribe to keep the example code simple.
				log.Warnf("%s", e)
				log.Infof("Delete and resubscribe")
				deleteAllSubscriptions()
				goto RETRY
			} else if e != nil {
				log.Errorf("failed to create subscription: %v", e)
				allSubscribed = false
			} else {
				log.Infof("created subscription: %s", su.String())
				s.URILocation = su.URILocation
				s.ID = su.ID
			}
		}
		if allSubscribed {
			log.Info("all subscriptions created successfully")
			subscribed[p] = true
		}
	}
}

func createSubscription(resourceAddress string) (sub pubsub.PubSub, status int, err error) {
	subURL := &types.URI{URL: url.URL{Scheme: "http",
		Host: apiAddr,
		Path: apiPath + "subscriptions"}}
	endpointURL := &types.URI{URL: url.URL{Scheme: "http",
		Host: localAPIAddr,
		Path: "event"}}

	sub = v1pubsub.NewPubSub(endpointURL, resourceAddress)
	var subB []byte

	if subB, err = json.Marshal(&sub); err == nil {
		rc := restclient.New()
		status, subB = rc.PostWithReturn(subURL, subB)
		if status == http.StatusCreated {
			err = json.Unmarshal(subB, &sub)
		} else {
			err = fmt.Errorf("api at %s returned status %d for %s", subURL, status, resourceAddress)
			if status == http.StatusConflict {
				return
			}
		}
	} else {
		err = fmt.Errorf("failed to marshal subscription for %s", resourceAddress)
	}
	return
}

// getCurrentState get event state for the resource
func getCurrentState(resource string) error {
	//create publisher
	url := &types.URI{URL: url.URL{Scheme: "http",
		Host: apiAddr,
		Path: fmt.Sprintf("%s%s", apiPath, fmt.Sprintf("%s/CurrentState", resource[1:]))}}
	rc := restclient.New()
	status, cloudEvent, err := rc.Get(url)
	if status != http.StatusOK {
		if err != nil {
			log.Error(err)
		}
		return fmt.Errorf("CurrentState: error %d from url %s", status, url.String())
	} else {
		log.Debugf("Got CurrentState: %s ", string(cloudEvent))
	}
	return nil
}

// Consumer webserver
func server() {
	http.HandleFunc("/event", getEvent)
	http.HandleFunc("/ack/event", ackEvent)

	port := extractPort(localAPIAddr)
	log.Infof("Starting local API listening to %s", port)
	server := &http.Server{
		Addr:              port,
		ReadHeaderTimeout: 3 * time.Second,
	}

	err := server.ListenAndServe()
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
		log.Infof("received event %s", string(bodyBytes))
		if err = processEvent(bodyBytes); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
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

func processEvent(data []byte) error {
	var e ce.Event
	if err := json.Unmarshal(data, &e); err != nil {
		log.Errorf("failed to unmarshal event, %v", err)
		return err
	}
	latency := time.Now().UnixMilli() - e.Context.GetTime().UnixMilli()
	// set log to Info level for performance measurement
	log.Infof("Latency for the event: %v ms", latency)
	log.Infof("Event: %s", e.String())
	return nil
}

func initSubscribers(cType ConsumerTypeEnum) map[string]string {
	subscribeTo := make(map[string]string)
	switch cType {
	case PTP:
		subscribeTo[string(ptpEvent.OsClockSyncStateChange)] = string(ptpEvent.OsClockSyncState)
		subscribeTo[string(ptpEvent.PtpClockClassChange)] = string(ptpEvent.PtpClockClass)
		subscribeTo[string(ptpEvent.PtpStateChange)] = string(ptpEvent.PtpLockState)
		subscribeTo[string(ptpEvent.GnssStateChange)] = string(ptpEvent.GnssSyncStatus)
		subscribeTo[string(ptpEvent.SyncStateChange)] = string(ptpEvent.SyncStatusState)
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

func pullEvents() {
	for _, subOK := range subscribed {
		if !subOK {
			log.Info("skipping getCurrentState() since publisher not available")
			return
		}
	}
	for _, s := range subs {
		if err := getCurrentState(s.Resource); err != nil {
			log.Error(err)
		}
	}
}

func publisherHealthCheck(apiAddr string) bool {
	healthURL := &types.URI{URL: url.URL{Scheme: "http",
		Host: apiAddr,
		Path: apiPath + "health"}}
	ok, _ := common.APIHealthCheck(healthURL, HealthCheckRetryInterval*time.Second)
	return ok
}

func updateHTTPPublishers(nodeIP, nodeName string, addr ...string) {
	for _, s := range addr {
		if s == "" {
			continue
		}
		publisherServiceName := common.SanitizeTransportHost(s, nodeIP, nodeName)
		subscribed[publisherServiceName] = false
		log.Infof("publisher endpoint updated from %s to %s", s, publisherServiceName)
	}
}

func getFirstHTTPPublishers(nodeIP, nodeName string, addr ...string) string {
	for _, s := range addr {
		if s == "" {
			continue
		}
		return common.SanitizeTransportHost(s, nodeIP, nodeName)
	}
	return ""
}

// extract port from host:port and make sure to support ipv6
func extractPort(hostPort string) string {
	_, port, err := net.SplitHostPort(hostPort)
	if err != nil {
		return ""
	}
	return ":" + port
}
