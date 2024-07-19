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

// Package common ...
package common

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	storageClient "github.com/redhat-cne/cloud-event-proxy/pkg/storage/kubernetes"

	"github.com/cloudevents/sdk-go/v2/event"
	"github.com/google/uuid"

	"github.com/redhat-cne/rest-api/pkg/localmetrics"

	"github.com/redhat-cne/cloud-event-proxy/pkg/restclient"
	restapi "github.com/redhat-cne/rest-api"
	v2restapi "github.com/redhat-cne/rest-api/v2"
	"github.com/redhat-cne/sdk-go/pkg/channel"
	ceevent "github.com/redhat-cne/sdk-go/pkg/event"
	"github.com/redhat-cne/sdk-go/pkg/pubsub"
	"github.com/redhat-cne/sdk-go/pkg/types"
	v1event "github.com/redhat-cne/sdk-go/v1/event"
	v1pubsub "github.com/redhat-cne/sdk-go/v1/pubsub"
	subscriberApi "github.com/redhat-cne/sdk-go/v1/subscriber"
	log "github.com/sirupsen/logrus"
)

// TransportType  defines transport type supported
type TransportType int

const (
	// HTTP ...
	HTTP TransportType = iota
	// UNKNOWN ...
	UNKNOWN
)

var transportTypes = [...]string{"HTTP", "UNKNOWN"}

// ToString ...
func (t TransportType) ToString() string {
	return transportTypes[t]
}

// TransportHost  holds transport url type
type TransportHost struct {
	Type   TransportType
	URL    string
	Host   string
	Port   int
	Scheme string
	URI    *types.URI
	Err    error
}

func (t *TransportHost) String() string {
	s := strings.Builder{}
	s.WriteString("Host: " + t.Host + "\n")
	s.WriteString("URL: " + t.URL + "\n")
	s.WriteString("Port: " + fmt.Sprintf("%d", t.Port) + "\n")
	s.WriteString("Scheme: " + t.Scheme + "\n")
	s.WriteString("Type: " + fmt.Sprintf("%d", t.Type) + "\n")
	if t.Err != nil {
		s.WriteString("Error: " + t.Err.Error() + "\n")
	} else {
		s.WriteString("Error:  \n")
	}

	return s.String()
}

// SanitizeTransportHost ... Replace string modifiers
func SanitizeTransportHost(transportHost, nodeIP, nodeName string) string {
	if nodeIP != "" {
		transportHost = strings.Replace(transportHost, "NODE_IP", nodeIP, 1)
		log.Infof("transport host path is set to %s", transportHost)
	} else if strings.Contains(transportHost, "NODE_NAME") { // allow overriding transport host
		if nodeName != "" {
			if strings.Contains(nodeName, ".") {
				transportHost = strings.Replace(transportHost, "NODE_NAME", strings.Split(nodeName, ".")[0], 1)
			} else {
				transportHost = strings.Replace(transportHost, "NODE_NAME", nodeName, 1)
			}
			log.Infof("transport host path is set to  %s", transportHost)
		} else {
			log.Info("NODE_NAME env is not set")
		}
	}
	return transportHost
}

// ParseTransportHost ... prase the url to identify type
func (t *TransportHost) ParseTransportHost() {
	var (
		host      string
		sPort     string
		port      int
		parsedURL *url.URL
		err       error
		uri       string
	)
	t.Type = UNKNOWN
	uri = t.URL
	if !strings.Contains(t.URL, "http") {
		uri = fmt.Sprintf("http://%s", t.URL)
	}
	if parsedURL, err = url.Parse(uri); err != nil {
		t.Err = err
		return
	}

	t.Scheme = parsedURL.Scheme
	switch t.Scheme {
	case "http", "https":
		t.Type = HTTP
	}
	t.URI = types.ParseURI(uri)

	if host, sPort, err = net.SplitHostPort(parsedURL.Host); err != nil {
		t.Err = err
		return
	}
	t.Host = host

	port, err = strconv.Atoi(sPort)
	t.Port = port
	t.Err = err
}

// SCConfiguration simple configuration to initialize variables
type SCConfiguration struct {
	EventInCh  chan *channel.DataChan
	EventOutCh chan *channel.DataChan
	StatusCh   chan *channel.StatusChan
	CloseCh    chan struct{}
	APIPort    int
	APIPath    string
	APIVersion string
	PubSubAPI  *v1pubsub.API
	// this is used in V2 when pubsub is removed from local store
	SubscriberAPI     *subscriberApi.API
	StorePath         string
	BaseURL           *types.URI
	TransportHost     *TransportHost
	TransPortInstance interface{}
	clientID          uuid.UUID
	StorageType       storageClient.StorageTypeType
	K8sClient         *storageClient.Client
	RestAPI           *v2restapi.Server
}

// ClientID ... read clientID from the configurations
func (sc *SCConfiguration) ClientID() uuid.UUID {
	return sc.clientID
}

