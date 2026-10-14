// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package client

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/cluster/spec/descriptor"
	"github.com/SukramJ/go-fabric/cluster/spec/onoff"
	"github.com/SukramJ/go-fabric/controller/interaction"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// Device types and clusters of the test topologies.
const (
	dtRootNode   = 0x0016
	dtAggregator = 0x000E
	dtBridged    = 0x0013
	dtPlug       = 0x010A
	dtOnOffLight = 0x0100
	clBasicInfo  = 0x0028
	clBridgedBI  = 0x0039
)

// report builds an interaction.Report.
type report struct{ r interaction.Report }

func (b *report) attr(ep uint16, cluster, attr uint32, v spec.Node, version uint32) *report {
	b.r.Attributes = append(b.r.Attributes, interaction.AttributeData{
		Path:        im.ConcreteAttributePath{Endpoint: ep, Cluster: cluster, Attribute: attr, HasEndpoint: true, HasCluster: true, HasAttribute: true},
		DataVersion: version, Value: v,
	})
	return b
}

// cluster adds a cluster's global attributes and the given values; its
// AttributeList names them all.
func (b *report) cluster(ep uint16, cluster uint32, featureMap uint64, values map[uint32]spec.Node, accepted ...uint64) *report {
	ids := make([]uint64, 0, 5+len(values))
	ids = append(ids, uint64(spec.AttrClusterRevision), uint64(spec.AttrFeatureMap), uint64(spec.AttrAttributeList), uint64(spec.AttrAcceptedCommandList), uint64(spec.AttrGeneratedCommandList))
	keys := make([]uint32, 0, len(values))
	for id := range values {
		keys = append(keys, id)
	}
	slices.Sort(keys)
	for _, id := range keys {
		b.attr(ep, cluster, id, values[id], 1)
		ids = append(ids, uint64(id))
	}
	b.attr(ep, cluster, spec.AttrClusterRevision, uintNode(1), 1)
	b.attr(ep, cluster, spec.AttrFeatureMap, uintNode(featureMap), 1)
	b.attr(ep, cluster, spec.AttrAttributeList, listNode(ids...), 1)
	b.attr(ep, cluster, spec.AttrAcceptedCommandList, listNode(accepted...), 1)
	b.attr(ep, cluster, spec.AttrGeneratedCommandList, listNode(), 1)
	return b
}

func structNode(fields ...uint64) spec.Node {
	n := spec.Node{El: tlv.Element{Type: tlv.TypeStructure, IsContainer: true}}
	for i, f := range fields {
		c := uintNode(f)
		c.El.Tag = tlv.ContextTag(uint8(i)) //nolint:gosec // a test field index
		n.Children = append(n.Children, c)
	}
	return n
}

func deviceTypes(dts ...uint64) spec.Node {
	n := spec.Node{El: tlv.Element{Type: tlv.TypeArray, IsContainer: true}}
	for _, dt := range dts {
		n.Children = append(n.Children, structNode(dt, 1))
	}
	return n
}

func ids16(vs ...uint16) []uint64 {
	out := make([]uint64, 0, len(vs))
	for _, v := range vs {
		out = append(out, uint64(v))
	}
	return out
}

// endpoint adds an endpoint's Descriptor, and each server cluster with an
// attribute 0 when it is not the Descriptor; dts nil leaves the
// DeviceTypeList out.
func (b *report) endpoint(ep uint16, dts []uint64, servers []uint32, parts ...uint16) *report {
	sl := make([]uint64, 0, len(servers))
	for _, c := range servers {
		sl = append(sl, uint64(c))
	}
	values := map[uint32]spec.Node{
		descriptor.AttrServerList: listNode(sl...),
		descriptor.AttrClientList: listNode(),
		descriptor.AttrPartsList:  listNode(ids16(parts...)...),
	}
	if dts != nil {
		values[descriptor.AttrDeviceTypeList] = deviceTypes(dts...)
	}
	b.cluster(ep, descriptor.ClusterID, 0, values)
	for _, c := range servers {
		if c != descriptor.ClusterID {
			b.cluster(ep, c, 0, map[uint32]spec.Node{0: boolNode(false)})
		}
	}
	return b
}

