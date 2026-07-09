package metrics_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/redhat-cne/cloud-event-proxy/pkg/common"
	ptpConfig "github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/config"
	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/metrics"
	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/ptp4lconf"
	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/stats"
	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/types"
	"github.com/redhat-cne/sdk-go/pkg/event/ptp"
	"github.com/stretchr/testify/assert"
	"k8s.io/utils/pointer"
)

const (
	ntpFailoverProfile = "ntp-failover"
	ntpPtp4lCfgName    = "ptp4l.0.config"
	ntpChronydCfgName  = "chronyd.0.config"
	ntpPhc2sysOpts     = "-a -r -r -n 24"
	ntpChronydOpts     = "-f /etc/chrony.conf"
)

// TestNTPProcessDownDoesNotEmitOsClockSyncStateChange verifies that when
// phc2sys reports process_status 0 in an NTP-failover configuration (chronyd
// enabled), no OsClockSyncStateChange event is emitted for CLOCK_REALTIME.
// Chronyd is the sole E3 publisher in NTP mode.
func TestNTPProcessDownDoesNotEmitOsClockSyncStateChange(t *testing.T) {
	eventManager := metrics.NewPTPEventManager("", initPubSubTypes(), "testnode", &common.SCConfiguration{StorePath: "/tmp/store"})
	eventManager.MockTest(true)

	ptp4lCfg := &ptp4lconf.PTP4lConfig{
		Name:    ntpPtp4lCfgName,
		Profile: ntpFailoverProfile,
		Interfaces: []*ptp4lconf.PTPInterface{
			{
				Name:     "ens3f0",
				PortID:   1,
				PortName: "port 1",
				Role:     types.SLAVE,
			},
		},
	}
	eventManager.AddPTPConfig(types.ConfigName(ntpPtp4lCfgName), ptp4lCfg)

	phc2sysOpts := ntpPhc2sysOpts
	chronydOpts := ntpChronydOpts
	eventManager.PtpConfigMapUpdates.PtpProcessOpts = map[string]*ptpConfig.PtpProcessOpts{
		ntpFailoverProfile: {
			Phc2Opts:    &phc2sysOpts,
			ChronydOpts: &chronydOpts,
		},
	}

	ptpStats := eventManager.GetStats(types.ConfigName(ntpPtp4lCfgName))
	ptpStats.CheckSource(metrics.ClockRealTime, ntpPtp4lCfgName, "phc2sys")
	ptpStats[metrics.ClockRealTime].SetLastSyncState(ptp.LOCKED)

	eventManager.ResetMockEvent()

	phc2sysDown := fmt.Sprintf("phc2sys[1234.567]: [%s] PTP_PROCESS_STATUS 0", ntpPtp4lCfgName)
	eventManager.ExtractMetrics(phc2sysDown)

	mockEvents := eventManager.GetMockEvent()
	for _, evt := range mockEvents {
		assert.NotEqual(t, ptp.OsClockSyncStateChange, evt,
			"phc2sys process-down must NOT emit OsClockSyncStateChange when chronyd is enabled")
	}

	assert.Equal(t, ptp.LOCKED, ptpStats[metrics.ClockRealTime].LastSyncState(),
		"CLOCK_REALTIME internal state must remain LOCKED (chronyd manages it)")
}

