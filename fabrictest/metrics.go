// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest

import (
	"sync"

	"github.com/SukramJ/go-fabric/node"
)

// Metrics is a recording metrics port (node.Metrics) for a test: a device
// of the site reports to it when its Topology.Configure sets
// cfg.Metrics. Safe for concurrent use.
type Metrics struct {
	// mu guards counters and gauges.
	mu       sync.Mutex
	counters map[string]int64
	gauges   map[string]int64
}

var _ node.Metrics = (*Metrics)(nil)

// NewMetrics returns an empty recording port.
func NewMetrics() *Metrics {
	return &Metrics{counters: map[string]int64{}, gauges: map[string]int64{}}
}

// Counter implements node.Metrics.
func (m *Metrics) Counter(name string, delta int64) {
	m.mu.Lock()
	m.counters[name] += delta
	m.mu.Unlock()
}

// Gauge implements node.Metrics.
func (m *Metrics) Gauge(name string, value int64) {
	m.mu.Lock()
	m.gauges[name] = value
	m.mu.Unlock()
}

// Count returns the sum of the deltas counter name received.
func (m *Metrics) Count(name string) int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.counters[name]
}

// GaugeValue returns the last value gauge name was set to, and whether it
// was set at all.
func (m *Metrics) GaugeValue(name string) (int64, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.gauges[name]
	return v, ok
}
