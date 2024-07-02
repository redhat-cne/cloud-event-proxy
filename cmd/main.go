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

// Package main ...
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path"
	"sync"
	"syscall"
	"time"

	"github.com/redhat-cne/sdk-go/pkg/subscriber"
	v1pubs "github.com/redhat-cne/sdk-go/v1/pubsub"

	"github.com/redhat-cne/sdk-go/pkg/types"

	"github.com/prometheus/client_golang/prometheus/collectors"

	"github.com/redhat-cne/sdk-go/pkg/util/wait"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/redhat-cne/cloud-event-proxy/pkg/localmetrics"
	storageClient "github.com/redhat-cne/cloud-event-proxy/pkg/storage/kubernetes"

	log "github.com/sirupsen/logrus"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redhat-cne/cloud-event-proxy/pkg/common"
	"github.com/redhat-cne/cloud-event-proxy/pkg/plugins"
	"github.com/redhat-cne/cloud-event-proxy/pkg/restclient"
	apiMetrics "github.com/redhat-cne/rest-api/pkg/localmetrics"
	"github.com/redhat-cne/sdk-go/pkg/channel"
	sdkMetrics "github.com/redhat-cne/sdk-go/pkg/localmetrics"
	v1event "github.com/redhat-cne/sdk-go/v1/event"
	v1http "github.com/redhat-cne/sdk-go/v1/http"
)

var (
	// defaults
	storePath               string
	transportHost           string
	apiPort                 int
	apiVersion              string
	channelBufferSize       = 100
	statusChannelBufferSize = 50
	scConfig                *common.SCConfiguration
	metricsAddr             string
	apiPath                 = "/api/ocloudNotifications/v1/"
	// Event API Version 2 is O-RAN V3.0 Compliant
	apiPathV2          = "/api/ocloudNotifications/v2/"
	httpEventPublisher string
	pluginHandler      plugins.Handler
	nodeName           string
	namespace          string
	isV1Api            bool
)

