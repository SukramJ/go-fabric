// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im/subscription"
)

// metricsPort holds the attached [contract.Metrics]; the bridge keeps it
// behind an atomic pointer because the record sites sit on the receive
// path, which runs concurrently with an attach.
type metricsPort struct{ m contract.Metrics }

// AttachMetrics wires the port the bridge reports its named counters and
// gauges to ([contract.MetricNames]): Interaction Model messages,
// sessions, subscriptions, PASE failures and lockouts, MRP retransmits
// and recovered panics. Passing nil detaches it; without one nothing is
// recorded. The counters the bridge keeps for its own readers
// ([Bridge.DeviceLoad], the PASE lockout, the listeners' RecoveredPanics)
// count on regardless.
//
// Safe to call while the bridge serves. The panic hook reaches a listener
// opened after the attach; the node attaches before Start.
func (b *Bridge) AttachMetrics(m contract.Metrics) {
	if b == nil {
		return
	}
	if m == nil {
		b.metrics.Store(nil)
		return
	}
	b.metrics.Store(&metricsPort{m: m})
}

// count adds delta to the counter name when a port is attached.
func (b *Bridge) count(name string, delta int64) {
	if p := b.metrics.Load(); p != nil {
		p.m.Counter(name, delta)
	}
}

// gauge sets the gauge name when a port is attached.
func (b *Bridge) gauge(name string, value int64) {
	if p := b.metrics.Load(); p != nil {
		p.m.Gauge(name, value)
	}
}

// panicRecovered is the listeners' panic hook: one recovered panic.
func (b *Bridge) panicRecovered() { b.count(contract.MetricPanicsRecovered, 1) }

// sessionCounter is the optional capability of a [SessionRegistry] to
// count its sessions (secure/operational.Manager.Active).
type sessionCounter interface{ Active() int }

// sessionCounterRef holds the attached registry's counter, nil when the
// registry does not count; atomic so the close hook, which may run under
// the bridge's lock, never takes it.
type sessionCounterRef struct{ c sessionCounter }

// recordSessionsOpen sets the sessions.open gauge from the session
// registry, when a port is attached and the registry counts.
func (b *Bridge) recordSessionsOpen() {
	if b.metrics.Load() == nil {
		return
	}
	if ref := b.sessionCount.Load(); ref != nil {
		b.gauge(contract.MetricSessionsOpen, int64(ref.c.Active()))
	}
}

// recordSubscriptionsActive sets the subscriptions.active gauge from m,
// when a port is attached. The callers hold m: the close hook runs where
// the bridge's lock may be held.
func (b *Bridge) recordSubscriptionsActive(m *subscription.Manager) {
	if m == nil || b.metrics.Load() == nil {
		return
	}
	b.gauge(contract.MetricSubscriptionsActive, int64(m.Active()))
}
