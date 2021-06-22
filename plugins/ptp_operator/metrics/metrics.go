package metrics

import (
	"math"
	"strconv"
	"strings"
	"sync"

	"github.com/golang/glog"
	"github.com/prometheus/client_golang/prometheus"
	ceevent "github.com/redhat-cne/sdk-go/pkg/event"
	log "github.com/sirupsen/logrus"
)

const (
	ptpNamespace                = "openshift"
	ptpSubsystem                = "ptp"
	phc2sysProcessName          = "phc2sys"
	ptp4lProcessName            = "ptp4l"
	unLocked                    = "s0"
	clockStep                   = "s1"
	locked                      = "s2"
	maxOffsetThrushHold float64 = 100
	minOffsetThrushHold float64 = -100
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

// Stats calculates stats  nolint:unused
type Stats struct {
	num                 float64
	max                 float64
	min                 float64
	mean                float64
	sumSqr              float64
	sumDiffSqr          float64
	frequencyAdjustment float64
	delayFromMaster     float64
	lastOffset          float64 //nolint:structcheck
	lastClockState      ceevent.SyncState
}

func (s *Stats) addValue(val float64) {

	oldMean := s.mean

	if s.num == 0 || s.max < val {
		s.max = val
	}
	if s.num == 0 && s.min > val {
		s.mean = val
	}
	s.num++
	s.mean = oldMean + (val-oldMean)/s.num
	s.sumSqr += val * val
	s.sumDiffSqr += (val - oldMean) * (val - s.mean)

}

// get stdDev
func (s *Stats) getStdev() float64 { //nolint:unused
	if s.num > 0 {
		return math.Sqrt(s.sumDiffSqr / s.num)
	}
	return 1
}
func (s *Stats) getMaxAbs() float64 {
	if s.max > s.mean {
		return s.max
	}
	return s.min

}

// reset status
func (s *Stats) reset() { //nolint:unused
	s.num = 0
	s.max = 0
	s.mean = 0
	s.sumDiffSqr = 0
	s.sumSqr = 0
}

// Metric for PTP
type Metric struct {
	lock  sync.RWMutex
	Stats map[string]*Stats
}

// ExtractMetrics ...
func (m *Metric) ExtractMetrics(msg string) {
	replacer := strings.NewReplacer("[", " ", "]", " ", ":", " ")
	output := replacer.Replace(msg)
	fields := strings.Fields(output)
	if len(fields) < 2 {
		glog.Errorf("ignoring log:log  is not in required format processname[time]: [iface]")
		return
	}
	processName := fields[0]
	iface := fields[2]
	if strings.Contains(output, " max ") { // this get generated in case -u is passed an option to phy2sys opts
		offsetFromMaster, maxOffsetFromMaster, frequencyAdjustment, delayFromMaster := extractSummaryMetrics(processName, output)
		UpdatePTPMetrics(processName, iface, offsetFromMaster, maxOffsetFromMaster, frequencyAdjustment, delayFromMaster)
	} else if strings.Contains(output, " offset ") { // dispatcher calls metrics for phy2sys
		offsetFromMaster, maxOffsetFromMaster, frequencyAdjustment, delayFromMaster, clockState := extractRegularMetrics(processName, output)
		// update stats
		if clockState == "" {
			return
		}
		var s *Stats
		var found bool
		if s, found = m.Stats[iface]; !found {
			m.lock.Lock()
			s = &Stats{}
			m.Stats[iface] = s
			m.lock.Unlock()
		}
		s.frequencyAdjustment = frequencyAdjustment
		s.delayFromMaster = delayFromMaster
		s.lastOffset = offsetFromMaster

		s.addValue(offsetFromMaster)
		lastLockState := s.lastClockState
		s.lastClockState = clockState
		if phc2sysProcessName == processName {
			GenPhc2SysEvent(iface, offsetFromMaster, clockState, lastLockState)
			UpdatePTPMetrics(processName, iface, offsetFromMaster, s.getMaxAbs(), frequencyAdjustment, delayFromMaster)
		} else if ptp4lProcessName == processName {
			UpdatePTPMetrics(processName, iface, offsetFromMaster, maxOffsetFromMaster, frequencyAdjustment, delayFromMaster)
		}
	} else if ptp4lProcessName == processName {
		clockState := extractPTP4lEventState(processName, output)
		if clockState == "" {
			return
		}
		var s *Stats
		var found bool
		if s, found = m.Stats[iface]; !found {
			m.lock.Lock()
			s = &Stats{}
			m.Stats[iface] = s
			m.lock.Unlock()
		}

		lastLockState := s.lastClockState
		s.lastClockState = clockState
		if clockState != lastLockState {
			log.Infof("EVENT DATA FOR %s  IS %#v", iface, eventData(clockState))
		}
	}
}

var registerMetrics sync.Once

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

func extractRegularMetrics(processName, output string) (offsetFromMaster, maxOffsetFromMaster, frequencyAdjustment, delayFromMaster float64, clockState ceevent.SyncState) {
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

	state := fields[2]

	switch state {
	case unLocked:
		clockState = ceevent.FREERUN
	case clockStep:
		clockState = ceevent.FREERUN
	case locked:
		clockState = ceevent.LOCKED
	default:
		glog.Errorf("%s -  failed to parse clock state output `%s` ", processName, fields[2])
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

// ExtractPTP4lEvent ... extract event form ptp4l logs
func extractPTP4lEventState(processName, output string) (clockState ceevent.SyncState) {
	// This makes the out to equals
	//phc2sys[187248.740]:[ens5f0] CLOCK_REALTIME phc offset        12 s2 freq   +6879 delay    49
	// phc2sys[187248.740]:[ens5f1] CLOCK_REALTIME phc offset        12 s2 freq   +6879 delay    49
	//ptp4l[3535499.740]: [ens5f0] master offset         -6 s2 freq   -1879 path delay        88
	if strings.Contains(output, "INITIALIZING to LISTENING on INIT_COMPLETE") {
		clockState = ceevent.FREERUN
	} else if strings.Contains(output, "LISTENING to UNCALIBRATED on RS_SLAVE") {
		clockState = ceevent.FREERUN
	} else if strings.Contains(output, "UNCALIBRATED to SLAVE on MASTER_CLOCK_SELECTED") {
		clockState = ceevent.FREERUN
	} else if strings.Contains(output, "SLAVE to FAULTY on FAULT_DETECTED") {
		clockState = ceevent.FREERUN
	} else if strings.Contains(output, "LISTENING to FAULTY on FAULT_DETECTED") {
		clockState = ceevent.FREERUN
	} else if strings.Contains(output, "FAULTY to LISTENING on INIT_COMPLETE") {
		clockState = ceevent.FREERUN
	} else if strings.Contains(output, "FAULTY to SLAVE on INIT_COMPLETE") {
		clockState = ceevent.FREERUN
	} else if strings.Contains(output, "SLAVE to UNCALIBRATED on SYNCHRONIZATION_FAULT") {
		clockState = ceevent.HOLDOVER
	}
	return
}

// GenPhc2SysEvent ... generate events form the logs
func GenPhc2SysEvent(iface string, offsetFromMaster float64, clockState, lastClockState ceevent.SyncState) {
	//TODO: Decide from HOLDOVER  to FREERUN
	log.Infof("lastClockState: %s\n curent clock state: %s\n offsetFromMaster %f\n  maxOffsetThrushHold %f\n ", lastClockState, clockState, offsetFromMaster, maxOffsetThrushHold)
	if clockState == ceevent.LOCKED {
		if offsetFromMaster != 0 { //ptp4l will  return 0 offset
			if (offsetFromMaster > maxOffsetThrushHold || offsetFromMaster < minOffsetThrushHold) &&
				lastClockState != ceevent.HOLDOVER {
				log.Printf("EVENT DATA FOR %s IS %#v", iface, eventData(ceevent.HOLDOVER))
			} else if offsetFromMaster > maxOffsetThrushHold || offsetFromMaster < minOffsetThrushHold &&
				lastClockState != ceevent.FREERUN {
				log.Printf("EVENT DATA FOR %s  IS %#v", iface, eventData(ceevent.FREERUN))
			} else if lastClockState != clockState {
				log.Printf("EVENT DATA FOR %s  IS %#v", iface, eventData(clockState))
			}
		} else if clockState != lastClockState {
			log.Printf("EVENT DATA FOR %s  IS %#v", iface, eventData(clockState))
		}
	} else if clockState != lastClockState {
		log.Printf("EVENT DATA FOR %s  IS %#v", iface, eventData(clockState))
	}
}

func eventData(state ceevent.SyncState) ceevent.Data {
	// create an event
	data := ceevent.Data{
		Version: "v1",
		Values: []ceevent.DataValue{{
			Resource:  "/cluster/node/ptp",
			DataType:  ceevent.NOTIFICATION,
			ValueType: ceevent.ENUMERATION,
			Value:     state,
		},
		},
	}
	return data
}