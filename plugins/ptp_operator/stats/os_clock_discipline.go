package stats

import (
	"sync"
	"time"
)

// OsClockPhase tracks where the OS-clock discipline detection state machine
// is within a phc2sys selection burst after upstream loss under -a -r.
//
// Transitions:
//
//	Idle ──Begin()──→ Selecting ──SourceClockSelected()──→ SourceSelected ──Finalise()──→ Undisciplined
//	                  │                        │                          │
//	                  └──SinkClockSelected()──→ Idle                        └──ClearOnRealtimeOffset()──→ Idle
//	                  └──Abort()───────────→ Idle └──SinkClockSelected()──→ Idle
//	                                            └──Abort()─────────→ Idle
type OsClockPhase int

const (
	OsClockIdle           OsClockPhase = iota // No selection window open
	OsClockSelecting                          // Window opened by "reconfiguring after port state change"
	OsClockSourceSelected                     // Source clock selected, waiting for CLOCK_REALTIME or settle timeout
	OsClockUndisciplined                      // CLOCK_REALTIME not being network-disciplined
)

// OsClockDiscipline is a self-contained state machine tracking whether
// phc2sys is disciplining CLOCK_REALTIME.  Embed it in Stats.
type OsClockDiscipline struct {
	mu        sync.Mutex
	phase     OsClockPhase
	settle    *time.Timer
	selectGen uint64
}

// Phase returns the current phase.
func (d *OsClockDiscipline) Phase() OsClockPhase {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.phase
}

// IsUndisciplined reports whether CLOCK_REALTIME is not network-disciplined.
func (d *OsClockDiscipline) IsUndisciplined() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.phase == OsClockUndisciplined
}

// IsWindowOpen reports whether a selection window is currently open.
func (d *OsClockDiscipline) IsWindowOpen() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.phase == OsClockSelecting || d.phase == OsClockSourceSelected
}

// Begin opens (or resets) a selection window after
// "reconfiguring after port state change".
func (d *OsClockDiscipline) Begin() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.stopSettleLocked()
	d.selectGen++
	d.phase = OsClockSelecting
}

// Abort closes an open window without transitioning to Undisciplined.
func (d *OsClockDiscipline) Abort() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.stopSettleLocked()
	if d.phase == OsClockSelecting || d.phase == OsClockSourceSelected {
		d.phase = OsClockIdle
	}
}

// SourceClockSelected records a non-CLOCK_REALTIME sink and arms the settle timer.
func (d *OsClockDiscipline) SourceClockSelected(delay time.Duration, onSettle func(uint64)) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.phase != OsClockSelecting && d.phase != OsClockSourceSelected {
		return
	}
	d.phase = OsClockSourceSelected
	d.stopSettleLocked()
	gen := d.selectGen
	d.settle = time.AfterFunc(delay, func() { onSettle(gen) })
}

// SinkClockSelected records that CLOCK_REALTIME was selected; clears the
// window and any undisciplined state.
func (d *OsClockDiscipline) SinkClockSelected() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.stopSettleLocked()
	d.phase = OsClockIdle
}

// ReArmSettle resets the settle timer while in SourceSelected phase.
func (d *OsClockDiscipline) ReArmSettle(delay time.Duration, onSettle func(uint64)) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.phase != OsClockSourceSelected {
		return
	}
	d.stopSettleLocked()
	gen := d.selectGen
	d.settle = time.AfterFunc(delay, func() { onSettle(gen) })
}

// Finalise closes the window.  Returns true if a NIC was seen without
// CLOCK_REALTIME, meaning FREERUN should be published.
func (d *OsClockDiscipline) Finalise() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.finaliseLocked()
}

// FinaliseIfGeneration is for settle-timer callbacks; only finalises if the
// generation matches (i.e. the window has not been superseded).
func (d *OsClockDiscipline) FinaliseIfGeneration(generation uint64) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.selectGen != generation {
		return false
	}
	return d.finaliseLocked()
}

// ClearOnRealtimeOffset closes any open window when a forward CLOCK_REALTIME
// phc-offset sample proves discipline, and resets the undisciplined flag.
func (d *OsClockDiscipline) ClearOnRealtimeOffset() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.stopSettleLocked()
	d.phase = OsClockIdle
}

// Reset clears all state.
func (d *OsClockDiscipline) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.stopSettleLocked()
	d.phase = OsClockIdle
}

func (d *OsClockDiscipline) stopSettleLocked() {
	if d.settle != nil {
		d.settle.Stop()
		d.settle = nil
	}
}

func (d *OsClockDiscipline) finaliseLocked() bool {
	d.stopSettleLocked()
	if d.phase != OsClockSelecting && d.phase != OsClockSourceSelected {
		return false
	}
	activate := d.phase == OsClockSourceSelected
	if activate {
		d.phase = OsClockUndisciplined
	} else {
		d.phase = OsClockIdle
	}
	return activate
}
