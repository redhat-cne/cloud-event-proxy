package metrics

import (
	"fmt"
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
	mockEvent      ptp.EventType
	// PtpConfigMapUpdates holds ptp-configmap updated details
	PtpConfigMapUpdates *ptpConfig.LinuxPTPConfigMapUpdate
	// Ptp4lConfigInterfaces holds interfaces and its roles , after reading from ptp4l config files
	Ptp4lConfigInterfaces map[types.ConfigName]*ptp4lconf.PTP4lConfig
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
		Name: string(configName),
	}
	pc.Interfaces = []*ptp4lconf.PTPInterface{}
	p.AddPTPConfig(configName, pc)
	return pc
}

// GetStatsForInterface ... get stats for interface
func (p *PTPEventManager) GetStatsForInterface(name types.ConfigName, iface types.IFace) *stats.Stats {
	var found bool
	if _, found = p.Stats[name]; !found {
		p.Stats[name] = make(stats.PTPStats)
		p.Stats[name][iface] = stats.NewStats(string(name))
	} else if _, found = p.Stats[name][iface]; !found {
		p.Stats[name][iface] = stats.NewStats(string(name))
	}
	return p.Stats[name][iface]
}

// GetStats ... get stats
func (p *PTPEventManager) GetStats(name types.ConfigName) stats.PTPStats {
	if _, found := p.Stats[name]; !found {
		p.Stats[name] = make(stats.PTPStats)
	}
	return p.Stats[name]
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
		p.mockEvent = eventType
		log.Infof("PublishClockClassEvent clockClass=%f, source=%s, eventType=%s", clockClass, source, eventType)
		return
	}
	data := p.GetPTPEventsData(ptp.LOCKED, int64(clockClass), source, eventType)
	resourceAddress := fmt.Sprintf(p.resourcePrefix, p.nodeName, string(p.publisherTypes[eventType].Resource))
	p.publish(*data, resourceAddress, eventType)
}

// PublishClockClassEvent ...publish events
func (p *PTPEventManager) publishGNSSEvent(state int64, offset float64, source string, eventType ptp.EventType) {
	if p.mock {
		p.mockEvent = eventType
		log.Infof("publishGNSSEvent state=%d, offset=%f, source=%s, eventType=%s", state, offset, source, eventType)
		return
	}
	var data *ceevent.Data
	if state < 3 {
		data = p.GetPTPEventsData(ptp.LOCKED, int64(offset), source, eventType)
	} else {
		data = p.GetPTPEventsData(ptp.FREERUN, int64(offset), source, eventType)
	}

	resourceAddress := fmt.Sprintf(p.resourcePrefix, p.nodeName, string(p.publisherTypes[eventType].Resource))
	p.publish(*data, resourceAddress, eventType)
}

// GetPTPEventsData ... get PTP event data object
func (p *PTPEventManager) GetPTPEventsData(state ptp.SyncState, ptpOffset int64, source string, eventType ptp.EventType) *ceevent.Data {
	// create an event
	if state == "" {
		return nil
	}
	// /cluster/xyz/ptp/CLOCK_REALTIME this is not address the event is published to
	eventSource := fmt.Sprintf(p.resourcePrefix, p.nodeName, fmt.Sprintf("/%s", source))
	data := ceevent.Data{
		Version: "v1",
		Values:  []ceevent.DataValue{},
	}
	if eventType != ptp.PtpClockClassChange {
		data.Values = append(data.Values, ceevent.DataValue{
			Resource:  eventSource,
			DataType:  ceevent.NOTIFICATION,
			ValueType: ceevent.ENUMERATION,
			Value:     state,
		})
	}

	data.Values = append(data.Values, ceevent.DataValue{
		Resource:  eventSource,
		DataType:  ceevent.METRIC,
		ValueType: ceevent.DECIMAL,
		Value:     ptpOffset,
	})
	return &data
}

// GetPTPCloudEvents ...GetEvent events
func (p *PTPEventManager) GetPTPCloudEvents(data ceevent.Data, eventType ptp.EventType) *cloudevents.Event {
	resourceAddress := fmt.Sprintf(p.resourcePrefix, p.nodeName, string(p.publisherTypes[eventType].Resource))
	if pubs, ok := p.publisherTypes[eventType]; ok {
		cneEvent, cneErr := common.CreateEvent(pubs.PubID, string(eventType), resourceAddress, data)
		if cneErr != nil {
			log.Errorf("failed to create ptp event, %s", cneErr)
			return nil
		}
		if ceEvent, err := common.GetPublishingCloudEvent(p.scConfig, cneEvent); err == nil {
			// the saw because api is not processing this, returned  directly by currentState call
			return ceEvent
		}
	}
	return nil
}

