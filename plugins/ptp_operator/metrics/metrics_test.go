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

//go:build unittests
// +build unittests

package metrics_test

import (
	"os"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
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
	s0     = 0.0
	s1     = 2.0
	s2     = 1.0
	MYNODE = "mynode"
	// SKIP skip the verification of the metric
	SKIP    = 101010101
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

var ptpEventManager *metrics.PTPEventManager
var scConfig *common.SCConfiguration
var resourcePrefix = ""
var registry *prometheus.Registry
var ptpStats *stats.Stats

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
	log                            string
	from                           string
	process                        string
	node                           string
	iface                          string
	lastSyncState                  ptp.SyncState
	expectedPtpOffset              float64 // offset_ns
	expectedPtpMaxOffset           float64 // max_offset_ns
	expectedPtpFrequencyAdjustment float64 // frequency_adjustment_ns
	expectedPtpDelay               float64 // delay_ns
	expectedSyncState              float64 // clock_state
	expectedNmeaStatus             float64 // nmea_status
	expectedPpsStatus              float64 // pps_status
	expectedClockClassMetrics      float64 // clock_class
	expectedEvent                  ptp.EventType
}

func (tc *TestCase) init() {
	tc.expectedPtpOffset = SKIP
	tc.expectedPtpMaxOffset = SKIP
	tc.expectedPtpFrequencyAdjustment = SKIP
	tc.expectedPtpDelay = SKIP
	tc.expectedSyncState = SKIP
	tc.expectedNmeaStatus = SKIP
	tc.expectedPpsStatus = SKIP
	tc.expectedClockClassMetrics = SKIP
	tc.expectedEvent = ""
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
	metrics.ClockClassMetrics.With(map[string]string{"process": tc.process, "node": tc.node}).Set(CLEANUP)
	ptpEventManager.ResetMockEvent()
}

func setLastSyncState(iface string, state ptp.SyncState) {
	if iface != metrics.ClockRealTime {
		iface = "master"
	}
	s := ptpEventManager.GetStatsForInterface(types.ConfigName(logPtp4lConfig.Name), types.IFace(iface))
	s.SetLastSyncState(state)
}

func statsAddValue(iface string, val int64) {
	if iface != metrics.ClockRealTime {
		iface = "master"
	}
	s := ptpEventManager.GetStatsForInterface(types.ConfigName(logPtp4lConfig.Name), types.IFace(iface))
	s.AddValue(val)
}

