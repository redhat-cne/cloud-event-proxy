package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/redhat-cne/cloud-event-proxy/pkg/localmetrics"
	log "github.com/sirupsen/logrus"

	restapi "github.com/redhat-cne/rest-api"
	v1amqp "github.com/redhat-cne/sdk-go/v1/amqp"
	v1pubsub "github.com/redhat-cne/sdk-go/v1/pubsub"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redhat-cne/cloud-event-proxy/pkg/common"
	"github.com/redhat-cne/cloud-event-proxy/pkg/plugins"
	"github.com/redhat-cne/cloud-event-proxy/pkg/restclient"
	apimetrics "github.com/redhat-cne/rest-api/pkg/localmetrics"
	"github.com/redhat-cne/sdk-go/pkg/channel"
	sdkmetrics "github.com/redhat-cne/sdk-go/pkg/localmetrics"
	v1event "github.com/redhat-cne/sdk-go/v1/event"
)

var (
	//defaults
	storePath         string = "."
	amqpHost          string = "amqp:localhost:5672"
	apiPort           int    = 8080
	channelBufferSize int    = 10
	scConfig          *common.SCConfiguration
	pubSubAPI         *restapi.Server
	metricsAddr       string = ":9292"
	apiPath           string = "/api/cloudNotifications/v1/"
)

func main() {
	// init
	common.InitLogger()
	flag.StringVar(&metricsAddr, "metrics-addr", ":9433", "The address the metric endpoint binds to.")
	flag.StringVar(&storePath, "store-path", ".", "The path to store publisher and subscription info.")
	flag.StringVar(&amqpHost, "transport-host", "amqp:localhost:5672", "The transport bus hostname or service name.")
	flag.IntVar(&apiPort, "api-port", 8080, "The address the rest api endpoint binds to.")
	flag.Parse()

	// Register metrics
	localmetrics.RegisterMetrics()
	apimetrics.RegisterMetrics()
	sdkmetrics.RegisterMetrics()

	// Including these stats kills performance when Prometheus polls with multiple targets
	prometheus.Unregister(prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}))
	prometheus.Unregister(prometheus.NewGoCollector())

	scConfig = &common.SCConfiguration{
		EventInCh:  make(chan *channel.DataChan, channelBufferSize),
		EventOutCh: make(chan *channel.DataChan, channelBufferSize),
		CloseCh:    make(chan bool),
		APIPort:    apiPort,
		APIPath:    apiPath,
		PubSubAPI:  v1pubsub.GetAPIInstance(storePath),
		StorePath:  storePath,
		AMQPHost:   amqpHost,
		BaseURL:    nil,
	}

	wg := sync.WaitGroup{}
	wg.Add(1)
	go metricServer(metricsAddr)

	var err error
	pubSubAPI, err = common.StartPubSubService(&wg, scConfig)
	if err != nil {
		log.Fatal("pub/sub service API failed to start.")
	}

	pl := plugins.Handler{Path: "./plugins"}
	// load amqp
	_, err = pl.LoadAMQPPlugin(&wg, scConfig)
	if err != nil {
		log.Warnf("requires QPID router installed to function fully %s", err.Error())
		scConfig.PubSubAPI.DisableTransport()
	}

	// load all senders and listeners from the existing store files.
	loadFromPubSubStore()
	// assume this depends on rest plugin or you can use api to create subscriptions
	if common.GetBoolEnv("PTP_PLUGIN") {
		err := pl.LoadPTPPlugin(&wg, scConfig, nil)
		if err != nil {
			log.Fatalf("error loading ptp plugin %v", err)
		}
	}

	log.Info("ready...")
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
		go ProcessInChannel(&wg, scConfig)
	}
	ProcessOutChannel(&wg, scConfig)
}

func metricServer(address string) {
	log.Info("starting metrics")
	http.Handle("/metrics", promhttp.Handler())
	// always returns error. ErrServerClosed on graceful close
	if err := http.ListenAndServe(address, nil); err != http.ErrServerClosed {
		// unexpected error. port in use?
		log.Errorf("metrics server error: %v", err)
	}
}

