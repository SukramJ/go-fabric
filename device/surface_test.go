// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"context"
	"errors"
	"fmt"
	"math/bits"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/endpoint/endpointtest"
	"github.com/SukramJ/go-fabric/groups"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/surface"
	"github.com/SukramJ/go-fabric/surface/surfacetest"
)

// onOffID is the OnOff cluster (on-off.element.ts, 0x0006).
const onOffID uint32 = 0x0006

// assembleAll is [assemble] returning the assembler's error.
func assembleAll(devices []*Device) (*endpoint.Topology, error) {
	mgr, err := groups.NewManager(nopGroupStore{}, nil)
	if err != nil {
		return nil, err
	}
	cfg := endpointtest.AssemblerConfig()
	cfg.Groups = mgr
	a, err := endpoint.New(endpointtest.NewFakeStore(), cfg, nil)
	if err != nil {
		return nil, err
	}
	specs := make([]endpoint.Spec, 0, len(devices))
	for _, d := range devices {
		specs = append(specs, d.Spec())
	}
	return a.Assemble(context.Background(), []endpoint.Snapshot{{Scope: "test", Endpoints: specs}})
}

// wireUint reads an unsigned attribute value as the wire carries it.
func wireUint(t *testing.T, v any) uint32 {
	t.Helper()
	switch x := v.(type) {
	case uint8:
		return uint32(x)
	case uint16:
		return uint32(x)
	case uint32:
		return x
	case uint64:
		return uint32(x) //nolint:gosec // FeatureMap and ClusterRevision fit
	default:
		t.Fatalf("value %#v is no unsigned integer", v)
		return 0
	}
}

// wireRead is what a controller's wildcard read of endpoint id returns,
// cluster by cluster: ClusterRevision, FeatureMap and the three command
// and attribute lists, and the events the dispatcher accepts an event path
// for (EventList itself is not served, dispatcher.go synthesizeGlobalRead).
func wireRead(t *testing.T, topo *endpoint.Topology, id uint16) map[uint32]surface.Cluster {
	t.Helper()
	d := endpoint.NewTopologyDispatcher(topo)
	out := map[uint32]surface.Cluster{}
	for _, r := range d.Read(context.Background(), im.ConcreteAttributePath{Endpoint: id, HasEndpoint: true}) {
		if r.Status != im.StatusSuccess {
			continue
		}
		c := out[r.Path.Cluster]
		c.ID = r.Path.Cluster
		ids := func() []uint32 {
			l, ok := r.Value.Value.([]uint32)
			if !ok {
				t.Fatalf("0x%04X/0x%04X reads %#v, no id list", r.Path.Cluster, r.Path.Attribute, r.Value.Value)
			}
			out := append(make([]uint32, 0, len(l)), l...)
			slices.Sort(out)
			return out
		}
		switch r.Path.Attribute {
		case cluster.AttrGlobalAttributeList:
			c.Attributes = ids()
		case cluster.AttrGlobalAcceptedCommandList:
			c.Accepted = ids()
		case cluster.AttrGlobalGeneratedCommandList:
			c.Generated = ids()
		case cluster.AttrGlobalFeatureMap:
			c.FeatureMap = wireUint(t, r.Value.Value)
		case cluster.AttrGlobalClusterRevision:
			c.Revision = uint16(wireUint(t, r.Value.Value)) //nolint:gosec // a ClusterRevision is a uint16
		}
		out[r.Path.Cluster] = c
	}
	for cid, c := range out {
		c.Events = []uint32{}
		const probe = 0x100
		for ev := range uint32(probe) {
			if d.EventPathStatus(im.ConcreteEventPath{Endpoint: id, HasEndpoint: true, Cluster: cid, HasCluster: true, Event: ev, HasEvent: true}) == im.StatusSuccess {
				c.Events = append(c.Events, ev)
			}
		}
		if len(c.Events) == probe {
			// No event lister: the dispatcher accepts every event path
			// and the surface lists none.
			c.Events = []uint32{}
		}
		out[cid] = c
	}
	return out
}