// bridge is a bridge of n plugs: the root, an aggregator on endpoint 1
// (full-family), plugs 2..n+1 below it.
func bridge(n int) *report {
	b := &report{}
	plugs := make([]uint16, 0, n)
	for i := range n {
		plugs = append(plugs, uint16(2+i)) //nolint:gosec // a small test count
	}
	b.endpoint(0, []uint64{dtRootNode}, []uint32{descriptor.ClusterID, clBasicInfo}, append([]uint16{1}, plugs...)...)
	b.endpoint(1, []uint64{dtAggregator}, []uint32{descriptor.ClusterID}, plugs...)
	for _, p := range plugs {
		b.endpoint(p, []uint64{dtBridged, dtPlug}, []uint32{descriptor.ClusterID, clBridgedBI, onoff.ClusterID})
	}
	return b
}

// recorder collects the hooks a node calls.
type recorder struct {
	mu  sync.Mutex
	log []string
}

func (r *recorder) add(s string) {
	r.mu.Lock()
	r.log = append(r.log, s)
	r.mu.Unlock()
}

func (r *recorder) take() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.log
	r.log = nil
	return out
}

func (r *recorder) hooks() Hooks {
	return Hooks{
		EndpointInstalled: func(ep *Endpoint) { r.add(fmt.Sprintf("endpoint %d", ep.Number())) },
		ClusterInstalled:  func(ep *Endpoint, c *Cluster) { r.add(fmt.Sprintf("cluster %d/%s", ep.Number(), c.Name())) },
		ClusterReplaced:   func(ep *Endpoint, c *Cluster) { r.add(fmt.Sprintf("replaced %d/%s", ep.Number(), c.Name())) },
		ClusterDeleted:    func(ep *Endpoint, c *Cluster) { r.add(fmt.Sprintf("deleted %d/%s", ep.Number(), c.Name())) },
		Changed:           func() { r.add("changed") },
	}
}

// logRecorder is a slog handler keeping the messages.
type logRecorder struct {
	mu   sync.Mutex
	msgs []string
}

func (h *logRecorder) Enabled(context.Context, slog.Level) bool { return true }
func (h *logRecorder) WithAttrs([]slog.Attr) slog.Handler       { return h }
func (h *logRecorder) WithGroup(string) slog.Handler            { return h }
func (h *logRecorder) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	h.msgs = append(h.msgs, r.Message)
	h.mu.Unlock()
	return nil
}

func (h *logRecorder) has(msg string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Contains(h.msgs, msg)
}

// testNode is a node over st with a recorder and no network; the hooks of
// its loading are dropped.
func testNode(t *testing.T, st Store) (*Node, *recorder, *logRecorder) {
	t.Helper()
	n, rec, logs := loadedNode(t, st)
	rec.take()
	return n, rec, logs
}

// loadedNode is testNode with the hooks of the loading kept.
func loadedNode(t *testing.T, st Store) (*Node, *recorder, *logRecorder) {
	t.Helper()
	rec, logs := &recorder{}, &logRecorder{}
	n, err := New(Config{ID: "n", Store: st, Interaction: interaction.Bind(nil, interaction.Config{}), Hooks: rec.hooks(), Logger: slog.New(logs)})
	if err != nil {
		t.Fatal(err)
	}
	return n, rec, logs
}

var wildcard = newScope([]im.ConcreteAttributePath{{}}, false)

// tree describes the installed tree: "ep:type:owner" per endpoint.
func tree(n *Node) string {
	eps := n.Endpoints()
	parts := make([]string, 0, len(eps))
	for _, ep := range eps {
		owner := "-"
		if o := ep.Owner(); o != nil {
			owner = strconv.Itoa(int(o.Number()))
		}
		parts = append(parts, fmt.Sprintf("%d:%#x:%s", ep.Number(), ep.DeviceType(), owner))
	}
	return strings.Join(parts, " ")
}

