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

package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/redhat-cne/sdk-go/pkg/types"

	"github.com/prometheus/client_golang/prometheus/collectors"

	"github.com/redhat-cne/sdk-go/pkg/util/wait"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/redhat-cne/cloud-event-proxy/pkg/localmetrics"
	log "github.com/sirupsen/logrus"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redhat-cne/cloud-event-proxy/pkg/common"
	"github.com/redhat-cne/cloud-event-proxy/pkg/plugins"
	"github.com/redhat-cne/cloud-event-proxy/pkg/restclient"
	apiMetrics "github.com/redhat-cne/rest-api/pkg/localmetrics"
	"github.com/redhat-cne/sdk-go/pkg/channel"
	sdkMetrics "github.com/redhat-cne/sdk-go/pkg/localmetrics"
	v1amqp "github.com/redhat-cne/sdk-go/v1/amqp"
	v1event "github.com/redhat-cne/sdk-go/v1/event"
	v1hwevent "github.com/redhat-cne/sdk-go/v1/hwevent"
	v1pubs "github.com/redhat-cne/sdk-go/v1/pubsub"
)

var (
	//defaults
	storePath         string
	amqpHost          string
	apiPort           int
	channelBufferSize = 100
	scConfig          *common.SCConfiguration
	metricsAddr       string
	apiPath           = "/api/cloudNotifications/v1/"
)

func main() {
	// init
	common.InitLogger()
	flag.StringVar(&metricsAddr, "metrics-addr", ":9091", "The address the metric endpoint binds to.")
	flag.StringVar(&storePath, "store-path", ".", "The path to store publisher and subscription info.")
	flag.StringVar(&amqpHost, "transport-host", "amqp:localhost:5672", "The transport bus hostname or service name.")
	flag.IntVar(&apiPort, "api-port", 8080, "The address the rest api endpoint binds to.")

	flag.Parse()

	// Register metrics
	localmetrics.RegisterMetrics()
	apiMetrics.RegisterMetrics()
	sdkMetrics.RegisterMetrics()

	// Including these stats kills performance when Prometheus polls with multiple targets
	prometheus.Unregister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	prometheus.Unregister(collectors.NewGoCollector())

	scConfig = &common.SCConfiguration{
		EventInCh:  make(chan *channel.DataChan, channelBufferSize),
		EventOutCh: make(chan *channel.DataChan, channelBufferSize),
		CloseCh:    make(chan struct{}),
		APIPort:    apiPort,
		APIPath:    apiPath,
		PubSubAPI:  v1pubs.GetAPIInstance(storePath),
		StorePath:  storePath,
		AMQPHost:   amqpHost,
		BaseURL:    nil,
	}

	metricServer(metricsAddr)
	wg := sync.WaitGroup{}
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	go func() {
		<-sigCh
		log.Info("exiting...")
		close(scConfig.CloseCh)
		//wg.Wait()
		os.Exit(1)
	}()

	_, err := common.StartPubSubService(scConfig)
	if err != nil {
		log.Fatal("pub/sub service API failed to start.")
	}

	pl := plugins.Handler{Path: "./plugins"}
	// load amqp
	_, err = pl.LoadAMQPPlugin(&wg, scConfig)
	if err != nil {
		log.Warnf("requires QPID router installed to function fully %s", err.Error())
		scConfig.PubSubAPI.DisableTransport()
		wg.Add(1)
		go ProcessInChannel(&wg, scConfig)
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

	if common.GetBoolEnv("MOCK_PLUGIN") {
		err := pl.LoadMockPlugin(&wg, scConfig, nil)
		if err != nil {
			log.Fatalf("error loading mock plugin %v", err)
		}
	}
	ProcessOutChannel(&wg, scConfig)
}

func metricServer(address string) {
	log.Info("starting metrics")
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())

	go wait.Until(func() {
		err := http.ListenAndServe(address, mux)
		if err != nil {
			log.Errorf("error with metrics server %s\n, will retry to establish", err.Error())
		}
	}, 5*time.Second, scConfig.CloseCh)
}

