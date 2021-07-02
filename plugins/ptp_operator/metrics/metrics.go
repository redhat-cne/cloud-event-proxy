package metrics

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redhat-cne/sdk-go/pkg/channel"

	"github.com/prometheus/client_golang/prometheus/collectors"

	"github.com/redhat-cne/cloud-event-proxy/pkg/common"

	"github.com/golang/glog"
	"github.com/prometheus/client_golang/prometheus"
	ceevent "github.com/redhat-cne/sdk-go/pkg/event"
	log "github.com/sirupsen/logrus"
)

const (
	ptpNamespace                     = "openshift"
	ptpSubsystem                     = "ptp"
	phc2sysProcessName               = "phc2sys"
	ptp4lProcessName                 = "ptp4l"
	unLocked                         = "s0"
	clockStep                        = "s1"
	locked                           = "s2"
	maxOffsetThreshold int64         = 100 //in nano secs
	minOffsetThreshold int64         = -100
	holdoverTimeout    time.Duration = 5
)

var (
	// NodeName from the env
	ptpNodeName = ""

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

	// ClockState metrics to show current clock state
	ClockState = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: ptpNamespace,
			Subsystem: ptpSubsystem,
			Name:      "clock_state",
			Help:      "0 = FREERUN, 1 = LOCKED, 2 = HOLDOVER",
		}, []string{"process", "node", "iface"})
)

var registerMetrics sync.Once

// RegisterMetrics ... register metrics for all side car plugins
func RegisterMetrics(nodeName string) {
	registerMetrics.Do(func() {
		prometheus.MustRegister(OffsetFromMaster)
		prometheus.MustRegister(MaxOffsetFromMaster)
		prometheus.MustRegister(FrequencyAdjustment)
		prometheus.MustRegister(DelayFromMaster)
		prometheus.MustRegister(ClockState)

		// Including these stats kills performance when Prometheus polls with multiple targets
		prometheus.Unregister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
		prometheus.Unregister(collectors.NewGoCollector())

		ptpNodeName = nodeName
	})
}

// Stats calculates stats  nolint:unused
type Stats struct {
	num                 int64
	max                 int64
	min                 int64
	mean                int64
	sumSqr              int64
	sumDiffSqr          int64
	frequencyAdjustment int64
	delayFromMaster     int64
	lastOffset          int64 //nolint:structcheck
	lastClockState      ceevent.SyncState
	lastClockStateCount int64
}

func (s *Stats) addValue(val int64) {

	oldMean := s.mean

	if s.max < val {
		s.max = val
	}
	if s.num == 0 || s.min > val {
		s.min = val
	}
	s.num++
	s.mean = oldMean + (val-oldMean)/s.num
	s.sumSqr += val * val
	s.sumDiffSqr += (val - oldMean) * (val - s.mean)

}

// get stdDev
func (s *Stats) getStdev() float64 { //nolint:unused
	if s.num > 0 {
		return math.Sqrt(float64(s.sumDiffSqr / s.num))
	}
	return 1
}

func (s *Stats) getMaxAbs() int64 {
	if s.max > s.min {
		return s.max
	}
	return s.min

}

// Offset return last known offset
func (s *Stats) Offset() int64 {
	return s.lastOffset
}

// ClockState return last known clock state
func (s *Stats) ClockState() ceevent.SyncState {
	return s.lastClockState
}

// reset status
func (s *Stats) reset() { //nolint:unused
	s.num = 0
	s.max = 0
	s.mean = 0
	s.min = 0
	s.sumDiffSqr = 0
	s.sumSqr = 0
}

// PTPEventManager for PTP
type PTPEventManager struct {
	publisherID        string
	nodeName           string
	scConfig           *common.SCConfiguration
	lock               sync.RWMutex
	Stats              map[string]*Stats
	mock               bool
	holdoverTimeout    time.Duration
	maxOffsetThreshold int64
	minOffsetThreshold int64
}

