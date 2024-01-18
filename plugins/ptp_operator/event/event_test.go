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

package event_test

import (
	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/event"
	"github.com/redhat-cne/sdk-go/pkg/event/ptp"
	"github.com/stretchr/testify/assert"
	"k8s.io/utils/pointer"
	"testing"
)

var (
	depObject = &event.PTPEventState{DependsOn: map[string]event.DependingClockState{}}
)

type inputState struct {
	state   ptp.SyncState
	process string
	offset  *float64
	eType   ptp.EventType
}
type eventTestCase struct {
	expectedState    ptp.SyncState
	eventStateObject *event.PTPEventState
	input            inputState
}

type ClockStateTestCase struct {
	expectedCount    int
	eventStateObject *event.PTPEventState
	input            event.ClockState
}

var clockStateTestCase = []ClockStateTestCase{{
	eventStateObject: depObject,
	input: event.ClockState{
		State:       ptp.FREERUN,
		Offset:      pointer.Float64(01),
		IFace:       pointer.String("ens01"),
		Process:     "dpll",
		Value:       map[string]int64{"frequency_status": 2, "phase_status": 3, "pps_status": 1},
		ClockSource: event.DPLL,
		NodeName:    "test",
		HelpText: map[string]string{
			"frequency_status": "-1=UNKNOWN, 0=INVALID, 1=FREERUN, 2=LOCKED, 3=LOCKED_HO_ACQ, 4=HOLDOVER",
			"phase_status":     "-1=UNKNOWN, 0=INVALID, 1=FREERUN, 2=LOCKED, 3=LOCKED_HO_ACQ, 4=HOLDOVER",
			"pps_status":       "0=UNAVAILABLE, 1=AVAILABLE",
		},
	},
	expectedCount: 1,
},
	{eventStateObject: depObject,
		input: event.ClockState{
			State:       ptp.FREERUN,
			Offset:      pointer.Float64(01),
			IFace:       pointer.String("ens02"),
			Process:     "dpll",
			Value:       map[string]int64{"frequency_status": 2, "phase_status": 3, "pps_status": 1},
			ClockSource: event.DPLL,
			NodeName:    "test",
			HelpText: map[string]string{
				"frequency_status": "-1=UNKNOWN, 0=INVALID, 1=FREERUN, 2=LOCKED, 3=LOCKED_HO_ACQ, 4=HOLDOVER",
				"phase_status":     "-1=UNKNOWN, 0=INVALID, 1=FREERUN, 2=LOCKED, 3=LOCKED_HO_ACQ, 4=HOLDOVER",
				"pps_status":       "0=UNAVAILABLE, 1=AVAILABLE",
			},
		},
		expectedCount: 2,
	},
	{eventStateObject: depObject,
		input: event.ClockState{
			State:       ptp.FREERUN,
			Offset:      pointer.Float64(01),
			IFace:       pointer.String("ens01"),
			Process:     "GM",
			Value:       nil,
			Metric:      nil,
			ClockSource: event.GM,
			NodeName:    "test",
		},
		expectedCount: 1,
	},
	{eventStateObject: depObject,
		input: event.ClockState{
			State:       ptp.FREERUN,
			Offset:      pointer.Float64(01),
			IFace:       pointer.String("ens01"),
			Process:     "gnss",
			Value:       map[string]int64{"gnss_status": 3},
			ClockSource: event.GNSS,
			HelpText:    map[string]string{"gnss_status": "0=NOFIX, 1=Dead Reckoning Only, 2=2D-FIX, 3=3D-FIX, 4=GPS+dead reckoning fix, 5=Time only fix"},
			NodeName:    "test",
		},
		expectedCount: 1,
	}}
var testCase = []eventTestCase{{
	eventStateObject: &event.PTPEventState{
		CurrentPTPStateEvent: ptp.FREERUN,
		Type:                 ptp.PtpStateChange,

		DependsOn: map[string]event.DependingClockState{
			"TS2phc": {
				&event.ClockState{
					State:       ptp.FREERUN,
					Offset:      pointer.Float64(01),
					IFace:       nil,
					Process:     "GNSS",
					ClockSource: "",
					Value:       nil,
					Metric:      nil,
					NodeName:    "",
					HelpText:    nil,
				},
				&event.ClockState{
					State:       ptp.FREERUN,
					Offset:      pointer.Float64(01),
					IFace:       nil,
					Process:     "DPLL",
					ClockSource: "",
					Value:       nil,
					Metric:      nil,
					NodeName:    "",
					HelpText:    nil,
				},
			},
		}},
	input: inputState{
		state:   ptp.LOCKED,
		process: "GNSS",
		offset:  pointer.Float64(0),
	},
	expectedState: ptp.FREERUN,
},
	{
		eventStateObject: &event.PTPEventState{
			CurrentPTPStateEvent: ptp.FREERUN,
			Type:                 ptp.PtpStateChange,
			DependsOn: map[string]event.DependingClockState{
				"TS2phc": {
					&event.ClockState{
						State:       ptp.LOCKED,
						Offset:      pointer.Float64(01),
						IFace:       nil,
						Process:     "GNSS",
						ClockSource: "",
						Value:       nil,
						Metric:      nil,
						NodeName:    "",
						HelpText:    nil,
					},
					&event.ClockState{
						State:       ptp.LOCKED,
						Offset:      pointer.Float64(01),
						IFace:       nil,
						Process:     "DPLL",
						ClockSource: "",
						Value:       nil,
						Metric:      nil,
						NodeName:    "",
						HelpText:    nil,
					},
					&event.ClockState{
						State:       ptp.FREERUN,
						Offset:      pointer.Float64(99902),
						IFace:       nil,
						Process:     "ptp4l",
						ClockSource: "",
						Value:       nil,
						Metric:      nil,
						NodeName:    "",
						HelpText:    nil,
					},
				},
			}},
		input: inputState{
			state:   ptp.LOCKED,
			process: "ptp4l",
			offset:  pointer.Float64(05),
		},
		expectedState: ptp.FREERUN,
	},
}

func Test_UpdateEventState(t *testing.T) {
	for _, tc := range testCase {
		cSTate := tc.eventStateObject.UpdateCurrentEventState(event.ClockState{
			State:   tc.input.state,
			Offset:  tc.input.offset,
			Process: tc.input.process,
		}, nil, nil)
		assert.Equal(t, tc.expectedState, cSTate)
	}
}

func TestPTPEventState_UpdateCurrentEventState(t *testing.T) {
	for _, tc := range clockStateTestCase {
		tc.eventStateObject.UpdateCurrentEventState(tc.input, nil, nil)
		assert.Equal(t, tc.expectedCount, len(tc.eventStateObject.DependsOn[tc.input.Process]))
	}
}
