package metrics_test

import (
	"sort"
	"strings"
	"testing"

	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/ptp4lconf"

	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/metrics"
	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/stats"
	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/types"
	"github.com/redhat-cne/sdk-go/pkg/event/ptp"
	"github.com/stretchr/testify/assert"
	"k8s.io/utils/pointer"
)

var (
	configName  = "ptp4l.0.config"
	ptp4lConfig = &ptp4lconf.PTP4lConfig{
		Name:    "ptp4l.0.config",
		Profile: "grandmaster",
		Interfaces: []*ptp4lconf.PTPInterface{
			{
				Name:     "ens2f0",
				PortID:   1,
				PortName: "port 1",
				Role:     2, //master
			},
			{
				Name:     "ens7f0",
				PortID:   2,
				PortName: "port 3",
				Role:     2, // master
			},
		},
	}
)

type testCase struct {
	processName   string
	output        string
	expectedState ptp.SyncState
	interfaceName string
}

// InitPubSubTypes ... initialize types of publishers for ptp operator
func initPubSubTypes() map[ptp.EventType]*types.EventPublisherType {
	InitPubs := make(map[ptp.EventType]*types.EventPublisherType)
	InitPubs[ptp.OsClockSyncStateChange] = &types.EventPublisherType{
		EventType: ptp.OsClockSyncStateChange,
		Resource:  ptp.OsClockSyncState,
	}
	InitPubs[ptp.PtpClockClassChange] = &types.EventPublisherType{
		EventType: ptp.PtpClockClassChange,
		Resource:  ptp.PtpClockClass,
	}
	InitPubs[ptp.PtpStateChange] = &types.EventPublisherType{
		EventType: ptp.PtpStateChange,
		Resource:  ptp.PtpLockState,
	}
	InitPubs[ptp.GnssStateChange] = &types.EventPublisherType{
		EventType: ptp.GnssStateChange,
		Resource:  ptp.GnssSyncStatus,
	}
	return InitPubs
}
func Test_ParseGNSSLogs(t *testing.T) {
	var ptpEventManager *metrics.PTPEventManager
	tc := []testCase{
		{
			processName:   "gnss",
			output:        "gnss[1689014431]:[ts2phc.0.config] ens2f1 gnss_status 5 offset 0 s0",
			expectedState: ptp.FREERUN,
			interfaceName: "ens2f1",
		},
		{
			processName:   "gnss",
			output:        "gnss[1689014431]:[ts2phc.0.config] ens2f1 gnss_status 5 offset 0 s2",
			expectedState: ptp.LOCKED,
			interfaceName: "ens2f1",
		},
	}
	ptpEventManager = metrics.NewPTPEventManager("", initPubSubTypes(), "tetsnode", nil)
	ptpEventManager.MockTest(true)
	ptpEventManager.Stats[types.ConfigName(ptp4lConfig.Name)] = make(stats.PTPStats)
	ptpStats := ptpEventManager.GetStats(types.ConfigName(configName))
	replacer := strings.NewReplacer("[", " ", "]", " ", ":", " ")
	for _, tt := range tc {
		output := replacer.Replace(tt.output)
		fields := strings.Fields(output)
		ptpEventManager.ParseGNSSLogs(tt.processName, configName, output, fields, ptpStats)
		lastState, errState := ptpStats[types.IFace(tt.interfaceName)].GetStateState(tt.processName, pointer.String(tt.interfaceName))
		assert.Equal(t, errState, nil)
		assert.Equal(t, tt.expectedState, lastState)
		for _, ptpStats := range ptpEventManager.Stats { // configname->PTPStats
			for _, s := range ptpStats {
				_, _, sync, _ := s.GetDependsOnValueState("gnss", nil, "gnss_status")
				assert.Equal(t, tt.expectedState, sync)
			}
		}
	}
}