// SetClientID  set clientID for the configuration
func (sc *SCConfiguration) SetClientID(clientID uuid.UUID) error {
	if sc.clientID != uuid.Nil {
		sc.clientID = clientID
		return nil
	}
	return fmt.Errorf("clientID is already present, cannot reset clientID once assigned")
}

// GetIntEnv get int value from env
func GetIntEnv(key string) int {
	if val, ok := os.LookupEnv(key); ok && val != "" {
		if ret, err := strconv.Atoi(val); err == nil {
			return ret
		}
	}
	return 0
}

// GetFloatEnv get int value from env
func GetFloatEnv(key string) float64 {
	if val, ok := os.LookupEnv(key); ok && val != "" {
		if ret, err := strconv.ParseFloat(val, 64); err == nil {
			return ret
		}
	}
	return 0
}

// GetBoolEnv get bool value from env
func GetBoolEnv(key string) bool {
	if val, ok := os.LookupEnv(key); ok && val != "" {
		if ret, err := strconv.ParseBool(val); err == nil {
			return ret
		}
	}
	return false
}

// StartPubSubService starts rest api service to manage events publishers and subscriptions
func StartPubSubService(scConfig *SCConfiguration) (err error) {
	// init
	if scConfig.TransportHost == nil {
		scConfig.TransportHost.Type = UNKNOWN
	}
	if IsV1Api(scConfig.APIVersion) {
		server := restapi.InitServer(scConfig.APIPort, scConfig.APIPath,
			scConfig.StorePath, scConfig.EventInCh, scConfig.CloseCh)

		server.Start()
		err = server.EndPointHealthChk()
		if err == nil {
			scConfig.BaseURL = server.GetHostPath()
			scConfig.APIPort = server.Port()
		}
	} else {
		// reload sub store from configMap
		scConfig.SubscriberAPI.ReloadStore()
		// use EventOutCh instead since this is only used in producer side
		server := v2restapi.InitServer(scConfig.APIPort, scConfig.TransportHost.Host, scConfig.APIPath,
			scConfig.StorePath, scConfig.EventOutCh, scConfig.CloseCh, nil)
		scConfig.RestAPI = server
		server.Start()
		err = server.EndPointHealthChk()
		if err == nil {
			scConfig.BaseURL = server.GetHostPath()
			scConfig.APIPort = server.Port()
		}
	}
	return err
}

// CreatePublisher creates a publisher objects
func CreatePublisher(config *SCConfiguration, publisher pubsub.PubSub) (pub pubsub.PubSub, err error) {
	apiURL := fmt.Sprintf("%s%s", config.BaseURL.String(), "publishers")
	var pubB []byte
	var status int
	if pubB, err = json.Marshal(&publisher); err == nil {
		rc := restclient.New()
		if status, pubB = rc.PostWithReturn(types.ParseURI(apiURL), pubB); status != http.StatusCreated {
			err = fmt.Errorf("publisher creation api at %s, returned status %d", apiURL, status)
			return
		}
	} else {
		log.Error("failed to marshal publisher ")
	}
	if err = json.Unmarshal(pubB, &pub); err != nil {
		return
	}
	return pub, err
}

// CreateSubscription creates a subscription object
func CreateSubscription(config *SCConfiguration, subscription pubsub.PubSub) (sub pubsub.PubSub, err error) {
	apiURL := fmt.Sprintf("%s%s", config.BaseURL.String(), "subscriptions")
	var subB []byte
	var status int
	if subB, err = json.Marshal(&subscription); err == nil {
		rc := restclient.New()
		if status, subB = rc.PostWithReturn(types.ParseURI(apiURL), subB); status != http.StatusCreated {
			err = fmt.Errorf("subscription creation api at %s, returned status %d", apiURL, status)
			return
		}
	} else {
		log.Error("failed to marshal subscription ")
	}
	if err = json.Unmarshal(subB, &sub); err != nil {
		return
	}
	return sub, err
}

// CreateEvent create an event
func CreateEvent(pubSubID, eventType, source string, data ceevent.Data) (ceevent.Event, error) {
	// create an event
	if pubSubID == "" {
		return ceevent.Event{}, fmt.Errorf("id is a required field")
	}
	if eventType == "" {
		return ceevent.Event{}, fmt.Errorf("eventType  is a required field")
	}
	event := v1event.CloudNativeEvent()
	event.ID = pubSubID
	event.Type = eventType
	event.SetSource(source)
	event.SetTime(types.Timestamp{Time: time.Now().UTC()}.Time)
	event.SetDataContentType(ceevent.ApplicationJSON)
	event.SetData(data)
	return event, nil
}

// PublishEvent publishes event
func PublishEvent(scConfig *SCConfiguration, e ceevent.Event) error {
	publishToURL := fmt.Sprintf("%s%s", scConfig.BaseURL.String(), "create/event")
	rc := restclient.New()
	err := rc.PostEvent(types.ParseURI(publishToURL), e)
	if err != nil {
		log.Errorf("error posting cloud native events %v", err)
		return err
	}
	log.Debugf("published cloud native event %s", e.String())

	return nil
}

