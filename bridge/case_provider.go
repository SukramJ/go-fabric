// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"sync"
	"time"

	"thingfabric.dev/go-fabric/clock"
	"thingfabric.dev/go-fabric/internal/clockvar"
)

// PerExchangeCaseProvider is a [CaseHandlerProvider] that lazily
// allocates a fresh [CaseAdapter] per inbound CASE handshake — one
// initiator's exchange, [HandshakeKey]. It supports concurrent
// operational sessions by isolating responder state per handshake, and
// keeps a message of one initiator from reaching another initiator's
// responder on the same exchange id — Apple Home opens parallel CASE sessions from
// the iPhone (IPv6) and HomePod (IPv4); a singleton CaseAdapter
// gets stuck in `Finished` after the first successful Sigma3 and
// rejects every subsequent Sigma1 with `ProcessSigma1 already called`.
//
// The factory MUST return a fully-configured CaseAdapter (sigma
// responder seeded from the current operational identity, fabric
// verifier, OnSessionEstablished callback wired). Operators rotate
// the underlying identity by replacing the factory closure.
//
// Memory model: an entry is stamped when it is made and when a Sigma1
// begins a handshake on it, and entries not stamped within `ttl` get
// reaped by the optional [PerExchangeCaseProvider.StartReaper] background
// goroutine.
// Daemons handling many concurrent commissioners over time should
// always wire the reaper; without it the adapters map grows
// unboundedly.
//
// The optional onEvict callback (set via [PerExchangeCaseProvider.SetOnEvict])
// is called on TTL-eviction of each stale entry — the bridge wires
// [Bridge.forgetSigma1Replied] here so aborted CASE exchanges
// (Sigma1 arrived, Sigma3 never came) do not leak entries in the
// sigma1Replied dedupe map — see [Bridge.forgetSigma1Replied].
type PerExchangeCaseProvider struct {
	clock clockvar.Var // see SetClock
	// mu guards cancel, entries and onEvict.
	mu       sync.Mutex
	entries  map[HandshakeKey]*caseEntry
	factory  func() *CaseAdapter
	onEvict  func(key HandshakeKey) // optional; called when an entry is TTL-reaped
	cancel   context.CancelFunc
	reaperWG sync.WaitGroup
}

type caseEntry struct {
	adapter *CaseAdapter
	// lastTouched is when the adapter was made, then when a Sigma1 last
	// began a handshake on it: the reaper's and the bound's age.
	lastTouched time.Time
	// begun is set once a Sigma1 began a handshake on the adapter
	// ([PerExchangeCaseProvider.began]); only such an entry counts toward
	// [maxCaseHandshakes] and can lose its place.
	begun bool
}

// SetClock sets the clock the entries are stamped and reaped on; nil is
// [clock.System]. Call it before [PerExchangeCaseProvider.StartReaper].
func (p *PerExchangeCaseProvider) SetClock(c clock.Clock) { p.clock.Store(c) }

// NewPerExchangeCaseProvider constructs a provider backed by factory.
// A nil factory produces a provider that always returns nil from
// Resolve — equivalent to "no CASE handler", which the SC router
// degrades to a debug-logged drop.
func NewPerExchangeCaseProvider(factory func() *CaseAdapter) *PerExchangeCaseProvider {
	return &PerExchangeCaseProvider{
		entries: make(map[HandshakeKey]*caseEntry),
		factory: factory,
	}
}