// TestSurfaceEqualsTheWire is the zero-value guarantee: for every device
// type the device layer builds without a declaration, the effective
// surface Device.Surface reports is, cluster by cluster, what a wildcard
// read of the assembled endpoint returns. The assembled endpoint's other
// clusters are the assembler's own.
func TestSurfaceEqualsTheWire(t *testing.T) {
	var devices []*Device
	for _, typ := range Types() {
		d, err := New(typ, testOptions(typ.Name))
		if err != nil {
			continue
		}
		devices = append(devices, d)
	}
	// A device with a feature selection and an optional element declared.
	r, _ := OnOffLightType.Server(onOffID)
	base, bit := optionalFeature(t, r)
	cid, attr := optionalAttribute(t)
	declared, err := New(&OnOffLightType, testOptions("declared"),
		withFeatures(onOffID, base|bit), withElements(cid, spec.Options{Attributes: []uint32{attr}}))
	if err != nil {
		t.Fatal(err)
	}
	devices = append(devices, declared)
	topo, err := assembleAll(devices)
	if err != nil {
		t.Fatal(err)
	}
	byKey := map[string]*endpoint.Endpoint{}
	for _, ep := range topo.Bridged() {
		byKey[endpoint.RenderSourceKey(ep.SourceKey)] = ep
	}
	compared := 0
	for _, d := range devices {
		rep := d.Surface()
		if rep.Policies != (surface.Policies{}).Digest() {
			t.Fatalf("%s: policies digest %x, want the zero value's", d.typ.Name, rep.Policies)
		}
		if len(rep.Effective.Endpoints) != 1 || len(rep.Effective.Root.Clusters) != 0 {
			t.Fatalf("%s: effective surface %+v, want one endpoint and an empty root", d.typ.Name, rep.Effective)
		}
		own := rep.Effective.Endpoints[0]
		ep := byKey[own.Key]
		if ep == nil {
			t.Fatalf("%s: key %q not assembled", d.typ.Name, own.Key)
		}
		if want := (surface.DeviceTypeRef{ID: uint32(d.typ.ID), Revision: d.typ.Revision}); !reflect.DeepEqual(own.DeviceTypes, []surface.DeviceTypeRef{want}) {
			t.Errorf("%s: device types %v", d.typ.Name, own.DeviceTypes)
		}
		wire := wireRead(t, topo, ep.ID)
		for _, c := range own.Clusters {
			if !reflect.DeepEqual(c, wire[c.ID]) {
				t.Errorf("%s cluster 0x%04X:\nsurface %+v\n   wire %+v", d.typ.Name, c.ID, c, wire[c.ID])
			}
			delete(wire, c.ID)
		}
		for id := range wire {
			if _, mounted := assemblerMounted[id]; !mounted && d.measured.ids[id] == false {
				t.Errorf("%s: the wire serves cluster 0x%04X the surface does not list", d.typ.Name, id)
			}
		}
		compared++
	}
	if !slices.Contains(clusterOf(declared.Surface(), cid).Attributes, attr) {
		t.Fatal("the declared device does not serve its declared attribute")
	}
	if compared < 50 {
		t.Fatalf("compared %d device types; the guard needs the generated set", compared)
	}
}

// optionalFeature returns a feature bit of the requirement's cluster the
// selection base may add and drop again: both selections pass
// spec.CheckFeatures.
func optionalFeature(t *testing.T, r ClusterRequirement) (base, bit uint32) {
	t.Helper()
	def := r.Definition
	base = r.Features | clusterMandatedFeatures(def)
	for i := range def.Features {
		b := def.Features[i].Mask()
		if base&b == 0 && spec.CheckFeatures(def, base|b) == nil && spec.CheckFeatures(def, base) == nil {
			return base, b
		}
	}
	t.Fatalf("%s has no optional feature over 0x%X", r.Name, base)
	return 0, 0
}

// optionalAttribute returns an optional attribute of OnOffLight's
// clusters, found in the definition at the default selection.
func optionalAttribute(t *testing.T) (cid, attr uint32) {
	t.Helper()
	for _, r := range OnOffLightType.Servers {
		if _, mounted := assemblerMounted[r.ID]; mounted || r.Definition == nil {
			continue
		}
		ctx := spec.Context(r.Definition, r.Features|clusterMandatedFeatures(r.Definition))
		for _, a := range r.Definition.Attributes {
			if a.Conformance.Applicability(ctx) == spec.ApplicabilityOptional && a.ID < 0xFFF0 {
				return r.ID, a.ID
			}
		}
	}
	t.Fatal("no optional attribute on OnOffLight's clusters")
	return 0, 0
}

