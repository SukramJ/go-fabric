// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"sync"
	"testing"

	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/endpoint"
	"thingfabric.dev/go-fabric/endpoint/endpointtest"
	"thingfabric.dev/go-fabric/im/subscription"
)

// observedOccupancy is a PIR occupancy reading whose source can lose its
// observation, as a host source does while its device is unreachable.
type observedOccupancy struct {
	mu       sync.Mutex
	cbs      []func()
	value    bool
	observed bool
}

func (*observedOccupancy) MatterMeasurementClass() contract.MeasurementClass {
	return contract.MeasurementOccupancy
}

func (s *observedOccupancy) MatterBoolValue() (value, observed bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.value, s.observed
}

func (s *observedOccupancy) OnMatterValueChanged(cb func()) func() {
	s.mu.Lock()
	s.cbs = append(s.cbs, cb)
	s.mu.Unlock()
	return func() {}
}

func (s *observedOccupancy) set(value, observed bool) {
	s.mu.Lock()
	s.value, s.observed = value, observed
	cbs := append([]func(){}, s.cbs...)
	s.mu.Unlock()
	for _, cb := range cbs {
		cb()
	}
}

// TestOccupancyChangedEventFollowsObservedReadings: OccupancyChanged
// follows the readings the source takes. A source that loses its
// observation reads 0 on the wire but emits no event and is not
// remembered, so an observation that differs from the last one taken —
// the first one included — emits the event. matter.js emits on
// occupancy$Changed, a change of a value the application set
// (packages/node/src/behaviors/occupancy-sensing/OccupancySensingServer.ts:66-69,
// :88-90 at db021da0); an unobserved source sets none.
func TestOccupancyChangedEventFollowsObservedReadings(t *testing.T) {
	t.Parallel()
	src := &observedOccupancy{}
	asm, err := endpoint.New(endpointtest.NewFakeStore(), endpointtest.AssemblerConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	snap := func(ctx context.Context) (*endpoint.Topology, error) {
		return asm.Assemble(ctx, []endpoint.Snapshot{{Scope: "s", Endpoints: []endpoint.Spec{{
			StableKey: endpoint.StringKey("occ"), DeviceAddress: "O", DeviceType: 0x0107,
			FriendlyName: "Occupancy", ChannelAddress: "O:1", Measurement: src,
		}}}})
	}
	b := newStartedBridgeWithSnapshotter(t, snap)
	b.AttachSubscriptionManager(subscription.NewManager(subscription.Config{}, nil, nil))
	var ep uint16
	for _, e := range b.Topology().Bridged() {
		ep = e.ID
	}
	count := func() int { return len(b.EventLog().Query(ep, 0x0406, 0x00, 0)) }
	for i, step := range []struct {
		what            string
		value, observed bool
		want            int
	}{
		{"the first observed reading, unoccupied", false, true, 1},
		{"occupied", true, true, 2},
		{"the reading lost", false, false, 2},
		{"unoccupied observed again", false, true, 3},
		{"the reading lost again", true, false, 3},
		{"unoccupied, as last observed", false, true, 3},
	} {
		src.set(step.value, step.observed)
		if n := count(); n != step.want {
			t.Fatalf("step %d, %s: %d OccupancyChanged events, want %d", i, step.what, n, step.want)
		}
	}
}
