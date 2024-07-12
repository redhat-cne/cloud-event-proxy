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

// Package restapi Pub/Sub Rest API.
//
// Rest API spec .
//
// Terms Of Service:
//
//	Schemes: http, https
//	Host: localhost:8089
//	basePath: /api/ocloudNotifications/v1
//	Version: 1.0.0
//	Contact: Aneesh Puttur<aputtur@redhat.com>
//
//	Consumes:
//	- application/json
//
//	Produces:
//	- application/json
//
// swagger:meta
package restapi

import (
	"fmt"

	"github.com/redhat-cne/sdk-go/pkg/util/wait"

	"sync"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/gorilla/mux"
	"github.com/redhat-cne/sdk-go/pkg/channel"
	"github.com/redhat-cne/sdk-go/pkg/event"
	"github.com/redhat-cne/sdk-go/pkg/pubsub"
	"github.com/redhat-cne/sdk-go/pkg/types"
	pubsubv1 "github.com/redhat-cne/sdk-go/v1/pubsub"
	subscriberApi "github.com/redhat-cne/sdk-go/v1/subscriber"

	"io"
	"net/http"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
)

var once sync.Once

// ServerInstance ... is singleton instance
var ServerInstance *Server
var healthCheckPause = 2 * time.Second

type serverStatus int

const (
	API_VERSION           = "2.0"
	HTTPReadHeaderTimeout = 2 * time.Second
)

const (
	starting = iota
	started
	notReady
	failed
	CURRENTSTATE = "CurrentState"
)

// Server defines rest routes server object
type Server struct {
	port    int
	apiHost string
	apiPath string
	//use dataOut chanel to write to configMap
	dataOut                 chan<- *channel.DataChan
	closeCh                 <-chan struct{}
	HTTPClient              *http.Client
	httpServer              *http.Server
	pubSubAPI               *pubsubv1.API
	subscriberAPI           *subscriberApi.API
	status                  serverStatus
	statusReceiveOverrideFn func(e cloudevents.Event, dataChan *channel.DataChan) error
}

// publisher/subscription data model
// swagger:response pubSubResp
type swaggPubSubRes struct { //nolint:deadcode,unused
	// in:body
	Body pubsub.PubSub
}

// PubSub request model
// swagger:response eventResp
type swaggPubSubEventRes struct { //nolint:deadcode,unused
	// in:body
	Body event.Event
}

// Error Bad Request
// swagger:response badReq
type swaggReqBadRequest struct { //nolint:deadcode,unused
	// in:body
	Body struct {
		// HTTP status code 400 -  Bad Request
		Code int `json:"code" example:"400"`
	}
}

// Error Not Found
// swagger:response notFoundReq
type swaggReqNotFound struct { //nolint:deadcode,unused
	// in:body
	Body struct {
		// HTTP status code 404 -  Not Found
		Code int `json:"code" example:"404"`
	}
}

// Accepted
// swagger:response acceptedReq
type swaggReqAccepted struct { //nolint:deadcode,unused
	// in:body
	Body struct {
		// HTTP status code 202 -  Accepted
		Code int `json:"code" example:"202"`
	}
}

// InitServer is used to supply configurations for rest routes server
func InitServer(port int, apiHost, apiPath, storePath string,
	dataOut chan<- *channel.DataChan, closeCh <-chan struct{},
	onStatusReceiveOverrideFn func(e cloudevents.Event, dataChan *channel.DataChan) error) *Server {
	once.Do(func() {
		ServerInstance = &Server{
			port:    port,
			apiHost: apiHost,
			apiPath: apiPath,
			dataOut: dataOut,
			closeCh: closeCh,
			status:  notReady,
			HTTPClient: &http.Client{
				Transport: &http.Transport{
					MaxIdleConnsPerHost: 20,
				},
				Timeout: 10 * time.Second,
			},
			pubSubAPI:               pubsubv1.GetAPIInstance(storePath),
			subscriberAPI:           subscriberApi.GetAPIInstance(storePath),
			statusReceiveOverrideFn: onStatusReceiveOverrideFn,
		}
	})
	// singleton
	return ServerInstance
}

