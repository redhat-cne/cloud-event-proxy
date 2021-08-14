package metrics

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/ptp4lconf"
	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/types"

	"github.com/redhat-cne/sdk-go/pkg/channel"

	"github.com/prometheus/client_golang/prometheus/collectors"

	"github.com/redhat-cne/cloud-event-proxy/pkg/common"

	"github.com/prometheus/client_golang/prometheus"
	ptpConfig "github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/config"
	ceevent "github.com/redhat-cne/sdk-go/pkg/event"
	log "github.com/sirupsen/logrus"
)

const (
	ptpNamespace       = "openshift"
	ptpSubsystem       = "ptp"
	phc2sysProcessName = "phc2sys"
	ptp4lProcessName   = "ptp4l"
	unLocked           = "s0"
	clockStep          = "s1"
	locked             = "s2"
	//FreeRunOffsetValue when sync state is FREERUN
	FreeRunOffsetValue = -9999999999999999
	//ClockRealTime is the slave
	ClockRealTime = "CLOCK_REALTIME"
	//MasterClockType is teh slave sync slave clock to master
	MasterClockType = "master"
)

var (
	ptpConfigFileRegEx = regexp.MustCompile(`ptp4l.[0-9]*.config`)
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

	// OffsetFromSystem metrics for offset from the system
	OffsetFromSystem = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: ptpNamespace,
			Subsystem: ptpSubsystem,
			Name:      "offset_from_system",
			Help:      "",
		}, []string{"process", "node", "iface"})

	// MaxOffsetFromSystem  metrics for max offset from the system
	MaxOffsetFromSystem = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: ptpNamespace,
			Subsystem: ptpSubsystem,
			Name:      "max_offset_from_system",
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

	// MasterSyncState metrics to show current clock state
	MasterSyncState = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: ptpNamespace,
			Subsystem: ptpSubsystem,
			Name:      "clock_state",
			Help:      "0 = FREERUN, 1 = LOCKED, 2 = HOLDOVER",
		}, []string{"process", "node", "iface"})

	// SystemSyncState metrics to show current clock state
	SystemSyncState = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: ptpNamespace,
			Subsystem: ptpSubsystem,
			Name:      "clock_state",
			Help:      "0 = FREERUN, 1 = LOCKED, 2 = HOLDOVER",
		}, []string{"process", "node", "iface"})

	// Threshold metrics to show current ptp threshold
	Threshold = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: ptpNamespace,
			Subsystem: ptpSubsystem,
			Name:      "ptp_threshold",
			Help:      "",
		}, []string{"threshold", "node", "iface"})

	// InterfaceRole metrics to show current interface role
	InterfaceRole = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: ptpNamespace,
			Subsystem: ptpSubsystem,
			Name:      "ptp_interface_role",
			Help:      "0 = PASSIVE 1 = SLAVE 2 = MASTER 3 = FAULTY",
		}, []string{"process", "node", "iface"})
)

var registerMetrics sync.Once

// RegisterMetrics ... register metrics for all side car plugins
func RegisterMetrics(nodeName string) {
	registerMetrics.Do(func() {
		prometheus.MustRegister(OffsetFromMaster)
		prometheus.MustRegister(MaxOffsetFromMaster)
		prometheus.MustRegister(OffsetFromSystem)
		prometheus.MustRegister(MaxOffsetFromSystem)
		prometheus.MustRegister(FrequencyAdjustment)
		prometheus.MustRegister(DelayFromMaster)
		prometheus.MustRegister(MasterSyncState)
		prometheus.MustRegister(Threshold)
		prometheus.MustRegister(InterfaceRole)

		// Including these stats kills performance when Prometheus polls with multiple targets
		prometheus.Unregister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
		prometheus.Unregister(collectors.NewGoCollector())

		ptpNodeName = nodeName
	})
}

