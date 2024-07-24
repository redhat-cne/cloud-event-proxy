// Copyright 2024 The Cloud Native Events Authors
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
	"io"
	"strings"
	"time"

	"github.com/redhat-cne/rest-api/pkg/localmetrics"
	"github.com/redhat-cne/rest-api/pkg/restclient"
	"github.com/redhat-cne/sdk-go/pkg/channel"
	cne "github.com/redhat-cne/sdk-go/pkg/event"
	"github.com/redhat-cne/sdk-go/pkg/pubsub"
	"github.com/redhat-cne/sdk-go/pkg/subscriber"
	"github.com/redhat-cne/sdk-go/pkg/types"
	"github.com/redhat-cne/sdk-go/v1/event"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	ce "github.com/cloudevents/sdk-go/v2/event"

	"net/http"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	log "github.com/sirupsen/logrus"
)

// createSubscription create subscription and send it to a channel that is shared by middleware to process
// Creates a new subscription .
// If subscription exists with same resource then existing subscription is returned .
// responses:
//
//	201: repoResp
//	400: badReq
//	204: noContent
func (s *Server) createSubscription(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	sub := pubsub.PubSub{}
	sub.SetVersion(API_VERSION)
	if err = json.Unmarshal(bodyBytes, &sub); err != nil {
		respondWithStatusCode(w, http.StatusBadRequest, fmt.Sprintf("marshalling error %v", err))
		localmetrics.UpdateSubscriptionCount(localmetrics.FAILCREATE, 1)
		return
	}
	endPointURI := sub.GetEndpointURI()
	if endPointURI == "" {
		respondWithStatusCode(w, http.StatusBadRequest, "EndpointURI can not be empty")
		localmetrics.UpdateSubscriptionCount(localmetrics.FAILCREATE, 1)
		return
	}
	clientIDs := s.subscriberAPI.GetClientIDByResource(sub.GetResource())
	if len(clientIDs) != 0 {
		respondWithStatusCode(w, http.StatusConflict,
			fmt.Sprintf("subscription (clientID: %s) with same resource already exists, skipping creation",
				clientIDs[0]))
		return
	}

	id := uuid.New().String()
	sub.SetID(id)
	sub.SetURILocation(fmt.Sprintf("http://%s:%d%s%s/%s", s.apiHost, s.port, s.apiPath, "subscriptions", sub.ID)) //nolint:errcheck
	addr := sub.GetResource()

	// this is placeholder not sending back to report
	out := channel.DataChan{
		Address: addr,
		// ClientID is not used
		ClientID: uuid.New(),
		Status:   channel.NEW,
		Type:     channel.STATUS, // could be new event of new subscriber (sender)
	}

	e, _ := out.CreateCloudEvents(CURRENTSTATE)
	e.SetSource(addr)

	if s.statusReceiveOverrideFn == nil {
		respondWithStatusCode(w, http.StatusNotFound, "onReceive function not defined")
		localmetrics.UpdateSubscriptionCount(localmetrics.FAILCREATE, 1)
		return
	}

	if statusErr := s.statusReceiveOverrideFn(*e, &out); statusErr != nil {
		respondWithStatusCode(w, http.StatusNotFound, statusErr.Error())
		localmetrics.UpdateSubscriptionCount(localmetrics.FAILCREATE, 1)
		return
	}

	if out.Data == nil {
		respondWithStatusCode(w, http.StatusNotFound, fmt.Sprintf("event not found for %s", addr))
		localmetrics.UpdateSubscriptionCount(localmetrics.FAILCREATE, 1)
		return
	}

	restClient := restclient.New()
	// make sure event ID is unique
	out.Data.SetID(uuid.New().String())
	status, err := restClient.PostCloudEvent(sub.EndPointURI, *out.Data)
	if err != nil {
		respondWithStatusCode(w, http.StatusBadRequest,
			fmt.Sprintf("failed to POST initial notification: %v, subscription wont be created", err))
		localmetrics.UpdateSubscriptionCount(localmetrics.FAILCREATE, 1)
		return
	}
	if status != http.StatusNoContent {
		respondWithStatusCode(w, http.StatusBadRequest,
			fmt.Sprintf("initial notification returned wrong status code %d", status))
		localmetrics.UpdateSubscriptionCount(localmetrics.FAILCREATE, 1)
		return
	}

	log.Infof("initial notification is successful for subscription %s", addr)
	// create unique clientId for each subscription based on endPointURI
	subs := subscriber.New(s.getClientIDFromURI(endPointURI))
	_ = subs.SetEndPointURI(endPointURI)

	subs.AddSubscription(sub)
	subs.Action = channel.NEW
	cevent, _ := subs.CreateCloudEvents()
	cevent.SetSource(addr)

	// send this to dataOut channel to update configMap
	out = channel.DataChan{
		Address: addr,
		Data:    cevent,
		Status:  channel.NEW,
		Type:    channel.SUBSCRIBER,
	}

	var updatedObj *subscriber.Subscriber
	// writes a file <clientID>.json that has the same content as configMap.
	// configMap was created later as a way to persist the data.
	if updatedObj, err = s.subscriberAPI.CreateSubscription(subs.ClientID, *subs); err != nil {
		out.Status = channel.FAILED
		respondWithStatusCode(w, http.StatusNotFound,
			fmt.Sprintf("failed creating subscription for %s, %v", subs.ClientID.String(), err))
		localmetrics.UpdateSubscriptionCount(localmetrics.FAILCREATE, 1)
	} else {
		out.Status = channel.SUCCESS
		_ = out.Data.SetData("", updatedObj)
		log.Infof("subscription created successfully.")
		localmetrics.UpdateSubscriptionCount(localmetrics.ACTIVE, 1)
		respondWithJSON(w, http.StatusCreated, sub)
	}

	s.dataOut <- &out
}

