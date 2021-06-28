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
	//CNEStatusCheckReceivedCount ...  Total no of events received
	cneStatusCheckReceivedCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "cne_status_check_received",
			Help: "Metric to get number of status check received",
		}, []string{"type", "status"})
)

//RegisterMetrics ... register metrics collector with Prometheus
func RegisterMetrics() {
	prometheus.MustRegister(cneEventAckCount)
	prometheus.MustRegister(cneEventReceivedCount)
	prometheus.MustRegister(cneStatusCheckReceivedCount)
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

// UpdateStatusAckCount  ...
func UpdateStatusAckCount(pubType string, status MetricStatus) {
	cneStatusCheckReceivedCount.With(
		prometheus.Labels{"type": pubType, "status": string(status)}).Inc()
}

