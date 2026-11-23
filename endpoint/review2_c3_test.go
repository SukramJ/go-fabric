// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"context"
	"errors"
	"strings"
	"testing"

	mattercore "thingfabric.dev/go-fabric/cluster/core"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/groups"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/schema"
	"thingfabric.dev/go-fabric/store"
)

func c3Assembler(t *testing.T) *Assembler {
	t.Helper()
	a, err := New(newInternalFakeStore(), Config{VendorID: 0xFFF1, ProductID: 0x8001}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func flowSpec(key SourceKey, name string) Spec {
	return Spec{StableKey: key, DeviceType: 0x0306, FriendlyName: name, Measurement: measurementSource{class: contract.MeasurementFlow}}
}

// TestFailedAssemblyLeavesTheLiveStateAlone (R2-C2-fsm-0451): the state a
// spec restores — the host name cleared, the restored NodeLabel, the
// ConfigurationVersion — is shared with the live topology's endpoint, so it
// is written only once the whole assembly succeeded. A failed Assemble
// leaves what the live endpoint serves unchanged.
func TestFailedAssemblyLeavesTheLiveStateAlone(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	a := c3Assembler(t)
	good := flowSpec(StringKey("a"), "Friendly")
	topo, err := a.Assemble(ctx, []Snapshot{{Scope: "s", Endpoints: []Spec{good}}})
	if err != nil {
		t.Fatal(err)
	}
	live := topo.Endpoints[2]
	live.SetHostName("LiveName")
	beforeLabel, beforeVersion := live.NodeLabel(), live.ConfigurationVersion()

	restored := good
	restored.NodeLabel = "Restored"
	restored.ConfigurationVersion = 7
	bad := flowSpec(StringKey("b"), "Bad")
	bad.VendorName = strings.Repeat("x", 40) // beyond BridgedDeviceBasicInformation VendorName "max 32"
	if _, err := a.Assemble(ctx, []Snapshot{{Scope: "s", Endpoints: []Spec{restored, bad}}}); err == nil {
		t.Fatal("the assembly with an over-long VendorName succeeded; the test needs it to fail")
	}
	if got := live.NodeLabel(); got != beforeLabel {
		t.Errorf("after a failed assembly the live NodeLabel = %q, want %q unchanged", got, beforeLabel)
	}
	if got := live.ConfigurationVersion(); got != beforeVersion {
		t.Errorf("after a failed assembly the live ConfigurationVersion = %d, want %d unchanged", got, beforeVersion)
	}

	// The same restore in an assembly that succeeds is applied.
	topo, err = a.Assemble(ctx, []Snapshot{{Scope: "s", Endpoints: []Spec{restored}}})
	if err != nil {
		t.Fatal(err)
	}
	if got := topo.Endpoints[2].NodeLabel(); got != "Restored" {
		t.Errorf("after a successful assembly NodeLabel = %q, want the restored %q", got, "Restored")
	}
	if got := topo.Endpoints[2].ConfigurationVersion(); got != 7 {
		t.Errorf("after a successful assembly ConfigurationVersion = %d, want 7", got)
	}
}

// TestAssembleRefusesASharedOrMissingKey (R2-C2-err-0452) holds the
// contract of [Spec.StableKey] ("Two specs must never share one key"): two
// specs with one key, across scopes too, and a spec whose key is nil or
// renders empty refuse the assembly with an error naming the key — no
// topology with two endpoints on one id and one state.
func TestAssembleRefusesASharedOrMissingKey(t *testing.T) {
	t.Parallel()
	cases := map[string][]Snapshot{
		"duplicate in one scope": {{Scope: "s", Endpoints: []Spec{flowSpec(StringKey("dup"), "one"), flowSpec(StringKey("dup"), "two")}}},
		"duplicate across scopes": {
			{Scope: "s", Endpoints: []Spec{flowSpec(StringKey("dup"), "one")}},
			{Scope: "t", Endpoints: []Spec{flowSpec(StringKey("dup"), "two")}},
		},
		"duplicate of a part": {{Scope: "s", Endpoints: []Spec{func() Spec {
			s := flowSpec(StringKey("dup"), "one")
			s.Parts = []Spec{flowSpec(StringKey("dup"), "part")}
			return s
		}()}}},
		"nil keys":      {{Scope: "s", Endpoints: []Spec{flowSpec(nil, "one"), flowSpec(nil, "two")}}},
		"one nil key":   {{Scope: "s", Endpoints: []Spec{flowSpec(nil, "one")}}},
		"an empty key":  {{Scope: "s", Endpoints: []Spec{flowSpec(StringKey(""), "one")}}},
		"distinct keys": nil, // the control below
	}
	for name, snaps := range cases {
		if snaps == nil {
			continue
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var topo *Topology
			var err error
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("Assemble panicked: %v", r)
					}
				}()
				topo, err = c3Assembler(t).Assemble(context.Background(), snaps)
			}()
			if err == nil {
				ids := make([]uint16, 0, len(topo.Endpoints))
				for _, ep := range topo.Endpoints {
					ids = append(ids, ep.ID)
				}
				t.Fatalf("Assemble accepted it: endpoints %v", ids)
			}
			if topo != nil {
				t.Errorf("Assemble returned a topology beside the error %v", err)
			}
		})
	}
	topo, err := c3Assembler(t).Assemble(context.Background(), []Snapshot{{Scope: "s", Endpoints: []Spec{flowSpec(StringKey("a"), "one"), flowSpec(StringKey("b"), "two")}}})
	if err != nil || len(topo.Endpoints) != 4 {
		t.Fatalf("two distinct keys: err=%v", err)
	}
}