// Resolve implements [CaseHandlerProvider]. Returns the existing
// adapter for key, allocating a fresh one via the factory on first
// sighting. Returns nil when the factory is nil or returned nil;
// callers (the SC router) drop the datagram on nil.
//
// The provider holds at most [maxCaseHandshakes] adapters whose Sigma1
// began a handshake: once a new adapter's Sigma1 began one, the least
// recently begun adapter goes as a reaped one goes (review II,
// R2-P3-dr-dev-03). A new adapter takes nobody's place while its Sigma1 is
// judged — one that begins nothing leaves on its own
// ([CaseAdapter.handshakeFailed]) — so a Sigma1 without a handshake ends no
// handshake in progress (R2-P6-sec-0301). Until then the adapters exceed
// the bound by at most the Sigma1s being judged. A Resolve of a held key
// does not extend the adapter's lifetime: that runs from the Sigma1 that
// began its handshake (U4-fr-sec-1).
func (p *PerExchangeCaseProvider) Resolve(key HandshakeKey) CaseHandler {
	p.mu.Lock()
	if e, ok := p.entries[key]; ok {
		p.mu.Unlock()
		return e.adapter
	}
	if p.factory == nil {
		p.mu.Unlock()
		return nil
	}
	a := p.factory()
	if a == nil {
		p.mu.Unlock()
		return nil
	}
	a.setDetach(func() { p.drop(key, a) })
	a.setOnBegun(func() { p.began(key, a) })
	p.entries[key] = &caseEntry{adapter: a, lastTouched: p.clock.Load().Now()}
	p.mu.Unlock()
	return a
}

// began follows a Sigma1 that began a handshake on a, key's adapter: the
// entry counts toward the bound from now, its lifetime restarts, and the
// least recently begun entries other than key's go while more than
// [maxCaseHandshakes] are begun.
func (p *PerExchangeCaseProvider) began(key HandshakeKey, a *CaseAdapter) {
	p.mu.Lock()
	e, ok := p.entries[key]
	if !ok || e.adapter != a {
		p.mu.Unlock()
		return
	}
	e.begun = true
	e.lastTouched = p.clock.Load().Now()
	var (
		evictedKeys []HandshakeKey
		gone        []*caseEntry
	)
	for p.begunLocked() > maxCaseHandshakes {
		k, oldest := p.leastRecentlyBegunLocked(key)
		if oldest == nil {
			break
		}
		delete(p.entries, k)
		evictedKeys = append(evictedKeys, k)
		gone = append(gone, oldest)
	}
	onEvict := p.onEvict
	p.mu.Unlock()
	dropped(gone)
	if onEvict != nil {
		for _, k := range evictedKeys {
			onEvict(k)
		}
	}
}

// begunLocked counts the entries whose Sigma1 began a handshake. The
// caller holds p.mu.
func (p *PerExchangeCaseProvider) begunLocked() int {
	n := 0
	for _, e := range p.entries {
		if e.begun {
			n++
		}
	}
	return n
}

// leastRecentlyBegunLocked returns the begun entry other than keep's that
// was touched longest ago, and its key; nil when there is none. The caller
// holds p.mu.
func (p *PerExchangeCaseProvider) leastRecentlyBegunLocked(keep HandshakeKey) (HandshakeKey, *caseEntry) {
	var (
		oldestKey HandshakeKey
		oldest    *caseEntry
	)
	for k, e := range p.entries {
		if !e.begun || k == keep {
			continue
		}
		if oldest == nil || e.lastTouched.Before(oldest.lastTouched) {
			oldestKey, oldest = k, e
		}
	}
	return oldestKey, oldest
}

// drop removes key's entry when it still holds a — the adapter's handshake
// failed ([CaseAdapter.SetOnUnfinished]) — and reports the eviction.
func (p *PerExchangeCaseProvider) drop(key HandshakeKey, a *CaseAdapter) {
	p.mu.Lock()
	e, ok := p.entries[key]
	if !ok || e.adapter != a {
		p.mu.Unlock()
		return
	}
	delete(p.entries, key)
	onEvict := p.onEvict
	p.mu.Unlock()
	if onEvict != nil {
		onEvict(key)
	}
}

// dropped returns the staked session id of each adapter the provider let
// go ([CaseAdapter.SetOnUnfinished]). Called without p.mu.
func dropped(entries []*caseEntry) {
	for _, e := range entries {
		if e != nil && e.adapter != nil {
			e.adapter.releaseStakedID()
		}
	}
}

