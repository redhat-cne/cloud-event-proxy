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
	"bufio"
	"fmt"
	"net"
	"os"
	"sync"

	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/ptp4lconf"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/redhat-cne/sdk-go/pkg/channel"
	"github.com/redhat-cne/sdk-go/pkg/event/ptp"

	v2 "github.com/cloudevents/sdk-go/v2"
	"github.com/redhat-cne/cloud-event-proxy/pkg/common"
	ptpSocket "github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/socket"
	ptpTypes "github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/types"
	v1amqp "github.com/redhat-cne/sdk-go/v1/amqp"
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
	//ClockRealTime is the slave
	ClockRealTime = "CLOCK_REALTIME"
	//MasterClockType is teh slave sync slave clock to master
	MasterClockType = "master"
)

var (
	resourceAddress string = "/cluster/node/%s/ptp"
	config          *common.SCConfiguration
	eventManager    *ptpMetrics.PTPEventManager
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

	// 1. Create event Publication
	var pub pubsub.PubSub
	var err error
	if pub, err = createPublisher(fmt.Sprintf(resourceAddress, nodeName)); err != nil {
		log.Errorf("failed to create a publisher %v", err)
		return err
	}
	log.Printf("Created publisher %v", pub)
	// Initialize the Event Manager
	eventManager = ptpMetrics.NewPTPEventManager(pub.ID, nodeName, config)
	wg.Add(1)
	// create socket listener
	go listenToSocket(wg)
	// watch for ptp any config updates
	go eventManager.PtpConfigMapUpdates.WatchConfigMapUpdate(nodeName, configuration.CloseCh)

	//create a ptpConfigDir directory watcher for config changes
	notifyConfigDirUpdates := make(chan *ptp4lconf.PtpConfigUpdate)
	w, err := ptp4lconf.NewPtp4lConfigWatcher(ptpConfigDir, notifyConfigDirUpdates)
	if err != nil {
		log.Errorf("cannot monitor ptp4l configuation folder at %s : %s", ptpConfigDir, err)
		return err
	}

	// update interface and threshold details when ptpConfig change found
	go func() {
		for {
			select {
			case ptpConfigEvent := <-notifyConfigDirUpdates:
				if ptpConfigEvent == nil || ptpConfigEvent.Name == nil {
					continue
				}
				log.Infof("updating ptp config changes for %s", *ptpConfigEvent.Name)
				switch ptpConfigEvent.Removed {
				case false: //create or modified
					// get config fileName
					ptpConfigFileName := ptpTypes.ConfigName(*ptpConfigEvent.Name)
					// read all interface names from the config
					newInterfaces := ptpConfigEvent.GetAllInterface()

					if _, ok := eventManager.Ptp4lConfigInterfaces[ptpConfigFileName]; !ok { // config file name is the key here
						eventManager.Ptp4lConfigInterfaces[ptpConfigFileName] = nil
					}
					//loop through to find if any interface changed
					if eventManager.Ptp4lConfigInterfaces[ptpConfigFileName] != nil &&
						HasEqualInterface(newInterfaces, eventManager.Ptp4lConfigInterfaces[ptpConfigFileName].Interfaces) {
						log.Infof("skipped update,interface not changed in ptl4lconfig")
						continue
					}

					//cleanup functions to check if interface exists
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
								ptpMetrics.DeleteInterfaceRoleMetrics(ptp4lProcessName, ptpInterface.Name)
							}
						}
					}
					//build new interfaces and its order of index
					var ptpInterfaces []*ptp4lconf.PTPInterface
					for index, ptpInterface := range newInterfaces {
						role := ptpTypes.UNKNOWN
						if p, e := ptp4lConfig.ByInterface(*ptpInterface); e == nil && p.PortID == index+1 { //maintain role
							role = p.Role
						} //else new config order is not same
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
					//add to eventManager
					eventManager.AddPTPConfig(ptpConfigFileName, ptp4lConfig)
				case true: //ptp4l.X.conf is deleted
					//delete metrics, ptp4l config is removed
					ptpConfigFileName := ptpTypes.ConfigName(*ptpConfigEvent.Name)
					ptpStats := eventManager.GetStats(ptpConfigFileName)
					//clean up
					if ptpConfig, ok := eventManager.Ptp4lConfigInterfaces[ptpConfigFileName]; ok {
						for _, ptpInterface := range ptpConfig.Interfaces {
							ptpMetrics.DeleteInterfaceRoleMetrics(ptp4lProcessName, ptpInterface.Name)
						}
						if t, ok := eventManager.PtpConfigMapUpdates.EventThreshold[ptpConfig.Profile]; ok {
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
						eventManager.PublishEvent(ptp.FREERUN, ptpMetrics.FreeRunOffsetValue, ClockRealTime, ptp.PtpStateChange)
					}
					if s, found := ptpStats[MasterClockType]; found {
						ptpMetrics.DeletedPTPMetrics(s.OffsetSource(), ptp4lProcessName, s.Alias())
						masterResource := fmt.Sprintf("%s/%s", s.Alias(), MasterClockType)
						eventManager.PublishEvent(ptp.FREERUN, ptpMetrics.FreeRunOffsetValue, masterResource, ptp.PtpStateChange)
					}
					eventManager.DeleteStatsConfig(ptpConfigFileName)
					eventManager.DeletePTPConfig(ptpConfigFileName)
				}
			case <-config.CloseCh:
				w.Close()
				return
			}
		}
	}()

	// ONly update threshold.
	//get threshold data on change of ptp config
	// update node profile when configmap changes
	go func() {
		for {
			select {
			case <-eventManager.PtpConfigMapUpdates.UpdateCh:
				log.Infof("updating ptp profile changes %d", len(eventManager.PtpConfigMapUpdates.NodeProfiles))
				//clean up
				if len(eventManager.PtpConfigMapUpdates.NodeProfiles) == 0 {
					eventManager.PtpConfigMapUpdates.DeleteAllPTPThreshold()
					for _, pConfig := range eventManager.Ptp4lConfigInterfaces {
						ptpMetrics.DeleteThresholdMetrics(pConfig.Profile)
					}
				} else {
					//updates
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
	onReceiveOverrideFn := func(e v2.Event, d *channel.DataChan) error {
		log.Info("got status check call,fire events for above publisher")
		if len(eventManager.Stats) == 0 {
			eventManager.PublishEvent(ptp.FREERUN, 0, "ptp-not-set", ptp.PtpStateChange)
		} else {
			for _, ptpInterfaces := range eventManager.Stats {
				for ptpInterface, s := range ptpInterfaces {
					if ptpInterface == ptpMetrics.MasterClockType || ptpInterface == ptpMetrics.ClockRealTime {
						if ptpInterface == ptpMetrics.MasterClockType && s.Alias() != "" { // if its master stats then replace with slave interface(masked) +X
							ptpInterface = ptpTypes.IFace(fmt.Sprintf("%s/%s", s.Alias(), ptpMetrics.MasterClockType))
						}
						eventManager.PublishEvent(s.SyncState(), s.LastOffset(), string(ptpInterface), ptp.PtpStateChange)
					}
				}
			}
		}
		d.Type = channel.STATUS
		return nil
	}
	log.Infof("setting up status listener")
	v1amqp.CreateNewStatusListener(config.EventInCh, fmt.Sprintf("%s/%s", pub.Resource, "status"), onReceiveOverrideFn, fn)
	return nil
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
	l, err := ptpSocket.Listen(eventSocket)
	if err != nil {
		log.Errorf("error setting up socket %s", err)
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

//HasEqualInterface checks if configmap  changes has same interface
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