// TestUniqueIDOfAnUnrenderableKeyNamesNoAddress (R2-C2-err-0452): the
// fallback for a key that renders empty hashed a "%p" of it, which for a nil
// interface — or any non-pointer — is one constant, not the uniqueness its
// comment promised. The assembler refuses such a key now; a hand-built
// endpoint that carries one hashes the empty rendering and no fmt error
// verb.
func TestUniqueIDOfAnUnrenderableKeyNamesNoAddress(t *testing.T) {
	t.Parallel()
	if uniqueIDFor(nil) != uniqueIDFor(SourceKey(nil)) || uniqueIDFor(nil) != uniqueIDFor(StringKey("")) {
		t.Error("an unrenderable key's UniqueID depends on how the nothing is typed")
	}
	if uniqueIDFor(nil) == uniqueIDFor(StringKey("go-fabric-bridged-%!p(<nil>)")) {
		t.Error("an unrenderable key hashes the fmt error string of a pointer verb")
	}
}

// valueSubject is a DeviceTypeSubject whose dynamic type is not comparable
// (it carries slices) — the natural shape of "one endpoint's lists".
type valueSubject struct {
	n       uint16
	types   []uint32
	servers []uint32
	parts   []uint16
}

func (s valueSubject) EndpointNumber() uint16                         { return s.n }
func (s valueSubject) DeviceTypeIDs() []uint32                        { return s.types }
func (s valueSubject) ServerClusterIDs() []uint32                     { return s.servers }
func (s valueSubject) ClientClusterIDs() []uint32                     { return nil }
func (s valueSubject) ServerClusterShape(uint32) (ClusterShape, bool) { return ClusterShape{}, false }
func (s valueSubject) StatedConditions() []string                     { return nil }
func (s valueSubject) PartNumbers() []uint16                          { return s.parts }

