package metrics

import (
	"sync"

	log "github.com/sirupsen/logrus"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/types"
	"github.com/redhat-cne/sdk-go/pkg/event/ptp"
)

var (
	// PtpOffset metrics for offset
	PtpOffset = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: ptpNamespace,
			Subsystem: ptpSubsystem,
			Name:      "offset_ns",
			Help:      "",
		}, []string{"from", "process", "node", "iface"})

	// PtpMaxOffset  metrics for max offset
	PtpMaxOffset = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: ptpNamespace,
			Subsystem: ptpSubsystem,
			Name:      "max_offset_ns",
			Help:      "",
		}, []string{"from", "process", "node", "iface"})

	// PtpFrequencyAdjustment metrics to show frequency adjustment
	PtpFrequencyAdjustment = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: ptpNamespace,
			Subsystem: ptpSubsystem,
			Name:      "frequency_adjustment_ns",
			Help:      "",
		}, []string{"from", "process", "node", "iface"})

	// PtpDelay metrics to show delay
	PtpDelay = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: ptpNamespace,
			Subsystem: ptpSubsystem,
			Name:      "delay_ns",
			Help:      "",
		}, []string{"from", "process", "node", "iface"})

	// SyncState metrics to show current clock state
	SyncState = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: ptpNamespace,
			Subsystem: ptpSubsystem,
			Name:      "clock_state",
			Help:      "0 = FREERUN, 1 = LOCKED, 2 = HOLDOVER",
		}, []string{"process", "node", "iface"})

	// NmeaStatus metrics to show current nmea status
	NmeaStatus = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: ptpNamespace,
			Subsystem: ptpSubsystem,
			Name:      "nmea_status",
			Help:      "0 = UNAVAILABLE, 1 = AVAILABLE",
		}, []string{"process", "node", "iface"})

	// Threshold metrics to show current ptp threshold
	Threshold = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: ptpNamespace,
			Subsystem: ptpSubsystem,
			Name:      "threshold",
			Help:      "",
		}, []string{"threshold", "node", "profile"})

	// InterfaceRole metrics to show current interface role
	InterfaceRole = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: ptpNamespace,
			Subsystem: ptpSubsystem,
			Name:      "interface_role",
			Help:      "0 = PASSIVE, 1 = SLAVE, 2 = MASTER, 3 = FAULTY, 4 = UNKNOWN, 5 = LISTENING",
		}, []string{"process", "node", "iface"})

	// ClockClassMetrics metrics to show current clock class for the node
	ClockClassMetrics = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: ptpNamespace,
			Subsystem: ptpSubsystem,
			Name:      "clock_class",
			Help:      "6 = Locked, 7 = PRC unlocked in-spec, 52/187 = PRC unlocked out-of-spec, 248 = Default, 255 = Slave Only Clock",
		}, []string{"process", "node"})

	// ProcessStatus  ... update process status
	ProcessStatus = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: ptpNamespace,
			Subsystem: ptpSubsystem,
			Name:      "process_status",
			Help:      "0 = DOWN, 1 = UP",
		}, []string{"process", "node", "config"})

	// ProcessReStartCount update process status cound
	ProcessReStartCount = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: ptpNamespace,
			Subsystem: ptpSubsystem,
			Name:      "process_restart_count",
			Help:      "",
		}, []string{"process", "node", "config"})
)

var registerMetrics sync.Once

// RegisterMetrics ... register metrics for all side car plugins
func RegisterMetrics(nodeName string) {
	registerMetrics.Do(func() {
		prometheus.MustRegister(PtpOffset)
		prometheus.MustRegister(PtpMaxOffset)
		prometheus.MustRegister(PtpFrequencyAdjustment)
		prometheus.MustRegister(PtpDelay)
		prometheus.MustRegister(SyncState)
		prometheus.MustRegister(NmeaStatus)
		prometheus.MustRegister(Threshold)
		prometheus.MustRegister(InterfaceRole)
		prometheus.MustRegister(ClockClassMetrics)
		prometheus.MustRegister(ProcessStatus)
		prometheus.MustRegister(ProcessReStartCount)

		// Including these stats kills performance when Prometheus polls with multiple targets
		prometheus.Unregister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
		prometheus.Unregister(collectors.NewGoCollector())

		ptpNodeName = nodeName
	})
}

// UpdatePTPMetrics ... update ptp metrics
func UpdatePTPMetrics(metricsType, process, eventResourceName string, offset, maxOffset, frequencyAdjustment, delay float64) {
	PtpOffset.With(prometheus.Labels{"from": metricsType,
		"process": process, "node": ptpNodeName, "iface": eventResourceName}).Set(offset)

	PtpMaxOffset.With(prometheus.Labels{"from": metricsType,
		"process": process, "node": ptpNodeName, "iface": eventResourceName}).Set(maxOffset)

	PtpFrequencyAdjustment.With(prometheus.Labels{"from": metricsType,
		"process": process, "node": ptpNodeName, "iface": eventResourceName}).Set(frequencyAdjustment)

	PtpDelay.With(prometheus.Labels{"from": metricsType,
		"process": process, "node": ptpNodeName, "iface": eventResourceName}).Set(delay)
}

