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
	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/alias"
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
	alias.SetAlias("ens2f0", "ens2fx")
	alias.SetAlias("ens2f1", "ens2fx")
	alias.SetAlias("ens7f0", "ens7fx")
	alias.SetAlias("ens3f0", "ens3fx")
	alias.SetAlias("ens3f1", "ens3fx")

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

	// OCPBUGS-85092 regression tests: prove that stale replay data (port role
	// transition arriving after live offset) corrupts metrics and they do NOT
	// self-recover. This documents WHY the live gate is essential — without it,
	// a replayed FAULTY port role permanently poisons the clock state.
	t.Run("ReplayThenLive_ClockStateCorruption", func(t *testing.T) {
		assert := assert.New(t)
		const lockedLine = "ptp4l[5000000.100]: [ptp4l.1.config] master offset 5 s2 freq -1000 path delay 100"
		const stalePortRole = "ptp4l[4999999.000]: [ptp4l.1.config:4] port 1 (ens3f0): SLAVE to FAULTY on FAULT_DETECTED (FT_UNSPECIFIED)"
		labels := map[string]string{"process": "ptp4l", "node": MYNODE, "iface": "ens3fx"}

		// Reset shared config state from previous subtests
		logPtp4lConfigDualFollower.Interfaces[0].UpdateRole(types.SLAVE)
		s := ptpEventManager.GetStatsForInterface(types.ConfigName(logPtp4lConfigDualFollower.Name), types.IFace("master"))
		s.SetRole(types.SLAVE)

		// Step 1: Establish LOCKED state via live offset s2
		metrics.SyncState.With(labels).Set(CLEANUP)
		setLastSyncState("master", ptp.LOCKED, logPtp4lConfigDualFollower.Name)
		ptpEventManager.ResetMockEvent()
		ptpEventManager.ExtractMetrics(lockedLine)
		assert.Equal(float64(types.LOCKED), testutil.ToFloat64(metrics.SyncState.With(labels)),
			"step 1: live offset s2 should set SyncState to LOCKED")

		// Step 2: Stale replay port role (SLAVE→FAULTY) corrupts the state
		ptpEventManager.ResetMockEvent()
		ptpEventManager.ExtractMetrics(stalePortRole)
		assert.Equal(float64(types.HOLDOVER), testutil.ToFloat64(metrics.SyncState.With(labels)),
			"step 2: stale SLAVE→FAULTY should corrupt SyncState to HOLDOVER")

		// Step 3: Another live s2 offset does NOT recover to LOCKED because
		// the port is now FAULTY — proving the corruption is permanent without
		// the live gate preventing stale replay from arriving after live data.
		ptpEventManager.ResetMockEvent()
		ptpEventManager.ExtractMetrics(lockedLine)
		assert.Equal(float64(types.HOLDOVER), testutil.ToFloat64(metrics.SyncState.With(labels)),
			"step 3: HOLDOVER persists despite s2 offset — the bug: stale replay permanently poisons clock state")
	})

	t.Run("ReplayThenLive_PortRoleCorruption", func(t *testing.T) {
		assert := assert.New(t)
		const liveSlaveRole = "ptp4l[5000001.000]: [ptp4l.1.config:4] port 1 (ens3f0): UNCALIBRATED to SLAVE on MASTER_CLOCK_SELECTED"
		const staleFaultyRole = "ptp4l[4999998.000]: [ptp4l.1.config:4] port 1 (ens3f0): SLAVE to FAULTY on FAULT_DETECTED (FT_UNSPECIFIED)"
		roleLabels := map[string]string{"process": "ptp4l", "node": MYNODE, "iface": "ens3f0"}

		// Reset: set to LISTENING so UNCALIBRATED→SLAVE triggers a role change
		logPtp4lConfigDualFollower.Interfaces[0].UpdateRole(types.LISTENING)

		// Step 1: Live port role → SLAVE
		metrics.InterfaceRole.With(roleLabels).Set(CLEANUP)
		ptpEventManager.ResetMockEvent()
		ptpEventManager.ExtractMetrics(liveSlaveRole)
		assert.Equal(float64(types.SLAVE), testutil.ToFloat64(metrics.InterfaceRole.With(roleLabels)),
			"step 1: port role should be SLAVE")

		// Step 2: Stale replay port role → FAULTY (corruption)
		ptpEventManager.ResetMockEvent()
		ptpEventManager.ExtractMetrics(staleFaultyRole)
		assert.Equal(float64(types.FAULTY), testutil.ToFloat64(metrics.InterfaceRole.With(roleLabels)),
			"step 2: stale replay SLAVE→FAULTY corrupts port role to FAULTY")

		// Port roles only change on state-transition events — there is no
		// continuous refresh like offset lines. The FAULTY role persists
		// indefinitely until a real port state transition occurs. This is
		// why the live gate is critical: it prevents this scenario entirely.
	})

	// With the live gate fix: replay arrives FIRST, then live data overwrites it.
	// This is the correct ordering enforced by the gate, proving metrics end up
	// in the correct LOCKED/SLAVE state.
	t.Run("WithLiveGate_ReplayThenLive_MetricsCorrect", func(t *testing.T) {
		assert := assert.New(t)
		syncLabels := map[string]string{"process": "ptp4l", "node": MYNODE, "iface": "ens3fx"}
		roleLabels := map[string]string{"process": "ptp4l", "node": MYNODE, "iface": "ens3f0"}

		// Reset shared config state from previous subtests
		logPtp4lConfigDualFollower.Interfaces[0].UpdateRole(types.SLAVE)
		metrics.SyncState.With(syncLabels).Set(CLEANUP)
		metrics.InterfaceRole.With(roleLabels).Set(CLEANUP)

		// --- Replay phase (arrives first, gate blocks live data) ---

		// Replay: stale FAULTY port role from before recovery
		setLastSyncState("master", ptp.LOCKED, logPtp4lConfigDualFollower.Name)
		ptpEventManager.ResetMockEvent()
		ptpEventManager.ExtractMetrics(
			"ptp4l[0.000]: [ptp4l.1.config:4] port 1 (ens3f0): SLAVE to FAULTY on FAULT_DETECTED (FT_UNSPECIFIED)")

		// After replay, port is FAULTY and clock is HOLDOVER — stale state
		assert.Equal(float64(types.FAULTY), testutil.ToFloat64(metrics.InterfaceRole.With(roleLabels)),
			"after replay: port role is stale FAULTY")
		assert.Equal(float64(types.HOLDOVER), testutil.ToFloat64(metrics.SyncState.With(syncLabels)),
			"after replay: clock state is HOLDOVER")

		// --- CMD LIVE_START (gate opens, live data follows) ---
		// In real CEP, CMD LIVE_START is just skipped (continue) — no metric effect.

		// --- Live phase: port recovers through standard ptp4l sequence ---
		// FAULTY → LISTENING → UNCALIBRATED → SLAVE, then LOCKED offset

		ptpEventManager.ResetMockEvent()
		ptpEventManager.ExtractMetrics(
			"ptp4l[10.000]: [ptp4l.1.config:4] port 1 (ens3f0): FAULTY to LISTENING on INIT_COMPLETE")

		ptpEventManager.ResetMockEvent()
		ptpEventManager.ExtractMetrics(
			"ptp4l[10.100]: [ptp4l.1.config:4] port 1 (ens3f0): LISTENING to UNCALIBRATED on RS_SLAVE")

		ptpEventManager.ResetMockEvent()
		ptpEventManager.ExtractMetrics(
			"ptp4l[10.200]: [ptp4l.1.config:4] port 1 (ens3f0): UNCALIBRATED to SLAVE on MASTER_CLOCK_SELECTED")
		assert.Equal(float64(types.SLAVE), testutil.ToFloat64(metrics.InterfaceRole.With(roleLabels)),
			"after live port recovery: port role is SLAVE")

		// Live: first offset after SLAVE recovery triggers LOCKED
		ptpEventManager.ResetMockEvent()
		ptpEventManager.ExtractMetrics(
			"ptp4l[11.000]: [ptp4l.1.config] master offset 3 s2 freq -998 path delay 99")
		assert.Equal(float64(types.LOCKED), testutil.ToFloat64(metrics.SyncState.With(syncLabels)),
			"after live offset s2: SyncState is LOCKED (correct final state)")
	})

	// lastOverallGMState (see ParseGMLogs): when master offset source is ts2phc, ExtractMetrics
	// forces non-ts2phc downstream sync to FREERUN only if last GM snapshot was FREERUN.
	// The E1 check (worst_of(phc2sys_state, E1_state)) also fires for T-GM profiles
	// via the "GM" key in ptpStats, providing HOLDOVER handling per O-RAN Table 37.
	t.Run("lastOverallGMState_holdover_vs_freerun", func(t *testing.T) {
		const logPhcLocked = "phc2sys[1000000900]: [ptp4l.0.config] CLOCK_REALTIME phc offset       -62 s2 freq  -78368 delay   1100"
		assert := assert.New(t)
		t.Cleanup(func() {
			ptpEventManager.SetLastOverallGMStateForTesting("")
		})

		t.Run("FREERUN_GM_forces_ClockRealTime_even_when_log_reports_s2", func(t *testing.T) {
			metrics.SyncState.With(map[string]string{"process": "phc2sys", "node": MYNODE, "iface": metrics.ClockRealTime}).Set(CLEANUP)
			setLastSyncState(metrics.ClockRealTime, ptp.FREERUN, logPtp4lConfig.Name)
			ptpEventManager.SetLastOverallGMStateForTesting(ptp.FREERUN)
			ptpEventManager.ResetMockEvent()
			ptpEventManager.ExtractMetrics(logPhcLocked)

			ss := metrics.SyncState.With(map[string]string{"process": "phc2sys", "node": MYNODE, "iface": metrics.ClockRealTime})
			assert.Equal(float64(types.FREERUN), testutil.ToFloat64(ss),
				"when last GM state is FREERUN, downstream phc2sys sync state must be forced to FREERUN")
		})

		t.Run("HOLDOVER_GM_gives_E3_HOLDOVER_per_ORAN_Table37", func(t *testing.T) {
			metrics.SyncState.With(map[string]string{"process": "phc2sys", "node": MYNODE, "iface": metrics.ClockRealTime}).Set(CLEANUP)
			setLastSyncState(metrics.ClockRealTime, ptp.FREERUN, logPtp4lConfig.Name)
			ptpEventManager.SetLastOverallGMStateForTesting(ptp.HOLDOVER)

			gmStats := ptpEventManager.GetStatsForInterface(types.ConfigName(logPtp4lConfig.Name), types.IFace(stats.GMMainClockName))
			gmStats.SetLastSyncState(ptp.HOLDOVER)

			ptpEventManager.ResetMockEvent()
			ptpEventManager.ExtractMetrics(logPhcLocked)

			ss := metrics.SyncState.With(map[string]string{"process": "phc2sys", "node": MYNODE, "iface": metrics.ClockRealTime})
			assert.Equal(float64(types.HOLDOVER), testutil.ToFloat64(ss),
				"when GM E1 is HOLDOVER, E3 must be HOLDOVER per O-RAN Table 37 row 2")
		})
	})
	// ts2phc reports s2 (LOCKED) but offset exceeds threshold → must be FREERUN.
	t.Run("ts2phc_high_offset_s2_forces_FREERUN", func(t *testing.T) {
		assert := assert.New(t)

		// Enable GM process on master stats so ts2phc enters the threshold-check branch
		setLastSyncState("ens2fx", ptp.FREERUN, logPtp4lConfig.Name)
		metrics.SyncState.With(map[string]string{"process": "GM", "node": MYNODE, "iface": "ens2fx"}).Set(CLEANUP)
		ptpEventManager.ResetMockEvent()
		ptpEventManager.ExtractMetrics("GM[2000000100]:[ts2phc.0.config] ens2f0 T-GM-STATUS s2")

		// ts2phc reports s2 but offset 5000 exceeds default threshold (100/-100)
		labels := map[string]string{"process": "ts2phc", "node": MYNODE, "iface": "ens2fx"}
		metrics.SyncState.With(labels).Set(CLEANUP)
		ptpEventManager.ResetMockEvent()
		ptpEventManager.ExtractMetrics("ts2phc[2000000200]: [ts2phc.0.config] ens2f0 master offset 5000 s2 freq -0")

		assert.Equal(float64(types.FREERUN), testutil.ToFloat64(metrics.SyncState.With(labels)),
			"ts2phc s2 with offset exceeding threshold must report FREERUN")
	})
}