//ProcessStats ...
type ProcessStats struct {
	Stats map[types.ProcessName]*Stats
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
	lastSyncState       ceevent.SyncState
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

// SyncState return last known SyncState state
func (s *Stats) SyncState() ceevent.SyncState {
	return s.lastSyncState
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

// NewStats ... create new stats
func NewStats() *Stats {
	return &Stats{}
}

// PTPEventManager for PTP
type PTPEventManager struct {
	publisherID           string
	nodeName              string
	scConfig              *common.SCConfiguration
	lock                  sync.RWMutex
	Stats                 map[types.IFace]*Stats
	mock                  bool
	PtpConfigMapUpdates   *ptpConfig.LinuxPTPConfigMapUpdate
	Ptp4lConfigInterfaces map[types.ConfigName]*ptp4lconf.PTP4lConfig
}

// NewPTPEventManager to manage events and metrics
func NewPTPEventManager(publisherID string, nodeName string, config *common.SCConfiguration) (ptpEventManager *PTPEventManager) {
	ptpEventManager = &PTPEventManager{
		publisherID:           publisherID,
		nodeName:              nodeName,
		scConfig:              config,
		lock:                  sync.RWMutex{},
		Stats:                 make(map[types.IFace]*Stats),
		Ptp4lConfigInterfaces: make(map[types.ConfigName]*ptp4lconf.PTP4lConfig),
		mock:                  false,
	}
	//attach ptp config updates
	ptpEventManager.PtpConfigMapUpdates = ptpConfig.NewLinuxPTPConfUpdate()
	return
}

// PtpThreshold ... return ptp threshold
func (p *PTPEventManager) PtpThreshold(iface string) ptpConfig.PtpClockThreshold {
	if t, found := p.PtpConfigMapUpdates.EventThreshold[iface]; found {
		return ptpConfig.PtpClockThreshold{
			HoldOverTimeout:    t.HoldOverTimeout,
			MaxOffsetThreshold: t.MaxOffsetThreshold,
			MinOffsetThreshold: t.MinOffsetThreshold,
		}
	}
	return ptpConfig.GetDefaultThreshold()
}

// MockTest .. use for test only
func (p *PTPEventManager) MockTest(t bool) {
	p.mock = t
}

// DeleteStats ... delete stats obj
func (p *PTPEventManager) DeleteStats(key types.IFace) {
	p.lock.Lock()
	delete(p.Stats, key)
	p.lock.Unlock()
}

// AddPTPConfig ... Add PtpConfigUpdate obj
func (p *PTPEventManager) AddPTPConfig(fileName types.ConfigName, ptpConfig *ptp4lconf.PTP4lConfig) {
	p.lock.Lock()
	p.Ptp4lConfigInterfaces[fileName] = ptpConfig
	p.lock.Unlock()
}

// GetPTPConfig ... Add PtpConfigUpdate obj
func (p *PTPEventManager) GetPTPConfig(configName types.ConfigName) *ptp4lconf.PTP4lConfig {

	if _, ok := p.Ptp4lConfigInterfaces[configName]; ok && p.Ptp4lConfigInterfaces[configName] != nil {
		return p.Ptp4lConfigInterfaces[configName]
	}
	pc := &ptp4lconf.PTP4lConfig{
		Name: string(configName),
	}
	pc.Interfaces = []*ptp4lconf.PTPInterface{}
	p.AddPTPConfig(configName, pc)
	return pc
}

// DeletePTPConfig ... delete ptp obj
func (p *PTPEventManager) DeletePTPConfig(key types.ConfigName) {
	p.lock.Lock()
	delete(p.Ptp4lConfigInterfaces, key)
	p.lock.Unlock()
}

// ExtractMetrics ...
func (p *PTPEventManager) ExtractMetrics(msg string) {
	defer func() {
		if err := recover(); err != nil {
			log.Errorf("restored from extract metrics and events: %s", err)
			log.Errorf("failed to extract %s", msg)
		}
	}()
	replacer := strings.NewReplacer("[", " ", "]", " ", ":", " ")
	output := replacer.Replace(msg)
	fields := strings.Fields(output)
	if len(fields) < 3 {
		log.Errorf("ignoring log:log is not in required format ptp4l/phc2sys[time]: [config] %s", output)
		return
	}
	processName := fields[0]
	processNameType := types.ProcessName(processName)
	configName := fields[2]
	ptp4lCfg := p.GetPTPConfig(types.ConfigName(configName))

	if ptp4lCfg == nil { //TODO: Use PMC to update port and roles
		log.Errorf("file watcher have not picked the files yet")
		return
	}
	var ptpInterface ptp4lconf.PTPInterface
	var err error
	if strings.Contains(output, " max ") { // this get generated in case -u is passed an option to phy2sys opts
		interfaceName, offsetFromMaster, maxOffsetFromMaster, frequencyAdjustment, delayFromMaster := extractSummaryMetrics(processName, output)
		switch interfaceName {
		case ClockRealTime:
			UpdatePTPMasterMetrics(processName, interfaceName, offsetFromMaster, maxOffsetFromMaster, frequencyAdjustment, delayFromMaster)
		case MasterClockType:
			UpdatePTPMasterMetrics(processName, interfaceName, offsetFromMaster, maxOffsetFromMaster, frequencyAdjustment, delayFromMaster)
		case "":
			//do nothing
		default:
			UpdatePTPSystemMetrics(processName, interfaceName, offsetFromMaster, maxOffsetFromMaster, frequencyAdjustment, delayFromMaster)
		}
	} else if strings.Contains(output, " offset ") {
		//ptp4l[5196819.100]: [ptp4l.0.config] master offset   -2162130 s2 freq +22451884 path delay    374976
		//phc2sys[4268818.286]: [ptp4l.0.config] CLOCK_REALTIME phc offset       -62 s0 freq  -78368 delay   1100
		//phc2sys[4268818.287]: [ptp4l.0.config] ens5f1 phc offset       -92 s0 freq    -890 delay   2464   ( this is down)
		//phc2sys[4268818.287]: [ptp4l.0.config] ens5f0 phc offset       -47 s2 freq   -2047 delay   2438
		// Use threshold to CLOCK_REALTIME==SLAVE, rest send clock state to metrics no events
		ifPtp, offsetFromMaster, _, frequencyAdjustment, delayFromMaster, syncState := extractRegularMetrics(processName, output)
		if ifPtp == "" {
			return // don't do if iface not known
		}
		reportInterface := ifPtp
		ifPtpType := types.IFace(ifPtp)

		if _, found := p.Stats[ifPtpType]; !found {
			p.Stats[ifPtpType] = NewStats()
		}
		if _, found := p.Stats[ifPtpType]; !found {
			p.Stats[ifPtpType] = &Stats{}
		}
		p.Stats[ifPtpType].frequencyAdjustment = int64(frequencyAdjustment)
		p.Stats[ifPtpType].delayFromMaster = int64(delayFromMaster)

		switch ifPtp {
		case ClockRealTime:
			if ptpInterface, err = ptp4lCfg.ByRole(types.SLAVE); err != nil {
				return
			}
			//ifPtpName := ptpInterface.Name
			if ptpInterface.Role != types.FAULTY { // already event generated by ptp4l watcher and  /HOLDOVER/FREERUN
				p.GenPhc2SysEvent(processNameType, ifPtpType, int64(offsetFromMaster), syncState)
				UpdateSyncStateMetrics(processName, ifPtp, p.Stats[ifPtpType].lastSyncState)
				//this is same as CLOCK_REALTIME
				UpdateSyncStateMetrics(processName, ptpInterface.Name, p.Stats[ifPtpType].lastSyncState)
			}
			UpdatePTPMasterMetrics(processName, reportInterface, offsetFromMaster, float64(p.Stats[ifPtpType].getMaxAbs()),
				frequencyAdjustment, delayFromMaster)
		case MasterClockType: // this ptp4l[5196819.100]: [ptp4l.0.config] master offset   -2162130 s2 freq +22451884 path delay
			p.Stats[ifPtpType].addValue(int64(offsetFromMaster))
			UpdatePTPMasterMetrics(processName, reportInterface, offsetFromMaster, float64(p.Stats[ifPtpType].getMaxAbs()),
				frequencyAdjustment, delayFromMaster)
			return
		default:
			if ptpInterface, err = ptp4lCfg.ByInterface(ifPtp); err != nil {
				log.Error(err)
				return
			}
			p.Stats[ifPtpType].addValue(int64(offsetFromMaster))
			if ptpInterface.Role != types.FAULTY { // already event generated by ptp4l watcher and  /HOLDOVER/FREERUN
				p.GenPhc2SysEvent(processNameType, ifPtpType, int64(offsetFromMaster), syncState)
				UpdateSyncStateMetrics(processName, ifPtp, p.Stats[ifPtpType].lastSyncState)
			}
			UpdatePTPSystemMetrics(processName, reportInterface, offsetFromMaster, float64(p.Stats[ifPtpType].getMaxAbs()),
				frequencyAdjustment, delayFromMaster)
		}
	} else if ptp4lProcessName == processName { //all we get from ptp4l is stats
		if strings.Contains(output, " port ") {
			portID, role, syncState := extractPTP4lEventState(output)
			if portID == 0 || role == types.UNKNOWN {
				return
			}
			if ptpInterface, err = ptp4lCfg.ByPortID(portID); err != nil {
				log.Error(err)
				log.Errorf("possible error due to filewatcher not updated")
				return
			}

			lastRole := ptpInterface.Role
			ifPtp := ptpInterface.Name
			ifPtpType := types.IFace(ptpInterface.Name)

			if lastRole != role {
				log.Infof("I UPDATEING interface %s with role %d", ifPtp, role)
				configNameType := types.ConfigName(ptp4lCfg.Name)
				p.Ptp4lConfigInterfaces[configNameType].Interfaces[portID-1].UpdateRole(role)
				log.Infof("AFTER %s", p.GetPTPConfig(configNameType).String())
				// update role metrics
				UpdateInterfaceRoleMetrics(processName, ifPtp, role)
			}

			if syncState == "" {
				return
			}
			if _, found := p.Stats[ifPtpType]; !found {
				p.Stats[ifPtpType] = NewStats()
			}
			if _, found := p.Stats[ifPtpType]; !found {
				p.Stats[ifPtpType] = &Stats{}
			}

			lastSyncState := p.Stats[ifPtpType].lastSyncState
			offset := p.Stats[ifPtpType].Offset()
			p.Stats[ifPtpType].lastSyncState = syncState

			if syncState != lastSyncState { // ptp4l events happened
				p.PublishEvent(syncState, offset, ifPtp, channel.PTPEvent)
				UpdateSyncStateMetrics(phc2sysProcessName, ifPtp, syncState)
				if syncState == ceevent.HOLDOVER {
					threshold := p.PtpThreshold(ifPtp)
					go func(ptpManager *PTPEventManager, threshold ptpConfig.PtpClockThreshold, iface string, role types.PtpPortRole) {
						time.AfterFunc(time.Duration(threshold.HoldOverTimeout)*time.Second, func() {
							defer func() {
								if r := recover(); r != nil {
									fmt.Println("Recovered in f", r)
								}
							}()
							ifPtpType := types.IFace(ifPtp)
							if s, found := p.Stats[ifPtpType]; found {
								if s.lastSyncState == ceevent.HOLDOVER {
									log.Infof("HOLDOVER timeout after %d secs,setting clock state to FREERUN from HOLDOVER state for %s",
										threshold.HoldOverTimeout, iface)
									ptpManager.PublishEvent(ceevent.FREERUN, s.Offset(), iface, channel.PTPEvent)
									UpdateSyncStateMetrics(phc2sysProcessName, iface, ceevent.FREERUN)
									s.lastSyncState = ceevent.FREERUN
									//s.reset()
								}
							} else {
								log.Errorf("failed to switch from holdover, could not find stats for interface %s", iface)
							}
						})
					}(p, threshold, ifPtp, lastRole)
				}
			}
		}
	}
}

// GenPhc2SysEvent ... generate events form the logs
func (p *PTPEventManager) GenPhc2SysEvent(processName types.ProcessName, iface types.IFace, offsetFromMaster int64, clockState ceevent.SyncState) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("Recovered in f", r)
		}
	}()
	if clockState == "" {
		return
	}
	lastClockState := p.Stats[iface].lastSyncState
	threshold := p.PtpThreshold(string(iface))
	switch clockState {
	case ceevent.LOCKED:
		switch lastClockState {
		case ceevent.FREERUN: //last state was already sent for FreeRUN , but if its within then send again with new state
			if offsetFromMaster < threshold.MaxOffsetThreshold && offsetFromMaster > threshold.MinOffsetThreshold { // within range
				p.PublishEvent(clockState, offsetFromMaster, string(iface), channel.PTPEvent) // change to locked
				p.Stats[iface].lastSyncState = clockState
				p.Stats[iface].lastOffset = int64(offsetFromMaster)
				p.Stats[iface].addValue(int64(offsetFromMaster)) // update off set when its in locked state and hold over only
			}
		case ceevent.LOCKED: // last state was in sync , check if it is out of sync now
			if offsetFromMaster > threshold.MaxOffsetThreshold || offsetFromMaster < threshold.MinOffsetThreshold { // out of sync
				p.PublishEvent(ceevent.FREERUN, offsetFromMaster, string(iface), channel.PTPEvent)
				p.Stats[iface].lastSyncState = ceevent.FREERUN
				p.Stats[iface].lastOffset = int64(offsetFromMaster)
				p.Stats[iface].reset()
			} else {
				p.Stats[iface].lastOffset = int64(offsetFromMaster)
				p.Stats[iface].addValue(int64(offsetFromMaster)) // update off set when its in locked state and hold over only
			}
		case ceevent.HOLDOVER: // last state was holdover, since it is in locked now check if it qualifies to be locked
			//do nothing , the time out will switch holdover to FREERUN
		default: // not yet used states
			log.Warnf("%s unkown handled last ptp state %s", iface, lastClockState)
			p.PublishEvent(clockState, offsetFromMaster, string(iface), channel.PTPEvent) // change to locked
			p.Stats[iface].lastSyncState = clockState
			p.Stats[iface].lastOffset = int64(offsetFromMaster)
			//do nothing already send event
		}
	case ceevent.FREERUN:
		if offsetFromMaster < threshold.MaxOffsetThreshold && offsetFromMaster > threshold.MinOffsetThreshold { // within range
			p.PublishEvent(ceevent.LOCKED, offsetFromMaster, string(iface), channel.PTPEvent) // change to locked
			p.Stats[iface].lastSyncState = ceevent.LOCKED
			p.Stats[iface].lastOffset = int64(offsetFromMaster)
			p.Stats[iface].addValue(int64(offsetFromMaster))
		}
	default:
		log.Warnf("%s unkown current ptp state %s", iface, clockState)
		p.PublishEvent(clockState, offsetFromMaster, string(iface), channel.PTPEvent) // change to locked
		p.Stats[iface].lastSyncState = clockState
		p.Stats[iface].lastOffset = int64(offsetFromMaster)
		p.Stats[iface].reset() // reset when its free run
	}
}