// TestStructureBridge: a bridge's report installs the tree with its owners
// (ClientStructure.ts:851-1009, :1300-1403): the aggregator's full-family
// list makes it the plugs' parent; the root keeps its RootNode type with
// an unknown revision (:876-892); the root's clusters are announced, an
// endpoint's come with it (:1227-1263).
func TestStructureBridge(t *testing.T) {
	t.Parallel()
	n, rec, _ := testNode(t, &MemoryStore{})
	n.apply(wildcard, &bridge(2).r)
	if got := tree(n); got != "0:0x16:- 1:0xe:0 2:0x10a:1 3:0x10a:1" {
		t.Fatalf("tree %s", got)
	}
	root := n.Endpoint(0)
	if _, ok := root.DeviceRevision(); ok || root.Name() != "RootNode" || root.Classification() != "node" || !root.Installed() {
		t.Fatal("root")
	}
	plug := n.Endpoint(2)
	if rev, ok := plug.DeviceRevision(); !ok || rev != 1 || plug.Name() != "OnOffPlugInUnit" || plug.Classification() != "simple" {
		t.Fatalf("plug %s %d", plug.Name(), rev)
	}
	if c := plug.Cluster(onoff.ClusterID); c == nil || c.Definition() == nil || !c.Supports(onoff.AttrOnOff) || len(plug.Clusters()) != 3 {
		t.Fatal("plug clusters")
	}
	if plug.Cluster(0x7777) != nil || n.Endpoint(9) != nil || len(n.Endpoint(1).Parts()) != 2 {
		t.Fatal("lookups")
	}
	want := []string{"cluster 0/Descriptor", "cluster 0/BasicInformation", "endpoint 1", "endpoint 2", "endpoint 3", "changed"}
	if got := rec.take(); !slices.Equal(got, want) {
		t.Fatalf("hooks %v, want %v", got, want)
	}
}

// TestStructureTree: a tree-composed endpoint owns the parts it names
// (:1381-1392); of several ordinary claimants the first wins with a
// warning (:1382-1390); a full-family claim yields to an ordinary one.
func TestStructureTree(t *testing.T) {
	t.Parallel()
	n, _, logs := testNode(t, &MemoryStore{})
	b := &report{}
	b.endpoint(0, []uint64{dtRootNode}, []uint32{descriptor.ClusterID}, 1, 2, 3, 4)
	b.endpoint(1, []uint64{dtOnOffLight}, []uint32{descriptor.ClusterID}, 3)
	b.endpoint(2, []uint64{dtOnOffLight}, []uint32{descriptor.ClusterID}, 3)
	b.endpoint(3, []uint64{dtOnOffLight}, []uint32{descriptor.ClusterID})
	b.endpoint(4, []uint64{dtAggregator}, []uint32{descriptor.ClusterID}, 3)
	n.apply(wildcard, &b.r)
	if got := tree(n); got != "0:0x16:- 1:0x100:0 2:0x100:0 3:0x100:1 4:0xe:0" {
		t.Fatalf("tree %s", got)
	}
	if !logs.has("matter.controller.client.several_parents") {
		t.Fatal("no warning for several ordinary claimants")
	}
}

// TestStructureUnknownCompositionWaits: a part whose claimant has not
// reported its device types waits (:1374-1376) until it does.
func TestStructureUnknownCompositionWaits(t *testing.T) {
	t.Parallel()
	n, rec, _ := testNode(t, &MemoryStore{})
	b := &report{}
	b.endpoint(0, []uint64{dtRootNode}, []uint32{descriptor.ClusterID}, 1, 2)
	b.endpoint(1, nil, []uint32{descriptor.ClusterID}, 2)
	b.endpoint(2, []uint64{dtOnOffLight}, []uint32{descriptor.ClusterID})
	n.apply(wildcard, &b.r)
	if got := tree(n); got != "0:0x16:- 1:0xffffffff:0" {
		t.Fatalf("tree %s", got)
	}
	rec.take()
	n.apply(wildcard, &(&report{}).attr(1, descriptor.ClusterID, descriptor.AttrDeviceTypeList, deviceTypes(dtOnOffLight), 2).r)
	if got := tree(n); got != "0:0x16:- 1:0x100:0 2:0x100:1" {
		t.Fatalf("tree after the device types %s", got)
	}
	if got := rec.take(); !slices.Equal(got, []string{"endpoint 2", "changed"}) {
		t.Fatalf("hooks %v", got)
	}
}

