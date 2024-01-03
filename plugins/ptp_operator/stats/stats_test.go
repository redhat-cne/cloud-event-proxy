package stats_test

import (
	"testing"

	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/event"
	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/metrics"
	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/stats"
	"github.com/redhat-cne/sdk-go/pkg/event/ptp"
	"github.com/stretchr/testify/assert"
	"k8s.io/utils/pointer"
)

func TestStats_SetPtpDependentEventState(t *testing.T) {
	s := stats.NewStats("testCfg")
	s.SetPtpDependentEventState(event.ClockState{
		State:   ptp.FREERUN,
		Offset:  pointer.Float64(0),
		Process: metrics.GNSS,
	}, nil, nil)
	assert.Equal(t, ptp.FREERUN, s.PtpDependentEventState().CurrentPTPStateEvent)
}