//PublishEvent ...publish events
func (p *PTPEventManager) PublishEvent(state ceevent.SyncState, offsetFromMaster int64, iface string, eventType channel.EventDataType) {
	// create an event
	if state == "" {
		return
	}
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
		if err = common.PublishEventViaAPI(p.scConfig, e); err != nil {
			log.Errorf("failed to publish ptp event %v, %s", e, err)
			return
		}
	}
}

// UpdatePTPMasterMetrics ...
func UpdatePTPMasterMetrics(process, iface string, offsetFromMaster, maxOffsetFromMaster, frequencyAdjustment, delayFromMaster float64) {
	OffsetFromMaster.With(prometheus.Labels{
		"process": process, "node": ptpNodeName, "iface": iface}).Set(offsetFromMaster)

	MaxOffsetFromMaster.With(prometheus.Labels{
		"process": process, "node": ptpNodeName, "iface": iface}).Set(maxOffsetFromMaster)

	FrequencyAdjustment.With(prometheus.Labels{
		"process": process, "node": ptpNodeName, "iface": iface}).Set(frequencyAdjustment)

	DelayFromMaster.With(prometheus.Labels{
		"process": process, "node": ptpNodeName, "iface": iface}).Set(delayFromMaster)
}

// UpdatePTPSystemMetrics ...
func UpdatePTPSystemMetrics(process, iface string, offsetFromMaster, maxOffsetFromMaster, frequencyAdjustment, delayFromMaster float64) {
	OffsetFromSystem.With(prometheus.Labels{
		"process": process, "node": ptpNodeName, "iface": iface}).Set(offsetFromMaster)

	MaxOffsetFromSystem.With(prometheus.Labels{
		"process": process, "node": ptpNodeName, "iface": iface}).Set(maxOffsetFromMaster)

	FrequencyAdjustment.With(prometheus.Labels{
		"process": process, "node": ptpNodeName, "iface": iface}).Set(frequencyAdjustment)

	DelayFromMaster.With(prometheus.Labels{
		"process": process, "node": ptpNodeName, "iface": iface}).Set(delayFromMaster)
}

