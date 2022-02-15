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

	"github.com/prometheus/client_golang/prometheus/collectors"

	"github.com/redhat-cne/cloud-event-proxy/pkg/common"

	"github.com/prometheus/client_golang/prometheus"
	ptpConfig "github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/config"
	ceevent "github.com/redhat-cne/sdk-go/pkg/event"
	"github.com/redhat-cne/sdk-go/pkg/event/ptp"
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
	//MasterClockType is the slave sync slave clock to master
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
		}, []string{"threshold", "node", "profile"})

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
	publisherTypes map[ptp.EventType]*types.EventPublisherType
	nodeName       string
	scConfig       *common.SCConfiguration
	lock           sync.RWMutex
	Stats          map[types.ConfigName]map[types.IFace]*stats.Stats
	mock           bool
	//PtpConfigMapUpdates holds ptp-configmap updated details
	PtpConfigMapUpdates *ptpConfig.LinuxPTPConfigMapUpdate
	// Ptp4lConfigInterfaces holds interfaces and its roles , after reading from ptp4l config files
	Ptp4lConfigInterfaces map[types.ConfigName]*ptp4lconf.PTP4lConfig
}

// NewPTPEventManager to manage events and metrics
func NewPTPEventManager(publisherTypes map[ptp.EventType]*types.EventPublisherType,
	nodeName string, config *common.SCConfiguration) (ptpEventManager *PTPEventManager) {
	ptpEventManager = &PTPEventManager{
		publisherTypes:        publisherTypes,
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
func (p *PTPEventManager) PtpThreshold(profileName string) ptpConfig.PtpClockThreshold {
	if t, found := p.PtpConfigMapUpdates.EventThreshold[profileName]; found {
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

	// make sure configName is found in logs
	index := FindInLogForCfgFileIndex(output)
	if index == -1 {
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
			ptpInterface, _ = ptp4lCfg.ByRole(types.SLAVE)
			var alias string
			if ptpInterface.Name != "" {
				r := []rune(ptpInterface.Name)
				alias = string(r[:len(r)-1]) + "x"
				UpdatePTPMetrics(master, processName, alias, ptpOffset, maxPtpOffset, frequencyAdjustment, delay)
			} else {
				// this should not happen
				log.Errorf("metrics found for empty interface %s", output)
			}
		}
	} else if strings.Contains(output, " offset ") {
		//ptp4l[5196819.100]: [ptp4l.0.config] master offset   -2162130 s2 freq +22451884 path delay    374976
		//phc2sys[4268818.286]: [ptp4l.0.config] CLOCK_REALTIME phc offset       -62 s0 freq  -78368 delay   1100
		//phc2sys[4268818.287]: [ptp4l.0.config] ens5f1 phc offset       -92 s0 freq    -890 delay   2464   ( this is down)
		//phc2sys[4268818.287]: [ptp4l.0.config] ens5f0 phc offset       -47 s2 freq   -2047 delay   2438
		// Use threshold to CLOCK_REALTIME==SLAVE, rest send clock state to metrics no events
		interfaceName, ptpOffset, _, frequencyAdjustment, delay, syncState := extractRegularMetrics(processName, output)
		eventType := ptp.PtpStateChange
		if interfaceName == "" {
			return // don't do if iface not known
		}
		if !(interfaceName == master || interfaceName == ClockRealTime) {
			return // only master and clock_realtime are supported
		}
		offsetSource := master
		if strings.Contains(output, "sys offset") {
			offsetSource = sys
		} else if strings.Contains(output, "phc offset") {
			offsetSource = phc
			eventType = ptp.OsClockSyncStateChange
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
			p.GenPTPEvent(ptp4lCfg.Profile, ptpStats[interfaceType], interfaceName, int64(ptpOffset), syncState, eventType)
			UpdateSyncStateMetrics(processName, interfaceName, ptpStats[interfaceType].LastSyncState())
			UpdatePTPMetrics(offsetSource, processName, interfaceName, ptpOffset, float64(ptpStats[interfaceType].MaxAbs()), frequencyAdjustment, delay)
		case MasterClockType: // this ptp4l[5196819.100]: [ptp4l.0.config] master offset   -2162130 s2 freq +22451884 path delay
			// Report events for master  by masking the index  number of the slave interface
			if ptpInterface.Name != "" {
				alias := ptpStats[interfaceType].Alias()
				if alias == "" {
					r := []rune(ptpInterface.Name)
					alias = string(r[:len(r)-1]) + "x"
					ptpStats[interfaceType].SetAlias(alias)
				}
				masterResource := fmt.Sprintf("%s/%s", alias, MasterClockType)
				p.GenPTPEvent(ptp4lCfg.Profile, ptpStats[interfaceType], masterResource, int64(ptpOffset), syncState, eventType)
				UpdateSyncStateMetrics(processName, alias, ptpStats[interfaceType].LastSyncState())
				UpdatePTPMetrics(offsetSource, processName, alias, ptpOffset, float64(ptpStats[interfaceType].MaxAbs()),
					frequencyAdjustment, delay)
				ptpStats[interfaceType].AddValue(int64(ptpOffset))
			}
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
			ptpIFace := ptpInterface.Name

			if role == types.SLAVE {
				if _, found := ptpStats[ClockRealTime]; !found {
					ptpStats[ClockRealTime] = stats.NewStats(configName)
					ptpStats[ClockRealTime].SetProcessName(phc2sysProcessName)
				}
				if _, found := ptpStats[master]; !found {
					ptpStats[master] = stats.NewStats(configName)
					ptpStats[master].SetProcessName(ptp4lProcessName)
				}
			}

			if lastRole != role {
				/*
				   If role changes from FAULTY to SLAVE/PASSIVE/MASTER , then cancel HOLDOVER timer
				    and publish metrics
				*/
				if lastRole == types.FAULTY { //recovery
					if role == types.SLAVE { // cancel any HOLDOVER timeout for master, if new role is slave
						if t, ok := p.PtpConfigMapUpdates.EventThreshold[ptp4lCfg.Profile]; ok {
							log.Infof("interface %s is not anymore faulty, cancel holdover", ptpIFace)
							t.SafeClose() // close any holdover go routines
						}
						masterResource := fmt.Sprintf("%s/%s", ptpStats[master].Alias(), MasterClockType)
						p.GenPTPEvent(ptp4lCfg.Profile, ptpStats[master], masterResource, FreeRunOffsetValue, ptp.FREERUN, ptp.PtpStateChange)
						UpdateSyncStateMetrics(ptpStats[master].ProcessName(), ptpStats[master].Alias(), ptp.FREERUN)

						p.GenPTPEvent(ptp4lCfg.Profile, ptpStats[ClockRealTime], ClockRealTime, FreeRunOffsetValue, ptp.FREERUN, ptp.OsClockSyncStateChange)
						UpdateSyncStateMetrics(ptpStats[ClockRealTime].ProcessName(), ptpIFace, ptp.FREERUN)
					}
				}

				log.Infof("update interface %s with portid %d from role %s to  role %s", ptpIFace, portID, lastRole, role)
				ptp4lCfg.Interfaces[portID-1].UpdateRole(role)
				// update role metrics
				UpdateInterfaceRoleMetrics(processName, ptpIFace, role)
			}

			//Enter the HOLDOVER state: If current sycState is HOLDOVER(Role is at FAULTY) ,then spawn a go routine to hold the state until
			// holdover timeout, always put only master offset from ptp4l to HOLDOVER,when this goes to FREERUN
			// make any slave interface master offset to FREERUN
			if syncState != "" && syncState != ptpStats[master].LastSyncState() && syncState == ptp.HOLDOVER {
				// Put master in HOLDOVER state
				masterResource := fmt.Sprintf("%s/%s", ptpStats[master].Alias(), MasterClockType)
				p.PublishEvent(syncState, ptpStats[master].LastOffset(), masterResource, ptp.PtpStateChange)
				ptpStats[master].SetLastSyncState(syncState)
				UpdateSyncStateMetrics(ptpStats[master].ProcessName(), ptpStats[master].Alias(), syncState)

				//// Put CLOCK_REALTIME in FREERUN state
				p.PublishEvent(ptp.FREERUN, ptpStats[ClockRealTime].LastOffset(), ClockRealTime, ptp.PtpStateChange)
				ptpStats[ClockRealTime].SetLastSyncState(ptp.FREERUN)
				UpdateSyncStateMetrics(ptpStats[ClockRealTime].ProcessName(), ClockRealTime, syncState)

				threshold := p.PtpThreshold(ptp4lCfg.Profile)
				go func(ptpManager *PTPEventManager, holdoverTimeout int64, ptpIFace string, role types.PtpPortRole, c chan struct{}) {
					defer func() {
						log.Infof("exiting holdover for interface %s", ptpIFace)
						if r := recover(); r != nil {
							fmt.Println("Recovered in f", r)
						}
					}()

					select {
					case <-c:
						log.Infof("call recieved to close holderover timeout")
						return
					case <-time.After(time.Duration(holdoverTimeout) * time.Second):
						log.Infof("time expired for interface %s", ptpIFace)
						if s, found := ptpStats[master]; found {
							if s.LastSyncState() == ptp.HOLDOVER {
								log.Infof("HOLDOVER timeout after %d secs,setting clock state to FREERUN from HOLDOVER state for %s",
									holdoverTimeout, master)
								masterResource := fmt.Sprintf("%s/%s", ptpStats[master].Alias(), MasterClockType)
								p.PublishEvent(ptp.FREERUN, ptpStats[MasterClockType].LastOffset(), masterResource, ptp.PtpStateChange)
								ptpStats[MasterClockType].SetLastSyncState(ptp.FREERUN)
								UpdateSyncStateMetrics(s.ProcessName(), s.Alias(), ptp.FREERUN)
								//s.reset()
							}
						} else {
							log.Errorf("failed to switch from holdover, could not find stats for interface %s", ptpIFace)
						}
					}
				}(p, threshold.HoldOverTimeout, MasterClockType, lastRole, threshold.Close)
			}
		}
	}
}

// GenPTPEvent ... generate events form the logs
func (p *PTPEventManager) GenPTPEvent(profileName string, stats *stats.Stats, eventResourceName string, ptpOffset int64, clockState ptp.SyncState, eventType ptp.EventType) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("Recovered in f", r)
		}
	}()
	if clockState == "" {
		return
	}

	lastClockState := stats.LastSyncState()
	threshold := p.PtpThreshold(profileName)
	switch clockState {
	case ptp.LOCKED:
		switch lastClockState {
		case ptp.FREERUN: //last state was already sent for FreeRUN , but if its within then send again with new state
			if isOffsetInRange(ptpOffset, threshold.MaxOffsetThreshold, threshold.MinOffsetThreshold) { // within range
				log.Infof(" publishing event for %s with last state %s and current clock state %s and offset %d for ( Max/Min Threshold %d/%d )",
					eventResourceName, stats.LastSyncState(), clockState, ptpOffset, threshold.MaxOffsetThreshold, threshold.MinOffsetThreshold)
				p.PublishEvent(clockState, ptpOffset, eventResourceName, eventType) // change to locked
				stats.SetLastSyncState(clockState)
				stats.SetLastOffset(int64(ptpOffset))
				stats.AddValue(int64(ptpOffset)) // update off set when its in locked state and hold over only
			}
		case ptp.LOCKED: // last state was in sync , check if it is out of sync now
			if isOffsetInRange(ptpOffset, threshold.MaxOffsetThreshold, threshold.MinOffsetThreshold) {
				stats.SetLastOffset(int64(ptpOffset))
				stats.AddValue(int64(ptpOffset)) // update off set when its in locked state and hold over only
			} else {
				log.Infof(" publishing event for %s with last state %s and current clock state %s and offset %d for ( Max/Min Threshold %d/%d )",
					eventResourceName, stats.LastSyncState(), clockState, ptpOffset, threshold.MaxOffsetThreshold, threshold.MinOffsetThreshold)
				p.PublishEvent(ptp.FREERUN, ptpOffset, eventResourceName, eventType)
				stats.SetLastSyncState(ptp.FREERUN)
				stats.SetLastOffset(int64(ptpOffset))
			}
		case ptp.HOLDOVER:
			//do nothing, the timeout will switch holdover to FREERUN
		default: // not yet used states
			log.Warnf("unknown %s sync state %s ,has last ptp state %s", eventResourceName, clockState, lastClockState)
			if !isOffsetInRange(ptpOffset, threshold.MaxOffsetThreshold, threshold.MinOffsetThreshold) {
				clockState = ptp.FREERUN
			}
			log.Infof(" publishing event for %s with last state %s and current clock state %s and offset %d for ( Max/Min Threshold %d/%d )",
				eventResourceName, stats.LastSyncState(), clockState, ptpOffset, threshold.MaxOffsetThreshold, threshold.MinOffsetThreshold)
			p.PublishEvent(clockState, ptpOffset, eventResourceName, eventType) // change to unknown
			stats.SetLastSyncState(clockState)
			stats.SetLastOffset(int64(ptpOffset))
		}
	case ptp.FREERUN:
		if lastClockState != ptp.FREERUN { // within range
			log.Infof(" publishing event for %s with last state %s and current clock state %s and offset %d for ( Max/Min Threshold %d/%d )",
				eventResourceName, stats.LastSyncState(), clockState, ptpOffset, threshold.MaxOffsetThreshold, threshold.MinOffsetThreshold)
			p.PublishEvent(clockState, ptpOffset, eventResourceName, eventType) // change to locked
			stats.SetLastSyncState(clockState)
			stats.SetLastOffset(int64(ptpOffset))
			stats.AddValue(int64(ptpOffset))
		}
	default:
		log.Warnf("%s unknown current ptp state %s", eventResourceName, clockState)
		if ptpOffset > threshold.MaxOffsetThreshold || ptpOffset < threshold.MinOffsetThreshold {
			clockState = ptp.FREERUN
		}
		log.Infof(" publishing event for %s with last state %s and current clock state %s and offset %d for ( Max/Min Threshold %d/%d )",
			eventResourceName, stats.LastSyncState(), clockState, ptpOffset, threshold.MaxOffsetThreshold, threshold.MinOffsetThreshold)
		p.PublishEvent(clockState, ptpOffset, eventResourceName, ptp.PtpStateChange) // change to unknown state
		stats.SetLastSyncState(clockState)
		stats.SetLastOffset(int64(ptpOffset))
	}
}

