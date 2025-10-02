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

// Package restapi O-RAN Compliant REST API
//
// REST API Spec.
//
// Terms Of Service:
//
//	Schemes: http, https
//	Host: localhost:9043
//	BasePath: /api/ocloudNotifications/v2
//	Version: 2.0.0
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
	"encoding/json"
	"fmt"
	"os"

	"github.com/redhat-cne/sdk-go/pkg/util/wait"

	"sync"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/gorilla/mux"
	"github.com/redhat-cne/sdk-go/pkg/channel"
	"github.com/redhat-cne/sdk-go/pkg/event"
	"github.com/redhat-cne/sdk-go/pkg/types"
	pubsubv1 "github.com/redhat-cne/sdk-go/v1/pubsub"
	subscriberApi "github.com/redhat-cne/sdk-go/v1/subscriber"

	"crypto/tls"
	"crypto/x509"
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

type ServerStatus int

const (
	HTTPReadHeaderTimeout = 2 * time.Second
)

const (
	starting = iota
	started
	notReady
	failed
	CURRENTSTATE = "CurrentState"
)

// AuthConfig contains authentication configuration for both single and multi-node OpenShift clusters
type AuthConfig struct {
	// mTLS configuration - works for both single and multi-node clusters
	EnableMTLS     bool   `json:"enableMTLS"`
	CACertPath     string `json:"caCertPath"`
	ServerCertPath string `json:"serverCertPath"`
	ServerKeyPath  string `json:"serverKeyPath"`
	UseServiceCA   bool   `json:"useServiceCA"` // Use OpenShift Service CA (recommended for all cluster sizes)

	// OAuth configuration using OpenShift OAuth Server - works for both single and multi-node clusters
	EnableOAuth         bool     `json:"enableOAuth"`
	OAuthIssuer         string   `json:"oauthIssuer"`         // OpenShift OAuth server URL
	OAuthJWKSURL        string   `json:"oauthJWKSURL"`        // OpenShift JWKS endpoint
	RequiredScopes      []string `json:"requiredScopes"`      // Required OAuth scopes
	RequiredAudience    string   `json:"requiredAudience"`    // Required OAuth audience
	ServiceAccountName  string   `json:"serviceAccountName"`  // ServiceAccount for client authentication
	ServiceAccountToken string   `json:"serviceAccountToken"` // ServiceAccount token path
	UseOpenShiftOAuth   bool     `json:"useOpenShiftOAuth"`   // Use OpenShift's built-in OAuth server (recommended for all cluster sizes)
}

// LoadAuthConfig loads authentication configuration from a JSON file
func LoadAuthConfig(configPath string) (*AuthConfig, error) {
	// Check if file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("authentication config file not found: %s", configPath)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read authentication config file %s: %v", configPath, err)
	}

	var config AuthConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal authentication config: %v", err)
	}
	return &config, nil
}

// GetConfigSummary returns a summary of the authentication configuration
func (c *AuthConfig) GetConfigSummary() string {
	summary := "Authentication Configuration Summary:\n"
	summary += fmt.Sprintf("  Enable mTLS: %t\n", c.EnableMTLS)
	if c.EnableMTLS {
		summary += fmt.Sprintf("    CA Cert Path: %s\n", c.CACertPath)
		summary += fmt.Sprintf("    Server Cert Path: %s\n", c.ServerCertPath)
		summary += fmt.Sprintf("    Server Key Path: %s\n", c.ServerKeyPath)
		summary += fmt.Sprintf("    Use Service CA: %t\n", c.UseServiceCA)
	}
	summary += fmt.Sprintf("  Enable OAuth: %t\n", c.EnableOAuth)
	if c.EnableOAuth {
		summary += fmt.Sprintf("    OAuth Issuer: %s\n", c.OAuthIssuer)
		summary += fmt.Sprintf("    OAuth JWKS URL: %s\n", c.OAuthJWKSURL)
		summary += fmt.Sprintf("    Required Scopes: %v\n", c.RequiredScopes)
		summary += fmt.Sprintf("    Required Audience: %s\n", c.RequiredAudience)
		summary += fmt.Sprintf("    Service Account Name: %s\n", c.ServiceAccountName)
		summary += fmt.Sprintf("    Service Account Token Path: %s\n", c.ServiceAccountToken)
		summary += fmt.Sprintf("    Use OpenShift OAuth: %t\n", c.UseOpenShiftOAuth)
	}
	return summary
}

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
	status                  ServerStatus
	statusReceiveOverrideFn func(e cloudevents.Event, dataChan *channel.DataChan) error
	statusLock              sync.RWMutex
	authConfig              *AuthConfig
	caCertPool              *x509.CertPool
}

