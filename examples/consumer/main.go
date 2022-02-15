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
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/redhat-cne/cloud-event-proxy/pkg/common"
	"github.com/redhat-cne/cloud-event-proxy/pkg/restclient"
	"github.com/redhat-cne/sdk-go/pkg/event"
	"github.com/redhat-cne/sdk-go/pkg/pubsub"
	"github.com/redhat-cne/sdk-go/pkg/types"
	v1pubsub "github.com/redhat-cne/sdk-go/v1/pubsub"
	log "github.com/sirupsen/logrus"
	"github.com/valyala/fasthttp"
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
	apiAddr      string = "localhost:8089"
	apiPath      string = "/api/cloudNotifications/v1/"
	localAPIAddr string = "localhost:8989"

	resourceAddressMock    string = "/cluster/node/%s/mock"
	resourceAddressPTP     string = "/cluster/node/%s/ptp"
	resourceAddressHwEvent string = "/cluster/node/%s/redfish/event"
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
		nodeName = "mock"
	}

	enableStatusCheck := common.GetBoolEnv("ENABLE_STATUS_CHECK")

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
		Resource: fmt.Sprintf(resourceAddressHwEvent, nodeName),
	}, &pubsub.PubSub{
		Resource: fmt.Sprintf(resourceAddressMock, nodeName),
	}, &pubsub.PubSub{
		Resource: fmt.Sprintf(resourceAddressPTP, nodeName),
	},
	}

	var sub *pubsub.PubSub
	if consumerType == PTP {
		sub = subs[2]
	} else if consumerType == MOCK || consumerType == "" {
		sub = subs[1]
	} else {
		sub = subs[0]
	}

	if enableStatusCheck {
		createPublisherForStatusPing(sub.Resource) // ptp // disable this for testing else you will see context deadline error
	}

	var result []byte

	result = createSubscription(sub.Resource)
	if result != nil {
		if err := json.Unmarshal(result, sub); err != nil {
			log.Errorf("failed to create a subscription object %v\n", err)
		}
	}
	log.Infof("created subscription: %s\n", sub.String())

	// ping  for status every n secs

	if enableStatusCheck {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range time.Tick(StatusCheckInterval * time.Second) {
				pingForStatus(sub.ID)
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

func fastHTTPHandler(ctx *fasthttp.RequestCtx) {
	switch string(ctx.Path()) {
	case "/event":
		getEvent(ctx)
	case "/ack/event":
		ackEvent(ctx)
	default:
		ctx.Error("Unsupported path %s", fasthttp.StatusNotFound)
	}

}

// Consumer webserver
func server() {
	fasthttp.ListenAndServe(localAPIAddr, fastHTTPHandler)
}

func getEvent(ctx *fasthttp.RequestCtx) {
	body := ctx.PostBody()
	if len(body) > 0 {
		processEvent(body)
		log.Debugf("received event %s", string(body))
	} else {
		ctx.SetStatusCode(http.StatusNoContent)
	}
}

func ackEvent(ctx *fasthttp.RequestCtx) {
	body := ctx.PostBody()
	if len(body) > 0 {
		log.Debugf("received ack %s", string(body))
	} else {
		ctx.SetStatusCode(http.StatusNoContent)
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
