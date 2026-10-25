// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package icd

import (
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/clock"
)

// Mode is the device's logical idle/active mode — distinct from the ICD
// operating mode SIT/LIT. Mirrors matter.js IcdMode.ts:9-12 (IcdMode).
type Mode uint8

// The two modes.
const (
	// ModeIdle is the mode before Start, after Stop and after EnterIdle.
	ModeIdle Mode = iota
	// ModeActive is the mode Start, NoteActivity on an idle device and
	// RequestActive enter.
	ModeActive
)

// String is the mode's matter.js name.
func (m Mode) String() string {
	if m == ModeActive {
		return "active"
	}
	return "idle"
}

// ModeOptions configures a [ModeState] (IcdModeStateOptions,
// IcdMode.ts:14-25).
type ModeOptions struct {
	// ActiveModeDuration is the shortest active window after entering
	// active mode (Core §9.16.6.2).
	ActiveModeDuration time.Duration
	// ActiveModeThreshold is the extension of the active window each
	// network activity grants (Core §9.16.6.3).
	ActiveModeThreshold time.Duration
	// OnActiveEntered runs on the initial power-up and every idle→active
	// wake: the Check-In send point.
	OnActiveEntered func()
	// OnIdleEntered runs on every active→idle transition.
	OnIdleEntered func()
	// OnMayEnterIdle runs when the active window elapsed with no further
	// activity: the device may now be put to sleep. The mode stays active.
	OnMayEnterIdle func()
	// Clock times the active window; nil is the system clock.
	Clock clock.Clock
}

// ModeState is the device's ICD idle/active mode, driven from outside: it
// never cycles on its own. Its one timer, the active window, only signals
// OnMayEnterIdle; every transition comes from Start, Stop, EnterIdle,
// RequestActive or NoteActivity. A window is never shortened. A module node
// never truly sleeps and stays reachable; the mode models the logical state
// for the specification, a controller's view and the host's power hooks
// (Core §9.15.1, §9.16).
//
// The callbacks run on the goroutine of the call that caused them (the
// window's timer for OnMayEnterIdle), after the state's lock is released.
//
// Mirrors matter.js node/src/behaviors/icd-management/IcdMode.ts:37-152
// (IcdModeState).
type ModeState struct {
	opts  ModeOptions
	clock clock.Clock

	// mu guards mode, running, activeUntil, timer and gen.
	mu          sync.Mutex
	mode        Mode
	running     bool
	activeUntil time.Time // zero: no window
	timer       clock.Timer
	gen         uint64 // the armed window's generation; a stale timer fire checks it
}

// NewModeState returns an idle, stopped mode machine.
func NewModeState(opts ModeOptions) *ModeState {
	return &ModeState{opts: opts, clock: clock.Or(opts.Clock)}
}

// Mode is the current mode (IcdMode.ts:57-59).
func (s *ModeState) Mode() Mode {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mode
}

// IsActive reports active mode (IcdMode.ts:61-63).
func (s *ModeState) IsActive() bool { return s.Mode() == ModeActive }

// Start begins tracking on node online and enters active mode for
// ActiveModeDuration. Idempotent. Mirrors IcdMode.ts:66-72 (start).
func (s *ModeState) Start() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.enterActiveLocked(s.opts.ActiveModeDuration, s.clock.Now())
	s.mu.Unlock()
	s.call(s.opts.OnActiveEntered)
}

// Stop pauses the machine on node offline: the window's timer stops, the
// mode is idle, and nothing is reported. Idempotent. Mirrors IcdMode.ts:75-79
// (stop).
func (s *ModeState) Stop() {
	s.mu.Lock()
	s.running = false
	s.stopTimerLocked()
	s.mode = ModeIdle
	s.mu.Unlock()
}

// Close is Stop (IcdMode.ts:120-122, Symbol.dispose).
func (s *ModeState) Close() { s.Stop() }

