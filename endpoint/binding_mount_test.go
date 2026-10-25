// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"context"
	"slices"
	"testing"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/contract"
)

// TestDeclaredClientMountsBinding: an endpoint that declares a bindable
// application client (OnOff) reports it in its Descriptor's ClientList,
// satisfies Base's Client condition and gets the Binding server Base then
// mandates ("Simple & Client") without its source supplying one — matter.js
// BindingServer.ts:54-56, ResolvedEndpoint.ts:120-139,
// ConditionAssertions.ts:600-602. One that declares only an unbindable
// client (WebRtcTransportProvider, effectiveBindable false) does not; nor
// does one that declares none. The mounted server is the endpoint's own:
// the same instance on every dispatch.
func TestDeclaredClientMountsBinding(t *testing.T) {
	t.Parallel()
	const switchDT, onOff, webRTCProvider = 0x0103, 0x0006, 0x0553
	a, err := New(newInternalFakeStore(), Config{VendorID: 1, ProductID: 1, NodeLabel: "x"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	src := func() contract.EndpointSource {
		return deviceTypeSource{dt: switchDT, servers: []contract.ClusterServer{dtServer{id: 0x0003}}}
	}
	topo, err := a.Assemble(context.Background(), []Snapshot{{Scope: "s", Endpoints: []Spec{
		{StableKey: StringKey("bound"), DeviceType: switchDT, FriendlyName: "Bound", ClientClusters: []uint32{onOff}, Source: src()},
		{StableKey: StringKey("unbindable"), DeviceType: switchDT, FriendlyName: "Unbindable", ClientClusters: []uint32{webRTCProvider}, Source: src()},
		{StableKey: StringKey("none"), DeviceType: switchDT, FriendlyName: "None", Source: src()},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	byKey := map[string]*Endpoint{}
	for _, ep := range topo.Endpoints {
		if ep.SourceKey != nil {
			byKey[ep.SourceKey.String()] = ep
		}
	}
	bindingOf := func(ep *Endpoint) *mattercore.Binding {
		for _, srv := range ClusterServers(ep) {
			if b, ok := srv.(*mattercore.Binding); ok {
				return b
			}
		}
		return nil
	}
	bound := byKey["bound"]
	b := bindingOf(bound)
	if b == nil {
		t.Fatal("the endpoint declaring an OnOff client has no Binding server")
	}
	if again := bindingOf(bound); again != b {
		t.Fatal("the Binding server is rebuilt per dispatch")
	}
	for _, srv := range ClusterServers(bound) {
		if srv.MatterClusterID() == 0x001D {
			v, _ := srv.MatterRead(0x0002)
			if l, _ := v.([]uint32); !slices.Equal(l, []uint32{onOff}) {
				t.Fatalf("ClientList = %v", v)
			}
			v, _ = srv.MatterRead(0x0001)
			if l, _ := v.([]uint32); !slices.Contains(l, bindingClusterID) {
				t.Fatalf("ServerList %v lacks Binding", v)
			}
		}
	}
	for _, key := range []string{"unbindable", "none"} {
		if bindingOf(byKey[key]) != nil {
			t.Errorf("%s: a Binding server was mounted", key)
		}
	}

	// Validation: the bound endpoint carries what Base mandates; the others
	// owe nothing, since Client does not hold for them.
	for _, v := range ValidateDeviceTypes(topo) {
		if v.Requirement == "Binding" {
			t.Errorf("Binding violation: %v", v)
		}
	}
}

// TestBaseMandatesBinding pins the evaluation of Base's "Simple & Client"
// Binding requirement: a simple device type with a bindable application
// client, and nothing else.
func TestBaseMandatesBinding(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		dt      uint16
		clients []uint32
		want    bool
	}{
		{"simple with OnOff client", 0x0103, []uint32{0x0006}, true},
		{"simple light with an OccupancySensing client", 0x0100, []uint32{0x0406}, true},
		{"simple with an unbindable client", 0x0103, []uint32{0x0553}, false},
		{"simple with a utility client only", 0x0103, []uint32{0x0003}, false},
		{"simple without clients", 0x0103, nil, false},
		{"node device type with OnOff client", 0x0016, []uint32{0x0006}, false},
		{"unknown device type", 0xFFF0, []uint32{0x0006}, false},
	}
	for _, c := range cases {
		if got := baseMandatesBinding(c.dt, c.clients); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}
