// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"slices"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/schema"
)

// Test support for device type validation: fake facts over a tree of
// synthetic endpoints and a builder for synthetic models, after matter.js
// packages/model/test/logic/device-types/fake-facts.ts.

// fakeEP is one endpoint of a fake tree.
type fakeEP struct {
	id          uint16
	name        string
	parent      *fakeEP
	parts       []*fakeEP
	deviceTypes []uint32
	// servers maps a server cluster to the feature codes it supports.
	servers  map[uint32][]string
	order    []uint32
	clients  []uint32
	elements map[uint32]map[schema.RequirementElement][]uint32
	stated   []string
}

// fakeTree answers dtFacts for fake endpoints.
type fakeTree struct {
	eps  map[uint16]*fakeEP
	next uint16
	node []string
}

func newFakeTree() *fakeTree { return &fakeTree{eps: map[uint16]*fakeEP{}} }

// add creates an endpoint under parent (nil for a tree root).
func (f *fakeTree) add(name string, parent *fakeEP, deviceTypes ...uint32) *fakeEP {
	ep := &fakeEP{
		id: f.next, name: name, parent: parent, deviceTypes: deviceTypes,
		servers: map[uint32][]string{}, elements: map[uint32]map[schema.RequirementElement][]uint32{},
	}
	f.next++
	f.eps[ep.id] = ep
	if parent != nil {
		parent.parts = append(parent.parts, ep)
	}
	return ep
}

// server mounts a server cluster with the features it supports.
func (e *fakeEP) server(cluster uint32, features ...string) *fakeEP {
	if _, ok := e.servers[cluster]; !ok {
		e.order = append(e.order, cluster)
	}
	e.servers[cluster] = features
	return e
}

// implements records the attributes, commands or events a server
// implements.
func (e *fakeEP) implements(cluster uint32, element schema.RequirementElement, ids ...uint32) *fakeEP {
	if e.elements[cluster] == nil {
		e.elements[cluster] = map[schema.RequirementElement][]uint32{}
	}
	e.elements[cluster][element] = append(e.elements[cluster][element], ids...)
	return e
}

func (e *fakeEP) client(clusters ...uint32) *fakeEP {
	e.clients = append(e.clients, clusters...)
	return e
}

func (e *fakeEP) state(conditions ...string) *fakeEP {
	e.stated = append(e.stated, conditions...)
	return e
}

func (f *fakeTree) parentOf(ep uint16) (uint16, bool) {
	if p := f.eps[ep].parent; p != nil {
		return p.id, true
	}
	return 0, false
}

func (f *fakeTree) partsOf(ep uint16) []uint16 {
	out := make([]uint16, 0, len(f.eps[ep].parts))
	for _, p := range f.eps[ep].parts {
		out = append(out, p.id)
	}
	return out
}

func (f *fakeTree) deviceTypeIDs(ep uint16) []uint32  { return f.eps[ep].deviceTypes }
func (f *fakeTree) serverClusters(ep uint16) []uint32 { return f.eps[ep].order }
func (f *fakeTree) clientClusters(ep uint16) []uint32 { return f.eps[ep].clients }

func (f *fakeTree) features(ep uint16, cluster uint32) []string { return f.eps[ep].servers[cluster] }

func (f *fakeTree) supports(ep uint16, cluster uint32, element schema.RequirementElement, id uint32) bool {
	return slices.Contains(f.eps[ep].elements[cluster][element], id)
}

func (f *fakeTree) statedConditions(ep uint16) []string { return f.eps[ep].stated }
func (f *fakeTree) nodeConditions(uint16) []string      { return f.node }
func (f *fakeTree) describe(ep uint16) string           { return f.eps[ep].name }

// check runs DeviceTypeConformance.check on ep in a fresh pass.
func (f *fakeTree) check(l *dtLookups, ep *fakeEP) []DeviceTypeViolation {
	return newDTPass(f, l).check(ep.id)
}

// kindsOf renders violations as matter.js's test helper kindsOf does.
func kindsOf(vs []DeviceTypeViolation) []string {
	out := make([]string, 0, len(vs))
	for i := range vs {
		out = append(out, vs[i].Key())
	}
	return out
}

