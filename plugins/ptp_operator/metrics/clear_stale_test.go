package metrics

import (
	"fmt"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	ptpConfig "github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/config"
	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/ptp4lconf"
	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/types"
	"github.com/redhat-cne/sdk-go/pkg/event/ptp"
	"github.com/stretchr/testify/assert"
)

const (
	staleMetricProfile = "ntp-failover"
	staleMetricPtp4l   = "ptp4l.0.config"
	staleMetricChronyd = "chronyd.0.config"
	staleMetricPhcOpts = "-a -r -r -n 24"
	staleMetricChrOpts = "-f /etc/chrony.conf"
	labelProcessName   = "process"
	labelIfaceName     = "iface"
)

func syncStateExists(process string) bool {
	ch := make(chan prometheus.Metric, 64)
	go func() {
		SyncState.Collect(ch)
		close(ch)
	}()
	for m := range ch {
		var d dto.Metric
		if err := m.Write(&d); err != nil {
			continue
		}
		var p, i string
		for _, lp := range d.Label {
			switch lp.GetName() {
			case labelProcessName:
				p = lp.GetValue()
			case labelIfaceName:
				i = lp.GetValue()
			}
		}
		if p == process && i == ClockRealTime {
			return true
		}
	}
	return false
}

func newStaleMetricManager(t *testing.T, phcOpts, chronydOpts *string) *PTPEventManager {
	t.Helper()
	ensureTestNode(t)
	mgr := NewPTPEventManager("", nil, testNode, nil)
	mgr.MockTest(true)
	mgr.AddPTPConfig(types.ConfigName(staleMetricPtp4l), &ptp4lconf.PTP4lConfig{
		Name:    staleMetricPtp4l,
		Profile: staleMetricProfile,
		Interfaces: []*ptp4lconf.PTPInterface{{
			Name: "ens3f0", PortID: 1, PortName: "port 1", Role: types.SLAVE,
		}},
	})
	mgr.AddPTPConfig(types.ConfigName(staleMetricChronyd), &ptp4lconf.PTP4lConfig{
		Name:    staleMetricChronyd,
		Profile: staleMetricProfile,
	})
	mgr.PtpConfigMapUpdates.PtpProcessOpts = map[string]*ptpConfig.PtpProcessOpts{
		staleMetricProfile: {
			Phc2Opts:    phcOpts,
			ChronydOpts: chronydOpts,
		},
	}
	return mgr
}

func TestClearStaleClockRealTimeMetric_NoOpWithoutFailoverPair(t *testing.T) {
	phc := staleMetricPhcOpts
	mgr := newStaleMetricManager(t, &phc, nil) // chronyd disabled

	UpdateSyncStateMetrics(phc2sysProcessName, ClockRealTime, ptp.LOCKED)
	UpdateSyncStateMetrics(chronydProcessName, ClockRealTime, ptp.LOCKED)
	assert.True(t, syncStateExists(phc2sysProcessName))
	assert.True(t, syncStateExists(chronydProcessName))

	mgr.clearStaleClockRealTimeMetric(staleMetricProfile, chronydProcessName)
	assert.True(t, syncStateExists(phc2sysProcessName), "must not delete when chronyd is not enabled")
	assert.True(t, syncStateExists(chronydProcessName))

	mgr.clearStaleClockRealTimeMetric("missing-profile", chronydProcessName)
	assert.True(t, syncStateExists(phc2sysProcessName), "must not delete when profile opts are missing")

	DeleteSyncStateMetrics(phc2sysProcessName, ClockRealTime)
	DeleteSyncStateMetrics(chronydProcessName, ClockRealTime)
}

func TestClearStaleClockRealTimeMetric_SwapsOwner(t *testing.T) {
	phc := staleMetricPhcOpts
	chr := staleMetricChrOpts
	mgr := newStaleMetricManager(t, &phc, &chr)

	DeleteSyncStateMetrics(phc2sysProcessName, ClockRealTime)
	DeleteSyncStateMetrics(chronydProcessName, ClockRealTime)

	UpdateSyncStateMetrics(phc2sysProcessName, ClockRealTime, ptp.LOCKED)
	assert.True(t, syncStateExists(phc2sysProcessName))

	mgr.clearStaleClockRealTimeMetric(staleMetricProfile, chronydProcessName)
	assert.False(t, syncStateExists(phc2sysProcessName), "chronyd active must clear phc2sys series")

	UpdateSyncStateMetrics(chronydProcessName, ClockRealTime, ptp.LOCKED)
	assert.True(t, syncStateExists(chronydProcessName))

	mgr.clearStaleClockRealTimeMetric(staleMetricProfile, phc2sysProcessName)
	assert.False(t, syncStateExists(chronydProcessName), "phc2sys active must clear chronyd series")
}

func TestExtractMetrics_ClearsStaleClockRealTimeOnFailover(t *testing.T) {
	phc := staleMetricPhcOpts
	chr := staleMetricChrOpts
	mgr := newStaleMetricManager(t, &phc, &chr)

	DeleteSyncStateMetrics(phc2sysProcessName, ClockRealTime)
	DeleteSyncStateMetrics(chronydProcessName, ClockRealTime)
	UpdateSyncStateMetrics(phc2sysProcessName, ClockRealTime, ptp.LOCKED)

	mgr.ExtractMetrics(fmt.Sprintf(
		"chronyd[1000.000]: [%s] Selected source 192.168.1.1 (ntp.example.com)", staleMetricChronyd))
	assert.True(t, syncStateExists(chronydProcessName))
	assert.False(t, syncStateExists(phc2sysProcessName),
		"Selected source must clear stale phc2sys CLOCK_REALTIME series")

	mgr.ExtractMetrics(fmt.Sprintf(
		"phc2sys[1001.000]: [%s] CLOCK_REALTIME phc offset       -10 s2 freq  -100 delay   100", staleMetricPtp4l))
	assert.True(t, syncStateExists(phc2sysProcessName))
	assert.False(t, syncStateExists(chronydProcessName),
		"phc2sys CLOCK_REALTIME offset must clear stale chronyd series")

	UpdateSyncStateMetrics(phc2sysProcessName, ClockRealTime, ptp.LOCKED)
	mgr.ExtractMetrics(fmt.Sprintf(
		"chronyd[1002.000]: [%s] no selectable sources", staleMetricChronyd))
	assert.True(t, syncStateExists(chronydProcessName))
	assert.False(t, syncStateExists(phc2sysProcessName),
		"no selectable sources must also clear stale phc2sys series")
}