// TestStructureRootListErases: an endpoint the root no longer names is
// erased with its store (:971-1008, :1130-1161); one that still holds a
// named endpoint is kept with a warning (:987-1003).
func TestStructureRootListErases(t *testing.T) {
	t.Parallel()
	st := &MemoryStore{}
	n, rec, logs := testNode(t, st)
	n.apply(wildcard, &bridge(2).r)
	rec.take()
	b := &report{}
	b.attr(0, descriptor.ClusterID, descriptor.AttrPartsList, listNode(1, 2), 2)
	b.attr(1, descriptor.ClusterID, descriptor.AttrPartsList, listNode(2), 2)
	n.apply(wildcard, &b.r)
	if got := tree(n); got != "0:0x16:- 1:0xe:0 2:0x10a:1" {
		t.Fatalf("tree %s", got)
	}
	if got := rec.take(); !slices.Equal(got, []string{"changed"}) {
		t.Fatalf("hooks %v", got)
	}
	eps, _ := st.Load("n")
	for _, e := range eps {
		if e.Number == 3 {
			t.Fatal("the erased endpoint is still stored")
		}
	}
	// The root drops the aggregator but still names its plug: kept.
	n.apply(wildcard, &(&report{}).attr(0, descriptor.ClusterID, descriptor.AttrPartsList, listNode(2), 3).r)
	if got := tree(n); got != "0:0x16:- 1:0xe:0 2:0x10a:1" || !logs.has("matter.controller.client.endpoint_kept") {
		t.Fatalf("tree %s", got)
	}
	// Both gone: the aggregator is erased with its plug.
	n.apply(wildcard, &(&report{}).attr(0, descriptor.ClusterID, descriptor.AttrPartsList, listNode(), 4).r)
	if got := tree(n); got != "0:0x16:-" {
		t.Fatalf("tree %s", got)
	}
}

// TestStructureServerList: a cluster the ServerList drops is deleted with
// its store (:922-937, :1177-1202) — unless the peer sends data for it in
// the same report ("Schrödinger's cluster", :555-565, :926-933).
func TestStructureServerList(t *testing.T) {
	t.Parallel()
	st := &MemoryStore{}
	n, rec, logs := testNode(t, st)
	n.apply(wildcard, &bridge(2).r)
	rec.take()
	b := &report{}
	b.attr(2, onoff.ClusterID, onoff.AttrOnOff, boolNode(true), 2)
	b.attr(2, descriptor.ClusterID, descriptor.AttrServerList, listNode(uint64(descriptor.ClusterID), clBridgedBI), 2)
	b.attr(3, descriptor.ClusterID, descriptor.AttrServerList, listNode(uint64(descriptor.ClusterID), clBridgedBI), 2)
	n.apply(wildcard, &b.r)
	if got := rec.take(); !slices.Equal(got, []string{"deleted 3/OnOff", "changed"}) {
		t.Fatalf("hooks %v", got)
	}
	if n.Endpoint(3).Cluster(onoff.ClusterID) != nil || n.Endpoint(2).Cluster(onoff.ClusterID) == nil {
		t.Fatal("deletion")
	}
	if _, _, ok := stored(t, st, 3, onoff.ClusterID); ok {
		t.Fatal("the deleted cluster is still stored")
	}
	// Data after the list in the same report cancels the deletion too.
	b = &report{}
	b.attr(2, descriptor.ClusterID, descriptor.AttrServerList, listNode(uint64(descriptor.ClusterID), clBridgedBI), 3)
	b.attr(2, onoff.ClusterID, onoff.AttrOnOff, boolNode(false), 3)
	n.apply(wildcard, &b.r)
	if n.Endpoint(2).Cluster(onoff.ClusterID) == nil || !logs.has("matter.controller.client.cluster_kept") {
		t.Fatal("a cluster with data in the report was deleted")
	}
	// A cluster that comes back is installed afresh.
	n.apply(wildcard, &(&report{}).cluster(3, onoff.ClusterID, 0, map[uint32]spec.Node{0: boolNode(true)}).r)
	if got := rec.take(); !slices.Equal(got, []string{"changed", "cluster 3/OnOff", "changed"}) {
		t.Fatalf("hooks %v", got)
	}
}