func TestPTPEventManager_ParseDPLLLogs(t *testing.T) {
	var ptpEventManager *metrics.PTPEventManager
	tc := []testCase{
		{
			processName:   "dpll",
			output:        "dpll[1700598434]:[ts2phc.0.config] ens2f0 frequency_status 3 offset 0 phase_status 3 pps_status 1 s0",
			expectedState: ptp.FREERUN,
			interfaceName: "ens2f0",
		},
		{
			processName:   "dpll",
			output:        "dpll[1700598434]:[ts2phc.0.config] ens1f0 frequency_status 3 offset 0 phase_status 3 pps_status 1 s2",
			expectedState: ptp.LOCKED,
			interfaceName: "ens1f0",
		},
		{
			processName:   "dpll",
			output:        "dpll[1700598434]:[ts2phc.0.config] ens2f0 frequency_status 3 offset 0 phase_status 3 pps_status 1 s0",
			expectedState: ptp.FREERUN,
			interfaceName: "ens2f0",
		},
		{
			processName:   "dpll",
			output:        "dpll[1700598434]:[ts2phc.0.config] ens1f0 frequency_status 3 offset 0 phase_status 3 pps_status 1 s2",
			expectedState: ptp.LOCKED,
			interfaceName: "ens1f0",
		},
	}

	ptpEventManager = metrics.NewPTPEventManager("", initPubSubTypes(), "tetsnode", nil)
	ptpEventManager.Stats[types.ConfigName(ptp4lConfig.Name)] = make(stats.PTPStats)
	ptpStats := ptpEventManager.GetStats(types.ConfigName(configName))
	ptpEventManager.MockTest(true)
	replacer := strings.NewReplacer("[", " ", "]", " ", ":", " ")
	for _, tt := range tc {
		output := replacer.Replace(tt.output)
		fields := strings.Fields(output)
		ptpEventManager.ParseDPLLLogs(tt.processName, configName, output, fields, ptpStats)

		lastState, errState := ptpStats[types.IFace(tt.interfaceName)].GetStateState(tt.processName, pointer.String(tt.interfaceName))
		assert.Equal(t, errState, nil)
		assert.Equal(t, tt.expectedState, lastState)
	}
}
func TestParseSyncELogs(t *testing.T) {
	ptpEventManager := metrics.NewPTPEventManager("", initPubSubTypes(), "tetsnode", nil)
	processName := "synce4l"
	configName = "synce4l.0.config"
	ptpEventManager.Stats[types.ConfigName(ptp4lConfig.Name)] = make(stats.PTPStats)
	ptpStats := ptpEventManager.GetStats(types.ConfigName(configName))

	ptpEventManager.MockTest(true)

	tests := []struct {
		id            int
		name          string
		output        string
		fields        []string
		expectedStats *stats.SyncEStats
		expectErr     bool
	}{
		{
			id:            1,
			name:          "Invalid log format",
			output:        "invalid log format",
			fields:        []string{"invalid", "log", "format"},
			expectedStats: nil,
		},
		{
			id:     2,
			name:   "Valid log with clock_quality",
			output: "synce4l 1722458091 synce4l.0.config ens7f0 clock_quality PRTC device synce1 ext_ql 0x20 network_option 2 ql 0x1 s2",
			fields: []string{"synce4l", "1722458091", "synce4l.0.config", "ens7f0", "clock_quality", "PRTC", "device", "synce1", "ext_ql", "0x20", "network_option", "2", "ql", "0x1", "s2"},
			expectedStats: &stats.SyncEStats{
				Name:      "synce1",
				ExtSource: "",
				Port: map[string]*stats.PortState{"ens7f0": {
					Name:               "ens7f0",
					State:              ptp.SyncState("s2"),
					ClockQuality:       "PRTC",
					QL:                 0x1,
					ExtQL:              0x20,
					ExtendedTvlEnabled: true,
					LastQLState:        33,
				}},
				ClockState:    ptp.SyncState("s1"),
				NetworkOption: 1,
			},
		},
		{
			id:     3,
			name:   "Valid log with eec_state",
			output: "synce4l 1722456110 synce4l.0.config ens7f0 device synce1 eec_state EEC_HOLDOVER network_option 2 s1",
			fields: []string{"synce4l", "1722456110", "synce4l.0.config", "ens7f0", "device", "synce1", "eec_state", "EEC_HOLDOVER", "network_option", "2", "s1"},
			expectedStats: &stats.SyncEStats{
				Name:      "synce1",
				ExtSource: "",
				Port: map[string]*stats.PortState{"ens7f0": {
					Name:               "ens7f0",
					State:              ptp.SyncState("s1"),
					ClockQuality:       "",
					QL:                 0x1,
					ExtQL:              0x20,
					ExtendedTvlEnabled: true,
					LastQLState:        33,
				}},
				ClockState:    ptp.SyncState("s1"),
				NetworkOption: 1,
			},
		},
		{
			name:   "Valid log with clock_quality new device",
			output: "synce4l 1722458091 synce4l.0.config ens5f1 clock_quality PRTC device synce2 network_option 2 ql 0x1 s2",
			fields: []string{"synce4l", "1722458091", "synce4l.0.config", "ens5f1", "clock_quality", "PRTC", "device", "synce2", "network_option", "2", "ql", "0x1", "s2"},
			expectedStats: &stats.SyncEStats{
				Name:      "synce2",
				ExtSource: "",
				Port: map[string]*stats.PortState{"ens5f1": {
					Name:               "ens5f1",
					State:              ptp.SyncState("s2"),
					ClockQuality:       "PRTC",
					QL:                 0x1,
					ExtQL:              0,
					ExtendedTvlEnabled: false,
					LastQLState:        1,
				}},
				ClockState:    ptp.SyncState("s1"),
				NetworkOption: 1,
			},
		},
	}

	// Extract keys and sort them
	keys := make([]int, 0, len(tests))
	for key := range tests {
		keys = append(keys, key)
	}
	sort.Ints(keys)

	for _, k := range keys {
		tt := tests[k]
		t.Run(tt.name, func(t *testing.T) {
			ptpEventManager.ParseSyncELogs(processName, configName, tt.output, tt.fields, ptpStats)
			if tt.expectedStats == nil {
				assert.Nil(t, tt.expectedStats, ptpStats)
			} else {
				synceEStat := ptpStats[types.IFace(tt.expectedStats.Name)].GetSyncE()
				assert.Equal(t, tt.expectedStats.Name, synceEStat.Name)
				assert.Equal(t, 1, len(synceEStat.Port))
				for key, val := range synceEStat.Port {
					assert.Equal(t, tt.expectedStats.Port[key].Name, val.Name)
					assert.Equal(t, tt.expectedStats.Port[key].ExtendedTvlEnabled, val.ExtendedTvlEnabled)
					assert.Equal(t, tt.expectedStats.Port[key].ExtQL, val.ExtQL)
					assert.Equal(t, tt.expectedStats.Port[key].QL, val.QL)
					assert.Equal(t, tt.expectedStats.Port[key].LastQLState, val.LastQLState)
				}
			}
		})
	}
}