// UpdateDeletedPTPMetrics ... update metrics for deleted ptp config
func UpdateDeletedPTPMetrics(iface, processName string) {
	UpdatePTPMasterMetrics(processName, iface, FreeRunOffsetValue, FreeRunOffsetValue, FreeRunOffsetValue, FreeRunOffsetValue)
}

// UpdateSyncStateMetrics ...
func UpdateSyncStateMetrics(process, iface string, state ceevent.SyncState) {
	if state == ceevent.LOCKED {
		MasterSyncState.With(prometheus.Labels{
			"process": process, "node": ptpNodeName, "iface": iface}).Set(1)
	} else if state == ceevent.FREERUN {
		MasterSyncState.With(prometheus.Labels{
			"process": process, "node": ptpNodeName, "iface": iface}).Set(0)
	} else if state == ceevent.HOLDOVER {
		MasterSyncState.With(prometheus.Labels{
			"process": process, "node": ptpNodeName, "iface": iface}).Set(2)
	}
}

//UpdateInterfaceRoleMetrics ...
func UpdateInterfaceRoleMetrics(process, iface string, role types.PtpPortRole) {
	InterfaceRole.With(prometheus.Labels{
		"process": process, "node": ptpNodeName, "iface": iface}).Set(float64(role))
}

func extractSummaryMetrics(processName, output string) (iface string, offsetFromMaster, maxOffsetFromMaster, frequencyAdjustment, delayFromMaster float64) {
	// remove everything before the rms string
	// This makes the out to equals
	// 0            1       2              3     4   5      6     7     8       9
	//phc2sys 5196755.139 ptp4l.0.config ens7f1 rms 3151717 max 3151717 freq -6085106 +/-   0 delay  2746 +/-   0
	//phc2sys 5196755.139 ptp4l.0.config ens5f0 rms 3152778 max 3152778 freq -6083928 +/-   0 delay  2791 +/-   0
	//phc2sys 5196804.326 ptp4l.0.config CLOCK_REALTIME rms 9452637 max 9452637 freq +1196097 +/-   0 delay  1000
	// remove everything before the rms string
	// This makes the out to equals

	indx := strings.Index(output, "rms")
	if indx < 0 {
		return
	}

	replacer := strings.NewReplacer("[", " ", "]", " ", ":", " ")
	output = replacer.Replace(output)
	indx = FindLogFileIndex(output)
	if indx == -1 {
		log.Errorf("config name is not found in log outpt")
		return
	}
	output = output[indx:]
	fields := strings.Fields(output)

	//ptp4l.0.config CLOCK_REALTIME rms   31 max   31 freq -77331 +/-   0 delay  1233 +/-   0
	if len(fields) < 8 {
		log.Errorf("%s failed to parse output %s: unexpected number of fields", processName, output)
		return
	}

	iface = fields[1]

	offsetFromMaster, err := strconv.ParseFloat(fields[3], 64)
	if err != nil {
		log.Errorf("%s failed to parse offset from master output %s error %v", processName, fields[3], err)
	}

	maxOffsetFromMaster, err = strconv.ParseFloat(fields[5], 64)
	if err != nil {
		log.Errorf("%s failed to parse max offset from master output %s error %v", processName, fields[5], err)
	}

	frequencyAdjustment, err = strconv.ParseFloat(fields[7], 64)
	if err != nil {
		log.Errorf("%s failed to parse frequency adjustment output %s error %v", processName, fields[7], err)
	}

	if len(fields) >= 11 {
		delayFromMaster, err = strconv.ParseFloat(fields[11], 64)
		if err != nil {
			log.Errorf("%s failed to parse delay from master output %s error %v", processName, fields[11], err)
		}
	} else {
		// If there is no delay from master this mean we are out of sync
		log.Warningf("no delay from master process %s out of sync", processName)
	}

	return
}

