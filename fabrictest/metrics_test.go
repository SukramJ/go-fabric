// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest_test

import (
	"context"
	"testing"
	"time"

	basicinfo "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"
)

// TestSubscriptionsActiveGauge: the node's subscriptions.active gauge
// follows every subscription a controller establishes — the observer of
// the count a host attaches through node.Config.Metrics instead of polling
// Node.Subscriptions().Active() (brief CJ §1.17).
func TestSubscriptionsActiveGauge(t *testing.T) {
	t.Parallel()
	m := fabrictest.NewMetrics()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	pair := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{
		Configure: func(cfg *node.Config) { cfg.Metrics = m },
	}})
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	for want := int64(1); want <= 2; want++ {
		var req fabrictest.SubscribeRequest
		req.Attributes = []client.AttributePath{client.AttributeAt(0, basicinfo.ClusterID, basicinfo.AttrNodeLabel)}
		req.MaxIntervalCeiling = client.Interval(time.Minute)
		if _, err := fabrictest.Subscribe(ctx, pair, req); err != nil {
			t.Fatal(err)
		}
		if v, ok := m.GaugeValue(node.MetricSubscriptionsActive); !ok || v != want {
			t.Fatalf("subscriptions.active = %d (%v), want %d", v, ok, want)
		}
		if got := pair.Device.Node().Subscriptions().Active(); int64(got) != want {
			t.Fatalf("Subscriptions().Active() = %d, want %d", got, want)
		}
	}
}

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
