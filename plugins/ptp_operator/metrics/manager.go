package metrics

import (
	"fmt"
	"path"
	"strings"
	"sync"

	cloudevents "github.com/cloudevents/sdk-go/v2"

	"github.com/redhat-cne/cloud-event-proxy/pkg/common"
	ptpConfig "github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/config"
	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/ptp4lconf"
	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/stats"
	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/types"
	ceevent "github.com/redhat-cne/sdk-go/pkg/event"
	"github.com/redhat-cne/sdk-go/pkg/event/ptp"
	log "github.com/sirupsen/logrus"
)

// PTPEventManager ... for PTP
type PTPEventManager struct {
	resourcePrefix string
	publisherTypes map[ptp.EventType]*types.EventPublisherType
	nodeName       string
	scConfig       *common.SCConfiguration
	lock           sync.RWMutex
	Stats          map[types.ConfigName]stats.PTPStats
	mock           bool
	mockEvent      []ptp.EventType
	// PtpConfigMapUpdates holds ptp-configmap updated details
	PtpConfigMapUpdates *ptpConfig.LinuxPTPConfigMapUpdate
	// Ptp4lConfigInterfaces holds interfaces and its roles, after reading from ptp4l config files
	Ptp4lConfigInterfaces map[types.ConfigName]*ptp4lconf.PTP4lConfig
	lastOverallSyncState  ptp.SyncState
}

// NewPTPEventManager to manage events and metrics
func NewPTPEventManager(resourcePrefix string, publisherTypes map[ptp.EventType]*types.EventPublisherType,
	nodeName string, config *common.SCConfiguration) (ptpEventManager *PTPEventManager) {
	ptpEventManager = &PTPEventManager{
		resourcePrefix:        resourcePrefix,
		publisherTypes:        publisherTypes,
		nodeName:              nodeName,
		scConfig:              config,
		lock:                  sync.RWMutex{},
		Stats:                 map[types.ConfigName]stats.PTPStats{},
		Ptp4lConfigInterfaces: make(map[types.ConfigName]*ptp4lconf.PTP4lConfig),
		mock:                  false,
	}
	// attach ptp config updates
	ptpEventManager.PtpConfigMapUpdates = ptpConfig.NewLinuxPTPConfUpdate()
	return
}

