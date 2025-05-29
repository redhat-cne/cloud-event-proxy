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
	// FAILCREATE ... failed to create subscription or publisher
	FAILCREATE MetricStatus = "failed to create"
	// FAILDELETE ... failed to delete subscription or publisher
	FAILDELETE MetricStatus = "failed to delete"
	// ACTIVE ...  active publishers and subscriptions
	ACTIVE MetricStatus = "active"
	// SUCCESS .... success events published
	SUCCESS MetricStatus = "success"
	// FAIL .... failed events published
	FAIL MetricStatus = "fail"
)

var (
	//eventPublishedCount ...  Total no of events published by the api
	eventPublishedCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "cne_api_events_published",
			Help: "Metric to get number of events published by the rest api",
		}, []string{"address", "status"})

	//subscriptionCount ...  Total no of connection resets
	subscriptionCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "cne_api_subscriptions",
			Help: "Metric to get number of subscriptions",
		}, []string{"status"})

	//publisherCount ...  Total no of events published by the transport
	publisherCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "cne_api_publishers",
			Help: "Metric to get number of publishers",
		}, []string{"status"})

	//statusCallCount ...  Total no of status check was made
	statusCallCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "cne_api_status_ping",
			Help: "Metric to get number of status call",
		}, []string{"status"})
)

// RegisterMetrics ... register metrics
func RegisterMetrics() {
	prometheus.MustRegister(eventPublishedCount)
	prometheus.MustRegister(subscriptionCount)
	prometheus.MustRegister(publisherCount)
	prometheus.MustRegister(statusCallCount)
}

// UpdateEventPublishedCount ...
func UpdateEventPublishedCount(address string, status MetricStatus, val int) {
	eventPublishedCount.With(
		prometheus.Labels{"address": address, "status": string(status)}).Add(float64(val))
}

// UpdateSubscriptionCount ...
func UpdateSubscriptionCount(status MetricStatus, val int) {
	subscriptionCount.With(
		prometheus.Labels{"status": string(status)}).Add(float64(val))
}

// UpdatePublisherCount ...
func UpdatePublisherCount(status MetricStatus, val int) {
	publisherCount.With(
		prometheus.Labels{"status": string(status)}).Add(float64(val))
}

// UpdateStatusCount ...
func UpdateStatusCount(_ string, status MetricStatus, val int) {
	statusCallCount.With(
		prometheus.Labels{"status": string(status)}).Add(float64(val))
}
