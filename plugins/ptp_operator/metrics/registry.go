package metrics

import (
	"strconv"
	"sync"

	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/stats"

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
			Help:      "6 = Locked, 7 = PRC unlocked in-spec, 52/187 = PRC unlocked out-of-spec, 135 = T-BC holdover in-spec, 165 = T-BC holdover out-of-spec, 248 = Default, 255 = Slave Only Clock",
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
	// PTPHAMetrics metrics to show current ha profiles
	PTPHAMetrics = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: ptpNamespace,
			Subsystem: ptpSubsystem,
			Name:      "ha_profile_status",
			Help:      "0 = INACTIVE 1 = ACTIVE",
		}, []string{"process", "node", "profile"})

	// SynceClockQL  metrics to show current synce Clock Qulity
	SynceClockQL = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: ptpNamespace,
			Subsystem: ptpSubsystem,
			Name:      "synce_clock_quality",
			Help:      "network_option1: ePRTC = 32 PRTC = 34 PRC = 257  SSU-A = 259 SSU-B = 263 EEC1 = 266 QL-DNU = 270 network_option2: ePRTC = 34 PRTC = 33 PRS = 256 STU = 255 ST2 = 262 TNC = 259 ST3E =268 EEC2 = 265 PROV = 269 QL-DUS = 270",
		}, []string{"process", "node", "profile", "network_option", "iface", "device"})

	// SynceQLInfo metrics to show current QL values
	SynceQLInfo = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: ptpNamespace,
			Subsystem: ptpSubsystem,
			Name:      "synce_ssm_ql",
			Help: "network_option1: ePRTC: {0, 0x2, 0x21}, PRTC:  {1, 0x2, 0x20}, PRC:   {2, 0x2, 0xFF}, SSUA:  {3, 0x4, 0xFF}, SSUB:  {4, 0x8, 0xFF}, EEC1:  {5, 0xB, 0xFF}, QL-DNU: {6,0xF,0xFF} " +
				"   network_option2 ePRTC: {0, 0x1, 0x21}, PRTC:  {1, 0x1, 0x20}, PRS:   {2, 0x1, 0xFF}, STU:   {3, 0x0, 0xFF}, ST2:   {4, 0x7, 0xFF}, TNC:   {5, 0x4, 0xFF}, ST3E:  {6, 0xD, 0xFF}, EEC2:  {7, 0xA, 0xFF}, PROV:  {8, 0xE, 0xFF}, QL-DUS: {9,0xF,0xFF}",
		}, []string{"process", "node", "profile", "network_option", "iface", "device", "ql_type"})
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
		prometheus.MustRegister(PTPHAMetrics)
		prometheus.MustRegister(SynceQLInfo)
		prometheus.MustRegister(SynceClockQL)

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
		clockState = float64(types.LOCKED)
	} else if state == ptp.FREERUN {
		clockState = float64(types.FREERUN)
	} else if state == ptp.HOLDOVER {
		clockState = float64(types.HOLDOVER)
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

// UpdatePTPHaMetrics ... update ptp ha  status metrics
func UpdatePTPHaMetrics(profile string, status int64) {
	PTPHAMetrics.With(prometheus.Labels{
		"process": phc2sysProcessName, "node": ptpNodeName, "profile": profile}).Set(float64(status))
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

// DeletePTPHAMetrics ... delete ptp ha metrics
func DeletePTPHAMetrics(profile string) {
	PTPHAMetrics.Delete(prometheus.Labels{
		"process": phc2sysProcessName, "node": ptpNodeName, "profile": profile})
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

// UpdateSyncEClockQlMetrics ... update synce clock quality metrics
func UpdateSyncEClockQlMetrics(process, cfgName string, iface string, networkOption int, device string, value float64) {
	SynceClockQL.With(prometheus.Labels{
		"process": process, "node": ptpNodeName, "profile": cfgName, "network_option": strconv.Itoa(networkOption), "iface": iface, "device": device}).Set(value)
}

// UpdateSyncEQLMetrics ... update QL metrics
func UpdateSyncEQLMetrics(process, cfgName string, iface string, networkOption int, device string, qlType string, value byte) {
	SynceQLInfo.With(prometheus.Labels{
		"process": process, "node": ptpNodeName, "profile": cfgName, "iface": iface,
		"network_option": strconv.Itoa(networkOption), "device": device, "ql_type": qlType}).Set(float64(value))
}

// DeleteSyncEMetrics ... delete synce metrics
func DeleteSyncEMetrics(process, configName string, synceStats stats.SyncEStats) {
	for iface := range synceStats.Port {
		SynceQLInfo.Delete(prometheus.Labels{
			"process": process, "node": ptpNodeName, "profile": configName, "iface": iface, "device": synceStats.Name, "network_option": strconv.Itoa(synceStats.NetworkOption), "ql_type": "SSM"})
		SynceQLInfo.Delete(prometheus.Labels{
			"process": process, "node": ptpNodeName, "profile": configName, "iface": iface, "device": synceStats.Name, "network_option": strconv.Itoa(synceStats.NetworkOption), "ql_type": "Extended SSM"})

		SynceClockQL.Delete(prometheus.Labels{
			"process": process, "node": ptpNodeName, "profile": configName, "iface": iface, "device": synceStats.Name, "network_option": strconv.Itoa(synceStats.NetworkOption)})

		SyncState.Delete(prometheus.Labels{
			"process": process, "node": ptpNodeName, "iface": iface})
	}
}
