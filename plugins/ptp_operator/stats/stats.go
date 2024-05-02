// Package stats ...
package stats

import (
	"fmt"
	"math"
	"strings"
	"sync"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/event"
	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/types"

	"github.com/redhat-cne/sdk-go/pkg/event/ptp"
)

// PTPStats ...
type PTPStats map[types.IFace]*Stats

// Stats calculates stats  nolint:unused
type Stats struct {
	configName             string
	offsetSource           string
	processName            string
	num                    int64
	max                    int64
	min                    int64
	mean                   int64
	sumSqr                 int64
	sumDiffSqr             int64
	frequencyAdjustment    int64
	delay                  int64
	lastOffset             int64
	lastSyncState          ptp.SyncState
	aliasName              string
	clackClass             int64
	role                   types.PtpPortRole
	ptpDependentEventState *event.PTPEventState
	configDeleted          bool
}

// AddValue ...add value
func (s *Stats) AddValue(val int64) {
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

// StDev ... set dev
func (s *Stats) StDev() float64 { //nolint:unused
	if s.num > 0 {
		return math.Sqrt(float64(s.sumDiffSqr / s.num))
	}
	return 1
}

// MaxAbs ... get Max abs value
func (s *Stats) MaxAbs() int64 {
	if s.max > s.min {
		return s.max
	}
	return s.min
}

// OffsetSource ... get offset source
func (s *Stats) OffsetSource() string {
	return s.offsetSource
}

// SetOffsetSource ... set offset source ptp4/phc2sys/master
func (s *Stats) SetOffsetSource(os string) {
	s.offsetSource = os
}

// ProcessName ... name of the process either ptp4l or phc2sys
func (s *Stats) ProcessName() string {
	return s.processName
}

// SetProcessName ... set process name
func (s *Stats) SetProcessName(processName string) {
	s.processName = processName
}

// Offset return last known offset
func (s *Stats) Offset() int64 {
	return s.lastOffset
}

// ClockClass return last known ClockClass
func (s *Stats) ClockClass() int64 {
	return s.clackClass
}

// Alias return alias name
func (s *Stats) Alias() string {
	return s.aliasName
}

// SetAlias ... set alias name for slave port
func (s *Stats) SetAlias(val string) {
	s.aliasName = val
}

// SyncState return last known SyncState state
func (s *Stats) SyncState() ptp.SyncState {
	return s.lastSyncState
}

// ConfigName ...get config name
func (s *Stats) ConfigName() string {
	return s.configName
}

// reset status
func (s *Stats) reset() { //nolint:unused
	s.num = 0
	s.max = 0
	s.mean = 0
	s.min = 0
	s.sumDiffSqr = 0
	s.sumSqr = 0
	s.aliasName = ""
	s.role = types.UNKNOWN
	s.frequencyAdjustment = 0
	s.delay = 0
	s.clackClass = 0
	s.configDeleted = false
	s.processName = ""
	s.lastOffset = 0
	s.offsetSource = ""
}

// NewStats ... create new stats
func NewStats(configName string) *Stats {
	return &Stats{configName: configName}
}

// SetFrequencyAdjustment ... set frequency adjustment
func (s *Stats) SetFrequencyAdjustment(val int64) {
	s.frequencyAdjustment = val
}

// SetDelay ... set delay value
func (s *Stats) SetDelay(val int64) {
	s.delay = val
}

// SetLastOffset ... set last offset value
func (s *Stats) SetLastOffset(val int64) {
	s.lastOffset = val
}

// SetClockClass ... set last clock class value
func (s *Stats) SetClockClass(val int64) {
	s.clackClass = val
}

// SetLastSyncState ... set last sync state
func (s *Stats) SetLastSyncState(val ptp.SyncState) {
	s.lastSyncState = val
}

// FrequencyAdjustment ... get frequency adjustment value
func (s *Stats) FrequencyAdjustment() int64 {
	return s.frequencyAdjustment
}

// Delay ... get delay value
func (s *Stats) Delay() int64 {
	return s.delay
}

// LastOffset ... last offset value
func (s *Stats) LastOffset() int64 {
	return s.lastOffset
}

// LastSyncState ... last sync state
func (s *Stats) LastSyncState() ptp.SyncState {
	return s.lastSyncState
}

// SetRole ... set role name
func (s *Stats) SetRole(role types.PtpPortRole) {
	s.role = role
}

// Role ... get role name
func (s *Stats) Role() types.PtpPortRole {
	return s.role
}

// PtpDependentEventState ... get ptp dependent event state
func (s *Stats) PtpDependentEventState() *event.PTPEventState {
	return s.ptpDependentEventState
}

// GetCurrentDependentEventState ... get current dependent event state
func (s *Stats) GetCurrentDependentEventState() ptp.SyncState {
	if s.ptpDependentEventState == nil {
		return ptp.LOCKED
	}
	return s.ptpDependentEventState.CurrentPTPStateEvent
}

// SetPtpDependentEventState ... set ptp dependent event state
func (s *Stats) SetPtpDependentEventState(e event.ClockState, metrics map[string]*event.PMetric, help map[string]string) {
	if s.ptpDependentEventState == nil {
		s.ptpDependentEventState = &event.PTPEventState{
			Mutex:                sync.Mutex{},
			CurrentPTPStateEvent: "",
			DependsOn:            map[string]event.DependingClockState{},
			Type:                 "",
		}
	}
	if !s.configDeleted {
		s.ptpDependentEventState.UpdateCurrentEventState(e, metrics, help)
	}
}

// GetStateState ... get state
func (s *Stats) GetStateState(processName string, iface *string) (ptp.SyncState, error) {
	if s.ptpDependentEventState != nil && s.ptpDependentEventState.DependsOn != nil {
		if d, ok := s.ptpDependentEventState.DependsOn[processName]; ok {
			if iface == nil && len(d) > 0 {
				return d[0].State, nil
			}
			for _, state := range d {
				if *state.IFace == *iface {
					return state.State, nil
				}
			}
		}
	}
	return ptp.FREERUN, fmt.Errorf("sync state not found %s", processName)
}

// HasProcessEnabled ... check if process is enabled
func (s *Stats) HasProcessEnabled(processName string) bool {
	if s.ptpDependentEventState != nil && s.ptpDependentEventState.DependsOn != nil {
		if _, ok := s.ptpDependentEventState.DependsOn[processName]; ok {
			return true
		}
	}
	return false
}

// GetInterfaceByIndex ... get iface
func (s *Stats) GetInterfaceByIndex(processName string, index int) *string {
	if s.ptpDependentEventState != nil && s.ptpDependentEventState.DependsOn != nil {
		if d, ok := s.ptpDependentEventState.DependsOn[processName]; ok {
			if len(d) >= index {
				return d[index].IFace
			}
		}
	}
	return nil
}

func (s *Stats) String() string {
	b := strings.Builder{}
	b.WriteString("  configName: " + s.configName + "\n")
	b.WriteString("  processName: " + s.processName + "\n")
	b.WriteString("  aliasName: " + s.aliasName + "\n")
	b.WriteString("  offsetSource: " + s.offsetSource + "\n")
	b.WriteString("--------------------------------\n")
	if s.PtpDependentEventState() != nil && s.ptpDependentEventState.DependsOn != nil {
		for pp, p := range s.ptpDependentEventState.DependsOn {
			b.WriteString("Depends on process: " + pp + "\n")
			b.WriteString("--------------------------------\n")
			for _, pd := range p {
				b.WriteString("  interface: " + *pd.IFace + "\n")
				b.WriteString("  state: " + string(pd.State) + "\n")
			}
		}
	}

	return b.String()
}

// DeleteAllMetrics ...  delete all metrics
//
//	write a functions to delete meteric from dependson object
func (s *Stats) DeleteAllMetrics(m []*prometheus.GaugeVec) {
	if s.ptpDependentEventState != nil {
		s.ptpDependentEventState.DeleteAllMetrics(m)
	}
}

// CheckSource ... check key
func (ps PTPStats) CheckSource(k types.IFace, configName, processName string) {
	if _, found := ps[k]; !found {
		ps[k] = NewStats(configName)
		ps[k].SetProcessName(processName)
	}
}

// New ...
func (ps PTPStats) New() PTPStats {
	ptpStats := make(map[types.IFace]*Stats)
	return ptpStats
}

// SetConfigAsDeleted ...
func (ps PTPStats) SetConfigAsDeleted(state bool) {
	for _, p := range ps {
		p.configDeleted = state
	}
}

// Reset ...all values to empty
func (ps PTPStats) Reset() {
	for _, p := range ps {
		p.reset()
	}
}

// HasMetrics ...
func (ps PTPStats) HasMetrics(process string) map[string]*event.PMetric {
	for _, s := range ps {
		if s.ptpDependentEventState != nil && s.ptpDependentEventState.DependsOn != nil {
			for p, d := range s.ptpDependentEventState.DependsOn { // [processname]Clockstate
				if p == process {
					for _, c := range d {
						return c.Metric
					}
				}
			}
		}
	}
	return nil
}

// HasMetricHelp ...
func (ps PTPStats) HasMetricHelp(process string) map[string]string {
	for _, s := range ps {
		if s.ptpDependentEventState != nil && s.ptpDependentEventState.DependsOn != nil {
			for p, d := range s.ptpDependentEventState.DependsOn { // [processname]Clockstate
				if p == process {
					for _, c := range d {
						return c.HelpText
					}
				}
			}
		}
	}
	return nil
}