// PtpThreshold ... return ptp threshold
// resetCh will reset any closed channel
func (p *PTPEventManager) PtpThreshold(profileName string, resetCh bool) ptpConfig.PtpClockThreshold {
	if t, found := p.PtpConfigMapUpdates.EventThreshold[profileName]; found {
		if resetCh {
			t.Close = make(chan struct{}) // reset channel to new
		}
		return ptpConfig.PtpClockThreshold{
			HoldOverTimeout:    t.HoldOverTimeout,
			MaxOffsetThreshold: t.MaxOffsetThreshold,
			MinOffsetThreshold: t.MinOffsetThreshold,
			Close:              t.Close,
		}
	} else if len(p.PtpConfigMapUpdates.EventThreshold) > 0 { // if not found get the first item since one per config)
		for _, t := range p.PtpConfigMapUpdates.EventThreshold {
			if resetCh {
				t.Close = make(chan struct{})
			}
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

// MockTest ... use for test only
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
func (p *PTPEventManager) AddPTPConfig(fileName types.ConfigName, ptpCfg *ptp4lconf.PTP4lConfig) {
	p.lock.Lock()
	p.Ptp4lConfigInterfaces[fileName] = ptpCfg
	p.lock.Unlock()
}

// GetPTPConfig ... Add PtpConfigUpdate obj
func (p *PTPEventManager) GetPTPConfig(configName types.ConfigName) *ptp4lconf.PTP4lConfig {
	if _, ok := p.Ptp4lConfigInterfaces[configName]; ok && p.Ptp4lConfigInterfaces[configName] != nil {
		return p.Ptp4lConfigInterfaces[configName]
	}
	pc := &ptp4lconf.PTP4lConfig{
		Name:    string(configName),
		Profile: "",
	}
	pc.Interfaces = []*ptp4lconf.PTPInterface{}
	p.AddPTPConfig(configName, pc)
	return pc
}

// GetPTPConfigByProfile ...
func (p *PTPEventManager) GetPTPConfigByProfile(profile string) string {
	for _, config := range p.Ptp4lConfigInterfaces {
		if config.Profile == profile {
			return config.Name
		}
	}
	return ""
}

// GetPTPConfigDeepCopy  ... Add PtpConfigUpdate obj
func (p *PTPEventManager) GetPTPConfigDeepCopy(configName types.ConfigName) *ptp4lconf.PTP4lConfig {
	if _, ok := p.Ptp4lConfigInterfaces[configName]; ok && p.Ptp4lConfigInterfaces[configName] != nil {
		pc := &ptp4lconf.PTP4lConfig{
			Name:       p.Ptp4lConfigInterfaces[configName].Name,
			Profile:    p.Ptp4lConfigInterfaces[configName].Profile,
			Interfaces: []*ptp4lconf.PTPInterface{},
		}
		pc.Interfaces = append(pc.Interfaces, p.Ptp4lConfigInterfaces[configName].Interfaces...)
		return pc
	}
	pc := &ptp4lconf.PTP4lConfig{
		Name:    string(configName),
		Profile: "",
	}

	return pc
}

// GetStatsForInterface ... get stats for interface
func (p *PTPEventManager) GetStatsForInterface(name types.ConfigName, iface types.IFace) *stats.Stats {
	p.lock.RLock()
	if statsForName, found := p.Stats[name]; found {
		if stat, found2 := statsForName[iface]; found2 {
			p.lock.RUnlock()
			return stat
		}
	}
	p.lock.RUnlock()

	p.lock.Lock()
	defer p.lock.Unlock()
	if _, found := p.Stats[name]; !found {
		p.Stats[name] = make(stats.PTPStats)
	}
	if _, found := p.Stats[name][iface]; !found {
		p.Stats[name][iface] = stats.NewStats(string(name))
	}
	return p.Stats[name][iface]
}

// GetStats ... get stats
func (p *PTPEventManager) GetStats(name types.ConfigName) stats.PTPStats {
	p.lock.RLock()
	if stat, found := p.Stats[name]; found {
		p.lock.RUnlock()
		return stat
	}
	p.lock.RUnlock()

	p.lock.Lock()
	defer p.lock.Unlock()
	if _, found := p.Stats[name]; !found {
		p.Stats[name] = make(stats.PTPStats)
	}
	return p.Stats[name]
}

// SetStats sets the stats for a given config name.
func (p *PTPEventManager) SetStats(configName types.ConfigName, s stats.PTPStats) {
	p.lock.Lock()
	defer p.lock.Unlock()
	p.Stats[configName] = s
}

// DeletePTPConfig ... delete ptp obj
func (p *PTPEventManager) DeletePTPConfig(key types.ConfigName) {
	p.lock.Lock()
	delete(p.Ptp4lConfigInterfaces, key)
	p.lock.Unlock()
}

// PublishClockClassEvent ...publish events
func (p *PTPEventManager) PublishClockClassEvent(clockClass float64, source string, eventType ptp.EventType) {
	if p.mock {
		p.mockEvent = []ptp.EventType{eventType}
		log.Infof("PublishClockClassEvent clockClass=%f, source=%s, eventType=%s", clockClass, source, eventType)
		return
	}
	data := p.GetPTPEventsData(ptp.LOCKED, int64(clockClass), source, eventType)
	resourceAddress := path.Join(p.resourcePrefix, p.nodeName, string(p.publisherTypes[eventType].Resource))
	p.publish(*data, resourceAddress, eventType)
}

// PublishClockClassEvent ...publish events
func (p *PTPEventManager) publishGNSSEvent(state int64, offset float64, syncState ptp.SyncState, source string, eventType ptp.EventType) {
	if p.mock {
		p.mockEvent = []ptp.EventType{eventType}
		log.Infof("publishGNSSEvent state=%d, offset=%f, source=%s, eventType=%s", state, offset, source, eventType)
		return
	}
	var data *ceevent.Data
	gpsFixState := p.GetGPSFixState(state, syncState)
	data = p.GetPTPEventsData(gpsFixState, int64(offset), source, eventType)
	data.Values = append(data.Values, ceevent.DataValue{
		Resource:  fmt.Sprintf("%s/%s", data.Values[0].GetResource(), "gpsFix"),
		DataType:  ceevent.METRIC,
		ValueType: ceevent.DECIMAL,
		Value:     state,
	})
	resourceAddress := path.Join(p.resourcePrefix, p.nodeName, string(p.publisherTypes[eventType].Resource))
	p.publish(*data, resourceAddress, eventType)
}

// publishSyncEEvent ...publish events
func (p *PTPEventManager) publishSyncEEvent(syncState ptp.SyncState, source string, ql byte, extQl byte, extendedTvlEnabled bool, eventType ptp.EventType) {
	if p.mock {
		p.mockEvent = []ptp.EventType{eventType}
		log.Infof("publishSyncEEvent state=%s, source=%s, eventType=%s", syncState, source, eventType)
		return
	}
	if _, ok := p.publisherTypes[eventType]; !ok {
		log.Infof("cannot publish event, resource not found  for %s", eventType)
		return
	}

	data := &ceevent.Data{
		Version: ceevent.APISchemaVersion,
		Values:  []ceevent.DataValue{},
	}
	resource := path.Join(p.resourcePrefix, p.nodeName, source, "Ql")
	if syncState == "" { // clock quality event
		data.Values = append(data.Values, ceevent.DataValue{
			Resource:  resource,
			DataType:  ceevent.METRIC,
			ValueType: ceevent.DECIMAL,
			Value:     float64(ql),
		})
		resource = path.Join(p.resourcePrefix, p.nodeName, source, "extQl")
		if !extendedTvlEnabled { // have the default value for clarity
			data.Values = append(data.Values, ceevent.DataValue{
				Resource:  resource,
				DataType:  ceevent.METRIC,
				ValueType: ceevent.DECIMAL,
				Value:     int64(0xFF), // default
			})
		} else {
			data.Values = append(data.Values, ceevent.DataValue{
				Resource:  resource,
				DataType:  ceevent.METRIC,
				ValueType: ceevent.DECIMAL,
				Value:     int64(extQl), // default
			})
		}
	} else {
		data.Values = append(data.Values, ceevent.DataValue{
			Resource:  path.Join(p.resourcePrefix, p.nodeName, source),
			DataType:  ceevent.METRIC,
			ValueType: ceevent.DECIMAL,
			Value:     syncState,
		})
	}
	resourceAddress := path.Join(p.resourcePrefix, p.nodeName, string(p.publisherTypes[eventType].Resource))
	p.publish(*data, resourceAddress, eventType)
}

// GetPTPEventsData ... get PTP event data object
func (p *PTPEventManager) GetPTPEventsData(state ptp.SyncState, ptpOffset int64, source string, eventType ptp.EventType) *ceevent.Data {
	// create an event
	if state == "" {
		return nil
	}
	// /cluster/xyz/ptp/CLOCK_REALTIME this is not address the event is published to
	eventSource := path.Join(p.resourcePrefix, p.nodeName, source)
	data := ceevent.Data{
		Version: ceevent.APISchemaVersion,
		Values:  []ceevent.DataValue{},
	}
	if eventType != ptp.PtpClockClassChange && eventType != ptp.SynceClockQualityChange {
		data.Values = append(data.Values, ceevent.DataValue{
			Resource:  eventSource,
			DataType:  ceevent.NOTIFICATION,
			ValueType: ceevent.ENUMERATION,
			Value:     state,
		})
	}
	if eventType != ptp.SyncStateChange && eventType != ptp.SynceStateChange {
		data.Values = append(data.Values, ceevent.DataValue{
			Resource:  eventSource,
			DataType:  ceevent.METRIC,
			ValueType: ceevent.DECIMAL,
			Value:     ptpOffset,
		})
	}
	return &data
}

// GetPTPCloudEvents ...GetEvent events
func (p *PTPEventManager) GetPTPCloudEvents(data ceevent.Data, eventType ptp.EventType) (*cloudevents.Event, error) {
	if pubs, ok := p.publisherTypes[eventType]; ok {
		cneEvent, cneErr := common.CreateEvent(
			pubs.PubID, string(eventType),
			path.Join(p.resourcePrefix, p.nodeName, string(p.publisherTypes[eventType].Resource)),
			data)
		if cneErr != nil {
			return nil, fmt.Errorf("failed to create ptp event, %s", cneErr)
		}
		ceEvent, err := common.GetPublishingCloudEvent(p.scConfig, cneEvent)
		if err != nil {
			return nil, err
		}
		return ceEvent, nil
	}
	return nil, fmt.Errorf("EventPublisherType not found for event type %s", string(eventType))
}

// PublishEvent ...publish events
func (p *PTPEventManager) PublishEvent(state ptp.SyncState, ptpOffset int64, source string, eventType ptp.EventType) {
	// create an event
	if state == "" {
		return
	}
	if p.mock {
		p.mockEvent = []ptp.EventType{eventType}
		if eventType == ptp.PtpStateChange || eventType == ptp.OsClockSyncStateChange {
			p.mockEvent = append(p.mockEvent, ptp.SyncStateChange)
		}
		log.Infof("PublishEvent state=%s, ptpOffset=%d, source=%s, eventType=%s", state, ptpOffset, source, eventType)
		return
	}

	// /cluster/xyz/ptp/CLOCK_REALTIME this is not address the event is published to
	data := p.GetPTPEventsData(state, ptpOffset, source, eventType)
	resourceAddress := path.Join(p.resourcePrefix, p.nodeName, string(p.publisherTypes[eventType].Resource))
	p.publish(*data, resourceAddress, eventType)
	// publish the event again as overall sync state
	// SyncStateChange is the overall sync state including PtpStateChange and OsClockSyncStateChange
	if eventType == ptp.PtpStateChange || eventType == ptp.OsClockSyncStateChange {
		nodeState := p.GetNodeSyncState(state)
		if state != p.lastOverallSyncState {
			eventType = ptp.SyncStateChange
			source = string(p.publisherTypes[eventType].Resource)
			data = p.GetPTPEventsData(state, ptpOffset, source, eventType)
			resourceAddress = path.Join(p.resourcePrefix, p.nodeName, source)
			p.publish(*data, resourceAddress, eventType)
			p.lastOverallSyncState = nodeState
		}
	}
}

func (p *PTPEventManager) publish(data ceevent.Data, resourceAddress string, eventType ptp.EventType) {
	var e ceevent.Event
	var err error
	if pubs, ok := p.publisherTypes[eventType]; ok {
		e, err = common.CreateEvent(pubs.PubID, string(eventType), string(p.publisherTypes[eventType].Resource), data)
		if err != nil {
			log.Errorf("failed to create ptp event, %s", err)
			return
		}
		if err = common.PublishEventViaAPI(p.scConfig, e, resourceAddress); err != nil {
			log.Errorf("failed to publish ptp event %v, %s", e, err)
			return
		}
	} else {
		log.Errorf("failed to publish ptp event due to missing publisher for type %s", string(eventType))
	}
}

// GenPTPEvent ... generate events form the logs
func (p *PTPEventManager) GenPTPEvent(ptpProfileName string, oStats *stats.Stats, eventResourceName string, ptpOffset int64, clockState ptp.SyncState, eventType ptp.EventType) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("Recovered in f", r)
		}
	}()
	if clockState == "" {
		return
	}

	lastClockState := oStats.LastSyncState()
	threshold := p.PtpThreshold(ptpProfileName, false)
	switch clockState {
	case ptp.LOCKED:
		switch lastClockState {
		case ptp.FREERUN: // last state was already sent for FreeRUN , but if its within then send again with new state
			if isOffsetInRange(ptpOffset, threshold.MaxOffsetThreshold, threshold.MinOffsetThreshold) { // within range
				log.Infof(" publishing event for ( profile %s) %s with last state %s and current clock state %s and offset %d for ( Max/Min Threshold %d/%d )",
					ptpProfileName, eventResourceName, lastClockState, clockState, ptpOffset, threshold.MaxOffsetThreshold, threshold.MinOffsetThreshold)
				oStats.SetLastSyncState(clockState)
				p.PublishEvent(clockState, ptpOffset, eventResourceName, eventType) // change to locked
				oStats.SetLastOffset(ptpOffset)
				oStats.AddValue(ptpOffset) // update off set when its in locked state and hold over only
			}
		case ptp.LOCKED: // last state was in sync , check if it is out of sync now
			if isOffsetInRange(ptpOffset, threshold.MaxOffsetThreshold, threshold.MinOffsetThreshold) {
				oStats.SetLastOffset(ptpOffset)
				oStats.AddValue(ptpOffset) // update off set when its in locked state and hold over only
			} else {
				clockState = ptp.FREERUN
				log.Infof(" publishing event for ( profile %s) %s with last state %s and current clock state %s and offset %d for ( Max/Min Threshold %d/%d )",
					ptpProfileName, eventResourceName, lastClockState, clockState, ptpOffset, threshold.MaxOffsetThreshold, threshold.MinOffsetThreshold)
				oStats.SetLastSyncState(clockState)
				p.PublishEvent(clockState, ptpOffset, eventResourceName, eventType)
				oStats.SetLastOffset(ptpOffset)
			}
		case ptp.HOLDOVER:
			//FOR OC/BC  do nothing, the timeout will switch holdover to FREE-RUN OR LOCKED  if it is not within the holdover timeout
			// FOR T-GM its handled differently
			// previous state was HOLDOVER, now it is in LOCKED state, cancel any HOLDOVER
			if isOffsetInRange(ptpOffset, threshold.MaxOffsetThreshold, threshold.MinOffsetThreshold) {
				log.Infof("interface %s is in LOCKED state, cancel any holdover states", eventResourceName)
				threshold.SafeClose()
				log.Infof(" publishing event for ( profile %s) %s with last state %s and current clock state %s and offset %d for ( Max/Min Threshold %d/%d )",
					ptpProfileName, eventResourceName, lastClockState, clockState, ptpOffset, threshold.MaxOffsetThreshold, threshold.MinOffsetThreshold)
				oStats.SetLastSyncState(clockState)
				p.PublishEvent(clockState, ptpOffset, eventResourceName, eventType) // change to locked
				oStats.SetLastOffset(ptpOffset)
				oStats.AddValue(ptpOffset) // update off set when its in locked state and hold over only/ update off set when its in locked state and hold over only
			} // else continue to stay in HOLDOVER UNTIL its really LOCKED state

		default: // not yet used states
			clockState = ptp.FREERUN
			if isOffsetInRange(ptpOffset, threshold.MaxOffsetThreshold, threshold.MinOffsetThreshold) {
				clockState = ptp.LOCKED
			}
			log.Warnf("%s sync state %s, last ptp state is unknown, setting to  %s", eventResourceName, lastClockState, clockState)

			log.Infof(" publishing event for (profile %s) %s with last state %s and current clock state %s and offset %d for ( Max/Min Threshold %d/%d )",
				ptpProfileName, eventResourceName, lastClockState, clockState, ptpOffset, threshold.MaxOffsetThreshold, threshold.MinOffsetThreshold)
			oStats.SetLastSyncState(clockState)
			p.PublishEvent(clockState, ptpOffset, eventResourceName, eventType) // change to unknown
			oStats.SetLastOffset(ptpOffset)
		}
	case ptp.HOLDOVER:
		oStats.SetLastSyncState(clockState)
		if lastClockState != ptp.HOLDOVER { //send event only once
			log.Infof(" publishing event for (profile %s) %s with last state %s and current clock state %s and offset %d )",
				ptpProfileName, eventResourceName, lastClockState, clockState, ptpOffset)
			p.PublishEvent(clockState, ptpOffset, eventResourceName, eventType)
		}
		oStats.SetLastOffset(ptpOffset)
		oStats.AddValue(ptpOffset)
		return
	case ptp.FREERUN:
		oStats.SetLastSyncState(clockState)
		if lastClockState != ptp.HOLDOVER {
			if lastClockState != ptp.FREERUN { // don't send event if last event was freerun
				log.Infof("publishing event for (profile %s) %s with last state %s and current clock state %s and offset %d for ( Max/Min Threshold %d/%d )",
					ptpProfileName, eventResourceName, lastClockState, clockState, ptpOffset, threshold.MaxOffsetThreshold, threshold.MinOffsetThreshold)
				p.PublishEvent(clockState, ptpOffset, eventResourceName, eventType)
			}
			oStats.SetLastOffset(ptpOffset)
			oStats.AddValue(ptpOffset)
		}
	default:
		clockState = ptp.FREERUN
		if isOffsetInRange(ptpOffset, threshold.MaxOffsetThreshold, threshold.MinOffsetThreshold) {
			clockState = ptp.LOCKED
		}
		log.Warnf("%s unknown current ptp state, setting to  %s", eventResourceName, clockState)
		log.Infof(" publishing event for (profile %s) %s with last state %s and current clock state %s and offset %d for ( Max/Min Threshold %d/%d )",
			ptpProfileName, eventResourceName, lastClockState, clockState, ptpOffset, threshold.MaxOffsetThreshold, threshold.MinOffsetThreshold)
		oStats.SetLastSyncState(clockState)
		p.PublishEvent(clockState, ptpOffset, eventResourceName, ptp.PtpStateChange) // change to unknown state
		oStats.SetLastOffset(ptpOffset)
	}
}