// TestStructureAttributeListAndFeatures: a shrinking AttributeList prunes
// the stored values (:668-707) and, with a changed FeatureMap, rebuilds
// the shape (:632-659).
func TestStructureAttributeListAndFeatures(t *testing.T) {
	t.Parallel()
	st := &MemoryStore{}
	n, rec, _ := testNode(t, st)
	b := bridge(1)
	b.cluster(2, onoff.ClusterID, 0, map[uint32]spec.Node{onoff.AttrOnOff: boolNode(true), onoff.AttrOnTime: uintNode(5)})
	n.apply(wildcard, &b.r)
	rec.take()
	c := n.Endpoint(2).Cluster(onoff.ClusterID)
	if _, ok := c.Value(onoff.AttrOnTime); !ok {
		t.Fatal("no OnTime")
	}
	b = &report{}
	b.attr(2, onoff.ClusterID, spec.AttrFeatureMap, uintNode(uint64(onoff.FeatureLighting)), 2)
	b.attr(2, onoff.ClusterID, spec.AttrAttributeList, listNode(0, 0xFFF8, 0xFFF9, 0xFFFB, 0xFFFC, 0xFFFD), 2)
	n.apply(wildcard, &b.r)
	if got := rec.take(); !slices.Equal(got, []string{"replaced 2/OnOff", "changed"}) {
		t.Fatalf("hooks %v", got)
	}
	if _, ok := c.Value(onoff.AttrOnTime); ok || !slices.Equal(c.Features(), []string{"LT"}) || c.Supports(onoff.AttrOnTime) {
		t.Fatalf("after the change: features %v", c.Features())
	}
	if values, _, _ := stored(t, st, 2, onoff.ClusterID); len(values) != 6 {
		t.Fatalf("stored %d values", len(values))
	}
	// A report that changes nothing of the definition keeps the shape.
	n.apply(wildcard, &(&report{}).attr(2, onoff.ClusterID, spec.AttrClusterRevision, uintNode(1), 3).r)
	if got := rec.take(); !slices.Equal(got, []string{"changed"}) {
		t.Fatalf("hooks %v", got)
	}
}

// TestStructureVersionAndFabricFilter: the version is taken only from a read
// covering the whole cluster (:494-497); an interaction with another fabric
// filter than the subscription's stores nothing (:468-471).
func TestStructureVersionAndFabricFilter(t *testing.T) {
	t.Parallel()
	n, _, _ := testNode(t, &MemoryStore{})
	n.apply(wildcard, &bridge(1).r)
	c := n.Endpoint(2).Cluster(onoff.ClusterID)
	specific := newScope([]im.ConcreteAttributePath{{Endpoint: 2, Cluster: onoff.ClusterID, Attribute: 0, HasEndpoint: true, HasCluster: true, HasAttribute: true}}, false)
	n.apply(specific, &(&report{}).attr(2, onoff.ClusterID, 0, boolNode(true), 9).r)
	if v, _ := c.Version(); v != 1 {
		t.Fatalf("a specific read set version %d", v)
	}
	if v, _ := c.Value(0); !v.El.Bool {
		t.Fatal("value not applied")
	}
	n.apply(newScope([]im.ConcreteAttributePath{{}}, true), &(&report{}).attr(2, onoff.ClusterID, 0, boolNode(false), 10).r)
	if v, _ := c.Value(0); !v.El.Bool {
		t.Fatal("an unfiltered read was stored")
	}
	n.apply(wildcard, &(&report{}).attr(2, onoff.ClusterID, 0, boolNode(false), 11).r)
	if v, _ := c.Version(); v != 11 {
		t.Fatalf("version %d after a wildcard read", v)
	}
}