// NoteActivity records network activity: an idle device wakes for
// ActiveModeThreshold, an active one extends its window by it. Ignored
// while stopped. Mirrors IcdMode.ts:82-91 (noteActivity).
func (s *ModeState) NoteActivity() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	now := s.clock.Now()
	woke := s.mode == ModeIdle
	if woke {
		s.enterActiveLocked(s.opts.ActiveModeThreshold, now)
	} else {
		s.scheduleActiveForLocked(s.opts.ActiveModeThreshold, now)
	}
	s.mu.Unlock()
	if woke {
		s.call(s.opts.OnActiveEntered)
	}
}

// RequestActive forces active mode for at least d, waking an idle device,
// and returns the whole remaining active window the device is now committed
// to, in whole milliseconds; zero while stopped. Mirrors IcdMode.ts:97-108
// (requestActive).
func (s *ModeState) RequestActive(d time.Duration) time.Duration {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return 0
	}
	now := s.clock.Now() // one reading: the promise and the window share it
	woke := s.mode == ModeIdle
	if woke {
		s.enterActiveLocked(d, now)
	} else {
		s.scheduleActiveForLocked(d, now)
	}
	remaining := s.remainingLocked(now).Truncate(time.Millisecond) // Millis.floor
	s.mu.Unlock()
	if woke {
		s.call(s.opts.OnActiveEntered)
	}
	return remaining
}

// EnterIdle forces idle mode now, unconditionally — the host's or the test
// harness's control. A no-op while stopped or idle. Mirrors IcdMode.ts:111-118
// (enterIdle).
func (s *ModeState) EnterIdle() {
	s.mu.Lock()
	if !s.running || s.mode == ModeIdle {
		s.mu.Unlock()
		return
	}
	s.stopTimerLocked()
	s.mode = ModeIdle
	s.mu.Unlock()
	s.call(s.opts.OnIdleEntered)
}

// remainingLocked is the active window left at now, never negative
// (IcdMode.ts:124-126, #remainingActive).
func (s *ModeState) remainingLocked(now time.Time) time.Duration {
	return max(s.activeUntil.Sub(now), 0)
}

// enterActiveLocked enters active mode for at least max(ActiveModeDuration,
// forAtLeast) from now; the caller reports OnActiveEntered after unlocking
// (IcdMode.ts:128-133, #enterActive).
func (s *ModeState) enterActiveLocked(forAtLeast time.Duration, now time.Time) {
	s.mode = ModeActive
	s.activeUntil = time.Time{} // the next schedule sets the floor from now
	s.scheduleActiveForLocked(max(s.opts.ActiveModeDuration, forAtLeast), now)
}

// scheduleActiveForLocked extends the active deadline to at least now +
// fromNow, never shortening it, and re-arms the one-shot window timer
// (IcdMode.ts:136-146, #scheduleActiveFor).
func (s *ModeState) scheduleActiveForLocked(fromNow time.Duration, now time.Time) {
	candidate := now.Add(fromNow)
	if !s.activeUntil.IsZero() && !candidate.After(s.activeUntil) {
		return
	}
	s.activeUntil = candidate
	s.stopTimerLocked()
	s.gen++
	gen := s.gen
	s.timer = s.clock.AfterFunc(s.remainingLocked(now), func() { s.windowExpired(gen) })
}

func (s *ModeState) stopTimerLocked() {
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	s.gen++
}

// windowExpired signals OnMayEnterIdle and changes nothing else
// (IcdMode.ts:148-151, #onWindowExpired).
func (s *ModeState) windowExpired(gen uint64) {
	s.mu.Lock()
	if gen != s.gen {
		s.mu.Unlock()
		return
	}
	s.timer = nil
	s.mu.Unlock()
	s.call(s.opts.OnMayEnterIdle)
}

func (*ModeState) call(fn func()) {
	if fn != nil {
		fn()
	}
}