var testCases = []TestCase{
	{
		log:                            "dpll[1000000100]:[ts2phc.0.config] ens7f0 frequency_status 3 offset 5 phase_status 3 pps_status 1 s2",
		from:                           "master",
		process:                        "dpll",
		iface:                          "ens7fx",
		lastSyncState:                  ptp.FREERUN,
		expectedPtpOffset:              SKIP,
		expectedPtpMaxOffset:           SKIP,
		expectedPtpFrequencyAdjustment: SKIP,
		expectedPtpDelay:               SKIP,
		expectedSyncState:              s2,
		expectedNmeaStatus:             SKIP,
		expectedPpsStatus:              1,
		expectedClockClassMetrics:      SKIP,
	},
	{
		log:                            "dpll[1000000110]:[ts2phc.0.config] ens7f0 frequency_status 3 offset 5 phase_status 3 pps_status 0 s0",
		from:                           "master",
		process:                        "dpll",
		iface:                          "ens7fx",
		lastSyncState:                  ptp.LOCKED,
		expectedPtpOffset:              SKIP,
		expectedPtpMaxOffset:           SKIP,
		expectedPtpFrequencyAdjustment: SKIP,
		expectedPtpDelay:               SKIP,
		expectedSyncState:              s0,
		expectedNmeaStatus:             SKIP,
		expectedPpsStatus:              0,
		expectedEvent:                  "",
		expectedClockClassMetrics:      SKIP,
	},
	{
		log:                            "dpll[1000000120]:[ts2phc.0.config] ens7f0 frequency_status 3 offset 7 phase_status 3 pps_status 0 s1",
		from:                           "master",
		process:                        "dpll",
		iface:                          "ens7fx",
		expectedPtpOffset:              SKIP,
		expectedPtpMaxOffset:           SKIP,
		expectedPtpFrequencyAdjustment: SKIP,
		expectedPtpDelay:               SKIP,
		expectedSyncState:              s1,
		expectedNmeaStatus:             SKIP,
		expectedPpsStatus:              0,
		expectedClockClassMetrics:      SKIP,
	},
	{
		log:                            "ts2phc[1000000200]:[ts2phc.0.config] ens2f0 nmea_status 0 offset 999999 s0",
		from:                           "master",
		process:                        "ts2phc",
		iface:                          "ens2fx",
		lastSyncState:                  ptp.LOCKED,
		expectedPtpOffset:              SKIP,
		expectedPtpMaxOffset:           SKIP,
		expectedPtpFrequencyAdjustment: SKIP,
		expectedPtpDelay:               SKIP,
		expectedSyncState:              SKIP,
		expectedNmeaStatus:             0,
		expectedPpsStatus:              SKIP,
		expectedClockClassMetrics:      SKIP,
		expectedEvent:                  "",
	},
	{
		log:                            "ts2phc[1000000210]:[ts2phc.0.config] ens2f0 nmea_status 1 offset 0 s2",
		from:                           "master",
		process:                        "ts2phc",
		iface:                          "ens2fx",
		expectedPtpOffset:              SKIP,
		expectedPtpMaxOffset:           SKIP,
		expectedPtpFrequencyAdjustment: SKIP,
		expectedPtpDelay:               SKIP,
		expectedSyncState:              SKIP,
		expectedNmeaStatus:             1,
		expectedPpsStatus:              SKIP,
		expectedClockClassMetrics:      SKIP,
		expectedEvent:                  "",
	},
	{
		log:                            "ts2phc[1000000300]: [ts2phc.0.config] ens2f0 master offset  0 s2 freq -0",
		from:                           "master",
		process:                        "ts2phc",
		iface:                          "ens2fx",
		expectedPtpOffset:              0,
		expectedPtpMaxOffset:           SKIP,
		expectedPtpFrequencyAdjustment: SKIP,
		expectedPtpDelay:               SKIP,
		expectedSyncState:              SKIP,
		expectedNmeaStatus:             SKIP,
		expectedPpsStatus:              SKIP,
		expectedClockClassMetrics:      SKIP,
		expectedEvent:                  ptp.PtpStateChange,
	},
	{
		log:                            "ts2phc[1000000310]: [ts2phc.0.config] ens7f0 master offset 999 s0 freq      -0",
		from:                           "master",
		process:                        "ts2phc",
		iface:                          "ens7fx",
		expectedPtpOffset:              999,
		expectedPtpMaxOffset:           SKIP,
		expectedPtpFrequencyAdjustment: SKIP,
		expectedPtpDelay:               SKIP,
		expectedSyncState:              s0,
		expectedNmeaStatus:             SKIP,
		expectedPpsStatus:              SKIP,
		expectedClockClassMetrics:      SKIP,
		expectedEvent:                  ptp.PtpStateChange,
	},
	{
		log:                            "GM[1000000400]:[ts2phc.0.config] ens2f0 T-GM-STATUS s0",
		from:                           "master",
		process:                        "GM",
		iface:                          "ens2fx",
		expectedPtpOffset:              SKIP,
		expectedPtpMaxOffset:           SKIP,
		expectedPtpFrequencyAdjustment: SKIP,
		expectedPtpDelay:               SKIP,
		expectedSyncState:              s0,
		expectedNmeaStatus:             SKIP,
		expectedPpsStatus:              SKIP,
		expectedClockClassMetrics:      SKIP,
		expectedEvent:                  ptp.PtpStateChange,
	},
	{
		log:                            "gnss[1000000500]:[ts2phc.0.config] ens2f1 gnss_status 3 offset 5 s2",
		from:                           "gnss",
		process:                        "gnss",
		iface:                          "ens2fx",
		lastSyncState:                  ptp.FREERUN,
		expectedPtpOffset:              5,
		expectedPtpMaxOffset:           SKIP,
		expectedPtpFrequencyAdjustment: SKIP,
		expectedPtpDelay:               SKIP,
		expectedSyncState:              s2,
		expectedNmeaStatus:             SKIP,
		expectedPpsStatus:              SKIP,
		expectedClockClassMetrics:      SKIP,
		expectedEvent:                  ptp.GnssStateChange,
	},
	{
		log:                            "ptp4l[1000000600]:[ptp4l.0.config] CLOCK_CLASS_CHANGE 248.000000",
		process:                        "ptp4l",
		iface:                          "master",
		lastSyncState:                  ptp.FREERUN,
		expectedPtpOffset:              SKIP,
		expectedPtpMaxOffset:           SKIP,
		expectedPtpFrequencyAdjustment: SKIP,
		expectedPtpDelay:               SKIP,
		expectedSyncState:              SKIP,
		expectedNmeaStatus:             SKIP,
		expectedPpsStatus:              SKIP,
		expectedClockClassMetrics:      248,
		expectedEvent:                  ptp.PtpClockClassChange,
	},
	{
		log:                            "ptp4l[1000000610]:[ptp4l.0.config] CLOCK_CLASS_CHANGE 6.000000",
		process:                        "ptp4l",
		iface:                          "master",
		lastSyncState:                  ptp.FREERUN,
		expectedPtpOffset:              SKIP,
		expectedPtpMaxOffset:           SKIP,
		expectedPtpFrequencyAdjustment: SKIP,
		expectedPtpDelay:               SKIP,
		expectedSyncState:              SKIP,
		expectedNmeaStatus:             SKIP,
		expectedPpsStatus:              SKIP,
		expectedClockClassMetrics:      6,
		expectedEvent:                  ptp.PtpClockClassChange,
	},
	{
		log:                            "phc2sys[1000000700]: [ptp4l.0.config] CLOCK_REALTIME phc offset       -62 s0 freq  -78368 delay   1100",
		from:                           "phc",
		process:                        "phc2sys",
		iface:                          metrics.ClockRealTime,
		expectedPtpOffset:              -62,
		expectedPtpMaxOffset:           SKIP,
		expectedPtpFrequencyAdjustment: -78368,
		expectedPtpDelay:               1100,
		expectedSyncState:              s0,
		expectedNmeaStatus:             SKIP,
		expectedPpsStatus:              SKIP,
		expectedClockClassMetrics:      SKIP,
		expectedEvent:                  ptp.OsClockSyncStateChange,
	},
	{
		log:                            "phc2sys[1000000710]: [ptp4l.0.config] CLOCK_REALTIME phc offset       -62 s0 freq  -78368 delay   1100",
		from:                           "phc",
		process:                        "phc2sys",
		iface:                          metrics.ClockRealTime,
		lastSyncState:                  ptp.LOCKED,
		expectedPtpOffset:              -62,
		expectedPtpMaxOffset:           SKIP,
		expectedPtpFrequencyAdjustment: -78368,
		expectedPtpDelay:               1100,
		expectedSyncState:              s0,
		expectedNmeaStatus:             SKIP,
		expectedPpsStatus:              SKIP,
		expectedClockClassMetrics:      SKIP,
		expectedEvent:                  ptp.OsClockSyncStateChange,
	},
}

