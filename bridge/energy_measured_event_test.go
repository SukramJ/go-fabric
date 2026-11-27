// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"slices"
	"sync"
	"testing"

	"thingfabric.dev/go-fabric/cluster/measurement"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/endpoint"
	"thingfabric.dev/go-fabric/endpoint/endpointtest"
	"thingfabric.dev/go-fabric/im/subscription"
)

// fakeMeter is an electrical sensor channel with an energy counter (Wh).
type fakeMeter struct {
	mu       sync.Mutex
	cbs      []func()
	energy   float64
	observed bool
	power    float64
	powered  bool
	voltage  float64
	volted   bool
}

func (*fakeMeter) MatterMeasurementClass() contract.MeasurementClass {
	return contract.MeasurementElectrical
}
func (*fakeMeter) Current() (float64, bool)   { return 0, false }
func (*fakeMeter) Frequency() (float64, bool) { return 0, false }
func (*fakeMeter) HasEnergy() bool            { return true }

func (m *fakeMeter) Voltage() (float64, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.voltage, m.volted
}

func (m *fakeMeter) Energy() (float64, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.energy, m.observed
}

func (m *fakeMeter) ActivePower() (float64, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.power, m.powered
}

func (m *fakeMeter) MatterFloatValue() (float64, bool) { return m.ActivePower() }

func (m *fakeMeter) OnMatterValueChanged(cb func()) func() {
	m.mu.Lock()
	m.cbs = append(m.cbs, cb)
	m.mu.Unlock()
	return func() {}
}

func (m *fakeMeter) set(wh float64) {
	m.mu.Lock()
	m.energy, m.observed = wh, true
	m.mu.Unlock()
	m.notify()
}

func (m *fakeMeter) setPower(w float64) {
	m.mu.Lock()
	m.power, m.powered = w, true
	m.mu.Unlock()
	m.notify()
}

func (m *fakeMeter) setVoltage(v float64) {
	m.mu.Lock()
	m.voltage, m.volted = v, true
	m.mu.Unlock()
	m.notify()
}

func (m *fakeMeter) notify() {
	m.mu.Lock()
	cbs := append([]func(){}, m.cbs...)
	m.mu.Unlock()
	for _, cb := range cbs {
		cb()
	}
}

// TestCumulativeEnergyMeasuredEvent: the measurement-projected
// ElectricalEnergyMeasurement server, bound to CumulativeEnergy and
// ImportedEnergy, lists CumulativeEnergyMeasured — mandatory with CUME
// (packages/model/src/standard/elements/electrical-energy-measurement.element.ts:56-59
// at db91886c) — and the event is emitted, carrying the imported energy,
// when the source's cumulative value changes: matter.js emits it with each
// measurement set through setMeasurement
// (packages/node/src/behaviors/electrical-energy-measurement/ElectricalEnergyMeasurementServer.ts:44-62),
// connectedhomeip when a snapshot changes the attribute
// (src/app/clusters/electrical-energy-measurement-server/ElectricalEnergyMeasurementCluster.cpp:442-469
// at 6170af84). A notification that does not move the value emits nothing
// (review II, R2-P4-nv-05a).
func TestCumulativeEnergyMeasuredEvent(t *testing.T) {
	t.Parallel()
	src := &fakeMeter{}
	asm, err := endpoint.New(endpointtest.NewFakeStore(), endpointtest.AssemblerConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	snap := func(ctx context.Context) (*endpoint.Topology, error) {
		return asm.Assemble(ctx, []endpoint.Snapshot{{Scope: "s", Endpoints: []endpoint.Spec{{
			StableKey: endpoint.StringKey("meter"), DeviceAddress: "M", DeviceType: 0x0510,
			FriendlyName: "Meter", ChannelAddress: "M:1", Measurement: src,
		}}}})
	}
	b := newStartedBridgeWithSnapshotter(t, snap)
	b.AttachSubscriptionManager(subscription.NewManager(subscription.Config{}, nil, nil))
	var ep uint16
	for _, e := range b.Topology().Bridged() {
		ep = e.ID
	}
	var lister contract.ClusterEventLister
	for _, srv := range endpoint.ClusterServers(b.Topology().FindByID(ep)) {
		if srv.MatterClusterID() == measurement.ClusterElectricalEnergy {
			lister, _ = srv.(contract.ClusterEventLister)
		}
	}
	if lister == nil || !slices.Contains(lister.MatterEvents(), measurement.ElectricalEnergyEventCumulativeMeasured) {
		t.Fatal("the ElectricalEnergyMeasurement server does not list CumulativeEnergyMeasured")
	}
	events := func() int { return len(b.EventLog().Query(ep, measurement.ClusterElectricalEnergy, 0x00, 0)) }

	src.set(1.5)
	if n := events(); n != 1 {
		t.Fatalf("after a change: %d CumulativeEnergyMeasured events, want 1", n)
	}
	src.set(1.5)
	if n := events(); n != 1 {
		t.Fatalf("after an unchanged notification: %d CumulativeEnergyMeasured events, want still 1", n)
	}
	src.set(2.25)
	got := b.EventLog().Query(ep, measurement.ClusterElectricalEnergy, 0x00, 0)
	if len(got) != 2 {
		t.Fatalf("after a second change: %d CumulativeEnergyMeasured events, want 2", len(got))
	}
	data, ok := got[1].Payload.(measurement.CumulativeEnergyMeasuredEvent)
	if !ok || data.EnergyImported == nil || data.EnergyImported.Energy != 2250 || data.EnergyExported != nil {
		t.Fatalf("event payload %#v, want EnergyImported 2250 mWh and no EnergyExported", got[1].Payload)
	}
	// The payload goes on the wire as its struct, not as the null of a
	// value the writer does not know (0x35: a context-tagged structure).
	if out := writeValue(t, data); len(out) == 0 || out[0] != 0x35 {
		t.Fatalf("event payload encodes as % x, want a structure", out)
	}
}