// NodeName ...
func (p *PTPEventManager) NodeName() string {
	return p.nodeName
}

// GetMockEvent ...
func (p *PTPEventManager) GetMockEvent() []ptp.EventType {
	return p.mockEvent
}

// ResetMockEvent ...
func (p *PTPEventManager) ResetMockEvent() {
	p.mockEvent = []ptp.EventType{}
}

// PrintStats .... for debug
func (p *PTPEventManager) PrintStats() string {
	b := strings.Builder{}
	index := 0
	for cfgname, s := range p.Stats {
		b.WriteString(string(rune(index)) + ") cfgName: " + string(cfgname) + "\n")
		for iface, ss := range s {
			b.WriteString("interface: " + string(iface) + "\n")
			b.WriteString("-------------------------------\n")
			b.WriteString("stats: " + ss.String() + "\n")
		}
		index++
	}
	return b.String()
}

// IsHAProfile ... if profile for ha found pass
func (p *PTPEventManager) IsHAProfile(name string) bool {
	// Check if PtpSettings exist, if so proceed with confidence
	return p.PtpConfigMapUpdates.HAProfile == name
}

// HAProfiles ... if profile for ha found pass the settings
func (p *PTPEventManager) HAProfiles() (profile string, profiles []string) {
	// Check if PtpSettings exist, if so proceed with confidence
	if p.PtpConfigMapUpdates.PtpSettings != nil {
		p.lock.RLock()
		defer p.lock.RUnlock()
		for profileName, ptpSettings := range p.PtpConfigMapUpdates.PtpSettings {
			if ptpSettings != nil {
				if haProfile, ok := ptpSettings["haProfiles"]; ok {
					profile = profileName
					rawProfiles := strings.Split(haProfile, ",")
					for _, hp := range rawProfiles {
						profiles = append(profiles, strings.TrimSpace(hp))
					}
					return
				}
			}
		}
	}
	return
}