func setup() {
	ptpEventManager = metrics.NewPTPEventManager(resourcePrefix, InitPubSubTypes(), "tetsnode", scConfig)
	ptpEventManager.MockTest(true)

	ptpEventManager.AddPTPConfig(types.ConfigName(logPtp4lConfig.Name), logPtp4lConfig)

	stats_master := stats.NewStats(logPtp4lConfig.Name)
	stats_master.SetOffsetSource("master")
	stats_master.SetProcessName("ts2phc")
	stats_master.SetAlias("ens2fx")

	stats_slave := stats.NewStats(logPtp4lConfig.Name)
	stats_slave.SetOffsetSource("phc")
	stats_slave.SetProcessName("phc2sys")
	stats_slave.SetLastSyncState("LOCKED")
	stats_slave.SetClockClass(0)

	ptpEventManager.Stats[types.ConfigName(logPtp4lConfig.Name)] = make(stats.PTPStats)
	ptpEventManager.Stats[types.ConfigName(logPtp4lConfig.Name)][types.IFace("master")] = stats_master
	ptpEventManager.Stats[types.ConfigName(logPtp4lConfig.Name)][types.IFace("CLOCK_REALTIME")] = stats_slave
	ptpEventManager.Stats[types.ConfigName(logPtp4lConfig.Name)][types.IFace("ens2f0")] = stats_master
	ptpEventManager.Stats[types.ConfigName(logPtp4lConfig.Name)][types.IFace("ens7f0")] = stats_slave
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
func Test_ExtractMetrics(t *testing.T) {

	assert := assert.New(t)
	for _, tc := range testCases {
		tc.node = MYNODE
		tc.cleanupMetrics()
		setLastSyncState(tc.iface, tc.lastSyncState)
		ptpEventManager.ExtractMetrics(tc.log)
		if tc.expectedPtpOffset != SKIP {
			ptpOffset := metrics.PtpOffset.With(map[string]string{"from": tc.from, "process": tc.process, "node": tc.node, "iface": tc.iface})
			statsAddValue(tc.iface, int64(testutil.ToFloat64(ptpOffset)))
			assert.Equal(tc.expectedPtpOffset, testutil.ToFloat64(ptpOffset), "PtpOffset does not match\n%s", tc.String())
		}
		if tc.expectedPtpMaxOffset != SKIP {
			ptpMaxOffset := metrics.PtpMaxOffset.With(map[string]string{"from": tc.from, "process": tc.process, "node": tc.node, "iface": tc.iface})
			assert.Equal(tc.expectedPtpMaxOffset, testutil.ToFloat64(ptpMaxOffset), "PtpMaxOffset does not match\n%s", tc.String())
		}
		if tc.expectedPtpFrequencyAdjustment != SKIP {
			ptpFrequencyAdjustment := metrics.PtpFrequencyAdjustment.With(map[string]string{"from": tc.from, "process": tc.process, "node": tc.node, "iface": tc.iface})
			assert.Equal(tc.expectedPtpFrequencyAdjustment, testutil.ToFloat64(ptpFrequencyAdjustment), "PtpFrequencyAdjustment does not match\n%s", tc.String())
		}
		if tc.expectedPtpDelay != SKIP {
			ptpDelay := metrics.PtpDelay.With(map[string]string{"from": tc.from, "process": tc.process, "node": tc.node, "iface": tc.iface})
			assert.Equal(tc.expectedPtpDelay, testutil.ToFloat64(ptpDelay), "PtpDelay does not match\n%s", tc.String())
		}
		if tc.expectedSyncState != SKIP {
			clockState := metrics.SyncState.With(map[string]string{"process": tc.process, "node": tc.node, "iface": tc.iface})
			assert.Equal(tc.expectedSyncState, testutil.ToFloat64(clockState), "SyncState does not match\n%s", tc.String())
		}
		if tc.expectedNmeaStatus != SKIP {
			nmeaStatus := metrics.NmeaStatus.With(map[string]string{"process": tc.process, "node": tc.node, "iface": tc.iface})
			assert.Equal(tc.expectedNmeaStatus, testutil.ToFloat64(nmeaStatus), "NmeaStatus does not match\n%s", tc.String())
		}
		if tc.expectedClockClassMetrics != SKIP {
			clockClassMetrics := metrics.ClockClassMetrics.With(map[string]string{"process": tc.process, "node": tc.node})
			assert.Equal(tc.expectedClockClassMetrics, testutil.ToFloat64(clockClassMetrics), "ClockClassMetrics does not match\n%s", tc.String())
		}
		assert.Equal(tc.expectedEvent, ptpEventManager.GetMockEvent(), "Expected Event does not match\n%s", tc.String())
	}
}
