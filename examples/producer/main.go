package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/redhat-cne/cloud-event-proxy/pkg/common"
	"github.com/redhat-cne/cloud-event-proxy/pkg/restclient"
	cneevent "github.com/redhat-cne/sdk-go/pkg/event"
	"github.com/redhat-cne/sdk-go/pkg/pubsub"
	"github.com/redhat-cne/sdk-go/pkg/types"
	v1event "github.com/redhat-cne/sdk-go/v1/event"
	v1pubsub "github.com/redhat-cne/sdk-go/v1/pubsub"
	log "github.com/sirupsen/logrus"
	"io/ioutil"
	"net/http"
	"net/url"
	"sync"
	"time"
)

var (
	apiAddr                string = "localhost:8080"
	apiPath                string = "/api/cloudNotifications/v1/"
	resourceAddressSports  string = "/news-service/sports"
	resourceAddressFinance string = "/news-service/finance"
	localAPIAddr           string = "localhost:9088"
)

func main() {
	common.InitLogger()
	flag.StringVar(&localAPIAddr, "local-api-addr", "localhost:9088", "The address the local api binds to .")
	flag.StringVar(&apiPath, "api-path", "/api/cloudNotifications/v1/", "The rest api path.")
	flag.StringVar(&apiAddr, "api-addr", "localhost:8080", "The address the framework api endpoint binds to.")
	flag.Parse()

	var wg sync.WaitGroup
	wg.Add(1)
	go server()

	pubs := []*pubsub.PubSub{&pubsub.PubSub{
		Resource: resourceAddressSports,
	}, &pubsub.PubSub{
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
				event.Type = "ptp_status_type"
				event.SetTime(types.Timestamp{Time: time.Now().UTC()}.Time)
				event.SetDataContentType(cneevent.ApplicationJSON)
				data := cneevent.Data{
					Version: "v1",
					Values: []cneevent.DataValue{{
						Resource:  pub.Resource,
						DataType:  cneevent.NOTIFICATION,
						ValueType: cneevent.ENUMERATION,
						Value:     cneevent.ACQUIRING_SYNC,
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
		Path: fmt.Sprintf("%s", "ack/event")}}
	log.Infof("ednpo %s", endpointURL.String())
	pub := v1pubsub.NewPubSub(endpointURL, resourceAddress)
	if b, err := json.Marshal(&pub); err == nil {
		rc := restclient.New()
		if status, b := rc.PostWithReturn(publisherURL, b); status == http.StatusCreated {
			log.Infof("sdss%s", string(b))
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
	bodyBytes, err := ioutil.ReadAll(req.Body)
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
