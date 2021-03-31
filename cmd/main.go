package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/redhat-cne/cloud-event-proxy/pkg/common"
	"github.com/redhat-cne/cloud-event-proxy/pkg/plugins"

	restapi "github.com/redhat-cne/rest-api"
	"github.com/redhat-cne/sdk-go/pkg/channel"
	v1event "github.com/redhat-cne/sdk-go/v1/event"
	v1pubsub "github.com/redhat-cne/sdk-go/v1/pubsub"
)

var (
	eventOutCh     chan *channel.DataChan
	eventInCh      chan *channel.DataChan
	closeCh          chan bool
	restPort       int    = 8080
	apiPath        string = "/api/cnf/"
	pubSubInstance *v1pubsub.API
	server         *restapi.Server
	storePath      string = "."
	amqpHost       string = "amqp:localhost:5672"
)

func init() {
	//amqp channels
	eventOutCh = make(chan *channel.DataChan, 10)
	eventInCh = make(chan *channel.DataChan, 10)
	closeCh = make(chan bool)

}
func main() {
	//plugins := []string{"plugins/amqp_plugin.so", "plugins/rest_api_plugin.so"}
	pubSubInstance = v1pubsub.GetAPIInstance(".")
	pl:=plugins.PluginLoader{Path:"../plugins"}
	wg := &sync.WaitGroup{}
	//based on configuration we should be able to build this plugin
	//TODO:  read metadata from env and configure plugin accordingly
	pl.LoadAMQPPlugin(wg, amqpHost, eventInCh, eventOutCh, closeCh)
	server, _ = pl.LoadRestPlugin(wg, restPort, apiPath, storePath, eventOutCh, closeCh)
	common.EndPointHealthChk(fmt.Sprintf("http://%s:%d%shealth", "localhost", restPort, apiPath))
	log.Printf("waiting for events")
	processOutChannel(wg)
}

//processOutChannel this process the out channel;data put out by amqp
func processOutChannel(wg *sync.WaitGroup) {
	//qdr throws out the data on this channel ,listen to data coming out of qdrEventOutCh

	for { //nolint:gosimple
		select { //nolint:gosimple
		case d := <-eventOutCh: // do something that is put out by QDR
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
			// regular  events
			event, err := v1event.GetCloudNativeEvents(*d.Data)
			if err != nil {
				log.Printf("Error marshalling event data when reading from amqp %v", err)
			} else {
				log.Printf("the channel data %#v", d)
				// find the endpoint you need to post
				if d.Type == channel.EVENT { //|always event or status| d.PubSubType == protocol.CONSUMER
					if d.Status == channel.NEW {
						if sub, ok := pubSubInstance.HasSubscription(d.Address); ok {
							if sub.EndPointURI != nil {
								b, err := json.Marshal(event)
								if err != nil {
									log.Printf("error posting event %v", event)
								} else {
									timeout := time.Duration(5 * time.Second)
									client := http.Client{
										Timeout: timeout,
									}
									log.Printf("posting to http")
									request, err := http.NewRequest("POST", fmt.Sprintf("%s%s", server.GetHostPath(), "log"), bytes.NewBuffer(b))
									request.Header.Set("content-type", "application/json")
									if err != nil {
										log.Printf("error creating request %v", err)
									} else {
										response, err := client.Do(request)

										if err == nil {
											log.Printf("Posted event to %s with status code %d", request.URL.String(), response.StatusCode)

										} else {
											log.Printf("error posting  request %s", request.URL.String())

										}
									}
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
