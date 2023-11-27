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
	SKIP    = 12345678
	CLEANUP = -1
)

var ptp4lConfig = &ptp4lconf.PTP4lConfig{
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
	expectedPtpOffset              float64 // offset_ns
	expectedPtpMaxOffset           float64 // max_offset_ns
	expectedPtpFrequencyAdjustment float64 // frequency_adjustment_ns
	expectedPtpDelay               float64 // delay_ns
	expectedSyncState              float64 // clock_state
	expectedNmeaStatus             float64 // nmea_status
	expectedPpsStatus              float64 // pps_status
}

func (tc *TestCase) init() {
	tc.expectedPtpOffset = SKIP
	tc.expectedPtpMaxOffset = SKIP
	tc.expectedPtpFrequencyAdjustment = SKIP
	tc.expectedPtpDelay = SKIP
	tc.expectedSyncState = SKIP
	tc.expectedNmeaStatus = SKIP
	tc.expectedPpsStatus = SKIP
}

func (tc *TestCase) String() string {
	b := strings.Builder{}
	b.WriteString("log: \"" + tc.log + "\"\n")
	b.WriteString("from: " + tc.from + "\n")
	b.WriteString("process: " + tc.process + "\n")
	b.WriteString("node: " + tc.node + "\n")
	b.WriteString("iface: " + tc.iface + "\n")
	return b.String()
}

func (tc *TestCase) cleanupMetrics() {
	metrics.PtpOffset.With(map[string]string{"from": tc.from, "process": tc.process, "node": tc.node, "iface": tc.iface}).Set(CLEANUP)
	metrics.SyncState.With(map[string]string{"process": tc.process, "node": tc.node, "iface": tc.iface}).Set(CLEANUP)
	metrics.PpsStatus.With(map[string]string{"process": tc.process, "node": tc.node, "iface": tc.iface}).Set(CLEANUP)
	metrics.NmeaStatus.With(map[string]string{"process": tc.process, "node": tc.node, "iface": tc.iface}).Set(CLEANUP)
}

var testCases = []TestCase{
	{
		log:                            "dpll[1700614893]:[ts2phc.0.config] ens7f0 frequency_status 3 offset 5 phase_status 3 pps_status 1 s2",
		process:                        "dpll",
		iface:                          "ens7fx",
		expectedPtpOffset:              SKIP,
		expectedPtpMaxOffset:           SKIP,
		expectedPtpFrequencyAdjustment: SKIP,
		expectedPtpDelay:               SKIP,
		expectedSyncState:              s2,
		expectedNmeaStatus:             SKIP,
		expectedPpsStatus:              1,
	},
	{
		log:                            "dpll[1700614893]:[ts2phc.0.config] ens7f0 frequency_status 3 offset 5 phase_status 3 pps_status 0 s0",
		process:                        "dpll",
		iface:                          "ens7fx",
		expectedPtpOffset:              SKIP,
		expectedPtpMaxOffset:           SKIP,
		expectedPtpFrequencyAdjustment: SKIP,
		expectedPtpDelay:               SKIP,
		expectedSyncState:              s0,
		expectedNmeaStatus:             SKIP,
		expectedPpsStatus:              0,
	},
	{
		log:                            "dpll[1700614893]:[ts2phc.0.config] ens7f0 frequency_status 3 offset 7 phase_status 3 pps_status 0 s1",
		process:                        "dpll",
		iface:                          "ens7fx",
		expectedPtpOffset:              SKIP,
		expectedPtpMaxOffset:           SKIP,
		expectedPtpFrequencyAdjustment: SKIP,
		expectedPtpDelay:               SKIP,
		expectedSyncState:              s1,
		expectedNmeaStatus:             SKIP,
		expectedPpsStatus:              0,
	},
	{
		log:                            "ts2phc[1699929121]:[ts2phc.0.config] ens2f0 nmea_status 0 offset 999999 s0",
		from:                           "ts2phc",
		process:                        "ts2phc",
		iface:                          "ens2fx",
		expectedPtpOffset:              SKIP,
		expectedPtpMaxOffset:           SKIP,
		expectedPtpFrequencyAdjustment: SKIP,
		expectedPtpDelay:               SKIP,
		expectedSyncState:              s0,
		expectedNmeaStatus:             0,
		expectedPpsStatus:              SKIP,
	},
	{
		log:                            "ts2phc[1699929171]:[ts2phc.0.config] ens2fx nmea_status 1 offset 0 s2",
		process:                        "ts2phc",
		iface:                          "ens2fx",
		expectedPtpOffset:              SKIP,
		expectedPtpMaxOffset:           SKIP,
		expectedPtpFrequencyAdjustment: SKIP,
		expectedPtpDelay:               SKIP,
		expectedSyncState:              s2,
		expectedNmeaStatus:             1,
		expectedPpsStatus:              SKIP,
	},
	{
		log:                            "ts2phc[441664.291]: [ts2phc.0.config] ens2f0 master offset          0 s2 freq      -0",
		from:                           "master",
		process:                        "ts2phc",
		iface:                          "ens2fx",
		expectedPtpOffset:              0,
		expectedPtpMaxOffset:           SKIP,
		expectedPtpFrequencyAdjustment: SKIP,
		expectedPtpDelay:               SKIP,
		expectedSyncState:              s2,
		expectedNmeaStatus:             SKIP,
		expectedPpsStatus:              SKIP,
	},
	{
		log:                            "ts2phc[441664.291]: [ts2phc.0.config] ens7f0 master offset 999 s0 freq      -0",
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
	},
	{
		log:                            "GM[1699929086]:[ts2phc.0.config] ens2f0 T-GM-STATUS s0",
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
	},
	{
		log:                            "gnss[1689014431]:[ts2phc.0.config] ens2f1 gnss_status 3 offset 5 s2",
		from:                           "gnss",
		process:                        "gnss",
		iface:                          "ens2fx",
		expectedPtpOffset:              5,
		expectedPtpMaxOffset:           SKIP,
		expectedPtpFrequencyAdjustment: SKIP,
		expectedPtpDelay:               SKIP,
		expectedSyncState:              s2,
		expectedNmeaStatus:             SKIP,
		expectedPpsStatus:              SKIP,
	},
}

