package metrics_test

import (
	"strings"
	"testing"

	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/event"
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
