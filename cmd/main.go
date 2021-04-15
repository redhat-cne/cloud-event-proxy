package main

import (
	"fmt"
	"log"
	"net/url"
	"os"
	"sync"

	"github.com/redhat-cne/cloud-event-proxy/pkg/common"
	"github.com/redhat-cne/cloud-event-proxy/pkg/plugins"
	"github.com/redhat-cne/cloud-event-proxy/pkg/restclient"
	"github.com/redhat-cne/sdk-go/pkg/types"

	"github.com/redhat-cne/sdk-go/pkg/channel"
	v1event "github.com/redhat-cne/sdk-go/v1/event"
	v1pubsub "github.com/redhat-cne/sdk-go/v1/pubsub"
)

var (
	eventOutCh        chan *channel.DataChan
	eventInCh         chan *channel.DataChan
	closeCh           chan bool
	restPort          int    = 8080
	apiPath           string = "/api/cloudNotifications/v1/"
	pubSubInstance    *v1pubsub.API
	storePath         string = "."
	amqpHost          string = "amqp:localhost:5672"
	channelBufferSize int    = 10
)

func init() {
	// amqp channels
	eventOutCh = make(chan *channel.DataChan, channelBufferSize)
	eventInCh = make(chan *channel.DataChan, channelBufferSize)
	closeCh = make(chan bool)
	//Read environment variables
	if aHost, ok := os.LookupEnv("AMQP_HOST"); ok && aHost != "" {
		amqpHost = aHost
	}
	if sPath, ok := os.LookupEnv("STORE_PATH"); ok && sPath != "" {
		storePath = sPath
	}
	if ePort := common.GetIntEnv("API_PORT"); ePort > 0 {
		restPort = ePort
	}
}

func main() {
	// init
	baseURL := &types.URI{URL: url.URL{Scheme: "http", Host: fmt.Sprintf("%s%d", "localhost", restPort), Path: apiPath}}
	pubSubInstance = v1pubsub.GetAPIInstance(storePath, baseURL)
	pl := plugins.Handler{Path: "./plugins"}
	wg := &sync.WaitGroup{}

	// load plugins
	if common.GetBoolEnv("AMQP_PLUGIN") {
		_, err := pl.LoadAMQPPlugin(wg, amqpHost, eventInCh, eventOutCh, closeCh)
		if err != nil {
			log.Fatalf("error loading amqp plugin %v", err)
		}
	}

	if common.GetBoolEnv("REST_PLUGIN") {
		_, err := pl.LoadRestPlugin(wg, restPort, apiPath, storePath, eventOutCh, closeCh)
		if err != nil {
			log.Fatalf("error loading reset service plugin %v", err)
		}
		err = common.EndPointHealthChk(fmt.Sprintf("http://%s:%d%shealth", "localhost", restPort, apiPath))
		if err != nil {
			log.Fatalf("error starting rest service %v", err)
		}
	}
	// assume this depends on rest plugin or you can use api to create subscriptions
	if common.GetBoolEnv("PTP_PLUGIN") {
		err := pl.LoadPTPPlugin(wg, pubSubInstance, eventInCh, closeCh, nil)
		if err != nil {
			log.Fatalf("error loading ptp plugin %v", err)
		}
	}

	log.Printf("waiting for events")
	processOutChannel()
}

//processOutChannel this process the out channel;data put out by amqp
func processOutChannel() {
	//qdr throws out the data on this channel ,listen to data coming out of qdrEventOutCh
	restClient := restclient.New()
	for { //nolint:gosimple
		select { //nolint:gosimple
		case d := <-eventOutCh: // do something that is put out by QDR
			log.Printf("processing data from amqp\n")
			// regular events
			event, err := v1event.GetCloudNativeEvents(*d.Data)
			if err != nil {
				log.Printf("Error marshalling event data when reading from amqp %v", err)
			} else if d.Type == channel.EVENT { //|always event or status| d.PubSubType == protocol.CONSUMER
				if d.Status == channel.NEW {
					if d.ProcessEventFn != nil { // always leave event to handle by default method for events
						if err := d.ProcessEventFn(event); err != nil {
							log.Printf("error processing data %v", err)
						}
					} else if sub, ok := pubSubInstance.HasSubscription(d.Address); ok {
						if sub.EndPointURI != nil {
							if err := restClient.PostEvent(sub.EndPointURI.String(), event); err != nil {
								log.Printf("error posting request at %s", sub.EndPointURI)
							}
						} else {
							log.Printf("endpoint uri  not given , posting event to log  %#v for address %s\n", event, d.Address)
						}
					} else {
						log.Printf("subscription not found posting event %#v to log  for address %s\n", event, d.Address)
					}
				} else if d.Status == channel.SUCCEED || d.Status == channel.FAILED { // send back to publisher
					log.Printf("send a acknowlegment to the address in uriLocation ")
				}
			}
		case <-closeCh:
			return
		}
	}
}
