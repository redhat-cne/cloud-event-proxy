//go:build unittests
// +build unittests

package metrics_test

import (
	"testing"

	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/types"

	"sync"

	"github.com/redhat-cne/cloud-event-proxy/pkg/common"
	ptpConfig "github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/config"
	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/metrics"
	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/stats"
	"github.com/redhat-cne/sdk-go/pkg/event/ptp"
	"github.com/stretchr/testify/assert"
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
		expectedEvents    []ptp.EventType
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
			expectedEvents:    []ptp.EventType{ptp.PtpStateChange, ptp.SyncStateChange},
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
			expectedEvents:    []ptp.EventType{ptp.PtpStateChange, ptp.SyncStateChange},
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
			expectedEvents:    []ptp.EventType{ptp.PtpStateChange, ptp.SyncStateChange},
		},
		{
			name:              "holdover to freerun state",
			ptpProfileName:    "profile3",
			oStats:            stats.NewStats("profile3"),
			eventResourceName: "resource3",
			ptpOffset:         500,
			lastClockState:    ptp.HOLDOVER,
			clockState:        ptp.FREERUN,
			eventType:         ptp.PtpStateChange,
			mock:              true,
			wantLastSyncState: ptp.FREERUN,
			expectedEvents:    []ptp.EventType{ptp.PtpStateChange, ptp.SyncStateChange},
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
			expectedEvents:    []ptp.EventType{ptp.PtpStateChange, ptp.SyncStateChange},
		},
		{
			name:              "locked to holdover state",
			ptpProfileName:    "T-GM",
			oStats:            stats.NewStats("T-GM"),
			eventResourceName: "resource3",
			ptpOffset:         50,
			lastClockState:    ptp.LOCKED,
			clockState:        ptp.HOLDOVER,
			eventType:         ptp.PtpStateChange,
			mock:              true,
			wantLastSyncState: ptp.HOLDOVER,
			expectedEvents:    []ptp.EventType{ptp.PtpStateChange, ptp.SyncStateChange},
		},
		{
			name:              "holdover to holdover state",
			ptpProfileName:    "T-GM",
			oStats:            stats.NewStats("T-GM"),
			eventResourceName: "resource3",
			ptpOffset:         50,
			lastClockState:    ptp.HOLDOVER,
			clockState:        ptp.HOLDOVER,
			eventType:         ptp.PtpStateChange,
			mock:              true,
			wantLastSyncState: ptp.HOLDOVER,
			expectedEvents:    []ptp.EventType{ptp.PtpStateChange, ptp.SyncStateChange},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.oStats.SetLastSyncState(tt.lastClockState)
			metrics.Filesystem = &metrics.MockFileSystem{}
			p := metrics.NewPTPEventManager("", nil, "testnode", &common.SCConfiguration{StorePath: "/tmp/store"})
			p.PtpConfigMapUpdates = &ptpConfig.LinuxPTPConfigMapUpdate{
				EventThreshold: map[string]*ptpConfig.PtpClockThreshold{
					tt.ptpProfileName: {
						MaxOffsetThreshold: 500,
						MinOffsetThreshold: 10,
					},
				},
			}
			p.MockTest(tt.mock)
			p.GenPTPEvent(tt.ptpProfileName, tt.oStats, tt.eventResourceName, tt.ptpOffset, tt.clockState, tt.eventType)
			if got := tt.oStats.LastSyncState(); got != tt.wantLastSyncState {
				t.Errorf("GenPTPEvent() = %v, want %v", got, tt.wantLastSyncState)
				assert.Equal(t, len(tt.expectedEvents), len(p.GetMockEvent()))
				for _, event := range tt.expectedEvents {
					assert.Contains(t, p.GetMockEvent(), event)
				}
			}

		})
	}
}

