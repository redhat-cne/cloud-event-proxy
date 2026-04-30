//go:build unittests
// +build unittests

package metrics_test

import (
	"testing"

	"github.com/redhat-cne/cloud-event-proxy/pkg/common"
	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/metrics"
	ceevent "github.com/redhat-cne/sdk-go/pkg/event"
	"github.com/redhat-cne/sdk-go/pkg/event/ptp"
	"github.com/stretchr/testify/assert"
)

func newTestEventManager() *metrics.PTPEventManager {
	metrics.Filesystem = &metrics.MockFileSystem{}
	return metrics.NewPTPEventManager("/cluster/node", nil, "testnode", &common.SCConfiguration{StorePath: "/tmp/store"})
}

func TestGetPTPEventsData(t *testing.T) {
	tests := []struct {
		name            string
		state           ptp.SyncState
		ptpOffset       int64
		source          string
		eventType       ptp.EventType
		wantStateValue  bool
		wantOffsetValue bool
	}{
		{
			name:            "state change with non-empty state includes state and excludes offset",
			state:           ptp.LOCKED,
			ptpOffset:       100,
			source:          "ens1f0/master",
			eventType:       ptp.SyncStateChange,
			wantStateValue:  true,
			wantOffsetValue: false,
		},
		{
			name:            "state change with empty state excludes both",
			state:           "",
			ptpOffset:       100,
			source:          "ens1f0/master",
			eventType:       ptp.SyncStateChange,
			wantStateValue:  false,
			wantOffsetValue: false,
		},
		{
			name:            "ptp state change with non-empty state includes both",
			state:           ptp.FREERUN,
			ptpOffset:       -50,
			source:          "ens1f0/master",
			eventType:       ptp.PtpStateChange,
			wantStateValue:  true,
			wantOffsetValue: true,
		},
		{
			name:            "ptp state change with empty state excludes state, includes offset",
			state:           "",
			ptpOffset:       200,
			source:          "ens1f0/master",
			eventType:       ptp.PtpStateChange,
			wantStateValue:  false,
			wantOffsetValue: true,
		},
		{
			name:            "clock class change ignores state, includes offset",
			state:           ptp.LOCKED,
			ptpOffset:       6,
			source:          "ens1f0/master",
			eventType:       ptp.PtpClockClassChange,
			wantStateValue:  false,
			wantOffsetValue: true,
		},
		{
			name:            "clock class change with empty state still includes offset",
			state:           "",
			ptpOffset:       248,
			source:          "ens1f0/master",
			eventType:       ptp.PtpClockClassChange,
			wantStateValue:  false,
			wantOffsetValue: true,
		},
		{
			name:            "synce clock quality change ignores state, includes offset",
			state:           ptp.LOCKED,
			ptpOffset:       1,
			source:          "ens1f0",
			eventType:       ptp.SynceClockQualityChange,
			wantStateValue:  false,
			wantOffsetValue: true,
		},
		{
			name:            "synce state change with non-empty state includes state, excludes offset",
			state:           ptp.LOCKED,
			ptpOffset:       0,
			source:          "ens1f0",
			eventType:       ptp.SynceStateChange,
			wantStateValue:  true,
			wantOffsetValue: false,
		},
		{
			name:            "os clock sync state change includes both",
			state:           ptp.HOLDOVER,
			ptpOffset:       -3,
			source:          "CLOCK_REALTIME",
			eventType:       ptp.OsClockSyncStateChange,
			wantStateValue:  true,
			wantOffsetValue: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newTestEventManager()
			data := p.GetPTPEventsData(tt.state, tt.ptpOffset, tt.source, tt.eventType)

			assert.NotNil(t, data)
			assert.Equal(t, ceevent.APISchemaVersion, data.Version)

			var hasState, hasOffset bool
			for _, v := range data.Values {
				if v.DataType == ceevent.NOTIFICATION && v.ValueType == ceevent.ENUMERATION {
					hasState = true
					assert.Equal(t, tt.state, v.Value)
				}
				if v.DataType == ceevent.METRIC && v.ValueType == ceevent.DECIMAL {
					hasOffset = true
					assert.Equal(t, tt.ptpOffset, v.Value)
				}
			}
			assert.Equal(t, tt.wantStateValue, hasState, "state value presence mismatch")
			assert.Equal(t, tt.wantOffsetValue, hasOffset, "offset value presence mismatch")
		})
	}
}
