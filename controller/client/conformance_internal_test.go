// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

import (
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/cluster/spec/descriptor"
	"github.com/SukramJ/go-fabric/cluster/spec/onoff"
	fabricdevice "github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
)

// Clusters of the conformance fixture.
const (
	clIdentify  = 0x0003
	clGroups    = 0x0004
	clScenes    = 0x0062
	clBinding   = 0x001E
	clNetComm   = 0x0031
	clOccupancy = 0x0406
)

// plugFixture is a node with one OnOffPlugInUnit on endpoint 1 under the
// root. mutate edits the plug's server list and clusters before the
// report is built: servers maps a cluster to its FeatureMap and accepted
// commands.
type plugFixture struct {
	servers  []uint32
	features map[uint32]uint64
	accepted map[uint32][]uint64
	dt       uint64
	clients  []uint64
	// skip are listed servers that deliver no data.
	skip []uint32
}

func cleanPlug() *plugFixture {
	return &plugFixture{
		servers:  []uint32{descriptor.ClusterID, clIdentify, clGroups, onoff.ClusterID, clScenes},
		features: map[uint32]uint64{onoff.ClusterID: 1},
		accepted: map[uint32][]uint64{clIdentify: {0x00, 0x40}, clGroups: {0x00, 0x01, 0x02, 0x03, 0x04, 0x05}, onoff.ClusterID: {0x00, 0x01, 0x02}, clScenes: {0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x40}},
		dt:       dtPlug,
		clients:  []uint64{},
	}
}

func (f *plugFixture) report() *report {
	b := &report{}
	b.endpoint(0, []uint64{dtRootNode}, []uint32{descriptor.ClusterID, clBasicInfo}, 1)
	sl := make([]uint64, 0, len(f.servers))
	for _, c := range f.servers {
		sl = append(sl, uint64(c))
	}
	b.cluster(1, descriptor.ClusterID, 0, map[uint32]spec.Node{
		descriptor.AttrDeviceTypeList: deviceTypes(f.dt),
		descriptor.AttrServerList:     listNode(sl...),
		descriptor.AttrClientList:     listNode(f.clients...),
		descriptor.AttrPartsList:      listNode(),
	})
	for _, c := range f.servers {
		if c == descriptor.ClusterID || slices.Contains(f.skip, c) {
			continue
		}
		b.cluster(1, c, f.features[c], map[uint32]spec.Node{0: uintNode(0)}, f.accepted[c]...)
	}
	return b
}

func (f *plugFixture) node(t *testing.T) *Node {
	t.Helper()
	n, _, _ := testNode(t, &MemoryStore{})
	n.apply(wildcard, &f.report().r)
	if n.Endpoint(1) == nil {
		t.Fatalf("no plug endpoint: %s", tree(n))
	}
	return n
}

func keys(vs []endpoint.DeviceTypeViolation) []string {
	out := make([]string, 0, len(vs))
	for i := range vs {
		out = append(out, vs[i].Key())
	}
	return out
}

// TestEndpointDevice: a plug endpoint resolves to the generated
// OnOffPlugInUnit type with the snapshot's revision beside the peer's; an
// unknown device type to none; CastingVideoClient, the last type devicegen
// excluded, resolves since every snapshot device type is generated.
func TestEndpointDevice(t *testing.T) {
	t.Parallel()
	n := cleanPlug().node(t)
	plug := n.Endpoint(1)
	if d := plug.Device(); d != &fabricdevice.OnOffPlugInUnitType || d.Name != "OnOffPlugInUnit" {
		t.Fatalf("Device() = %v", d)
	}
	if rev, ok := plug.DeviceRevision(); !ok || rev != 1 || plug.Device().Revision != fabricdevice.OnOffPlugInUnitRevision {
		t.Fatalf("peer revision %d %v, snapshot %d", rev, ok, plug.Device().Revision)
	}
	for _, dt := range []uint64{0xFFF1_0001, 0xFFFF_FFFF} { // manufacturer-specific; the client view's unknown device type
		f := cleanPlug()
		f.dt = dt
		if d := f.node(t).Endpoint(1).Device(); d != nil {
			t.Errorf("device type %#x: Device() = %s", dt, d.Name)
		}
	}
	cvc := cleanPlug()
	cvc.dt = 0x0029
	if d := cvc.node(t).Endpoint(1).Device(); d != &fabricdevice.CastingVideoClientType {
		t.Errorf("device type 0x29: Device() = %v", d)
	}
	if d := newEndpoint(&n.mu, 7).Device(); d != nil {
		t.Errorf("an endpoint of no known type: %s", d.Name)
	}
}

