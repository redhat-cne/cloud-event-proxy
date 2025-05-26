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

//go:build unittests
// +build unittests

package main

import (
	"encoding/json"
	"fmt"
	v2 "github.com/cloudevents/sdk-go/v2"
	"github.com/google/uuid"
	event2 "github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/event"
	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/metrics"
	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/stats"
	"github.com/redhat-cne/sdk-go/pkg/event"
	"github.com/redhat-cne/sdk-go/pkg/types"
	"k8s.io/utils/pointer"
	"log"
	"os"
	"path"
	"strings"
	"sync"
	"testing"

	"github.com/redhat-cne/cloud-event-proxy/pkg/common"
	ptpTypes "github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/types"
	restapi "github.com/redhat-cne/rest-api/v2"
	"github.com/redhat-cne/sdk-go/pkg/channel"
	ptpEvent "github.com/redhat-cne/sdk-go/pkg/event/ptp"
	v1event "github.com/redhat-cne/sdk-go/v1/event"
	"github.com/stretchr/testify/assert"

	v1pubsub "github.com/redhat-cne/sdk-go/v1/pubsub"
	subscriberApi "github.com/redhat-cne/sdk-go/v1/subscriber"
)

var (
	wg                sync.WaitGroup
	server            *restapi.Server
	scConfig          *common.SCConfiguration
	channelBufferSize int = 10
	storePath             = "../../.."
	apiPort           int = 8990
	c                 chan os.Signal
	pubsubTypes       map[ptpEvent.EventType]*ptpTypes.EventPublisherType
	nodeName          = "test_node"
)

func TestMain(m *testing.M) {
	defer cleanUP()
	scConfig = &common.SCConfiguration{
		EventInCh:     make(chan *channel.DataChan, channelBufferSize),
		EventOutCh:    make(chan *channel.DataChan, channelBufferSize),
		CloseCh:       make(chan struct{}),
		APIPort:       apiPort,
		APIPath:       "/api/ocloudNotifications/v2/",
		PubSubAPI:     v1pubsub.GetAPIInstance(storePath),
		SubscriberAPI: subscriberApi.GetAPIInstance(storePath),
		StorePath:     storePath,
		TransportHost: &common.TransportHost{
			Type: common.HTTP,
			URL:  "localhost:8990",
			Host: "localhost",
			Port: 8990,
			Err:  nil,
		},
		BaseURL: nil,
	}

	c = make(chan os.Signal)
	cleanUP()
	common.StartPubSubService(scConfig)
	pubsubTypes = InitPubSubTypes()
	scConfig.RestAPI.SetOnStatusReceiveOverrideFn(getMockOverrideFn())
	os.Exit(m.Run())
}
func cleanUP() {
	_, _ = scConfig.SubscriberAPI.DeleteAllSubscriptions()
	_ = scConfig.PubSubAPI.DeleteAllPublishers()
}

