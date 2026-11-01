// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/schema"
)

// plainSubject is a DeviceTypeSubject over plain data — what a client view
// hands the validator.
type plainSubject struct {
	number  uint16
	types   []uint32
	servers []uint32
	clients []uint32
	shapes  map[uint32]ClusterShape
	stated  []string
	parts   []uint16
}

func (s *plainSubject) EndpointNumber() uint16     { return s.number }
func (s *plainSubject) DeviceTypeIDs() []uint32    { return s.types }
func (s *plainSubject) ServerClusterIDs() []uint32 { return s.servers }
func (s *plainSubject) ClientClusterIDs() []uint32 { return s.clients }
func (s *plainSubject) StatedConditions() []string { return s.stated }
func (s *plainSubject) PartNumbers() []uint16      { return s.parts }

func (s *plainSubject) ServerClusterShape(cluster uint32) (ClusterShape, bool) {
	sh, ok := s.shapes[cluster]
	return sh, ok
}

// copySubjects copies a tree's subjects into plain ones, as a client view
// holds a remote node's image.
func copySubjects(tree DeviceTypeTree) []*plainSubject {
	out := make([]*plainSubject, 0, len(tree.Endpoints))
	for _, s := range tree.Endpoints {
		p := &plainSubject{
			number: s.EndpointNumber(), types: s.DeviceTypeIDs(), servers: s.ServerClusterIDs(),
			clients: s.ClientClusterIDs(), stated: s.StatedConditions(), parts: s.PartNumbers(),
			shapes: map[uint32]ClusterShape{},
		}
		for _, c := range p.servers {
			if sh, ok := s.ServerClusterShape(c); ok {
				p.shapes[c] = sh
			}
		}
		out = append(out, p)
	}
	return out
}

func treeOf(subjects []*plainSubject, node func(uint16) []string) DeviceTypeTree {
	tree := DeviceTypeTree{NodeConditions: node}
	for _, s := range subjects {
		tree.Endpoints = append(tree.Endpoints, s)
	}
	return tree
}

func keysOf(vs []DeviceTypeViolation, ep uint16) []string {
	var out []string
	for i := range vs {
		if vs[i].Endpoint == ep {
			out = append(out, vs[i].Key())
		}
	}
	return out
}

