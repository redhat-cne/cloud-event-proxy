package metrics

import (
	"slices"
	"strings"

	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/stats"
	"github.com/redhat-cne/sdk-go/pkg/event/ptp"
	log "github.com/sirupsen/logrus"
)

// OS-clock discipline (phc2sysOpts: -a -r)
//
// After upstream loss, phc2sys may stop selecting CLOCK_REALTIME as a sink, so
// "CLOCK_REALTIME phc offset" samples stop and E3 can stay LOCKED. Watch the
// selection burst after "reconfiguring after port state change":
//   - NIC selected, CLOCK_REALTIME not (already selected / settle silence) → E3 FREERUN
//   - selecting CLOCK_REALTIME or CLOCK_REALTIME phc offset → clear sticky, normal path
//
// Not reverse sync (-r -r). Do not force E3 from T-BC alone (OCPBUGS-88369).

// osClockLineClass classifies a phc2sys log line for the selection-window
// state machine.  Merging the per-line checks into one classifier keeps the
// handler a clean switch.
type osClockLineClass int

const (
	osClockLineOther       osClockLineClass = iota // no match — continue to normal processing
	osClockLineReconfigure                         // "reconfiguring after port state change"
	osClockLineWaiting                             // "source clock not ready" / "postponing" / "no PHC ready"
	osClockLineSourceClock                         // "as domain source clock" / "as out-of-domain source clock"
	osClockLineFinalised                           // "already selected" or " offset "
)

func classifyOsClockLine(output string) osClockLineClass {
	if strings.Contains(output, "reconfiguring after port state change") {
		return osClockLineReconfigure
	}
	if strings.Contains(output, "source clock not ready, waiting") ||
		strings.Contains(output, "multiple source clocks available, postponing sync") ||
		strings.Contains(output, "no PHC ready, waiting") {
		return osClockLineWaiting
	}
	if strings.Contains(output, "as domain source clock") ||
		strings.Contains(output, "as out-of-domain source clock") {
		return osClockLineSourceClock
	}
	if strings.Contains(output, "already selected") ||
		strings.Contains(output, " offset ") {
		return osClockLineFinalised
	}
	return osClockLineOther
}

func (p *PTPEventManager) handlePhc2sysSyncDirection(profileName, configName, output string,
	fields []string, ptpStats stats.PTPStats) bool {
	ptpStats.CheckSource(ClockRealTime, configName, phc2sysProcessName)
	cr := ptpStats[ClockRealTime]

	onSettle := func(generation uint64) {
		p.extractMu.Lock()
		defer p.extractMu.Unlock()
		if cr.OsClock.FinaliseIfGeneration(generation) {
			log.Infof("os-clock: profile=%s reason=selection-settle → E3 FREERUN", profileName)
			p.publishOsClockFreerunUndisciplined(profileName, ptpStats)
		}
	}

	switch classifyOsClockLine(output) {
	case osClockLineReconfigure:
		cr.OsClock.Begin()
		log.Debugf("os-clock: profile=%s selection-window=begin", profileName)
		return true

	case osClockLineWaiting:
		if cr.OsClock.IsWindowOpen() {
			cr.OsClock.Abort()
			log.Debugf("os-clock: profile=%s selection-window=abort reason=waiting", profileName)
		}
		return true

	case osClockLineSourceClock:
		cr.OsClock.ReArmSettle(phc2sysSelectionSettleDelay, onSettle)
		return true

	case osClockLineFinalised:
		if !cr.OsClock.IsWindowOpen() {
			return false
		}
		if strings.Contains(output, ClockRealTime) && strings.Contains(output, " offset ") {
			cr.OsClock.Abort()
			return false
		}
		if cr.OsClock.Finalise() {
			log.Infof("os-clock: profile=%s reason=selection-finalised → E3 FREERUN", profileName)
			p.publishOsClockFreerunUndisciplined(profileName, ptpStats)
		}
		if strings.Contains(output, " offset ") {
			return false
		}
		return true
	}

	if clockName, ok := parseSelectingForSynchronization(fields); ok {
		if !cr.OsClock.IsWindowOpen() {
			if clockName == ClockRealTime {
				cr.OsClock.SinkClockSelected()
			}
			return true
		}
		if clockName == ClockRealTime {
			cr.OsClock.SinkClockSelected()
			log.Debugf("os-clock: profile=%s selecting=CLOCK_REALTIME → disciplined", profileName)
			return true
		}
		cr.OsClock.SourceClockSelected(phc2sysSelectionSettleDelay, onSettle)
		log.Debugf("os-clock: profile=%s selecting=%s → arm settle", profileName, clockName)
		return true
	}

	return false
}

// handlePhc2sysOffsetSyncDirection clears undisciplined state when a forward
// CLOCK_REALTIME phc-offset sample arrives, proving discipline.
func handlePhc2sysOffsetSyncDirection(profileName, configName,
	interfaceName string, ptpStats stats.PTPStats) {
	if interfaceName != ClockRealTime {
		return
	}
	ptpStats.CheckSource(ClockRealTime, configName, phc2sysProcessName)
	cr := ptpStats[ClockRealTime]
	was := cr.OsClock.IsUndisciplined()
	cr.OsClock.ClearOnRealtimeOffset()
	if was {
		log.Infof("os-clock: profile=%s reason=clock-realtime-phc-offset → clear undisciplined",
			profileName)
	}
}

func (p *PTPEventManager) publishOsClockFreerunUndisciplined(profileName string, ptpStats stats.PTPStats) {
	s, ok := ptpStats[ClockRealTime]
	if !ok {
		return
	}
	opts := p.PtpConfigMapUpdates.LookupPtpProcessOpts(profileName)
	if opts != nil && opts.ChronydEnabled() {
		log.Debugf("os-clock: profile=%s skip E3 FREERUN (chronyd owns OS clock)", profileName)
		return
	}
	p.GenPTPEvent(profileName, s, ClockRealTime, FreeRunOffsetValue, ptp.FREERUN, ptp.OsClockSyncStateChange)
	UpdateSyncStateMetrics(phc2sysProcessName, ClockRealTime, ptp.FREERUN)
}

func parseSelectingForSynchronization(fields []string) (string, bool) {
	idx := slices.Index(fields, "selecting")
	if idx < 0 {
		return "", false
	}
	// "selecting <clock> for synchronization"
	if idx+3 < len(fields) && fields[idx+2] == "for" && fields[idx+3] == "synchronization" {
		return fields[idx+1], true
	}
	// "selecting system clock for synchronization"
	if idx+4 < len(fields) &&
		fields[idx+1] == "system" && fields[idx+2] == "clock" &&
		fields[idx+3] == "for" && fields[idx+4] == "synchronization" {
		return ClockRealTime, true
	}
	return "", false
}