// ProcessInChannel will be  called if Transport is disabled
func ProcessInChannel() {
	for { //nolint:gosimple
		select {
		case d := <-scConfig.EventInCh:
			if d.Type == channel.SUBSCRIBER {
				log.Printf("transport disabled,no action taken: request to create listener address %s was called,but transport is not enabled", d.Address)
			} else if d.Type == channel.PUBLISHER {
				log.Printf("no action taken: request to create sender for address %s was called,but transport is not enabled", d.Address)
			} else if d.Type == channel.EVENT && d.Status == channel.NEW {
				out := channel.DataChan{
					Address:        d.Address,
					Data:           d.Data,
					Status:         channel.SUCCESS,
					Type:           channel.EVENT,
					ProcessEventFn: d.ProcessEventFn,
				}
				if d.OnReceiveOverrideFn != nil {
					if err := d.OnReceiveOverrideFn(*d.Data, &out); err != nil {
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

func TestGetCurrentStatOverrideFn(t *testing.T) {
	// Setup
	//CLIENT SUBSCRIPTION: create a subscription to consume events

	var err error
	endpointURL := fmt.Sprintf("%s%s", scConfig.BaseURL, "dummy")
	for _, pTypes := range pubsubTypes {
		pub := v1pubsub.NewPubSub(types.ParseURI(endpointURL), path.Join(resourcePrefix, nodeName, string(pTypes.Resource)))
		pub, err = common.CreatePublisher(scConfig, pub)
		assert.Nil(t, err)
		assert.NotEmpty(t, pub.ID)
		assert.NotEmpty(t, pub.URILocation)
		pTypes.PubID = pub.ID
		pTypes.Pub = &pub
	}
	assert.Equal(t, 7, len(pubsubTypes))
	eventManager = metrics.NewPTPEventManager("/cluster/node", pubsubTypes, nodeName, scConfig)
	eventManager.MockTest(true)

	tests := []struct {
		name                    string
		eventSource             ptpEvent.EventResource
		eventType               ptpEvent.EventType
		expectedSyncState       ptpTypes.SyncState
		expectedResourceAddress string
		statsData               []statsData
		depsClockState          []event2.ClockState
	}{
		{
			name:                    "PTP State is Locked - Single Event",
			expectedSyncState:       ptpTypes.LOCKED,
			eventSource:             ptpEvent.PtpLockState,
			eventType:               ptpEvent.PtpStateChange,
			expectedResourceAddress: fmt.Sprintf("/cluster/node/%s/%s/%s", nodeName, "ens1fx", MasterClockType),
			statsData: []statsData{
				{clockType: MasterClockType, configName: "ptp4l.0.config", processName: "ptp4l", alias: "ens1fx", iface: "ens1f0", syncState: ptpEvent.LOCKED},
			},
		},
		{
			name:                    "OS CLOCK event not found",
			expectedSyncState:       ptpTypes.FREERUN,
			eventSource:             ptpEvent.OsClockSyncState,
			eventType:               ptpEvent.OsClockSyncStateChange,
			expectedResourceAddress: fmt.Sprintf("/cluster/node/%s/%s", nodeName, "event-not-found"),
			statsData: []statsData{
				{clockType: MasterClockType, configName: "ptp4l.0.config", processName: "phc2sys", alias: "ens1fx", iface: "ens1f0", syncState: ptpEvent.LOCKED},
			},
		},
		{
			name:                    "OS CLOCK is Locked - Single Event",
			expectedSyncState:       ptpTypes.LOCKED,
			eventSource:             ptpEvent.OsClockSyncState,
			eventType:               ptpEvent.OsClockSyncStateChange,
			expectedResourceAddress: fmt.Sprintf("/cluster/node/%s/%s", nodeName, ClockRealTime),
			statsData: []statsData{
				{clockType: ClockRealTime, configName: "ptp4l.0.config", processName: "phc2sys", alias: "", iface: "", syncState: ptpEvent.LOCKED},
			},
		},
		{
			name:                    "SyncStatusState := LOCKED Master + FREERUN OS",
			eventSource:             ptpEvent.SyncStatusState,
			eventType:               ptpEvent.SyncStateChange,
			expectedSyncState:       ptpTypes.FREERUN,
			expectedResourceAddress: fmt.Sprintf("/cluster/node/%s%s", nodeName, ptpEvent.SyncStatusState),
			statsData: []statsData{
				{clockType: MasterClockType, configName: "ptp4l.0.config", processName: "ptp4l", alias: "ens1fx", iface: "ens1f0", syncState: ptpEvent.LOCKED},
				{clockType: ClockRealTime, configName: "ptp4l.0.config", processName: "phc2sys", syncState: ptpEvent.FREERUN},
			},
		},
		{
			name:                    "SyncStatusState:= FREERUN Master + FREERUN OS",
			eventSource:             ptpEvent.SyncStatusState,
			eventType:               ptpEvent.SyncStateChange,
			expectedSyncState:       ptpTypes.FREERUN,
			expectedResourceAddress: fmt.Sprintf("/cluster/node/%s%s", nodeName, ptpEvent.SyncStatusState),
			statsData: []statsData{
				{clockType: MasterClockType, configName: "ptp4l.0.config", processName: "ptp4l", alias: "ens1fx", iface: "ens1f0", syncState: ptpEvent.FREERUN},
				{clockType: ClockRealTime, configName: "ptp4l.0.config", processName: "phc2sys", syncState: ptpEvent.FREERUN},
			},
		},
		{
			name:                    "SyncStatusState:= LOCKED Master + LOCKED OS",
			eventSource:             ptpEvent.SyncStatusState,
			eventType:               ptpEvent.SyncStateChange,
			expectedSyncState:       ptpTypes.LOCKED,
			expectedResourceAddress: fmt.Sprintf("/cluster/node/%s%s", nodeName, ptpEvent.SyncStatusState),
			statsData: []statsData{
				{clockType: MasterClockType, configName: "ptp4l.0.config", processName: "ptp4l", alias: "ens1fx", iface: "ens1f0", syncState: ptpEvent.LOCKED},
				{clockType: ClockRealTime, configName: "ptp4l.0.config", processName: "phc2sys", syncState: ptpEvent.LOCKED},
			},
		},
		{
			name:                    "SyncStatusState:= T-GM everything is  locked ",
			eventSource:             ptpEvent.SyncStatusState,
			eventType:               ptpEvent.SyncStateChange,
			expectedSyncState:       ptpTypes.LOCKED,
			expectedResourceAddress: fmt.Sprintf("/cluster/node/%s%s", nodeName, ptpEvent.SyncStatusState),
			statsData: []statsData{
				{clockType: MasterClockType, configName: "ptp4l.0.config", processName: "ptp4l", alias: "ens1fx", iface: "ens1f0", syncState: ptpEvent.LOCKED},
				{clockType: ClockRealTime, configName: "ptp4l.0.config", processName: "phc2sys", syncState: ptpEvent.LOCKED},
			},
			depsClockState: []event2.ClockState{
				{ClockSource: event2.GNSS, Process: gnssProcessName, Offset: pointer.Float64(1.0), IFace: pointer.String("ens1f0"), State: ptpEvent.LOCKED},
				{ClockSource: event2.DPLL, Process: "dpll", Offset: pointer.Float64(1.0), IFace: pointer.String("ens1f0"), State: ptpEvent.LOCKED},
				{ClockSource: event2.GM, Process: "gm", Offset: pointer.Float64(1.0), IFace: pointer.String("ens1f0"), State: ptpEvent.LOCKED},
			},
		},
		{
			name:                    "SyncStatusState:= T-GM everything is  locked ",
			eventSource:             ptpEvent.SyncStatusState,
			eventType:               ptpEvent.SyncStateChange,
			expectedSyncState:       ptpTypes.FREERUN,
			expectedResourceAddress: fmt.Sprintf("/cluster/node/%s%s", nodeName, ptpEvent.SyncStatusState),
			statsData: []statsData{
				{clockType: MasterClockType, configName: "ptp4l.0.config", processName: "ptp4l", alias: "ens1fx", iface: "ens1f0", syncState: ptpEvent.LOCKED},
				{clockType: ClockRealTime, configName: "ptp4l.0.config", processName: "phc2sys", syncState: ptpEvent.FREERUN},
			},
			depsClockState: []event2.ClockState{
				{ClockSource: event2.GNSS, Process: gnssProcessName, Offset: pointer.Float64(1.0), IFace: pointer.String("ens1f0"), State: ptpEvent.LOCKED},
				{ClockSource: event2.DPLL, Process: "dpll", Offset: pointer.Float64(1.0), IFace: pointer.String("ens1f0"), State: ptpEvent.LOCKED},
				{ClockSource: event2.GM, Process: "gm", Offset: pointer.Float64(1.0), IFace: pointer.String("ens1f0"), State: ptpEvent.LOCKED},
			},
		},
		{
			name:                    "SyncStatusState:= T-GM not everything is locked ",
			eventSource:             ptpEvent.SyncStatusState,
			eventType:               ptpEvent.SyncStateChange,
			expectedSyncState:       ptpTypes.LOCKED,
			expectedResourceAddress: fmt.Sprintf("/cluster/node/%s%s", nodeName, ptpEvent.SyncStatusState),
			statsData: []statsData{
				{clockType: MasterClockType, configName: "ptp4l.0.config", processName: "ptp4l", alias: "ens1fx", iface: "ens1f0", syncState: ptpEvent.LOCKED},
				{clockType: ClockRealTime, configName: "ptp4l.0.config", processName: "phc2sys", syncState: ptpEvent.LOCKED},
			},
			depsClockState: []event2.ClockState{
				{ClockSource: event2.GNSS, Process: gnssProcessName, Offset: pointer.Float64(1.0), IFace: pointer.String("ens1f0"), State: ptpEvent.LOCKED},
				{ClockSource: event2.DPLL, Process: "dpll", Offset: pointer.Float64(1.0), IFace: pointer.String("ens1f0"), State: ptpEvent.FREERUN},
				{ClockSource: event2.GM, Process: "gm", Offset: pointer.Float64(1.0), IFace: pointer.String("ens1f0"), State: ptpEvent.LOCKED},
			},
		},
	}

	// Iterate over test cases
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			event := buildEvent(nodeName, tt.eventSource, tt.eventType)
			eventManager.Stats = getStats(tt.statsData, tt.depsClockState)

			// Initialize Stats object
			// Invoke the function
			// Mock input
			mockDataChan := &channel.DataChan{
				ClientID: uuid.MustParse("123e4567-e89b-12d3-a456-426614174000"),
			}

			overrideFn := getCurrentStatOverrideFn()
			err := overrideFn(event, mockDataChan)

			// Assertions
			assert.NoError(t, err, "Expected no error from getCurrentStatOverrideFn")
			assert.NotNil(t, mockDataChan.Data, "Expected DataChan.Data to be populated")
			eventReceived, err2 := v1event.GetCloudNativeEvents(*mockDataChan.Data)

			assert.Nil(t, err2)
			assert.Equal(t, tt.expectedSyncState.String(), eventReceived.Data.Values[0].Value, "Expected SyncState to match event state")
			assert.Equal(t, tt.expectedResourceAddress, eventReceived.Data.Values[0].Resource, "Expected resource to match expected resource")
			expectedReturnAddr := fmt.Sprintf("/cluster/node/%s%s", nodeName, string(tt.eventSource))
			assert.Equal(t, expectedReturnAddr, *mockDataChan.ReturnAddress)
		})
	}
}

// Define the struct to match the JSON structure

type statsData struct {
	clockType   string
	configName  string
	alias       string
	iface       string
	processName string
	syncState   ptpEvent.SyncState
}

func getStats(statsData []statsData, depsClockState []event2.ClockState) map[ptpTypes.ConfigName]stats.PTPStats {
	s := make(map[ptpTypes.ConfigName]stats.PTPStats)

	for index, statsObj := range statsData {

		if _, found := s[ptpTypes.ConfigName(statsObj.configName)]; !found {
			s[ptpTypes.ConfigName(statsObj.configName)] = make(stats.PTPStats)
		}
		if _, found := s[ptpTypes.ConfigName(statsObj.configName)][ptpTypes.IFace(statsObj.clockType)]; !found {
			s[ptpTypes.ConfigName(statsObj.configName)][ptpTypes.IFace(statsObj.clockType)] = stats.NewStats(string(statsObj.clockType))
			s[ptpTypes.ConfigName(statsObj.configName)][ptpTypes.IFace(statsObj.clockType)].SetOffsetSource(statsObj.processName)
			s[ptpTypes.ConfigName(statsObj.configName)][ptpTypes.IFace(statsObj.clockType)].SetAlias(statsObj.alias)
			s[ptpTypes.ConfigName(statsObj.configName)][ptpTypes.IFace(statsObj.clockType)].SetProcessName(statsObj.processName)
			s[ptpTypes.ConfigName(statsObj.configName)][ptpTypes.IFace(statsObj.clockType)].SetLastSyncState(statsObj.syncState)

			// Loop through depsClockState and call SetPtpDependentEventState for each ClockState
			if index == 0 {
				for _, clockState := range depsClockState {
					s[ptpTypes.ConfigName(statsObj.configName)][ptpTypes.IFace(statsObj.clockType)].SetPtpDependentEventState(
						clockState,
						map[string]*event2.PMetric{
							"metric1": {},
						},
						map[string]string{
							"metric1": "Metric 1 description",
						},
					)
				}
			}
		}

	}
	return s
}

func getDeps(state ptpEvent.SyncState, iface, processName string) event2.ClockState {
	return event2.ClockState{
		State:   state,
		Offset:  pointer.Float64(5.0),
		IFace:   pointer.String(iface),
		Process: processName,
	}

}

func ConvertToEvent(dataEncoded []byte) (*event.Event, error) {
	// Unmarshal the JSON data into the Event struct
	var event event.Event
	err := json.Unmarshal(dataEncoded, &event)
	if err != nil {
		return nil, fmt.Errorf("error unmarshaling JSON: %w", err)
	}
	return &event, nil
}

func buildEvent(node string, source ptpEvent.EventResource, eventType ptpEvent.EventType) v2.Event {
	e := v2.NewEvent()
	e.SetSource(fmt.Sprintf("/cluster/node/%s%s", node, string(source)))
	e.SetType(string(eventType))
	return e
}

func getMockOverrideFn() func(e v2.Event, d *channel.DataChan) error {
	return func(e v2.Event, d *channel.DataChan) error {
		if e.Source() != "" {
			d.ReturnAddress = pointer.String(e.Source())
		}

		var eventType ptpEvent.EventType
		var eventSource ptpEvent.EventResource

		switch {
		case strings.Contains(e.Source(), string(ptpEvent.PtpLockState)):
			eventType = ptpEvent.PtpStateChange
			eventSource = ptpEvent.PtpLockState
		case strings.Contains(e.Source(), string(ptpEvent.OsClockSyncState)):
			eventType = ptpEvent.OsClockSyncStateChange
			eventSource = ptpEvent.OsClockSyncState
		case strings.Contains(e.Source(), string(ptpEvent.PtpClockClass)):
			eventType = ptpEvent.PtpClockClassChange
			eventSource = ptpEvent.PtpClockClass
		case strings.Contains(e.Source(), string(ptpEvent.PtpClockClassV1)):
			eventType = ptpEvent.PtpClockClassChange
			eventSource = ptpEvent.PtpClockClassV1
		case strings.Contains(e.Source(), string(ptpEvent.GnssSyncStatus)):
			eventType = ptpEvent.GnssStateChange
			eventSource = ptpEvent.GnssSyncStatus
		case strings.Contains(e.Source(), string(ptpEvent.SyncStatusState)):
			eventType = ptpEvent.SyncStateChange
			eventSource = ptpEvent.SyncStatusState
		case strings.Contains(e.Source(), string(ptpEvent.SyncStatusState)):
			eventType = ptpEvent.SynceStateChange
			eventSource = ptpEvent.SyncStatusState
		case strings.Contains(e.Source(), string(ptpEvent.SynceClockQuality)):
			eventType = ptpEvent.SynceClockQualityChange
			eventSource = ptpEvent.SynceClockQuality
		default:
			return fmt.Errorf("mock: unsupported event source: %s", e.Source())
		}

		// Create dummy event data
		data := &event.Data{
			Version: "1.0",
			Values: []event.DataValue{
				{
					Resource:  fmt.Sprintf("/mock/resource/%s", eventSource),
					Value:     ptpEvent.LOCKED,
					ValueType: event.ENUMERATION,
					DataType:  event.NOTIFICATION,
				},
			},
		}

		// Encode to CloudEvent format
		evts, err := eventManager.GetPTPCloudEvents(*data, eventType)
		if err != nil {
			return fmt.Errorf("mock: failed to get cloud event: %w", err)
		}
		evts.SetSource(string(eventSource))
		d.Data = evts

		return nil
	}
}