// TestNTPProcessDownEmitsOsClockWhenNoChronyd confirms that the existing
// behavior is preserved: when phc2sys goes down in a profile WITHOUT chronyd,
// CLOCK_REALTIME should transition to FREERUN.
func TestNTPProcessDownEmitsOsClockWhenNoChronyd(t *testing.T) {
	profileName := "ptp-oc"

	eventManager := metrics.NewPTPEventManager("", initPubSubTypes(), "testnode", &common.SCConfiguration{StorePath: "/tmp/store"})
	eventManager.MockTest(true)

	ptp4lCfg := &ptp4lconf.PTP4lConfig{
		Name:    ntpPtp4lCfgName,
		Profile: profileName,
		Interfaces: []*ptp4lconf.PTPInterface{
			{
				Name:     "ens3f0",
				PortID:   1,
				PortName: "port 1",
				Role:     types.SLAVE,
			},
		},
	}
	eventManager.AddPTPConfig(types.ConfigName(ntpPtp4lCfgName), ptp4lCfg)

	phc2sysOpts := ntpPhc2sysOpts
	eventManager.PtpConfigMapUpdates.PtpProcessOpts = map[string]*ptpConfig.PtpProcessOpts{
		profileName: {
			Phc2Opts: &phc2sysOpts,
		},
	}

	ptpStats := eventManager.GetStats(types.ConfigName(ntpPtp4lCfgName))
	ptpStats.CheckSource(metrics.ClockRealTime, ntpPtp4lCfgName, "phc2sys")
	ptpStats[metrics.ClockRealTime].SetLastSyncState(ptp.LOCKED)

	eventManager.ResetMockEvent()

	phc2sysDown := fmt.Sprintf("phc2sys[1234.567]: [%s] PTP_PROCESS_STATUS 0", ntpPtp4lCfgName)
	eventManager.ExtractMetrics(phc2sysDown)

	mockEvents := eventManager.GetMockEvent()
	hasOsClockEvent := false
	for _, evt := range mockEvents {
		if evt == ptp.OsClockSyncStateChange {
			hasOsClockEvent = true
		}
	}
	assert.True(t, hasOsClockEvent,
		"phc2sys process-down MUST emit OsClockSyncStateChange when chronyd is NOT enabled")

	assert.Equal(t, ptp.FREERUN, ptpStats[metrics.ClockRealTime].LastSyncState(),
		"CLOCK_REALTIME should be FREERUN after phc2sys dies (no chronyd)")
}

// TestNTPChronydSelectedSourceSetsLocked verifies that chronyd "Selected source"
// correctly sets CLOCK_REALTIME to LOCKED and is the sole E3 publisher.
func TestNTPChronydSelectedSourceSetsLocked(t *testing.T) {
	eventManager := metrics.NewPTPEventManager("", initPubSubTypes(), "testnode", &common.SCConfiguration{StorePath: "/tmp/store"})
	eventManager.MockTest(true)

	ptp4lCfg := &ptp4lconf.PTP4lConfig{
		Name:    ntpChronydCfgName,
		Profile: ntpFailoverProfile,
	}
	eventManager.AddPTPConfig(types.ConfigName(ntpChronydCfgName), ptp4lCfg)

	chronydOpts := ntpChronydOpts
	eventManager.PtpConfigMapUpdates.PtpProcessOpts = map[string]*ptpConfig.PtpProcessOpts{
		ntpFailoverProfile: {
			ChronydOpts: &chronydOpts,
		},
	}

	eventManager.ResetMockEvent()

	chronydLog := fmt.Sprintf("chronyd[5678.901]: [%s] Selected source 192.168.1.1 (ntp.example.com)", ntpChronydCfgName)
	eventManager.ExtractMetrics(chronydLog)

	ptpStats := eventManager.GetStats(types.ConfigName(ntpChronydCfgName))
	cStat, ok := ptpStats[metrics.ClockRealTime]
	assert.True(t, ok, "CLOCK_REALTIME stats should exist after chronyd Selected source")
	assert.Equal(t, ptp.LOCKED, cStat.LastSyncState(),
		"CLOCK_REALTIME should be LOCKED from chronyd Selected source")

	mockEvents := eventManager.GetMockEvent()
	hasOsClockEvent := false
	for _, evt := range mockEvents {
		if evt == ptp.OsClockSyncStateChange {
			hasOsClockEvent = true
		}
	}
	assert.True(t, hasOsClockEvent,
		"chronyd Selected source must emit OsClockSyncStateChange")
}

