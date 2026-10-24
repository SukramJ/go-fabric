// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"
)

// TestMetricsRecords: counters add up, a gauge keeps its last value, an
// unset gauge says so.
func TestMetricsRecords(t *testing.T) {
	t.Parallel()
	m := fabrictest.NewMetrics()
	m.Counter(node.MetricIMSent, 2)
	m.Counter(node.MetricIMSent, 1)
	m.Gauge(node.MetricSessionsOpen, 3)
	m.Gauge(node.MetricSessionsOpen, 1)
	if got := m.Count(node.MetricIMSent); got != 3 {
		t.Errorf("im.sent = %d, want 3", got)
	}
	if v, ok := m.GaugeValue(node.MetricSessionsOpen); !ok || v != 1 {
		t.Errorf("sessions.open = %d (%v), want 1", v, ok)
	}
	if _, ok := m.GaugeValue(node.MetricSubscriptionsActive); ok {
		t.Error("an unset gauge reads as set")
	}
}