// SubscriptionInfo
//
// SubscriptionInfo defines data types used for subscription.
// swagger:parameters
type SubscriptionInfo struct { //nolint:deadcode,unused
	// Identifier for the created subscription resource.
	// example: d1dd1770-e718-401e-ba32-cef05a286164
	// +required
	ID string `json:"SubscriptionId" omit:"empty"`
	// Endpoint URI (a.k.a callback URI), e.g. http://localhost:8080/resourcestatus/ptp
	// example: http://event-receiver/endpoint
	// +required
	EndPointURI string `json:"EndpointUri" example:"http://localhost:8080/resourcestatus/ptp" omit:"empty"`
	// The URI location for querying the subscription created.
	// example: http://localhost:9043/api/ocloudNotifications/v2/publishers/d1dd1770-e718-401e-ba32-cef05a286164
	// +required
	URILocation string `json:"UriLocation" omit:"empty"`
	// The resource address specifies the Event Producer with a hierarchical path.
	// Format /{clusterName}/{siteName}(/optional/hierarchy/..)/{nodeName}/{(/optional/hierarchy)/resource}
	// example: /east-edge-10/vdu3/o-ran-sync/sync-group/sync-status/sync-state
	// +required
	Resource string `json:"ResourceAddress" example:"/east-edge-10/vdu3/o-ran-sync/sync-group/sync-status/sync-state"`
}

// Event Data Model
//
// Event Data Model specifies the event Status Notification data model supported by the API. The current model supports JSON encoding of the CloudEvents.io specification for the event payload.
// swagger:parameters
type EventData struct {
	// Identifies the event. The Event Producer SHALL ensure that source + id is unique for each distinct event
	// example: e0dcb68b-2541-4d21-ab73-a222e42373c2
	// +required
	ID string `json:"id"`
	// Identifies the context in which an event happened.
	// example: /sync/sync-status/sync-state
	// +required
	Source string `json:"source"`
	// This attribute contains a value describing the type of event related to the originating occurrence.
	// example: event.sync.sync-status.synchronization-state-change
	// +required
	Type string `json:"type"`
	// The version of the CloudEvents specification which the event uses. This enables the interpretation of the context.
	// example: 1.0
	// +optional
	SpecVersion string `json:"specversion" default:"1.0"`
	// Time at which the event occurred.
	// example: 2021-03-05T20:59:00.999999999Z
	// +optional
	Time *types.Timestamp `json:"time,omitempty"`
	// Array of JSON objects defining the information for the event
	// +required
	Data *event.Data `json:"data"`
}

// SubscriptionId
//
// This is used for operations that want the SubscriptionId in the path
// swagger:parameters getSubscriptionByID deleteSubscription
type SubscriptionId struct { //nolint
	// Identifier for subscription resource, created after a successful subscription.
	//
	// in: path
	// required: true
	ID string `json:"subscriptionId"`
}

// ResourceAddress
//
// This is used for operations that want the ResourceAddress in the path
// swagger:parameters getCurrentState
type ResourceAddress struct {
	// Identifier for subscription resource
	//
	// in: path
	// required: true
	Resource string `json:"ResourceAddress" example:"/east-edge-10/vdu3/o-ran-sync/sync-group/sync-status/sync-state"`
}

// Shall be returned when the subscription resource is created successfully.
// swagger:response pubSubResp
type swaggPubSubRes struct { //nolint:deadcode,unused
	// in:body
	Body SubscriptionInfo
}

// swagger:response publishers
type swaggPubSubResList struct { //nolint:deadcode,unused
	// in:body
	Body []SubscriptionInfo
}

// Returns the subscription resources and their associated properties that already exist.
// swagger:response subscriptions
type swaggSubList struct { //nolint:deadcode,unused
	// in:body
	Body []SubscriptionInfo
}

// Returns the subscription resource object and its associated properties.
// swagger:response subscription
type swaggSub struct { //nolint:deadcode,unused
	// in:body
	Body SubscriptionInfo
}

// OK
// swagger:response statusOK
type statusOK struct { //nolint:deadcode,unused
	// in:body
	// example:"OK"
	Status string `example:"OK"`
}

// Return the pull event status
// swagger:response eventResp
type swaggEventData struct { //nolint:deadcode,unused
	// in:body
	Body EventData
}

