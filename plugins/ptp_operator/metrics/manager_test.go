//go:build unittests
// +build unittests

package metrics_test

import (
	"testing"

	ptpConfig "github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/config"
	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/metrics"
	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/stats"
	"github.com/redhat-cne/sdk-go/pkg/event/ptp"
)

func TestPTPEventManager_GenPTPEvent(t *testing.T) {
	tests := []struct {
		name              string
		ptpProfileName    string
		oStats            *stats.Stats
		eventResourceName string
		ptpOffset         int64
		clockState        ptp.SyncState
		lastClockState    ptp.SyncState
		eventType         ptp.EventType
		mock              bool
		wantLastSyncState ptp.SyncState
	}{
		{
			name:              "locked state within threshold",
			ptpProfileName:    "profile1",
			oStats:            stats.NewStats("profile1"),
			lastClockState:    ptp.LOCKED,
			eventResourceName: "resource1",
			ptpOffset:         100,
			clockState:        ptp.LOCKED,
			eventType:         ptp.PtpStateChange,
			mock:              true,
			wantLastSyncState: ptp.LOCKED,
		},
		{
			name:              "freerun state outside threshold",
			ptpProfileName:    "profile2",
			oStats:            stats.NewStats("profile2"),
			eventResourceName: "resource2",
			ptpOffset:         1000,
			clockState:        ptp.FREERUN,
			lastClockState:    ptp.LOCKED,
			eventType:         ptp.PtpStateChange,
			mock:              true,
			wantLastSyncState: ptp.FREERUN,
		},
		{
			name:              "freerun to Locked state",
			ptpProfileName:    "profile1",
			oStats:            stats.NewStats("profile1"),
			lastClockState:    ptp.FREERUN,
			eventResourceName: "resource1",
			ptpOffset:         100,
			clockState:        ptp.LOCKED,
			eventType:         ptp.PtpStateChange,
			mock:              true,
			wantLastSyncState: ptp.LOCKED,
		},
		{
			name:              "holdover to holdover state",
			ptpProfileName:    "profile3",
			oStats:            stats.NewStats("profile3"),
			eventResourceName: "resource3",
			ptpOffset:         500,
			lastClockState:    ptp.HOLDOVER,
			clockState:        ptp.FREERUN, // stays in HOLDOVER until times out
			eventType:         ptp.PtpStateChange,
			mock:              true,
			wantLastSyncState: ptp.HOLDOVER,
		},
		{
			name:              "holdover to locked state",
			ptpProfileName:    "profile3",
			oStats:            stats.NewStats("profile3"),
			eventResourceName: "resource3",
			ptpOffset:         50,
			lastClockState:    ptp.HOLDOVER,
			clockState:        ptp.LOCKED,
			eventType:         ptp.PtpStateChange,
			mock:              true,
			wantLastSyncState: ptp.LOCKED,
		},
		{
			name:              "holdover to locked state",
			ptpProfileName:    "T-GM",
			oStats:            stats.NewStats("T-GM"),
			eventResourceName: "resource3",
			ptpOffset:         50,
			lastClockState:    ptp.LOCKED,
			clockState:        ptp.HOLDOVER,
			eventType:         ptp.PtpStateChange,
			mock:              true,
			wantLastSyncState: ptp.HOLDOVER,
		},
		{
			name:              "holdover to locked state",
			ptpProfileName:    "T-GM",
			oStats:            stats.NewStats("T-GM"),
			eventResourceName: "resource3",
			ptpOffset:         50,
			lastClockState:    ptp.HOLDOVER,
			clockState:        ptp.HOLDOVER,
			eventType:         ptp.PtpStateChange,
			mock:              true,
			wantLastSyncState: ptp.HOLDOVER,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.oStats.SetLastSyncState(tt.lastClockState)
			p := &metrics.PTPEventManager{
				PtpConfigMapUpdates: &ptpConfig.LinuxPTPConfigMapUpdate{
					EventThreshold: map[string]*ptpConfig.PtpClockThreshold{
						tt.ptpProfileName: {
							MaxOffsetThreshold: 500,
							MinOffsetThreshold: 10,
						},
					},
				},
			}
			p.MockTest(tt.mock)
			p.GenPTPEvent(tt.ptpProfileName, tt.oStats, tt.eventResourceName, tt.ptpOffset, tt.clockState, tt.eventType)
			if got := tt.oStats.LastSyncState(); got != tt.wantLastSyncState {
				t.Errorf("GenPTPEvent() = %v, want %v", got, tt.wantLastSyncState)
			}
		})
	}
}