func setup() {
	ptpEventManager = metrics.NewPTPEventManager(resourcePrefix, InitPubSubTypes(), "tetsnode", scConfig)
	ptpEventManager.MockTest(true)

	ptpEventManager.AddPTPConfig(types.ConfigName(ptp4lConfig.Name), ptp4lConfig)

	stats_master := stats.NewStats(ptp4lConfig.Name)
	stats_master.SetOffsetSource("master")
	stats_master.SetProcessName("ts2phc")
	stats_master.SetAlias("ens2fx")

	stats_slave := stats.NewStats(ptp4lConfig.Name)
	stats_slave.SetOffsetSource("phc")
	stats_slave.SetProcessName("phc2sys")
	stats_slave.SetLastSyncState("LOCKED")
	stats_slave.SetClockClass(0)

	ptpEventManager.Stats[types.ConfigName(ptp4lConfig.Name)] = make(map[types.IFace]*stats.Stats)
	ptpEventManager.Stats[types.ConfigName(ptp4lConfig.Name)][types.IFace("master")] = stats_master
	ptpEventManager.Stats[types.ConfigName(ptp4lConfig.Name)][types.IFace("CLOCK_REALTIME")] = stats_slave
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
		ptpEventManager.ExtractMetrics(tc.log)
		if tc.expectedPtpOffset != SKIP {
			ptpOffset := metrics.PtpOffset.With(map[string]string{"from": tc.from, "process": tc.process, "node": tc.node, "iface": tc.iface})
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
		if tc.expectedPpsStatus != SKIP {
			ppsStatus := metrics.PpsStatus.With(map[string]string{"process": tc.process, "node": tc.node, "iface": tc.iface})
			assert.Equal(tc.expectedPpsStatus, testutil.ToFloat64(ppsStatus), "PpsStatus does not match\n%s", tc.String())
		}
		if tc.expectedNmeaStatus != SKIP {
			nmeaStatus := metrics.NmeaStatus.With(map[string]string{"process": tc.process, "node": tc.node, "iface": tc.iface})
			assert.Equal(tc.expectedNmeaStatus, testutil.ToFloat64(nmeaStatus), "NmeaStatus does not match\n%s", tc.String())
		}
	}
}