// TestDeviceTypeSubjectSameVerdicts: the assembled fixture judged through
// plain subjects that carry only what the interface asks gives the
// verdicts ValidateDeviceTypes gives the topology — the interface carries
// everything the validator reads.
func TestDeviceTypeSubjectSameVerdicts(t *testing.T) {
	t.Parallel()
	a, err := New(&oneEndpointStore{}, Config{VendorID: 1, ProductID: 1, RootDeviceConditions: []string{conditionLanguage}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	onOff := dtServer{id: clOnOff, features: uint32(0)}
	topo, err := a.Assemble(context.Background(), []Snapshot{{Scope: "s", Endpoints: []Spec{{
		StableKey: StringKey("lamp"), DeviceType: dtOnOffLight, FriendlyName: "Lamp",
		Source:           deviceTypeSource{dt: dtOnOffLight, servers: []contract.ClusterServer{onOff}},
		DeviceConditions: []string{"Nope"},
	}}}})
	if err != nil {
		t.Fatal(err)
	}
	want := ValidateDeviceTypes(topo)
	if len(want) == 0 {
		t.Fatal("the fixture conforms; it should not")
	}
	plain := copySubjects(topologyTree(topo))
	got := treeOf(plain, assembledNodeConditions).Validate()
	if len(got) != len(want) {
		t.Fatalf("plain %d violations, assembled %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Key() != want[i].Key() || got[i].Endpoint != want[i].Endpoint || got[i].Detail != want[i].Detail {
			t.Errorf("violation %d: plain %v, assembled %v", i, got[i], want[i])
		}
	}
	// Check judges one endpoint in the whole tree's context.
	if lamp := treeOf(plain, assembledNodeConditions).Check(2); !slices.Equal(keysOf(lamp, 2), keysOf(want, 2)) || len(lamp) != len(keysOf(want, 2)) {
		t.Errorf("Check(2) = %v, want %v", keysOf(lamp, 2), keysOf(want, 2))
	}
	if treeOf(plain, nil).Check(99) != nil {
		t.Error("Check of an endpoint the tree lacks")
	}
}

// TestDeviceTypeSubjectUnknownShape: a cluster whose shape is not known
// is judged present, nothing inside it is; an unknown list leaves the
// requirements of its kind unjudged; an unknown FeatureMap every nested
// one.
func TestDeviceTypeSubjectUnknownShape(t *testing.T) {
	t.Parallel()
	light := func(shapes map[uint32]ClusterShape) []string {
		root := &plainSubject{number: 0, types: []uint32{dtRootNode}, servers: []uint32{clDescriptor}, parts: []uint16{1}, shapes: map[uint32]ClusterShape{}}
		lamp := &plainSubject{
			number: 1, types: []uint32{dtOnOffLight},
			servers: []uint32{clDescriptor, clIdentify, clGroups, clOnOff, clScenes}, shapes: shapes,
		}
		return keysOf(treeOf([]*plainSubject{root, lamp}, nil).Validate(), 1)
	}
	known := ClusterShape{FeaturesKnown: true, AttributesKnown: true, CommandsKnown: true, EventsKnown: true}
	// OnOff without LT and without its attributes, everything known.
	all := light(map[uint32]ClusterShape{clOnOff: known})
	if !slices.Contains(all, "missing OnOff.LT") {
		t.Fatalf("known shape: %v", all)
	}
	// The same cluster with no shape: present, nothing inside judged.
	for _, k := range light(map[uint32]ClusterShape{}) {
		if strings.HasPrefix(k, "missing OnOff.") {
			t.Errorf("an unknown shape was judged: %s", k)
		}
	}
	// FeatureMap unknown: nothing inside judged either.
	for _, k := range light(map[uint32]ClusterShape{clOnOff: {AttributesKnown: true, CommandsKnown: true, EventsKnown: true}}) {
		if strings.HasPrefix(k, "missing OnOff.") {
			t.Errorf("an unknown FeatureMap was judged: %s", k)
		}
	}
	// Identify's TriggerEffect mandate: judged with the commands known,
	// not without.
	withCmds := light(map[uint32]ClusterShape{clIdentify: known})
	noCmds := light(map[uint32]ClusterShape{clIdentify: {FeaturesKnown: true, AttributesKnown: true, EventsKnown: true}})
	if !slices.Contains(withCmds, "missing Identify.TriggerEffect") || slices.Contains(noCmds, "missing Identify.TriggerEffect") {
		t.Errorf("commands known %v, unknown %v", withCmds, noCmds)
	}
}

// TestSubjectFactsTree: parts the tree lacks, self-parts and a second
// claim own nothing; a duplicate number keeps the first subject.
func TestSubjectFactsTree(t *testing.T) {
	t.Parallel()
	a := &plainSubject{number: 0, parts: []uint16{1, 2, 0, 9, 1}}
	b := &plainSubject{number: 1, parts: []uint16{2}}
	c := &plainSubject{number: 2, parts: []uint16{1}}
	dup := &plainSubject{number: 1, types: []uint32{7}}
	f := newSubjectFacts(DeviceTypeTree{Endpoints: []DeviceTypeSubject{a, b, nil, c, dup}})
	if got := f.partsOf(0); !slices.Equal(got, []uint16{1, 2}) {
		t.Errorf("parts of 0 = %v", got)
	}
	if got := f.partsOf(1); got != nil {
		t.Errorf("parts of 1 = %v (2 is claimed by 0)", got)
	}
	if p, ok := f.parentOf(2); !ok || p != 0 {
		t.Errorf("parent of 2 = %d %v", p, ok)
	}
	if _, ok := f.parentOf(0); ok {
		t.Error("the root has a parent")
	}
	if f.deviceTypeIDs(1) != nil || f.nodeConditions(0) != nil || f.statedConditions(5) != nil ||
		f.serverClusters(5) != nil || f.clientClusters(5) != nil || f.deviceTypeIDs(5) != nil {
		t.Error("a duplicate or a missing subject answers")
	}
	if f.supports(0, clOnOff, schema.RequirementAttribute, 0) || f.judgeable(5, clOnOff, schema.RequirementAttribute) || f.features(0, clOnOff) != nil {
		t.Error("a cluster the endpoint lacks answers")
	}
}