func TestConcurrentMapAccess(t *testing.T) {
	// Initialize the PTPEventManager with a map and a mutex
	metrics.Filesystem = &metrics.MockFileSystem{}
	manager := metrics.NewPTPEventManager("", nil, "testnode", &common.SCConfiguration{StorePath: "/tmp/store"})
	manager.PtpConfigMapUpdates = &ptpConfig.LinuxPTPConfigMapUpdate{
		EventThreshold: map[string]*ptpConfig.PtpClockThreshold{
			"ptofile": {
				MaxOffsetThreshold: 500,
				MinOffsetThreshold: 10,
			},
		},
	}
	manager.MockTest(true)
	// Function to simulate concurrent writes
	writeFunc := func(wg *sync.WaitGroup, key types.ConfigName, value stats.PTPStats) {
		defer wg.Done()
		manager.SetStats(key, value)
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

func TestGetNodeSyncState_WithOptionalCurrentState(t *testing.T) {

	tests := []struct {
		name           string
		statsData      map[types.ConfigName]stats.PTPStats
		currentState   ptp.SyncState
		expectedResult ptp.SyncState
	}{
		{
			name:           "Empty stats, no current state",
			statsData:      map[types.ConfigName]stats.PTPStats{},
			currentState:   "",
			expectedResult: ptp.FREERUN,
		},
		{
			name: "Only LOCKED stats",
			statsData: map[types.ConfigName]stats.PTPStats{
				"ptp4l.0.config": {
					metrics.MasterClockType: {},
				},
				"ptp4l.1.config": {
					metrics.MasterClockType: func() *stats.Stats {
						s := stats.NewStats("ptp4l.0.config")
						s.SetLastSyncState(ptp.LOCKED)
						return s
					}(),
				},
				"phc2sys.1.config": {
					metrics.ClockRealTime: func() *stats.Stats {
						s := stats.NewStats("phc2sys.1.config")
						s.SetLastSyncState(ptp.LOCKED)
						return s
					}(),
				},
			},
			currentState:   ptp.LOCKED,
			expectedResult: ptp.LOCKED,
		},
		{
			name: "Mixed stats with worst state FREERUN from currentState",
			statsData: map[types.ConfigName]stats.PTPStats{
				"ptp4l.0.config": {
					metrics.MasterClockType: func() *stats.Stats {
						s := stats.NewStats("ptp4l.0.config")
						s.SetLastSyncState(ptp.LOCKED)
						return s
					}(),
				},
				"ptp4l.1.config": {
					metrics.MasterClockType: func() *stats.Stats {
						s := stats.NewStats("ptp4l.1.config")
						s.SetLastSyncState(ptp.FREERUN)
						return s
					}(),
				},
				"phc2sys.1.config": {
					metrics.ClockRealTime: func() *stats.Stats {
						s := stats.NewStats("phc2sys.1.config")
						s.SetLastSyncState(ptp.LOCKED)
						return s
					}(),
				},
			},
			currentState:   ptp.FREERUN,
			expectedResult: ptp.FREERUN,
		},
		{
			name: "Dual BC- Nic one in Locked, Nic2 PHC is Holdover and OSClock- Freerun",
			statsData: map[types.ConfigName]stats.PTPStats{
				"ptp4l.0.config": {
					metrics.MasterClockType: func() *stats.Stats {
						s := stats.NewStats("ptp4l.0.config")
						s.SetLastSyncState(ptp.LOCKED)
						return s
					}(),
				},
				"ptp4l.1.config": {
					metrics.MasterClockType: func() *stats.Stats {
						s := stats.NewStats("ptp4l.1.config")
						s.SetLastSyncState(ptp.HOLDOVER)
						return s
					}(),
				},
				"phc2sys.1.config": { // OS CLock is freerun
					metrics.ClockRealTime: func() *stats.Stats {
						s := stats.NewStats("phc2sys.1.config")
						s.SetLastSyncState(ptp.FREERUN)
						return s
					}(),
				},
			},
			currentState:   ptp.LOCKED,
			expectedResult: ptp.FREERUN,
		},
		{
			name: "OC- PHC is in Holdover, osClock is in Freerun",
			statsData: map[types.ConfigName]stats.PTPStats{
				"ptp4l.0.config": {
					metrics.MasterClockType: func() *stats.Stats {
						s := stats.NewStats("ptp4l.0.config")
						s.SetLastSyncState(ptp.HOLDOVER)
						return s
					}(),
				},
				"phc2sys.1.config": {
					metrics.ClockRealTime: func() *stats.Stats {
						s := stats.NewStats("phc2sys.1.config")
						s.SetLastSyncState(ptp.FREERUN)
						return s
					}(),
				},
			},
			currentState:   ptp.LOCKED,
			expectedResult: ptp.FREERUN,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &metrics.PTPEventManager{
				PtpConfigMapUpdates: &ptpConfig.LinuxPTPConfigMapUpdate{},
				Stats:               tt.statsData,
			}
			p.MockTest(true)

			result := p.GetNodeSyncState(tt.currentState)
			if result != tt.expectedResult {
				t.Errorf("expected %s, got %s", tt.expectedResult, result)
			}
		})
	}
}

func TestOverallState(t *testing.T) {
	tests := []struct {
		name     string
		current  ptp.SyncState
		updated  ptp.SyncState
		expected ptp.SyncState
	}{
		// -- Basic transitions there can't be current state as empty string , if found ignore
		{"FREERUN + FREERUN", ptp.FREERUN, ptp.FREERUN, ptp.FREERUN},
		{"FREERUN + HOLDOVER", ptp.FREERUN, ptp.HOLDOVER, ptp.FREERUN},
		{"FREERUN + LOCKED", ptp.FREERUN, ptp.LOCKED, ptp.FREERUN},

		{"HOLDOVER + FREERUN", ptp.HOLDOVER, ptp.FREERUN, ptp.FREERUN},
		{"HOLDOVER + HOLDOVER", ptp.HOLDOVER, ptp.HOLDOVER, ptp.HOLDOVER},
		{"HOLDOVER + LOCKED", ptp.HOLDOVER, ptp.LOCKED, ptp.HOLDOVER},

		{"LOCKED + FREERUN", ptp.LOCKED, ptp.FREERUN, ptp.FREERUN},
		{"LOCKED + HOLDOVER", ptp.LOCKED, ptp.HOLDOVER, ptp.HOLDOVER},
		{"LOCKED + LOCKED", ptp.LOCKED, ptp.LOCKED, ptp.LOCKED},

		// -- Edge cases
		{"Current empty, updated LOCKED", "", ptp.LOCKED, ptp.LOCKED},
		{"Current empty, updated HOLDOVER", "", ptp.HOLDOVER, ptp.HOLDOVER},
		{"Current empty, updated FREERUN", "", ptp.FREERUN, ptp.FREERUN},

		{"Updated empty", ptp.LOCKED, "", ptp.LOCKED},
		{"Updated unknown", ptp.HOLDOVER, "UNKNOWN_STATE", ""}, // This would also log a warning
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := metrics.OverallState(tt.current, tt.updated)
			assert.Equal(t, tt.expected, result)
		})
	}
}
