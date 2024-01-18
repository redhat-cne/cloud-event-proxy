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

// Package main defines ptp-operator plugin
package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"

	"k8s.io/utils/pointer"

	"github.com/redhat-cne/sdk-go/pkg/event"

	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/ptp4lconf"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/redhat-cne/sdk-go/pkg/channel"
	"github.com/redhat-cne/sdk-go/pkg/event/ptp"

	v2 "github.com/cloudevents/sdk-go/v2"
	"github.com/redhat-cne/cloud-event-proxy/pkg/common"
	ptpSocket "github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/socket"
	ptpTypes "github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/types"
	v1amqp "github.com/redhat-cne/sdk-go/v1/amqp"
	v1http "github.com/redhat-cne/sdk-go/v1/http"
	log "github.com/sirupsen/logrus"

	ptpMetrics "github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/metrics"
	"github.com/redhat-cne/sdk-go/pkg/pubsub"
	"github.com/redhat-cne/sdk-go/pkg/types"
	v1pubs "github.com/redhat-cne/sdk-go/v1/pubsub"
)

const (
	eventSocket        = "/cloud-native/events.sock"
	ptpConfigDir       = "/var/run/"
	phc2sysProcessName = "phc2sys"
	ptp4lProcessName   = "ptp4l"
	ts2PhcProcessName  = "ts2phc"
	gnssProcessName    = "gnss"
	// ClockRealTime is the slave
	ClockRealTime = "CLOCK_REALTIME"
	// MasterClockType is the slave sync slave clock to master
	MasterClockType = "master"
)

var (
	resourcePrefix         = "/cluster/node/%s%s"
	publishers             = map[ptp.EventType]*ptpTypes.EventPublisherType{}
	config                 *common.SCConfiguration
	eventManager           *ptpMetrics.PTPEventManager
	notifyConfigDirUpdates chan *ptp4lconf.PtpConfigUpdate
	fileWatcher            *ptp4lconf.Watcher
)

