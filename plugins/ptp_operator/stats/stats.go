package stats

import (
	ceevent "github.com/redhat-cne/sdk-go/pkg/event"
	"math"
)

// Stats calculates stats  nolint:unused
type Stats struct {
	configName          string
	num                 int64
	max                 int64
	min                 int64
	mean                int64
	sumSqr              int64
	sumDiffSqr          int64
	frequencyAdjustment int64
	delayFromMaster     int64
	lastOffset          int64
	lastSyncState       ceevent.SyncState
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

// GetStdev ...
func (s *Stats) GetStdev() float64 { //nolint:unused
	if s.num > 0 {
		return math.Sqrt(float64(s.sumDiffSqr / s.num))
	}
	return 1
}

// GetMaxAbs ...
func (s *Stats) GetMaxAbs() int64 {
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
}

// NewStats ... create new stats
func NewStats(configName string) *Stats {
	return &Stats{configName: configName}
}

// FrequencyAdjustment ...
func (s *Stats) FrequencyAdjustment(val int64) {
	s.frequencyAdjustment = val
}

// DelayFromMaster ...
func (s *Stats) DelayFromMaster(val int64) {
	s.delayFromMaster = val
}

// LastOffset ...
func (s *Stats) LastOffset(val int64) {
	s.lastOffset = val
}

// LastSyncState ...
func (s *Stats) LastSyncState(val ceevent.SyncState) {
	s.lastSyncState = val
}

// GetFrequencyAdjustment ...
func (s *Stats) GetFrequencyAdjustment() int64 {
	return s.frequencyAdjustment
}

// GetDelayFromMaster ...
func (s *Stats) GetDelayFromMaster() int64 {
	return s.delayFromMaster
}

// GetLastOffset ...
func (s *Stats) GetLastOffset() int64 {
	return s.lastOffset
}

// GetLastSyncState ...
func (s *Stats) GetLastSyncState() ceevent.SyncState {
	return s.lastSyncState
}
