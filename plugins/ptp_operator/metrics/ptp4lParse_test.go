package metrics

import (
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	ptpConfig "github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/config"
	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/ptp4lconf"
	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/stats"
	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/types"
	"github.com/redhat-cne/sdk-go/pkg/event/ptp"
	"github.com/stretchr/testify/assert"
)

const (
	testNode        = "mynode"
	testConfigName  = "ptp4l.0.config"
	testProfileName = "boundary"
)

func ensureTestNode(t *testing.T) {
	t.Helper()
	if ptpNodeName == "" {
		ptpNodeName = testNode
	}
}

func TestHandleHoldOverStateExpiryTransitionsToFreerun(t *testing.T) {
	ensureTestNode(t)
	mgr := NewPTPEventManager("", nil, testNode, nil)
	mgr.MockTest(true)

	configName := testConfigName
	profileName := testProfileName
	mgr.Stats[types.ConfigName(configName)] = make(stats.PTPStats)
	ptpStats := mgr.GetStats(types.ConfigName(configName))
	ptpStats.CheckSource(master, configName, ptp4lProcessName)
	ptpStats[master].SetAlias("ens5fx")
	ptpStats[master].SetLastSyncState(ptp.HOLDOVER)

	closeCh := make(chan struct{})
	go handleHoldOverState(mgr, configName, profileName, 1, "ens5fx", closeCh)
	time.Sleep(1500 * time.Millisecond)

	assert.Equal(t, ptp.FREERUN, ptpStats[master].LastSyncState())
	syncState := testutil.ToFloat64(SyncState.With(map[string]string{
		"process": ptp4lProcessName,
		"node":    ptpNodeName,
		"iface":   "ens5fx",
	}))
	assert.Equal(t, float64(GetSyncStateID(string(ptp.FREERUN))), syncState)
	assert.Contains(t, mgr.GetMockEvent(), ptp.PtpStateChange)
}

func TestHandleHoldOverStateFaultHoldoverExpiry(t *testing.T) {
	t.Parallel()
	ensureTestNode(t)
	mgr := NewPTPEventManager("", nil, testNode, nil)
	mgr.MockTest(true)

	configName := testConfigName
	profileName := testProfileName
	mgr.Stats[types.ConfigName(configName)] = make(stats.PTPStats)
	ptpStats := mgr.GetStats(types.ConfigName(configName))
	ptpStats.CheckSource(master, configName, ptp4lProcessName)
	ptpStats[master].SetAlias("ens3fx")
	ptpStats[master].SetLastSyncState(ptp.HOLDOVER)

	closeCh := make(chan struct{})
	go handleHoldOverState(mgr, configName, profileName, 1, "ens3fx", closeCh)
	time.Sleep(1500 * time.Millisecond)

	assert.Equal(t, ptp.FREERUN, ptpStats[master].LastSyncState())
}

func TestHandleHoldOverStateCancelled(t *testing.T) {
	t.Parallel()
	ensureTestNode(t)
	mgr := NewPTPEventManager("", nil, testNode, nil)
	mgr.MockTest(true)

	configName := testConfigName
	mgr.Stats[types.ConfigName(configName)] = make(stats.PTPStats)
	ptpStats := mgr.GetStats(types.ConfigName(configName))
	ptpStats.CheckSource(master, configName, ptp4lProcessName)
	ptpStats[master].SetAlias("ens5fx")
	ptpStats[master].SetLastSyncState(ptp.HOLDOVER)

	closeCh := make(chan struct{})
	close(closeCh)
	go handleHoldOverState(mgr, configName, testProfileName, 1, "ens5fx", closeCh)
	time.Sleep(200 * time.Millisecond)

	assert.Equal(t, ptp.HOLDOVER, ptpStats[master].LastSyncState())
}

