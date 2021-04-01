package main

import (
	"fmt"
	"github.com/redhat-cne/cloud-event-proxy/pkg/common"
	"github.com/redhat-cne/cloud-event-proxy/pkg/plugins"
	"github.com/redhat-cne/cloud-event-proxy/pkg/restclient"
	"log"
	"os"
	"sync"

	"github.com/redhat-cne/sdk-go/pkg/channel"
	v1event "github.com/redhat-cne/sdk-go/v1/event"
	v1pubsub "github.com/redhat-cne/sdk-go/v1/pubsub"
)

var (
	eventOutCh     chan *channel.DataChan
	eventInCh      chan *channel.DataChan
	closeCh        chan bool
	restPort       int    = 8080
	apiPath        string = "/api/ocloudNotifications/v1/"
	pubSubInstance *v1pubsub.API
	storePath      string = "."
	amqpHost       string = "amqp:localhost:5672"
)

func init() {
	//amqp channels
	eventOutCh = make(chan *channel.DataChan, 10)
	eventInCh = make(chan *channel.DataChan, 10)
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

	//init
	pubSubInstance = v1pubsub.GetAPIInstance(storePath)
	pl := plugins.Handler{Path: "../plugins"}
	wg := &sync.WaitGroup{}

	//load plugins
	if common.GetBoolEnv("AMQP_PLUGIN") {
		err := pl.LoadAMQPPlugin(wg, amqpHost, eventInCh, eventOutCh, closeCh)
		if err != nil {
			log.Fatal("error loading amqp plugin")
		}
	}
	if common.GetBoolEnv("REST_PLUGIN") {
		_, err := pl.LoadRestPlugin(wg, restPort, apiPath, storePath, eventOutCh, closeCh)
		if err != nil {
			log.Fatal("error loading reset service plugin")
		}
		common.EndPointHealthChk(fmt.Sprintf("http://%s:%d%shealth", "localhost", restPort, apiPath))
	}
	log.Printf("waiting for events")
	processOutChannel(wg)
}

//processOutChannel this process the out channel;data put out by amqp
func processOutChannel(wg *sync.WaitGroup) {
	//qdr throws out the data on this channel ,listen to data coming out of qdrEventOutCh
	restClient := restclient.New()
	for { //nolint:gosimple
		select { //nolint:gosimple
		case d := <-eventOutCh: // do something that is put out by QDR
			log.Printf("processing data from amqp\n")
			//Special handle need to redesign
			// PTP status request ,get the request data ask for PTP socket for data and send it back in its return address
			if d.Type == channel.STATUS {
				if d.Status == channel.NEW {
					//wg.Add(1)
					//go processStatus(&wg, d)
					//TODO:HANDLE STATUS
					log.Printf("handling new status here")
				} else if d.Status == channel.SUCCEED {
					log.Printf("handling success status here (acknowledgment")
				}
				continue
			}
			// regular events
			event, err := v1event.GetCloudNativeEvents(*d.Data)
			if err != nil {
				log.Printf("Error marshalling event data when reading from amqp %v", err)
			} else {
				// find the endpoint you need to post
				if d.Type == channel.EVENT { //|always event or status| d.PubSubType == protocol.CONSUMER
					if d.Status == channel.NEW {
						if sub, ok := pubSubInstance.HasSubscription(d.Address); ok {
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
			}
		case <-closeCh:
			return
		}
	}
}