// TestNTPNodeSyncStateIncludesMasterFreerun verifies that in the STABLE NTP
// state, the node-level sync-state is FREERUN because ptp4l master (PTP source
// absent) is in FREERUN. Per the state chart, sync-state = worst(os-clock=LOCKED,
// ptp-lock=FREERUN) = FREERUN.
func TestNTPNodeSyncStateIncludesMasterFreerun(t *testing.T) {
	eventManager := metrics.NewPTPEventManager("", initPubSubTypes(), "testnode", &common.SCConfiguration{StorePath: "/tmp/store"})
	eventManager.MockTest(true)

	ptp4lCfg := &ptp4lconf.PTP4lConfig{
		Name:    ntpPtp4lCfgName,
		Profile: ntpFailoverProfile,
		Interfaces: []*ptp4lconf.PTPInterface{
			{
				Name:     "ens3f0",
				PortID:   1,
				PortName: "port 1",
				Role:     types.SLAVE,
			},
		},
	}
	eventManager.AddPTPConfig(types.ConfigName(ntpPtp4lCfgName), ptp4lCfg)

	chronydCfg := &ptp4lconf.PTP4lConfig{
		Name:    ntpChronydCfgName,
		Profile: ntpFailoverProfile,
	}
	eventManager.AddPTPConfig(types.ConfigName(ntpChronydCfgName), chronydCfg)

	phc2sysOpts := ntpPhc2sysOpts
	chronydOpts := ntpChronydOpts
	eventManager.PtpConfigMapUpdates.PtpProcessOpts = map[string]*ptpConfig.PtpProcessOpts{
		ntpFailoverProfile: {
			Phc2Opts:    &phc2sysOpts,
			ChronydOpts: &chronydOpts,
		},
	}

	ptp4lStats := eventManager.GetStats(types.ConfigName(ntpPtp4lCfgName))
	ptp4lStats.CheckSource(metrics.MasterClockType, ntpPtp4lCfgName, "ptp4l")
	ptp4lStats[metrics.MasterClockType].SetLastSyncState(ptp.FREERUN)
	ptp4lStats[metrics.MasterClockType].SetAlias("ens3f0")

	chronydStats := eventManager.GetStats(types.ConfigName(ntpChronydCfgName))
	chronydStats.CheckSource(metrics.ClockRealTime, ntpChronydCfgName, "chronyd")
	chronydStats[metrics.ClockRealTime].SetLastSyncState(ptp.LOCKED)

	nodeState := eventManager.GetNodeSyncState(ptp.LOCKED)
	assert.Equal(t, ptp.FREERUN, nodeState,
		"STABLE NTP: sync-state must be FREERUN (ptp4l master FREERUN drags it down per state chart)")
}

// TestNTPNoDoubleClockRealtime verifies the full NTP-failover scenario:
// phc2sys goes down but chronyd maintains CLOCK_REALTIME, resulting in a
// single consistent CLOCK_REALTIME state (LOCKED) without conflicting events.
func TestNTPNoDoubleClockRealtime(t *testing.T) {
	eventManager := metrics.NewPTPEventManager("", initPubSubTypes(), "testnode", &common.SCConfiguration{StorePath: "/tmp/store"})
	eventManager.MockTest(true)

	ptp4lCfg := &ptp4lconf.PTP4lConfig{
		Name:    ntpPtp4lCfgName,
		Profile: ntpFailoverProfile,
		Interfaces: []*ptp4lconf.PTPInterface{
			{
				Name:     "ens3f0",
				PortID:   1,
				PortName: "port 1",
				Role:     types.SLAVE,
			},
		},
	}
	eventManager.AddPTPConfig(types.ConfigName(ntpPtp4lCfgName), ptp4lCfg)

	chronydCfg := &ptp4lconf.PTP4lConfig{
		Name:    ntpChronydCfgName,
		Profile: ntpFailoverProfile,
	}
	eventManager.AddPTPConfig(types.ConfigName(ntpChronydCfgName), chronydCfg)

	phc2sysOpts := ntpPhc2sysOpts
	chronydOpts := ntpChronydOpts
	eventManager.PtpConfigMapUpdates.PtpProcessOpts = map[string]*ptpConfig.PtpProcessOpts{
		ntpFailoverProfile: {
			Phc2Opts:    &phc2sysOpts,
			ChronydOpts: &chronydOpts,
		},
	}

	metrics.SetMasterOffsetSource("ptp4l")

	// Step 1: ptp4l reports port SLAVE — initializes ptpStats
	replacer := strings.NewReplacer("[", " ", "]", " ", ":", " ")
	_ = replacer

	ptp4lSlave := fmt.Sprintf("ptp4l[1000.000]: [%s] port 1 (ens3f0): LISTENING to SLAVE on RS_SLAVE", ntpPtp4lCfgName)
	eventManager.ExtractMetrics(ptp4lSlave)
	t.Log("Step 1: ptp4l port goes SLAVE")

	// Step 2: phc2sys reports process DOWN
	eventManager.ResetMockEvent()
	phc2sysDown := fmt.Sprintf("phc2sys[1001.000]: [%s] PTP_PROCESS_STATUS 0", ntpPtp4lCfgName)
	eventManager.ExtractMetrics(phc2sysDown)
	t.Log("Step 2: phc2sys process goes DOWN")

	mockEvents := eventManager.GetMockEvent()
	for _, evt := range mockEvents {
		assert.NotEqual(t, ptp.OsClockSyncStateChange, evt,
			"No OsClockSyncStateChange should fire for phc2sys down in NTP mode")
	}

	// Step 3: chronyd reports Selected source → CLOCK_REALTIME LOCKED
	eventManager.ResetMockEvent()
	chronydSelected := fmt.Sprintf("chronyd[1002.000]: [%s] Selected source 192.168.1.1 (ntp.example.com)", ntpChronydCfgName)
	eventManager.ExtractMetrics(chronydSelected)
	t.Log("Step 3: chronyd Selected source")

	chronydStats := eventManager.GetStats(types.ConfigName(ntpChronydCfgName))
	assert.Equal(t, ptp.LOCKED, chronydStats[metrics.ClockRealTime].LastSyncState(),
		"chronyd CLOCK_REALTIME should be LOCKED")

	// Verify ptp4l's stats for CLOCK_REALTIME were not set to FREERUN
	ptp4lStats := eventManager.GetStats(types.ConfigName(ntpPtp4lCfgName))
	if crStat, ok := ptp4lStats[metrics.ClockRealTime]; ok {
		assert.NotEqual(t, ptp.FREERUN, crStat.LastSyncState(),
			"ptp4l config CLOCK_REALTIME should NOT be set to FREERUN by processDownEvent in NTP mode")
	}
}