// TestEndpointConformance: the plug conforms when its clusters carry what
// the type mandates; dropping Identify from the ServerList is a missing
// cluster; an OnOff FeatureMap without LT a missing feature; the
// ClientList is compared but informational.
func TestEndpointConformance(t *testing.T) {
	t.Parallel()
	clean := cleanPlug().node(t).Endpoint(1).Conformance()
	if clean.Status != ConformanceClean || len(clean.Violations) != 0 || len(clean.Unjudged) != 0 {
		t.Fatalf("clean plug: %v %v unjudged %v", clean.Status, keys(clean.Violations), clean.Unjudged)
	}

	noIdentify := cleanPlug()
	noIdentify.servers = slices.DeleteFunc(noIdentify.servers, func(c uint32) bool { return c == clIdentify })
	r := noIdentify.node(t).Endpoint(1).Conformance()
	if r.Status != ConformanceViolated || !slices.Contains(keys(r.Violations), "missing Identify") {
		t.Fatalf("Identify dropped: %v %v", r.Status, keys(r.Violations))
	}
	i := slices.IndexFunc(r.Violations, func(v endpoint.DeviceTypeViolation) bool { return v.Requirement == "Identify" })
	if v := r.Violations[i]; v.Endpoint != 1 || v.DeviceType != "OnOffPlugInUnit" || !v.HasCluster || v.Cluster != clIdentify {
		t.Errorf("violation %+v", v)
	}

	noLT := cleanPlug()
	noLT.features[onoff.ClusterID] = 0
	r = noLT.node(t).Endpoint(1).Conformance()
	if r.Status != ConformanceViolated || !slices.Contains(keys(r.Violations), "missing OnOff.LT") {
		t.Fatalf("OnOff without LT: %v %v", r.Status, keys(r.Violations))
	}

	noTrigger := cleanPlug()
	noTrigger.accepted[clIdentify] = []uint64{0x00}
	r = noTrigger.node(t).Endpoint(1).Conformance()
	if !slices.Contains(keys(r.Violations), "missing Identify.TriggerEffect") {
		t.Fatalf("Identify without TriggerEffect: %v", keys(r.Violations))
	}

	// A bindable application client (OccupancySensing, optional for the
	// plug) makes Base mandate Binding: judged like any server.
	client := cleanPlug()
	client.clients = []uint64{clOccupancy}
	r = client.node(t).Endpoint(1).Conformance()
	if r.Status != ConformanceViolated || !slices.Equal(keys(r.Violations), []string{"missing Binding"}) {
		t.Fatalf("a client without Binding: %v %v", r.Status, keys(r.Violations))
	}
	client.servers = append(client.servers, clBinding)
	if r = client.node(t).Endpoint(1).Conformance(); r.Status != ConformanceClean || len(r.Clients) != 0 {
		t.Fatalf("with Binding: %v %v clients %v", r.Status, keys(r.Violations), keys(r.Clients))
	}

	// The client clusters a type mandates are compared with the ClientList
	// and reported apart: an OnOffLightSwitch listing none stays clean.
	sw := &plugFixture{
		servers:  []uint32{descriptor.ClusterID, clIdentify, clBinding},
		features: map[uint32]uint64{},
		accepted: map[uint32][]uint64{clIdentify: {0x00, 0x40}},
		dt:       0x0103,
		clients:  []uint64{},
	}
	r = sw.node(t).Endpoint(1).Conformance()
	if r.Status != ConformanceClean || !slices.Equal(keys(r.Clients), []string{"missing client:Identify", "missing client:OnOff"}) {
		t.Fatalf("switch without clients: %v %v clients %v", r.Status, keys(r.Violations), keys(r.Clients))
	}
	sw.clients = []uint64{clIdentify, uint64(onoff.ClusterID)}
	if r = sw.node(t).Endpoint(1).Conformance(); r.Status != ConformanceClean || len(r.Clients) != 0 {
		t.Fatalf("switch with clients: %v %v clients %v", r.Status, keys(r.Violations), keys(r.Clients))
	}
}