// TestValidateTakesUncomparableSubjects (R2-C2-api-0651): the tree tells the
// first subject of an endpoint number from a later duplicate by its
// position, never by comparing interface values, so a subject of an
// uncomparable dynamic type is judged instead of panicking — and the first
// of a duplicate number is the one judged.
func TestValidateTakesUncomparableSubjects(t *testing.T) {
	t.Parallel()
	tree := DeviceTypeTree{Endpoints: []DeviceTypeSubject{
		valueSubject{n: 0, types: []uint32{0x0016}, parts: []uint16{1}},
		valueSubject{n: 1, types: []uint32{0x0100}, servers: []uint32{0x0003, 0x0006, 0x001D}},
		valueSubject{n: 1, types: []uint32{0x0100}, servers: nil, parts: []uint16{0}},
	}}
	var first, dup []DeviceTypeViolation
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("Validate over value-typed subjects panicked: %v", r)
			}
		}()
		_ = tree.Validate()
		_ = tree.Check(1)
		first = DeviceTypeTree{Endpoints: tree.Endpoints[:2]}.Check(1)
		dup = tree.Check(1)
	}()
	if len(first) != len(dup) {
		t.Errorf("Check(1) with a later duplicate = %d violations, without it %d: the duplicate was judged", len(dup), len(first))
	}
}

var c3NilServerClass = contract.RegisterMeasurementKind(contract.MeasurementKind{
	Name: "c3-nil-server",
	Materialize: func(any, contract.MeasurementContext) []contract.ClusterServer {
		return []contract.ClusterServer{nil}
	},
})

// TestNilMeasurementServerIsDropped (R2-C2-err-0450, the half of
// R2-P6-err-0403 its fix did not reach): a nil entry in what a measurement
// kind's materialiser returns is dropped like one in a source's set, so the
// endpoint's server list holds no nil and a read of it answers.
func TestNilMeasurementServerIsDropped(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := Spec{
		StableKey: StringKey("n"), DeviceType: 0x0306, FriendlyName: "n", Measurement: measurementSource{class: c3NilServerClass},
		PowerSource: measurementSource{class: contract.MeasurementBattery},
	}
	topo, err := c3Assembler(t).Assemble(ctx, []Snapshot{{Scope: "s", Endpoints: []Spec{s}}})
	if err != nil {
		t.Fatal(err)
	}
	ep := topo.Endpoints[2]
	for i, srv := range ClusterServers(ep) {
		if srv == nil {
			t.Errorf("ClusterServers handed out a nil entry at %d", i)
		}
	}
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("a read of the endpoint panicked: %v", r)
			}
		}()
		res := NewTopologyDispatcher(topo).Read(ctx, im.ConcreteAttributePath{Endpoint: ep.ID, Cluster: 0x001D, Attribute: 0x0001, HasEndpoint: true, HasCluster: true, HasAttribute: true})
		if len(res) != 1 || res[0].Status != im.StatusSuccess {
			t.Errorf("ServerList read = %+v, want one Success", res)
		}
	}()
}

// TestAssembleRefusesAnEndpointThatServesNothing (R2-C2-err-0453): a spec
// with no Source whose measurement class nothing materialises would be an
// endpoint of the topology — listed in the PartsList — with no cluster, not
// even a Descriptor. The assembly refuses it, naming the key.
func TestAssembleRefusesAnEndpointThatServesNothing(t *testing.T) {
	t.Parallel()
	unknown := Spec{StableKey: StringKey("u"), DeviceType: 0x0306, FriendlyName: "u", Measurement: measurementSource{class: contract.MeasurementClass(60000)}}
	_, err := c3Assembler(t).Assemble(context.Background(), []Snapshot{{Scope: "s", Endpoints: []Spec{unknown}}})
	if err == nil || !strings.Contains(err.Error(), "u") {
		t.Fatalf("Assemble of a spec that serves no cluster: err=%v, want an error naming the key", err)
	}
	part := flowSpec(StringKey("p"), "p")
	part.Parts = []Spec{{StableKey: StringKey("pp"), DeviceType: 0x0306, Measurement: measurementSource{class: contract.MeasurementClass(60000)}}}
	if _, err := c3Assembler(t).Assemble(context.Background(), []Snapshot{{Scope: "s", Endpoints: []Spec{part}}}); err == nil {
		t.Fatal("Assemble of a part that serves no cluster succeeded")
	}
	// A spec whose measurement class materialises is accepted.
	if _, err := c3Assembler(t).Assemble(context.Background(), []Snapshot{{Scope: "s", Endpoints: []Spec{flowSpec(StringKey("f"), "f")}}}); err != nil {
		t.Fatalf("a flow sensor: %v", err)
	}
}