// UpdatePTPOffsetMetrics ... update ptp offset metrics
func UpdatePTPOffsetMetrics(metricsType, process, eventResourceName string, offset float64) {
	PtpOffset.With(prometheus.Labels{"from": metricsType,
		"process": process, "node": ptpNodeName, "iface": eventResourceName}).Set(offset)
}

// DeletedPTPMetrics ... update metrics for deleted ptp config
func DeletedPTPMetrics(clockType, processName, eventResourceName string) {
	PtpOffset.Delete(prometheus.Labels{"from": clockType,
		"process": processName, "node": ptpNodeName, "iface": eventResourceName})
	PtpMaxOffset.Delete(prometheus.Labels{"from": clockType,
		"process": processName, "node": ptpNodeName, "iface": eventResourceName})
	PtpFrequencyAdjustment.Delete(prometheus.Labels{"from": clockType,
		"process": processName, "node": ptpNodeName, "iface": eventResourceName})
	PtpDelay.Delete(prometheus.Labels{"from": clockType,
		"process": processName, "node": ptpNodeName, "iface": eventResourceName})
	SyncState.Delete(prometheus.Labels{
		"process": processName, "node": ptpNodeName, "iface": eventResourceName})
}

// DeleteThresholdMetrics ... delete threshold metrics
func DeleteThresholdMetrics(profile string) {
	Threshold.Delete(prometheus.Labels{
		"threshold": "MinOffsetThreshold", "node": ptpNodeName, "profile": profile})
	Threshold.Delete(prometheus.Labels{
		"threshold": "MaxOffsetThreshold", "node": ptpNodeName, "profile": profile})
	Threshold.Delete(prometheus.Labels{
		"threshold": "HoldOverTimeout", "node": ptpNodeName, "profile": profile})
}

// UpdateSyncStateMetrics ... update sync state metrics
func UpdateSyncStateMetrics(process, iface string, state ptp.SyncState) {
	var clockState float64
	if state == ptp.LOCKED {
		clockState = 1
	} else if state == ptp.FREERUN {
		clockState = 0
	} else if state == ptp.HOLDOVER {
		clockState = 2
	}
	// prevent reporting wrong metrics
	if iface == master && process == phc2sysProcessName {
		log.Errorf("wrong metrics are processed, ignoring interface %s with process %s", iface, process)
		return
	}
	SyncState.With(prometheus.Labels{
		"process": process, "node": ptpNodeName, "iface": iface}).Set(clockState)
}

// UpdateNmeaStatusMetrics ... update nmea status metrics
func UpdateNmeaStatusMetrics(process, iface string, status float64) {
	NmeaStatus.With(prometheus.Labels{
		"process": process, "node": ptpNodeName, "iface": iface}).Set(status)
}

// UpdateInterfaceRoleMetrics ... update interface role metrics
func UpdateInterfaceRoleMetrics(process, ptpInterface string, role types.PtpPortRole) {
	InterfaceRole.With(prometheus.Labels{
		"process": process, "node": ptpNodeName, "iface": ptpInterface}).Set(float64(role))
}

// DeleteInterfaceRoleMetrics ... delete interface role metrics
func DeleteInterfaceRoleMetrics(process, ptpInterface string) {
	if process != "" {
		InterfaceRole.Delete(prometheus.Labels{"process": process, "iface": ptpInterface, "node": ptpNodeName})
	} else {
		InterfaceRole.Delete(prometheus.Labels{"iface": ptpInterface, "node": ptpNodeName})
	}
}

// UpdateProcessStatusMetrics  -- update process status metrics
func UpdateProcessStatusMetrics(process, cfgName string, status int64) {
	ProcessStatus.With(prometheus.Labels{
		"process": process, "node": ptpNodeName, "config": cfgName}).Set(float64(status))
	if status == PtpProcessUp {
		ProcessReStartCount.With(prometheus.Labels{
			"process": process, "node": ptpNodeName, "config": cfgName}).Inc()
	}
}

// DeleteProcessStatusMetricsForConfig ...
func DeleteProcessStatusMetricsForConfig(node string, cfgName string, process ...string) {
	labels := prometheus.Labels{}
	if node != "" {
		labels["node"] = node
	}
	if cfgName != "" {
		labels["config"] = cfgName
	}
	if len(process) == 0 {
		ProcessStatus.Delete(labels)
		ProcessReStartCount.Delete(labels)
	}
	for _, p := range process {
		if p != "" {
			labels["process"] = p
			ProcessStatus.Delete(labels)
			ProcessReStartCount.Delete(labels)
		}
	}
}
