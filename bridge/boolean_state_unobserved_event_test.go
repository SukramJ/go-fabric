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

// observedContact is a contact reading whose source can lose its
// observation, as a host source does while its device is unreachable.
type observedContact struct {
	mu       sync.Mutex
	cbs      []func()
	value    bool
	observed bool
}

func (*observedContact) MatterMeasurementClass() contract.MeasurementClass {
	return contract.MeasurementContact
}

func (s *observedContact) MatterBoolValue() (value, observed bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.value, s.observed
}

func (s *observedContact) OnMatterValueChanged(cb func()) func() {
	s.mu.Lock()
	s.cbs = append(s.cbs, cb)
	s.mu.Unlock()
	return func() {}
}

func (s *observedContact) set(value, observed bool) {
	s.mu.Lock()
	s.value, s.observed = value, observed
	cbs := append([]func(){}, s.cbs...)
	s.mu.Unlock()
	for _, cb := range cbs {
		cb()
	}
}

// TestBooleanStateChangeEventFollowsObservedReadings: StateChange follows
// the readings the source takes. A source that loses its observation reads
// false on the wire but emits no event and is not remembered, so an
// observation that differs from the last one taken — the first one
// included — emits the event. matter.js emits StateChange on
// stateValue$Changed, a change of a value the application set
// (packages/node/src/behaviors/boolean-state/BooleanStateServer.ts:20-28 at
// db91886c); an unobserved source sets none. The same rule as
// OccupancyChanged's (TestOccupancyChangedEventFollowsObservedReadings).
func TestBooleanStateChangeEventFollowsObservedReadings(t *testing.T) {
	t.Parallel()
	src := &observedContact{}
	asm, err := endpoint.New(endpointtest.NewFakeStore(), endpointtest.AssemblerConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	snap := func(ctx context.Context) (*endpoint.Topology, error) {
		return asm.Assemble(ctx, []endpoint.Snapshot{{Scope: "s", Endpoints: []endpoint.Spec{{
			StableKey: endpoint.StringKey("contact"), DeviceAddress: "C", DeviceType: 0x0015,
			FriendlyName: "Contact", ChannelAddress: "C:1", Measurement: src,
		}}}})
	}
	b := newStartedBridgeWithSnapshotter(t, snap)
	b.AttachSubscriptionManager(subscription.NewManager(subscription.Config{}, nil, nil))
	var ep uint16
	for _, e := range b.Topology().Bridged() {
		ep = e.ID
	}
	count := func() int { return len(b.EventLog().Query(ep, 0x0045, 0x00, 0)) }
	for i, step := range []struct {
		what            string
		value, observed bool
		want            int
	}{
		{"the first observed reading, false", false, true, 1},
		{"true", true, true, 2},
		{"the reading lost", false, false, 2},
		{"true observed again", true, true, 2},
		{"the reading lost again", false, false, 2},
		{"false observed", false, true, 3},
	} {
		src.set(step.value, step.observed)
		if n := count(); n != step.want {
			t.Fatalf("step %d, %s: %d StateChange events, want %d", i, step.what, n, step.want)
		}
	}
}