// PublishEventViaAPI ... publish events by not using http request but direct api
func PublishEventViaAPI(scConfig *SCConfiguration, cneEvent ceevent.Event, resourceAddress string) error {
	if ceEvent, err := GetPublishingCloudEvent(scConfig, cneEvent); err == nil {
		if IsV1Api(scConfig.APIVersion) {
			scConfig.EventInCh <- &channel.DataChan{
				Type:     channel.EVENT,
				Status:   channel.NEW,
				Data:     ceEvent,
				Address:  ceEvent.Source(), // this is the publishing address
				ClientID: scConfig.ClientID(),
			}
		} else {
			// use EventOutCh instead of EventInCh to bypass http transport
			scConfig.EventOutCh <- &channel.DataChan{
				Type:     channel.EVENT,
				Status:   channel.NEW,
				Data:     ceEvent,
				Address:  resourceAddress, // this is the publishing address
				ClientID: scConfig.ClientID(),
			}
		}

		log.Debugf("event source %s sent to queue to process", ceEvent.Source())
		log.Debugf("event sent %s", cneEvent.JSONString())

		localmetrics.UpdateEventPublishedCount(ceEvent.Source(), localmetrics.SUCCESS, 1)
	}
	return nil
}

// GetPublishingCloudEvent ... Get Publishing cloud event
func GetPublishingCloudEvent(scConfig *SCConfiguration, cneEvent ceevent.Event) (ceEvent *event.Event, err error) {
	pub, err := scConfig.PubSubAPI.GetPublisher(cneEvent.ID)
	if err != nil {
		localmetrics.UpdateEventPublishedCount(cneEvent.ID, localmetrics.FAIL, 1)
		return nil, err
	}
	if IsV1Api(scConfig.APIVersion) {
		ceEvent, err = cneEvent.NewCloudEvent(&pub)
	} else {
		ceEvent, err = cneEvent.NewCloudEventV2()
	}
	if err != nil {
		localmetrics.UpdateEventPublishedCount(pub.Resource, localmetrics.FAIL, 1)
		return nil, fmt.Errorf("error converting to CloudEvents %s", err)
	}
	return ceEvent, nil
}

// APIHealthCheck ... rest api should be ready before starting to consume api
func APIHealthCheck(uri *types.URI, delay time.Duration) (ok bool, err error) {
	log.Printf("checking for rest service health")
	for i := 0; i <= 5; i++ {
		log.Infof("health check %s", uri.String())
		response, errResp := http.Get(uri.String())
		if errResp != nil {
			log.Warnf("try %d, return health check of the rest service for error %v", i, errResp)
			time.Sleep(delay)
			err = errResp
			continue
		}
		if response != nil && response.StatusCode == http.StatusOK {
			_ = response.Body.Close()
			log.Info("rest service returned healthy status")
			err = nil
			ok = true
			return
		}
	}
	if err != nil {
		err = fmt.Errorf("error connecting to rest api %s", err.Error())
	}
	return
}

// HTTPTransportHealthCheck ... http transport should be ready before starting to consume events
func HTTPTransportHealthCheck(uri *types.URI, delay time.Duration) (ok bool, err error) {
	for i := 0; i <= 5; i++ {
		log.Infof("health check %s ", uri.String())
		response, errResp := http.Get(uri.String())
		if errResp != nil {
			log.Warnf("try %d, return health check of the http transportfor error  %v", i, errResp)
			time.Sleep(delay)
			err = errResp
			continue
		}
		if response != nil && response.StatusCode == http.StatusOK {
			_ = response.Body.Close()
			log.Info("http transport returned healthy status")
			err = nil
			ok = true
			return
		}
	}
	if err != nil {
		err = fmt.Errorf("error connecting to http transport %s", err.Error())
	}
	return
}

// InitLogger initilaize logger
func InitLogger() {
	lvl, ok := os.LookupEnv("LOG_LEVEL")
	// LOG_LEVEL not set, let's default to debug
	if !ok {
		lvl = "debug"
	}
	// parse string, this is built-in feature of logrus
	ll, err := log.ParseLevel(lvl)
	if err != nil {
		ll = log.DebugLevel
	}
	// set global log level
	log.SetLevel(ll)
}

// GetMajorVersion returns major version
func GetMajorVersion(version string) (int, error) {
	if version == "" {
		return 1, nil
	}
	version = strings.TrimPrefix(version, "v")
	version = strings.TrimPrefix(version, "V")
	v := strings.Split(version, ".")
	majorVersion, err := strconv.Atoi(v[0])
	if err != nil {
		log.Errorf("Error parsing major version from %s, %v", version, err)
		return 1, err
	}
	return majorVersion, nil
}

// IsV1Api ...
func IsV1Api(version string) bool {
	if majorVersion, err := GetMajorVersion(version); err == nil {
		if majorVersion >= 2 {
			return false
		}
	}
	// by default use V1
	return true
}
