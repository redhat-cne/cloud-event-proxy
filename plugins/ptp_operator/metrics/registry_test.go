package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
)

func clockClassLabels(config string) prometheus.Labels {
	return prometheus.Labels{
		"process": ptp4lProcessName,
		"config":  config,
		"node":    ptpNodeName,
	}
}

func setClockClass(config string, value float64) {
	ClockClassMetrics.With(clockClassLabels(config)).Set(value)
}

func clockClassValue(t *testing.T, config string) float64 {
	t.Helper()
	return testutil.ToFloat64(ClockClassMetrics.With(clockClassLabels(config)))
}

func assertClockClassAbsent(t *testing.T, config string) {
	t.Helper()
	assert.False(t, ClockClassMetrics.Delete(clockClassLabels(config)),
		"clock class metric still present for config %s", config)
}

func TestDeleteClockClassMetricsForConfig(t *testing.T) {
	ensureTestNode(t)

	t.Run("single_delete", func(t *testing.T) {
		config := "ptp4l.0.config"
		setClockClass(config, 6)

		DeleteClockClassMetricsForConfig(ptp4lProcessName, config)

		assertClockClassAbsent(t, config)
	})

	t.Run("partial_delete_preserves_other_config", func(t *testing.T) {
		config0 := "ptp4l.0.config"
		config1 := "ptp4l.1.config"
		setClockClass(config0, 6)
		setClockClass(config1, 7)

		DeleteClockClassMetricsForConfig(ptp4lProcessName, config0)

		assertClockClassAbsent(t, config0)
		assert.Equal(t, 7.0, clockClassValue(t, config1))

		DeleteClockClassMetricsForConfig(ptp4lProcessName, config1)
	})

	t.Run("idempotent_double_delete", func(t *testing.T) {
		config := "ptp4l.2.config"
		setClockClass(config, 248)

		DeleteClockClassMetricsForConfig(ptp4lProcessName, config)
		DeleteClockClassMetricsForConfig(ptp4lProcessName, config)

		assertClockClassAbsent(t, config)
	})

	t.Run("delete_then_recreate_same_config", func(t *testing.T) {
		config := "ptp4l.3.config"
		setClockClass(config, 6)

		DeleteClockClassMetricsForConfig(ptp4lProcessName, config)
		assertClockClassAbsent(t, config)

		setClockClass(config, 248)
		assert.Equal(t, 248.0, clockClassValue(t, config))

		DeleteClockClassMetricsForConfig(ptp4lProcessName, config)
		assertClockClassAbsent(t, config)
	})

	t.Run("delete_all_for_node", func(t *testing.T) {
		config0 := "ptp4l.4.config"
		config1 := "ptp4l.5.config"
		setClockClass(config0, 6)
		setClockClass(config1, 7)

		DeleteAllClockClassMetricsForNode()

		assertClockClassAbsent(t, config0)
		assertClockClassAbsent(t, config1)
	})
}