//ProcessOutChannel this process the out channel;data put out by amqp
func ProcessOutChannel(wg *sync.WaitGroup, scConfig *common.SCConfiguration) {
	defer wg.Done()
	//qdr throws out the data on this channel ,listen to data coming out of qdrEventOutCh
	restClient := restclient.New()
	for { //nolint:gosimple
		select { //nolint:gosimple
		case d := <-scConfig.EventOutCh: // do something that is put out by QDR
			// regular events
			event, err := v1event.GetCloudNativeEvents(*d.Data)
			if err != nil {
				log.Errorf("error marshalling event data when reading from amqp %v\n %#v", err, d)
				log.Infof("data %#v", d.Data)
			} else if d.Type == channel.EVENT {
				if d.Status == channel.NEW {
					if d.ProcessEventFn != nil { // always leave event to handle by default method for events
						if err := d.ProcessEventFn(event); err != nil {
							log.Errorf("error processing data %v", err)
							localmetrics.UpdateEventReceivedCount(d.Address, localmetrics.FAILED)
						}
					} else if sub, ok := scConfig.PubSubAPI.HasSubscription(d.Address); ok {
						if sub.EndPointURI != nil {
							event.ID = sub.ID // set ID to the subscriptionID
							if err := restClient.PostEvent(sub.EndPointURI, event); err != nil {
								log.Errorf("error posting request at %s", sub.EndPointURI)
								localmetrics.UpdateEventReceivedCount(d.Address, localmetrics.FAILED)
							}
						} else {
							log.Warnf("endpoint uri not given, posting event to log %#v for address %s\n", event, d.Address)
							localmetrics.UpdateEventReceivedCount(d.Address, localmetrics.SUCCESS)
						}
					} else {
						log.Warnf("subscription not found, posting event %#v to log for address %s\n", event, d.Address)
						localmetrics.UpdateEventReceivedCount(d.Address, localmetrics.FAILED)
					}
				} else if d.Status == channel.SUCCESS || d.Status == channel.FAILED { // event sent ,ack back to publisher
					//Send back the acknowledgement to publisher
					if pub, ok := scConfig.PubSubAPI.HasPublisher(d.Address); ok {
						if d.Status == channel.SUCCESS {
							localmetrics.UpdateEventAckCount(d.Address, localmetrics.SUCCESS)
						} else {
							localmetrics.UpdateEventAckCount(d.Address, localmetrics.FAILED)
						}
						if pub.EndPointURI != nil {
							log.Debugf("posting event status %s to publisher %s", channel.Status(d.Status), pub.Resource)
							_ = restClient.Post(pub.EndPointURI,
								[]byte(fmt.Sprintf(`{eventId:"%s",status:"%s"}`, pub.ID, d.Status)))
						}
					} else {
						log.Warnf("could not send ack to publisher ,`publisher` for address %s not found", d.Address)
						localmetrics.UpdateEventAckCount(d.Address, localmetrics.FAILED)
					}
				}
			}
		case <-scConfig.CloseCh:
			return
		}
	}
}

//ProcessInChannel will be called if Transport is disabled
func ProcessInChannel(wg *sync.WaitGroup, scConfig *common.SCConfiguration) {
	defer wg.Done()
	for { //nolint:gosimple
		select {
		case d := <-scConfig.EventInCh:
			if d.Type == channel.LISTENER {
				log.Warnf("amqp disabled,no action taken: request to create listener address %s was called,but transport is not enabled", d.Address)
			} else if d.Type == channel.SENDER {
				log.Warnf("no action taken: request to create sender for address %s was called,but transport is not enabled", d.Address)
			} else if d.Type == channel.EVENT && d.Status == channel.NEW {
				if e, err := v1event.GetCloudNativeEvents(*d.Data); err != nil {
					log.Warnf("error marshalling event data")
				} else {
					log.Warnf("amqp disabled,no action taken(can't send to a desitination): logging new event %s\n", e.String())
				}
				out := channel.DataChan{
					Address:        d.Address,
					Data:           d.Data,
					Status:         channel.SUCCESS,
					Type:           channel.EVENT,
					ProcessEventFn: d.ProcessEventFn,
				}
				if d.OnReceiveOverrideFn != nil {
					if err := d.OnReceiveOverrideFn(*d.Data); err != nil {
						out.Status = channel.FAILED
					} else {
						out.Status = channel.SUCCESS
					}
				}
				scConfig.EventOutCh <- &out
			}
		case <-scConfig.CloseCh:
			return
		}
	}
}

func loadFromPubSubStore() {
	pubs := scConfig.PubSubAPI.GetPublishers()
	//apimetrics.UpdatePublisherCount(apimetrics.ACTIVE, len(pubs))
	for _, pub := range pubs {
		v1amqp.CreateSender(scConfig.EventInCh, pub.Resource)
	}
	subs := scConfig.PubSubAPI.GetSubscriptions()
	//apimetrics.UpdateSubscriptionCount(apimetrics.ACTIVE, len(subs))
	for _, sub := range subs {
		v1amqp.CreateListener(scConfig.EventInCh, sub.Resource)
	}
}