// NewPTPEventManager to manage events and metrics
func NewPTPEventManager(publisherID string, nodeName string, config *common.SCConfiguration) (ptpEventManager *PTPEventManager) {
	ptpEventManager = &PTPEventManager{
		publisherID:        publisherID,
		nodeName:           nodeName,
		scConfig:           config,
		lock:               sync.RWMutex{},
		Stats:              make(map[string]*Stats),
		mock:               false,
		holdoverTimeout:    holdoverTimeout,
		maxOffsetThreshold: maxOffsetThreshold,
		minOffsetThreshold: minOffsetThreshold,
	}
	if ht := common.GetIntEnv("ptp_holdover_timeout"); ht != 0 {
		ptpEventManager.holdoverTimeout = time.Duration(ht)
	}
	if maxTh := common.GetIntEnv("ptp_maxoffset_threshold"); maxTh != 0 {
		ptpEventManager.maxOffsetThreshold = int64(maxTh)
	}
	if minTh := common.GetIntEnv("ptp_minoffset_threshold"); minTh != 0 {
		ptpEventManager.minOffsetThreshold = int64(minTh)
	}

	log.Infof("ptp event configured with "+
		"HoldoverStateThreshold: %d,"+
		"maxOffsetThreshold:%d,"+
		"minOffsetThreshold %d",
		ptpEventManager.holdoverTimeout,
		ptpEventManager.maxOffsetThreshold,
		ptpEventManager.minOffsetThreshold)

	return
}

// HoldOverStateThreshold .. use for test only
func (p *PTPEventManager) HoldOverStateThreshold(h time.Duration) {
	p.holdoverTimeout = h
}

// MaxOffsetThreshold .. offset threshold
func (p *PTPEventManager) MaxOffsetThreshold(m int64) {
	p.maxOffsetThreshold = m
}

// MinOffsetThreshold .. offset threshold
func (p *PTPEventManager) MinOffsetThreshold(m int64) {
	p.minOffsetThreshold = m
}

// MockTest .. use for test only
func (p *PTPEventManager) MockTest(t bool) {
	p.mock = t
}

// ExtractMetrics ...
func (p *PTPEventManager) ExtractMetrics(msg string) {
	defer func() {
		if err := recover(); err != nil {
			log.Errorf("restored from extract metrics and events: %s", err)
			log.Errorf("message  failed to send %s", msg)
		}
	}()
	replacer := strings.NewReplacer("[", " ", "]", " ", ":", " ")
	output := replacer.Replace(msg)
	fields := strings.Fields(output)
	if len(fields) < 3 {
		glog.Errorf("ignoring log:log is not in required format ptp4l/phc2sys[time]: [interface]")
		return
	}
	processName := fields[0]
	iface := fields[2]
	if strings.Contains(output, " max ") { // this get generated in case -u is passed an option to phy2sys opts
		offsetFromMaster, maxOffsetFromMaster, frequencyAdjustment, delayFromMaster := extractSummaryMetrics(processName, output)
		UpdatePTPMetrics(processName, iface, offsetFromMaster, maxOffsetFromMaster, frequencyAdjustment, delayFromMaster, "")
	} else if strings.Contains(output, " offset ") {
		offsetFromMaster, maxOffsetFromMaster, frequencyAdjustment, delayFromMaster, clockState := extractRegularMetrics(processName, output)
		// update stats
		if clockState == "" {
			return
		}
		var s *Stats
		var found bool
		if s, found = p.Stats[iface]; !found {
			s = &Stats{}
			p.Stats[iface] = s
		}
		s.frequencyAdjustment = int64(frequencyAdjustment)
		s.delayFromMaster = int64(delayFromMaster)
		s.lastOffset = int64(offsetFromMaster)

		if phc2sysProcessName == processName {
			p.GenPhc2SysEvent(iface, s.lastOffset, clockState)
			UpdatePTPMetrics(processName, iface, offsetFromMaster, float64(s.getMaxAbs()), frequencyAdjustment, delayFromMaster,
				p.Stats[iface].lastClockState)
		} else if ptp4lProcessName == processName {
			UpdatePTPMetrics(processName, iface, offsetFromMaster, maxOffsetFromMaster, frequencyAdjustment, delayFromMaster,
				p.Stats[iface].lastClockState)
		}
	} else if ptp4lProcessName == processName {
		clockState := extractPTP4lEventState(output)
		if clockState == "" {
			return
		}
		var s *Stats
		var found bool
		if s, found = p.Stats[iface]; !found {
			p.lock.Lock()
			s = &Stats{}
			p.Stats[iface] = s
			p.lock.Unlock()
		}
		lastLockState := s.lastClockState
		s.lastClockState = clockState
		if clockState != lastLockState {
			p.PublishEvent(clockState, 0, iface, channel.PTPEvent)
			updateClockStateMetrics(processName, iface, clockState)
			if clockState == ceevent.HOLDOVER {
				go func(ptpManager *PTPEventManager, iface string) {
					time.AfterFunc(ptpManager.holdoverTimeout*time.Second, func() {
						s := ptpManager.Stats[iface]
						if s.lastClockState == ceevent.HOLDOVER {
							log.Infof("HOLDOVER timeout after %d secs,setting clock state to FREERUN from HOLDOVER state for %s",
								ptpManager.holdoverTimeout, iface)
							s.lastClockState = ceevent.FREERUN
							s.reset()
							ptpManager.PublishEvent(ceevent.FREERUN, 0, iface, channel.PTPEvent)
							updateClockStateMetrics(processName, iface, ceevent.FREERUN)
						}
					})
				}(p, iface)
			}
		}
	}
}