// ProcessOutChannel this process the out channel;data put out by amqp
func ProcessOutChannel(wg *sync.WaitGroup, scConfig *common.SCConfiguration) {
	//qdr throws out the data on this channel ,listen to data coming out of qdrEventOutCh
	//Send back the acknowledgement to publisher
	postProcessFn := func(address string, status channel.Status) {
		if pub, ok := scConfig.PubSubAPI.HasPublisher(address); ok {
			if status == channel.SUCCESS {
				localmetrics.UpdateEventAckCount(address, localmetrics.SUCCESS)
			} else {
				localmetrics.UpdateEventAckCount(address, localmetrics.FAILED)
			}
			if pub.EndPointURI != nil {
				log.Debugf("posting event status %s to publisher %s", channel.Status(status), pub.Resource)
				restClient := restclient.New()
				_ = restClient.Post(pub.EndPointURI,
					[]byte(fmt.Sprintf(`{eventId:"%s",status:"%s"}`, pub.ID, status)))
			}
		} else {
			log.Warnf("could not send ack to publisher ,`publisher` for address %s not found", address)
			localmetrics.UpdateEventAckCount(address, localmetrics.FAILED)
		}
	}
	postHandler := func(err error, endPointURI *types.URI, address string) {
		if err != nil {
			log.Errorf("error posting request at %s : %s", endPointURI, err)
			localmetrics.UpdateEventReceivedCount(address, localmetrics.FAILED)
		} else {
			localmetrics.UpdateEventReceivedCount(address, localmetrics.SUCCESS)
		}
	}

	for { //nolint:gosimple
		select { //nolint:gosimple
		case d := <-scConfig.EventOutCh: // do something that is put out by QDR
			switch d.Data.Type() {
			case channel.HWEvent:
				event, err := v1hwevent.GetCloudNativeEvents(*d.Data)
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
								restClient := restclient.New()
								event.ID = sub.ID // set ID to the subscriptionID
								err := restClient.PostHwEvent(sub.EndPointURI, event)
								postHandler(err, sub.EndPointURI, d.Address)
							} else {
								log.Warnf("endpoint uri not given, posting event to log %#v for address %s\n", event, d.Address)
								localmetrics.UpdateEventReceivedCount(d.Address, localmetrics.SUCCESS)
							}
						} else {
							log.Warnf("subscription not found, posting event %#v to log for address %s\n", event, d.Address)
							localmetrics.UpdateEventReceivedCount(d.Address, localmetrics.FAILED)
						}
					} else if d.Status == channel.SUCCESS || d.Status == channel.FAILED { // event sent ,ack back to publisher
						postProcessFn(d.Address, d.Status)
					}
				}
			default:
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
								restClient := restclient.New()
								event.ID = sub.ID // set ID to the subscriptionID
								err := restClient.PostEvent(sub.EndPointURI, event)
								postHandler(err, sub.EndPointURI, d.Address)
							} else {
								log.Warnf("endpoint uri not given, posting event to log %#v for address %s\n", event, d.Address)
								localmetrics.UpdateEventReceivedCount(d.Address, localmetrics.SUCCESS)
							}
						} else {
							log.Warnf("subscription not found, posting event %#v to log for address %s\n", event, d.Address)
							localmetrics.UpdateEventReceivedCount(d.Address, localmetrics.FAILED)
						}
					} else if d.Status == channel.SUCCESS || d.Status == channel.FAILED { // event sent ,ack back to publisher
						postProcessFn(d.Address, d.Status)
					}
				} else if d.Type == channel.STATUS {
					if d.Status == channel.SUCCESS {
						localmetrics.UpdateStatusAckCount(d.Address, localmetrics.SUCCESS)
					} else {
						log.Errorf("failed to receive status request to address %s", d.Address)
						localmetrics.UpdateStatusAckCount(d.Address, localmetrics.FAILED)
					}
				}
			} // end switch
		case <-scConfig.CloseCh:
			return
		}
	}
}

// ProcessInChannel will be called if Transport is disabled
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
					if err := d.OnReceiveOverrideFn(*d.Data, &out); err != nil {
						log.Errorf("error onReceiveOverrideFn %s", err)
						out.Status = channel.FAILED
					} else {
						out.Status = channel.SUCCESS
					}
				}
				scConfig.EventOutCh <- &out
			} else if d.Type == channel.STATUS && d.Status == channel.NEW {
				log.Warnf("amqp disabled,no action taken(can't send to a destination): logging new status check %v\n", d)
				out := channel.DataChan{
					Address:        d.Address,
					Data:           d.Data,
					Status:         channel.SUCCESS,
					Type:           channel.EVENT,
					ProcessEventFn: d.ProcessEventFn,
				}
				if d.OnReceiveOverrideFn != nil {
					if err := d.OnReceiveOverrideFn(*d.Data, &out); err != nil {
						log.Errorf("error onReceiveOverrideFn %s", err)
						out.Status = channel.FAILED
					} else {
						out.Status = channel.SUCCESS
					}
				}
			}
		case <-scConfig.CloseCh:
			return
		}
	}
}

func loadFromPubSubStore() {
	pubs := scConfig.PubSubAPI.GetPublishers()
	//apiMetrics.UpdatePublisherCount(apiMetrics.ACTIVE, len(pubs))
	for _, pub := range pubs {
		v1amqp.CreateSender(scConfig.EventInCh, pub.Resource)
	}
	subs := scConfig.PubSubAPI.GetSubscriptions()
	//apiMetrics.UpdateSubscriptionCount(apiMetrics.ACTIVE, len(subs))
	for _, sub := range subs {
		v1amqp.CreateListener(scConfig.EventInCh, sub.Resource)
	}
}