func main() {
	// init
	common.InitLogger()
	flag.StringVar(&metricsAddr, "metrics-addr", ":9091", "The address the metric endpoint binds to.")
	flag.StringVar(&storePath, "store-path", ".", "The path to store publisher and subscription info.")
	flag.StringVar(&transportHost, "transport-host", "http://ptp-event-publisher-service-NODE_NAME.openshift-ptp.svc.cluster.local:9043", "The transport bus hostname or service name.")
	flag.IntVar(&apiPort, "api-port", 8089, "The address the rest api endpoint binds to.")
	flag.StringVar(&apiVersion, "api-version", "1.0", "The address the rest api endpoint binds to.")
	flag.StringVar(&httpEventPublisher, "http-event-publishers", "", "Comma separated address of the publishers available.")

	flag.Parse()

	// Register metrics
	localmetrics.RegisterMetrics()
	apiMetrics.RegisterMetrics()
	sdkMetrics.RegisterMetrics()

	// Including these stats kills performance when Prometheus polls with multiple targets
	prometheus.Unregister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	prometheus.Unregister(collectors.NewGoCollector())

	nodeIP := os.Getenv("NODE_IP")
	nodeName = os.Getenv("NODE_NAME")
	namespace = os.Getenv("NAME_SPACE")

	transportHost = common.SanitizeTransportHost(transportHost, nodeIP, nodeName)
	eventPublishers := updateHTTPPublishers(nodeIP, nodeName, httpEventPublisher)

	parsedTransportHost := &common.TransportHost{URL: transportHost}

	parsedTransportHost.ParseTransportHost()
	if parsedTransportHost.Err != nil {
		log.Errorf("error parsing transport host, data will written to log %s", parsedTransportHost.Err.Error())
	}
	scConfig = &common.SCConfiguration{
		EventInCh:     make(chan *channel.DataChan, channelBufferSize),
		EventOutCh:    make(chan *channel.DataChan, channelBufferSize),
		StatusCh:      make(chan *channel.StatusChan, statusChannelBufferSize),
		CloseCh:       make(chan struct{}),
		APIPort:       apiPort,
		APIPath:       apiPath,
		APIVersion:    apiVersion,
		StorePath:     storePath,
		PubSubAPI:     v1pubs.GetAPIInstance(storePath),
		BaseURL:       nil,
		TransportHost: parsedTransportHost,
		StorageType:   storageClient.EmptyDir,
	}
	/****/

	// Use kubeconfig to create client config.
	client, err := storageClient.NewClient()
	if err != nil {
		log.Infof("error fetching client, storage defaulted to emptyDir{} %s", err.Error())
	} else {
		scConfig.K8sClient = client
	}
	if namespace != "" && nodeName != "" && scConfig.TransportHost.Type == common.HTTP {
		// if consumer doesn't pass namespace then this will default to empty dir
		if e := client.InitConfigMap(scConfig.StorePath, nodeName, namespace); e != nil {
			log.Errorf("failed to initlialize configmap, subcription will be stored in empty dir %s", e.Error())
		} else {
			scConfig.StorageType = storageClient.ConfigMap
		}
	}
	metricServer(metricsAddr)
	wg := sync.WaitGroup{}
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	go func() {
		<-sigCh
		log.Info("exiting...")
		close(scConfig.CloseCh)
		os.Exit(1)
	}()

	pluginHandler = plugins.Handler{Path: "./plugins"}
	isV1Api = common.IsV1Api(scConfig.APIVersion)
	// make PubSub REST API accessible by event service ptp-event-publisher-service-NODE_NAME
	if !isV1Api {
		// switch between Internal API port and PubSub port
		tmpPort := scConfig.APIPort
		scConfig.APIPort = scConfig.TransportHost.Port
		scConfig.TransportHost.Port = tmpPort
		scConfig.APIPath = apiPathV2
		log.Infof("v2 rest api set scConfig.APIPort=%d, scConfig.APIPath=%s, scConfig.TransportHost.Port=%d", scConfig.APIPort, scConfig.APIPath, scConfig.TransportHost.Port)
	}

	var transportEnabled bool
	if scConfig.TransportHost.Type == common.HTTP && isV1Api {
		transportEnabled = enableHTTPTransport(&wg, eventPublishers)
	} else {
		transportEnabled = false
	}

	// if all transport types failed then process internally
	if !transportEnabled && isV1Api {
		log.Errorf("No transport is enabled for sending events %s", scConfig.TransportHost.String())
		wg.Add(1)
		go ProcessInChannel(&wg, scConfig)
	}

	/* Enable pub/sub services */
	err = common.StartPubSubService(scConfig)
	if err != nil {
		log.Fatal("pub/sub service API failed to start.")
	}

	// load all publishers or subscribers from the existing store files.
	loadFromPubSubStore()
	// assume this depends on rest plugin, or you can use api to create subscriptions
	if common.GetBoolEnv("PTP_PLUGIN") {
		if ptpPluginError := pluginHandler.LoadPTPPlugin(&wg, scConfig, nil); ptpPluginError != nil {
			log.Fatalf("error loading ptp plugin %v", err)
		}
	}

	if common.GetBoolEnv("MOCK_PLUGIN") {
		if mPluginError := pluginHandler.LoadMockPlugin(&wg, scConfig, nil); mPluginError != nil {
			log.Fatalf("error loading mock plugin %v", err)
		}
	}

	// process data that are coming from api server requests
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

// ProcessOutChannel this process the out channel;data put out by transport
func ProcessOutChannel(wg *sync.WaitGroup, scConfig *common.SCConfiguration) {
	// Send back the acknowledgement to publisher
	defer wg.Done()
	postProcessFn := func(address string, status channel.Status) {
		if pub, ok := scConfig.PubSubAPI.HasPublisher(address); ok {
			if status == channel.SUCCESS {
				localmetrics.UpdateEventAckCount(address, localmetrics.SUCCESS)
			} else {
				localmetrics.UpdateEventAckCount(address, localmetrics.FAILED)
			}
			if pub.EndPointURI != nil {
				log.Debugf("posting acknowledgment with status: %s to publisher: %s", status, pub.EndPointURI)
				restClient := restclient.New()
				_ = restClient.Post(pub.EndPointURI,
					[]byte(fmt.Sprintf(`{eventId:"%s",status:"%s"}`, pub.ID, status)))
			}
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
		case d := <-scConfig.EventOutCh: // do something that is put out by transporter
			if d.Type == channel.EVENT {
				if d.Data == nil {
					log.Errorf("nil event data was sent via event channel,ignoring")
					continue
				}
				event, err := v1event.GetCloudNativeEvents(*d.Data)
				if err != nil {
					log.Errorf("error marshalling event data when reading from transport %v\n %#v", err, d)
					log.Infof("data %#v", d.Data)
					continue
				} else if d.Status == channel.NEW {
					if d.ProcessEventFn != nil { // always leave event to handle by default method for events
						if err = d.ProcessEventFn(event); err != nil {
							log.Errorf("error processing data %v", err)
							localmetrics.UpdateEventReceivedCount(d.Address, localmetrics.FAILED)
						}
					} else if sub, ok := scConfig.PubSubAPI.HasSubscription(d.Address); ok {
						if sub.EndPointURI != nil {
							restClient := restclient.New()
							event.ID = sub.ID // set ID to the subscriptionID
							if common.IsV1Api(scConfig.APIVersion) {
								err = restClient.PostEvent(sub.EndPointURI, event)
							} else {
								err = restClient.PostCloudEvent(sub.EndPointURI, *d.Data)
							}
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
			} else if d.Type == channel.SUBSCRIBER { // these data are provided by HTTP transport
				if scConfig.StorageType != storageClient.ConfigMap {
					continue
				}
				if d.Status == channel.SUCCESS && d.Data != nil {
					var obj subscriber.Subscriber
					if err := json.Unmarshal(d.Data.Data(), &obj); err != nil {
						log.Infof("data is not subscriber object ignoring processing")
						continue
					}
					log.Infof("subscriber processed for %s", d.Address)
					if err := scConfig.K8sClient.UpdateConfigMap(context.Background(), []subscriber.Subscriber{obj}, nodeName, namespace); err != nil {
						log.Errorf("failed to update subscription in configmap %s", err.Error())
					} else {
						log.Infof("subscriber saved in configmap %s", obj.String())
					}
				} else if d.Status == channel.DELETE {
					var obj subscriber.Subscriber
					obj.Action = channel.DELETE
					obj.ClientID = d.ClientID
					if err := scConfig.K8sClient.UpdateConfigMap(context.Background(), []subscriber.Subscriber{obj}, nodeName, namespace); err != nil {
						log.Errorf("failed to delete subscription in configmap %s", err.Error())
					} else {
						log.Infof("deleted subscription %s ", obj.ClientID.String())
					}
				}
			}
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
			if d.Type == channel.SUBSCRIBER {
				log.Warnf("event transport disabled,no action taken: request to create listener address %s was called,but transport is not enabled", d.Address)
			} else if d.Type == channel.PUBLISHER {
				log.Warnf("no action taken: request to create sender for address %s was called,but transport is not enabled", d.Address)
			} else if d.Type == channel.EVENT && d.Status == channel.NEW {
				if e, err := v1event.GetCloudNativeEvents(*d.Data); err != nil {
					log.Warnf("error marshalling event data")
				} else {
					log.Warnf("event disabled,no action taken(can't send to a desitination): logging new event %s\n", e.JSONString())
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
				log.Warnf("event disabled,no action taken(can't send to a destination): logging new status check %v\n", d)
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
	if scConfig.TransportHost.Type == common.HTTP {
		subs := scConfig.PubSubAPI.GetSubscriptions() // the publisher won't have any subscription usually the consumer gets this publisher
		for _, sub := range subs {
			v1http.CreateSubscription(scConfig.EventInCh, sub.ID, sub.Resource)
		}
	}
}

func enableHTTPTransport(wg *sync.WaitGroup, eventPublishers []string) bool {
	log.Infof("HTTP enabled as event transport %s", scConfig.TransportHost.String())
	var httpServer *v1http.HTTP
	httpServer, err := pluginHandler.LoadHTTPPlugin(wg, scConfig, nil, nil)
	if err != nil {
		log.Warnf("failed to load http plugin for tansport %s", err.Error())
		scConfig.PubSubAPI.DisableTransport()
		return false
	}
	/*** wait till you get the publisher service running  */
	time.Sleep(5 * time.Second)
RETRY:
	tHost := types.ParseURI(scConfig.TransportHost.URI.String())
	if ok := httpServer.IsReadyToServe(tHost); !ok {
		goto RETRY
	}

	scConfig.TransPortInstance = httpServer
	// TODO: Need a Better way to know if this publisher or consumer
	if common.GetBoolEnv("PTP_PLUGIN") || common.GetBoolEnv("HW_PLUGIN") {
		httpServer.RegisterPublishers(types.ParseURI(scConfig.TransportHost.URL))
	}
	for _, s := range eventPublishers {
		if s == "" {
			continue
		}
		th := common.TransportHost{URL: s}
		th.ParseTransportHost()
		if th.URI != nil {
			s = th.URI.String()
			th.URI.Path = path.Join(th.URI.Path, "health")
			if ok, _ := common.HTTPTransportHealthCheck(th.URI, 2*time.Second); !ok {
				log.Warningf("health check failed for transport %s", s)
			}
			log.Infof("Registering publisher %s", s)
			httpServer.RegisterPublishers(types.ParseURI(s))
		} else {
			log.Errorf("failed to parse publisher url %s", s)
		}
	}

	log.Infof("following publishers are registered %s", eventPublishers)
	return true
}

func updateHTTPPublishers(nodeIP, nodeName string, addr ...string) (httpPublishers []string) {
	for _, s := range addr {
		if s == "" {
			continue
		}
		publisherServiceName := common.SanitizeTransportHost(s, nodeIP, nodeName)
		httpPublishers = append(httpPublishers, publisherServiceName)
		log.Infof("publisher endpoint updated from %s to %s", s, publisherServiceName)
	}
	return httpPublishers
}
