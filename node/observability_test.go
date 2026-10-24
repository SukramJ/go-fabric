// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	basicinfo "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	onoffdef "github.com/SukramJ/go-fabric/cluster/spec/onoff"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/controller/commissioner"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"
)

// observedPair is a commissioned pair whose device reports to m and has
// one bridged light.
func observedPair(t *testing.T, site *fabrictest.Site, m *fabrictest.Metrics, extra func(*node.Config)) (*fabrictest.Pair, *device.OnOffLight) {
	t.Helper()
	light, err := device.NewOnOffLight(device.Options{StableKey: device.StringKey("light"), Name: "Lamp"})
	if err != nil {
		t.Fatal(err)
	}
	pair := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{
		Bridged: []node.Endpoint{light},
		Configure: func(cfg *node.Config) {
			cfg.Metrics = m
			if extra != nil {
				extra(cfg)
			}
		},
	}})
	return pair, light
}

func observeContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// TestMetricsOfAPairing: commissioning establishes sessions (the gauge is
// set), every Interaction Model message counts both ways, and DeviceLoad
// keeps its own counters beside the port.
func TestMetricsOfAPairing(t *testing.T) {
	t.Parallel()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	m := fabrictest.NewMetrics()
	pair, _ := observedPair(t, site, m, nil)
	ctx := observeContext(t)
	if _, err := fabrictest.Read(ctx, pair, client.AttributeAt(0, basicinfo.ClusterID, basicinfo.AttrNodeLabel)); err != nil {
		t.Fatal(err)
	}
	if v, ok := m.GaugeValue(node.MetricSessionsOpen); !ok || v < 1 {
		t.Errorf("sessions.open = %d (set %v), want at least the CASE session", v, ok)
	}
	if m.Count(node.MetricIMReceived) < 1 || m.Count(node.MetricIMSent) < 1 {
		t.Errorf("im.received %d, im.sent %d", m.Count(node.MetricIMReceived), m.Count(node.MetricIMSent))
	}
	load := pair.Device.Node().Bridge().DeviceLoad(0)
	if int64(load.TotalInteractionModelMessagesReceived) != m.Count(node.MetricIMReceived) {
		t.Errorf("DeviceLoad received %d, im.received %d: the two counters disagree",
			load.TotalInteractionModelMessagesReceived, m.Count(node.MetricIMReceived))
	}
	if m.Count(node.MetricPASEFailures) != 0 {
		t.Errorf("pase.failures = %d after a successful pairing", m.Count(node.MetricPASEFailures))
	}
}