// TestNTPNodeSyncStateDuringHoldover verifies that during the "LOST GNSS
// (IN HOLDOVER SPEC)" state, sync-state is correctly HOLDOVER even when
// os-clock-sync-state (CLOCK_REALTIME) is LOCKED. Per the state chart,
// sync-state = worst(os-clock=LOCKED, ptp-lock=HOLDOVER) = HOLDOVER.
func TestNTPNodeSyncStateDuringHoldover(t *testing.T) {
	profileName := "gnss-failover"

	eventManager := metrics.NewPTPEventManager("", initPubSubTypes(), "testnode", &common.SCConfiguration{StorePath: "/tmp/store"})
	eventManager.MockTest(true)

	ptp4lCfg := &ptp4lconf.PTP4lConfig{
		Name:    ntpPtp4lCfgName,
		Profile: profileName,
		Interfaces: []*ptp4lconf.PTPInterface{
			{
				Name:     "ens3f0",
				PortID:   1,
				PortName: "port 1",
				Role:     types.SLAVE,
			},
		},
	}
	eventManager.AddPTPConfig(types.ConfigName(ntpPtp4lCfgName), ptp4lCfg)

	phc2sysOpts := ntpPhc2sysOpts
	eventManager.PtpConfigMapUpdates.PtpProcessOpts = map[string]*ptpConfig.PtpProcessOpts{
		profileName: {
			Phc2Opts: &phc2sysOpts,
		},
	}

	ptpStats := eventManager.GetStats(types.ConfigName(ntpPtp4lCfgName))
	ptpStats.CheckSource(metrics.MasterClockType, ntpPtp4lCfgName, "ptp4l")
	ptpStats[metrics.MasterClockType].SetLastSyncState(ptp.HOLDOVER)
	ptpStats[metrics.MasterClockType].SetAlias("ens3f0")

	ptpStats.CheckSource(metrics.ClockRealTime, ntpPtp4lCfgName, "phc2sys")
	ptpStats[metrics.ClockRealTime].SetLastSyncState(ptp.LOCKED)

	nodeState := eventManager.GetNodeSyncState(ptp.LOCKED)
	assert.Equal(t, ptp.HOLDOVER, nodeState,
		"LOST GNSS (HOLDOVER): sync-state must be HOLDOVER even though os-clock is LOCKED")
}

// Silence unused-import warnings for packages referenced indirectly.
var _ = pointer.String
var _ = stats.TBCMainClockName
