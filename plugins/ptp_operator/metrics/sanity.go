package metrics

import (
	"runtime/debug"

	log "github.com/sirupsen/logrus"
)

// CheckMetricSanity checks if process and iface are populated before emitting metrics.
func CheckMetricSanity(metricName, process, iface string) bool {
	if process == "" || iface == "" {
		log.Warnf("Sanity check failed for metric '%s': process='%s', iface='%s'. Stack trace:\n%s", metricName, process, iface, string(debug.Stack()))
		return false
	}
	return true
}
