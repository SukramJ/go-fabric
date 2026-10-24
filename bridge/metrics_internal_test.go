// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/contract"
)

// recordingMetrics is a contract.Metrics that keeps what it was told.
type recordingMetrics struct {
	mu       sync.Mutex
	counters map[string]int64
	gauges   map[string]int64
}

func (r *recordingMetrics) Counter(name string, delta int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.counters == nil {
		r.counters = map[string]int64{}
	}
	r.counters[name] += delta
}

func (r *recordingMetrics) Gauge(name string, value int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.gauges == nil {
		r.gauges = map[string]int64{}
	}
	r.gauges[name] = value
}

func (r *recordingMetrics) counter(name string) int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.counters[name]
}

// TestMetricsPortAttach: without a port nothing is recorded and nothing
// panics; the panic hook counts; a detach stops the recording; the
// sessions gauge reads a registry that counts.
func TestMetricsPortAttach(t *testing.T) {
	t.Parallel()
	b := &Bridge{}
	b.panicRecovered()
	b.recordSessionsOpen()
	b.recordSubscriptionsActive(nil)
	m := &recordingMetrics{}
	b.AttachMetrics(m)
	b.panicRecovered()
	if got := m.counter(contract.MetricPanicsRecovered); got != 1 {
		t.Errorf("panics.recovered = %d, want 1", got)
	}
	b.sessionCount.Store(&sessionCounterRef{c: activeCount(3)})
	b.recordSessionsOpen()
	m.mu.Lock()
	open := m.gauges[contract.MetricSessionsOpen]
	m.mu.Unlock()
	if open != 3 {
		t.Errorf("sessions.open = %d, want 3", open)
	}
	b.AttachMetrics(nil)
	b.panicRecovered()
	if got := m.counter(contract.MetricPanicsRecovered); got != 1 {
		t.Errorf("panics.recovered = %d after the detach, want 1", got)
	}
	var nilBridge *Bridge
	nilBridge.AttachMetrics(m)
}

type activeCount int

func (a activeCount) Active() int { return int(a) }
