//go:build unittests
// +build unittests

package metrics_test

import (
	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/types"
	"testing"

	ptpConfig "github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/config"
	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/metrics"
	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/stats"
	"github.com/redhat-cne/sdk-go/pkg/event/ptp"
	"github.com/stretchr/testify/assert"
	"sync"
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

func TestConcurrentMapAccess(t *testing.T) {
	// Initialize the PTPEventManager with a map and a mutex
	manager := &metrics.PTPEventManager{
		Stats: map[types.ConfigName]stats.PTPStats{},
		PtpConfigMapUpdates: &ptpConfig.LinuxPTPConfigMapUpdate{
			EventThreshold: map[string]*ptpConfig.PtpClockThreshold{
				"ptofile": {
					MaxOffsetThreshold: 500,
					MinOffsetThreshold: 10,
				},
			},
		},
	}
	manager.MockTest(true)
	// Function to simulate concurrent writes
	writeFunc := func(wg *sync.WaitGroup, key string, value stats.PTPStats) {
		defer wg.Done()
		manager.GetStats(types.ConfigName(configName))
	}

	// Function to simulate concurrent reads
	readFunc := func(wg *sync.WaitGroup, key types.ConfigName) {
		defer wg.Done()
		manager.GetStats(key)
	}
	// Function to simulate concurrent reads
	readInterfaceFunc := func(wg *sync.WaitGroup, key types.ConfigName) {
		defer wg.Done()
		manager.GetStatsForInterface(types.ConfigName("profile"), types.IFace("ens10"))
	}

	var wg sync.WaitGroup
	numGoroutines := 1000

	// Start multiple goroutines to write to the map
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go writeFunc(&wg, "key", stats.PTPStats{})
	}

	// Start multiple goroutines to read from the map
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go readFunc(&wg, "key")
	}
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go readInterfaceFunc(&wg, "key")
	}

	// Wait for all goroutines to finish
	wg.Wait()
}

func TestListHAProfilesWith(t *testing.T) {
	// Setup
	manager := &metrics.PTPEventManager{
		Stats: map[types.ConfigName]stats.PTPStats{},
		PtpConfigMapUpdates: &ptpConfig.LinuxPTPConfigMapUpdate{
			PtpSettings: map[string]map[string]string{
				"pProfile": {
					"haProfiles": "profile1,profile2",
				},
			},
		},
	}

	// Case 1: Found in node1
	_, result := manager.ListHAProfilesWith("profile2")
	assert.ElementsMatch(t, []string{"profile1", "profile2"}, result)

	// Case 3: Not found
	_, result = manager.ListHAProfilesWith("unknown")
	assert.Equal(t, len(result), 0)

	// Case 4: Empty input
	_, result = manager.ListHAProfilesWith(" ")
	assert.Equal(t, len(result), 0)

	// Case 5: Empty settings
	manager.PtpConfigMapUpdates = &ptpConfig.LinuxPTPConfigMapUpdate{
		PtpSettings: map[string]map[string]string{},
	}
	_, result = manager.ListHAProfilesWith("profile1")
	assert.Equal(t, len(result), 0)
}