// Start ptp plugin to process events,metrics and status, expects rest api available to create publisher and subscriptions
func Start(wg *sync.WaitGroup, configuration *common.SCConfiguration, fn func(e interface{}) error) error { //nolint:deadcode,unused
	// The name of NodePtpDevice CR for this node is equal to the node name
	nodeName := os.Getenv("NODE_NAME")
	if nodeName == "" {
		log.Error("cannot find NODE_NAME environment variable")
		return fmt.Errorf("cannot find NODE_NAME environment variable %s", nodeName)
	}

	config = configuration
	// register metrics type
	ptpMetrics.RegisterMetrics(nodeName)
	publishers = InitPubSubTypes()

	// 1. Create event Publication
	var err error
	for _, publisherType := range publishers {
		var pub pubsub.PubSub
		if pub, err = createPublisher(fmt.Sprintf(resourcePrefix, nodeName, string(publisherType.Resource))); err != nil {
			log.Errorf("failed to create a publisher %v", err)
			return err
		}

		publisherType.PubID = pub.ID
		publisherType.Pub = &pub
		log.Printf("Created publisher %v", pub)
	}

	// Initialize the Event Manager
	eventManager = ptpMetrics.NewPTPEventManager(resourcePrefix, publishers, nodeName, config)
	wg.Add(1)
	// create socket listener
	go listenToSocket(wg)
	// watch for ptp any config updates
	go eventManager.PtpConfigMapUpdates.WatchConfigMapUpdate(nodeName, configuration.CloseCh)

	// watch and monitor ptp4l config file creation and deletion
	// create a ptpConfigDir directory watcher for config changes
	notifyConfigDirUpdates = make(chan *ptp4lconf.PtpConfigUpdate)
	fileWatcher, err = ptp4lconf.NewPtp4lConfigWatcher(ptpConfigDir, notifyConfigDirUpdates)
	if err != nil {
		log.Errorf("cannot monitor ptp4l configuation folder at %s : %s", ptpConfigDir, err)
		return err
	}
	// process ptp4l conf file updates under /var/run/ptp4l.X.conf
	go processPtp4lConfigFileUpdates()

	// get threshold data on change of ptp config
	// update node profile when configmap changes
	go func() {
		for {
			select {
			case <-eventManager.PtpConfigMapUpdates.UpdateCh:
				log.Infof("updating ptp profile changes %d", len(eventManager.PtpConfigMapUpdates.NodeProfiles))
				// clean up
				if len(eventManager.PtpConfigMapUpdates.NodeProfiles) == 0 {
					log.Infof("Zero Profile to update: cleaning up threshold")
					eventManager.PtpConfigMapUpdates.DeleteAllPTPThreshold()
					for _, pConfig := range eventManager.Ptp4lConfigInterfaces {
						ptpMetrics.DeleteThresholdMetrics(pConfig.Profile)
					}
					// delete all metrics related to process
					ptpMetrics.DeleteProcessStatusMetricsForConfig(nodeName, "", "")
				} else {
					// updates
					eventManager.PtpConfigMapUpdates.UpdatePTPProcessOptions()
					eventManager.PtpConfigMapUpdates.UpdatePTPThreshold()
					for key, np := range eventManager.PtpConfigMapUpdates.EventThreshold {
						ptpMetrics.Threshold.With(prometheus.Labels{
							"threshold": "MinOffsetThreshold", "node": nodeName, "profile": key}).Set(float64(np.MinOffsetThreshold))
						ptpMetrics.Threshold.With(prometheus.Labels{
							"threshold": "MaxOffsetThreshold", "node": nodeName, "profile": key}).Set(float64(np.MaxOffsetThreshold))
						ptpMetrics.Threshold.With(prometheus.Labels{
							"threshold": "HoldOverTimeout", "node": nodeName, "profile": key}).Set(float64(np.HoldOverTimeout))
					}
				}
			case <-config.CloseCh:
				return
			}
		}
	}()

	// 2.Create Status Listener
	// method to be called when ping received
	//TODO support CurrentState for AMQ
	onReceiveOverrideFn := getCurrentStatOverrideFn()

	log.Infof("setting up status listener")
	if config.TransportHost.Type == common.AMQ {
		for _, pType := range publishers {
			baseURL := fmt.Sprintf(resourcePrefix, nodeName, string(pType.Resource))
			log.Infof("for %s", fmt.Sprintf("%s/%s", baseURL, "status"))
			v1amqp.CreateNewStatusListener(config.EventInCh, fmt.Sprintf("%s/%s", baseURL, "status"), onReceiveOverrideFn, fn)
		}
	} else if config.TransportHost.Type == common.HTTP {
		if httpInstance, ok := config.TransPortInstance.(*v1http.HTTP); ok {
			httpInstance.SetOnStatusReceiveOverrideFn(onReceiveOverrideFn)
		} else {
			log.Error("could not set receiver for http ")
		}
	}
	return nil
}