// TestMetricsSubscriptionsActive: a subscription sets the gauge to one,
// its close on the device back to zero.
func TestMetricsSubscriptionsActive(t *testing.T) {
	t.Parallel()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	m := fabrictest.NewMetrics()
	pair, light := observedPair(t, site, m, nil)
	ctx := observeContext(t)
	ep, _ := light.EndpointNumber()
	sub, err := fabrictest.Subscribe(ctx, pair, fabrictest.SubscribeRequest{
		Attributes: []fabrictest.AttributePath{client.AttributeAt(ep, onoffdef.ClusterID, onoffdef.AttrOnOff)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := m.GaugeValue(node.MetricSubscriptionsActive); v != 1 {
		t.Errorf("subscriptions.active = %d after a subscribe, want 1", v)
	}
	defer sub.Close()
	// The device ends it (its endpoint's subscriptions close, as on a
	// removal): the close hook sets the gauge back.
	if n := pair.Device.Node().Subscriptions().CloseEndpoint(ep); n != 1 {
		t.Fatalf("closed %d subscriptions", n)
	}
	if v, _ := m.GaugeValue(node.MetricSubscriptionsActive); v != 0 {
		t.Errorf("subscriptions.active = %d after the close, want 0", v)
	}
}

// TestMetricsPASEFailure: a commissioning with the wrong passcode counts
// one PASE failure and no lockout.
func TestMetricsPASEFailure(t *testing.T) {
	t.Parallel()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	m := fabrictest.NewMetrics()
	u := site.UncommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{
		Configure: func(cfg *node.Config) { cfg.Metrics = m },
	}})
	ctx := observeContext(t)
	_, err := u.Controller.Commission(ctx, u.Device, fabrictest.CommissionOptions{Edit: func(o *commissioner.Options) {
		o.Passcode = 11223344
	}})
	if err == nil {
		t.Fatal("a commissioning with the wrong passcode succeeded")
	}
	if err := site.Eventually(ctx, "the failure", func() bool { return m.Count(node.MetricPASEFailures) >= 1 }); err != nil {
		t.Fatalf("pase.failures = %d: %v", m.Count(node.MetricPASEFailures), err)
	}
	if m.Count(node.MetricPASELockouts) != 0 {
		t.Errorf("pase.lockouts = %d after one failure", m.Count(node.MetricPASELockouts))
	}
}

// TestMetricsReassemblyAndRetransmit: a device added to the running
// node counts one reassembly; a report the network drops once is
// retransmitted and counted.
func TestMetricsReassemblyAndRetransmit(t *testing.T) {
	t.Parallel()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	m := fabrictest.NewMetrics()
	pair, _ := observedPair(t, site, m, nil)
	ctx := observeContext(t)
	before := m.Count(node.MetricAggregatorReassemblies)
	second, err := device.NewOnOffLight(device.Options{StableKey: device.StringKey("light2"), Name: "Lamp 2"})
	if err != nil {
		t.Fatal(err)
	}
	if err := pair.Device.Act(func(n *node.Node) error { return n.Aggregator().Add(ctx, second) }); err != nil {
		t.Fatal(err)
	}
	if got := m.Count(node.MetricAggregatorReassemblies); got != before+1 {
		t.Errorf("aggregator.reassemblies %d, want %d", got, before+1)
	}

	var mu sync.Mutex
	dropped := 0
	site.Net().Filter(func(d fabrictest.Datagram) bool {
		mu.Lock()
		defer mu.Unlock()
		if dropped == 0 && d.From.String() == pair.Device.Addr().String() {
			dropped++
			return false
		}
		return true
	})
	if _, err := fabrictest.Read(ctx, pair, client.AttributeAt(0, basicinfo.ClusterID, basicinfo.AttrNodeLabel)); err != nil {
		t.Fatalf("read with the device's reply dropped once: %v", err)
	}
	site.Net().Filter(nil)
	if got := m.Count(node.MetricMRPRetransmits); got < 1 {
		t.Errorf("mrp.retransmits = %d after a dropped reply", got)
	}
}

// TestDiagnosticEventsOnTheFacade: the node's trace is read from the
// facade, an inline observer sees each event as it is recorded, an
// asynchronous one gets the same events by Stop, and the capacity bounds
// the trace.
func TestDiagnosticEventsOnTheFacade(t *testing.T) {
	t.Parallel()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	var mu sync.Mutex
	var inline, async []node.DiagnosticEvent
	pair := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{
		Configure: func(cfg *node.Config) { cfg.Diagnostics = node.Diagnostics{Capacity: 2} },
		Prepare: func(n *node.Node) {
			n.OnDiagnosticEvent(func(e node.DiagnosticEvent) {
				mu.Lock()
				inline = append(inline, e)
				mu.Unlock()
			})
			n.OnDiagnosticEventAsync(func(e node.DiagnosticEvent) {
				mu.Lock()
				async = append(async, e)
				mu.Unlock()
			})
			stopped := n.OnDiagnosticEvent(func(node.DiagnosticEvent) { t.Error("an unsubscribed observer ran") })
			stopped()
		},
	}})
	ctx := observeContext(t)
	n := pair.Device.Node()
	if err := pair.Controller.Restart(ctx); err != nil {
		t.Fatal(err)
	}
	// The controller's stop closes its session on the wire, the bridge
	// re-announces itself: two more events than the trace keeps.
	if err := site.Eventually(ctx, "the close", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(inline) >= 3
	}); err != nil {
		t.Fatal(err)
	}
	events := n.DiagnosticEvents()
	if len(events) != 2 {
		t.Fatalf("%d events in a trace of capacity 2", len(events))
	}
	if err := pair.Device.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !slices.EqualFunc(inline, async, func(a, b node.DiagnosticEvent) bool { return a.At.Equal(b.At) && a.Message == b.Message }) {
		t.Errorf("async %d events differ from inline %d", len(async), len(inline))
	}
	// Newest first, as the ring's Snapshot.
	if last := inline[len(inline)-1]; events[0].Message != last.Message || !events[0].At.Equal(last.At) {
		t.Errorf("DiagnosticEvents()[0] = %q, the newest inline %q", events[0].Message, last.Message)
	}
}