func extractRegularMetrics(processName, output string) (interfaceName string, offsetFromMaster, maxOffsetFromMaster, frequencyAdjustment, delayFromMaster float64, clockState ceevent.SyncState) {
	// remove everything before the rms string
	// This makes the out to equals
	//ptp4l[5196819.100]: [ptp4l.0.config] master offset   -2162130 s2 freq +22451884 path delay    374976
	//phc2sys[4268818.286]: [] CLOCK_REALTIME phc offset       -62 s0 freq  -78368 delay   1100
	//phc2sys[4268818.287]: [] ens5f1 phc offset       -92 s0 freq    -890 delay   2464   ( this is down)
	/// phc2sys[4268818.287]: [] ens5f0 phc offset       -47 s2 freq   -2047 delay   2438

	// 0     1            2              3       4         5    6   7     8         9   10       11
	//                                  1       2           3   4   5     6        7    8         9
	//ptp4l 5196819.100 ptp4l.0.config master offset   -2162130 s2 freq +22451884 path delay    374976
	output = strings.Replace(output, "path", "", 1)
	replacer := strings.NewReplacer("[", " ", "]", " ", ":", " ", "phc", "")
	output = replacer.Replace(output)

	index := FindLogFileIndex(output)
	if index == -1 {
		log.Errorf("config name is not found in log outpt")
		return
	}
	output = output[index:]
	fields := strings.Fields(output)

	if len(fields) < 7 {
		log.Errorf("%s failed to parse output %s: unexpected number of fields", processName, output)
		return
	}

	interfaceName = fields[1]

	offsetFromMaster, err := strconv.ParseFloat(fields[3], 64)
	if err != nil {
		log.Errorf("%s failed to parse offset from master output %s error %v", processName, fields[3], err)
	}

	maxOffsetFromMaster, err = strconv.ParseFloat(fields[3], 64)
	if err != nil {
		log.Errorf("%s failed to parse max offset from master output %s error %v", processName, fields[3], err)
	}

	frequencyAdjustment, err = strconv.ParseFloat(fields[6], 64)
	if err != nil {
		log.Errorf("%s failed to parse frequency adjustment output %s error %v", processName, fields[6], err)
	}

	state := fields[4]

	switch state {
	case unLocked:
		clockState = ceevent.FREERUN
	case clockStep:
		clockState = ceevent.FREERUN
	case locked:
		clockState = ceevent.LOCKED
	default:
		log.Errorf("%s -  failed to parse clock state output `%s` ", processName, fields[2])
	}

	if len(fields) >= 8 {
		delayFromMaster, err = strconv.ParseFloat(fields[8], 64)
		if err != nil {
			log.Errorf("%s failed to parse delay from master output %s error %v", processName, fields[8], err)
		}
	} else {
		// If there is no delay from master this mean we are out of sync
		clockState = ceevent.HOLDOVER
		log.Warningf("no delay from master process %s out of sync", processName)
	}

	return
}