// getCurrentStatOverrideFn is called when current state is received by rest api
func getCurrentStatOverrideFn() func(e v2.Event, d *channel.DataChan) error {
	return func(e v2.Event, d *channel.DataChan) error {
		if e.Source() != "" {
			log.Infof("setting return address to %s", e.Source())
			d.ReturnAddress = pointer.String(e.Source())
		}
		log.Infof("got status check call,send events for subscriber %s => %s", d.ClientID.String(), e.Source())
		var eventType ptp.EventType
		if strings.Contains(e.Source(), string(ptp.PtpLockState)) {
			eventType = ptp.PtpStateChange
		} else if strings.Contains(e.Source(), string(ptp.OsClockSyncState)) {
			eventType = ptp.OsClockSyncStateChange
		} else if strings.Contains(e.Source(), string(ptp.PtpClockClass)) {
			eventType = ptp.PtpClockClassChange
		} else if strings.Contains(e.Source(), string(ptp.GnssSyncStatus)) {
			eventType = ptp.GnssStateChange
		} else {
			log.Warnf("could not find any events for requested resource type %s", e.Source())
			return fmt.Errorf("could not find any events for requested resource type %s", e.Source())
		}
		if len(eventManager.Stats) == 0 {
			data := eventManager.GetPTPEventsData(ptp.FREERUN, 0, "ptp-not-set", eventType)
			d.Data = eventManager.GetPTPCloudEvents(*data, eventType)
			return nil
		}
		// process events
		var data *event.Data
		processDataFn := func(data, d *event.Data) *event.Data {
			if data == nil {
				data = d
			} else if d != nil {
				data.Values = append(data.Values, d.Values...)
			}
			return data
		}

		for _, ptpInterfaces := range eventManager.Stats {
			for ptpInterface, s := range ptpInterfaces {
				if s.Alias() != "" {
					ptpInterface = ptpTypes.IFace(fmt.Sprintf("%s/%s", s.Alias(), ptpMetrics.MasterClockType))
				}
				switch ptpInterface {
				case ptpMetrics.MasterClockType:
					switch eventType {
					case ptp.PtpStateChange:
						// if its master stats then replace with slave interface(masked) +X
						data = processDataFn(data, eventManager.GetPTPEventsData(s.SyncState(), s.LastOffset(), string(ptpInterface), eventType))
					case ptp.PtpClockClassChange:
						clockClass := fmt.Sprintf("%s/%s", string(ptpInterface), ptpMetrics.ClockClass)
						data = processDataFn(data, eventManager.GetPTPEventsData(s.SyncState(), s.ClockClass(), clockClass, eventType))
					}
				case ptpMetrics.ClockRealTime:
					if eventType == ptp.OsClockSyncStateChange {
						data = processDataFn(data, eventManager.GetPTPEventsData(s.SyncState(), s.LastOffset(), string(ptpInterface), eventType))
					}
				default:
					switch eventType {
					case ptp.GnssStateChange:
						if s.HasProcessEnabled(gnssProcessName) { //gnss is with the master
							ptpInterface = ptpTypes.IFace(fmt.Sprintf("%s/%s", s.Alias(), ptpMetrics.MasterClockType))
							data = processDataFn(data, eventManager.GetPTPEventsData(s.SyncState(), s.LastOffset(), string(ptpInterface), eventType))
						}
					}
				}
			}
		}
		if data != nil {
			d.Data = eventManager.GetPTPCloudEvents(*data, eventType)
		} else {
			data = eventManager.GetPTPEventsData(ptp.FREERUN, 0, "event-not-found", eventType)
			d.Data = eventManager.GetPTPCloudEvents(*data, eventType)
			log.Errorf("could not find any events for requested resource type %s", e.Source())
			return nil
		}
		return nil
	}
}