func (p *PTPEventManager) ListHAProfilesWith(currentProfile string) (profile string, profiles []string) {
	// Check if PtpSettings exist, if so proceed
	currentProfile = strings.TrimSpace(currentProfile)
	if currentProfile == "" {
		return "", nil
	}
	if p.PtpConfigMapUpdates.PtpSettings != nil {
		profile, profiles = p.HAProfiles()
		for _, hp := range profiles {
			if hp == strings.TrimSpace(currentProfile) {
				// Return all profiles in the same HA group
				return profile, profiles
			}
		}
	}
	// No match found — return empty
	return "", nil
}

// GetNodeSyncState evaluates the node-level sync state based on FREERUN, HOLDOVER, and LOCKED.
// It returns the worst state among the available MasterClock and ClockRealTime stats.
func (p *PTPEventManager) GetNodeSyncState(currentState ptp.SyncState) ptp.SyncState {
	if currentState == "" {
		currentState = ptp.FREERUN
	}

	var finalState ptp.SyncState = ""
	found := false

	p.lock.RLock()
	defer p.lock.RUnlock()

	for _, ptpStats := range p.Stats {
		for iface, stat := range ptpStats {
			if iface != MasterClockType && iface != ClockRealTime {
				continue
			}
			s := stat.LastSyncState()
			if s != ptp.FREERUN && s != ptp.HOLDOVER && s != ptp.LOCKED {
				continue
			}
			finalState = OverallState(s, finalState)
			found = true
		}
	}

	if !found {
		// No usable stats, use currentState as fallback
		return currentState
	}

	// Compare with currentState and return the worst
	return OverallState(currentState, finalState)
}

func OverallState(current, updated ptp.SyncState) ptp.SyncState {
	if current == "" {
		return updated
	}
	switch updated {
	case ptp.FREERUN:
		return ptp.FREERUN
	case ptp.HOLDOVER:
		if current == ptp.FREERUN {
			return current
		}
		return updated
	case ptp.LOCKED:
		return current
	case "":
		return current
	default:
		log.Warnf("last sync state is unknown: %s", updated)
	}
	return ""
}
