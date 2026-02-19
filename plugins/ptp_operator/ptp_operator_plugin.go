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
	"syscall"

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
	log "github.com/sirupsen/logrus"

	ptpMetrics "github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/metrics"
	"github.com/redhat-cne/sdk-go/pkg/pubsub"
	"github.com/redhat-cne/sdk-go/pkg/types"
	v1pubs "github.com/redhat-cne/sdk-go/v1/pubsub"
)

const (
	eventSocket  = "/cloud-native/events.sock"
	ptpConfigDir = "/var/run/"
	// restartCommand is the control command sent by linuxptp-daemon on the event
	// socket to request a sidecar restart. The CMD prefix distinguishes it from
	// process log lines (which always start with a process name like ptp4l, phc2sys).
	restartCommand     = "CMD RESTART"
	phc2sysProcessName = "phc2sys"
	ptp4lProcessName   = "ptp4l"
	ts2PhcProcessName  = "ts2phc"
	chronydProcessName = "chronyd"
	gnssProcessName    = "gnss"
	syncE4lProcessName = "synce4l"
	// ClockRealTime is the slave
	ClockRealTime = "CLOCK_REALTIME"
	// MasterClockType is the slave sync slave clock to master
	MasterClockType = "master"
	EventNotFound   = "event-not-found"
	PTPNotSet       = "ptp-not-set"
)

var (
	resourcePrefix = "/cluster/node"
	publishers     = map[ptp.EventType]*ptpTypes.EventPublisherType{}
	config         *common.SCConfiguration
	eventManager   *ptpMetrics.PTPEventManager
)

// restartProcess replaces the current process with a fresh copy of itself via
// syscall.Exec. This guarantees all in-memory state (metrics, stats, holdover
// goroutines, interface roles, etc.) is fully reset, equivalent to a container
// restart but without Kubernetes involvement.
func restartProcess(reason string) {
	log.Infof("configuration change detected (%s), restarting process for clean state...", reason)

	// Remove the Unix socket file so the new process can bind cleanly.
	// The socket Listen() also handles stale sockets as a safety net.
	if err := os.Remove(eventSocket); err != nil && !os.IsNotExist(err) {
		log.Warningf("failed to remove socket file %s: %v", eventSocket, err)
	}

	binary, err := os.Executable()
	if err != nil {
		log.Fatalf("failed to get executable path for restart: %v", err)
	}

	log.Infof("restarting: %s %v", binary, os.Args)
	if err = syscall.Exec(binary, os.Args, os.Environ()); err != nil {
		log.Fatalf("failed to restart process: %v", err)
	}
}

// loadInitialPtp4lConfigs reads all ptp4l/phc2sys/synce4l/chronyd config files
// from ptpConfigDir once at startup and registers them with the event manager.
// If no files exist yet (daemon hasn't written them), the function returns
// gracefully; the daemon will send CMD RESTART once configs are ready.
func loadInitialPtp4lConfigs() {
	configs := ptp4lconf.ReadAllConfig(ptpConfigDir)
	log.Infof("loading %d initial ptp4l config(s) from %s", len(configs), ptpConfigDir)
	for _, ptpConfigEvent := range configs {
		processConfigCreate(ptpConfigEvent)
	}
}