//PublishEvent ...publish events
func (p *PTPEventManager) PublishEvent(state ptp.SyncState, ptpOffset int64, eventResourceName string, eventType ptp.EventType) {
	// create an event
	if state == "" {
		return
	}
	source := fmt.Sprintf("/cluster/%s/ptp/%s", p.nodeName, eventResourceName)
	data := ceevent.Data{
		Version: "v1",
		Values: []ceevent.DataValue{{
			Resource:  string(p.publisherTypes[eventType].Resource),
			DataType:  ceevent.NOTIFICATION,
			ValueType: ceevent.ENUMERATION,
			Value:     state,
		}, {
			Resource:  string(p.publisherTypes[eventType].Resource),
			DataType:  ceevent.METRIC,
			ValueType: ceevent.DECIMAL,
			Value:     float64(ptpOffset),
		},
		},
	}
	if pubs, ok := p.publisherTypes[eventType]; ok {
		e, err := common.CreateEvent(pubs.PubID, string(eventType), source, data)
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
	} else {
		log.Errorf("failed to publish ptp event due to missing publisher for type %s", string(eventType))
	}
}

// UpdatePTPMetrics ...
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

// DeleteThresholdMetrics .. delete threshold metrics
func DeleteThresholdMetrics(profile string) {
	Threshold.Delete(prometheus.Labels{
		"threshold": "MinOffsetThreshold", "node": ptpNodeName, "profile": profile})
	Threshold.Delete(prometheus.Labels{
		"threshold": "MaxOffsetThreshold", "node": ptpNodeName, "profile": profile})
	Threshold.Delete(prometheus.Labels{
		"threshold": "HoldOverTimeout", "node": ptpNodeName, "profile": profile})
}