// GenPhc2SysEvent ... generate events form the logs
func (p *PTPEventManager) GenPhc2SysEvent(iface string, offsetFromMaster int64, clockState ceevent.SyncState) {
	iStats := p.Stats[iface]
	lastClockState := iStats.lastClockState

	switch clockState {
	case ceevent.LOCKED:
		switch lastClockState {
		case ceevent.FREERUN: //last state was already sent for FreeRUN , but if its within then send again with new state
			if offsetFromMaster < p.maxOffsetThreshold && offsetFromMaster > p.minOffsetThreshold { // within range
				p.PublishEvent(clockState, offsetFromMaster, iface, channel.PTPEvent) // change to locked
				iStats.lastClockState = clockState
				iStats.lastClockStateCount = 1
				p.Stats[iface].addValue(int64(offsetFromMaster)) // update off set when its in locked state and hold over only
			}
		case ceevent.LOCKED: // last state was in sync , check if it is out of sync now
			if offsetFromMaster > p.maxOffsetThreshold || offsetFromMaster < p.minOffsetThreshold { // out of sync
				p.PublishEvent(ceevent.FREERUN, offsetFromMaster, iface, channel.PTPEvent)
				iStats.lastClockState = ceevent.FREERUN
				iStats.lastClockStateCount = 1
				iStats.reset()
			} else {
				p.Stats[iface].addValue(int64(offsetFromMaster)) // update off set when its in locked state and hold over only
			}
		case ceevent.HOLDOVER: // last state was holdover, since it is in locked now check if it qualifies to be locked
			if offsetFromMaster > p.maxOffsetThreshold || offsetFromMaster < p.minOffsetThreshold { // out of sync
				p.PublishEvent(clockState, offsetFromMaster, iface, channel.PTPEvent) // change to freerun
				iStats.lastClockState = ceevent.FREERUN
				iStats.lastClockStateCount = 1
				iStats.reset()
			}
		default: // not yet used states
			p.PublishEvent(clockState, offsetFromMaster, iface, channel.PTPEvent) // change to locked
			iStats.lastClockState = clockState
			iStats.lastClockStateCount = 1
			//do nothing already send event
		}
	case ceevent.FREERUN:
		if offsetFromMaster < p.maxOffsetThreshold && offsetFromMaster > p.minOffsetThreshold { // within range
			p.PublishEvent(ceevent.LOCKED, offsetFromMaster, iface, channel.PTPEvent) // change to locked
			iStats.lastClockState = ceevent.LOCKED
			iStats.lastClockStateCount = 1
			p.Stats[iface].addValue(int64(offsetFromMaster))
		}
	default:
		p.PublishEvent(clockState, offsetFromMaster, iface, channel.PTPEvent) // change to locked
		iStats.lastClockState = clockState
		iStats.lastClockStateCount = 1
		p.Stats[iface].reset() // reset when its free run
	}
}

