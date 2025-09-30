package metrics_test

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/redhat-cne/cloud-event-proxy/pkg/common"
	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/event"
	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/ptp4lconf"

	"encoding/json"

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

func Test_ParseTBCLogs(t *testing.T) {
	var ptpEventManager *metrics.PTPEventManager
	tc := []testCase{
		{
			processName:   "T-BC",
			output:        "T-BC[1743005894]:[ptp4l.0.config] ens2f0  offset 123 T-BC-STATUS s0",
			expectedState: ptp.FREERUN,
			interfaceName: "ens2f0",
		},
		{
			processName:   "T-BC",
			output:        "T-BC[1743005894]:[ptp4l.0.config] ens2f0  offset  55 T-BC-STATUS s2",
			expectedState: ptp.LOCKED,
			interfaceName: "ens2f0",
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
		ptpEventManager.ParseTBCLogs(tt.processName, configName, output, fields, ptpStats)
		lastState, errState := ptpStats[types.IFace(metrics.MasterClockType)].GetStateState(tt.processName, pointer.String(tt.interfaceName))
		assert.Equal(t, nil, errState)
		assert.Equal(t, tt.expectedState, lastState)
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
					State:              metrics.GetSyncState("s2"),
					ClockQuality:       "PRTC",
					QL:                 0x1,
					ExtQL:              0x20,
					ExtendedTvlEnabled: true,
					LastQLState:        33,
				}},
				ClockState:    metrics.GetSyncState("s2"),
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
					State:              metrics.GetSyncState("s1"),
					ClockQuality:       "",
					QL:                 0x1,
					ExtQL:              0x20,
					ExtendedTvlEnabled: true,
					LastQLState:        33,
				}},
				ClockState:    metrics.GetSyncState("s1"),
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
					State:              metrics.GetSyncState("s2"),
					ClockQuality:       "PRTC",
					QL:                 0x1,
					ExtQL:              0,
					ExtendedTvlEnabled: false,
					LastQLState:        1,
				}},
				ClockState:    metrics.GetSyncState("s2"),
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
					assert.Equal(t, tt.expectedStats.ClockState, val.State)
					assert.Equal(t, tt.expectedStats.Port[key].State, val.State)
				}
			}
		})
	}
}

