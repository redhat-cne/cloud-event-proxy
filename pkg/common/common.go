package common

import (
	"encoding/json"
	"fmt"
	"github.com/redhat-cne/cloud-event-proxy/pkg/restclient"
	restapi "github.com/redhat-cne/rest-api"
	"github.com/redhat-cne/sdk-go/pkg/channel"
	ceevent "github.com/redhat-cne/sdk-go/pkg/event"
	"github.com/redhat-cne/sdk-go/pkg/pubsub"
	"github.com/redhat-cne/sdk-go/pkg/types"
	v1event "github.com/redhat-cne/sdk-go/v1/event"
	v1pubsub "github.com/redhat-cne/sdk-go/v1/pubsub"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
)

// SCConfiguration simple configuration to initialize variables
type SCConfiguration struct {
	EventInCh  chan *channel.DataChan
	EventOutCh chan *channel.DataChan
	CloseCh    chan bool
	APIPort    int
	APIPath    string
	PubSubAPI  *v1pubsub.API
	StorePath  string
	AMQPHost   string
	BaseURL    *types.URI
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
func StartPubSubService(wg *sync.WaitGroup, scConfig *SCConfiguration) (*restapi.Server, error) {
	// init
	server := restapi.InitServer(scConfig.APIPort, scConfig.APIPath, scConfig.StorePath, scConfig.EventInCh, scConfig.CloseCh)
	defer wg.Done()
	wg.Add(1)
	go server.Start(wg)
	err := server.EndPointHealthChk()
	if err == nil {
		scConfig.BaseURL = server.GetHostPath()
		scConfig.APIPort = server.Port()
	}
	return server, err
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
		log.Printf("failed to marshal publisher ")
	}
	if err = json.Unmarshal(pubB, &pub); err != nil {
		return
	}
	return pub, err
}

//CreateSubscription creates a subscription object
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
		log.Printf("failed to marshal subscription ")
	}
	if err = json.Unmarshal(subB, &sub); err != nil {
		return
	}
	return sub, err
}

//CreateEvent create an event
func CreateEvent(pubSubID, eventType string, data ceevent.Data) (ceevent.Event, error) {
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
	event.SetTime(types.Timestamp{Time: time.Now().UTC()}.Time)
	event.SetDataContentType(ceevent.ApplicationJSON)
	event.SetData(data)
	return event, nil
}

// PublishEvent publishes event
func PublishEvent(scConfig *SCConfiguration, e ceevent.Event) error {
	//create publisher
	url := fmt.Sprintf("%s%s", scConfig.BaseURL.String(), "create/event")
	rc := restclient.New()
	err := rc.PostEvent(types.ParseURI(url), e)
	if err != nil {
		log.Printf("error posting ptp events %v", err)
		return err
	}
	log.Printf("published ptp event %s", e.String())

	return nil
}

//APIHealthCheck .. rest api should be ready before starting to consume api
func APIHealthCheck(uri *types.URI, delay time.Duration) (ok bool, err error) {
	log.Printf("checking for rest service health\n")
	for i := 0; i <= 5; i++ {
		log.Printf("health check %s ", uri.String())
		response, errResp := http.Get(uri.String())
		if errResp != nil {
			log.Printf("try %d, return health check of the rest service for error  %v", i, errResp)
			time.Sleep(delay)
			err = errResp
			continue
		}
		if response != nil && response.StatusCode == http.StatusOK {
			response.Body.Close()
			log.Printf("rest service returned healthy status")
			time.Sleep(delay)
			err = nil
			ok = true
			return
		}
		response.Body.Close()
	}
	if err != nil {
		err = fmt.Errorf("error connecting to rest api %s", err.Error())
	}
	return
}