// TestEndpointConformanceUnknown: an unknown device type, an endpoint
// whose Descriptor has not delivered, and one off the image give an
// unknown report; a listed cluster not yet built is not judged.
func TestEndpointConformanceUnknown(t *testing.T) {
	t.Parallel()
	unknown := cleanPlug()
	unknown.dt = 0xFFF1_0001
	un := unknown.node(t)
	ep := un.Endpoint(1)
	if ep.Device() != nil {
		t.Fatal("an unknown type has a Device")
	}
	if r := ep.Conformance(); r.Status != ConformanceUnknown || r.Violations != nil {
		t.Fatalf("unknown device type: %+v", r)
	}
	if r := newEndpoint(&un.mu, 9).Conformance(); r.Status != ConformanceUnknown {
		t.Fatalf("an endpoint off the image: %+v", r)
	}

	// The plug's OnOff is listed but has delivered nothing: present, not
	// judged inside, so the report is incomplete rather than failing.
	reading := cleanPlug()
	reading.features[onoff.ClusterID] = 0
	reading.skip = []uint32{onoff.ClusterID}
	r := reading.node(t).Endpoint(1).Conformance()
	if r.Status != ConformanceIncomplete || len(r.Violations) != 0 || !slices.Equal(r.Unjudged, []uint32{onoff.ClusterID}) {
		t.Fatalf("OnOff not built: %v %v unjudged %v", r.Status, keys(r.Violations), r.Unjudged)
	}
	// Built but without its FeatureMap: not judged either.
	n, _, _ := testNode(t, &MemoryStore{})
	b := cleanPlug().report()
	b.r.Attributes = slices.DeleteFunc(b.r.Attributes, func(a interaction.AttributeData) bool {
		return a.Path.Endpoint == 1 && a.Path.Cluster == onoff.ClusterID && a.Path.Attribute == spec.AttrFeatureMap
	})
	n.apply(wildcard, &b.r)
	if r := n.Endpoint(1).Conformance(); r.Status != ConformanceIncomplete || !slices.Equal(r.Unjudged, []uint32{onoff.ClusterID}) {
		t.Fatalf("OnOff without FeatureMap: %v %v unjudged %v", r.Status, keys(r.Violations), r.Unjudged)
	}

	// A Descriptor without its ServerList: unknown.
	n, _, _ = testNode(t, &MemoryStore{})
	b = cleanPlug().report()
	b.r.Attributes = slices.DeleteFunc(b.r.Attributes, func(a interaction.AttributeData) bool {
		return a.Path.Endpoint == 1 && a.Path.Cluster == descriptor.ClusterID && a.Path.Attribute == descriptor.AttrServerList
	})
	n.apply(wildcard, &b.r)
	if ep := n.Endpoint(1); ep != nil {
		if r := ep.Conformance(); r.Status != ConformanceUnknown {
			t.Fatalf("no ServerList: %+v", r)
		}
	}
}

// TestNodeConditionsOfTheImage: a root without NetworkCommissioning makes
// CustomNetworkConfig hold (no NetworkCommissioning required of it); one
// with it leaves the condition false.
func TestNodeConditionsOfTheImage(t *testing.T) {
	t.Parallel()
	for _, withNC := range []bool{false, true} {
		b := &report{}
		servers := []uint32{descriptor.ClusterID, clBasicInfo}
		if withNC {
			servers = append(servers, clNetComm)
		}
		b.endpoint(0, []uint64{dtRootNode}, servers)
		n, _, _ := testNode(t, &MemoryStore{})
		n.apply(wildcard, &b.r)
		r := n.Endpoint(0).Conformance()
		if slices.Contains(keys(r.Violations), "missing NetworkCommissioning") {
			t.Errorf("NetworkCommissioning %v: %v", withNC, keys(r.Violations))
		}
		if r.Status == ConformanceUnknown {
			t.Errorf("the root unjudged: %+v", r)
		}
		tr, ok := n.Endpoint(0).conformanceTree()
		if got := tr.tree.NodeConditions(0); !ok || slices.Contains(got, "CustomNetworkConfig") == withNC {
			t.Errorf("NetworkCommissioning %v: node conditions %v", withNC, got)
		}
	}
}

// TestConformanceStatusString names every status.
func TestConformanceStatusString(t *testing.T) {
	t.Parallel()
	for s, want := range map[ConformanceStatus]string{
		ConformanceUnknown: "unknown", ConformanceIncomplete: "incomplete", ConformanceClean: "clean", ConformanceViolated: "violated",
	} {
		if s.String() != want {
			t.Errorf("%d: %s", s, s.String())
		}
	}
}