// UpdateSyncStateMetrics ...
func UpdateSyncStateMetrics(process string, iface string, state ptp.SyncState) {
	var clockState float64
	if state == ptp.LOCKED {
		clockState = 1
	} else if state == ptp.FREERUN {
		clockState = 0
	} else if state == ptp.HOLDOVER {
		clockState = 2
	}
	SyncState.With(prometheus.Labels{
		"process": process, "node": ptpNodeName, "iface": iface}).Set(clockState)

}

//UpdateInterfaceRoleMetrics ...
func UpdateInterfaceRoleMetrics(process, ptpInterface string, role types.PtpPortRole) {
	InterfaceRole.With(prometheus.Labels{
		"process": process, "node": ptpNodeName, "iface": ptpInterface}).Set(float64(role))
}

//DeleteInterfaceRoleMetrics ...
func DeleteInterfaceRoleMetrics(process, ptpInterface string) {
	InterfaceRole.Delete(prometheus.Labels{
		"process": process, "node": ptpNodeName, "iface": ptpInterface})
}

func extractSummaryMetrics(processName, output string) (iface string, ptpOffset, maxPtpOffset, frequencyAdjustment, delay float64) {
	// remove everything before the rms string
	// This makes the output to equal
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
	} else if fields[1] != ClockRealTime {
		// phc2sys[5196755.139]: [ptp4l.0.config] ens5f0 rms 3152778 max 3152778 freq -6083928 +/-   0 delay  2791 +/-   0
		return // do not register offset value for master (interface) port reported by phc2sys
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

func extractRegularMetrics(processName, output string) (interfaceName string, ptpOffset, maxPtpOffset, frequencyAdjustment, delay float64, clockState ptp.SyncState) {
	// remove everything before the rms string
	// This makes the out to equal
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
		clockState = ptp.FREERUN
	case clockStep:
		clockState = ptp.FREERUN
	case locked:
		clockState = ptp.LOCKED
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
		clockState = ptp.HOLDOVER
		log.Warningf("no delay from master process %s out of sync", processName)
	}

	return
}

// ExtractPTP4lEvent ... extract event form ptp4l logs
func extractPTP4lEventState(output string) (portID int, role types.PtpPortRole, clockState ptp.SyncState) {
	// This makes the out to equal
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

	clockState = ptp.FREERUN
	if strings.Contains(output, "UNCALIBRATED to SLAVE") {
		role = types.SLAVE
	} else if strings.Contains(output, "UNCALIBRATED to PASSIVE") || strings.Contains(output, "MASTER to PASSIVE") ||
		strings.Contains(output, "SLAVE to PASSIVE") || strings.Contains(output, "LISTENING to PASSIVE") {
		role = types.PASSIVE
	} else if strings.Contains(output, "UNCALIBRATED to MASTER") || strings.Contains(output, "LISTENING to MASTER") {
		role = types.MASTER
	} else if strings.Contains(output, "FAULT_DETECTED") || strings.Contains(output, "SYNCHRONIZATION_FAULT") {
		role = types.FAULTY
		clockState = ptp.HOLDOVER
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
