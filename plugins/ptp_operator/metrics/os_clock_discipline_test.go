package metrics

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/redhat-cne/cloud-event-proxy/pkg/common"
	ptpConfig "github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/config"
	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/ptp4lconf"
	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/stats"
	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/types"
	"github.com/redhat-cne/sdk-go/pkg/event/ptp"
	"github.com/stretchr/testify/assert"
)

const (
	osClockProfile  = "bc-os-clock"
	osClockCfgName  = "ptp4l.0.config"
	osClockIface    = "ens5f0"
	osClockIface2   = "ens5f1"
	osClockPhc2Opts = "-a -r"
	osClockPortName = "port 1"
)

// setPhc2sysSelectionSettleDelayForTest overrides the settle delay for tests.
func setPhc2sysSelectionSettleDelayForTest(d time.Duration) (restore func()) {
	prev := phc2sysSelectionSettleDelay
	phc2sysSelectionSettleDelay = d
	return func() { phc2sysSelectionSettleDelay = prev }
}

func osClockInitPubSubTypes() map[ptp.EventType]*types.EventPublisherType {
	initPubs := make(map[ptp.EventType]*types.EventPublisherType)
	initPubs[ptp.OsClockSyncStateChange] = &types.EventPublisherType{
		EventType: ptp.OsClockSyncStateChange,
		Resource:  ptp.OsClockSyncState,
	}
	initPubs[ptp.PtpClockClassChange] = &types.EventPublisherType{
		EventType: ptp.PtpClockClassChange,
		Resource:  ptp.PtpClockClass,
	}
	initPubs[ptp.PtpStateChange] = &types.EventPublisherType{
		EventType: ptp.PtpStateChange,
		Resource:  ptp.PtpLockState,
	}
	initPubs[ptp.GnssStateChange] = &types.EventPublisherType{
		EventType: ptp.GnssStateChange,
		Resource:  ptp.GnssSyncStatus,
	}
	return initPubs
}

func setupOsClockDisciplineManager(t *testing.T, withChronyd bool) *PTPEventManager {
	t.Helper()
	eventManager := NewPTPEventManager("", osClockInitPubSubTypes(), "testnode", &common.SCConfiguration{StorePath: t.TempDir()})
	eventManager.MockTest(true)

	ptp4lCfg := &ptp4lconf.PTP4lConfig{
		Name:    osClockCfgName,
		Profile: osClockProfile,
		Interfaces: []*ptp4lconf.PTPInterface{
			{
				Name:     osClockIface,
				PortID:   1,
				PortName: osClockPortName,
				Role:     types.SLAVE,
			},
		},
	}
	eventManager.AddPTPConfig(types.ConfigName(osClockCfgName), ptp4lCfg)

	phc2Opts := osClockPhc2Opts
	opts := &ptpConfig.PtpProcessOpts{Phc2Opts: &phc2Opts}
	if withChronyd {
		chronydOpts := "-f /etc/chrony.conf"
		opts.ChronydOpts = &chronydOpts
	}
	eventManager.PtpConfigMapUpdates.PtpProcessOpts = map[string]*ptpConfig.PtpProcessOpts{
		osClockProfile: opts,
	}
	return eventManager
}

func lockClockRealtime(t *testing.T, eventManager *PTPEventManager) {
	t.Helper()
	line := fmt.Sprintf("phc2sys[3263.065]: [%s] CLOCK_REALTIME phc offset 3 s2 freq -20217 delay 536", osClockCfgName)
	eventManager.ExtractMetrics(line)
	ptpStats := eventManager.GetStats(types.ConfigName(osClockCfgName))
	cStat, ok := ptpStats[ClockRealTime]
	assert.True(t, ok)
	assert.Equal(t, ptp.LOCKED, cStat.LastSyncState())
}

func waitOsClockFreerun(t *testing.T, eventManager *PTPEventManager) {
	t.Helper()
	assert.Eventually(t, func() bool {
		return containsEvent(eventManager.GetMockEvent(), ptp.OsClockSyncStateChange)
	}, time.Second, 10*time.Millisecond)
	ptpStats := eventManager.GetStats(types.ConfigName(osClockCfgName))
	cStat := ptpStats[ClockRealTime]
	assert.Equal(t, ptp.FREERUN, cStat.LastSyncState())
	assert.True(t, cStat.OsClock.IsUndisciplined())
}

func containsEvent(events []ptp.EventType, target ptp.EventType) bool {
	for _, e := range events {
		if e == target {
			return true
		}
	}
	return false
}

