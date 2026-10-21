// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package peer

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/client/exchange"
)

// ReachabilityReason says why a peer's reachability is verified —
// matter.js ReachabilityReason (protocol/ExchangeProvider.ts).
type ReachabilityReason int

const (
	// ReasonAddressChange is a check after DNS-SD stopped naming the
	// session's address: the current address is probed, then the other
	// discovered ones.
	ReasonAddressChange ReachabilityReason = iota
	// ReasonSessionSuspect is a check after the session itself looked dead
	// (a subscription timed out, say): only the current address is probed,
	// and the probe cooldown is reset first.
	ReasonSessionSuspect
)

func (r ReachabilityReason) String() string {
	if r == ReasonSessionSuspect {
		return "session-suspect"
	}
	return "address-change"
}

// monitor follows a peer's discovered addresses and probes the session
// when its address leaves them — matter.js PeerAddressMonitor
// (packages/protocol/src/peer/PeerAddressMonitor.ts:43-293 at 0ceb202d)
// without the ICD branch (:143-172).
type monitor struct {
	p     *Peer
	delay time.Duration
	min   time.Duration
	max   time.Duration

	// mu guards closed, fibCurr, fibPrev, lastProbeAt, running and timer.
	mu          sync.Mutex
	timer       exchange.Timer
	running     *monitorRun
	closed      bool
	lastProbeAt time.Time
	fibPrev     time.Duration
	fibCurr     time.Duration
}

// monitorRun is one verification other callers join.
type monitorRun struct {
	done chan struct{}
	ok   bool
}

// addressMonitor is the peer's monitor, created on first use with the
// set's timing (Peer.ts:603-614).
func (p *Peer) addressMonitor() *monitor {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.monitor == nil {
		t := p.set.Timing()
		p.monitor = &monitor{
			p:     p,
			delay: t.AddressChangeStabilizationDelay,
			min:   t.AddressChangeProbeCooldown.Min,
			max:   t.AddressChangeProbeCooldown.Max,
		}
	}
	return p.monitor
}

// scheduleAddressCheck schedules the monitor's check (Peer.ts:137-138).
func (p *Peer) scheduleAddressCheck() { p.addressMonitor().schedule() }

// VerifyReachability verifies the peer is reachable, driving recovery:
// false at once without a live session; otherwise the monitor's run,
// which concurrent callers join (Peer.ts:616-626). It reports whether the
// session is usable afterwards; the error is ctx's when ctx ended first —
// the run goes on for the others.
func (p *Peer) VerifyReachability(ctx context.Context, reason ReachabilityReason) (bool, error) {
	if !p.HasSession() {
		return false, nil
	}
	return p.addressMonitor().verify(ctx, reason)
}

// schedule restarts the stabilisation timer when the peer has a live
// session, so a burst of changes is checked once (PeerAddressMonitor.ts:74-84).
func (m *monitor) schedule() {
	if !m.p.HasSession() {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return
	}
	if m.timer != nil {
		m.timer.Stop()
	}
	m.timer = m.p.set.clock.AfterFunc(m.delay, func() {
		m.p.goWork(func() { _, _ = m.verify(m.p.ctx, ReasonAddressChange) })
	})
}

// stop stops the timer; a run in flight goes on (:86-91).
func (m *monitor) stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.timer != nil {
		m.timer.Stop()
	}
}

// close stops the timer and waits for a run in flight (:93-100); the
// peer's context has ended, so the run unwinds promptly.
func (m *monitor) close() {
	m.mu.Lock()
	m.closed = true
	if m.timer != nil {
		m.timer.Stop()
	}
	r := m.running
	m.mu.Unlock()
	if r != nil {
		<-r.done
	}
}

// verify runs the verification or joins the one in flight; only an
// address-change run re-arms the timer afterwards (:102-127).
func (m *monitor) verify(ctx context.Context, reason ReachabilityReason) (bool, error) {
	m.mu.Lock()
	r := m.running
	if r == nil {
		r = &monitorRun{done: make(chan struct{})}
		m.running = r
		go func() {
			ok := m.run(reason)
			m.mu.Lock()
			m.running = nil
			m.mu.Unlock()
			r.ok = ok
			close(r.done)
			if reason == ReasonAddressChange {
				m.schedule()
			}
		}()
	}
	m.mu.Unlock()
	select {
	case <-r.done:
		return r.ok, nil
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

// run is one verification (PeerAddressMonitor.ts:129-267, the ICD branch
// aside). The subscription close after a migration (:244-246) is the
// set's SessionMigrated hook.
func (m *monitor) run(reason ReachabilityReason) bool {
	p := m.p
	ctx := p.ctx
	s := p.NewestSession()
	if s == nil {
		return false
	}
	current := addressOf(s.Addr()).AddrPort()
	discovered := p.service.Addresses()
	log := p.log.With(slog.String("reason", reason.String()), slog.String("address", current.String()))
	if reason == ReasonSessionSuspect {
		m.resetBackoff()
	}
	if reason == ReasonAddressChange {
		if len(discovered) == 0 {
			return true
		}
		for _, a := range discovered {
			if a.AddrPort() == current {
				return true
			}
		}
		now := p.set.clock.Now()
		m.mu.Lock()
		lastKnownGood := m.lastProbeAt
		if at := s.ActiveTimestamp(); at.After(lastKnownGood) {
			lastKnownGood = at
		}
		recent := !lastKnownGood.IsZero() && now.Sub(lastKnownGood) < m.cooldownLocked()
		if !recent {
			m.lastProbeAt = now
		}
		m.mu.Unlock()
		if recent {
			return true
		}
		log.Info("matter.controller.peer.address_gone_probing")
	}
	mgr := p.set.ctx.Exchanges
	if probe(ctx, mgr, s, nil, log) {
		if reason == ReasonAddressChange {
			m.advanceBackoff()
		}
		return true
	}
	if reason == ReasonAddressChange {
		for _, a := range discovered {
			if ctx.Err() != nil {
				return false
			}
			if a.AddrPort() == current {
				continue
			}
			if probe(ctx, mgr, s, a.UDPAddr(), log) {
				log.Info("matter.controller.peer.session_migrated", slog.String("to", a.String()))
				s.SetAddr(a.UDPAddr())
				p.followAddress(s)
				if migrated := p.set.ctx.SessionMigrated; migrated != nil {
					migrated(p.address)
				}
				m.resetBackoff()
				return true
			}
		}
		m.resetBackoff()
	}
	if ctx.Err() != nil {
		return false
	}
	log.Info("matter.controller.peer.all_probes_failed")
	s.HandlePeerLoss(&exchange.PeerUnresponsiveError{})
	return false
}

// cooldownLocked is the current probe cooldown: the Fibonacci position or
// the minimum (:269-272).
func (m *monitor) cooldownLocked() time.Duration {
	if m.fibCurr != 0 {
		return m.fibCurr
	}
	return m.min
}

// advanceBackoff steps the Fibonacci cooldown: first (min, min), then
// prev+curr capped at the maximum (:274-286).
func (m *monitor) advanceBackoff() time.Duration {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fibCurr == 0 {
		m.fibPrev, m.fibCurr = m.min, m.min
	} else {
		m.fibPrev, m.fibCurr = m.fibCurr, min(m.fibPrev+m.fibCurr, m.max)
	}
	return m.fibCurr
}

// resetBackoff drops the accumulated trust (:288-292).
func (m *monitor) resetBackoff() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fibPrev, m.fibCurr = 0, 0
	m.lastProbeAt = time.Time{}
}
