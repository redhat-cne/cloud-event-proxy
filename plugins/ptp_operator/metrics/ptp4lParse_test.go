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
