package metrics

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/stats"

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
	ptpNamespace = "openshift"
	ptpSubsystem = "ptp"

	phc2sysProcessName = "phc2sys"
	ptp4lProcessName   = "ptp4l"

	unLocked  = "s0"
	clockStep = "s1"
	locked    = "s2"

	//FreeRunOffsetValue when sync state is FREERUN
	FreeRunOffsetValue = -9999999999999999
	//ClockRealTime is the slave
	ClockRealTime = "CLOCK_REALTIME"
	//MasterClockType is teh slave sync slave clock to master
	MasterClockType = "master"

	// from the logs
	processNameIndex = 0
	configNameIndex  = 2

	// from the logs
	offset = "offset"
	rms    = "rms"

	// offset source
	phc    = "phc"
	sys    = "sys"
	master = "master"
)

var (
	ptpConfigFileRegEx = regexp.MustCompile(`ptp4l.[0-9]*.config`)
	// NodeName from the env
	ptpNodeName = ""

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

	//SyncState metrics to show current clock state
	SyncState = prometheus.NewGaugeVec(
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
			Name:      "threshold",
			Help:      "",
		}, []string{"threshold", "node", "iface"})

	// InterfaceRole metrics to show current interface role
	InterfaceRole = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: ptpNamespace,
			Subsystem: ptpSubsystem,
			Name:      "interface_role",
			Help:      "0 = PASSIVE, 1 = SLAVE, 2 = MASTER, 3 = FAULTY, 4 =  UNKNOWN",
		}, []string{"process", "node", "iface"})
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
		prometheus.MustRegister(Threshold)
		prometheus.MustRegister(InterfaceRole)

		// Including these stats kills performance when Prometheus polls with multiple targets
		prometheus.Unregister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
		prometheus.Unregister(collectors.NewGoCollector())

		ptpNodeName = nodeName
	})
}

// PTPEventManager for PTP
type PTPEventManager struct {
	publisherID string
	nodeName    string
	scConfig    *common.SCConfiguration
	lock        sync.RWMutex
	Stats       map[types.ConfigName]map[types.IFace]*stats.Stats
	mock        bool
	//PtpConfigMapUpdates holds ptp-configmap updated details
	PtpConfigMapUpdates *ptpConfig.LinuxPTPConfigMapUpdate
	// Ptp4lConfigInterfaces holds interfaces and its roles , after reading from ptp4l config files
	Ptp4lConfigInterfaces map[types.ConfigName]*ptp4lconf.PTP4lConfig
}

// NewPTPEventManager to manage events and metrics
func NewPTPEventManager(publisherID string, nodeName string, config *common.SCConfiguration) (ptpEventManager *PTPEventManager) {
	ptpEventManager = &PTPEventManager{
		publisherID:           publisherID,
		nodeName:              nodeName,
		scConfig:              config,
		lock:                  sync.RWMutex{},
		Stats:                 make(map[types.ConfigName]map[types.IFace]*stats.Stats),
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
			Close:              t.Close,
		}
	} else if len(p.PtpConfigMapUpdates.EventThreshold) > 0 { //if not found get the first item since one per config)
		for _, t := range p.PtpConfigMapUpdates.EventThreshold {
			return ptpConfig.PtpClockThreshold{
				HoldOverTimeout:    t.HoldOverTimeout,
				MaxOffsetThreshold: t.MaxOffsetThreshold,
				MinOffsetThreshold: t.MinOffsetThreshold,
				Close:              t.Close,
			}
		}
	}
	return ptpConfig.GetDefaultThreshold()
}

// MockTest .. use for test only
func (p *PTPEventManager) MockTest(t bool) {
	p.mock = t
}

// DeleteStats ... delete stats obj
func (p *PTPEventManager) DeleteStats(name types.ConfigName, key types.IFace) {
	p.lock.Lock()
	if _, ok := p.Stats[name]; ok {
		delete(p.Stats[name], key)
	}
	p.lock.Unlock()
}