// EndPointHealthChk checks for rest service health
func (s *Server) EndPointHealthChk() (err error) {
	log.Info("checking for rest service health\n")
	for i := 0; i <= 5; i++ {
		if !s.Ready() {
			time.Sleep(healthCheckPause)
			log.Printf("server status %t", s.Ready())
			continue
		}

		log.Debugf("health check %s%s ", s.GetHostPath(), "health")
		response, errResp := http.Get(fmt.Sprintf("%s%s", s.GetHostPath(), "health"))
		if errResp != nil {
			log.Errorf("try %d, return health check of the rest service for error  %v", i, errResp)
			time.Sleep(healthCheckPause)
			err = errResp
			continue
		}
		if response != nil && response.StatusCode == http.StatusOK {
			response.Body.Close()
			log.Infof("rest service returned healthy status")
			time.Sleep(healthCheckPause)
			err = nil
			return
		}
		response.Body.Close()
	}
	if err != nil {
		err = fmt.Errorf("error connecting to rest api %s", err.Error())
	}
	return
}

// Port port id
func (s *Server) Port() int {
	return s.port
}

// Ready gives the status of the server
func (s *Server) Ready() bool {
	return s.status == started
}

// GetHostPath  returns hostpath
func (s *Server) GetHostPath() *types.URI {
	return types.ParseURI(fmt.Sprintf("http://localhost:%d%s", s.port, s.apiPath))
}

