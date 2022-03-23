package stats

import (
	"math"
	"strings"

	"github.com/redhat-cne/sdk-go/pkg/event/ptp"
)

// Stats calculates stats  nolint:unused
type Stats struct {
	configName          string
	offsetSource        string
	processName         string
	num                 int64
	max                 int64
	min                 int64
	mean                int64
	sumSqr              int64
	sumDiffSqr          int64
	frequencyAdjustment int64
	delay               int64
	lastOffset          int64
	lastSyncState       ptp.SyncState
	aliasName           string
}

// AddValue ...
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

// StDev ...
func (s *Stats) StDev() float64 { //nolint:unused
	if s.num > 0 {
		return math.Sqrt(float64(s.sumDiffSqr / s.num))
	}
	return 1
}

// MaxAbs ...
func (s *Stats) MaxAbs() int64 {
	if s.max > s.min {
		return s.max
	}
	return s.min

}

// OffsetSource ...
func (s *Stats) OffsetSource() string {
	return s.offsetSource

}

// SetOffsetSource ...
func (s *Stats) SetOffsetSource(os string) {
	s.offsetSource = os
}

// ProcessName ...
func (s *Stats) ProcessName() string {
	return s.processName
}

// SetProcessName ...
func (s *Stats) SetProcessName(processName string) {
	s.processName = processName
}

// Offset return last known offset
func (s *Stats) Offset() int64 {
	return s.lastOffset
}

// Alias return alias name
func (s *Stats) Alias() string {
	return s.aliasName
}

// SetAlias ...
func (s *Stats) SetAlias(val string) {
	s.aliasName = val
}

// SyncState return last known SyncState state
func (s *Stats) SyncState() ptp.SyncState {
	return s.lastSyncState
}

//ConfigName ...
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
}

// NewStats ... create new stats
func NewStats(configName string) *Stats {
	return &Stats{configName: configName}
}

// SetFrequencyAdjustment ...
func (s *Stats) SetFrequencyAdjustment(val int64) {
	s.frequencyAdjustment = val
}

// SetDelay ...
func (s *Stats) SetDelay(val int64) {
	s.delay = val
}

// SetLastOffset ...
func (s *Stats) SetLastOffset(val int64) {
	s.lastOffset = val
}

// SetLastSyncState ...
func (s *Stats) SetLastSyncState(val ptp.SyncState) {
	s.lastSyncState = val
}

// FrequencyAdjustment ...
func (s *Stats) FrequencyAdjustment() int64 {
	return s.frequencyAdjustment
}

// Delay ...
func (s *Stats) Delay() int64 {
	return s.delay
}

// LastOffset ...
func (s *Stats) LastOffset() int64 {
	return s.lastOffset
}

// LastSyncState ...
func (s *Stats) LastSyncState() ptp.SyncState {
	return s.lastSyncState
}

func (s *Stats) String() string {
	b := strings.Builder{}
	b.WriteString("  configName: " + s.configName + "\n")
	b.WriteString("  processName: " + s.processName + "\n")
	b.WriteString("  aliasName: " + s.aliasName + "\n")
	b.WriteString("  offsetSource: " + s.offsetSource + "\n")
	return b.String()
}