// InitServer is used to supply configurations for rest routes server
func InitServer(port int, apiHost, apiPath, storePath string,
	dataOut chan<- *channel.DataChan, closeCh <-chan struct{},
	onStatusReceiveOverrideFn func(e cloudevents.Event, dataChan *channel.DataChan) error,
	authConfig *AuthConfig) *Server {
	once.Do(func() {
		ServerInstance = &Server{
			port:                    port,
			apiHost:                 apiHost,
			apiPath:                 apiPath,
			dataOut:                 dataOut,
			closeCh:                 closeCh,
			status:                  notReady,
			pubSubAPI:               pubsubv1.GetAPIInstance(storePath),
			subscriberAPI:           subscriberApi.GetAPIInstance(storePath),
			statusReceiveOverrideFn: onStatusReceiveOverrideFn,
			authConfig:              authConfig,
		}

		// Configure HTTPClient with proper TLS settings for publisher endpoint validation
		if authConfig != nil && authConfig.EnableMTLS {
			// Create HTTPClient with TLS configuration that allows localhost connections
			ServerInstance.HTTPClient = &http.Client{
				Transport: &http.Transport{
					MaxIdleConnsPerHost: 20,
					TLSClientConfig: &tls.Config{
						InsecureSkipVerify: true, // nolint:gosec // Required for localhost connections in mTLS setup
					},
				},
				Timeout: 10 * time.Second,
			}
			log.Infof("InitServer: Configured HTTPClient with InsecureSkipVerify for mTLS localhost connections")
		} else {
			// Use default HTTP client for non-mTLS configurations
			ServerInstance.HTTPClient = &http.Client{
				Transport: &http.Transport{
					MaxIdleConnsPerHost: 20,
				},
				Timeout: 10 * time.Second,
			}
		}

		// Initialize mTLS CA certificate pool if mTLS is enabled
		if authConfig != nil && authConfig.EnableMTLS && authConfig.CACertPath != "" {
			fmt.Printf("InitServer: Setting authConfig with EnableMTLS=%t\n", authConfig.EnableMTLS)
			if err := ServerInstance.initMTLSCACertPool(); err != nil {
				log.Errorf("failed to initialize mTLS CA certificate pool: %v", err)
			}
		} else {
			fmt.Printf("InitServer: authConfig is nil or EnableMTLS is false (authConfig=%v, EnableMTLS=%t)\n", authConfig != nil, authConfig != nil && authConfig.EnableMTLS)
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

		healthURL := s.GetHealthPath()
		log.Debugf("health check %s", healthURL)

		var response *http.Response
		var errResp error

		if s.authConfig != nil && s.authConfig.EnableMTLS {
			// Use HTTPS client without client certificate for health checks
			client := &http.Client{
				Transport: &http.Transport{
					TLSClientConfig: &tls.Config{
						RootCAs:            s.caCertPool,
						InsecureSkipVerify: true, // nolint:gosec // Required for localhost health checks with self-signed certs
						// No client certificate provided - this is allowed for /health
					},
				},
			}
			response, errResp = client.Get(healthURL)
		} else {
			// Use regular HTTP client
			response, errResp = http.Get(healthURL)
		}

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
	s.statusLock.RLock()
	defer s.statusLock.RUnlock()
	return s.status == started
}

// SetStatus safely updates the server status
func (s *Server) SetStatus(newStatus ServerStatus) {
	s.statusLock.Lock()
	defer s.statusLock.Unlock()
	s.status = newStatus
}

func (s *Server) GetStatus() ServerStatus {
	s.statusLock.RLock()
	defer s.statusLock.RUnlock()
	return s.status
}

// GetHostPath  returns hostpath
func (s *Server) GetHostPath() *types.URI {
	protocol := "http"
	port := s.port
	path := s.apiPath

	if s.authConfig != nil && s.authConfig.EnableMTLS {
		protocol = "https"
		fmt.Printf("GetHostPath: Using HTTPS protocol (authConfig.EnableMTLS=%t)\n", s.authConfig.EnableMTLS)
	} else {
		fmt.Printf("GetHostPath: Using HTTP protocol (authConfig=%v, EnableMTLS=%t)\n", s.authConfig != nil, s.authConfig != nil && s.authConfig.EnableMTLS)
	}
	uri := types.ParseURI(fmt.Sprintf("%s://localhost:%d%s", protocol, port, path))
	fmt.Printf("GetHostPath: Returning URI=%s\n", uri.String())
	return uri
}

// GetHealthPath returns the health check URL
func (s *Server) GetHealthPath() string {
	protocol := "http"
	if s.authConfig != nil && s.authConfig.EnableMTLS {
		protocol = "https"
	}
	return fmt.Sprintf("%s://localhost:%d%shealth", protocol, s.port, s.apiPath)
}

// Start will start res routes service
func (s *Server) Start() {
	currentStatus := s.GetStatus()
	if currentStatus == started || currentStatus == starting {
		log.Infof("Server is already running at port %d", s.port)
		return
	}
	s.SetStatus(starting)
	r := mux.NewRouter()

	api := r.PathPrefix(s.apiPath).Subrouter()

	// Helper function to apply authentication to handlers
	applyAuth := func(handler http.HandlerFunc, needsAuth bool) http.Handler {
		if needsAuth {
			return s.combinedAuthMiddleware(http.Handler(handler))
		}
		return handler
	}

	// createSubscription create subscription and send it to a channel that is shared by middleware to process
	// swagger:operation POST /subscriptions Subscriptions createSubscription
	// ---
	// summary: Creates a subscription resource for the Event Consumer.
	// description: Creates a new subscription for the required event by passing the appropriate payload.
	// parameters:
	// - name: SubscriptionInfo
	//   description: The payload will include an event notification request, endpointUri and ResourceAddress. The SubscriptionId and UriLocation are ignored in the POST body (these will be sent to the client after the resource is created).
	//   in: body
	//   schema:
	//      "$ref": "#/definitions/SubscriptionInfo"
	// responses:
	//   "201":
	//     "$ref": "#/responses/pubSubResp"
	//   "400":
	//     description: Bad request. For example, the endpoint URI is not correctly formatted.
	//   "401":
	//     description: Unauthorized. Authentication required (mTLS and/or OAuth).
	//   "404":
	//     description: Not Found. Subscription resource is not available.
	//   "409":
	//     description: Conflict. The subscription resource already exists.
	api.Handle("/subscriptions", applyAuth(s.createSubscription, true)).Methods(http.MethodPost)

	// swagger:operation GET /subscriptions Subscriptions getSubscriptions
	// ---
	// summary: Retrieves a list of subscriptions.
	// description: Get a list of subscription object(s) and their associated properties.
	// responses:
	//   "200":
	//     "$ref": "#/responses/subscriptions"
	//   "400":
	//     description: Bad request by the client.
	api.Handle("/subscriptions", applyAuth(s.getSubscriptions, false)).Methods(http.MethodGet)

	// swagger:operation GET /subscriptions/{subscriptionId} Subscriptions getSubscriptionByID
	// ---
	// summary: Returns details for a specific subscription.
	// description: Returns details for the subscription with ID subscriptionId.
	// responses:
	//   "200":
	//     "$ref": "#/responses/subscription"
	//   "404":
	//     description: Not Found. Subscription resources are not available (not created).
	api.Handle("/subscriptions/{subscriptionId}", applyAuth(s.getSubscriptionByID, false)).Methods(http.MethodGet)

	// swagger:operation DELETE /subscriptions/{subscriptionId} Subscriptions deleteSubscription
	// ---
	// summary: Delete a specific subscription.
	// description: Deletes an individual subscription resource object and its associated properties.
	// responses:
	//   "204":
	//     description: Success.
	//   "401":
	//     description: Unauthorized. Authentication required (mTLS and/or OAuth).
	//   "404":
	//     description: Not Found. Subscription resources are not available (not created).
	api.Handle("/subscriptions/{subscriptionId}", applyAuth(s.deleteSubscription, true)).Methods(http.MethodDelete)

	// swagger:operation GET /{ResourceAddress}/CurrentState Events getCurrentState
	// ---
	// summary: Pulls the event status notifications for specified ResourceAddress.
	// description: As a result of successful execution of this method the Event Consumer will receive the current event status notifications of the node that the Event Consumer resides on.
	// responses:
	//   "200":
	//     "$ref": "#/responses/eventResp"
	//   "404":
	//     description: Not Found. Event notification resource is not available on this node.
	api.Handle("/{resourceAddress:.*}/CurrentState", applyAuth(s.getCurrentState, false)).Methods(http.MethodGet)

	// *** Extensions to O-RAN API ***

	// swagger:operation GET /health HealthCheck getHealth
	// ---
	// summary: (Extensions to O-RAN API) Returns the health status of API.
	// description: Returns the health status for the ocloudNotifications REST API.
	// responses:
	//   "200":
	//     "$ref": "#/responses/statusOK"
	// Note: Health endpoint is always accessible without authentication
	api.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, "OK") //nolint:errcheck
	}).Methods(http.MethodGet)

	//publishers create publisher and send it to a channel that is shared by middleware to process
	// swagger:operation GET /publishers Publishers getPublishers
	// ---
	// summary: (Extensions to O-RAN API) Get publishers.
	// description: Returns a list of publisher details for the cluster node.
	// parameters:
	// responses:
	//   "200":
	//     "$ref": "#/responses/publishers"
	//   "404":
	//	   description: Publishers not found
	api.Handle("/publishers", applyAuth(s.getPublishers, false)).Methods(http.MethodGet)

	// swagger:operation DELETE /subscriptions Subscriptions deleteAllSubscriptions
	// ---
	// summary: (Extensions to O-RAN API) Delete all subscriptions.
	// description: Delete all subscriptions.
	// responses:
	//   "204":
	//     description: Deleted all subscriptions.
	//   "401":
	//     description: Unauthorized. Authentication required (mTLS and/or OAuth).
	api.Handle("/subscriptions", applyAuth(s.deleteAllSubscriptions, true)).Methods(http.MethodDelete)

	// *** Internal API ***

	api.Handle("/publishers/{publisherid}", applyAuth(s.getPublisherByID, false)).Methods(http.MethodGet)
	api.Handle("/publishers/{publisherid}", applyAuth(s.deletePublisher, true)).Methods(http.MethodDelete)
	api.Handle("/publishers", applyAuth(s.deleteAllPublishers, true)).Methods(http.MethodDelete)

	//pingForSubscribedEventStatus pings for event status  if the publisher  has capability to push event on demand
	// this API is internal
	// operation POST /subscriptions/status subscriptions pingForSubscribedEventStatus
	// ---
	// summary: Get status of publishing events.
	// description: If publisher status ping is success, call  will be returned with status accepted.
	// parameters:
	// - name: subscriptionId
	//   description: subscription id to check status for
	// responses:
	//   "201":
	//     "$ref": "#/responses/pubSubResp"
	//   "400":
	//     "$ref": "#/responses/badReq"
	//   "401":
	//     description: Unauthorized. Authentication required (mTLS and/or OAuth).
	api.Handle("/subscriptions/status/{subscriptionId}", applyAuth(s.pingForSubscribedEventStatus, true)).Methods(http.MethodPut)

	api.Handle("/log", applyAuth(s.logEvent, true)).Methods(http.MethodPost)

	api.Handle("/publishers", applyAuth(s.createPublisher, true)).Methods(http.MethodPost)

	//publishEvent create event and send it to a channel that is shared by middleware to process
	// this API is internal
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
	//   "401":
	//     description: Unauthorized. Authentication required (mTLS and/or OAuth).
	api.Handle("/create/event", applyAuth(s.publishEvent, true)).Methods(http.MethodPost)

	// for internal test
	api.Handle("/dummy", applyAuth(dummy, true)).Methods(http.MethodPost)
	// for internal test: test multiple clients
	api.Handle("/dummy2", applyAuth(dummy, true)).Methods(http.MethodPost)

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
		s.SetStatus(started)
		s.httpServer = &http.Server{
			ReadHeaderTimeout: HTTPReadHeaderTimeout,
			Addr:              fmt.Sprintf(":%d", s.port),
			Handler:           api,
		}

		// Configure TLS if mTLS is enabled
		if s.authConfig != nil && s.authConfig.EnableMTLS {
			if s.authConfig.ServerCertPath == "" || s.authConfig.ServerKeyPath == "" {
				log.Error("mTLS enabled but server certificate or key path not provided")
				s.SetStatus(failed)
				return
			}

			// Load server certificate and key
			cert, err := tls.LoadX509KeyPair(s.authConfig.ServerCertPath, s.authConfig.ServerKeyPath)
			if err != nil {
				log.Errorf("failed to load server certificate: %v", err)
				s.SetStatus(failed)
				return
			}

			// Configure TLS to request client certificates but not require them
			// We'll handle certificate validation at the application level
			tlsConfig := &tls.Config{
				Certificates: []tls.Certificate{cert},
				ClientAuth:   tls.RequestClientCert, // Request but don't require
				ClientCAs:    s.caCertPool,
				MinVersion:   tls.VersionTLS12,
			}

			s.httpServer.TLSConfig = tlsConfig

			// Note: When mTLS is enabled, client certificates are requested but validated at middleware level.
			// The /health endpoint allows connections without certificates, while other endpoints require them.

			log.Info("starting HTTPS server with application-level mTLS")
			err = s.httpServer.ListenAndServeTLS("", "")
			if err != nil {
				log.Errorf("restarting due to error with TLS api server %s\n", err.Error())
				s.SetStatus(failed)
			}
		} else {
			log.Info("starting HTTP server")
			err := s.httpServer.ListenAndServe()
			if err != nil {
				log.Errorf("restarting due to error with api server %s\n", err.Error())
				s.SetStatus(failed)
			}
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

// GetSubscriberAPI ...
func (s *Server) GetSubscriberAPI() *subscriberApi.API {
	return s.subscriberAPI
}