// SetOnEvict wires the TTL-eviction callback. The bridge sets this to
// [Bridge.forgetSigma1Replied] so aborted CASE exchanges (Sigma1
// arrived, Sigma3 never completed) do not leak entries in the
// sigma1Replied dedupe map.
// Mirrors matter.js packages/protocol/src/session/case/CaseServer.ts
// per-exchange handler GC: TypeScript's GC reclaims the per-exchange
// handler automatically on exchange.close(); Go needs an explicit hook
// because the dedupe map is Bridge-owned, not handler-owned.
// Pass nil to detach.
func (p *PerExchangeCaseProvider) SetOnEvict(fn func(key HandshakeKey)) {
	p.mu.Lock()
	p.onEvict = fn
	p.mu.Unlock()
}

// Forget removes the adapter for key so the next [Resolve]
// allocates a fresh one. Operators wire this from a post-Sigma3
// hook for immediate cleanup; production daemons typically rely on
// the TTL reaper instead. The eviction hook runs as for a reaped adapter,
// so the handshake's state outside the provider — the router's record, its
// unsecured window, a connection's hold — goes with it (review II,
// N3-findev-2).
func (p *PerExchangeCaseProvider) Forget(key HandshakeKey) {
	p.mu.Lock()
	e, ok := p.entries[key]
	delete(p.entries, key)
	onEvict := p.onEvict
	p.mu.Unlock()
	if !ok {
		return
	}
	dropped([]*caseEntry{e})
	if onEvict != nil {
		onEvict(key)
	}
}

// Reset drops every per-exchange adapter. Operators call this when
// rotating the underlying identity so an in-flight commissioner
// cannot complete against the stale identity. The onEvict hook is
// called for every dropped entry so the Bridge's sigma1Replied map
// is also cleared.
func (p *PerExchangeCaseProvider) Reset() {
	p.mu.Lock()
	evicted := make([]HandshakeKey, 0, len(p.entries))
	gone := make([]*caseEntry, 0, len(p.entries))
	for k, e := range p.entries {
		delete(p.entries, k)
		evicted = append(evicted, k)
		gone = append(gone, e)
	}
	onEvict := p.onEvict
	p.mu.Unlock()
	dropped(gone)
	if onEvict != nil {
		for _, id := range evicted {
			onEvict(id)
		}
	}
}

// Active reports the count of in-flight per-exchange adapters. Test
// helper.
func (p *PerExchangeCaseProvider) Active() int {
	p.mu.Lock()
	n := len(p.entries)
	p.mu.Unlock()
	return n
}

// StartReaper spawns a background goroutine that drops adapters
// untouched for `ttl`, scanning every `interval`. Cancel via
// [PerExchangeCaseProvider.StopReaper]. Calling twice without an
// intervening Stop is a no-op.
func (p *PerExchangeCaseProvider) StartReaper(parent context.Context, interval, ttl time.Duration) {
	p.mu.Lock()
	if p.cancel != nil {
		p.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(parent)
	p.cancel = cancel
	p.mu.Unlock()

	p.reaperWG.Go(func() {
		t := p.clock.Load().NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C():
				p.reapLocked(ttl)
			}
		}
	})
}

// StopReaper stops the reaper goroutine and waits for it to exit.
func (p *PerExchangeCaseProvider) StopReaper() {
	p.mu.Lock()
	cancel := p.cancel
	p.cancel = nil
	p.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	p.reaperWG.Wait()
}

func (p *PerExchangeCaseProvider) reapLocked(ttl time.Duration) {
	cutoff := p.clock.Load().Now().Add(-ttl)
	p.mu.Lock()
	var (
		evicted []HandshakeKey
		gone    []*caseEntry
	)
	for k, e := range p.entries {
		if e.lastTouched.Before(cutoff) {
			delete(p.entries, k)
			evicted = append(evicted, k)
			gone = append(gone, e)
		}
	}
	onEvict := p.onEvict
	p.mu.Unlock()
	dropped(gone)
	// Call onEvict outside the lock; the callback (forgetSigma1Replied)
	// acquires the bridge's write lock and must not nest inside our mutex.
	if onEvict != nil {
		for _, id := range evicted {
			onEvict(id)
		}
	}
}
