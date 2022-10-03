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

package restapi

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/redhat-cne/sdk-go/pkg/types"

	"github.com/redhat-cne/rest-api/pkg/localmetrics"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	ce "github.com/cloudevents/sdk-go/v2/event"

	cne "github.com/redhat-cne/sdk-go/pkg/event"
	"github.com/redhat-cne/sdk-go/pkg/pubsub"

	"github.com/redhat-cne/sdk-go/v1/event"

	"github.com/google/uuid"
	"github.com/gorilla/mux"

	"github.com/redhat-cne/sdk-go/pkg/channel"

	"io/ioutil"
	"log"
	"net/http"
)

const (
	AMQ  = "AMQ"
	HTTP = "HTTP"
)

// createSubscription create subscription and send it to a channel that is shared by middleware to process
// Creates a new subscription .
// If subscription exists with same resource then existing subscription is returned .
// responses:
//  201: repoResp
//  400: badReq
//  204: noContent
func (s *Server) createSubscription(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var response *http.Response
	bodyBytes, err := ioutil.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	sub := pubsub.PubSub{}
	if err = json.Unmarshal(bodyBytes, &sub); err != nil {
		respondWithError(w, "marshalling error")
		localmetrics.UpdateSubscriptionCount(localmetrics.FAILCREATE, 1)
		return
	}
	if sub.GetEndpointURI() != "" {
		response, err = s.HTTPClient.Post(sub.GetEndpointURI(), cloudevents.ApplicationJSON, nil)
		if err != nil {
			log.Printf("there was error validating endpointurl %v, subscription wont be created", err)
			localmetrics.UpdateSubscriptionCount(localmetrics.FAILCREATE, 1)
			respondWithError(w, err.Error())
			return
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusNoContent {
			log.Printf("there was an error validating endpointurl %s returned status code %d", sub.GetEndpointURI(), response.StatusCode)
			respondWithError(w, "return url validation check failed for create subscription.check endpointURI")
			localmetrics.UpdateSubscriptionCount(localmetrics.FAILCREATE, 1)
			return
		}
	}
	// check sub.EndpointURI by get
	sub.SetID(uuid.New().String())
	_ = sub.SetURILocation(fmt.Sprintf("http://localhost:%d%s%s/%s", s.port, s.apiPath, "subscriptions", sub.ID)) //nolint:errcheck

	newSub, err := s.pubSubAPI.CreateSubscription(sub)
	if err != nil {
		log.Printf("error creating subscription %v", err)
		localmetrics.UpdateSubscriptionCount(localmetrics.FAILCREATE, 1)
		respondWithError(w, err.Error())
		return
	}
	log.Printf("subscription created successfully.")
	// go ahead and create QDR to this address
	s.sendOut(channel.SUBSCRIBER, &newSub)
	localmetrics.UpdateSubscriptionCount(localmetrics.ACTIVE, 1)
	respondWithJSON(w, http.StatusCreated, newSub)
}

// createPublisher create publisher and send it to a channel that is shared by middleware to process
// Creates a new publisher .
// If publisher exists with same resource then existing publisher is returned .
// responses:
//  201: repoResp
//  400: badReq
//  204: noContent
func (s *Server) createPublisher(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var response *http.Response
	bodyBytes, err := ioutil.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	pub := pubsub.PubSub{}
	if err = json.Unmarshal(bodyBytes, &pub); err != nil {
		localmetrics.UpdatePublisherCount(localmetrics.FAILCREATE, 1)
		respondWithError(w, "marshalling error")
		return
	}
	if pub.GetEndpointURI() != "" {
		response, err = s.HTTPClient.Post(pub.GetEndpointURI(), cloudevents.ApplicationJSON, nil)
		if err != nil {
			log.Printf("there was an error validating the publisher endpointurl %v, publisher won't be created.", err)
			localmetrics.UpdatePublisherCount(localmetrics.FAILCREATE, 1)
			respondWithError(w, err.Error())
			return
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusNoContent {
			log.Printf("there was an error validating endpointurl %s returned status code %d", pub.GetEndpointURI(), response.StatusCode)
			localmetrics.UpdatePublisherCount(localmetrics.FAILCREATE, 1)
			respondWithError(w, "return url validation check failed for create publisher,check endpointURI")
			return
		}
	}

	// check pub.EndpointURI by get
	pub.SetID(uuid.New().String())
	_ = pub.SetURILocation(fmt.Sprintf("http://localhost:%d%s%s/%s", s.port, s.apiPath, "publishers", pub.ID)) //nolint:errcheck
	newPub, err := s.pubSubAPI.CreatePublisher(pub)
	if err != nil {
		log.Printf("error creating publisher %v", err)
		localmetrics.UpdatePublisherCount(localmetrics.FAILCREATE, 1)
		respondWithError(w, err.Error())
		return
	}
	log.Printf("publisher created successfully.")
	// go ahead and create QDR to this address
	s.sendOut(channel.PUBLISHER, &newPub)
	localmetrics.UpdatePublisherCount(localmetrics.ACTIVE, 1)
	respondWithJSON(w, http.StatusCreated, newPub)
}

func (s *Server) sendOut(eType channel.Type, sub *pubsub.PubSub) {
	// go ahead and create QDR to this address
	s.dataOut <- &channel.DataChan{
		Address: sub.GetResource(),
		Data:    &ce.Event{},
		Type:    eType,
		Status:  channel.NEW,
	}
}

func (s *Server) getSubscriptionByID(w http.ResponseWriter, r *http.Request) {
	queries := mux.Vars(r)
	subscriptionID, ok := queries["subscriptionid"]
	if !ok {
		respondWithError(w, "subscription not found")
		return
	}
	sub, err := s.pubSubAPI.GetSubscription(subscriptionID)
	if err != nil {
		respondWithError(w, "subscription not found")
		return
	}
	respondWithJSON(w, http.StatusOK, sub)
}

func (s *Server) getPublisherByID(w http.ResponseWriter, r *http.Request) {
	queries := mux.Vars(r)
	publisherID, ok := queries["publisherid"]
	if !ok {
		respondWithError(w, "publisher parameter is required")
		return
	}
	pub, err := s.pubSubAPI.GetPublisher(publisherID)
	if err != nil {
		respondWithError(w, "publisher not found")
		return
	}
	respondWithJSON(w, http.StatusOK, pub)
}
func (s *Server) getSubscriptions(w http.ResponseWriter, r *http.Request) {
	b, err := s.pubSubAPI.GetSubscriptionsFromFile()
	if err != nil {
		respondWithError(w, "error loading subscriber data")
		return
	}
	respondWithByte(w, http.StatusOK, b)
}

func (s *Server) getPublishers(w http.ResponseWriter, r *http.Request) {
	b, err := s.pubSubAPI.GetPublishersFromFile()
	if err != nil {
		respondWithError(w, "error loading publishers data")
		return
	}
	respondWithByte(w, http.StatusOK, b)
}

func (s *Server) deletePublisher(w http.ResponseWriter, r *http.Request) {
	queries := mux.Vars(r)
	publisherID, ok := queries["publisherid"]
	if !ok {
		respondWithError(w, "publisherid param is missing")
		return
	}

	if err := s.pubSubAPI.DeletePublisher(publisherID); err != nil {
		localmetrics.UpdatePublisherCount(localmetrics.FAILDELETE, 1)
		respondWithError(w, err.Error())
		return
	}

	localmetrics.UpdatePublisherCount(localmetrics.ACTIVE, -1)
	respondWithMessage(w, http.StatusOK, "OK")
}

func (s *Server) deleteSubscription(w http.ResponseWriter, r *http.Request) {
	queries := mux.Vars(r)
	subscriptionID, ok := queries["subscriptionid"]
	if !ok {
		respondWithError(w, "subscriptionid param is missing")
		return
	}

	if err := s.pubSubAPI.DeleteSubscription(subscriptionID); err != nil {
		localmetrics.UpdateSubscriptionCount(localmetrics.FAILDELETE, 1)
		respondWithError(w, err.Error())
		return
	}

	localmetrics.UpdateSubscriptionCount(localmetrics.ACTIVE, -1)
	respondWithMessage(w, http.StatusOK, "OK")
}

func (s *Server) deleteAllSubscriptions(w http.ResponseWriter, r *http.Request) {
	size := len(s.pubSubAPI.GetSubscriptions())
	if err := s.pubSubAPI.DeleteAllSubscriptions(); err != nil {
		respondWithError(w, err.Error())
		return
	}
	//update metrics
	if size > 0 {
		localmetrics.UpdateSubscriptionCount(localmetrics.ACTIVE, -(size))
	}
	respondWithMessage(w, http.StatusOK, "deleted all subscriptions")
}

func (s *Server) deleteAllPublishers(w http.ResponseWriter, r *http.Request) {
	size := len(s.pubSubAPI.GetPublishers())

	if err := s.pubSubAPI.DeleteAllPublishers(); err != nil {
		respondWithError(w, err.Error())
		return
	}
	//update metrics
	if size > 0 {
		localmetrics.UpdatePublisherCount(localmetrics.ACTIVE, -(size))
	}
	respondWithMessage(w, http.StatusOK, "deleted all publishers")
}

// publishEvent gets cloud native events and converts it to cloud event and publishes to a transport to send
// it to the consumer
func (s *Server) publishEvent(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	bodyBytes, err := ioutil.ReadAll(r.Body)
	if err != nil {
		respondWithError(w, err.Error())
		return
	}
	cneEvent := event.CloudNativeEvent()
	if err = json.Unmarshal(bodyBytes, &cneEvent); err != nil {
		respondWithError(w, err.Error())
		return
	} // check if publisher is found
	pub, err := s.pubSubAPI.GetPublisher(cneEvent.ID)
	if err != nil {
		localmetrics.UpdateEventPublishedCount(cneEvent.ID, localmetrics.FAIL, 1)
		respondWithError(w, fmt.Sprintf("no publisher data for id %s found to publish event for", cneEvent.ID))
		return
	}
	ceEvent, err := cneEvent.NewCloudEvent(&pub)
	if err != nil {
		localmetrics.UpdateEventPublishedCount(pub.Resource, localmetrics.FAIL, 1)
		respondWithError(w, err.Error())
	} else {
		s.dataOut <- &channel.DataChan{
			Type:    channel.EVENT,
			Data:    ceEvent,
			Address: pub.GetResource(),
		}
		localmetrics.UpdateEventPublishedCount(pub.Resource, localmetrics.SUCCESS, 1)
		respondWithMessage(w, http.StatusAccepted, "Event sent")
	}
}

// getCurrentState get current status of the  events that are subscribed to
func (s *Server) getCurrentState(w http.ResponseWriter, r *http.Request) {
	queries := mux.Vars(r)
	resourceAddress, ok := queries["resourceAddress"]
	if !ok {
		respondWithError(w, "resourceAddress parameter not found")
		return
	}

	//identify publisher or subscriber is asking for status
	var sub *pubsub.PubSub
	if len(s.pubSubAPI.GetSubscriptions()) > 0 {
		for _, subscriptions := range s.pubSubAPI.GetSubscriptions() {
			if strings.Contains(subscriptions.GetResource(), resourceAddress) {
				sub = subscriptions
				break
			}
		}
	} else if len(s.pubSubAPI.GetPublishers()) > 0 {
		for _, publishers := range s.pubSubAPI.GetPublishers() {
			if strings.Contains(publishers.GetResource(), resourceAddress) {
				sub = publishers
				break
			}
		}
	} else {
		respondWithError(w, "subscription not found")
		return
	}

	if sub == nil {
		respondWithError(w, "subscription not found")
		return
	}
	cneEvent := event.CloudNativeEvent()
	cneEvent.SetID(sub.ID)
	cneEvent.Type = channel.STATUS.String()
	cneEvent.SetTime(types.Timestamp{Time: time.Now().UTC()}.Time)
	cneEvent.SetDataContentType(cloudevents.ApplicationJSON)
	cneEvent.SetData(cne.Data{
		Version: "v1",
	})
	ceEvent, err := cneEvent.NewCloudEvent(sub)

	if err != nil {
		respondWithError(w, err.Error())
	} else {
		// for http you send to the protocol address
		statusChannel := make(chan *channel.StatusChan, 1)
		if s.transportType == HTTP {
			s.dataOut <- &channel.DataChan{
				Type:       channel.STATUS,
				Data:       ceEvent,
				Address:    sub.GetResource(),
				StatusChan: statusChannel,
			}
			select {
			case d := <-statusChannel:
				if d.Data == nil {
					respondWithError(w, "event not found")
				} else {
					respondWithJSON(w, http.StatusOK, *d.Data)
				}
			case <-time.After(5 * time.Second):
				close(statusChannel)
				respondWithError(w, "time Out waiting for status")
			}
		} else {
			respondWithError(w, "Non HTTP protocol not supported")
		}
	}
}

// pingForSubscribedEventStatus sends ping to the listening address in the producer to fire all status as events
func (s *Server) pingForSubscribedEventStatus(w http.ResponseWriter, r *http.Request) {
	queries := mux.Vars(r)
	subscriptionID, ok := queries["subscriptionid"]
	if !ok {
		respondWithError(w, "subscription parameter not found")
		return
	}
	sub, err := s.pubSubAPI.GetSubscription(subscriptionID)
	if err != nil {
		respondWithError(w, "subscription not found")
		return
	}
	cneEvent := event.CloudNativeEvent()
	cneEvent.SetID(sub.ID)
	cneEvent.Type = "status_check"
	cneEvent.SetTime(types.Timestamp{Time: time.Now().UTC()}.Time)
	cneEvent.SetDataContentType(cloudevents.ApplicationJSON)
	cneEvent.SetData(cne.Data{
		Version: "v1",
	})
	ceEvent, err := cneEvent.NewCloudEvent(&sub)

	if err != nil {
		respondWithError(w, err.Error())
	} else {
		s.dataOut <- &channel.DataChan{
			Type:       channel.STATUS,
			StatusChan: nil,
			Data:       ceEvent,
			Address:    fmt.Sprintf("%s/%s", sub.GetResource(), "status"),
		}
		respondWithMessage(w, http.StatusAccepted, "ping sent")
	}
}

// logEvent gets cloud native events and converts it to cloud native event and writes to log
func (s *Server) logEvent(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	bodyBytes, err := ioutil.ReadAll(r.Body)
	if err != nil {
		respondWithError(w, err.Error())
		return
	}
	cneEvent := event.CloudNativeEvent()
	if err := json.Unmarshal(bodyBytes, &cneEvent); err != nil {
		respondWithError(w, err.Error())
		return
	} // check if publisher is found
	log.Printf("event received %v", cneEvent)
	respondWithMessage(w, http.StatusAccepted, "Event published to log")
}

func dummy(w http.ResponseWriter, r *http.Request) {
	respondWithMessage(w, http.StatusNoContent, "dummy test")
}

func respondWithError(w http.ResponseWriter, message string) {
	respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": message})
}

func respondWithJSON(w http.ResponseWriter, code int, payload interface{}) {
	if response, err := json.Marshal(payload); err == nil {
		w.Header().Set("Content-Type", cloudevents.ApplicationJSON)
		w.WriteHeader(code)
		w.Write(response) //nolint:errcheck
	} else {
		respondWithMessage(w, http.StatusBadRequest, "error parsing event data")
	}
}

func respondWithMessage(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", cloudevents.ApplicationJSON)
	respondWithJSON(w, code, map[string]string{"status": message})
}

func respondWithByte(w http.ResponseWriter, code int, message []byte) {
	w.Header().Set("Content-Type", cloudevents.ApplicationJSON)
	w.WriteHeader(code)
	w.Write(message) //nolint:errcheck
}
