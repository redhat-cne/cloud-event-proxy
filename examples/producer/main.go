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

// Package main ...
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/redhat-cne/cloud-event-proxy/pkg/common"
	"github.com/redhat-cne/cloud-event-proxy/pkg/restclient"
	cneevent "github.com/redhat-cne/sdk-go/pkg/event"
	"github.com/redhat-cne/sdk-go/pkg/event/ptp"
	"github.com/redhat-cne/sdk-go/pkg/pubsub"
	"github.com/redhat-cne/sdk-go/pkg/types"
	v1event "github.com/redhat-cne/sdk-go/v1/event"
	v1pubsub "github.com/redhat-cne/sdk-go/v1/pubsub"
	log "github.com/sirupsen/logrus"
)

var (
	apiAddr                string
	apiPath                string
	localAPIAddr           string
	resourceAddressSports  = "/news-service/sports"
	resourceAddressFinance = "/news-service/finance"
)

func main() {
	common.InitLogger()
	flag.StringVar(&localAPIAddr, "local-api-addr", "localhost:9088", "The address the local api binds to .")
	flag.StringVar(&apiPath, "api-path", "/api/ocloudNotifications/v1/", "The rest api path.")
	flag.StringVar(&apiAddr, "api-addr", "localhost:8089", "The address the framework api endpoint binds to.")
	flag.Parse()

	var wg sync.WaitGroup
	wg.Add(1)
	go server()

	pubs := []*pubsub.PubSub{{
		Resource: resourceAddressSports,
	}, {
		Resource: resourceAddressFinance,
	}}
	healthURL := &types.URI{URL: url.URL{Scheme: "http",
		Host: apiAddr,
		Path: fmt.Sprintf("%s%s", apiPath, "health")}}
RETRY:
	if ok, _ := common.APIHealthCheck(healthURL, 2*time.Second); !ok {
		goto RETRY
	}
	for _, pub := range pubs {
		result := createPublisher(pub.Resource)
		if result != nil {
			if err := json.Unmarshal(result, pub); err != nil {
				log.Errorf("failed to create a publisher object %v\n", err)
			}
		}
		log.Infof("created publisher : %s\n", pub.String())
	}

	// create events periodically
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range time.Tick(5 * time.Second) {
			// create an event
			for _, pub := range pubs {
				event := v1event.CloudNativeEvent()
				event.SetID(pub.ID)
				event.Type = string(ptp.PtpStateChange)
				event.SetTime(types.Timestamp{Time: time.Now().UTC()}.Time)
				event.SetDataContentType(cneevent.ApplicationJSON)
				data := cneevent.Data{
					Version: "v1",
					Values: []cneevent.DataValue{{
						Resource:  pub.Resource,
						DataType:  cneevent.NOTIFICATION,
						ValueType: cneevent.ENUMERATION,
						Value:     ptp.ACQUIRING_SYNC,
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
	publisherURL := &types.URI{URL: url.URL{Scheme: "http",
		Host: apiAddr,
		Path: fmt.Sprintf("%s%s", apiPath, "publishers")}}
	endpointURL := &types.URI{URL: url.URL{Scheme: "http",
		Host: localAPIAddr,
		Path: "ack/event"}}
	log.Infof("publisher endpoint %s", endpointURL.String())
	pub := v1pubsub.NewPubSub(endpointURL, resourceAddress)
	if b, err := json.Marshal(&pub); err == nil {
		var status int
		rc := restclient.New()
		if status, b = rc.PostWithReturn(publisherURL, b); status == http.StatusCreated {
			log.Infof("create publisher status %s", string(b))
			return b
		}
		log.Errorf("publisher create returned error %s", string(b))
	} else {
		log.Errorf("failed to create publisher ")
	}
	return nil
}

func publishEvent(e cneevent.Event) {
	//create publisher
	url := &types.URI{URL: url.URL{Scheme: "http",
		Host: apiAddr,
		Path: fmt.Sprintf("%s%s", apiPath, "create/event")}}
	rc := restclient.New()
	err := rc.PostEvent(url, e)
	if err != nil {
		log.Errorf("error publishing events %v to url %s", err, url.String())
	} else {
		log.Debugf("published event %s", e.ID)
	}
}

func server() {
	http.HandleFunc("/ack/event", ackEvent)
	http.ListenAndServe(localAPIAddr, nil)
}

func ackEvent(w http.ResponseWriter, req *http.Request) {
	defer req.Body.Close()
	bodyBytes, err := io.ReadAll(req.Body)
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