func TestSurfaceRemoveFeature(t *testing.T) {
	r, ok := OnOffLightType.Server(onOffID)
	if !ok {
		t.Fatal("OnOffLight lists no OnOff")
	}
	base, bit := optionalFeature(t, r)
	opts := testOptions("light")
	// Without a policy the host's selection is served (the control).
	plain, err := New(&OnOffLightType, opts, withFeatures(onOffID, base|bit))
	if err != nil {
		t.Fatal(err)
	}
	if fm := featureMapOf(t, plain); fm&bit == 0 {
		t.Fatalf("FeatureMap 0x%X lacks the selected 0x%X", fm, bit)
	}
	policies := &surface.Policies{Custom: []surface.Policy{surfacetest.Remove(surface.KindFeature, surfacetest.Match{Cluster: onOffID, ID: bit})}}
	opts.Surface = policies
	d, err := New(&OnOffLightType, opts, withFeatures(onOffID, base|bit))
	if err != nil {
		t.Fatal(err)
	}
	if fm := featureMapOf(t, d); fm != base {
		t.Fatalf("FeatureMap 0x%X, want 0x%X without 0x%X", fm, base, bit)
	}
	rep := d.Surface()
	if rep.Policies != policies.Digest() {
		t.Fatal("the report does not carry the device's policies")
	}
	if c := clusterOf(rep, onOffID); c.FeatureMap != base {
		t.Fatalf("effective FeatureMap 0x%X", c.FeatureMap)
	}
	found := false
	for _, dec := range rep.Decisions {
		if dec.Kind == surface.KindFeature && dec.Cluster == onOffID && dec.ID == bit {
			found = dec.Verdict == surface.Remove && dec.Origin == surface.OriginHostDeclared && !dec.Mandatory
		}
	}
	if !found {
		t.Fatalf("no Remove decision for feature 0x%X: %+v", bit, rep.Decisions)
	}
}

func TestSurfaceMandatoryFeatureKept(t *testing.T) {
	r, _ := OnOffLightType.Server(onOffID)
	if r.Features == 0 {
		t.Fatal("OnOffLight mandates no OnOff feature; the test needs one")
	}
	bit := uint32(1) << bits.TrailingZeros32(r.Features)
	opts := testOptions("light")
	opts.Surface = &surface.Policies{Custom: []surface.Policy{surfacetest.Remove(surface.KindFeature, surfacetest.Match{Cluster: onOffID})}}
	d, err := New(&OnOffLightType, opts)
	if err != nil {
		t.Fatal(err)
	}
	if fm := featureMapOf(t, d); fm&bit == 0 {
		t.Fatalf("a type-mandated feature was removed: FeatureMap 0x%X", fm)
	}
	for _, dec := range d.Surface().Decisions {
		if dec.Kind != surface.KindFeature || dec.ID != bit {
			continue
		}
		if dec.Verdict != surface.Keep || dec.Origin != surface.OriginDeviceType || !dec.Mandatory ||
			!strings.HasPrefix(dec.Reason, "kept: mandatory at pin (surfacetest.Remove") {
			t.Fatalf("decision %+v", dec)
		}
		return
	}
	t.Fatal("no decision for the mandated feature")
}

func TestSurfaceRemoveElementAndCluster(t *testing.T) {
	cid, attr := optionalAttribute(t)
	decl := withElements(cid, spec.Options{Attributes: []uint32{attr}})
	plain, err := New(&OnOffLightType, testOptions("light"), decl)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(clusterOf(plain.Surface(), cid).Attributes, attr) {
		t.Fatalf("the declared attribute 0x%04X is not served (control)", attr)
	}
	opts := testOptions("light")
	opts.Surface = &surface.Policies{Custom: []surface.Policy{surfacetest.Remove(surface.KindAttribute, surfacetest.Match{Cluster: cid, ID: attr})}}
	d, err := New(&OnOffLightType, opts, decl)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(clusterOf(d.Surface(), cid).Attributes, attr) {
		t.Fatalf("removed attribute 0x%04X still served", attr)
	}
	if slices.Contains(endpoint.GlobalList(d.Server(cid), cluster.AttrGlobalAttributeList), attr) {
		t.Fatal("the server still lists the removed attribute")
	}

	// An opted-in optional cluster is not built once removed; its
	// declaration's hooks go with it.
	var optional uint32
	for _, r := range OnOffLightType.Servers {
		if _, mounted := assemblerMounted[r.ID]; !mounted && !r.Mandatory && r.Definition != nil {
			if _, err := New(&OnOffLightType, testOptions("probe"), withOptional(r.ID)); err == nil {
				optional = r.ID
				break
			}
		}
	}
	if optional == 0 {
		t.Skip("OnOffLight has no buildable optional cluster")
	}
	opts.Surface = &surface.Policies{Custom: []surface.Policy{surfacetest.Remove(surface.KindCluster, surfacetest.Match{ID: optional})}}
	d, err = New(&OnOffLightType, opts, withOptional(optional))
	if err != nil {
		t.Fatal(err)
	}
	if d.Server(optional) != nil || clusterOf(d.Surface(), optional).ID != 0 {
		t.Fatalf("removed cluster 0x%04X is built", optional)
	}
}

