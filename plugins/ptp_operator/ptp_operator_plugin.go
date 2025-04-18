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
	"path"
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
	syncE4lProcessName = "synce4l"
	// ClockRealTime is the slave
	ClockRealTime = "CLOCK_REALTIME"
	// MasterClockType is the slave sync slave clock to master
	MasterClockType = "master"
)

var (
	resourcePrefix         = "/cluster/node"
	publishers             = map[ptp.EventType]*ptpTypes.EventPublisherType{}
	config                 *common.SCConfiguration
	eventManager           *ptpMetrics.PTPEventManager
	notifyConfigDirUpdates chan *ptp4lconf.PtpConfigUpdate
	fileWatcher            *ptp4lconf.Watcher
)

// Start ptp plugin to process events,metrics and status, expects rest api available to create publisher and subscriptions
func Start(wg *sync.WaitGroup, configuration *common.SCConfiguration, _ func(e interface{}) error) error { //nolint:deadcode,unused
	// The name of NodePtpDevice CR for this node is equal to the node name
	nodeName := os.Getenv("NODE_NAME")
	if nodeName == "" {
		log.Error("cannot find NODE_NAME environment variable")
		return fmt.Errorf("cannot find NODE_NAME environment variable %s", nodeName)
	}

	config = configuration
	// register metrics type
	ptpMetrics.RegisterMetrics(nodeName)
	publishers = InitPubSubTypes(config)

	// 1. Create event Publication
	var err error
	for _, publisherType := range publishers {
		var pub pubsub.PubSub
		if pub, err = createPublisher(path.Join(resourcePrefix, nodeName, string(publisherType.Resource))); err != nil {
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
	go eventManager.PtpConfigMapUpdates.WatchConfigMapUpdate(nodeName, configuration.CloseCh, false)
	// watch and monitor ptp4l config file creation and deletion
	// create a ptpConfigDir directory watcher for config changes
	notifyConfigDirUpdates = make(chan *ptp4lconf.PtpConfigUpdate, 20) // max 20 profiles since all files will be deleted in reapply
	fileWatcher, err = ptp4lconf.NewPtp4lConfigWatcher(ptpConfigDir, notifyConfigDirUpdates)
	if err != nil {
		log.Errorf("cannot monitor ptp4l configuation folder at %s : %s", ptpConfigDir, err)
		return err
	}

	// process ptp4l conf file updates under /var/run/ptp4l.X.conf
	go processPtp4lConfigFileUpdates()

	// Reading configmap on any updates to configmap
	// get threshold data on change of ptp config
	// update node profile when configmap changes
	go func() {
		for {
			select {
			case <-eventManager.PtpConfigMapUpdates.UpdateCh:
				log.Infof("updating ptp profile changes %d", len(eventManager.PtpConfigMapUpdates.NodeProfiles))
				// clean up when no profiles found in configmap
				if len(eventManager.PtpConfigMapUpdates.NodeProfiles) == 0 {
					log.Infof("Zero Profile to update: cleaning up threshold")
					eventManager.PtpConfigMapUpdates.DeleteAllPTPThreshold()
					for _, pConfig := range eventManager.Ptp4lConfigInterfaces {
						ptpMetrics.DeleteThresholdMetrics(pConfig.Profile)
					}
					// delete all metrics related to process
					ptpMetrics.DeleteProcessStatusMetricsForConfig(nodeName, "", "")
					// delete all metrics related to ptp ha if haProfile is deleted
					if _, haProfiles := eventManager.HAProfiles(); len(haProfiles) > 0 {
						for _, p := range haProfiles {
							ptpMetrics.DeletePTPHAMetrics(strings.TrimSpace(p))
						}
					}
				} else {
					// updates found in configmap
					eventManager.PtpConfigMapUpdates.UpdatePTPProcessOptions()
					eventManager.PtpConfigMapUpdates.UpdatePTPThreshold()
					eventManager.PtpConfigMapUpdates.UpdatePTPSetting()
					// if profile 1 is removed for ha then remove those metrics
					// if hasProfile is not found then we need to remove all metrics for HA
					// this is done by file watcher
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
	onReceiveOverrideFn := getCurrentStatOverrideFn()

	log.Infof("setting up status listener")
	if config.TransportHost.Type == common.HTTP {
		if httpInstance, ok := config.TransPortInstance.(*v1http.HTTP); ok {
			httpInstance.SetOnStatusReceiveOverrideFn(onReceiveOverrideFn)
		} else {
			log.Error("could not set receiver for http ")
		}
	}
	if !common.IsV1Api(config.APIVersion) {
		config.RestAPI.SetOnStatusReceiveOverrideFn(onReceiveOverrideFn)
	}
	return nil
}

// getCurrentStatOverrideFn is called when GET CurrentState request is received by rest api
func getCurrentStatOverrideFn() func(e v2.Event, d *channel.DataChan) error {
	return func(e v2.Event, d *channel.DataChan) error {
		var err error
		if e.Source() != "" {
			d.ReturnAddress = pointer.String(e.Source())
		}
		log.Infof("got status check call,send events for subscriber %s => %s", d.ClientID.String(), e.Source())
		var eventType ptp.EventType
		var eventSource ptp.EventResource
		if strings.Contains(e.Source(), string(ptp.PtpLockState)) {
			eventType = ptp.PtpStateChange
			eventSource = ptp.PtpLockState
		} else if strings.Contains(e.Source(), string(ptp.OsClockSyncState)) {
			eventType = ptp.OsClockSyncStateChange
			eventSource = ptp.OsClockSyncState
		} else if strings.Contains(e.Source(), string(ptp.PtpClockClass)) {
			eventType = ptp.PtpClockClassChange
			eventSource = ptp.PtpClockClass
		} else if strings.Contains(e.Source(), string(ptp.PtpClockClassV1)) {
			eventType = ptp.PtpClockClassChange
			eventSource = ptp.PtpClockClassV1
		} else if strings.Contains(e.Source(), string(ptp.GnssSyncStatus)) {
			eventType = ptp.GnssStateChange
			eventSource = ptp.GnssSyncStatus
		} else if strings.Contains(e.Source(), string(ptp.SyncStatusState)) {
			eventType = ptp.SyncStateChange
			eventSource = ptp.SyncStatusState
		} else if strings.Contains(e.Source(), string(ptp.SynceStateChange)) {
			eventType = ptp.SynceStateChange
			eventSource = ptp.SyncStatusState
		} else if strings.Contains(e.Source(), string(ptp.SynceClockQualityChange)) {
			eventType = ptp.SynceClockQualityChange
			eventSource = ptp.SynceClockQuality
		} else {
			log.Warnf("could not find any events for requested resource type %s", e.Source())
			return fmt.Errorf("could not find any events for requested resource type %s", e.Source())
		}
		if len(eventManager.Stats) == 0 {
			data := eventManager.GetPTPEventsData(ptp.FREERUN, 0, "ptp-not-set", eventType)
			d.Data, err = eventManager.GetPTPCloudEvents(*data, eventType)
			if err != nil {
				return err
			}
			d.Data.SetSource(string(eventSource))
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

		var overallSyncState ptp.SyncState

		for _, ptpStats := range eventManager.Stats { // configname->PTPStats
			for ptpInterface, s := range ptpStats { // iface->stats
				switch ptpInterface {
				case ptpMetrics.MasterClockType:
					if s.Alias() != "" {
						ptpInterface = ptpTypes.IFace(fmt.Sprintf("%s/%s", s.Alias(), ptpMetrics.MasterClockType))
					}
					switch eventType {
					case ptp.PtpStateChange:
						// if its master stats then replace with slave interface(masked) +X
						data = processDataFn(data, eventManager.GetPTPEventsData(s.SyncState(), s.LastOffset(), string(ptpInterface), eventType))
					case ptp.PtpClockClassChange:
						clockClass := fmt.Sprintf("%s/%s", string(ptpInterface), ptpMetrics.ClockClass)
						data = processDataFn(data, eventManager.GetPTPEventsData(s.SyncState(), s.ClockClass(), clockClass, eventType))
					case ptp.SyncStateChange:
						overallSyncState = getOverallState(overallSyncState, s.SyncState())
					}
				case ptpMetrics.ClockRealTime:
					switch eventType {
					case ptp.OsClockSyncStateChange:
						data = processDataFn(data, eventManager.GetPTPEventsData(s.SyncState(), s.LastOffset(), string(ptpInterface), eventType))
					// SyncStateChange includes OsClockSyncStateChange
					case ptp.SyncStateChange:
						overallSyncState = getOverallState(overallSyncState, s.SyncState())
					}
				default:
					switch eventType {
					case ptp.GnssStateChange:
						if s.HasProcessEnabled(gnssProcessName) { //gnss is with the master
							gpsFixState, offset, syncState, _ := s.GetDependsOnValueState(gnssProcessName, nil, "gnss_status")
							offsetInt := int64(offset)
							state := eventManager.GetGPSFixState(gpsFixState, syncState)
							ptpInterface = ptpTypes.IFace(fmt.Sprintf("%s/%s", s.Alias(), ptpMetrics.MasterClockType))
							data = processDataFn(data, eventManager.GetPTPEventsData(state, offsetInt, string(ptpInterface), eventType))
							// add gps fix data to data model
							data.Values = append(data.Values, event.DataValue{
								Resource:  fmt.Sprintf("%s/%s", data.Values[0].GetResource(), "gpsFix"),
								DataType:  event.METRIC,
								ValueType: event.DECIMAL,
								Value:     gpsFixState,
							})
						}
					case ptp.SynceStateChange:
						if s.GetSyncE() != nil {
							// has multiple synce state per port configurations
							// TODO: have synce state at device level when no ports are configured
							synceStats := s.GetSyncE()
							for _, sStats := range synceStats.Port {
								if sStats.State != "" {
									resource := fmt.Sprintf("%s/%s", synceStats.Name, sStats.Name)
									data = processDataFn(data, eventManager.GetPTPEventsData(sStats.State, 0, resource, eventType))
								}
							}
						}
					case ptp.SynceClockQualityChange:
						{
							if s.GetSyncE() != nil {
								// has multiple synce state per port configurations
								// TODO: have synce state at device level when no ports are configured
								synceStats := s.GetSyncE()
								for _, sStats := range synceStats.Port {
									resource := fmt.Sprintf("%s/%s/%s", synceStats.Name, sStats.Name, "Ql")
									data = processDataFn(data, eventManager.GetPTPEventsData("NA", int64(sStats.QL), resource, eventType))
									resource = fmt.Sprintf("%s/%s/%s", synceStats.Name, sStats.Name, "ExtQL")
									if sStats.ExtendedTvlEnabled {
										data.Values = append(data.Values, event.DataValue{
											Resource:  resource,
											DataType:  event.METRIC,
											ValueType: event.DECIMAL,
											Value:     int64(sStats.ExtQL),
										})
									} else {
										data.Values = append(data.Values, event.DataValue{
											Resource:  resource,
											DataType:  event.METRIC,
											ValueType: event.DECIMAL,
											Value:     int64(0xFF), // default
										})
									}
								}
							}
						}
					}
				}
			}
		}
		if eventType == ptp.SyncStateChange && overallSyncState != "" {
			data = processDataFn(data, eventManager.GetPTPEventsData(overallSyncState, 0, string(eventSource), eventType))
		}
		if data != nil {
			d.Data, err = eventManager.GetPTPCloudEvents(*data, eventType)
			if err != nil {
				return err
			}
			d.Data.SetSource(string(eventSource))
		} else {
			data = eventManager.GetPTPEventsData(ptp.FREERUN, 0, "event-not-found", eventType)
			d.Data, err = eventManager.GetPTPCloudEvents(*data, eventType)
			if err != nil {
				return err
			}
			d.Data.SetSource(string(eventSource))
			log.Errorf("could not find any events for requested resource type %s", e.Source())
			return nil
		}
		return nil
	}
}

// return worst of FREERUN, HOLDOVER or LOCKED
func getOverallState(current, updated ptp.SyncState) ptp.SyncState {
	return ptpMetrics.OverallState(current, updated)
}

// update interface details  and threshold details when ptpConfig change found.
// These are updated when config file is created or deleted under /var/run folder
// by  linuxptp-daemon.
// issue to resolve: linuxptp-daemon-container will read ptpconfig and update ptpConfigMap
// the updates are incremental and file creation happens when  ptpConfigMap is updated
// NOTE: there is delay between configmap updates and file creation
// FIRST ptpConfigMap created and then the daemon ticker when starting the process
// creates the files or deletes the  file when process is stopped
// This routine tries to rely on file creation to detect ptp config updates
// and on file creation call ptpConfig updates
// on file deletion reset to read fresh ptpconfig updates

func processPtp4lConfigFileUpdates() {
	fileModified := make(chan bool, 10) // buffered should not have 10 profile; made it 10 for qe; usually you will have only max 3 profiles
	for {
		// No Default Case: If no channel is ready and there's no default case in the select statement,
		// the entire select statement will block until at least one channel becomes ready.
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
				allSections := ptpConfigEvent.GetAllSections()
				ptp4lConfig := eventManager.GetPTPConfig(ptpConfigFileName)

				if ptp4lConfig.Profile == "" {
					ptp4lConfig.Profile = *ptpConfigEvent.Profile
					eventManager.AddPTPConfig(ptpConfigFileName, ptp4lConfig)
				}

				// loop through to find if any interface changed
				if eventManager.Ptp4lConfigInterfaces[ptpConfigFileName] != nil &&
					HasEqualInterface(newInterfaces, eventManager.Ptp4lConfigInterfaces[ptpConfigFileName].Interfaces) {
					log.Infof("skipped update,interface not changed in ptl4lconfig")
					// add to eventManager
					eventManager.AddPTPConfig(ptpConfigFileName, ptp4lConfig)
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
					ptpIFace := &ptp4lconf.PTPInterface{
						Name:     *ptpInterface,
						PortID:   index + 1,
						PortName: fmt.Sprintf("%s%d", "port ", index+1),
						Role:     role,
					}
					ptpInterfaces = append(ptpInterfaces, ptpIFace)
				}
				// updated ptp4lConfig is ready
				ptp4lConfig = &ptp4lconf.PTP4lConfig{
					Name:       string(ptpConfigFileName),
					Profile:    *ptpConfigEvent.Profile,
					Interfaces: ptpInterfaces,
					Sections:   allSections,
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
					if !opts.SyncE4lEnabled() {
						process = append(process, syncE4lProcessName)
					}
					ptpMetrics.DeleteProcessStatusMetricsForConfig(eventManager.NodeName(), cName,
						process...)
					// special ts2phc due to different configName replace ptp4l.0.conf to ts2phc.0.cnf
					if !opts.TS2PhcEnabled() {
						ptpMetrics.DeleteProcessStatusMetricsForConfig(eventManager.NodeName(),
							strings.Replace(cName, ptp4lProcessName, ts2PhcProcessName, 1), ts2PhcProcessName)
					}
				}
				for key, np := range eventManager.PtpConfigMapUpdates.EventThreshold {
					ptpMetrics.Threshold.With(prometheus.Labels{
						"threshold": "MinOffsetThreshold", "node": eventManager.NodeName(), "profile": key}).Set(float64(np.MinOffsetThreshold))
					ptpMetrics.Threshold.With(prometheus.Labels{
						"threshold": "MaxOffsetThreshold", "node": eventManager.NodeName(), "profile": key}).Set(float64(np.MaxOffsetThreshold))
					ptpMetrics.Threshold.With(prometheus.Labels{
						"threshold": "HoldOverTimeout", "node": eventManager.NodeName(), "profile": key}).Set(float64(np.HoldOverTimeout))
				}
			case true: // ptp4l.X.conf is deleted ; how to prevent updates happening at same time
				// delete metrics, ptp4l config is removed
				// set lock prevent any prpConfig update until delete operation is completed
				eventManager.PtpConfigMapUpdates.FileWatcherUpdateInProgress(true)
				ptpConfigFileName := ptpTypes.ConfigName(*ptpConfigEvent.Name)
				ptp4lConfig := eventManager.GetPTPConfig(ptpConfigFileName)
				log.Infof("deleting config %s with profile %s\n", *ptpConfigEvent.Name, ptp4lConfig.Profile)
				ptpStats := eventManager.GetStats(ptpConfigFileName)
				// to avoid new one getting created , make a copy of this stats
				ptpStats.SetConfigAsDeleted(true)
				// there is a possibility of new config with same name is  created immediately after the delete of the old config.
				// time="2024-03-19T19:40:16Z" level=info msg="config removed file: /var/run/phc2sys.2.config"
				// time="2024-03-19T19:40:16Z" level=info msg="updating ptp config changes for phc2sys.2.config"
				if eventManager.PtpConfigMapUpdates.HAProfile == ptp4lConfig.Profile {
					eventManager.PtpConfigMapUpdates.HAProfile = "" // clear profile
				}
				// clean up synce metrics
				for d, s := range ptpStats {
					if s != nil && s.GetSyncE() != nil {
						ptpMetrics.DeleteSyncEMetrics(syncE4lProcessName, string(ptpConfigFileName), *s.GetSyncE())
						ptpStats[d].SetSyncE(nil) // clear all
					}
				}
				// clean up
				if ptpConfig, ok := eventManager.Ptp4lConfigInterfaces[ptpConfigFileName]; ok {
					//clean up any ha metrics
					ptpMetrics.DeletePTPHAMetrics(ptpConfig.Profile)
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
				ptpMetrics.DeleteProcessStatusMetricsForConfig(eventManager.NodeName(), string(ptpConfigFileName), ptp4lProcessName, phc2sysProcessName, syncE4lProcessName)
				ptpMetrics.DeleteProcessStatusMetricsForConfig(eventManager.NodeName(), strings.Replace(string(ptpConfigFileName), ptp4lProcessName, ts2PhcProcessName, 1), ts2PhcProcessName)
				fileModified <- true
			}
		case <-config.CloseCh:
			fileWatcher.Close()
			return
		case <-fileModified:
			// release lock for ptpconfig updates
			eventManager.PtpConfigMapUpdates.FileWatcherUpdateInProgress(false)
			// reset ptpconfig dataset and update again clean
			eventManager.PtpConfigMapUpdates.SetAppliedNodeProfileJSON([]byte{})
			eventManager.PtpConfigMapUpdates.PushPtpConfigMapChanges(eventManager.NodeName())
		}
	}
}
func createPublisher(address string) (pub pubsub.PubSub, err error) {
	// this is loop back on server itself. Since current pod does not create any server
	returnURL := fmt.Sprintf("%s%s", config.BaseURL, "dummy")
	pubToCreate := v1pubs.NewPubSub(types.ParseURI(returnURL), address, config.APIVersion)
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
func InitPubSubTypes(scConfig *common.SCConfiguration) map[ptp.EventType]*ptpTypes.EventPublisherType {
	InitPubs := make(map[ptp.EventType]*ptpTypes.EventPublisherType)
	InitPubs[ptp.SyncStateChange] = &ptpTypes.EventPublisherType{
		EventType: ptp.SyncStateChange,
		Resource:  ptp.SyncStatusState,
	}
	InitPubs[ptp.OsClockSyncStateChange] = &ptpTypes.EventPublisherType{
		EventType: ptp.OsClockSyncStateChange,
		Resource:  ptp.OsClockSyncState,
	}
	if !common.IsV1Api(scConfig.APIVersion) {
		InitPubs[ptp.PtpClockClassChange] = &ptpTypes.EventPublisherType{
			EventType: ptp.PtpClockClassChange,
			Resource:  ptp.PtpClockClass,
		}
	} else {
		InitPubs[ptp.PtpClockClassChange] = &ptpTypes.EventPublisherType{
			EventType: ptp.PtpClockClassChange,
			Resource:  ptp.PtpClockClassV1,
		}
	}
	InitPubs[ptp.PtpStateChange] = &ptpTypes.EventPublisherType{
		EventType: ptp.PtpStateChange,
		Resource:  ptp.PtpLockState,
	}
	InitPubs[ptp.GnssStateChange] = &ptpTypes.EventPublisherType{
		EventType: ptp.GnssStateChange,
		Resource:  ptp.GnssSyncStatus,
	}
	InitPubs[ptp.SynceStateChange] = &ptpTypes.EventPublisherType{
		EventType: ptp.SynceStateChange,
		Resource:  ptp.SynceLockState,
	}
	InitPubs[ptp.SynceClockQualityChange] = &ptpTypes.EventPublisherType{
		EventType: ptp.SynceClockQualityChange,
		Resource:  ptp.SynceClockQuality,
	}
	return InitPubs
}