//PublishEvent ...publish events
func (p *PTPEventManager) PublishEvent(state ceevent.SyncState, offsetFromMaster int64, iface string, eventType channel.EventDataType) {
	// create an event
	data := ceevent.Data{
		Version: "v1",
		Values: []ceevent.DataValue{{
			Resource:  fmt.Sprintf("/cluster/%s/ptp/interface/%s", p.nodeName, iface),
			DataType:  ceevent.NOTIFICATION,
			ValueType: ceevent.ENUMERATION,
			Value:     state,
		}, {
			Resource:  fmt.Sprintf("/cluster/%s/ptp/interface/%s", p.nodeName, iface),
			DataType:  ceevent.METRIC,
			ValueType: ceevent.DECIMAL,
			Value:     float64(offsetFromMaster),
		},
		},
	}
	e, err := common.CreateEvent(p.publisherID, eventType.String(), data)
	if err != nil {
		log.Errorf("failed to create ptp event, %s", err)
		return
	}
	if !p.mock {
		if err = common.PublishEvent(p.scConfig, e); err != nil {
			log.Errorf("failed to publish ptp event %v, %s", e, err)
			return
		}
	}
}

// UpdatePTPMetrics ...
func UpdatePTPMetrics(process, iface string, offsetFromMaster, maxOffsetFromMaster, frequencyAdjustment, delayFromMaster float64, state ceevent.SyncState) {
	OffsetFromMaster.With(prometheus.Labels{
		"process": process, "node": ptpNodeName, "iface": iface}).Set(offsetFromMaster)

	MaxOffsetFromMaster.With(prometheus.Labels{
		"process": process, "node": ptpNodeName, "iface": iface}).Set(maxOffsetFromMaster)

	FrequencyAdjustment.With(prometheus.Labels{
		"process": process, "node": ptpNodeName, "iface": iface}).Set(frequencyAdjustment)

	DelayFromMaster.With(prometheus.Labels{
		"process": process, "node": ptpNodeName, "iface": iface}).Set(delayFromMaster)

	updateClockStateMetrics(process, iface, state)
}

// updateClockStateMetrics ...
func updateClockStateMetrics(process, iface string, state ceevent.SyncState) {
	if state == ceevent.LOCKED {
		ClockState.With(prometheus.Labels{
			"process": process, "node": ptpNodeName, "iface": iface}).Set(1)
	} else if state == ceevent.FREERUN {
		ClockState.With(prometheus.Labels{
			"process": process, "node": ptpNodeName, "iface": iface}).Set(0)
	} else if state == ceevent.HOLDOVER {
		ClockState.With(prometheus.Labels{
			"process": process, "node": ptpNodeName, "iface": iface}).Set(2)
	}
}

func extractSummaryMetrics(processName, output string) (offsetFromMaster, maxOffsetFromMaster, frequencyAdjustment, delayFromMaster float64) {
	// remove everything before the rms string
	// This makes the out to equals
	index := strings.Index(output, "rms")
	output = output[index:]
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
	index := strings.Index(output, "offset")
	output = output[index:]
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
		clockState = ceevent.HOLDOVER
	}

	return
}

// ExtractPTP4lEvent ... extract event form ptp4l logs
func extractPTP4lEventState(output string) (clockState ceevent.SyncState) {
	// This makes the out to equals
	//phc2sys[187248.740]:[ens5f0] CLOCK_REALTIME phc offset        12 s2 freq   +6879 delay    49
	// phc2sys[187248.740]:[ens5f1] CLOCK_REALTIME phc offset        12 s2 freq   +6879 delay    49
	//ptp4l[3535499.740]: [ens5f0] master offset         -6 s2 freq   -1879 path delay        88

	/*
		"INITIALIZING to LISTENING on INIT_COMPLETE"
		"LISTENING to UNCALIBRATED on RS_SLAVE"
		"UNCALIBRATED to SLAVE on MASTER_CLOCK_SELECTED"
		"SLAVE to FAULTY on FAULT_DETECTED"
		"LISTENING to FAULTY on FAULT_DETECTED"
		"FAULTY to LISTENING on INIT_COMPLETE"
		"FAULTY to SLAVE on INIT_COMPLETE"
		"SLAVE to UNCALIBRATED on SYNCHRONIZATION_FAULT"
	*/
	var rePTPStatus = regexp.MustCompile(`INITIALIZING|LISTENING|UNCALIBRATED|FAULTY|FAULT_DETECTED|SYNCHRONIZATION_FAULT`)

	if strings.Contains(output, "FAULT_DETECTED") || strings.Contains(output, "SYNCHRONIZATION_FAULT") {
		clockState = ceevent.HOLDOVER
	} else if rePTPStatus.MatchString(output) {
		clockState = ceevent.FREERUN
	}

	return
}