// failingFabricsStore is a group store whose fabric list cannot be read.
type failingFabricsStore struct{ nopGroupStore }

func (failingFabricsStore) ListFabrics(context.Context) ([]store.FabricRecord, error) {
	return nil, errors.New("store read fault")
}

// TestFabricSceneInfoOnAFabricListFault (R2-C2-err-0455): a store error
// while listing the node's fabrics is not answered as a node without
// fabrics (Success with an empty FabricSceneInfo): the accessing fabric
// still reads its own entry, as it does without a fabric source.
func TestFabricSceneInfoOnAFabricListFault(t *testing.T) {
	t.Parallel()
	mgr, err := groups.NewManager(failingFabricsStore{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ep := &Endpoint{
		ID: 7, DeviceType: 0x0100, FriendlyName: "Lamp", SourceKey: StringKey("lamp"),
		Source: deviceTypeSource{dt: 0x0100, servers: []contract.ClusterServer{fakeServer{id: 0x0006}}},
		groups: mgr,
	}
	var scenes *mattercore.ScenesManagement
	for _, s := range ClusterServers(ep) {
		if sm, ok := s.(*mattercore.ScenesManagement); ok {
			scenes = sm
		}
	}
	if scenes == nil {
		t.Fatal("no stack ScenesManagement server")
	}
	ctx := im.WithFabricFilter(context.Background(), true, 1)
	v, ok := scenes.MatterReadFiltered(ctx, 0x0002)
	si, _ := v.([]mattercore.SceneInfoStruct)
	if ok && len(si) == 0 {
		t.Fatalf("FabricSceneInfo on a fabric-list fault = Success, empty: the accessing fabric's entry is gone")
	}
	if ok && (len(si) != 1 || si[0].FabricIndex != 1) {
		t.Errorf("FabricSceneInfo on a fabric-list fault = %+v, want fabric 1's entry alone", si)
	}
}

// TestIdentifyOnEveryBridgedEndpointNotOnEveryPart (R2-C2-par-0454) pins
// the recorded departure: a bridged endpoint that is no part serves
// Identify whatever its device type — ElectricalSensor (0x0510) mandates
// none — for Apple Home's HAP-service rebuild; a part serves it only where
// its device type mandates it.
func TestIdentifyOnEveryBridgedEndpointNotOnEveryPart(t *testing.T) {
	t.Parallel()
	serves := func(ep *Endpoint) bool {
		for _, srv := range ClusterServers(ep) {
			if srv.MatterClusterID() == identifyClusterID {
				return true
			}
		}
		return false
	}
	s := Spec{StableKey: StringKey("es"), DeviceType: 0x0510, FriendlyName: "es", Measurement: measurementSource{class: contract.MeasurementFlow}}
	s.Parts = []Spec{{StableKey: StringKey("es-part"), DeviceType: 0x0510, Measurement: measurementSource{class: contract.MeasurementFlow}}}
	topo, err := c3Assembler(t).Assemble(context.Background(), []Snapshot{{Scope: "s", Endpoints: []Spec{s}}})
	if err != nil {
		t.Fatal(err)
	}
	if schema.DeviceTypeRequiresServerCluster(schema.DeviceTypeID(0x0510), identifyClusterID) {
		t.Fatal("the snapshot's ElectricalSensor mandates Identify now; the departure this test pins is gone — re-read by_design")
	}
	if !serves(topo.Endpoints[2]) {
		t.Error("the bridged ElectricalSensor endpoint serves no Identify")
	}
	if serves(topo.Endpoints[3]) {
		t.Error("the ElectricalSensor part serves Identify, which its device type does not mandate")
	}
}
