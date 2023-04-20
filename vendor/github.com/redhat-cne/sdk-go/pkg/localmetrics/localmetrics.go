// Copyright 2020 The Cloud Native Events Authors
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

package localmetrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

// MetricStatus metrics status
type MetricStatus string

const (
	// ACTIVE ...
	ACTIVE MetricStatus = "active"
	// SUCCESS ...
	SUCCESS MetricStatus = "success"
	// FAILED ...
	FAILED MetricStatus = "failed"
	// CONNECTION_RESET ...
	CONNECTION_RESET MetricStatus = "connection reset"
)

var (

	//transportEventReceivedCount ...  Total no of events received by the transport
	transportEventReceivedCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "cne_transport_events_received",
			Help: "Metric to get number of events received by the transport",
		}, []string{"address", "status"})
	//transportEventPublishedCount ...  Total no of events published by the transport
	transportEventPublishedCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "cne_transport_events_published",
			Help: "Metric to get number of events published by the transport",
		}, []string{"address", "status"})

	//transportConnectionResetCount ...  Total no of connection resets
	transportConnectionResetCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "cne_transport_connection_reset",
			Help: "Metric to get number of connection resets",
		}, []string{})

	//transportSenderCount ...  Total no of events published by the transport
	transportSenderCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "cne_transport_sender",
			Help: "Metric to get number of sender created",
		}, []string{"address", "status"})

	//transportReceiverCount ...  Total no of events published by the transport
	transportReceiverCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "cne_transport_receiver",
			Help: "Metric to get number of receiver created",
		}, []string{"address", "status"})

	//transportStatusCheckCount ...  Total no of status check received by the transport
	transportStatusCheckCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "cne_transport_status_check_published",
			Help: "Metric to get number of status check published by the transport",
		}, []string{"address", "status"})
)

// RegisterMetrics ...
func RegisterMetrics() {
	prometheus.MustRegister(transportEventReceivedCount)
	prometheus.MustRegister(transportEventPublishedCount)
	prometheus.MustRegister(transportConnectionResetCount)
	prometheus.MustRegister(transportSenderCount)
	prometheus.MustRegister(transportReceiverCount)
	prometheus.MustRegister(transportStatusCheckCount)
}

// UpdateTransportConnectionResetCount ...
func UpdateTransportConnectionResetCount(val int) {
	transportConnectionResetCount.With(prometheus.Labels{}).Add(float64(val))
}

// UpdateEventReceivedCount ...
func UpdateEventReceivedCount(address string, status MetricStatus, val int) {
	transportEventReceivedCount.With(
		prometheus.Labels{"address": address, "status": string(status)}).Add(float64(val))
}

// UpdateEventCreatedCount ...
func UpdateEventCreatedCount(address string, status MetricStatus, val int) {
	transportEventPublishedCount.With(
		prometheus.Labels{"address": address, "status": string(status)}).Add(float64(val))
}

// UpdateStatusCheckCount ...
func UpdateStatusCheckCount(address string, status MetricStatus, val int) {
	transportEventPublishedCount.With(
		prometheus.Labels{"address": address, "status": string(status)}).Add(float64(val))
}

// UpdateSenderCreatedCount ...
func UpdateSenderCreatedCount(address string, status MetricStatus, val int) {
	transportSenderCount.With(
		prometheus.Labels{"address": address, "status": string(status)}).Add(float64(val))
}

// UpdateReceiverCreatedCount ...
func UpdateReceiverCreatedCount(address string, status MetricStatus, val int) {
	transportReceiverCount.With(
		prometheus.Labels{"address": address, "status": string(status)}).Add(float64(val))
}