// TestOsClockSilenceSettlePublishesFreerun: reconfigure + selecting NIC, then
// silence → settle timeout publishes E3 FREERUN.
func TestOsClockSilenceSettlePublishesFreerun(t *testing.T) {
	restore := setPhc2sysSelectionSettleDelayForTest(30 * time.Millisecond)
	t.Cleanup(restore)

	eventManager := setupOsClockDisciplineManager(t, false)
	lockClockRealtime(t, eventManager)
	eventManager.ResetMockEvent()

	eventManager.ExtractMetrics(fmt.Sprintf(
		"phc2sys[3264.000]: [%s] reconfiguring after port state change", osClockCfgName))
	eventManager.ExtractMetrics(fmt.Sprintf(
		"phc2sys[3264.000]: [%s] selecting %s for synchronization", osClockCfgName, osClockIface))

	waitOsClockFreerun(t, eventManager)
}

// TestOsClockAlreadySelectedPublishesFreerun: selection ends with
// "already selected" and no CLOCK_REALTIME sample → E3 FREERUN.
func TestOsClockAlreadySelectedPublishesFreerun(t *testing.T) {
	eventManager := setupOsClockDisciplineManager(t, false)
	lockClockRealtime(t, eventManager)
	eventManager.ResetMockEvent()

	eventManager.ExtractMetrics(fmt.Sprintf(
		"phc2sys[3264.000]: [%s] reconfiguring after port state change", osClockCfgName))
	eventManager.ExtractMetrics(fmt.Sprintf(
		"phc2sys[3264.000]: [%s] selecting %s for synchronization", osClockCfgName, osClockIface))
	eventManager.ExtractMetrics(fmt.Sprintf(
		"phc2sys[3264.000]: [%s] skipping %s: %s has the same clock and is already selected",
		osClockCfgName, osClockIface2, osClockIface))

	ptpStats := eventManager.GetStats(types.ConfigName(osClockCfgName))
	cStat := ptpStats[ClockRealTime]
	assert.Equal(t, ptp.FREERUN, cStat.LastSyncState())
	assert.True(t, cStat.OsClock.IsUndisciplined())
	assert.Contains(t, eventManager.GetMockEvent(), ptp.OsClockSyncStateChange)
}

// TestForwardSelectRealtimeCancelsSettle: selecting CLOCK_REALTIME cancels
// settle so a healthy reconfig does not spuriously FREERUN.
func TestForwardSelectRealtimeCancelsSettle(t *testing.T) {
	restore := setPhc2sysSelectionSettleDelayForTest(40 * time.Millisecond)
	t.Cleanup(restore)

	eventManager := setupOsClockDisciplineManager(t, false)
	lockClockRealtime(t, eventManager)
	eventManager.ResetMockEvent()

	eventManager.ExtractMetrics(fmt.Sprintf(
		"phc2sys[3264.000]: [%s] reconfiguring after port state change", osClockCfgName))
	eventManager.ExtractMetrics(fmt.Sprintf(
		"phc2sys[3264.000]: [%s] selecting %s for synchronization", osClockCfgName, osClockIface))
	eventManager.ExtractMetrics(fmt.Sprintf(
		"phc2sys[3264.000]: [%s] selecting %s as domain source clock", osClockCfgName, osClockIface))
	eventManager.ExtractMetrics(fmt.Sprintf(
		"phc2sys[3264.000]: [%s] selecting CLOCK_REALTIME for synchronization", osClockCfgName))

	time.Sleep(80 * time.Millisecond)

	ptpStats := eventManager.GetStats(types.ConfigName(osClockCfgName))
	cStat := ptpStats[ClockRealTime]
	assert.Equal(t, ptp.LOCKED, cStat.LastSyncState())
	assert.False(t, cStat.OsClock.IsUndisciplined())
	for _, evt := range eventManager.GetMockEvent() {
		assert.NotEqual(t, ptp.OsClockSyncStateChange, evt)
	}
}

// TestOsClockFreerunSkippedWhenChronydEnabled: chronyd owns E3 — no FREERUN.
func TestOsClockFreerunSkippedWhenChronydEnabled(t *testing.T) {
	restore := setPhc2sysSelectionSettleDelayForTest(30 * time.Millisecond)
	t.Cleanup(restore)

	eventManager := setupOsClockDisciplineManager(t, true)
	lockClockRealtime(t, eventManager)
	eventManager.ResetMockEvent()

	eventManager.ExtractMetrics(fmt.Sprintf(
		"phc2sys[3264.000]: [%s] reconfiguring after port state change", osClockCfgName))
	eventManager.ExtractMetrics(fmt.Sprintf(
		"phc2sys[3264.000]: [%s] selecting %s for synchronization", osClockCfgName, osClockIface))
	eventManager.ExtractMetrics(fmt.Sprintf(
		"phc2sys[3264.000]: [%s] skipping %s: %s has the same clock and is already selected",
		osClockCfgName, osClockIface2, osClockIface))

	time.Sleep(80 * time.Millisecond)

	ptpStats := eventManager.GetStats(types.ConfigName(osClockCfgName))
	assert.Equal(t, ptp.LOCKED, ptpStats[ClockRealTime].LastSyncState())
	for _, evt := range eventManager.GetMockEvent() {
		assert.NotEqual(t, ptp.OsClockSyncStateChange, evt)
	}
}