// DeleteStatsConfig ... delete stats obj
func (p *PTPEventManager) DeleteStatsConfig(key types.ConfigName) {
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

// GetStatsForInterface ...
func (p *PTPEventManager) GetStatsForInterface(name types.ConfigName, iface types.IFace) map[types.IFace]*stats.Stats {
	if _, found := p.Stats[name]; !found {
		p.Stats[name] = make(map[types.IFace]*stats.Stats)
		p.Stats[name][iface] = stats.NewStats(string(name))
	} else if _, found := p.Stats[name][iface]; !found {
		p.Stats[name][iface] = stats.NewStats(string(name))
	}
	return p.Stats[name]
}

// GetStats ...
func (p *PTPEventManager) GetStats(name types.ConfigName) map[types.IFace]*stats.Stats {
	if _, found := p.Stats[name]; !found {
		p.Stats[name] = make(map[types.IFace]*stats.Stats)
	}
	return p.Stats[name]
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

	// make sure configname is found in logs
	indx := FindInLogForCfgFileIndex(output)
	if indx == -1 {
		log.Errorf("config name is not found in log outpt")
		return
	}

	if len(fields) < 3 {
		log.Errorf("ignoring log:log is not in required format ptp4l/phc2sys[time]: [config] %s", output)
		return
	}

	processName := fields[processNameIndex]
	configName := fields[configNameIndex]
	ptp4lCfg := p.GetPTPConfig(types.ConfigName(configName))
	ptpStats := p.GetStats(types.ConfigName(configName))

	if len(ptp4lCfg.Interfaces) == 0 { //TODO: Use PMC to update port and roles
		log.Errorf("file watcher have not picked the files yet")
		return
	}
	var ptpInterface ptp4lconf.PTPInterface
	var err error
	if strings.Contains(output, " max ") { // this get generated in case -u is passed as an option to phy2sys opts
		interfaceName, ptpOffset, maxPtpOffset, frequencyAdjustment, delay := extractSummaryMetrics(processName, output)
		switch interfaceName {
		case ClockRealTime:
			UpdatePTPMetrics(phc, processName, interfaceName, ptpOffset, maxPtpOffset, frequencyAdjustment, delay)
		case MasterClockType:
			UpdatePTPMetrics(master, processName, interfaceName, ptpOffset, maxPtpOffset, frequencyAdjustment, delay)
		case "":
			//do nothing
		default:
			UpdatePTPMetrics(phc, processName, interfaceName, ptpOffset, maxPtpOffset, frequencyAdjustment, delay)
		}
	} else if strings.Contains(output, " offset ") {
		//ptp4l[5196819.100]: [ptp4l.0.config] master offset   -2162130 s2 freq +22451884 path delay    374976
		//phc2sys[4268818.286]: [ptp4l.0.config] CLOCK_REALTIME phc offset       -62 s0 freq  -78368 delay   1100
		//phc2sys[4268818.287]: [ptp4l.0.config] ens5f1 phc offset       -92 s0 freq    -890 delay   2464   ( this is down)
		//phc2sys[4268818.287]: [ptp4l.0.config] ens5f0 phc offset       -47 s2 freq   -2047 delay   2438
		// Use threshold to CLOCK_REALTIME==SLAVE, rest send clock state to metrics no events
		interfaceName, ptpOffset, _, frequencyAdjustment, delay, syncState := extractRegularMetrics(processName, output)
		if interfaceName == "" {
			return // don't do if iface not known
		}
		offsetSource := master
		if strings.Contains(output, "sys offset") {
			offsetSource = sys
		} else if strings.Contains(output, "phc offset") {
			offsetSource = phc
		}

		interfaceType := types.IFace(interfaceName)

		if _, found := ptpStats[interfaceType]; !found {
			ptpStats[interfaceType] = stats.NewStats(configName)
		}
		// update metrics
		ptpStats[interfaceType].SetOffsetSource(offsetSource)
		ptpStats[interfaceType].SetProcessName(processName)
		ptpStats[interfaceType].SetFrequencyAdjustment(int64(frequencyAdjustment))
		ptpStats[interfaceType].SetDelay(int64(delay))

		ptpInterface, _ = ptp4lCfg.ByRole(types.SLAVE)

		switch interfaceName {
		case ClockRealTime: //CLOCK_REALTIME is active slave interface
			// copy  ClockRealTime value to current slave interface
			if ptpInterface.Name != "" {
				p.GenPhc2SysEvent(ptpStats[interfaceType], ptpInterface.Name, ClockRealTime, int64(ptpOffset), syncState)
				UpdateSyncStateMetrics(processName, ptpInterface.Name, ptpStats[interfaceType].LastSyncState())
				UpdatePTPMetrics(offsetSource, processName, ptpInterface.Name, ptpOffset, float64(ptpStats[interfaceType].MaxAbs()),
					frequencyAdjustment, delay)
			} else {
				p.GenPhc2SysEvent(ptpStats[interfaceType], interfaceName, ClockRealTime, int64(ptpOffset), syncState)
			}
			//this will update for CLOCK_REALTIME
			UpdateSyncStateMetrics(processName, interfaceName, ptpStats[interfaceType].LastSyncState())
			UpdatePTPMetrics(offsetSource, processName, interfaceName, ptpOffset, float64(ptpStats[interfaceType].MaxAbs()),
				frequencyAdjustment, delay)
		case MasterClockType: // this ptp4l[5196819.100]: [ptp4l.0.config] master offset   -2162130 s2 freq +22451884 path delay
			// Report events for master  by masking the index  number of the slave interface
			if ptpInterface.Name != "" {
				r := []rune(ptpInterface.Name)
				ptpInterface.Name = string(r[:len(r)-1]) + "x"
				masterResource := fmt.Sprintf("%s/%s", ptpInterface.Name, MasterClockType)
				p.GenPhc2SysEvent(ptpStats[interfaceType], ptpInterface.Name,
					masterResource,
					int64(ptpOffset), syncState)
				UpdateSyncStateMetrics(processName, ptpInterface.Name, ptpStats[interfaceType].LastSyncState())
				UpdatePTPMetrics(offsetSource, processName, ptpInterface.Name, ptpOffset, float64(ptpStats[interfaceType].MaxAbs()),
					frequencyAdjustment, delay)
			}
			ptpStats[interfaceType].AddValue(int64(ptpOffset))

		default:
			if ptpInterface, err = ptp4lCfg.ByInterface(interfaceName); err != nil {
				log.Error(err)
				break
			}
			if ptpInterface.Role != types.FAULTY && int64(ptpOffset) > 0 { //if its faulty leave it to last state(HOLDOVER  or FREERUN)
				p.GenPhc2SysEvent(ptpStats[interfaceType], interfaceName, interfaceName, int64(ptpOffset), syncState)
			}
			UpdateSyncStateMetrics(processName, interfaceName, ptpStats[interfaceType].LastSyncState())
			UpdatePTPMetrics(offsetSource, processName, interfaceName, ptpOffset, float64(ptpStats[interfaceType].MaxAbs()),
				frequencyAdjustment, delay)
		}
	}
	if ptp4lProcessName == processName { //all we get from ptp4l is stats
		if strings.Contains(output, " port ") {
			portID, role, syncState := extractPTP4lEventState(output)
			if portID == 0 || role == types.UNKNOWN {
				return
			}
			if ptpInterface, err = ptp4lCfg.ByPortID(portID); err != nil {
				log.Error(err)
				log.Errorf("possible error due to file watcher not updated")
				return
			}
			log.Infof("found interface %s for port id %d last role %s has currrent role %s",
				ptpInterface.Name, portID, ptpInterface.Role, role)

			lastRole := ptpInterface.Role
			iface := ptpInterface.Name
			ifaceType := types.IFace(ptpInterface.Name)

			if _, found := ptpStats[ifaceType]; !found {
				ptpStats[ifaceType] = stats.NewStats(configName)
				ptpStats[ifaceType].SetProcessName(phc2sysProcessName) //default will change once set offset
			}
			if lastRole != role {
				/*
				   If role changes from FAULTY to SLAVE/PASSIVE/MASTER , then cancel HOLDOVER timer
				    and publish metrics
				*/
				if lastRole == types.FAULTY { // cancel any HOLDOVER timeout
					if t, ok := p.PtpConfigMapUpdates.EventThreshold[iface]; ok {
						log.Infof("interface %s is not anymore faulty, cancel holdover", iface)
						t.SafeClose() // close any holdover go routines
					}
					p.PublishEvent(ceevent.FREERUN, ptpStats[ifaceType].LastOffset(), iface, channel.PTPEvent)
					UpdateSyncStateMetrics(ptpStats[ifaceType].ProcessName(), iface, ceevent.FREERUN)
					ptpStats[ifaceType].SetLastSyncState(ceevent.FREERUN)
				}
				log.Infof("update interface %s with portid %d from role %s to  role %s", iface, portID, lastRole, role)
				ptp4lCfg.Interfaces[portID-1].UpdateRole(role)
				// update role metrics
				UpdateInterfaceRoleMetrics(processName, iface, role)
			}

			//Enter the HOLDOVER state: If current sycState is HOLDOVER(Role is at FAULTY) ,then spawn a go routine to hold the state until
			// holdover timeout
			if syncState != "" && syncState != ptpStats[ifaceType].LastSyncState() && syncState == ceevent.HOLDOVER {
				ptpStats[ifaceType].SetLastSyncState(syncState)
				p.PublishEvent(syncState, ptpStats[ifaceType].LastOffset(), iface, channel.PTPEvent)
				UpdateSyncStateMetrics(ptpStats[ifaceType].ProcessName(), iface, syncState)
				threshold := p.PtpThreshold(iface)
				go func(ptpManager *PTPEventManager, holdoverTimeout int64, faultyIface string, role types.PtpPortRole, c chan struct{}) {
					defer func() {
						log.Infof("exiting holdover for interface %s", faultyIface)
						if r := recover(); r != nil {
							fmt.Println("Recovered in f", r)
						}
					}()

					select {
					case <-c:
						log.Infof("call recieved to close holderover timeout")
						return
					case <-time.After(time.Duration(holdoverTimeout) * time.Second):
						faultyIfaceType := types.IFace(faultyIface)
						log.Infof("time expired for interface %s", faultyIface)
						if s, found := ptpStats[faultyIfaceType]; found {
							if s.LastSyncState() == ceevent.HOLDOVER {
								log.Infof("HOLDOVER timeout after %d secs,setting clock state to FREERUN from HOLDOVER state for %s",
									holdoverTimeout, faultyIface)
								ptpManager.PublishEvent(ceevent.FREERUN, s.Offset(), faultyIface, channel.PTPEvent)
								UpdateSyncStateMetrics(s.ProcessName(), faultyIface, ceevent.FREERUN)
								s.SetLastSyncState(ceevent.FREERUN)
								//s.reset()
							}
						} else {
							log.Errorf("failed to switch from holdover, could not find stats for interface %s", faultyIface)
						}
					}

				}(p, threshold.HoldOverTimeout, iface, lastRole, threshold.Close)
			}
		}
	}
}

// GenPhc2SysEvent ... generate events form the logs
func (p *PTPEventManager) GenPhc2SysEvent(stats *stats.Stats, iface string, eventResourceName string, ptpOffset int64, clockState ceevent.SyncState) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("Recovered in f", r)
		}
	}()
	if clockState == "" {
		return
	}

	lastClockState := stats.LastSyncState()
	threshold := p.PtpThreshold(iface)
	switch clockState {
	case ceevent.LOCKED:
		switch lastClockState {
		case ceevent.FREERUN: //last state was already sent for FreeRUN , but if its within then send again with new state
			if isOffsetInRange(ptpOffset, threshold.MaxOffsetThreshold, threshold.MinOffsetThreshold) { // within range
				log.Infof(" publishing event for %s with last state %s and current clock state %s and offset %d", iface, stats.LastSyncState(), clockState, ptpOffset)
				p.PublishEvent(clockState, ptpOffset, eventResourceName, channel.PTPEvent) // change to locked
				stats.SetLastSyncState(clockState)
				stats.SetLastOffset(int64(ptpOffset))
				stats.AddValue(int64(ptpOffset)) // update off set when its in locked state and hold over only
			}
		case ceevent.LOCKED: // last state was in sync , check if it is out of sync now
			if isOffsetInRange(ptpOffset, threshold.MaxOffsetThreshold, threshold.MinOffsetThreshold) {
				stats.SetLastOffset(int64(ptpOffset))
				stats.AddValue(int64(ptpOffset)) // update off set when its in locked state and hold over only
			} else {
				log.Infof(" publishing event for %s with last state %s and current clock state %s and offset %d", iface, stats.LastSyncState(), clockState, ptpOffset)
				p.PublishEvent(ceevent.FREERUN, ptpOffset, eventResourceName, channel.PTPEvent)
				stats.SetLastSyncState(ceevent.FREERUN)
				stats.SetLastOffset(int64(ptpOffset))
			}

		case ceevent.HOLDOVER:
			//do nothing , the time out will switch holdover to FREERUN
		default: // not yet used states
			log.Warnf("unknown %s sync state %s ,has last ptp state %s", eventResourceName, clockState, lastClockState)
			if !isOffsetInRange(ptpOffset, threshold.MaxOffsetThreshold, threshold.MinOffsetThreshold) {
				clockState = ceevent.FREERUN
			}
			log.Infof(" publishing event for %s with last state %s and current clock state %s and offset %d", iface, stats.LastSyncState(), clockState, ptpOffset)
			p.PublishEvent(clockState, ptpOffset, eventResourceName, channel.PTPEvent) // change to unknown
			stats.SetLastSyncState(clockState)
			stats.SetLastOffset(int64(ptpOffset))
		}
	case ceevent.FREERUN:
		if lastClockState != ceevent.FREERUN { // within range
			log.Infof(" publishing event for %s with last state %s and current clock state %s and offset %d", iface, stats.LastSyncState(), clockState, ptpOffset)
			p.PublishEvent(clockState, ptpOffset, eventResourceName, channel.PTPEvent) // change to locked
			stats.SetLastSyncState(clockState)
			stats.SetLastOffset(int64(ptpOffset))
			stats.AddValue(int64(ptpOffset))
		}
	default:
		log.Warnf("%s unknown current ptp state %s", iface, clockState)
		if ptpOffset > threshold.MaxOffsetThreshold || ptpOffset < threshold.MinOffsetThreshold {
			clockState = ceevent.FREERUN
		}
		log.Infof(" publishing event for %s with last state %s and current clock state %s and offset %d", iface, stats.LastSyncState(), clockState, ptpOffset)
		p.PublishEvent(clockState, ptpOffset, eventResourceName, channel.PTPEvent) // change to unknown state
		stats.SetLastSyncState(clockState)
		stats.SetLastOffset(int64(ptpOffset))
	}
}