func TestHandleHoldOverStateExpiryNoOpWhenNotHoldover(t *testing.T) {
	t.Parallel()
	ensureTestNode(t)
	mgr := NewPTPEventManager("", nil, testNode, nil)
	mgr.MockTest(true)

	configName := testConfigName
	mgr.Stats[types.ConfigName(configName)] = make(stats.PTPStats)
	ptpStats := mgr.GetStats(types.ConfigName(configName))
	ptpStats.CheckSource(master, configName, ptp4lProcessName)
	ptpStats[master].SetAlias("ens5fx")
	ptpStats[master].SetLastSyncState(ptp.LOCKED)

	closeCh := make(chan struct{})
	go handleHoldOverState(mgr, configName, testProfileName, 1, "ens5fx", closeCh)
	time.Sleep(1500 * time.Millisecond)

	assert.Equal(t, ptp.LOCKED, ptpStats[master].LastSyncState())
}

func TestPtpThresholdResetClosesPreviousChannel(t *testing.T) {
	t.Parallel()
	ensureTestNode(t)
	mgr := NewPTPEventManager("", nil, testNode, nil)
	profileName := testProfileName
	oldClose := make(chan struct{})
	mgr.PtpConfigMapUpdates.EventThreshold[profileName] = &ptpConfig.PtpClockThreshold{
		HoldOverTimeout: 5,
		Close:           oldClose,
	}

	cancelled := make(chan struct{})
	go func() {
		<-oldClose
		close(cancelled)
	}()

	threshold := mgr.PtpThreshold(profileName, true)
	assert.NotNil(t, threshold.Close)
	assert.NotEqual(t, oldClose, threshold.Close)

	select {
	case <-cancelled:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("previous holdover timer channel was not closed on reset")
	}
}

func TestSlaveToMasterHoldoverTimerRecovery(t *testing.T) {
	ensureTestNode(t)
	SetMasterOffsetSource(ptp4lProcessName)

	mgr := NewPTPEventManager("", nil, testNode, nil)
	mgr.MockTest(true)

	configName := testConfigName
	profileName := testProfileName
	mgr.Stats[types.ConfigName(configName)] = make(stats.PTPStats)
	ptpStats := mgr.GetStats(types.ConfigName(configName))
	ptpStats.CheckSource(master, configName, ptp4lProcessName)
	ptpStats[master].SetAlias("ens5fx")
	ptpStats[master].SetLastSyncState(ptp.LOCKED)

	ptp4lCfg := &ptp4lconf.PTP4lConfig{
		Interfaces: []*ptp4lconf.PTPInterface{
			{Name: "ens5f0", PortID: 1, PortName: "port 1", Role: types.SLAVE},
		},
	}
	output := fmt.Sprintf("ptp4l[72444.514]: [%s:5] port 1 (ens5f0): SLAVE to MASTER on ANNOUNCE_RECEIPT_TIMEOUT_EXPIRES", testConfigName)
	fields := []string{"ptp4l", "1646672953", testConfigName, "port", "1", "(ens5f0)", "SLAVE to MASTER on ANNOUNCE_RECEIPT_TIMEOUT_EXPIRES"}

	mgr.ParsePTP4l(ptp4lProcessName, configName, profileName, output, fields,
		ptp4lconf.PTPInterface{Name: "ens5f0", PortID: 1, Role: types.SLAVE}, ptp4lCfg, ptpStats)
	assert.Equal(t, ptp.HOLDOVER, ptpStats[master].LastSyncState())

	closeCh := make(chan struct{})
	go handleHoldOverState(mgr, configName, profileName, 1, "ens5fx", closeCh)
	time.Sleep(1500 * time.Millisecond)
	assert.Equal(t, ptp.FREERUN, ptpStats[master].LastSyncState())
}