// TestTBCFreerunPlusOsClockSilencePublishesE3: T-BC FREERUN alone must not
// force E3 (OCPBUGS-88369); silence after NIC-only selection still does
// (OCPBUGS-105425).
func TestTBCFreerunPlusOsClockSilencePublishesE3(t *testing.T) {
	restore := setPhc2sysSelectionSettleDelayForTest(30 * time.Millisecond)
	t.Cleanup(restore)

	tbcProfile := "tbc-os-clock"
	cfgName := osClockCfgName
	iface := osClockIface

	eventManager := NewPTPEventManager("", osClockInitPubSubTypes(), "testnode",
		&common.SCConfiguration{StorePath: t.TempDir()})
	eventManager.MockTest(true)
	eventManager.PtpConfigMapUpdates.TBCProfiles = []string{tbcProfile}

	ptp4lCfg := &ptp4lconf.PTP4lConfig{
		Name:        cfgName,
		Profile:     tbcProfile,
		ProfileType: ptp4lconf.TBC,
		Interfaces: []*ptp4lconf.PTPInterface{
			{
				Name:     iface,
				PortID:   1,
				PortName: osClockPortName,
				Role:     types.SLAVE,
			},
		},
	}
	eventManager.AddPTPConfig(types.ConfigName(cfgName), ptp4lCfg)

	phc2Opts := osClockPhc2Opts
	eventManager.PtpConfigMapUpdates.PtpProcessOpts = map[string]*ptpConfig.PtpProcessOpts{
		tbcProfile: {Phc2Opts: &phc2Opts},
	}

	ptpStats := eventManager.GetStats(types.ConfigName(cfgName))
	ptpStats[MasterClockType] = stats.NewStats(cfgName)
	ptpStats[MasterClockType].SetAlias(iface)

	replacer := strings.NewReplacer("[", " ", "]", " ", ":", " ")
	tbcLockedLog := fmt.Sprintf("T-BC[1743005894]:[%s] %s offset 5 T-BC-STATUS s2", cfgName, iface)
	output := replacer.Replace(tbcLockedLog)
	eventManager.ParseTBCLogs("T-BC", cfgName, output, strings.Fields(output), ptpStats)

	eventManager.ExtractMetrics(fmt.Sprintf(
		"phc2sys[3263.065]: [%s] CLOCK_REALTIME phc offset 3 s2 freq -20217 delay 536", cfgName))
	cStat := ptpStats[ClockRealTime]
	assert.Equal(t, ptp.LOCKED, cStat.LastSyncState())

	eventManager.ResetMockEvent()
	tbcFreerunLog := fmt.Sprintf("T-BC[1743005900]:[%s] %s offset 123 T-BC-STATUS s0", cfgName, iface)
	output = replacer.Replace(tbcFreerunLog)
	eventManager.ParseTBCLogs("T-BC", cfgName, output, strings.Fields(output), ptpStats)

	tbcKey := types.IFace(stats.TBCMainClockName)
	assert.Equal(t, ptp.FREERUN, ptpStats[tbcKey].LastSyncState())
	assert.Equal(t, ptp.LOCKED, cStat.LastSyncState(),
		"CLOCK_REALTIME must stay LOCKED when only T-BC goes FREERUN")
	assert.NotContains(t, eventManager.GetMockEvent(), ptp.OsClockSyncStateChange)

	eventManager.ResetMockEvent()
	eventManager.ExtractMetrics(fmt.Sprintf(
		"phc2sys[3264.000]: [%s] reconfiguring after port state change", cfgName))
	eventManager.ExtractMetrics(fmt.Sprintf(
		"phc2sys[3264.000]: [%s] selecting %s for synchronization", cfgName, iface))

	assert.Eventually(t, func() bool {
		return containsEvent(eventManager.GetMockEvent(), ptp.OsClockSyncStateChange)
	}, time.Second, 10*time.Millisecond)
	assert.Equal(t, ptp.FREERUN, cStat.LastSyncState())
	assert.True(t, cStat.OsClock.IsUndisciplined())
}
