package main

import (
	"fmt"
	restapi "github.com/redhat-cne/rest-api"
	v1amqp "github.com/redhat-cne/sdk-go/v1/amqp"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/redhat-cne/cloud-event-proxy/pkg/common"
	"github.com/redhat-cne/cloud-event-proxy/pkg/plugins"
	"github.com/redhat-cne/cloud-event-proxy/pkg/restclient"
	"github.com/redhat-cne/sdk-go/pkg/channel"
	v1event "github.com/redhat-cne/sdk-go/v1/event"
	v1pubsub "github.com/redhat-cne/sdk-go/v1/pubsub"
)

var (
	//defaults
	storePath         string = "."
	amqpHost          string = "amqp:localhost:5672"
	apiPort           int    = 8080
	channelBufferSize int    = 10
	scConfig          *common.SCConfiguration
	pubSubAPI         *restapi.Server
)

func init() {
	//Read environment variables
	if aHost, ok := os.LookupEnv("AMQP_HOST"); ok && aHost != "" {
		amqpHost = aHost
	}
	if sPath, ok := os.LookupEnv("STORE_PATH"); ok && sPath != "" {
		storePath = sPath
	}
	if ePort := common.GetIntEnv("API_PORT"); ePort > 0 {
		apiPort = ePort
	}
	scConfig = &common.SCConfiguration{
		EventInCh:  make(chan *channel.DataChan, channelBufferSize),
		EventOutCh: make(chan *channel.DataChan, channelBufferSize),
		CloseCh:    make(chan bool),
		APIPort:    apiPort,
		APIPath:    "/api/cloudNotifications/v1/",
		PubSubAPI:  v1pubsub.GetAPIInstance(storePath),
		StorePath:  storePath,
		AMQPHost:   amqpHost,
		BaseURL:    nil,
	}

}

func main() {
	// init
	wg := sync.WaitGroup{}
	var err error
	pubSubAPI, err = common.StartPubSubService(&wg, scConfig)
	if err != nil {
		log.Fatal("pub/sub service API failed to start.")
	}
	pl := plugins.Handler{Path: "./plugins"}
	// load amqp
	_, err = pl.LoadAMQPPlugin(&wg, scConfig)
	if err != nil {
		log.Printf("requires Qpid router installed to function fully %s", err.Error())
		scConfig.PubSubAPI.DisableTransport()
	}
	// load all senders and listeners from the existing store files.
	loadAllSendersAndListeners()
	// assume this depends on rest plugin or you can use api to create subscriptions
	if common.GetBoolEnv("PTP_PLUGIN") {
		err := pl.LoadPTPPlugin(&wg, scConfig, nil)
		if err != nil {
			log.Fatalf("error loading ptp plugin %v", err)
		}
	}
	log.Printf("ready...")
	c := make(chan os.Signal)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		//clean up
		scConfig.CloseCh <- true
		pubSubAPI.Shutdown()
		os.Exit(1)
	}()
	if !scConfig.PubSubAPI.HasTransportEnabled() {
		wg.Add(1)
		go ProcessInChannel(&wg)
	}
	ProcessOutChannel()
}

//ProcessOutChannel this process the out channel;data put out by amqp
func ProcessOutChannel() {
	//qdr throws out the data on this channel ,listen to data coming out of qdrEventOutCh
	restClient := restclient.New()
	for { //nolint:gosimple
		select { //nolint:gosimple
		case d := <-scConfig.EventOutCh: // do something that is put out by QDR
			// regular events
			event, err := v1event.GetCloudNativeEvents(*d.Data)
			if err != nil {
				log.Printf("error marshalling event data when reading from amqp %v\n %#v", err, d)
				log.Printf("data %#v", d.Data)
			} else if d.Type == channel.EVENT {
				if d.Status == channel.NEW {
					if d.ProcessEventFn != nil { // always leave event to handle by default method for events
						if err := d.ProcessEventFn(event); err != nil {
							log.Printf("error processing data %v", err)
						}
					} else if sub, ok := scConfig.PubSubAPI.HasSubscription(d.Address); ok {
						if sub.EndPointURI != nil {
							event.ID = sub.ID // set ID to the subscriptionID
							if err := restClient.PostEvent(sub.EndPointURI, event); err != nil {
								log.Printf("error posting request at %s", sub.EndPointURI)
							}
						} else {
							log.Printf("endpoint uri not given, posting event to log %#v for address %s\n", event, d.Address)
						}
					} else {
						log.Printf("subscription not found posting event %#v to log for address %s\n", event, d.Address)
					}
				} else if d.Status == channel.SUCCEED || d.Status == channel.FAILED { // send back to publisher
					//Send back the acknowledgement to publisher
					if pub, ok := scConfig.PubSubAPI.HasPublisher(d.Address); ok {
						if pub.EndPointURI != nil {
							log.Printf("posting event status %s to publisher %s", channel.Status(d.Status), pub.Resource)
							_ = restClient.Post(pub.EndPointURI,
								[]byte(fmt.Sprintf(`{eventId:"%s",status:"%s"}`, pub.ID, d.Status)))
						}
					} else {
						log.Printf("could not send ack to publisher ,`publisher` for address %s not found", d.Address)
					}
				}
			}
		case <-scConfig.CloseCh:
			return
		}
	}
}

//ProcessInChannel will be called if Transport is disabled
func ProcessInChannel(wg *sync.WaitGroup) {
	defer wg.Done()
	for { //nolint:gosimple
		select {
		case d := <-scConfig.EventInCh:
			if d.Type == channel.LISTENER {
				log.Printf("amqp disabled,no action taken: request to create listener address %s was called,but transport is not enabled", d.Address)
			} else if d.Type == channel.SENDER {
				log.Printf("no action taken: request to create sender for address %s was called,but transport is not enabled", d.Address)
			} else if d.Type == channel.EVENT && d.Status == channel.NEW {
				if e, err := v1event.GetCloudNativeEvents(*d.Data); err != nil {
					log.Printf("error marshalling event data")
				} else {
					log.Printf("amqp disabled,no action taken(can't send to a desitination): logging new event %s\n", e.String())
				}
				out := channel.DataChan{
					Address:        d.Address,
					Data:           d.Data,
					Status:         channel.SUCCEED,
					Type:           channel.EVENT,
					ProcessEventFn: d.ProcessEventFn,
				}
				if d.OnReceiveOverrideFn != nil {
					if err := d.OnReceiveOverrideFn(*d.Data); err != nil {
						out.Status = channel.FAILED
					} else {
						out.Status = channel.SUCCEED
					}
				}
				scConfig.EventOutCh <- &out
			}
		case <-scConfig.CloseCh:
			return
		}
	}
}

func loadAllSendersAndListeners() {
	for _, pub := range scConfig.PubSubAPI.GetPublishers() {
		v1amqp.CreateSender(scConfig.EventInCh, pub.Resource)
	}
	for _, sub := range scConfig.PubSubAPI.GetSubscriptions() {
		v1amqp.CreateListener(scConfig.EventInCh, sub.Resource)
	}
}