// Start will start res routes service
func (s *Server) Start() {
	if s.status == started || s.status == starting {
		log.Infof("Server is already running at port %d", s.port)
		return
	}
	s.status = starting
	r := mux.NewRouter()

	api := r.PathPrefix(s.apiPath).Subrouter()

	// createSubscription create subscription and send it to a channel that is shared by middleware to process
	// swagger:operation POST /subscriptions subscription createSubscription
	// ---
	// summary: Creates a new subscription.
	// description: If subscription creation is success(or if already exists), subscription will be returned with Created (201).
	// parameters:
	// - name: subscription
	//   description: subscription to add to the list of subscriptions
	//   in: body
	//   schema:
	//      "$ref": "#/definitions/PubSub"
	// responses:
	//   "201":
	//     "$ref": "#/responses/pubSubResp"
	//   "400":
	//     "$ref": "#/responses/badReq"
	api.HandleFunc("/subscriptions", s.createSubscription).Methods(http.MethodPost)

	//createPublisher create publisher and send it to a channel that is shared by middleware to process
	// swagger:operation POST /publishers/ publishers createPublisher
	// ---
	// summary: Creates a new publisher.
	// description: If publisher creation is success(or if already exists), publisher will be returned with Created (201).
	// parameters:
	// - name: publisher
	//   description: publisher to add to the list of publishers
	//   in: body
	//   schema:
	//      "$ref": "#/definitions/PubSub"
	// responses:
	//   "201":
	//     "$ref": "#/responses/pubSubResp"
	//   "400":
	//     "$ref": "#/responses/badReq"
	api.HandleFunc("/publishers", s.createPublisher).Methods(http.MethodPost)
	/*
		 this method a list of subscription object(s) and their associated properties
		200  Returns the subscription resources and their associated properties that already exist.
			See note below.
		404 Subscription resources are not available (not created).
	*/
	api.HandleFunc("/subscriptions", s.getSubscriptions).Methods(http.MethodGet)
	//publishers create publisher and send it to a channel that is shared by middleware to process
	// swagger:operation GET /publishers/ publishers getPublishers
	// ---
	// summary: Get publishers.
	// description: If publisher creation is success(or if already exists), publisher will be returned with Created (201).
	// parameters:
	// responses:
	//   "200":
	//     "$ref": "#/responses/publishers"
	//   "404":
	//     "$ref": "#/responses/notFound"
	api.HandleFunc("/publishers", s.getPublishers).Methods(http.MethodGet)
	// 200 and 404
	api.HandleFunc("/subscriptions/{subscriptionid}", s.getSubscriptionByID).Methods(http.MethodGet)
	api.HandleFunc("/publishers/{publisherid}", s.getPublisherByID).Methods(http.MethodGet)
	// 204 on success or 404
	api.HandleFunc("/subscriptions/{subscriptionid}", s.deleteSubscription).Methods(http.MethodDelete)
	api.HandleFunc("/publishers/{publisherid}", s.deletePublisher).Methods(http.MethodDelete)

	api.HandleFunc("/subscriptions", s.deleteAllSubscriptions).Methods(http.MethodDelete)
	api.HandleFunc("/publishers", s.deleteAllPublishers).Methods(http.MethodDelete)

	//pingForSubscribedEventStatus pings for event status  if the publisher  has capability to push event on demand
	// swagger:operation POST /subscriptions/status subscriptions pingForSubscribedEventStatus
	// ---
	// summary: Get status of publishing events.
	// description: If publisher status ping is success, call  will be returned with status accepted.
	// parameters:
	// - name: subscriptionid
	//   description: subscription id to check status for
	// responses:
	//   "201":
	//     "$ref": "#/responses/pubSubResp"
	//   "400":
	//     "$ref": "#/responses/badReq"
	api.HandleFunc("/subscriptions/status/{subscriptionid}", s.pingForSubscribedEventStatus).Methods(http.MethodPut)

	api.HandleFunc("/{resourceAddress:.*}/CurrentState", s.getCurrentState).Methods(http.MethodGet)

	api.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, "OK") //nolint:errcheck
	}).Methods(http.MethodGet)

	api.HandleFunc("/dummy", dummy).Methods(http.MethodPost)
	api.HandleFunc("/log", s.logEvent).Methods(http.MethodPost)

	//publishEvent create event and send it to a channel that is shared by middleware to process
	// swagger:operation POST /create/event/ event publishEvent
	// ---
	// summary: Creates a new event.
	// description: If publisher is present for the event, then event creation is success and be returned with Accepted (202).
	// parameters:
	// - name: event
	//   description: event along with publisher id
	//   in: body
	//   schema:
	//      "$ref": "#/definitions/Event"
	// responses:
	//   "202":
	//     "$ref": "#/responses/acceptedReq"
	//   "400":
	//     "$ref": "#/responses/badReq"
	api.HandleFunc("/create/event", s.publishEvent).Methods(http.MethodPost)

	err := r.Walk(func(route *mux.Route, _ *mux.Router, _ []*mux.Route) error {
		pathTemplate, err := route.GetPathTemplate()
		if err == nil {
			log.Println("ROUTE:", pathTemplate)
		}
		pathRegexp, err := route.GetPathRegexp()
		if err == nil {
			log.Println("Path regexp:", pathRegexp)
		}
		queriesTemplates, err := route.GetQueriesTemplates()
		if err == nil {
			log.Println("Queries templates:", strings.Join(queriesTemplates, ","))
		}
		queriesRegexps, err := route.GetQueriesRegexp()
		if err == nil {
			log.Println("Queries regexps:", strings.Join(queriesRegexps, ","))
		}
		methods, err := route.GetMethods()
		if err == nil {
			log.Println("Methods:", strings.Join(methods, ","))
		}
		log.Println()
		return nil
	})

	if err != nil {
		log.Println(err)
	}
	api.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, r)
	})

	log.Infof("starting v2 rest api server at port %d, endpoint %s", s.port, s.apiPath)
	go wait.Until(func() {
		s.status = started
		s.httpServer = &http.Server{
			ReadHeaderTimeout: HTTPReadHeaderTimeout,
			Addr:              fmt.Sprintf(":%d", s.port),
			Handler:           api,
		}
		err := s.httpServer.ListenAndServe()
		if err != nil {
			log.Errorf("restarting due to error with api server %s\n", err.Error())
			s.status = failed
		}
	}, 1*time.Second, s.closeCh)
}

// Shutdown ... shutdown rest service api, but it will not close until close chan is called
func (s *Server) Shutdown() {
	log.Warnf("trying to shutdown rest api sever, please use close channel to shutdown ")
	s.httpServer.Close()
}

// SetOnStatusReceiveOverrideFn ... sets receiver function
func (s *Server) SetOnStatusReceiveOverrideFn(fn func(e cloudevents.Event, dataChan *channel.DataChan) error) {
	s.statusReceiveOverrideFn = fn
}