// processConfigCreate builds the PTP4lConfig from a PtpConfigUpdate (a parsed
// config file) and registers it with the event manager. It determines interface
// roles from the config file parameters (serverOnly / masterOnly / clientOnly /
// slaveOnly / global section).
func processConfigCreate(ptpConfigEvent *ptp4lconf.PtpConfigUpdate) {
	if ptpConfigEvent == nil || ptpConfigEvent.Name == nil || ptpConfigEvent.Profile == nil {
		return
	}
	ptpConfigFileName := ptpTypes.ConfigName(*ptpConfigEvent.Name)
	log.Infof("processing initial ptp config %s (profile: %s)", *ptpConfigEvent.Name, *ptpConfigEvent.Profile)

	newInterfaces := ptpConfigEvent.GetAllInterface()
	allSections := ptpConfigEvent.GetAllSections()

	var ptpInterfaces []*ptp4lconf.PTPInterface
	for index, ptpInterface := range newInterfaces {
		role := ptpTypes.UNKNOWN
		if interfaceSection, exists := allSections[*ptpInterface]; exists {
			if serverOnlyValue, hasServerOnly := interfaceSection["serverOnly"]; hasServerOnly {
				if serverOnlyValue == "1" {
					role = ptpTypes.MASTER
				} else if serverOnlyValue == "0" {
					role = ptpTypes.SLAVE
				}
			} else if masterOnlyValue, hasMasterOnly := interfaceSection["masterOnly"]; hasMasterOnly {
				if masterOnlyValue == "1" {
					role = ptpTypes.MASTER
				} else if masterOnlyValue == "0" {
					role = ptpTypes.SLAVE
				}
			} else {
				if globalSection, hasGlobal := allSections["global"]; hasGlobal {
					if globalClientOnly, hasGlobalClientOnly := globalSection["clientOnly"]; hasGlobalClientOnly && globalClientOnly == "1" {
						role = ptpTypes.SLAVE
					} else if globalSlaveOnly, hasGlobalSlaveOnly := globalSection["slaveOnly"]; hasGlobalSlaveOnly && globalSlaveOnly == "1" {
						role = ptpTypes.SLAVE
					} else {
						role = ptpTypes.SLAVE
					}
				} else {
					role = ptpTypes.SLAVE
				}
			}
		}
		ptpInterfaces = append(ptpInterfaces, &ptp4lconf.PTPInterface{
			Name:     *ptpInterface,
			PortID:   index + 1,
			PortName: fmt.Sprintf("port %d", index+1),
			Role:     role,
		})
		ptpMetrics.UpdateInterfaceRoleMetrics(ptp4lProcessName, *ptpInterface, role)
	}

	ptp4lConfig := &ptp4lconf.PTP4lConfig{
		Name:       string(ptpConfigFileName),
		Profile:    *ptpConfigEvent.Profile,
		Interfaces: ptpInterfaces,
		Sections:   allSections,
	}
	ptp4lConfig.ProfileType = eventManager.GetProfileType(ptp4lConfig.Profile)
	eventManager.AddPTPConfig(ptpConfigFileName, ptp4lConfig)
}

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
	publishers = InitPubSubTypes()

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
	_, err = eventManager.LoadFromStore(config)
	if err != nil {
		log.Warn(err)
	}
	err = eventManager.TriggerLogs()
	if err != nil {
		log.Warn(err)
	}
	eventManager.SetInitalMetrics()
	wg.Add(1)
	// create socket listener; the daemon sends log lines and CMD RESTART commands here
	go listenToSocket(wg)
	// read configmap once at startup to load thresholds, process options and settings
	go eventManager.PtpConfigMapUpdates.WatchConfigMapUpdate(nodeName, configuration.CloseCh, false)
	// read existing config files once at startup to register interface/role mappings;
	// if no files exist yet the daemon will send CMD RESTART once configs are ready
	loadInitialPtp4lConfigs()

	// one-shot configmap handler: load thresholds/options on the first update then exit
	go func() {
		select {
		case <-eventManager.PtpConfigMapUpdates.UpdateCh:
			log.Infof("loading ptp profile config: %d profiles", len(eventManager.PtpConfigMapUpdates.NodeProfiles))
			if len(eventManager.PtpConfigMapUpdates.NodeProfiles) > 0 {
				eventManager.PtpConfigMapUpdates.UpdatePTPProcessOptions()
				eventManager.PtpConfigMapUpdates.UpdatePTPThreshold()
				eventManager.PtpConfigMapUpdates.UpdatePTPSetting()
				for key, np := range eventManager.PtpConfigMapUpdates.EventThreshold {
					ptpMetrics.Threshold.With(prometheus.Labels{
						"threshold": "MinOffsetThreshold", "node": nodeName, "profile": key}).Set(float64(np.MinOffsetThreshold))
					ptpMetrics.Threshold.With(prometheus.Labels{
						"threshold": "MaxOffsetThreshold", "node": nodeName, "profile": key}).Set(float64(np.MaxOffsetThreshold))
					ptpMetrics.Threshold.With(prometheus.Labels{
						"threshold": "HoldOverTimeout", "node": nodeName, "profile": key}).Set(float64(np.HoldOverTimeout))
				}
			}
			// exit after first load; ConfigMap changes trigger a daemon-sent CMD RESTART
		case <-config.CloseCh:
			return
		}
	}()

	// 2.Create Status Listener
	// method to be called when ping received
	onReceiveOverrideFn := getCurrentStatOverrideFn()

	log.Infof("setting up status listener")
	config.RestAPI.SetOnStatusReceiveOverrideFn(onReceiveOverrideFn)
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
		} else if strings.Contains(e.Source(), string(ptp.SynceLockState)) {
			eventType = ptp.SynceStateChange
			eventSource = ptp.SynceLockState
		} else if strings.Contains(e.Source(), string(ptp.SynceClockQuality)) {
			eventType = ptp.SynceClockQualityChange
			eventSource = ptp.SynceClockQuality
		} else {
			log.Warnf("could not find any events for requested resource type %s", e.Source())
			return fmt.Errorf("could not find any events for requested resource type %s", e.Source())
		}
		if len(eventManager.Stats) == 0 {
			data := eventManager.GetPTPEventsData(ptp.FREERUN, 0, PTPNotSet, eventType)
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
		for config := range eventManager.Stats { // configname->PTPStats
			mainClockName := eventManager.Stats[config].GetMainClockName()
			for ptpInterface, s := range eventManager.GetStats(config) { // iface->stats
				switch ptpInterface {
				case mainClockName:
					if s.Alias() != "" {
						ptpInterface = ptpTypes.IFace(fmt.Sprintf("%s/%s", s.Alias(), ptpMetrics.MasterClockType))
					}
					switch eventType {
					case ptp.PtpStateChange:
						// if its master stats then replace with slave interface(masked) +X
						data = processDataFn(data, eventManager.GetPTPEventsData(s.SyncState(), s.LastOffset(), string(ptpInterface), eventType))
					case ptp.PtpClockClassChange:
						clockClass := fmt.Sprintf("%s/%s", string(ptpInterface), ptpMetrics.ClockClass)
						// make sure clock class has real value
						if s.ClockClass() != 0 {
							data = processDataFn(data, eventManager.GetPTPEventsData(s.SyncState(), s.ClockClass(), clockClass, eventType))
						} else {
							log.Debugf("Skipping PTP clock class change event for %s - clockClass not populated yet", string(ptpInterface))
						}
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
			data = eventManager.GetPTPEventsData(ptp.FREERUN, 0, EventNotFound, eventType)
			d.Data, err = eventManager.GetPTPCloudEvents(*data, eventType)
			if err != nil {
				return err
			}
			d.Data.SetSource(string(eventSource))
			log.Errorf("event data not found for requested resource type %s", e.Source())
			return nil
		}
		return nil
	}
}

// return worst of FREERUN, HOLDOVER or LOCKED
func getOverallState(current, updated ptp.SyncState) ptp.SyncState {
	return ptpMetrics.OverallState(current, updated)
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
		if msg == restartCommand {
			restartProcess("restart requested by daemon via socket")
			return // unreachable after exec
		}
		eventManager.ExtractMetrics(msg)
	}
}

// InitPubSubTypes ... initialize types of publishers for ptp operator
func InitPubSubTypes() map[ptp.EventType]*ptpTypes.EventPublisherType {
	InitPubs := make(map[ptp.EventType]*ptpTypes.EventPublisherType)
	InitPubs[ptp.SyncStateChange] = &ptpTypes.EventPublisherType{
		EventType: ptp.SyncStateChange,
		Resource:  ptp.SyncStatusState,
	}
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
