// Package restapi Pub/Sub Rest API.
//
// Rest API spec .
//
// Terms Of Service:
//
//	Schemes: http, https
//	Host: localhost:8080
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

	"github.com/gorilla/mux"
	"github.com/redhat-cne/sdk-go/pkg/channel"
	"github.com/redhat-cne/sdk-go/pkg/event"
	"github.com/redhat-cne/sdk-go/pkg/pubsub"
	"github.com/redhat-cne/sdk-go/pkg/types"
	pubsubv1 "github.com/redhat-cne/sdk-go/v1/pubsub"

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

const HTTPReadHeaderTimeout = 2 * time.Second

const (
	starting = iota
	started
	notReady
	failed
)

// Server defines rest routes server object
type Server struct {
	port    int
	apiPath string
	//data out is amqp in channel
	dataOut    chan<- *channel.DataChan
	closeCh    <-chan struct{}
	HTTPClient *http.Client
	httpServer *http.Server
	pubSubAPI  *pubsubv1.API
	status     serverStatus
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
func InitServer(port int, apiPath, storePath string, dataOut chan<- *channel.DataChan, closeCh <-chan struct{}) *Server {
	once.Do(func() {
		ServerInstance = &Server{
			port:    port,
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
			pubSubAPI: pubsubv1.GetAPIInstance(storePath),
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
	// swagger:operation POST /subscriptions/ subscription createSubscription
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

	api.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
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

	err := r.Walk(func(route *mux.Route, router *mux.Router, ancestors []*mux.Route) error {
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

	log.Info("starting rest api server")
	log.Infof("endpoint %s", s.apiPath)
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
