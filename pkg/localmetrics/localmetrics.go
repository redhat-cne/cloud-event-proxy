package localmetrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

// MetricStatus  ...
type MetricStatus string

const (
	// SUCCESS ...
	SUCCESS MetricStatus = "success"
	// FAILED ...
	FAILED MetricStatus = "failed"
)

var (
	//cneEventAckCount ...  Total no of events created
	cneEventAckCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "cne_events_ack",
			Help: "Metric to get number of events produced",
		}, []string{"type", "status"})
	//CNEEventReceivedCount ...  Total no of events received
	cneEventReceivedCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "cne_events_received",
			Help: "Metric to get number of events received",
		}, []string{"type", "status"})
)

//RegisterMetrics ... register metrics collector with Prometheus
func RegisterMetrics() {
	prometheus.MustRegister(cneEventAckCount)
	prometheus.MustRegister(cneEventReceivedCount)
}

// UpdateEventReceivedCount ...
func UpdateEventReceivedCount(subType string, status MetricStatus) {
	cneEventReceivedCount.With(
		prometheus.Labels{"type": subType, "status": string(status)}).Inc()
}

// UpdateEventAckCount  ...
func UpdateEventAckCount(pubType string, status MetricStatus) {
	cneEventAckCount.With(
		prometheus.Labels{"type": pubType, "status": string(status)}).Inc()
}
