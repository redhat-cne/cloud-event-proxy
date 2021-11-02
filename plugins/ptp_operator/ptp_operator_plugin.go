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

	ceTypes "github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/types"

	"github.com/redhat-cne/sdk-go/pkg/channel"
	"github.com/redhat-cne/sdk-go/pkg/event"

	v2 "github.com/cloudevents/sdk-go/v2"
	"github.com/redhat-cne/cloud-event-proxy/pkg/common"
	ptpSocket "github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/socket"
	ptpTypes "github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/types"
	v1amqp "github.com/redhat-cne/sdk-go/v1/amqp"
	log "github.com/sirupsen/logrus"

	ptpMetrics "github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/metrics"
	cneEvent "github.com/redhat-cne/sdk-go/pkg/event"
	"github.com/redhat-cne/sdk-go/pkg/pubsub"
	"github.com/redhat-cne/sdk-go/pkg/types"
	v1pubs "github.com/redhat-cne/sdk-go/v1/pubsub"
)

const (
	eventSocket        = "/cloud-native/events.sock"
	ptpConfigDir       = "/var/run/"
	phc2sysProcessName = "phc2sys"
	ptp4lProcessName   = "ptp4l"
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
	eventManager = ptpMetrics.NewPTPEventManager(pub.ID, nodeName, config)

	wg.Add(1)
	go listenToSocket(wg)
	go eventManager.PtpConfigMapUpdates.WatchConfigMapUpdate(nodeName, configuration.CloseCh)

	// watch for ptp 4l config changes
	notifyConfigDirUpdates := make(chan *ptp4lconf.PtpConfigUpdate)
	w, err := ptp4lconf.NewPtp4lConfigWatcher(ptpConfigDir, notifyConfigDirUpdates)
	if err != nil {
		log.Errorf("cannot monitor ptp4l configuation folder at %s : %s", ptpConfigDir, err)
		return err
	}

	// update interface details when ptpconfig change found
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
					ptpConfigFileName := ptpTypes.ConfigName(*ptpConfigEvent.Name)
					newInterfaces := ptpConfigEvent.GetAllInterface()

					if _, ok := eventManager.Ptp4lConfigInterfaces[ptpConfigFileName]; !ok { // config file name is the key here
						eventManager.Ptp4lConfigInterfaces[ptpConfigFileName] = nil
					}

					if eventManager.Ptp4lConfigInterfaces[ptpConfigFileName] != nil &&
						HasEqualInterface(newInterfaces, eventManager.Ptp4lConfigInterfaces[ptpConfigFileName].Interfaces) {
						log.Infof("skipped update,interface not changed in ptl4lconfig")
						continue
					}

					//cleanup
					isExists := func(i string) bool {
						for _, iface := range newInterfaces {
							if iface != nil && i == *iface {
								return true
							}
						}
						return false
					}
					ptp4lConfig := eventManager.GetPTPConfig(ptpConfigFileName)
					if ptp4lConfig != nil {
						ptpStats := eventManager.GetStats(ptpConfigFileName)
						for _, ifaces := range ptp4lConfig.Interfaces {
							if !isExists(ifaces.Name) {
								log.Errorf("config updated and interface  not exists, deleting %s", ifaces.Name)
								t := eventManager.PtpThreshold(ifaces.Name)
								close(t.Close) // close any holdover go routines

								eventManager.PublishEvent(cneEvent.FREERUN, ptpMetrics.FreeRunOffsetValue, ifaces.Name, channel.PTPEvent)
								ptpMetrics.UpdateSyncStateMetrics(phc2sysProcessName, ifaces.Name, cneEvent.FREERUN)
								if s, found := ptpStats[ceTypes.IFace(ifaces.Name)]; found {
									ptpMetrics.UpdateDeletedPTPMetrics(s.OffsetSource(), ifaces.Name, s.ProcessName())
									eventManager.DeleteStats(ptpConfigFileName, ptpTypes.IFace(ifaces.Name))
								}
								eventManager.PtpConfigMapUpdates.DeletePTPThreshold(ifaces.Name)
								ptpMetrics.UpdateInterfaceRoleMetrics(ptp4lProcessName, ifaces.Name, ptpTypes.UNKNOWN)
							}
						}
					}
					var ptpInterfaces []*ptp4lconf.PTPInterface
					for index, iface := range newInterfaces {
						role := ptpTypes.UNKNOWN
						if p, e := ptp4lConfig.ByInterface(*iface); e == nil && p.PortID == index+1 { //maintain role
							role = p.Role
						} //else new config order is not same
						ptpInterface := &ptp4lconf.PTPInterface{
							Name:     *iface,
							PortID:   index + 1,
							PortName: fmt.Sprintf("%s%d", "port ", index+1),
							Role:     role,
						}
						ptpInterfaces = append(ptpInterfaces, ptpInterface)
					}

					ptp4lConfig = &ptp4lconf.PTP4lConfig{
						Name:       string(ptpConfigFileName),
						Interfaces: ptpInterfaces,
					}

					eventManager.AddPTPConfig(ptpConfigFileName, ptp4lConfig)
				case true:
					//update metrics
					ptpConfigFileName := ptpTypes.ConfigName(*ptpConfigEvent.Name)
					ptpStats := eventManager.GetStats(ptpConfigFileName)
					if ptpConfig, ok := eventManager.Ptp4lConfigInterfaces[ptpConfigFileName]; ok {
						for _, iface := range ptpConfig.Interfaces {
							if t, ok := eventManager.PtpConfigMapUpdates.EventThreshold[iface.Name]; ok {
								// Make sure that the function does close the channel
								t.SafeClose()
							}
							eventManager.PublishEvent(cneEvent.FREERUN, ptpMetrics.FreeRunOffsetValue, iface.Name, channel.PTPEvent)
							ptpMetrics.UpdateSyncStateMetrics(phc2sysProcessName, iface.Name, cneEvent.FREERUN)
							if s, found := ptpStats[ceTypes.IFace(iface.Name)]; found {
								ptpMetrics.UpdateDeletedPTPMetrics(s.ProcessName(), iface.Name, s.ProcessName())
								eventManager.DeleteStats(ptpConfigFileName, ptpTypes.IFace(iface.Name))
							}
							ptpMetrics.UpdateInterfaceRoleMetrics(ptp4lProcessName, iface.Name, ptpTypes.UNKNOWN)
						}
					}
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
					for cfg, ifaces := range eventManager.Stats {
						for i := range ifaces {
							eventManager.PtpConfigMapUpdates.DeletePTPThreshold(string(i))
							eventManager.DeleteStats(cfg, i)
						}
					}
				} else {
					//updates
					eventManager.PtpConfigMapUpdates.UpdatePTPThreshold()
					for key, np := range eventManager.PtpConfigMapUpdates.EventThreshold {
						ptpMetrics.Threshold.With(prometheus.Labels{
							"threshold": "MinOffsetThreshold", "node": nodeName, "iface": key}).Set(float64(np.MinOffsetThreshold))
						ptpMetrics.Threshold.With(prometheus.Labels{
							"threshold": "MaxOffsetThreshold", "node": nodeName, "iface": key}).Set(float64(np.MaxOffsetThreshold))
						ptpMetrics.Threshold.With(prometheus.Labels{
							"threshold": "HoldOverTimeout", "node": nodeName, "iface": key}).Set(float64(np.HoldOverTimeout))
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
			eventManager.PublishEvent(event.FREERUN, 0, "ptp-not-set", "PTP_STATUS")
		} else {
			var publishStatus bool
			for c, ifaces := range eventManager.Stats {
				publishStatus = true // do not publish status for current slave interface
				for i, s := range ifaces {
					// CLOCK_REALTIME  data will be published instead
					if e, ok := eventManager.Ptp4lConfigInterfaces[c]; ok {
						if iface, err := e.ByRole(ptpTypes.SLAVE); err == nil && string(i) == iface.Name { //skip SLAVE status
							publishStatus = false //CLOCK_REALTIME stats will be published instead
						}
					}
					if publishStatus {
						eventManager.PublishEvent(s.SyncState(), s.Offset(), string(i), "PTP_STATUS")
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
	// this is loopback on server itself. Since current pod does not create any server
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
			log.Error("error reading socket input")
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