// TestStructureReload: a node on the same store loads the same tree and
// emits its events once (:183-232); a cached cluster whose values its
// cached AttributeList omits forgets its version (:721-744).
func TestStructureReload(t *testing.T) {
	t.Parallel()
	st := &MemoryStore{}
	first, _, _ := testNode(t, st)
	first.apply(wildcard, &bridge(2).r)
	want := tree(first)
	_ = st.Save("n", 3, onoff.ClusterID, map[uint32][]byte{0x7777: mustEncode(t, uintNode(1))}, ptr(uint32(5)))
	second, rec, _ := loadedNode(t, st)
	if got := tree(second); got != want {
		t.Fatalf("reloaded tree %s, want %s", got, want)
	}
	want2 := []string{"cluster 0/Descriptor", "cluster 0/BasicInformation", "endpoint 1", "endpoint 2", "endpoint 3", "changed"}
	if got := rec.take(); !slices.Equal(got, want2) {
		t.Fatalf("hooks %v", got)
	}
	if _, ok := second.Endpoint(3).Cluster(onoff.ClusterID).Version(); ok {
		t.Fatal("the unlisted seed value kept the version")
	}
	if v, ok := second.Endpoint(2).Cluster(onoff.ClusterID).Version(); !ok || v != 1 {
		t.Fatal("a clean cluster lost its version")
	}
	// Erase forgets the image and the store.
	if err := second.Erase(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := tree(second); got != "0:0x16:-" {
		t.Fatalf("after Erase %s", got)
	}
	// An unreadable stored value is skipped.
	_ = st.Save("n", 1, descriptor.ClusterID, map[uint32][]byte{0: {0x15}}, nil)
	if _, _, logs := testNode(t, st); !logs.has("matter.controller.client.cache_value_unreadable") {
		t.Fatal("no warning for an unreadable value")
	}
}

func mustEncode(t *testing.T, n spec.Node) []byte {
	t.Helper()
	b, err := encodeValue(n)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestStructureEvents: an event of an installed endpoint without data in
// the report is delivered before the structure events; one of an endpoint
// with data, or of an unknown one, after them (:506-528, :1553-1563).
func TestStructureEvents(t *testing.T) {
	t.Parallel()
	n, _, _ := testNode(t, &MemoryStore{})
	n.apply(wildcard, &bridge(2).r)
	ev := func(ep uint16) interaction.EventData {
		return interaction.EventData{Path: im.ConcreteEventPath{Endpoint: ep, Cluster: onoff.ClusterID, HasEndpoint: true, HasCluster: true, HasEvent: true}}
	}
	b := &report{}
	b.attr(2, onoff.ClusterID, 0, boolNode(true), 3)
	b.r.Events = []interaction.EventData{ev(2), ev(3), ev(9)}
	n.mu.Lock()
	n.s.mutate(wildcard, &b.r)
	em := n.s.takeEmissions()
	n.mu.Unlock()
	var got []string
	for _, e := range em {
		switch e.kind {
		case emitEvent:
			got = append(got, fmt.Sprint("event ", e.event.Path.Endpoint))
		case emitChanged:
			got = append(got, "changed")
		default:
			got = append(got, "structure")
		}
	}
	if !slices.Equal(got, []string{"event 3", "changed", "event 2", "event 9"}) {
		t.Fatalf("emissions %v", got)
	}
}

// TestStructureHookPanics: a panicking structure hook is logged, never
// raised (ClientStructureEvents.ts:138-144).
func TestStructureHookPanics(t *testing.T) {
	t.Parallel()
	logs := &logRecorder{}
	boom := func(*Endpoint, *Cluster) { panic("boom") }
	n, err := New(Config{ID: "n", Interaction: interaction.Bind(nil, interaction.Config{}), Logger: slog.New(logs), Hooks: Hooks{
		EndpointInstalled: func(*Endpoint) { panic("boom") }, ClusterInstalled: boom, ClusterReplaced: boom, ClusterDeleted: boom,
	}})
	if err != nil {
		t.Fatal(err)
	}
	n.apply(wildcard, &bridge(1).r)
	n.apply(wildcard, &(&report{}).attr(2, descriptor.ClusterID, descriptor.AttrServerList, listNode(uint64(descriptor.ClusterID)), 2).r)
	n.apply(wildcard, &(&report{}).attr(0, descriptor.ClusterID, spec.AttrFeatureMap, uintNode(1), 2).r)
	if !logs.has("matter.controller.client.hook_failed") {
		t.Fatal("no log")
	}
}

// TestStructureVersionFilters: one filter per covered cluster with a known
// version (ClientStructure.ts:252-290); with refreshChangesOmitted none for
// a cluster in which the peer has a changes-omitted attribute (:269-271,
// :1674-1692).
func TestStructureVersionFilters(t *testing.T) {
	t.Parallel()
	n, _, _ := testNode(t, &MemoryStore{})
	b := bridge(1)
	b.cluster(2, powerSource, 0, map[uint32]spec.Node{0: uintNode(1), 0x000B: uintNode(3300)})
	b.cluster(1, powerSource, 0, map[uint32]spec.Node{0: uintNode(1)})
	n.apply(wildcard, &b.r)
	n.mu.Lock()
	all := n.s.injectVersionFilters(wildcard, false)
	refreshed := n.s.injectVersionFilters(wildcard, true)
	plug := n.s.injectVersionFilters(newScope([]im.ConcreteAttributePath{{Endpoint: 2, HasEndpoint: true}}, false), false)
	n.mu.Unlock()
	has := func(fs []im.DataVersionFilter, ep uint16, c uint32) bool {
		return slices.ContainsFunc(fs, func(f im.DataVersionFilter) bool { return f.Endpoint == ep && f.Cluster == c && f.DataVersion == 1 })
	}
	if len(all) != 8 || !has(all, 2, powerSource) || !has(all, 1, powerSource) {
		t.Fatalf("filters %+v", all)
	}
	if len(refreshed) != 7 || has(refreshed, 2, powerSource) || !has(refreshed, 1, powerSource) {
		t.Fatalf("refreshed filters %+v", refreshed)
	}
	if len(plug) != 4 {
		t.Fatalf("endpoint 2 filters %+v", plug)
	}
	// Without the peer's AttributeList every changes-omitted attribute of
	// the definition counts (:1691).
	n.apply(wildcard, &(&report{}).attr(1, powerSource, spec.AttrAttributeList, listNode(), 2).r)
	n.mu.Lock()
	refreshed = n.s.injectVersionFilters(wildcard, true)
	n.mu.Unlock()
	if has(refreshed, 1, powerSource) {
		t.Fatal("a cluster without its AttributeList kept its filter")
	}
	// A named cluster (AccessControl has no generated definition) is judged
	// by the schema's table: AuxiliaryAcl (0x0007) is "C".
	n.apply(wildcard, &(&report{}).cluster(0, 0x001F, 0, map[uint32]spec.Node{0: listNode(), 0x0007: listNode()}).r)
	n.apply(wildcard, &(&report{}).cluster(1, 0x001F, 0, map[uint32]spec.Node{0: listNode()}).r)
	n.mu.Lock()
	refreshed = n.s.injectVersionFilters(wildcard, true)
	n.mu.Unlock()
	if has(refreshed, 0, 0x001F) || !has(refreshed, 1, 0x001F) {
		t.Fatalf("named clusters' filters %+v", refreshed)
	}
}

const powerSource = 0x002F

// TestStructureRetractedClaim: a part a list stops naming loses that
// claimant (:1426-1447) and goes to the remaining one.
func TestStructureRetractedClaim(t *testing.T) {
	t.Parallel()
	n, _, _ := testNode(t, &MemoryStore{})
	b := &report{}
	b.endpoint(0, []uint64{dtRootNode}, []uint32{descriptor.ClusterID}, 1, 2)
	b.endpoint(1, nil, []uint32{descriptor.ClusterID}, 2)
	b.endpoint(2, []uint64{dtOnOffLight}, []uint32{descriptor.ClusterID})
	n.apply(wildcard, &b.r)
	n.apply(wildcard, &(&report{}).attr(1, descriptor.ClusterID, descriptor.AttrPartsList, listNode(), 2).r)
	if got := tree(n); got != "0:0x16:- 1:0xffffffff:0 2:0x100:0" {
		t.Fatalf("tree %s", got)
	}
}