//PublishEvent ...publish events
func (p *PTPEventManager) PublishEvent(state ceevent.SyncState, ptpOffset int64, iface string, eventType channel.EventDataType) {
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
			Value:     float64(ptpOffset),
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

// UpdatePTPMetrics ...
func UpdatePTPMetrics(metricsType, process, iface string, offset, maxOffset, frequencyAdjustment, delay float64) {
	PtpOffset.With(prometheus.Labels{"from": metricsType,
		"process": process, "node": ptpNodeName, "iface": iface}).Set(offset)

	PtpMaxOffset.With(prometheus.Labels{"from": metricsType,
		"process": process, "node": ptpNodeName, "iface": iface}).Set(maxOffset)

	PtpFrequencyAdjustment.With(prometheus.Labels{"from": metricsType,
		"process": process, "node": ptpNodeName, "iface": iface}).Set(frequencyAdjustment)

	PtpDelay.With(prometheus.Labels{"from": metricsType,
		"process": process, "node": ptpNodeName, "iface": iface}).Set(delay)
}

// UpdateDeletedPTPMetrics ... update metrics for deleted ptp config
func UpdateDeletedPTPMetrics(offsetSource, iface, processName string) {
	UpdatePTPMetrics(offsetSource, processName, iface, FreeRunOffsetValue, FreeRunOffsetValue, FreeRunOffsetValue, FreeRunOffsetValue)
}

// UpdateSyncStateMetrics ...
func UpdateSyncStateMetrics(process string, iface string, state ceevent.SyncState) {
	if state == ceevent.LOCKED {
		SyncState.With(prometheus.Labels{
			"process": process, "node": ptpNodeName, "iface": iface}).Set(1)
	} else if state == ceevent.FREERUN {
		SyncState.With(prometheus.Labels{
			"process": process, "node": ptpNodeName, "iface": iface}).Set(0)
	} else if state == ceevent.HOLDOVER {
		SyncState.With(prometheus.Labels{
			"process": process, "node": ptpNodeName, "iface": iface}).Set(2)
	}
}

//UpdateInterfaceRoleMetrics ...
func UpdateInterfaceRoleMetrics(process, iface string, role types.PtpPortRole) {
	InterfaceRole.With(prometheus.Labels{
		"process": process, "node": ptpNodeName, "iface": iface}).Set(float64(role))
}

func extractSummaryMetrics(processName, output string) (iface string, ptpOffset, maxPtpOffset, frequencyAdjustment, delay float64) {
	// remove everything before the rms string
	// This makes the out to equals
	// 0            1       2              3     4   5      6     7     8       9
	//phc2sys 5196755.139 ptp4l.0.config ens7f1 rms 3151717 max 3151717 freq -6085106 +/-   0 delay  2746 +/-   0
	//phc2sys 5196804.326 ptp4l.0.config CLOCK_REALTIME rms 9452637 max 9452637 freq +1196097 +/-   0 delay  1000
	// ptp4l[74737.942]: [ptp4l.0.config] rms   53 max   74 freq -16642 +/-  40 delay  1089 +/-  20

	indx := strings.Index(output, "rms")
	if indx < 0 {
		return
	}

	replacer := strings.NewReplacer("[", " ", "]", " ", ":", " ")
	output = replacer.Replace(output)
	indx = FindInLogForCfgFileIndex(output)
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

	// when ptp4l log is missing interface name
	if fields[1] == rms {
		fields = append(fields, "") // Making space for the new element
		//  0             1     2
		//ptp4l.0.config rms   53 max   74 freq -16642 +/-  40 delay  1089 +/-  20
		copy(fields[2:], fields[1:]) // Shifting elements
		fields[1] = MasterClockType  // Copying/inserting the value
		//  0             0       1   2
		//ptp4l.0.config master rms   53 max   74 freq -16642 +/-  40 delay  1089 +/-  20
	}
	iface = fields[1]

	ptpOffset, err := strconv.ParseFloat(fields[3], 64)
	if err != nil {
		log.Errorf("%s failed to parse offset from the output %s error %v", processName, fields[3], err)
	}

	maxPtpOffset, err = strconv.ParseFloat(fields[5], 64)
	if err != nil {
		log.Errorf("%s failed to parse max offset from the output %s error %v", processName, fields[5], err)
	}

	frequencyAdjustment, err = strconv.ParseFloat(fields[7], 64)
	if err != nil {
		log.Errorf("%s failed to parse frequency adjustment output %s error %v", processName, fields[7], err)
	}

	if len(fields) >= 11 {
		delay, err = strconv.ParseFloat(fields[11], 64)
		if err != nil {
			log.Errorf("%s failed to parse delay from the output %s error %v", processName, fields[11], err)
		}
	} else {
		// If there is no delay from this mean we are out of sync
		log.Warningf("no delay from the process %s out of sync", processName)
	}

	return
}

func extractRegularMetrics(processName, output string) (interfaceName string, ptpOffset, maxPtpOffset, frequencyAdjustment, delay float64, clockState ceevent.SyncState) {
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
	replacer := strings.NewReplacer("[", " ", "]", " ", ":", " ", "phc", "", "sys", "")
	output = replacer.Replace(output)

	index := FindInLogForCfgFileIndex(output)
	if index == -1 {
		log.Errorf("config name is not found in log outpt")
		return
	}
	output = output[index:]
	fields := strings.Fields(output)

	//       0         1      2          3    4   5    6          7     8
	//ptp4l.0.config master offset   -2162130 s2 freq +22451884  delay 374976
	if len(fields) < 7 {
		log.Errorf("%s failed to parse output %s: unexpected number of fields", processName, output)
		return
	}

	if fields[2] != offset {
		log.Errorf("%s failed to parse offset from master output %s error %s", processName, fields[1], "offset is not in right order")
		return
	}

	interfaceName = fields[1]

	ptpOffset, err := strconv.ParseFloat(fields[3], 64)
	if err != nil {
		log.Errorf("%s failed to parse offset from master output %s error %v", processName, fields[3], err)
	}

	maxPtpOffset, err = strconv.ParseFloat(fields[3], 64)
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
		delay, err = strconv.ParseFloat(fields[8], 64)
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
	//port 1: delay timeout
	if len(fields) < 2 {
		log.Infof("failed to parse output %s: unexpected number of fields", output)
		return
	}

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
		strings.Contains(output, "SLAVE to PASSIVE") || strings.Contains(output, "LISTENING to PASSIVE") {
		role = types.PASSIVE
	} else if strings.Contains(output, "UNCALIBRATED to MASTER") || strings.Contains(output, "LISTENING to MASTER") {
		role = types.MASTER
	} else if strings.Contains(output, "FAULT_DETECTED") || strings.Contains(output, "SYNCHRONIZATION_FAULT") {
		role = types.FAULTY
		clockState = ceevent.HOLDOVER
	}
	return
}

//FindInLogForCfgFileIndex ...
func FindInLogForCfgFileIndex(out string) int {
	match := ptpConfigFileRegEx.FindStringIndex(out)
	if len(match) == 2 {
		return match[0]
	}
	return -1
}

func isOffsetInRange(ptpOffset, maxOffsetThreshold, minOffsetThreshold int64) bool {
	if ptpOffset < maxOffsetThreshold && ptpOffset > minOffsetThreshold {
		return true
	} else if ptpOffset > maxOffsetThreshold || ptpOffset < minOffsetThreshold {
		return false
	}
	return false
}