func detailsOf(vs []DeviceTypeViolation) []string {
	out := make([]string, 0, len(vs))
	for i := range vs {
		out = append(out, vs[i].Detail)
	}
	return out
}

func wantKinds(t *testing.T, got []DeviceTypeViolation, want ...string) {
	t.Helper()
	if want == nil {
		want = []string{}
	}
	if k := kindsOf(got); !slices.Equal(k, want) {
		t.Errorf("violations = %v, want %v\n%s", k, want, strings.Join(detailsOf(got), "\n"))
	}
}

// Conformance builders for synthetic requirements.

func confM() schema.Conformance { return schema.Conformance{Text: "M", Op: "M"} }
func confO() schema.Conformance { return schema.Conformance{Text: "O", Op: "O"} }
func confX() schema.Conformance { return schema.Conformance{Text: "X", Op: "X"} }

func confName(n string) schema.Conformance {
	return schema.Conformance{Text: n, Op: "name", Name: n}
}

func confOr(text string, a, b schema.Conformance) schema.Conformance {
	return schema.Conformance{Text: text, Op: "|", Args: []schema.Conformance{a, b}}
}

func confChoice(text string, expr schema.Conformance, name string, more bool) schema.Conformance {
	return schema.Conformance{Text: text, Op: "choice", Choice: &schema.ConformanceChoice{Name: name, Num: 1, OrMore: more}, Args: []schema.Conformance{expr}}
}

// clusterReq is a server cluster requirement.
func clusterReq(name string, id uint32, c schema.Conformance, nested ...schema.DeviceTypeRequirement) schema.DeviceTypeRequirement {
	return schema.DeviceTypeRequirement{
		Element: schema.RequirementServerCluster, Name: name, ID: id, HasID: true, Conformance: c,
		Referent: schema.Referent{Resolved: true, ID: id, Name: name}, Requirements: nested,
	}
}

// componentReq is a component device type requirement.
func componentReq(name string, id uint32, c schema.Conformance, count schema.CountRange, instance int, nested ...schema.DeviceTypeRequirement) schema.DeviceTypeRequirement {
	return schema.DeviceTypeRequirement{
		Element: schema.RequirementDeviceType, Name: name, ID: id, HasID: true, Conformance: c, Count: count, Instance: instance,
		Referent: schema.Referent{Resolved: true, ID: id, Name: name}, Requirements: nested,
	}
}

func featureReq(name string, bit uint32, c schema.Conformance) schema.DeviceTypeRequirement {
	return schema.DeviceTypeRequirement{
		Element: schema.RequirementFeature, Name: name, Conformance: c,
		Referent: schema.Referent{Resolved: true, ID: bit, Name: name},
	}
}

func minCount(n int) schema.CountRange { return schema.CountRange{Set: true, Min: n, HasMin: true} }
func maxCount(n int) schema.CountRange { return schema.CountRange{Set: true, Max: n, HasMax: true} }

// syntheticModel builds lookups over synthetic device types; cluster 6 is
// an application cluster with features LT (bit 0) and OFFONLY (bit 2).
func syntheticModel(base *schema.DeviceTypeDefinition, dts ...*schema.DeviceTypeDefinition) *dtLookups {
	m := &dtModel{
		deviceTypes: map[uint32]*schema.DeviceTypeDefinition{},
		features: func(cluster uint32) []schema.ClusterFeature {
			if cluster == 6 {
				return []schema.ClusterFeature{{Name: "LT", Bit: 0}, {Name: "OFFONLY", Bit: 2}}
			}
			return nil
		},
		classification: func(cluster uint32) string {
			switch cluster {
			case 6:
				return "application"
			case 3:
				return "endpoint"
			default:
				return ""
			}
		},
		bindable: func(uint32) bool { return true },
	}
	if base != nil {
		m.base = []*schema.DeviceTypeDefinition{base}
	}
	for _, dt := range dts {
		m.deviceTypes[dt.ID] = dt
	}
	return buildDeviceTypeLookups(m)
}
