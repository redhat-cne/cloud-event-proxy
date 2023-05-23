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

var testCase = []eventTestCase{{
	eventStateObject: &event.PTPEventState{
		CurrentPTPStateEvent: ptp.FREERUN,
		Type:                 ptp.PtpStateChange,
		DependsOn: map[string]*event.ClockState{"GNSS": {
			State:   ptp.FREERUN,
			Offset:  pointer.Float64(0),
			Process: "GNSS",
		}, "DPLL": {
			State:   ptp.FREERUN,
			Offset:  pointer.Float64(45670),
			Process: "DPLL",
		}},
	},
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
			DependsOn: map[string]*event.ClockState{"GNSS": {
				State:   ptp.LOCKED,
				Offset:  pointer.Float64(1),
				Process: "GNSS",
			}, "DPLL": {
				State:   ptp.LOCKED,
				Offset:  pointer.Float64(2),
				Process: "DPLL",
			}, "ptp4l": {
				State:   ptp.FREERUN,
				Offset:  pointer.Float64(99902),
				Process: "ptp4l",
			}},
		},
		input: inputState{
			state:   ptp.LOCKED,
			process: "ptp4l",
			offset:  pointer.Float64(05),
		},
		expectedState: ptp.LOCKED,
	},
	{
		eventStateObject: &event.PTPEventState{
			CurrentPTPStateEvent: ptp.HOLDOVER,
			Type:                 ptp.PtpStateChange,
			DependsOn: map[string]*event.ClockState{"pch2sys": {
				State:   ptp.LOCKED,
				Offset:  pointer.Float64(01),
				Process: "phc2sys",
			}, "ts2phc": {
				State:   ptp.HOLDOVER,
				Offset:  pointer.Float64(02),
				Process: "ts2phc",
			}},
		},
		input: inputState{
			state:   ptp.LOCKED,
			process: "ts2phc",
			offset:  pointer.Float64(03),
		},
		expectedState: ptp.LOCKED,
	},
}

func Test_UpdateEventState(t *testing.T) {
	for _, tc := range testCase {
		cSTate := tc.eventStateObject.UpdateCurrentEventState(event.ClockState{
			State:   tc.input.state,
			Offset:  tc.input.offset,
			Process: tc.input.process,
		})
		assert.Equal(t, tc.expectedState, cSTate)
	}
}