// update interface and threshold details when ptpConfig change found
func processPtp4lConfigFileUpdates() {
	for {
		select {
		case ptpConfigEvent := <-notifyConfigDirUpdates:
			if ptpConfigEvent == nil || ptpConfigEvent.Name == nil {
				continue
			}
			log.Infof("updating ptp config changes for %s", *ptpConfigEvent.Name)
			switch ptpConfigEvent.Removed {
			case false: // create or modified
				// get config fileName
				ptpConfigFileName := ptpTypes.ConfigName(*ptpConfigEvent.Name)
				// read all interface names from the config
				newInterfaces := ptpConfigEvent.GetAllInterface()

				if _, ok := eventManager.Ptp4lConfigInterfaces[ptpConfigFileName]; !ok { // config file name is the key here
					eventManager.Ptp4lConfigInterfaces[ptpConfigFileName] = nil
				}
				// loop through to find if any interface changed
				if eventManager.Ptp4lConfigInterfaces[ptpConfigFileName] != nil &&
					HasEqualInterface(newInterfaces, eventManager.Ptp4lConfigInterfaces[ptpConfigFileName].Interfaces) {
					log.Infof("skipped update,interface not changed in ptl4lconfig")
					continue
				}

				// cleanup functions to check if interface exists
				isExists := func(i string) bool {
					for _, ptpInterface := range newInterfaces {
						if ptpInterface != nil && i == *ptpInterface {
							return true
						}
					}
					return false
				}
				// update ptp4lConfig
				ptp4lConfig := eventManager.GetPTPConfig(ptpConfigFileName)
				if ptp4lConfig != nil {
					for _, ptpInterface := range ptp4lConfig.Interfaces {
						if !isExists(ptpInterface.Name) {
							log.Errorf("config updated and interface not found, deleting %s", ptpInterface.Name)
							// Remove interface role metrics if the interface has been removed from ptpConfig
							ptpMetrics.DeleteInterfaceRoleMetrics("", ptpInterface.Name)
						}
					}
				}
				// build new interfaces and its order of index
				var ptpInterfaces []*ptp4lconf.PTPInterface
				for index, ptpInterface := range newInterfaces {
					role := ptpTypes.UNKNOWN
					if p, e := ptp4lConfig.ByInterface(*ptpInterface); e == nil && p.PortID == index+1 { // maintain role
						role = p.Role
					} // else new config order is not same
					ptpInterface := &ptp4lconf.PTPInterface{
						Name:     *ptpInterface,
						PortID:   index + 1,
						PortName: fmt.Sprintf("%s%d", "port ", index+1),
						Role:     role,
					}
					ptpInterfaces = append(ptpInterfaces, ptpInterface)
				}
				// updated ptp4lConfig is ready
				ptp4lConfig = &ptp4lconf.PTP4lConfig{
					Name:       string(ptpConfigFileName),
					Profile:    *ptpConfigEvent.Profile,
					Interfaces: ptpInterfaces,
				}
				// add to eventManager
				eventManager.AddPTPConfig(ptpConfigFileName, ptp4lConfig)
				// clean up process metrics
				for cName, opts := range eventManager.PtpConfigMapUpdates.PtpProcessOpts {
					var process []string
					if !opts.Ptp4lEnabled() {
						process = append(process, ptp4lProcessName)
					}
					if !opts.Phc2SysEnabled() {
						process = append(process, phc2sysProcessName)
					}
					ptpMetrics.DeleteProcessStatusMetricsForConfig(eventManager.NodeName(), cName,
						process...)
					// special ts2phc due to different configName replace ptp4l.0.conf to ts2phc cnf
					if !opts.TS2PhcEnabled() {
						ptpMetrics.DeleteProcessStatusMetricsForConfig(eventManager.NodeName(),
							strings.Replace(cName, ptp4lProcessName, ts2PhcProcessName, 1), ts2PhcProcessName)
					}
				}
			case true: // ptp4l.X.conf is deleted
				// delete metrics, ptp4l config is removed
				ptpConfigFileName := ptpTypes.ConfigName(*ptpConfigEvent.Name)
				ptpStats := eventManager.GetStats(ptpConfigFileName)
				ptpStats.SetConfigAsDeleted(true)
				// clean up
				if ptpConfig, ok := eventManager.Ptp4lConfigInterfaces[ptpConfigFileName]; ok {
					for _, ptpInterface := range ptpConfig.Interfaces {
						ptpMetrics.DeleteInterfaceRoleMetrics(ptp4lProcessName, ptpInterface.Name)
					}
					if t, ok2 := eventManager.PtpConfigMapUpdates.EventThreshold[ptpConfig.Profile]; ok2 {
						// Make sure that the function does close the channel
						t.SafeClose()
					}
					ptpMetrics.DeleteThresholdMetrics(ptpConfig.Profile)
					eventManager.PtpConfigMapUpdates.DeletePTPThreshold(ptpConfig.Profile)
				}
				//  offset related metrics are reported only for clock realtime and master
				// if ptp4l config is deleted ,  remove any metrics reported by it config
				// for dual nic, keep the CLOCK_REALTIME,if master interface not in same config
				if s, found := ptpStats[ClockRealTime]; found {
					ptpMetrics.DeletedPTPMetrics(s.OffsetSource(), phc2sysProcessName, ClockRealTime)
					eventManager.PublishEvent(ptp.FREERUN, ptpMetrics.FreeRunOffsetValue, ClockRealTime, ptp.OsClockSyncStateChange)
				}
				if s, found := ptpStats[MasterClockType]; found {
					if s.ProcessName() == ptp4lProcessName {
						ptpMetrics.DeletedPTPMetrics(s.OffsetSource(), ptp4lProcessName, s.Alias())
					} else {
						ptpMetrics.DeletedPTPMetrics(s.OffsetSource(), ts2PhcProcessName, s.Alias())
						for _, p := range ptpStats {
							if p.PtpDependentEventState() != nil {
								if p.HasProcessEnabled(gnssProcessName) {
									if ptpMetrics.NmeaStatus != nil {
										ptpMetrics.NmeaStatus.Delete(prometheus.Labels{
											"process": ts2PhcProcessName, "node": eventManager.NodeName(), "iface": p.Alias()})
									}
									masterResource := fmt.Sprintf("%s/%s", p.Alias(), MasterClockType)
									eventManager.PublishEvent(ptp.FREERUN, ptpMetrics.FreeRunOffsetValue, masterResource, ptp.GnssStateChange)
								}
							}
							if ptpMetrics.ClockClassMetrics != nil {
								ptpMetrics.ClockClassMetrics.Delete(prometheus.Labels{
									"process": ptp4lProcessName, "node": eventManager.NodeName()})
							}
							ptpMetrics.DeletedPTPMetrics(MasterClockType, ts2PhcProcessName, p.Alias())
							p.DeleteAllMetrics([]*prometheus.GaugeVec{ptpMetrics.PtpOffset, ptpMetrics.SyncState})
						}
					}
					masterResource := fmt.Sprintf("%s/%s", s.Alias(), MasterClockType)
					eventManager.PublishEvent(ptp.FREERUN, ptpMetrics.FreeRunOffsetValue, masterResource, ptp.PtpStateChange)
				}
				eventManager.DeleteStatsConfig(ptpConfigFileName)
				eventManager.DeletePTPConfig(ptpConfigFileName)
				// clean up process metrics
				ptpMetrics.DeleteProcessStatusMetricsForConfig(eventManager.NodeName(), string(ptpConfigFileName), ptp4lProcessName, phc2sysProcessName)
				ptpMetrics.DeleteProcessStatusMetricsForConfig(eventManager.NodeName(), strings.Replace(string(ptpConfigFileName), ptp4lProcessName, ts2PhcProcessName, 1), ts2PhcProcessName)
			}
		case <-config.CloseCh:
			fileWatcher.Close()
			return
		}
	}
}
func createPublisher(address string) (pub pubsub.PubSub, err error) {
	// this is loop back on server itself. Since current pod does not create any server
	returnURL := fmt.Sprintf("%s%s", config.BaseURL, "dummy")
	pubToCreate := v1pubs.NewPubSub(types.ParseURI(returnURL), address)
	pub, err = common.CreatePublisher(config, pubToCreate)
	if err != nil {
		log.Errorf("failed to create publisher %v", pub)
	}
	return pub, err
}