func TestClockClass248WithoutHoldoverNoStateChange(t *testing.T) {
	ensureTestNode(t)
	mgr := NewPTPEventManager("", nil, testNode, nil)
	mgr.MockTest(true)

	configName := testConfigName
	profileName := testProfileName
	mgr.Stats[types.ConfigName(configName)] = make(stats.PTPStats)
	ptpStats := mgr.GetStats(types.ConfigName(configName))
	ptpStats.CheckSource(master, configName, ptp4lProcessName)
	ptpStats[master].SetAlias("ens5fx")
	ptpStats[master].SetLastSyncState(ptp.FREERUN)
	ptpStats[master].SetClockClass(6)

	ptp4lCfg := &ptp4lconf.PTP4lConfig{
		Name:    configName,
		Profile: profileName,
		Interfaces: []*ptp4lconf.PTPInterface{
			{Name: "ens5f0", PortID: 1, PortName: "port 1", Role: types.MASTER},
		},
	}

	output := fmt.Sprintf("ptp4l 1646672953 %s CLOCK_CLASS_CHANGE 248.000000", testConfigName)
	fields := []string{"ptp4l", "1646672953", testConfigName, "CLOCK_CLASS_CHANGE", "248.000000"}
	mgr.ParsePTP4l(ptp4lProcessName, configName, profileName, output, fields,
		ptp4lconf.PTPInterface{Name: "ens5f0"}, ptp4lCfg, ptpStats)

	assert.Equal(t, ptp.FREERUN, ptpStats[master].LastSyncState())
	assert.Equal(t, int64(248), ptpStats[master].ClockClass())
	clockClass := testutil.ToFloat64(ClockClassMetrics.With(map[string]string{
		"process": ptp4lProcessName,
		"config":  configName,
		"node":    ptpNodeName,
	}))
	assert.Equal(t, float64(248), clockClass)
}

func TestGetPtpProcessOptsPopulatesEmptyCache(t *testing.T) {
	ensureTestNode(t)
	mgr := NewPTPEventManager("", nil, testNode, nil)
	profileName := "boundary"
	ptp4lOpts := "foo"
	mgr.PtpConfigMapUpdates.NodeProfiles = []ptpConfig.PtpProfile{
		{Name: &profileName, Ptp4lOpts: &ptp4lOpts},
	}
	mgr.AddPTPConfig(types.ConfigName("ptp4l.0.config"), &ptp4lconf.PTP4lConfig{
		Name:    "ptp4l.0.config",
		Profile: profileName,
	})

	opts := mgr.GetPtpProcessOpts("", "ptp4l.0.config")
	assert.NotNil(t, opts)
	assert.True(t, opts.Ptp4lEnabled())
}

func TestStartHoldoverTimerWithNilPtpOptsStillRecovers(t *testing.T) {
	ensureTestNode(t)
	mgr := NewPTPEventManager("", nil, testNode, nil)
	configName := "ptp4l.0.config"
	profileName := "boundary"
	mgr.PtpConfigMapUpdates.EventThreshold[profileName] = &ptpConfig.PtpClockThreshold{
		HoldOverTimeout: 1,
		Close:           make(chan struct{}),
	}
	mgr.Stats[types.ConfigName(configName)] = make(stats.PTPStats)
	ptpStats := mgr.GetStats(types.ConfigName(configName))
	ptpStats.CheckSource(master, configName, ptp4lProcessName)
	ptpStats[master].SetAlias("ens5fx")
	ptpStats[master].SetLastSyncState(ptp.HOLDOVER)

	mgr.startHoldoverTimer(nil, configName, profileName, "ens5fx")
	time.Sleep(1500 * time.Millisecond)

	assert.Equal(t, ptp.FREERUN, ptpStats[master].LastSyncState())
}