// createPublisher create publisher and send it to a channel that is shared by middleware to process
// Creates a new publisher .
// If publisher exists with same resource then existing publisher is returned .
// responses:
//
//	201: repoResp
//	400: badReq
//	204: noContent
func (s *Server) createPublisher(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var response *http.Response
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	pub := pubsub.PubSub{}
	pub.SetVersion(API_VERSION)
	if err = json.Unmarshal(bodyBytes, &pub); err != nil {
		localmetrics.UpdatePublisherCount(localmetrics.FAILCREATE, 1)
		respondWithError(w, "marshalling error")
		return
	}
	if pub.GetEndpointURI() != "" {
		response, err = s.HTTPClient.Post(pub.GetEndpointURI(), cloudevents.ApplicationJSON, nil)
		if err != nil {
			log.Infof("there was an error validating the publisher endpointurl %v, publisher won't be created.", err)
			localmetrics.UpdatePublisherCount(localmetrics.FAILCREATE, 1)
			respondWithError(w, err.Error())
			return
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusNoContent {
			log.Infof("there was an error validating endpointurl %s returned status code %d", pub.GetEndpointURI(), response.StatusCode)
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
		log.Infof("error creating publisher %v", err)
		localmetrics.UpdatePublisherCount(localmetrics.FAILCREATE, 1)
		respondWithError(w, err.Error())
		return
	}
	log.Infof("publisher created successfully.")
	// go ahead and create QDR to this address
	s.sendOut(channel.PUBLISHER, &newPub)
	localmetrics.UpdatePublisherCount(localmetrics.ACTIVE, 1)
	respondWithJSON(w, http.StatusCreated, newPub)
}

func (s *Server) sendOut(eType channel.Type, sub *pubsub.PubSub) {
	// go ahead and create QDR to this address
	s.dataOut <- &channel.DataChan{
		ID:      sub.GetID(),
		Address: sub.GetResource(),
		Data:    &ce.Event{},
		Type:    eType,
		Status:  channel.NEW,
	}
}

func (s *Server) sendOutToDelete(eType channel.Type, sub *pubsub.PubSub) {
	// go ahead and create QDR to this address
	s.dataOut <- &channel.DataChan{
		ID:      sub.GetID(),
		Address: sub.GetResource(),
		Data:    &ce.Event{},
		Type:    eType,
		Status:  channel.DELETE,
	}
}

func (s *Server) getSubscriptionByID(w http.ResponseWriter, r *http.Request) {
	queries := mux.Vars(r)
	subscriptionID, ok := queries["subscriptionid"]
	if !ok {
		respondWithStatusCode(w, http.StatusNotFound, "")
		return
	}

	for _, c := range s.subscriberAPI.GetClientIDBySubID(subscriptionID) {
		sub, err := s.subscriberAPI.GetSubscription(c, subscriptionID)
		if err == nil {
			respondWithJSON(w, http.StatusOK, sub)
			return
		}
	}
	respondWithStatusCode(w, http.StatusNotFound, "")
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

func (s *Server) getSubscriptions(w http.ResponseWriter, _ *http.Request) {
	b, err := s.subscriberAPI.GetSubscriptions()
	if err != nil {
		log.Errorf("error loading subscriber data %v", err)
		respondWithError(w, "error loading subscriber data")
		return
	}
	respondWithByte(w, http.StatusOK, b)
}

func (s *Server) getPublishers(w http.ResponseWriter, _ *http.Request) {
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

	clientIDs := s.subscriberAPI.GetClientIDBySubID(subscriptionID)
	if len(clientIDs) == 0 {
		respondWithStatusCode(w, http.StatusNotFound, "")
		return
	}

	for _, c := range clientIDs {
		if err := s.subscriberAPI.DeleteSubscription(c, subscriptionID); err != nil {
			localmetrics.UpdateSubscriptionCount(localmetrics.FAILDELETE, 1)
			respondWithStatusCode(w, http.StatusNotFound, err.Error())
			return
		}
	}

	// update configMap
	for _, subs := range s.subscriberAPI.SubscriberStore.Store {
		cevent, _ := subs.CreateCloudEvents()
		out := channel.DataChan{
			Data:   cevent,
			Status: channel.SUCCESS,
			Type:   channel.SUBSCRIBER,
		}
		s.dataOut <- &out
	}

	localmetrics.UpdateSubscriptionCount(localmetrics.ACTIVE, -1)
	respondWithStatusCode(w, http.StatusNoContent, "")
}

func (s *Server) deleteAllSubscriptions(w http.ResponseWriter, _ *http.Request) {
	size := len(s.pubSubAPI.GetSubscriptions())
	if err := s.pubSubAPI.DeleteAllSubscriptions(); err != nil {
		respondWithError(w, err.Error())
		return
	}
	//update metrics
	if size > 0 {
		localmetrics.UpdateSubscriptionCount(localmetrics.ACTIVE, -(size))
	}
	// go ahead and create QDR to this address
	s.sendOutToDelete(channel.SUBSCRIBER, &pubsub.PubSub{ID: "", Resource: "delete-all-subscriptions"})
	respondWithMessage(w, http.StatusOK, "deleted all subscriptions")
}

func (s *Server) deleteAllPublishers(w http.ResponseWriter, _ *http.Request) {
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
	bodyBytes, err := io.ReadAll(r.Body)
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
	ceEvent, err := cneEvent.NewCloudEventV2()
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
	if resourceAddress == "" {
		respondWithError(w, "resourceAddress can not be empty")
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
		respondWithStatusCode(w, http.StatusNotFound, "no subscription data available")
		return
	}

	if sub == nil {
		respondWithStatusCode(w, http.StatusNotFound, fmt.Sprintf("subscription not found for %s", resourceAddress))
		return
	}

	if !strings.HasPrefix(resourceAddress, "/") {
		resourceAddress = fmt.Sprintf("/%s", resourceAddress)
	}
	// this is placeholder not sending back to report
	out := channel.DataChan{
		Address: resourceAddress,
		// ClientID is not used
		ClientID: uuid.New(),
		Status:   channel.NEW,
		Type:     channel.STATUS, // could be new event of new subscriber (sender)
	}

	e, _ := out.CreateCloudEvents(CURRENTSTATE)
	// statusReceiveOverrideFn must return value for
	if s.statusReceiveOverrideFn != nil {
		if statusErr := s.statusReceiveOverrideFn(*e, &out); statusErr != nil {
			respondWithStatusCode(w, http.StatusNotFound, statusErr.Error())
		} else if out.Data != nil {
			respondWithJSON(w, http.StatusOK, *out.Data)
		} else {
			respondWithStatusCode(w, http.StatusNotFound, fmt.Sprintf("event not found for %s", resourceAddress))
		}
	} else {
		respondWithStatusCode(w, http.StatusNotFound, "onReceive function not defined")
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
		Version: cne.APISchemaVersion,
	})
	ceEvent, err := cneEvent.NewCloudEventV2()

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
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		respondWithError(w, err.Error())
		return
	}
	cneEvent := event.CloudNativeEvent()
	if err := json.Unmarshal(bodyBytes, &cneEvent); err != nil {
		respondWithError(w, err.Error())
		return
	} // check if publisher is found
	log.Infof("event received %v", cneEvent)
	respondWithMessage(w, http.StatusAccepted, "Event published to log")
}

func (s *Server) getClientIDFromURI(uri string) uuid.UUID {
	return uuid.NewMD5(uuid.NameSpaceURL, []byte(uri))
}

func dummy(w http.ResponseWriter, _ *http.Request) {
	respondWithMessage(w, http.StatusNoContent, "dummy test")
}

func respondWithError(w http.ResponseWriter, message string) {
	respondWithJSON(w, http.StatusBadRequest, map[string]string{"error": message})
}

func respondWithStatusCode(w http.ResponseWriter, code int, message string) {
	if message != "" {
		log.Errorf("%s", message)
		// Response with message if spec were updated to allow message
		// respondWithJSON(w, code, map[string]string{"error": message})
	}
	w.WriteHeader(code)
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
