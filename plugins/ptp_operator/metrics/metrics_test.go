// Copyright 2023 The Cloud Native Events Authors
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

package metrics_test

import (
	"os"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/redhat-cne/cloud-event-proxy/pkg/common"
	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/config"
	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/metrics"
	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/ptp4lconf"
	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/stats"
	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/types"
	"github.com/redhat-cne/sdk-go/pkg/event/ptp"
	"github.com/stretchr/testify/assert"
)

const (
	MYNODE = "mynode"
	// SKIP skip the verification of the metric
	CLEANUP = -1
)

var logPtp4lConfig = &ptp4lconf.PTP4lConfig{
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

// DualFollower
var logPtp4lConfigDualFollower = &ptp4lconf.PTP4lConfig{
	Name:    "ptp4l.1.config",
	Profile: "dualFollower",
	Interfaces: []*ptp4lconf.PTPInterface{
		{
			Name:     "ens3f0",
			PortID:   1,
			PortName: "port 4",
			Role:     1, // slave
		},
		{
			Name:     "ens3f1",
			PortID:   2,
			PortName: "port 5",
			Role:     5, // listening
		},
	},
	Sections: map[string]map[string]string{
		"ens3f0": {
			"masterOnly": "0",
		},
		"ens3f1": {"masterOnly": "0"},
		"global": {
			"slaveOnly": "1",
		},
	},
}

var ptpEventManager *metrics.PTPEventManager
var scConfig *common.SCConfiguration
var resourcePrefix = ""

// InitPubSubTypes ... initialize types of publishers for ptp operator
func InitPubSubTypes() map[ptp.EventType]*types.EventPublisherType {
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

type TestCase struct {
	log           string
	from          string
	process       string
	node          string
	iface         string
	lastSyncState ptp.SyncState

	// offset_ns
	expectedPtpOffsetCheck bool
	expectedPtpOffset      float64
	// max_offset_ns
	expectedPtpMaxOffsetCheck bool
	expectedPtpMaxOffset      float64
	// frequency_adjustment_ns
	expectedPtpFrequencyAdjustmentCheck bool
	expectedPtpFrequencyAdjustment      float64
	// delay_ns
	expectedPtpDelayCheck bool
	expectedPtpDelay      float64
	// clock_state
	expectedSyncStateCheck bool
	expectedSyncState      float64
	// nmea_status
	expectedNmeaStatusCheck bool
	expectedNmeaStatus      float64
	// pps_status
	expectedPpsStatusCheck bool
	expectedPpsStatus      float64
	// clock_class
	expectedClockClassMetricsCheck bool
	expectedClockClassMetrics      float64
	//role
	expectedRoleCheck bool
	expectedRole      types.PtpPortRole

	expectedEvent        []ptp.EventType
	logPtp4lConfigName   string
	skipCleanupMetrics   bool
	skipSetLastSyncState bool
}

func (tc *TestCase) String() string {
	b := strings.Builder{}
	b.WriteString("log: \"" + tc.log + "\"\n")
	b.WriteString("from: " + tc.from + "\n")
	b.WriteString("process: " + tc.process + "\n")
	b.WriteString("node: " + tc.node + "\n")
	b.WriteString("iface: " + tc.iface + "\n")
	b.WriteString("lastSyncState: " + string(tc.lastSyncState) + "\n")
	return b.String()
}

func (tc *TestCase) cleanupMetrics() {
	metrics.PtpOffset.With(map[string]string{"from": tc.from, "process": tc.process, "node": tc.node, "iface": tc.iface}).Set(CLEANUP)
	metrics.PtpMaxOffset.With(map[string]string{"from": tc.from, "process": tc.process, "node": tc.node, "iface": tc.iface}).Set(CLEANUP)
	metrics.PtpFrequencyAdjustment.With(map[string]string{"from": tc.from, "process": tc.process, "node": tc.node, "iface": tc.iface}).Set(CLEANUP)
	metrics.PtpDelay.With(map[string]string{"from": tc.from, "process": tc.process, "node": tc.node, "iface": tc.iface}).Set(CLEANUP)
	metrics.SyncState.With(map[string]string{"process": tc.process, "node": tc.node, "iface": tc.iface}).Set(CLEANUP)
	metrics.NmeaStatus.With(map[string]string{"process": tc.process, "node": tc.node, "iface": tc.iface}).Set(CLEANUP)
	metrics.ClockClassMetrics.With(map[string]string{"process": tc.process, "config": "ptp4l.0.config", "node": tc.node}).Set(CLEANUP)
	metrics.InterfaceRole.With(map[string]string{"process": tc.process, "node": tc.node, "iface": tc.iface}).Set(CLEANUP)
	ptpEventManager.ResetMockEvent()
}

func setLastSyncState(iface string, state ptp.SyncState, ptp4lconfName string) {
	if iface != metrics.ClockRealTime {
		iface = "master"
	}
	s := ptpEventManager.GetStatsForInterface(types.ConfigName(ptp4lconfName), types.IFace(iface))
	s.SetLastSyncState(state)
}

func statsAddValue(iface string, val int64, ptp4lconfName string) {
	if iface != metrics.ClockRealTime {
		iface = "master"
	}
	s := ptpEventManager.GetStatsForInterface(types.ConfigName(ptp4lconfName), types.IFace(iface))
	s.AddValue(val)
}

var testCases = []TestCase{
	{
		log:                    "ptp4l[4270544.036]: [ptp4l.1.config:5] port 2 (ens3f1): UNCALIBRATED to SLAVE on MASTER_CLOCK_SELECTED",
		from:                   "master",
		process:                "ptp4l",
		iface:                  "ens3f1",
		expectedRoleCheck:      true,
		expectedRole:           types.SLAVE,
		expectedSyncStateCheck: true,
		expectedSyncState:      float64(types.FREERUN),
		expectedEvent:          []ptp.EventType{}, // no event until next sync state
		logPtp4lConfigName:     logPtp4lConfigDualFollower.Name,
		skipCleanupMetrics:     true,
	},
	{
		log:                    "ptp4l[4270543.688]: [ptp4l.1.config:5] port 2 (ens3f1): SLAVE to FAULTY on FAULT_DETECTED (FT_UNSPECIFIED)", // Will fail if run independently
		from:                   "master",
		process:                "ptp4l",
		iface:                  "ens3fx",
		logPtp4lConfigName:     logPtp4lConfigDualFollower.Name,
		expectedSyncStateCheck: true,
		expectedSyncState:      float64(types.HOLDOVER),
		expectedEvent:          []ptp.EventType{ptp.PtpStateChange, ptp.SyncStateChange},
		skipCleanupMetrics:     true,
		skipSetLastSyncState:   true,
	},
	{
		log:                    "ptp4l[4270543.688]: [ptp4l.1.config:5] port 1 (ens3f0): SLAVE to FAULTY on FAULT_DETECTED (FT_UNSPECIFIED)", // Will fail if run independently
		from:                   "master",
		process:                "ptp4l",
		iface:                  "ens3f0",
		lastSyncState:          ptp.FREERUN,
		expectedRoleCheck:      true,
		expectedRole:           types.FAULTY,
		logPtp4lConfigName:     logPtp4lConfigDualFollower.Name,
		expectedSyncStateCheck: true,
		expectedSyncState:      float64(types.FREERUN),
		expectedEvent:          []ptp.EventType{ptp.PtpStateChange, ptp.SyncStateChange},
		skipCleanupMetrics:     true,
	},

	{
		log:                    "dpll[1000000100]:[ts2phc.0.config] ens7f0 frequency_status 3 offset 5 phase_status 3 pps_status 1 s2",
		from:                   "master",
		process:                "dpll",
		iface:                  "ens7fx",
		lastSyncState:          ptp.FREERUN,
		expectedSyncStateCheck: true,
		expectedSyncState:      float64(types.LOCKED),
		expectedPpsStatusCheck: true,
		expectedEvent:          []ptp.EventType{},
		expectedPpsStatus:      1,
		logPtp4lConfigName:     logPtp4lConfig.Name,
	},
	{
		log:                    "dpll[1000000110]:[ts2phc.0.config] ens7f0 frequency_status 3 offset 5 phase_status 3 pps_status 0 s0",
		from:                   "master",
		process:                "dpll",
		iface:                  "ens7fx",
		lastSyncState:          ptp.LOCKED,
		expectedSyncStateCheck: true,
		expectedSyncState:      float64(types.FREERUN),
		expectedPpsStatusCheck: true,
		expectedPpsStatus:      0,
		expectedEvent:          []ptp.EventType{},
		logPtp4lConfigName:     logPtp4lConfig.Name,
	},
	{
		log:                    "dpll[1000000120]:[ts2phc.0.config] ens7f0 frequency_status 3 offset 7 phase_status 3 pps_status 0 s1",
		from:                   "master",
		process:                "dpll",
		iface:                  "ens7fx",
		expectedSyncStateCheck: true,
		expectedSyncState:      float64(types.HOLDOVER),
		expectedPpsStatusCheck: true,
		expectedEvent:          []ptp.EventType{},
		expectedPpsStatus:      0,
		logPtp4lConfigName:     logPtp4lConfig.Name,
	},
	{
		log:                     "ts2phc[1000000200]:[ts2phc.0.config] ens2f0 nmea_status 0 offset 999999 s0",
		from:                    "master",
		process:                 "ts2phc",
		iface:                   "ens2fx",
		lastSyncState:           ptp.LOCKED,
		expectedNmeaStatusCheck: true,
		expectedNmeaStatus:      0,
		expectedEvent:           []ptp.EventType{},
		logPtp4lConfigName:      logPtp4lConfig.Name,
	},
	{
		log:                     "ts2phc[1000000210]:[ts2phc.0.config] ens2f0 nmea_status 1 offset 0 s2",
		from:                    "master",
		process:                 "ts2phc",
		iface:                   "ens2fx",
		expectedNmeaStatusCheck: true,
		expectedNmeaStatus:      1,
		expectedEvent:           []ptp.EventType{},
		logPtp4lConfigName:      logPtp4lConfig.Name,
	},
	{
		log:                    "ts2phc[1000000300]: [ts2phc.0.config] ens2f0 master offset  0 s2 freq -0",
		from:                   "master",
		process:                "ts2phc",
		iface:                  "ens2fx",
		expectedPtpOffsetCheck: true,
		expectedPtpOffset:      0,
		expectedEvent:          []ptp.EventType{ptp.PtpStateChange, ptp.SyncStateChange},
		logPtp4lConfigName:     logPtp4lConfig.Name,
	},
	{
		log:                    "ts2phc[1000000310]: [ts2phc.0.config] ens7f0 master offset 999 s0 freq      -0",
		from:                   "master",
		process:                "ts2phc",
		iface:                  "ens7fx",
		expectedPtpOffsetCheck: true,
		expectedPtpOffset:      999,
		expectedSyncStateCheck: true,
		expectedSyncState:      float64(types.FREERUN),
		expectedEvent:          []ptp.EventType{ptp.PtpStateChange, ptp.SyncStateChange},
		logPtp4lConfigName:     logPtp4lConfig.Name,
	},
	{
		log:                    "GM[1000000400]:[ts2phc.0.config] ens2f0 T-GM-STATUS s0",
		from:                   "master",
		process:                "GM",
		iface:                  "ens2fx",
		expectedSyncStateCheck: true,
		expectedSyncState:      float64(types.FREERUN),
		expectedEvent:          []ptp.EventType{ptp.PtpStateChange, ptp.SyncStateChange},
		logPtp4lConfigName:     logPtp4lConfig.Name,
	},
	{
		log:                    "gnss[1000000500]:[ts2phc.0.config] ens2f1 gnss_status 3 offset 5 s2",
		from:                   "gnss",
		process:                "gnss",
		iface:                  "ens2fx",
		lastSyncState:          ptp.FREERUN,
		expectedPtpOffsetCheck: true,
		expectedPtpOffset:      5,
		expectedSyncStateCheck: true,
		expectedSyncState:      float64(types.LOCKED),
		expectedEvent:          []ptp.EventType{ptp.GnssStateChange},
		logPtp4lConfigName:     logPtp4lConfig.Name,
	},
	{
		log:                            "ptp4l[1000000600]:[ptp4l.0.config] CLOCK_CLASS_CHANGE 248.000000",
		process:                        "ptp4l",
		iface:                          "master",
		lastSyncState:                  ptp.FREERUN,
		expectedClockClassMetricsCheck: true,
		expectedClockClassMetrics:      248,
		expectedEvent:                  []ptp.EventType{ptp.PtpClockClassChange},
		logPtp4lConfigName:             logPtp4lConfig.Name,
	},
	{
		log:                            "ptp4l[1000000610]:[ptp4l.0.config] CLOCK_CLASS_CHANGE 6.000000",
		process:                        "ptp4l",
		iface:                          "master",
		lastSyncState:                  ptp.FREERUN,
		expectedClockClassMetricsCheck: true,
		expectedClockClassMetrics:      6,
		expectedEvent:                  []ptp.EventType{ptp.PtpClockClassChange},
		logPtp4lConfigName:             logPtp4lConfig.Name,
	},
	{
		log:                                 "phc2sys[1000000700]: [ptp4l.0.config] CLOCK_REALTIME phc offset       -62 s0 freq  -78368 delay   1100",
		from:                                "phc",
		process:                             "phc2sys",
		iface:                               metrics.ClockRealTime,
		expectedPtpOffsetCheck:              true,
		expectedPtpOffset:                   -62,
		expectedPtpFrequencyAdjustmentCheck: true,
		expectedPtpFrequencyAdjustment:      -78368,
		expectedPtpDelayCheck:               true,
		expectedPtpDelay:                    1100,
		expectedSyncStateCheck:              true,
		expectedSyncState:                   float64(types.FREERUN),
		expectedEvent:                       []ptp.EventType{ptp.OsClockSyncStateChange, ptp.SyncStateChange},
		logPtp4lConfigName:                  logPtp4lConfig.Name,
	},
	{
		log:                                 "phc2sys[1000000710]: [ptp4l.0.config] CLOCK_REALTIME phc offset       -62 s0 freq  -78368 delay   1100",
		from:                                "phc",
		process:                             "phc2sys",
		iface:                               metrics.ClockRealTime,
		lastSyncState:                       ptp.LOCKED,
		expectedPtpOffsetCheck:              true,
		expectedPtpOffset:                   -62,
		expectedPtpFrequencyAdjustmentCheck: true,
		expectedPtpFrequencyAdjustment:      -78368,
		expectedPtpDelayCheck:               true,
		expectedPtpDelay:                    1100,
		expectedSyncStateCheck:              true,
		expectedSyncState:                   float64(types.FREERUN),
		expectedEvent:                       []ptp.EventType{ptp.OsClockSyncStateChange, ptp.SyncStateChange},
		logPtp4lConfigName:                  logPtp4lConfig.Name,
	},
	// chronyd tests - similar to phc2sys but focused on sync state changes
	{
		log:                    "chronyd[1000000800]: [chronyd.0.config] Selected source 192.168.1.1",
		from:                   "chronyd",
		process:                "chronyd",
		iface:                  metrics.ClockRealTime,
		lastSyncState:          ptp.FREERUN,
		expectedSyncStateCheck: true,
		expectedSyncState:      float64(types.LOCKED),
		expectedEvent:          []ptp.EventType{ptp.OsClockSyncStateChange, ptp.SyncStateChange},
		logPtp4lConfigName:     "chronyd.0.config",
	},
	{
		log:                    "chronyd[1000000810]: [chronyd.0.config] Selected source 192.168.1.1",
		from:                   "chronyd",
		process:                "chronyd",
		iface:                  metrics.ClockRealTime,
		lastSyncState:          ptp.LOCKED,
		expectedSyncStateCheck: true,
		expectedSyncState:      float64(types.LOCKED),
		expectedEvent:          []ptp.EventType{},
		logPtp4lConfigName:     "chronyd.0.config",
	},
	{
		log:                    "chronyd[1000000820]: [chronyd.0.config] no selectable sources",
		from:                   "chronyd",
		process:                "chronyd",
		iface:                  metrics.ClockRealTime,
		lastSyncState:          ptp.LOCKED,
		expectedSyncStateCheck: true,
		expectedSyncState:      float64(types.FREERUN),
		expectedEvent:          []ptp.EventType{ptp.OsClockSyncStateChange, ptp.SyncStateChange},
		logPtp4lConfigName:     "chronyd.0.config",
	},
	{
		log:                    "chronyd[1000000830]: [chronyd.0.config] no selectable sources",
		from:                   "chronyd",
		process:                "chronyd",
		iface:                  metrics.ClockRealTime,
		lastSyncState:          ptp.FREERUN,
		expectedSyncStateCheck: true,
		expectedSyncState:      float64(types.FREERUN),
		expectedEvent:          []ptp.EventType{},
		logPtp4lConfigName:     "chronyd.0.config",
	},
}

func setup() {
	mockFS := &metrics.MockFileSystem{}
	scConfig = &common.SCConfiguration{StorePath: "/tmp/store"}
	metrics.Filesystem = mockFS
	ptpEventManager = metrics.NewPTPEventManager(resourcePrefix, InitPubSubTypes(), "tetsnode", scConfig)
	ptpEventManager.MockTest(true)

	ptpEventManager.AddPTPConfig(types.ConfigName(logPtp4lConfig.Name), logPtp4lConfig)

	statsMaster := stats.NewStats(logPtp4lConfig.Name)
	statsMaster.SetOffsetSource("master")
	statsMaster.SetProcessName("ts2phc")
	statsMaster.SetAlias("ens2fx")

	statsSlave := stats.NewStats(logPtp4lConfig.Name)
	statsSlave.SetOffsetSource("phc")
	statsSlave.SetProcessName("phc2sys")
	statsSlave.SetLastSyncState("LOCKED")
	statsSlave.SetClockClass(0)

	// DualFollower
	ptpEventManager.AddPTPConfig(types.ConfigName(logPtp4lConfigDualFollower.Name), logPtp4lConfigDualFollower)
	statsPHCDualFollower := stats.NewStats(logPtp4lConfigDualFollower.Name)
	statsPHCDualFollower.SetOffsetSource("master")
	statsPHCDualFollower.SetProcessName("ptp4l")
	statsPHCDualFollower.SetLastSyncState("LOCKED")
	statsPHCDualFollower.SetAlias("ens3fx")

	statsRTDualFollower := stats.NewStats(logPtp4lConfigDualFollower.Name)
	statsRTDualFollower.SetOffsetSource("phc")
	statsRTDualFollower.SetProcessName("phc2sys")
	statsRTDualFollower.SetLastSyncState("LOCKED")
	statsRTDualFollower.SetClockClass(0)

	ptpEventManager.Stats[types.ConfigName(logPtp4lConfig.Name)] = make(stats.PTPStats)
	ptpEventManager.Stats[types.ConfigName(logPtp4lConfig.Name)][types.IFace("master")] = statsMaster
	ptpEventManager.Stats[types.ConfigName(logPtp4lConfig.Name)][types.IFace("CLOCK_REALTIME")] = statsSlave
	ptpEventManager.Stats[types.ConfigName(logPtp4lConfig.Name)][types.IFace("ens2f0")] = statsMaster
	ptpEventManager.Stats[types.ConfigName(logPtp4lConfig.Name)][types.IFace("ens7f0")] = statsSlave

	// DualFollower
	ptpEventManager.Stats[types.ConfigName(logPtp4lConfigDualFollower.Name)] = make(stats.PTPStats)
	ptpEventManager.Stats[types.ConfigName(logPtp4lConfigDualFollower.Name)][types.IFace("master")] = statsPHCDualFollower
	ptpEventManager.Stats[types.ConfigName(logPtp4lConfigDualFollower.Name)][types.IFace("CLOCK_REALTIME")] = statsRTDualFollower
	ptpEventManager.Stats[types.ConfigName(logPtp4lConfigDualFollower.Name)][types.IFace("ens3f0")] = statsPHCDualFollower
	ptpEventManager.Stats[types.ConfigName(logPtp4lConfigDualFollower.Name)][types.IFace("ens3f1")] = statsPHCDualFollower

	// chronyd configuration
	chronydConfig := &ptp4lconf.PTP4lConfig{
		Name:    "chronyd.0.config",
		Profile: "chronyd",
	}
	ptpEventManager.AddPTPConfig(types.ConfigName(chronydConfig.Name), chronydConfig)
	statsChronyd := stats.NewStats(chronydConfig.Name)
	statsChronyd.SetOffsetSource("chronyd")
	statsChronyd.SetProcessName("chronyd")
	statsChronyd.SetLastSyncState("LOCKED")
	ptpEventManager.Stats[types.ConfigName(chronydConfig.Name)] = make(stats.PTPStats)
	ptpEventManager.Stats[types.ConfigName(chronydConfig.Name)][types.IFace("CLOCK_REALTIME")] = statsChronyd

	ptpEventManager.PtpConfigMapUpdates = config.NewLinuxPTPConfUpdate()

	metrics.RegisterMetrics("mynode")
}

func teardown() {
}

func TestMain(m *testing.M) {
	setup()
	code := m.Run()
	teardown()
	os.Exit(code)
}

// this test works for v2 only , there is no sync state event for v1
func Test_ExtractMetrics(t *testing.T) {
	for _, tc := range testCases {
		tc := tc
		tc.node = MYNODE
		t.Run(tc.log, func(t *testing.T) {
			assert := assert.New(t)
			if !tc.skipCleanupMetrics {
				tc.cleanupMetrics()
			}
			if !tc.skipSetLastSyncState {
				setLastSyncState(tc.iface, tc.lastSyncState, tc.logPtp4lConfigName)
			}
			ptpEventManager.ResetMockEvent()
			ptpEventManager.ExtractMetrics(tc.log)

			if tc.expectedRoleCheck {
				role := metrics.InterfaceRole.With(map[string]string{"process": tc.process, "node": tc.node, "iface": tc.iface})
				statsAddValue(tc.iface, int64(testutil.ToFloat64(role)), tc.logPtp4lConfigName)
				value := types.PtpPortRole(testutil.ToFloat64(role))
				assert.Equal(tc.expectedRole, value, "ptp role does not match\n%s", tc.String())
			}

			if tc.expectedPtpOffsetCheck {
				ptpOffset := metrics.PtpOffset.With(map[string]string{"from": tc.from, "process": tc.process, "node": tc.node, "iface": tc.iface})
				statsAddValue(tc.iface, int64(testutil.ToFloat64(ptpOffset)), tc.logPtp4lConfigName)
				assert.Equal(tc.expectedPtpOffset, testutil.ToFloat64(ptpOffset), "PtpOffset does not match\n%s", tc.String())
			}
			if tc.expectedPtpMaxOffsetCheck {
				ptpMaxOffset := metrics.PtpMaxOffset.With(map[string]string{"from": tc.from, "process": tc.process, "node": tc.node, "iface": tc.iface})
				assert.Equal(tc.expectedPtpMaxOffset, testutil.ToFloat64(ptpMaxOffset), "PtpMaxOffset does not match\n%s", tc.String())
			}
			if tc.expectedPtpFrequencyAdjustmentCheck {
				ptpFrequencyAdjustment := metrics.PtpFrequencyAdjustment.With(map[string]string{"from": tc.from, "process": tc.process, "node": tc.node, "iface": tc.iface})
				assert.Equal(tc.expectedPtpFrequencyAdjustment, testutil.ToFloat64(ptpFrequencyAdjustment), "PtpFrequencyAdjustment does not match\n%s", tc.String())
			}
			if tc.expectedPtpDelayCheck {
				ptpDelay := metrics.PtpDelay.With(map[string]string{"from": tc.from, "process": tc.process, "node": tc.node, "iface": tc.iface})
				assert.Equal(tc.expectedPtpDelay, testutil.ToFloat64(ptpDelay), "PtpDelay does not match\n%s", tc.String())
			}
			if tc.expectedSyncStateCheck {
				clockState := metrics.SyncState.With(map[string]string{"process": tc.process, "node": tc.node, "iface": tc.iface})

				cs := testutil.ToFloat64(clockState)
				assert.Equal(tc.expectedSyncState, cs, "SyncState does not match\n%s", tc.String())
			}
			if tc.expectedNmeaStatusCheck {
				nmeaStatus := metrics.NmeaStatus.With(map[string]string{"process": tc.process, "node": tc.node, "iface": tc.iface})
				assert.Equal(tc.expectedNmeaStatus, testutil.ToFloat64(nmeaStatus), "NmeaStatus does not match\n%s", tc.String())
			}
			if tc.expectedClockClassMetricsCheck {
				clockClassMetrics := metrics.ClockClassMetrics.With(map[string]string{"process": tc.process, "config": "ptp4l.0.config", "node": tc.node})
				assert.Equal(tc.expectedClockClassMetrics, testutil.ToFloat64(clockClassMetrics), "ClockClassMetrics does not match\n%s", tc.String())
			}
			assert.Equal(tc.expectedEvent, ptpEventManager.GetMockEvent(), "Expected Event does not match\n%s", tc.String())
		})
	}
}
