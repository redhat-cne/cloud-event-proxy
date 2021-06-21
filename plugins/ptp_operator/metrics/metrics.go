package metrics

import (
	"github.com/golang/glog"
	"github.com/prometheus/client_golang/prometheus"
	"strconv"
	"strings"
	"sync"
)

const (
	ptpNamespace = "openshift"
	ptpSubsystem = "ptp"
	phc2sysProcessName          = "phc2sys"
)

var (
	// NodeName from the env
	NodeName = ""

	// OffsetFromMaster metrics for offset from the master
	OffsetFromMaster = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: ptpNamespace,
			Subsystem: ptpSubsystem,
			Name:      "offset_from_master",
			Help:      "",
		}, []string{"process", "node", "iface"})

	// MaxOffsetFromMaster  metrics for max offset from the master
	MaxOffsetFromMaster = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: ptpNamespace,
			Subsystem: ptpSubsystem,
			Name:      "max_offset_from_master",
			Help:      "",
		}, []string{"process", "node", "iface"})

	// FrequencyAdjustment metrics to show frequency adjustment
	FrequencyAdjustment = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: ptpNamespace,
			Subsystem: ptpSubsystem,
			Name:      "frequency_adjustment",
			Help:      "",
		}, []string{"process", "node", "iface"})

	// DelayFromMaster metrics to show delay from the master
	DelayFromMaster = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: ptpNamespace,
			Subsystem: ptpSubsystem,
			Name:      "delay_from_master",
			Help:      "",
		}, []string{"process", "node", "iface"})
)

var registerMetrics sync.Once

// Metrics for PTP
type Metrics struct {
	Name string `json:"name"`
	Iface string `json:"iface"`
	Output string `json:"output"`
}

// RegisterMetrics ... register metrics for all side car plugins
func RegisterMetrics(nodeName string) {
	registerMetrics.Do(func() {
		prometheus.MustRegister(OffsetFromMaster)
		prometheus.MustRegister(MaxOffsetFromMaster)
		prometheus.MustRegister(FrequencyAdjustment)
		prometheus.MustRegister(DelayFromMaster)

		// Including these stats kills performance when Prometheus polls with multiple targets
		prometheus.Unregister(prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}))
		prometheus.Unregister(prometheus.NewGoCollector())

		NodeName = nodeName
	})
}

// UpdatePTPMetrics ...
func UpdatePTPMetrics(process, iface string, offsetFromMaster, maxOffsetFromMaster, frequencyAdjustment, delayFromMaster float64) {
	OffsetFromMaster.With(prometheus.Labels{
		"process": process, "node": NodeName, "iface": iface}).Set(offsetFromMaster)

	MaxOffsetFromMaster.With(prometheus.Labels{
		"process": process, "node": NodeName, "iface": iface}).Set(maxOffsetFromMaster)

	FrequencyAdjustment.With(prometheus.Labels{
		"process": process, "node": NodeName, "iface": iface}).Set(frequencyAdjustment)

	DelayFromMaster.With(prometheus.Labels{
		"process": process, "node": NodeName, "iface": iface}).Set(delayFromMaster)
}

// ExtractMetrics ...
func ExtractMetrics(processName, iface, output string) {
	if strings.Contains(output, " max ") { // this get genrated in case -u is passed an option to phy2sys opts
		offsetFromMaster, maxOffsetFromMaster, frequencyAdjustment, delayFromMaster := extractSummaryMetrics(processName, output)
		UpdatePTPMetrics(processName, iface, offsetFromMaster, maxOffsetFromMaster, frequencyAdjustment, delayFromMaster)
	} else if strings.Contains(output, " offset ")  && phc2sysProcessName!=processName{ // dispatcher calls metrics for phy2sys
		offsetFromMaster, maxOffsetFromMaster, frequencyAdjustment, delayFromMaster := extractRegularMetrics(processName, output)
		UpdatePTPMetrics(processName, iface, offsetFromMaster, maxOffsetFromMaster, frequencyAdjustment, delayFromMaster)
	}
}

func extractSummaryMetrics(processName, output string) (offsetFromMaster, maxOffsetFromMaster, frequencyAdjustment, delayFromMaster float64) {
	// remove everything before the rms string
	// This makes the out to equals
	indx := strings.Index(output, "rms")
	output = output[indx:]
	fields := strings.Fields(output)

	if len(fields) < 5 {
		glog.Errorf("%s failed to parse output %s: unexpected number of fields", processName, output)
		return
	}

	offsetFromMaster, err := strconv.ParseFloat(fields[1], 64)
	if err != nil {
		glog.Errorf("%s failed to parse offset from master output %s error %v", processName, fields[1], err)
	}

	maxOffsetFromMaster, err = strconv.ParseFloat(fields[3], 64)
	if err != nil {
		glog.Errorf("%s failed to parse max offset from master output %s error %v", processName, fields[3], err)
	}

	frequencyAdjustment, err = strconv.ParseFloat(fields[5], 64)
	if err != nil {
		glog.Errorf("%s failed to parse frequency adjustment output %s error %v", processName, fields[5], err)
	}

	if len(fields) >= 10 {
		delayFromMaster, err = strconv.ParseFloat(fields[9], 64)
		if err != nil {
			glog.Errorf("%s failed to parse delay from master output %s error %v", processName, fields[9], err)
		}
	} else {
		// If there is no delay from master this mean we are out of sync
		glog.Warningf("no delay from master process %s out of sync", processName)
	}

	return
}


func extractRegularMetrics(processName, output string) (offsetFromMaster, maxOffsetFromMaster, frequencyAdjustment, delayFromMaster float64) {
	// remove everything before the rms string
	// This makes the out to equals
	output = strings.Replace(output, "path", "", 1)
	indx := strings.Index(output, "offset")
	output = output[indx:]
	fields := strings.Fields(output)

	if len(fields) < 5 {
		glog.Errorf("%s failed to parse output %s: unexpected number of fields", processName, output)
		return
	}

	offsetFromMaster, err := strconv.ParseFloat(fields[1], 64)
	if err != nil {
		glog.Errorf("%s failed to parse offset from master output %s error %v", processName, fields[1], err)
	}

	maxOffsetFromMaster, err = strconv.ParseFloat(fields[1], 64)
	if err != nil {
		glog.Errorf("%s failed to parse max offset from master output %s error %v", processName, fields[1], err)
	}

	frequencyAdjustment, err = strconv.ParseFloat(fields[4], 64)
	if err != nil {
		glog.Errorf("%s failed to parse frequency adjustment output %s error %v", processName, fields[4], err)
	}

	if len(fields) >= 7 {
		delayFromMaster, err = strconv.ParseFloat(fields[6], 64)
		if err != nil {
			glog.Errorf("%s failed to parse delay from master output %s error %v", processName, fields[6], err)
		}
	} else {
		// If there is no delay from master this mean we are out of sync
		glog.Warningf("no delay from master process %s out of sync", processName)
	}

	return
}

