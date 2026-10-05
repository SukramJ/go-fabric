// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/endpoint/endpointtest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/im/subscription"
)

type fakeContact struct {
	mu    sync.Mutex
	cbs   []func()
	value bool
}

func (*fakeContact) MatterMeasurementClass() contract.MeasurementClass {
	return contract.MeasurementContact
}

func (s *fakeContact) MatterBoolValue() (bool, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.value, true
}

func (s *fakeContact) OnMatterValueChanged(cb func()) func() {
	s.mu.Lock()
	s.cbs = append(s.cbs, cb)
	s.mu.Unlock()
	return func() {}
}

func (s *fakeContact) set(v bool) {
	s.mu.Lock()
	s.value = v
	cbs := append([]func(){}, s.cbs...)
	s.mu.Unlock()
	for _, cb := range cbs {
		cb()
	}
}

// TestBooleanStateChangeEvent ports matter.js
// packages/node/test/behaviors/boolean-state/BooleanStateServerTest.ts: the
// ChangeEvent feature is on by default and a StateValue change emits
// StateChange (event 0x00) carrying the new value; a notification that does
// not change the value emits nothing.
func TestBooleanStateChangeEvent(t *testing.T) {
	t.Parallel()
	src := &fakeContact{}
	asm, err := endpoint.New(endpointtest.NewFakeStore(), endpointtest.AssemblerConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	snap := func(ctx context.Context) (*endpoint.Topology, error) {
		return asm.Assemble(ctx, []endpoint.Snapshot{{Scope: "s", ModelComplete: true, Endpoints: []endpoint.Spec{{
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
	count := func() int {
		return len(b.EventLog().Query(ep, 0x0045, 0x00, 0))
	}
	if fm, _ := readFeatureMap(t, b, ep); fm != 1 {
		t.Fatalf("BooleanState FeatureMap = %d, want 1 (ChangeEvent)", fm)
	}
	src.set(true)
	if n := count(); n != 1 {
		t.Fatalf("after a change: %d StateChange events, want 1", n)
	}
	src.set(true)
	if n := count(); n != 1 {
		t.Fatalf("after an unchanged notification: %d StateChange events, want still 1", n)
	}
}

func readFeatureMap(t *testing.T, b *Bridge, ep uint16) (uint32, bool) {
	t.Helper()
	for _, srv := range endpoint.ClusterServers(b.Topology().FindByID(ep)) {
		if srv.MatterClusterID() == 0x0045 {
			v, ok := srv.MatterRead(0xFFFC)
			u, _ := v.(uint32)
			return u, ok
		}
	}
	return 0, false
}

// TestPathValuesReportOnlyMovedAttributes pins the change filter behind a
// source's notification: matter.js Datasource broadcasts only the
// properties whose value changed, so an unchanged attribute is not
// reported again (TC-FAN-3.2 counts the FanMode reports).
func TestPathValuesReportOnlyMovedAttributes(t *testing.T) {
	t.Parallel()
	src := &fakeContact{}
	asm, err := endpoint.New(endpointtest.NewFakeStore(), endpointtest.AssemblerConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	topo, err := asm.Assemble(context.Background(), []endpoint.Snapshot{{Scope: "s", ModelComplete: true, Endpoints: []endpoint.Spec{{
		StableKey: endpoint.StringKey("contact"), DeviceAddress: "C", DeviceType: 0x0015,
		FriendlyName: "Contact", ChannelAddress: "C:1", Measurement: src,
	}}}})
	if err != nil {
		t.Fatal(err)
	}
	ep := topo.Bridged()[0]
	state := im.ConcreteAttributePath{Endpoint: ep.ID, Cluster: 0x0045, Attribute: 0, HasEndpoint: true, HasCluster: true, HasAttribute: true}
	missing := im.ConcreteAttributePath{Endpoint: ep.ID, Cluster: 0x0045, Attribute: 0x7777, HasEndpoint: true, HasCluster: true, HasAttribute: true}
	v := newPathValues(ep, []im.ConcreteAttributePath{state, missing})
	if got := v.changed(); len(got) != 1 || got[0] != missing {
		t.Fatalf("nothing moved: %v, want only the unreadable path", got)
	}
	src.mu.Lock()
	src.value = true
	src.mu.Unlock()
	if got := v.changed(); len(got) != 2 || got[0] != state {
		t.Fatalf("StateValue moved: %v, want it reported", got)
	}
	if got := v.changed(); len(got) != 1 {
		t.Fatalf("after the report: %v, want StateValue quiet again", got)
	}
	if derefValue((*int)(nil)) != nil || derefValue(nil) != nil {
		t.Fatal("derefValue of nil")
	}
}