func Test_ParseGmLogs(t *testing.T) {
	var ptpEventManager *metrics.PTPEventManager
	tc := []testCase{
		{
			processName:   "GM",
			output:        "GM 1689014431 ts2phc.0.config ens2f1 T-GM-STATUS s0",
			expectedState: ptp.FREERUN,
			interfaceName: "ens2f1",
		},
		{
			processName:   "GM",
			output:        "GM 1689014431 ts2phc.0.config ens2f1 T-GM-STATUS s1",
			expectedState: ptp.HOLDOVER,
			interfaceName: "ens2f1",
		},
		{
			processName:   "GM",
			output:        "GM 1689014431 ts2phc.0.config ens2f1 T-GM-STATUS s2",
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
		tt := tt
		output := replacer.Replace(tt.output)
		fields := strings.Fields(output)
		ptpStats[types.IFace(tt.interfaceName)] = &stats.Stats{}
		ptpStats[types.IFace(tt.interfaceName)].SetPtpDependentEventState(
			event.ClockState{
				State:       metrics.GetSyncState("s2"),
				Offset:      pointer.Float64(0),
				IFace:       &tt.interfaceName,
				Process:     "dpll",
				ClockSource: event.DPLL,
				Value:       map[string]int64{"frequency_status": 2, "phase_status": int64(0), "pps_status": int64(2)},
				Metric:      map[string]*event.PMetric{},
				NodeName:    "tetsnode",
				HelpText:    map[string]string{"phase_status": "-1=UNKNOWN, 0=INVALID, 1=FREERUN, 2=LOCKED, 3=LOCKED_HO_ACQ, 4=HOLDOVER", "frequency_status": "-1=UNKNOWN, 0=INVALID, 1=FREERUN, 2=LOCKED, 3=LOCKED_HO_ACQ, 4=HOLDOVER", "pps_status": "0=UNAVAILABLE, 1=AVAILABLE"},
			}, ptpStats.HasMetrics("dpll"), ptpStats.HasMetricHelp("dpll"))
		masterType := types.IFace(metrics.MasterClockType)
		ptpStats[masterType] = &stats.Stats{}
		ptpEventManager.ParseGMLogs(tt.processName, configName, output, fields, ptpStats)
		lastState, errState := ptpStats[masterType].GetStateState(tt.processName, pointer.String(tt.interfaceName))
		assert.Equal(t, errState, nil)
		assert.Equal(t, tt.expectedState, lastState)
	}
}

func Test_ParsTBCLogs(t *testing.T) {
	var eventManager *metrics.PTPEventManager
	tc := []testCase{
		{
			processName:   "T-BC",
			output:        "T-BC 1689014431 ts2phc.0.config ens2f1 offset  123  T-BC-STATUS s0",
			expectedState: ptp.FREERUN,
			interfaceName: "ens2f1",
		},
		{
			processName:   "T-BC",
			output:        "T-BC 1689014431 ts2phc.0.config ens2f1 offset  123  T-BC-STATUS s1",
			expectedState: ptp.HOLDOVER,
			interfaceName: "ens2f1",
		},
		{
			processName:   "T-BC",
			output:        "T-BC 1689014431 ts2phc.0.config ens2f1 offset  123  T-BC-STATUS s2",
			expectedState: ptp.LOCKED,
			interfaceName: "ens2f1",
		},
	}
	eventManager = metrics.NewPTPEventManager("", initPubSubTypes(), "tetsnode", nil)
	eventManager.MockTest(true)
	eventManager.Stats[types.ConfigName(ptp4lConfig.Name)] = make(stats.PTPStats)
	ptpStats := eventManager.GetStats(types.ConfigName(configName))
	replacer := strings.NewReplacer("[", " ", "]", " ", ":", " ")
	for _, tt := range tc {
		tt := tt
		output := replacer.Replace(tt.output)
		fields := strings.Fields(output)
		ptpStats[types.IFace(tt.interfaceName)] = &stats.Stats{}
		masterType := types.IFace(metrics.MasterClockType)
		ptpStats[masterType] = &stats.Stats{}
		eventManager.ParseTBCLogs(tt.processName, configName, output, fields, ptpStats)
		lastState, errState := ptpStats[masterType].GetStateState(tt.processName, pointer.String(tt.interfaceName))
		assert.Equal(t, errState, nil)
		assert.Equal(t, tt.expectedState, lastState)
	}
}

func TestExtractPTP4lEventState(t *testing.T) {
	tests := []struct {
		name          string
		logLine       string
		expectedPort  int
		expectedRole  types.PtpPortRole
		expectedState ptp.SyncState
	}{

		{
			name:          "b49691.fffe.a3f27c-1 changed state",
			logLine:       "phc2sys[152918.606]: [phc2sys.2.config:6] port b49691.fffe.a3f27c-1 changed state",
			expectedPort:  0,
			expectedRole:  types.UNKNOWN,
			expectedState: ptp.FREERUN,
		},

		{
			name:          "INITIALIZING to LISTENING",
			logLine:       "ptp4l.0.config  port 2 (ens1f1)  INITIALIZING to LISTENING on INIT_COMPLETE",
			expectedPort:  2,
			expectedRole:  types.LISTENING,
			expectedState: ptp.FREERUN,
		},
		{
			name:          "LISTENING to UNCALIBRATED ",
			logLine:       "ptp4l.1.config  port 1 (ens2f0)  LISTENING to UNCALIBRATED on RS_SLAVE",
			expectedPort:  1,
			expectedRole:  types.FAULTY,
			expectedState: ptp.HOLDOVER,
		},
		{
			name:          "FAULTY on FAULT_DETECTED",
			logLine:       "ptp4l[72444.514]: [ptp4l.0.config:5] port 1 (ens1f0): SLAVE to FAULTY on FAULT_DETECTED (FT_UNSPECIFIED)",
			expectedPort:  1,
			expectedRole:  types.FAULTY,
			expectedState: ptp.HOLDOVER,
		},
		{
			name:          "LISTENING to UNCALIBRATED",
			logLine:       "ptp4l[72530.751]: [ptp4l.0.config:5] port 1 (ens1f0): LISTENING to UNCALIBRATED on RS_SLAVE",
			expectedPort:  1,
			expectedRole:  types.FAULTY,
			expectedState: ptp.HOLDOVER,
		},
		{
			name:          "UNCALIBRATED to SLAVE ",
			logLine:       "ptp4l[72530.885]: [ptp4l.0.config:5] port 1 (ens1f0): UNCALIBRATED to SLAVE on MASTER_CLOCK_SELECTED",
			expectedPort:  1,
			expectedRole:  types.SLAVE,
			expectedState: ptp.FREERUN,
		},
		{
			name:          "SLAVE to UNCALIBRATED",
			logLine:       "ptp4l[5199193.712]: [ptp4l.0.config] port 1: SLAVE to UNCALIBRATED on SYNCHRONIZATION_FAULT",
			expectedPort:  1,
			expectedRole:  types.FAULTY,
			expectedState: ptp.HOLDOVER,
		},
		{
			name:          "LISTENING to SLAVE",
			logLine:       "ptp4l[5199200.100]: [ptp4l.0.config] port 1: LISTENING to SLAVE",
			expectedPort:  1,
			expectedRole:  types.SLAVE,
			expectedState: ptp.FREERUN,
		},
		{
			name:          "SLAVE to PASSIVE",
			logLine:       "ptp4l[5199210.200]: [ptp4l.0.config] port 1: SLAVE to PASSIVE",
			expectedPort:  1,
			expectedRole:  types.PASSIVE,
			expectedState: ptp.FREERUN,
		},
		{
			name:          "SLAVE to MASTER",
			logLine:       "ptp4l[5199220.300]: [ptp4l.0.config] port 1: SLAVE to MASTER",
			expectedPort:  1,
			expectedRole:  types.MASTER,
			expectedState: ptp.HOLDOVER,
		},
		{
			name:          "INITIALIZING to LISTENING (follower only)",
			logLine:       "ptp4l[5199230.400]: [ptp4l.0.config] port 1: INITIALIZING to LISTENING",
			expectedPort:  1,
			expectedRole:  types.LISTENING,
			expectedState: ptp.FREERUN,
		},
	}

	for _, tc := range tests {
		tc := tc
		for _, followers := range []int{0, 1, 2} {
			t.Run(tc.name, func(t *testing.T) {
				portID, role, clockState := metrics.TestFuncExtractPTP4lEventState(tc.logLine)

				if portID != tc.expectedPort {
					assert.Equal(t, tc.expectedPort, portID, fmt.Sprintf("with followers(%d) portID = %d; want %d", followers, portID, tc.expectedPort))
				}
				if role != tc.expectedRole {
					assert.Equal(t, tc.expectedRole, role, fmt.Sprintf("with followers(%d) role = %v; want %v", followers, role, tc.expectedRole))
				}
				if clockState != tc.expectedState {
					assert.Equal(t, tc.expectedPort, clockState, fmt.Sprintf("with followers(%d) state = %v; want %v", followers, clockState, tc.expectedState))
				}
			})
		}
	}
}

func TestParsePTP4l(t *testing.T) {
	tests := []struct {
		name                string
		processName         string
		configName          string
		profileName         string
		output              string
		fields              []string
		expectedStateChange bool
		expectedMasterState ptp.SyncState // expected state when exiting
		expectedOSState     ptp.SyncState
		ptpInterface        ptp4lconf.PTPInterface
		ptp4lCfg            *ptp4lconf.PTP4lConfig
		lastSyncState       ptp.SyncState // what ws the last sync state before entering new state
		expectedError       bool
		expectedMockWrites  int
	}{
		{
			name:        "valid CLOCK_CLASS_CHANGE event",
			processName: "ptp4l",
			configName:  "ptp4l.0.config",
			profileName: "test-profile",
			output:      "ptp4l 1646672953 ptp4l.0.config CLOCK_CLASS_CHANGE 165.000000",
			fields:      []string{"ptp4l", "1646672953", "ptp4l.0.config", "CLOCK_CLASS_CHANGE", "165.000000"},
			ptpInterface: ptp4lconf.PTPInterface{
				Name: "ens2f0",
			},
			ptp4lCfg: &ptp4lconf.PTP4lConfig{
				Interfaces: []*ptp4lconf.PTPInterface{
					{
						Name:     "ens2f0",
						PortID:   1,
						PortName: "port 1",
						Role:     types.SLAVE, //master
					},
					{
						Name:     "ens2f1",
						PortID:   2,
						PortName: "port 2",
						Role:     types.MASTER, // master
					},
				},
			},
			expectedError: false,
		},
		{
			name:        "SLAVE changing to FAULT",
			processName: "ptp4l",
			configName:  "ptp4l.0.config",
			profileName: "test-profile",
			output:      "ptp4l[72444.514]: [ptp4l.0.config:5] port 1 (ens2f0): SLAVE to FAULTY on FAULT_DETECTED (FT_UNSPECIFIED)",
			fields:      []string{"ptp4l", "1646672953", "ptp4l.0.config", "port", "1", "(ens2f0)", "SLAVE to FAULTY on FAULT_DETECTED (FT_UNSPECIFIED)"},
			ptpInterface: ptp4lconf.PTPInterface{
				Name: "ens2f0",
			},
			ptp4lCfg: &ptp4lconf.PTP4lConfig{
				Interfaces: []*ptp4lconf.PTPInterface{
					{
						Name:     "ens2f0",
						PortID:   1,
						PortName: "port 1",
						Role:     types.SLAVE,
					},
					{
						Name:     "ens2f1",
						PortID:   2,
						PortName: "port 2",
						Role:     types.MASTER, // master
					},
				},
			},
			expectedMasterState: ptp.HOLDOVER,
			expectedStateChange: true,
			lastSyncState:       ptp.LOCKED,
			expectedError:       false,
			expectedMockWrites:  1,
		},
		{
			name:        "FAULTY Changing to SLAVE",
			processName: "ptp4l",
			configName:  "ptp4l.0.config",
			profileName: "test-profile",
			output:      "ptp4l[72444.514]: [ptp4l.0.config:5] port 1 (ens2f0): FAULTY to SLAVE",
			fields:      []string{"ptp4l", "1646672953", "ptp4l.0.config", "port", "1", "(ens2f0)", "FAULTY to SLAVE"},
			ptpInterface: ptp4lconf.PTPInterface{
				Name: "ens2f0",
			},
			ptp4lCfg: &ptp4lconf.PTP4lConfig{
				Interfaces: []*ptp4lconf.PTPInterface{
					{
						Name:     "ens2f0",
						PortID:   1,
						PortName: "port 1",
						Role:     types.FAULTY,
					},
					{
						Name:     "ens2f1",
						PortID:   2,
						PortName: "port 2",
						Role:     types.MASTER, // master
					},
				},
			},
			expectedMasterState: ptp.HOLDOVER, //stays in holdover in 4.18 , when backporting we need to check this to make it FREERUN
			lastSyncState:       ptp.HOLDOVER,
			expectedStateChange: true,
			expectedError:       false,
		},
		{
			name:        "LISTENING Changing to SLAVE",
			processName: "ptp4l",
			configName:  "ptp4l.0.config",
			profileName: "test-profile",
			output:      "ptp4l[72444.514]: [ptp4l.0.config:5] port 1 (ens2f1): LISTENING to SLAVE",
			fields:      []string{"ptp4l", "1646672953", "ptp4l.0.config", "port", "2", "(ens2f1)", "LISTENING to SLAVE"},
			ptpInterface: ptp4lconf.PTPInterface{
				Name: "ens2f0",
			},
			ptp4lCfg: &ptp4lconf.PTP4lConfig{
				Interfaces: []*ptp4lconf.PTPInterface{
					{
						Name:     "ens2f0",
						PortID:   1,
						PortName: "port 1",
						Role:     types.FAULTY,
					},
					{
						Name:     "ens2f1",
						PortID:   2,
						PortName: "port 2",
						Role:     types.LISTENING, // master
					},
				},
			},
			expectedMasterState: ptp.FREERUN, // Here if the last state was freerun then satys in free run until it gets a locked
			lastSyncState:       ptp.FREERUN,
			expectedStateChange: true,
			expectedError:       false,
			expectedMockWrites:  1,
		},
		{
			name:        "SLAVE Changing to LISTENING",
			processName: "ptp4l",
			configName:  "ptp4l.0.config",
			profileName: "test-profile",
			output:      "ptp4l[72444.514]: [ptp4l.0.config:5] port 1 (ens2f0): SLAVE to LISTENING",
			fields:      []string{"ptp4l", "1646672953", "ptp4l.0.config", "port", "2", "(ens2f0)", "SLAVE to LISTENING"},
			ptpInterface: ptp4lconf.PTPInterface{
				Name: "ens2f0",
			},
			ptp4lCfg: &ptp4lconf.PTP4lConfig{
				Interfaces: []*ptp4lconf.PTPInterface{
					{
						Name:     "ens2f0",
						PortID:   1,
						PortName: "port 1",
						Role:     types.SLAVE,
					},
					{
						Name:     "ens2f1",
						PortID:   2,
						PortName: "port 2",
						Role:     types.LISTENING, // master
					},
				},
			},
			expectedMasterState: ptp.HOLDOVER, //stays in holdover in 4.18 , when backporting we need to check this to make it FREERUN
			lastSyncState:       ptp.LOCKED,
			expectedStateChange: true,
			expectedError:       false,
			expectedMockWrites:  1,
		},
	}

	// Mock os out so tests don't write to store
	mockFS := &metrics.MockFileSystem{}
	metrics.Filesystem = mockFS
	ptpEventManager := metrics.NewPTPEventManager("", initPubSubTypes(), "tetsnode", &common.SCConfiguration{StorePath: "/tmp/store"})
	ptpEventManager.MockTest(true)
	for _, followers := range []int{1, 2} {
		followers := followers // 👈 capture the value of `followers` too
		for _, tt := range tests {
			mockFS.Clear()
			tt := tt // Important: capture tt for each iteration
			tt.ptp4lCfg = deepCopyPTP4lCfg(tt.ptp4lCfg)
			t.Run(tt.name, func(t *testing.T) {
				if followers == 2 {
					tt.ptp4lCfg.Interfaces[1].UpdateRole(types.LISTENING)
				}

				metrics.SetMasterOffsetSource(tt.processName)
				ptpEventManager.Stats[types.ConfigName(tt.configName)] = make(stats.PTPStats)
				ptpStats := ptpEventManager.GetStats(types.ConfigName(tt.configName))
				ptpStats.CheckSource(metrics.ClockRealTime, configName, tt.processName)
				ptpStats.CheckSource(metrics.MasterClockType, configName, tt.processName)
				ptpStats[metrics.MasterClockType].SetLastSyncState(tt.lastSyncState)
				ptpEventManager.ParsePTP4l(tt.processName, tt.configName, tt.profileName, tt.output, tt.fields, tt.ptpInterface, tt.ptp4lCfg, ptpStats)
				if tt.expectedStateChange {
					assert.Equal(t, tt.expectedMasterState, ptpStats[metrics.MasterClockType].LastSyncState(), fmt.Sprintf("%s-followers(%d) state = %v; want %v", tt.name, followers, ptpStats[metrics.MasterClockType].LastSyncState(), tt.expectedMasterState))
				}
				assert.Equal(t, tt.expectedMockWrites, mockFS.WriteCount)
			})
		}
	}
}

func deepCopyPTP4lCfg(src *ptp4lconf.PTP4lConfig) *ptp4lconf.PTP4lConfig {
	var copied ptp4lconf.PTP4lConfig
	raw, _ := json.Marshal(src)
	_ = json.Unmarshal(raw, &copied)
	return &copied
}

func TestExtractMetrics_SkipsUTCTAIOffsetWarning(t *testing.T) {
	// Test that UTC-TAI offset warning messages are properly skipped
	// This log line should be filtered out because it doesn't contain a config name pattern
	utcTaiLogLine := "ts2phc[4331812.338] [ptp4l.0.config]: UTC-TAI offset not set in system! Trying to revert to leapfile"

	// Create event manager for testing
	ptpEventManager := metrics.NewPTPEventManager("", initPubSubTypes(), "testnode", nil)
	ptpEventManager.MockTest(true)

	// Set up a config so we can verify no stats are created for it
	ptpEventManager.AddPTPConfig(types.ConfigName(ptp4lConfig.Name), ptp4lConfig)
	ptpEventManager.Stats[types.ConfigName(ptp4lConfig.Name)] = make(stats.PTPStats)

	// Get initial stats count
	if len(ptpEventManager.Stats[types.ConfigName(ptp4lConfig.Name)]) != 0 {
		t.Fatalf("Should be empty for %s, but got length of %d", ptp4lConfig.Name, len(ptpEventManager.Stats[types.ConfigName(ptp4lConfig.Name)]))
	}

	// Reset mock events before test
	ptpEventManager.ResetMockEvent()

	// Extract metrics from the UTC-TAI log line
	ptpEventManager.ExtractMetrics(utcTaiLogLine)

	// Verify no events were generated (log line should be skipped)
	events := ptpEventManager.GetMockEvent()
	assert.Empty(t, events, "No events should be generated for UTC-TAI offset warning log lines")

	// Verify no new stats were added (no metrics created)
	if len(ptpEventManager.Stats[types.ConfigName(ptp4lConfig.Name)]) != 0 {
		t.Fatalf("Expected no stats to be created for %s, but got %d", ptp4lConfig.Name, len(ptpEventManager.Stats[types.ConfigName(ptp4lConfig.Name)]))
	}
}