// ExtractPTP4lEvent ... extract event form ptp4l logs
func extractPTP4lEventState(output string) (portID int, role types.PtpPortRole, clockState ceevent.SyncState) {
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
		     "MASTER to PASSIVE"
	*/
	//ptp4l[5199193.712]: [ptp4l.0.config] port 1: SLAVE to UNCALIBRATED on SYNCHRONIZATION_FAULT
	replacer := strings.NewReplacer("[", " ", "]", " ", ":", " ")
	output = replacer.Replace(output)

	index := strings.Index(output, " port ")
	if index == -1 {
		return
	}
	output = output[index:]
	fields := strings.Fields(output)
	portIndex := fields[1]
	role = types.UNKNOWN
	var e error
	portID, e = strconv.Atoi(portIndex)
	if e != nil {
		log.Errorf("error parsing port id %s", e)
		portID = 0
		return
	}
	clockState = ceevent.FREERUN

	if strings.Contains(output, "UNCALIBRATED to SLAVE") {
		role = types.SLAVE
	} else if strings.Contains(output, "UNCALIBRATED to PASSIVE") || strings.Contains(output, "MASTER to PASSIVE") ||
		strings.Contains(output, "SLAVE to PASSIVE") {
		role = types.PASSIVE
	} else if strings.Contains(output, "UNCALIBRATED to MASTER") || strings.Contains(output, "LISTENING to MASTER") {
		role = types.MASTER
	} else if strings.Contains(output, "FAULT_DETECTED") || strings.Contains(output, "SYNCHRONIZATION_FAULT") {
		role = types.FAULTY
		clockState = ceevent.HOLDOVER
	}
	return
}

//FindLogFileIndex ...
func FindLogFileIndex(out string) int {
	match := ptpConfigFileRegEx.FindStringIndex(out)
	if len(match) == 2 {
		return match[0]
	}
	return -1

}
