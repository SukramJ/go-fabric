// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package icd

import (
	"sync"
	"time"
)

// ClientKey names one registered client of the device: its fabric and its
// CheckInNodeID — matter.js's `${fabricIndex}:${checkInNodeId}` key
// (IcdCheckInBackOff.ts:9-11, IcdManagementServer.ts:766-768).
type ClientKey struct {
	FabricIndex   uint8
	CheckInNodeID uint64
}

// CheckInBackOff is the per-client Check-In back-off the device consults
// once per idle→active wake for each Permanent registration (Core §9.15.1,
// §9.16.6.10). Mirrors matter.js IcdCheckInBackOff.ts:15-26
// (IcdCheckInBackOff).
type CheckInBackOff interface {
	// ShouldSend reports a Check-In due for the client this wake and
	// advances the per-wake countdown: call it exactly once per wake.
	ShouldSend(ClientKey) bool
	// RecordSent records a Check-In attempted this wake.
	RecordSent(ClientKey)
	// RecordAnswered resets the client to immediate: it became covered by
	// an active subscription.
	RecordAnswered(ClientKey)
	// ResetAll resets every client to immediate: the User Active Mode
	// Trigger.
	ResetAll()
	// Forget drops the client's state: its registration was removed.
	Forget(ClientKey)
}

type backOffState struct {
	sendCount           int
	cyclesUntilNextSend int
}

// DoublingBackOff sends on a wake, then skips a doubling number of idle
// cycles (1, 2, 4, …), capped so the interval never exceeds the maximum
// Check-In back-off. When the maximum equals the idle duration the cap is
// one cycle: a Check-In every wake. Safe for concurrent use.
//
// Mirrors matter.js IcdCheckInBackOff.ts:38-84 (DoublingCheckInBackOff).
type DoublingBackOff struct {
	maxCycles int

	// mu guards clients.
	mu      sync.Mutex
	clients map[ClientKey]*backOffState
}

var _ CheckInBackOff = (*DoublingBackOff)(nil)

// NewDoublingBackOff returns the back-off of an idle duration and a maximum
// Check-In back-off, both counted in whole seconds as the attributes are
// (IcdCheckInBackOff.ts:42-46).
func NewDoublingBackOff(idleModeDuration, maximumCheckInBackoff time.Duration) *DoublingBackOff {
	maxCycles := 1
	if idle := idleModeDuration.Seconds(); idle > 0 {
		maxCycles = max(1, int(maximumCheckInBackoff.Seconds()/idle))
	}
	return &DoublingBackOff{maxCycles: maxCycles, clients: map[ClientKey]*backOffState{}}
}

func (b *DoublingBackOff) stateLocked(k ClientKey) *backOffState {
	st, ok := b.clients[k]
	if !ok {
		st = &backOffState{}
		b.clients[k] = st
	}
	return st
}

// ShouldSend is IcdCheckInBackOff.ts:57-64.
func (b *DoublingBackOff) ShouldSend(k ClientKey) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	st := b.stateLocked(k)
	if st.cyclesUntilNextSend <= 0 {
		return true
	}
	st.cyclesUntilNextSend--
	return false
}

// RecordSent is IcdCheckInBackOff.ts:66-70.
func (b *DoublingBackOff) RecordSent(k ClientKey) {
	b.mu.Lock()
	defer b.mu.Unlock()
	st := b.stateLocked(k)
	st.sendCount++
	gap := b.maxCycles
	if st.sendCount-1 < 31 {
		gap = min(1<<(st.sendCount-1), b.maxCycles)
	}
	st.cyclesUntilNextSend = gap - 1
}

// RecordAnswered drops the state, so the next wake sends
// (IcdCheckInBackOff.ts:72-75).
func (b *DoublingBackOff) RecordAnswered(k ClientKey) { b.Forget(k) }

// ResetAll is IcdCheckInBackOff.ts:77-79.
func (b *DoublingBackOff) ResetAll() {
	b.mu.Lock()
	clear(b.clients)
	b.mu.Unlock()
}

// Forget is IcdCheckInBackOff.ts:81-83.
func (b *DoublingBackOff) Forget(k ClientKey) {
	b.mu.Lock()
	delete(b.clients, k)
	b.mu.Unlock()
}
