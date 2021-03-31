package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"path/filepath"
	"plugin"
	"sync"
	"time"

	restapi "github.com/redhat-cne/rest-api"
	"github.com/redhat-cne/sdk-go/pkg/channel"
	cneevent "github.com/redhat-cne/sdk-go/pkg/event"
	types "github.com/redhat-cne/sdk-go/pkg/types"
	v1amqp "github.com/redhat-cne/sdk-go/v1/amqp"
	v1event "github.com/redhat-cne/sdk-go/v1/event"
	v1pubsub "github.com/redhat-cne/sdk-go/v1/pubsub"
)

var (
	eventOutCh     chan *channel.DataChan
	eventInCh      chan *channel.DataChan
	close          chan bool
	restPort       int    = 8080
	apiPath        string = "/api/cnf/"
	pubSubInstance *v1pubsub.API
	server         *restapi.Server
)

func init() {
	//amqp channels
	eventOutCh = make(chan *channel.DataChan, 10)
	eventInCh = make(chan *channel.DataChan, 10)
	close = make(chan bool)

}
func main() {
	//plugins := []string{"plugins/amqp_plugin.so", "plugins/rest_api_plugin.so"}
	pubSubInstance = v1pubsub.GetAPIInstance(".")
	wg := &sync.WaitGroup{}
	//base con configuration we should be able to build this plugin
	loadAMQPPlugin(wg)
	server, _ = loadRestPlugin(wg)

	restHealthChk()
	//create publisher

	endpointURL := &types.URI{URL: url.URL{Scheme: "http", Host: "localhost:8080", Path: fmt.Sprintf("%s%s", apiPath, "dummy")}}
	pub, err := pubSubInstance.CreatePublisher(v1pubsub.NewPubSub(endpointURL, "test/test"))
	//if you are using amqp then need to create sender (note rest api by default creates sender)
	if err == nil {
		v1amqp.CreateSender(eventInCh, pub.GetResource())
		log.Printf("publisher %v", pub)
	} else {
		log.Printf("failed to create publisher transport")
	}

	endpointURL = &types.URI{URL: url.URL{Scheme: "http", Host: "localhost:8080", Path: fmt.Sprintf("%s%s", apiPath, "log")}}
	sub, err := pubSubInstance.CreateSubscription(v1pubsub.NewPubSub(endpointURL, "test/test"))
	if err == nil {
		v1amqp.CreateListener(eventInCh, pub.GetResource()) // (note rest api by default creates listener)
		log.Printf("publisher %v", sub)
	} else {
		log.Printf("failed to create subscription")
	}

	// create an event
	event := v1event.CloudNativeEvent()
	event.SetID(pub.ID)
	event.Type = "ptp_status_type"
	event.SetTime(types.Timestamp{Time: time.Now().UTC()}.Time)
	event.SetDataContentType(cneevent.ApplicationJSON)
	data := cneevent.Data{}
	value := cneevent.DataValue{
		Resource:  "/cluster/node/ptp",
		DataType:  cneevent.NOTIFICATION,
		ValueType: cneevent.ENUMERATION,
		Value:     cneevent.GNSS_ACQUIRING_SYNC,
	}
	data.SetVersion("v1")    //nolint:errcheck
	data.AppendValues(value) //nolint:errcheck
	event.SetData(data)

	// post this to API

	//or using methods to post here you don't have urilocation data
	cloudEvent, _ := v1event.CreateCloudEvents(event, pub)
	v1event.SendNewEventToDataChannel(eventInCh, pub.Resource, cloudEvent)
	log.Printf("waiting for the data ////")
	processOutChannel(wg)
	//received:=<-eventOutCh
	//log.Printf("Data received %#v",received)

	//2nd event
	//event.SetTime(types.Timestamp{Time: time.Now().UTC()}.Time)
	//panic("test panic")
	// here the subscribe reads and writes to log

	// now create event and watch log for data

	// create subscription
	//create event
	// check log

	//wg.Wait()
}

func processOutChannel(wg *sync.WaitGroup) {
	//qdr throws out the data on this channel ,listen to data coming out of qdrEventOutCh

	for { //nolint:gosimple
		select { //nolint:gosimple
		case d := <-eventOutCh: // do something that is put out by QDR
			//Special handle need to redesign
			// PTP status request ,get teh request data ask for PTP socket for data and send it back in its return address
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
		case <-close:
			return
		}
	}
}

func restHealthChk() {
	log.Printf("health check %s ", fmt.Sprintf("http://%s:%d%shealth", "localhost", restPort, apiPath))
	for {
		log.Printf("checking for rest service health")
		response, err := http.Get(fmt.Sprintf("http://%s:%d%shealth", "localhost", restPort, apiPath))
		if err != nil {
			log.Printf("error while checking health of the rest service %v", err)
			time.Sleep(2 * time.Second)
			continue
		}
		if response != nil && response.StatusCode == http.StatusOK {
			response.Body.Close()
			return
		}
		response.Body.Close()

		time.Sleep(2 * time.Second)
	}
}

func loadAMQPPlugin(wg *sync.WaitGroup) {
	log.Printf("Starting AMQP server")
	amqpPlugin, err := filepath.Glob("plugins/amqp_plugin.so")
	if err != nil {
		log.Fatalf("cannot load amqp plugin %v", err)
		return
	}
	p, err := plugin.Open(amqpPlugin[0])
	if err != nil {
		log.Fatalf("cannot open amqp plugin %v", err)
		return
	}
	symbol, err := p.Lookup("Start")
	if err != nil {
		log.Fatalf("cannot open amqp plugin start method %v", err)
		return
	}

	startFunc, ok := symbol.(func(wg *sync.WaitGroup, amqpHost string, dataIn <-chan *channel.DataChan, dataOut chan<- *channel.DataChan, close <-chan bool) (*v1amqp.AMQP, error))
	if !ok {
		log.Fatalf("Plugin has no 'Start(amqpHost string, dataIn <-chan channel.DataChan, dataOut chan<- channel.DataChan, close <-chan bool) (*v1_amqp.AMQP,error)' function")
		return
	}
	_, err = startFunc(wg, "amqp://localhost:5672", eventInCh, eventOutCh, close)
	if err != nil {
		log.Fatalf("error starting amqp.%v", err)
	}

}
func loadRestPlugin(wg *sync.WaitGroup) (*restapi.Server, error) {

	// The plugins (the *.so files) must be in a 'plugins' sub-directory
	//all_plugins, err := filepath.Glob("plugins/*.so")
	//if err != nil {
	//	panic(err)
	//}
	restPlugin, err := filepath.Glob("plugins/rest_api_plugin.so")
	if err != nil {
		log.Fatalf("cannot load rest plugin %v", err)
	}
	p, err := plugin.Open(restPlugin[0])
	if err != nil {
		log.Fatalf("cannot open rest plugin %v", err)
		return nil, err
	}
	symbol, err := p.Lookup("Start")
	if err != nil {
		log.Fatalf("cannot open rest plugin start method %v", err)
		return nil, err
	}

	startFunc, ok := symbol.(func(*sync.WaitGroup, int, string, string, chan<- *channel.DataChan, <-chan bool) *restapi.Server)
	if !ok {
		log.Fatalf("Plugin has no 'Start(int, string, string, chan<- channel.DataChan,<-chan bool)(*rest_api.Server)' function")
	}
	wg.Add(1)
	return startFunc(wg, restPort, apiPath, ".", eventInCh, close), nil

}