func listenToSocket(wg *sync.WaitGroup) {
	log.Info("establishing socket connection for metrics and events")
	defer wg.Done()
	l, sErr := ptpSocket.Listen(eventSocket)
	if sErr != nil {
		log.Errorf("error setting up socket %s", sErr)
		return
	}
	log.Info("connection established successfully")
	for {
		fd, err := l.Accept()
		if err != nil {
			log.Errorf("accept error: %s", err)
		} else {
			go processMessages(fd)
		}
	}
}

func processMessages(c net.Conn) {
	scanner := bufio.NewScanner(c)
	for {
		ok := scanner.Scan()
		if !ok {
			log.Error("error reading socket input, retrying")
			break
		}
		msg := scanner.Text()
		eventManager.ExtractMetrics(msg)
	}
}

// HasEqualInterface checks if configmap  changes has same interface
func HasEqualInterface(a []*string, b []*ptp4lconf.PTPInterface) bool {
	if len(a) != len(b) {
		return false
	}
	for i, v := range a {
		if *v != b[i].Name {
			return false
		}
	}
	return true
}

// InitPubSubTypes ... initialize types of publishers for ptp operator
func InitPubSubTypes() map[ptp.EventType]*ptpTypes.EventPublisherType {
	InitPubs := make(map[ptp.EventType]*ptpTypes.EventPublisherType)
	InitPubs[ptp.OsClockSyncStateChange] = &ptpTypes.EventPublisherType{
		EventType: ptp.OsClockSyncStateChange,
		Resource:  ptp.OsClockSyncState,
	}
	InitPubs[ptp.PtpClockClassChange] = &ptpTypes.EventPublisherType{
		EventType: ptp.PtpClockClassChange,
		Resource:  ptp.PtpClockClass,
	}
	InitPubs[ptp.PtpStateChange] = &ptpTypes.EventPublisherType{
		EventType: ptp.PtpStateChange,
		Resource:  ptp.PtpLockState,
	}
	InitPubs[ptp.GnssStateChange] = &ptpTypes.EventPublisherType{
		EventType: ptp.GnssStateChange,
		Resource:  ptp.GnssSyncStatus,
	}
	return InitPubs
}