// TestParsePTP4l_DualUpstreamPreMasterRecovery reproduces the actual switchover/recovery
// sequence observed in OCPBUGS-111881 log-dual-upstream.txt, where the backup port (eno8403)
// transitions through MASTER to UNCALIBRATED → UNCALIBRATED to PRE_MASTER → PRE_MASTER to MASTER
// during and after the switchover, and must report MASTER (2) — not FAULTY (3) — in the metric.
//
// Sequence from ptp4l log:
//
//	ptp4l[17186.990] port 1 (eno8303): SLAVE to FAULTY           → eno8303=FAULTY
//	ptp4l[17187.015] port 2 (eno8403): MASTER to UNCALIBRATED    → eno8403=LISTENING (fixed, was FAULTY)
//	ptp4l[17192.156] port 1 (eno8303): FAULTY to LISTENING       → eno8303=LISTENING
//	ptp4l[17192.487] port 1 (eno8303): LISTENING to UNCALIBRATED → eno8303=LISTENING (fixed, was FAULTY)
//	ptp4l[17192.487] port 2 (eno8403): UNCALIBRATED to PRE_MASTER → eno8403=MASTER (was UNKNOWN/dropped)
//	ptp4l[17192.737] port 2 (eno8403): PRE_MASTER to MASTER      → eno8403=MASTER (was UNKNOWN/dropped)
//	ptp4l[17208.615] port 1 (eno8303): UNCALIBRATED to SLAVE     → eno8303=SLAVE
func TestParsePTP4l_DualUpstreamPreMasterRecovery(t *testing.T) {
	ensureTestNode(t)
	SetMasterOffsetSource(ptp4lProcessName)

	mgr := NewPTPEventManager("", nil, testNode, nil)
	mgr.MockTest(true)
	mockFS := &MockFileSystem{}
	Filesystem = mockFS

	cfgName := "ptp4l.1.config"
	profileName := testProfileName

	ptp4lCfg := &ptp4lconf.PTP4lConfig{
		Name:        cfgName,
		Profile:     profileName,
		ProfileType: ptp4lconf.TBC,
		Interfaces: []*ptp4lconf.PTPInterface{
			{Name: "eno8303", PortID: 1, PortName: "port 1", Role: types.SLAVE},
			{Name: "eno8403", PortID: 2, PortName: "port 2", Role: types.MASTER},
		},
	}
	mgr.AddPTPConfig(types.ConfigName(cfgName), ptp4lCfg)
	mgr.Stats[types.ConfigName(cfgName)] = make(stats.PTPStats)
	ptpStats := mgr.GetStats(types.ConfigName(cfgName))
	ptpStats.CheckSource(master, cfgName, ptp4lProcessName)
	ptpStats[master].SetAlias("eno8303x")
	ptpStats[master].SetLastSyncState(ptp.LOCKED)

	roleLabel := func(iface string) map[string]string {
		return map[string]string{"process": ptp4lProcessName, "node": testNode, "iface": iface}
	}

	// Pre-initialize metrics to match starting state (eno8303=SLAVE, eno8403=MASTER)
	UpdateInterfaceRoleMetrics(ptp4lProcessName, "eno8303", types.SLAVE)
	UpdateInterfaceRoleMetrics(ptp4lProcessName, "eno8403", types.MASTER)

	parse := func(output string) {
		fields := []string{"ptp4l", "0", cfgName}
		mgr.ParsePTP4l(ptp4lProcessName, cfgName, profileName, output, fields,
			ptp4lconf.PTPInterface{}, ptp4lCfg, ptpStats)
	}

	// Step 1: eno8303 link goes down → SLAVE to FAULTY
	parse("ptp4l[17186.990]: [ptp4l.1.config] port 1 (eno8303): SLAVE to FAULTY on FAULT_DETECTED (FT_UNSPECIFIED)")
	assert.Equal(t, types.FAULTY, ptp4lCfg.Interfaces[0].Role, "step1: eno8303 should be FAULTY")
	assert.Equal(t, float64(types.FAULTY), testutil.ToFloat64(InterfaceRole.With(roleLabel("eno8303"))), "step1: eno8303 metric=FAULTY")

	// Step 2: eno8403 backup starts evaluating upstream (MASTER to UNCALIBRATED → LISTENING, not FAULTY)
	parse("ptp4l[17187.015]: [ptp4l.1.config] port 2 (eno8403): MASTER to UNCALIBRATED on RS_SLAVE")
	assert.Equal(t, types.LISTENING, ptp4lCfg.Interfaces[1].Role, "step2: eno8403 should be LISTENING (BMCA eval, not FAULTY)")
	assert.Equal(t, float64(types.LISTENING), testutil.ToFloat64(InterfaceRole.With(roleLabel("eno8403"))), "step2: eno8403 metric=LISTENING")

	// Step 3: eno8303 link recovers → FAULTY to LISTENING
	parse("ptp4l[17192.156]: [ptp4l.1.config] port 1 (eno8303): FAULTY to LISTENING on INIT_COMPLETE")
	assert.Equal(t, types.LISTENING, ptp4lCfg.Interfaces[0].Role, "step3: eno8303 should be LISTENING")
	assert.Equal(t, float64(types.LISTENING), testutil.ToFloat64(InterfaceRole.With(roleLabel("eno8303"))), "step3: eno8303 metric=LISTENING")

	// Step 4: eno8303 selected as upstream (LISTENING to UNCALIBRATED → LISTENING, not FAULTY)
	parse("ptp4l[17192.487]: [ptp4l.1.config] port 1 (eno8303): LISTENING to UNCALIBRATED on RS_SLAVE")
	assert.Equal(t, types.LISTENING, ptp4lCfg.Interfaces[0].Role, "step4: eno8303 should remain LISTENING during BMCA eval")
	assert.Equal(t, float64(types.LISTENING), testutil.ToFloat64(InterfaceRole.With(roleLabel("eno8303"))), "step4: eno8303 metric=LISTENING")

	// Step 5: eno8403 transitions to PRE_MASTER (reported as MASTER)
	parse("ptp4l[17192.487]: [ptp4l.1.config] port 2 (eno8403): UNCALIBRATED to PRE_MASTER on RS_MASTER")
	assert.Equal(t, types.MASTER, ptp4lCfg.Interfaces[1].Role, "step5: eno8403 should be MASTER (PRE_MASTER→MASTER)")
	assert.Equal(t, float64(types.MASTER), testutil.ToFloat64(InterfaceRole.With(roleLabel("eno8403"))), "step5: eno8403 metric=MASTER")

	// Step 6: eno8403 fully becomes MASTER
	parse("ptp4l[17192.737]: [ptp4l.1.config] port 2 (eno8403): PRE_MASTER to MASTER on QUALIFICATION_TIMEOUT_EXPIRES")
	assert.Equal(t, types.MASTER, ptp4lCfg.Interfaces[1].Role, "step6: eno8403 should remain MASTER")
	assert.Equal(t, float64(types.MASTER), testutil.ToFloat64(InterfaceRole.With(roleLabel("eno8403"))), "step6: eno8403 metric=MASTER")

	// Step 7: eno8303 locks as SLAVE — final recovered state
	parse("ptp4l[17208.615]: [ptp4l.1.config] port 1 (eno8303): UNCALIBRATED to SLAVE on MASTER_CLOCK_SELECTED")
	assert.Equal(t, types.SLAVE, ptp4lCfg.Interfaces[0].Role, "step7: eno8303 should be SLAVE")
	assert.Equal(t, types.MASTER, ptp4lCfg.Interfaces[1].Role, "step7: eno8403 stays MASTER (serving downstream)")
	assert.Equal(t, float64(types.SLAVE), testutil.ToFloat64(InterfaceRole.With(roleLabel("eno8303"))), "step7: eno8303 metric=SLAVE")
	assert.Equal(t, float64(types.MASTER), testutil.ToFloat64(InterfaceRole.With(roleLabel("eno8403"))), "step7: eno8403 metric=MASTER")
}
