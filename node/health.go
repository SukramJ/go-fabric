// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/bridge"
)

// HealthSample is one verdict of the node's health probe
// ([bridge.HealthSample]): Healthy while the operational UDP listener is
// bound, with a stable English Note.
type HealthSample = bridge.HealthSample

// DefaultProbeInterval is the probe cadence a zero [Health].Interval
// selects ([bridge.DefaultProbeInterval], 30 s).
const DefaultProbeInterval = bridge.DefaultProbeInterval

// Health configures the node's health probe (bridge.StartHealthProbe):
// from [Node.Start] to [Node.Stop] the node checks its operational
// listener on [Config].Clock and hands each verdict to the callbacks
// [Node.OnHealthSample] registered; [Node.Health] returns the last one.
// A non-nil [Config].Health runs the probe, an optional sub-system (ADR
// 0025 principle 2).
type Health struct {
	// Interval is the probe cadence; 0 is [DefaultProbeInterval].
	Interval time.Duration
}

// healthProbe is the node's probe: the last sample and the host's
// callbacks, behind the bridge's HealthRecorder.
type healthProbe struct {
	onSample func(HealthSample)
	stop     func()

	// mu guards last and seen.
	mu   sync.Mutex
	last HealthSample
	seen bool
}

// Record implements bridge.HealthRecorder.
func (p *healthProbe) Record(_ string, s HealthSample) { p.record(s) }

// RecordUnhealthy implements bridge.HealthRecorder.
func (p *healthProbe) RecordUnhealthy(_ string, s HealthSample) { p.record(s) }

func (p *healthProbe) record(s HealthSample) {
	p.mu.Lock()
	p.last, p.seen = s, true
	p.mu.Unlock()
	if p.onSample != nil {
		p.onSample(s)
	}
}

// startHealthProbe begins the probe Config.Health asks for; Start calls
// it once the bridge's listener is bound.
func (n *Node) startHealthProbe() {
	h := n.cfg.Health
	if h == nil {
		return
	}
	p := &healthProbe{onSample: func(s HealthSample) { n.healthListeners.emit(n.logger, "health_sample", s) }}
	p.stop = bridge.StartHealthProbeWithClock(n.life, n.cfg.Clock, n.br, p, h.Interval)
	n.healthMu.Lock()
	n.health = p
	n.healthMu.Unlock()
}

// stopHealthProbe ends the probe before the bridge stops, so the
// listener's teardown is not reported as a failure; [Node.Health] keeps
// the last verdict.
func (n *Node) stopHealthProbe() {
	n.healthMu.Lock()
	p := n.health
	n.healthMu.Unlock()
	if p != nil {
		p.stop()
	}
}

// OnHealthSample registers fn for every verdict of the health probe
// ([Config].Health), the first at [Node.Start]. An unhealthy one (Healthy
// false) is a condition the probe states, not a sample — what
// bridge.HealthRecorder.RecordUnhealthy receives — so a host that damps
// flapping samples records it at once. fn runs inline on the probe's
// goroutine and must not block; [Node.OnHealthSampleAsync] queues it
// instead. It returns the unsubscribe function (ADR 0025 principle 8).
func (n *Node) OnHealthSample(fn func(HealthSample)) (unsubscribe func()) {
	return n.healthListeners.add(fn)
}

// OnHealthSampleAsync is [Node.OnHealthSample] with fn queued on the
// node's callback goroutine, one call after the other, while the node
// runs; Stop returns once every queued call ran.
func (n *Node) OnHealthSampleAsync(fn func(HealthSample)) (unsubscribe func()) {
	if fn == nil {
		return func() {}
	}
	return n.healthListeners.add(func(s HealthSample) { n.queued("health_sample", func() { fn(s) }) })
}

// Health returns the health probe's last verdict; false before the first
// one and when [Config].Health runs no probe.
func (n *Node) Health() (HealthSample, bool) {
	n.healthMu.Lock()
	p := n.health
	n.healthMu.Unlock()
	if p == nil {
		return HealthSample{}, false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.last, p.seen
}
