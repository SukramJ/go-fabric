// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package netwatch notices changes of the host's network interfaces by
// comparing a snapshot of them at an interval — matter.js
// Network.interfacesChanged (packages/general/src/net/Network.ts:142-215 at
// db021da0). The mDNS responder and the GeneralDiagnostics server each run
// one while somebody depends on the interfaces: the responder while
// services are published, GeneralDiagnostics while its attribute changes
// are observed.
package netwatch

import (
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/clock"
)

// DefaultInterval is how often a running [Watcher] compares the interfaces:
// half the 120 s TTL of the mDNS address records, so peers hold an old
// address for at most this interval plus one TTL — matter.js
// INTERFACE_CHECK_INTERVAL (Network.ts:91-95 at db021da0).
const DefaultInterval = 60 * time.Second

// Watcher compares snapshots of the interfaces every interval while it
// runs — between [Watcher.Start] and [Watcher.Stop], matter.js's "while
// observed" — and reports the names whose description changed. The first
// snapshot after [Watcher.Start] only records the state (Network.ts:197-206
// at db021da0); a name that appeared, disappeared or whose description
// changed is reported, sorted (:208-213).
//
// Checks never overlap, so changed never runs concurrently with itself
// (:174-186). Neither snapshot nor changed runs with the Watcher's own lock
// held: Start may be called under the caller's lock as long as snapshot
// does not take it.
type Watcher struct {
	clock    func() clock.Clock
	snapshot func() map[string]string
	changed  func(names []string)

	// checkMu serialises checks.
	checkMu sync.Mutex

	// mu guards everything below.
	mu       sync.Mutex
	interval time.Duration
	running  bool
	// gen counts the starts and stops; a check or tick of an earlier
	// generation is discarded. tickGen counts the interval changes, whose
	// earlier ticks are discarded too.
	gen     uint64
	tickGen uint64
	timer   clock.Timer
	prev    map[string]string
}

// New returns a stopped Watcher at [DefaultInterval]. clk is read on every
// tick, so a clock set later applies; snapshot maps each interface name to
// its description ([Describe]) and returns nil when the interfaces cannot
// be listed, which keeps the previous snapshot; changed receives the
// changed names. An interface without details is left out of the
// snapshot, so it counts as absent.
func New(clk func() clock.Clock, snapshot func() map[string]string, changed func(names []string)) *Watcher {
	return &Watcher{clock: clk, snapshot: snapshot, changed: changed, interval: DefaultInterval}
}

// SetInterval sets the check interval; zero or negative disables the
// periodic check — a running Watcher stops ticking, and [Watcher.Check]
// still compares while it runs. A running Watcher ticks at the new interval
// from now on and keeps its snapshot.
func (w *Watcher) SetInterval(d time.Duration) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.interval = d
	if !w.running {
		return
	}
	if w.timer != nil {
		w.timer.Stop()
		w.timer = nil
	}
	// A tick of the old interval that already fired is discarded by its
	// tick number.
	w.tickGen++
	w.armLocked()
}

// Start begins the observation, unless it runs: it records the current
// snapshot and, unless the interval is disabled, compares again after each
// interval (Network.ts:163-171 at db021da0).
func (w *Watcher) Start() {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return
	}
	w.running = true
	w.gen++
	gen := w.gen
	w.mu.Unlock()

	snap := w.snapshot()

	w.mu.Lock()
	defer w.mu.Unlock()
	if w.gen != gen {
		return
	}
	w.prev = snap
	// SetInterval may have armed the timer while the snapshot ran; one
	// tick chain only.
	if w.timer == nil {
		w.armLocked()
	}
}

// Stop ends the observation and forgets the snapshot, so a later Start
// records afresh (Network.ts:163-171 at db021da0).
func (w *Watcher) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.running {
		return
	}
	w.running = false
	w.gen++
	w.prev = nil
	if w.timer != nil {
		w.timer.Stop()
		w.timer = nil
	}
}

// Running reports whether the periodic check runs.
func (w *Watcher) Running() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.running
}

// Check compares the interfaces now and, while the Watcher runs, reports
// the changed names to changed and returns them — matter.js
// Network.checkInterfaces, for a host that learns of a change before the
// next periodic check (Network.ts:174-186 at db021da0). A stopped Watcher
// compares nothing.
func (w *Watcher) Check() []string {
	w.checkMu.Lock()
	defer w.checkMu.Unlock()
	w.mu.Lock()
	gen, running := w.gen, w.running
	w.mu.Unlock()
	if !running {
		return nil
	}

	snap := w.snapshot()
	if snap == nil {
		// A failed listing keeps the previous state (Network.ts:174-177).
		return nil
	}

	w.mu.Lock()
	if w.gen != gen || !w.running {
		w.mu.Unlock()
		return nil
	}
	prev := w.prev
	w.prev = snap
	w.mu.Unlock()
	if prev == nil {
		// The first snapshot only records the state (:202-206).
		return nil
	}

	names := Diff(prev, snap)
	if len(names) > 0 {
		w.changed(names)
	}
	return names
}

// armLocked schedules the next tick, unless the interval is disabled.
// Caller holds w.mu.
func (w *Watcher) armLocked() {
	if w.interval <= 0 {
		return
	}
	gen, tickGen := w.gen, w.tickGen
	w.timer = w.clock().AfterFunc(w.interval, func() { w.tick(gen, tickGen) })
}

// tick runs one periodic check and schedules the next.
func (w *Watcher) tick(gen, tickGen uint64) {
	current := func() bool { return w.running && w.gen == gen && w.tickGen == tickGen }
	w.mu.Lock()
	ok := current()
	w.mu.Unlock()
	if !ok {
		return
	}
	w.Check()
	w.mu.Lock()
	defer w.mu.Unlock()
	if current() {
		w.armLocked()
	}
}

// Diff returns the sorted names whose description differs between prev and
// next, those present in only one of them included (Network.ts:208-210 at
// db021da0).
func Diff(prev, next map[string]string) []string {
	var names []string
	for name, d := range prev {
		if nd, ok := next[name]; !ok || nd != d {
			names = append(names, name)
		}
	}
	for name := range next {
		if _, ok := prev[name]; !ok {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}

// Describe is an interface's description for the comparison: its MAC and
// its addresses, each address family sorted and without duplicates, so
// neither the order nor a repeat of an address counts as a change —
// matter.js describeDetails (Network.ts:251-254 at db021da0).
func Describe(mac string, families ...[]string) string {
	parts := make([]string, 0, 1+len(families))
	parts = append(parts, mac)
	for _, addrs := range families {
		sorted := slices.Clone(addrs)
		slices.Sort(sorted)
		parts = append(parts, strings.Join(slices.Compact(sorted), ","))
	}
	return strings.Join(parts, "|")
}
