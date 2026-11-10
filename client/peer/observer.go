// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package peer

import (
	"slices"

	"github.com/SukramJ/go-fabric/client/exchange"
)

// Observer receives one peer's signals — what matter.js's node layer
// observes on a Peer directly (NetworkClient.ts:81-83 at 0ceb202d): its
// session set's added and deleted events (Peer.ts:68, :258-260, a
// BasicSet) and establishmentUnresponsive (Peer.ts:90-92, :197-204). Each
// field is optional; the set-wide [Context] hooks stay as they are.
type Observer struct {
	// SessionAdded is called when the peer takes a session.
	SessionAdded func(s *exchange.Session)
	// SessionDeleted is called when a session leaves the peer.
	SessionDeleted func(s *exchange.Session)
	// EstablishmentUnresponsive is called when a CASE establishment
	// attempt retransmitted past the MRP budget.
	EstablishmentUnresponsive func()
	// Updated is called after the peer's descriptor was saved — matter.js
	// Peer.updated, emitted by #save (Peer.ts:190-195, :575-579 at
	// 0ceb202d), which CommissioningClient observes per node
	// (CommissioningClient.ts:711).
	Updated func()
	// ReachabilityVerified is called once at the end of an address
	// monitor run that probed the peer ([Peer.VerifyReachability], the
	// address check): ok when the session answered — on its address or,
	// migrated, on an alternate — with address the session's address
	// after the run; false with address "" when no probe answered and the
	// session was given up. A run that needs no probe (no live session, a
	// TCP session, a sleeping ICD, an address still discovered or probed
	// within the cooldown) and a run the peer's closing cut short report
	// nothing. This module's seam, for the controller's diagnostic trace:
	// matter.js's PeerAddressMonitor logs the verdict and emits nothing.
	ReachabilityVerified func(reason ReachabilityReason, ok bool, address string)
}

// observerEntry is a registered observer; its address identifies it for
// stop.
type observerEntry struct{ o Observer }

// Observe registers o and returns the function that removes it. Observers
// are called after the peer's lock is released, in registration order.
func (p *Peer) Observe(o Observer) (stop func()) {
	e := &observerEntry{o: o}
	p.mu.Lock()
	p.observers = append(p.observers, e)
	p.mu.Unlock()
	return func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		p.observers = slices.DeleteFunc(p.observers, func(x *observerEntry) bool { return x == e })
	}
}

// observing is a snapshot of the observers.
func (p *Peer) observing() []Observer {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]Observer, 0, len(p.observers))
	for _, e := range p.observers {
		out = append(out, e.o)
	}
	return out
}