func TestSurfaceRefuse(t *testing.T) {
	opts := testOptions("light")
	opts.Surface = &surface.Policies{Custom: []surface.Policy{surfacetest.Refuse(surface.KindCluster, surfacetest.Match{ID: onOffID})}}
	_, err := New(&OnOffLightType, opts)
	if !errors.Is(err, surface.ErrRefused) {
		t.Fatalf("err = %v, want ErrRefused", err)
	}
	for _, want := range []string{"OnOffLight", fmt.Sprintf("cluster 0x%04X", onOffID), `endpoint "test:light"`, "surfacetest.Refuse"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err, want)
		}
	}
	opts.Surface = &surface.Policies{Custom: []surface.Policy{nil}}
	if _, err := New(&OnOffLightType, opts); err == nil {
		t.Fatal("a nil policy composed")
	}
}

// TestSurfaceReportComplete: every candidate has a decision — the device
// type, every cluster the device layer builds, every FeatureMap bit of
// its default servers — and the zero value speaks once per candidate.
func TestSurfaceReportComplete(t *testing.T) {
	for _, typ := range Types() {
		d, err := New(typ, testOptions(typ.Name))
		if err != nil {
			continue
		}
		rep := d.Surface()
		seen := map[surface.Element]bool{}
		for _, dec := range rep.Decisions {
			if dec.Verdict != surface.Keep || dec.Policy != "" || seen[dec.Element] {
				t.Fatalf("%s: decision %+v under the zero value", typ.Name, dec)
			}
			seen[dec.Element] = true
		}
		has := func(kind surface.Kind, cid, id uint32) bool {
			for e := range seen {
				if e.Kind == kind && e.Cluster == cid && e.ID == id {
					return true
				}
			}
			return false
		}
		if !has(surface.KindDeviceType, 0, uint32(typ.ID)) {
			t.Errorf("%s: no device type row", typ.Name)
		}
		for _, c := range rep.Effective.Endpoints[0].Clusters {
			if !has(surface.KindCluster, c.ID, c.ID) {
				t.Errorf("%s: no row for cluster 0x%04X", typ.Name, c.ID)
			}
			if r, listed := typ.Server(c.ID); !listed || r.Definition == nil {
				continue
			}
			for x := c.FeatureMap; x != 0; x &= x - 1 {
				if bit := uint32(1) << bits.TrailingZeros32(x); !has(surface.KindFeature, c.ID, bit) {
					t.Errorf("%s: no row for feature 0x%X of 0x%04X", typ.Name, bit, c.ID)
				}
			}
		}
	}
}

// TestSurfaceNumberOnceAssembled: the report states the endpoint number
// once the device has one, and is a copy.
func TestSurfaceNumberOnceAssembled(t *testing.T) {
	d, err := New(&OnOffLightType, testOptions("light"))
	if err != nil {
		t.Fatal(err)
	}
	r := d.Surface()
	if r.Effective.Endpoints[0].Number != 0 {
		t.Fatal("a number before assembly")
	}
	r.Effective.Endpoints[0].Clusters[0].Attributes[0] = 0xDEAD
	if d.Surface().Effective.Endpoints[0].Clusters[0].Attributes[0] == 0xDEAD {
		t.Fatal("Surface returns shared lists")
	}
}

func featureMapOf(t *testing.T, d *Device) uint32 {
	t.Helper()
	v, ok := d.Server(onOffID).MatterRead(cluster.AttrGlobalFeatureMap)
	if !ok {
		t.Fatal("no FeatureMap")
	}
	return wireUint(t, v)
}

func clusterOf(r surface.Report, id uint32) surface.Cluster {
	for _, c := range r.Effective.Endpoints[0].Clusters {
		if c.ID == id {
			return c
		}
	}
	return surface.Cluster{}
}