// PublishEvent ...publish events
func (p *PTPEventManager) PublishEvent(state ptp.SyncState, ptpOffset int64, source string, eventType ptp.EventType) {
	// create an event
	if state == "" {
		return
	}
	if p.mock {
		p.mockEvent = eventType
		log.Infof("PublishEvent state=%s, ptpOffset=%d, source=%s, eventType=%s", state, ptpOffset, source, eventType)
		return
	}
	// /cluster/xyz/ptp/CLOCK_REALTIME this is not address the event is published to
	data := p.GetPTPEventsData(state, ptpOffset, source, eventType)
	resourceAddress := fmt.Sprintf(p.resourcePrefix, p.nodeName, string(p.publisherTypes[eventType].Resource))
	p.publish(*data, resourceAddress, eventType)
}

func (p *PTPEventManager) publish(data ceevent.Data, eventSource string, eventType ptp.EventType) {
	if pubs, ok := p.publisherTypes[eventType]; ok {
		e, err := common.CreateEvent(pubs.PubID, string(eventType), eventSource, data)
		if err != nil {
			log.Errorf("failed to create ptp event, %s", err)
			return
		}
		if err = common.PublishEventViaAPI(p.scConfig, e); err != nil {
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
				p.PublishEvent(clockState, ptpOffset, eventResourceName, eventType) // change to locked
				oStats.SetLastSyncState(clockState)
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
					ptpProfileName, eventResourceName, oStats.LastSyncState(), clockState, ptpOffset, threshold.MaxOffsetThreshold, threshold.MinOffsetThreshold)
				p.PublishEvent(clockState, ptpOffset, eventResourceName, eventType)
				oStats.SetLastSyncState(clockState)
				oStats.SetLastOffset(ptpOffset)
			}
		case ptp.HOLDOVER:
			// do nothing, the timeout will switch holdover to FREE-RUN
		default: // not yet used states
			log.Warnf("%s sync state %s, last ptp state is unknown: %s", eventResourceName, clockState, lastClockState)
			if !isOffsetInRange(ptpOffset, threshold.MaxOffsetThreshold, threshold.MinOffsetThreshold) {
				clockState = ptp.FREERUN
			}
			log.Infof(" publishing event for (profile %s) %s with last state %s and current clock state %s and offset %d for ( Max/Min Threshold %d/%d )",
				ptpProfileName, eventResourceName, oStats.LastSyncState(), clockState, ptpOffset, threshold.MaxOffsetThreshold, threshold.MinOffsetThreshold)
			p.PublishEvent(clockState, ptpOffset, eventResourceName, eventType) // change to unknown
			oStats.SetLastSyncState(clockState)
			oStats.SetLastOffset(ptpOffset)
		}
	case ptp.FREERUN:
		if lastClockState != ptp.FREERUN { // within range
			log.Infof(" publishing event for (profile %s) %s with last state %s and current clock state %s and offset %d for ( Max/Min Threshold %d/%d )",
				ptpProfileName, eventResourceName, oStats.LastSyncState(), clockState, ptpOffset, threshold.MaxOffsetThreshold, threshold.MinOffsetThreshold)
			p.PublishEvent(clockState, ptpOffset, eventResourceName, eventType) // change to locked
			oStats.SetLastSyncState(clockState)
			oStats.SetLastOffset(ptpOffset)
			oStats.AddValue(ptpOffset)
		}
	default:
		log.Warnf("%s unknown current ptp state %s", eventResourceName, clockState)
		if !isOffsetInRange(ptpOffset, threshold.MaxOffsetThreshold, threshold.MinOffsetThreshold) {
			clockState = ptp.FREERUN
		}
		log.Infof(" publishing event for (profile %s) %s with last state %s and current clock state %s and offset %d for ( Max/Min Threshold %d/%d )",
			ptpProfileName, eventResourceName, oStats.LastSyncState(), clockState, ptpOffset, threshold.MaxOffsetThreshold, threshold.MinOffsetThreshold)
		p.PublishEvent(clockState, ptpOffset, eventResourceName, ptp.PtpStateChange) // change to unknown state
		oStats.SetLastSyncState(clockState)
		oStats.SetLastOffset(ptpOffset)
	}
}

// NodeName ...
func (p *PTPEventManager) NodeName() string {
	return p.nodeName
}

// GetMockEvent ...
func (p *PTPEventManager) GetMockEvent() ptp.EventType {
	return p.mockEvent
}

// ResetMockEvent ...
func (p *PTPEventManager) ResetMockEvent() {
	p.mockEvent = ""
}
