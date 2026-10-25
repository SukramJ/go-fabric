// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behaviors/descriptor/DescriptorServerTest.ts
// at 862ad75f: the cases the cluster/core Descriptor parity tests do not
// cover — a removed part, the TagList edits, full-family composition, the
// hierarchy orders, membership changes that keep the count and the PartsList
// order (DescriptorServer.ts addTags :122-147, #updatePartsList :213-225,
// #currentPartsListNumbers :227-254, #composesFullFamily :262-270).
//
// matter.js builds MockEndpoints and MockServerNodes and reads
// state.descriptor; the port builds the endpoint tree through the
// assembler, as the daemon does, and reads PartsList through the
// TopologyDispatcher. The Go tree differs from a ServerNode's in two fixed
// ways: the Aggregator is always endpoint 1 under the root, and a bridged
// endpoint always lists BridgedNode, a full-family type, so a "composed
// parent" whose PartsList is its children is a part (Spec.Parts) and the
// endpoint whose device type changes "at runtime" is a part as well.
// matter.js's runtime changes — a device type set or added, an endpoint
// added or closed — are a new snapshot the assembler assembles again; an
// assembly is one step, so the order in which matter.js adds a parent and
// its child is the order of the snapshots, and a manual endpoint number is
// the number the store already holds for the stable key. The TagList edits
// run on the Descriptor server itself (core.Descriptor EnableTagList,
// AddTags), the MockEndpoint's local agent call.
//
// The tree is served by a fabrictest device whose host topology
// (Topology.Snapshotter) is the test's assembler, so the root's and the
// Aggregator's Descriptors are the node's, with the PartsList providers
// the node installs (node/root.go attachPartsListProviders).
package endpoint_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/endpoint/endpointtest"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
)

const (
	dsDtOnOffLight       = 0x0100
	dsDtOnOffLightSwitch = 0x0103
	dsDtTempSensor       = 0x0302
	dsDtBridgedNode      = 0x0013
	dsDtAggregator       = 0x000E
)

// dsSource is an endpoint source of a device type serving no cluster of
// its own; the assembler mounts the Descriptor and the bridged surface.
type dsSource struct{ dt uint16 }

func (s dsSource) MatterDeviceType() uint16                       { return s.dt }
func (s dsSource) MatterClusterServers() []contract.ClusterServer { return nil }

// dsTree is a fabrictest device serving the specs the test assembles.
type dsTree struct {
	t     *testing.T
	store *endpointtest.FakeStore
	a     *endpoint.Assembler
	dev   *fabrictest.Device

	mu    sync.Mutex
	specs []endpoint.Spec
}

func newDSTree(t *testing.T) *dsTree {
	t.Helper()
	st := endpointtest.NewFakeStore()
	a, err := endpoint.New(st, endpointtest.AssemblerConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	return &dsTree{t: t, store: st, a: a}
}

func (d *dsTree) snapshot(ctx context.Context) (*endpoint.Topology, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.a.Assemble(ctx, []endpoint.Snapshot{{Scope: "s", Endpoints: slices.Clone(d.specs)}})
}

// number pins the endpoint number of key, a matter.js manual ID: the
// assembler reuses the number its store holds for a stable key.
func (d *dsTree) number(key string, deviceType, n uint16) {
	d.t.Helper()
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, err := d.store.UpsertEndpointAssigning(context.Background(), endpoint.Record{
		Key: endpoint.StringKey(key), Scope: "s", DeviceType: deviceType, EndpointID: n,
	}); err != nil {
		d.t.Fatal(err)
	}
}

// assemble serves specs: the device's first topology, or a reassembly of
// the running one.
func (d *dsTree) assemble(specs ...endpoint.Spec) *endpoint.Topology {
	d.t.Helper()
	d.mu.Lock()
	d.specs = specs
	d.mu.Unlock()
	if d.dev == nil {
		site := fabrictest.NewSite(d.t, fabrictest.Options{})
		d.dev = site.AddDevice(fabrictest.Topology{Snapshotter: d.snapshot})
	} else if err := d.dev.Bridge().Reassemble(context.Background()); err != nil {
		d.t.Fatal(err)
	}
	return d.dev.Bridge().Topology()
}

// dsSpec is an endpoint of deviceType with parts.
func dsSpec(key string, deviceType uint16, parts ...endpoint.Spec) endpoint.Spec {
	return endpoint.Spec{
		StableKey: endpoint.StringKey(key), DeviceType: deviceType, FriendlyName: key,
		Source: dsSource{dt: deviceType}, Parts: parts,
	}
}

// idOf is the number the assembly gave key.
func idOf(t *testing.T, topo *endpoint.Topology, key string) uint16 {
	t.Helper()
	for _, ep := range topo.Endpoints {
		if ep.SourceKey != nil && ep.SourceKey.String() == endpoint.StringKey(key).String() {
			return ep.ID
		}
	}
	t.Fatalf("no endpoint %q", key)
	return 0
}

// partsListOf reads the endpoint's Descriptor PartsList through the
// dispatcher, as a controller's read reaches it.
func partsListOf(t *testing.T, topo *endpoint.Topology, ep uint16) []uint16 {
	t.Helper()
	res := endpoint.NewTopologyDispatcher(topo).Read(context.Background(), im.ConcreteAttributePath{
		Endpoint: ep, Cluster: 0x001D, Attribute: 0x0003, HasEndpoint: true, HasCluster: true, HasAttribute: true,
	})
	if len(res) != 1 || res[0].Status != im.StatusSuccess {
		t.Fatalf("PartsList of endpoint %d: %+v", ep, res)
	}
	l, ok := res[0].Value.Value.([]uint16)
	if !ok {
		t.Fatalf("PartsList of endpoint %d is %T", ep, res[0].Value.Value)
	}
	return l
}

func expectPartsList(t *testing.T, topo *endpoint.Topology, ep uint16, want ...uint16) {
	t.Helper()
	if want == nil {
		want = []uint16{}
	}
	if got := partsListOf(t, topo, ep); !slices.Equal(got, want) {
		t.Errorf("endpoint %d PartsList = %v, want %v", ep, got, want)
	}
}

// TestDescriptorServerStopsWatchingARemovedPart ports "stops watching a
// removed part" (DescriptorServerTest.ts:133-144): parent 1 with child 2,
// the child closed, the parent's parts list empty and the child's destroyed
// event no longer observed.
//
// The Go PartsList is the topology's: a removed
// part is gone from it, and no observer of the part outlives the assembly
// it was in, so the Go assertion is the PartsList and the part's absence
// from the topology.
func TestDescriptorServerStopsWatchingARemovedPart(t *testing.T) {
	t.Parallel()
	d := newDSTree(t)
	topo := d.assemble(dsSpec("parent", dsDtOnOffLight, dsSpec("child", dsDtOnOffLight)))
	parent, child := idOf(t, topo, "parent"), idOf(t, topo, "child")
	expectPartsList(t, topo, parent, child)

	topo = d.assemble(dsSpec("parent", dsDtOnOffLight))

	expectPartsList(t, topo, parent)
	if topo.FindByID(child) != nil {
		t.Errorf("the closed child %d is still in the topology", child)
	}
}

// dsTag is tag (DescriptorServerTest.ts:189-191): namespace 7, no
// manufacturer code, the label undefined (nil), null or a string.
func dsTag(tag uint8, label ...*string) core.SemanticTag {
	t := core.SemanticTag{MfgCode: spec.Nullable[uint16]{Null: true}, NamespaceId: 7, Tag: tag}
	if len(label) > 0 {
		if label[0] == nil {
			t.Label = &spec.Nullable[string]{Null: true}
		} else {
			t.Label = &spec.Nullable[string]{Value: *label[0]}
		}
	}
	return t
}

func dsLabel(s string) *string { return &s }

// dsTagLabel is one tagsOf entry: the tag and its label, "" for undefined.
type dsTagLabel struct {
	tag   uint8
	label string
}

func tagsOf(t *testing.T, d *core.Descriptor) []dsTagLabel {
	t.Helper()
	v, ok := d.MatterRead(0x0004)
	if !ok {
		t.Fatal("TagList not served")
	}
	tags, _ := v.([]core.SemanticTag)
	out := make([]dsTagLabel, 0, len(tags))
	for _, tg := range tags {
		l := ""
		if tg.Label != nil && !tg.Label.Null {
			l = tg.Label.Value
		}
		out = append(out, dsTagLabel{tg.Tag, l})
	}
	return out
}

// TestDescriptorServerAddTags ports the four tag-edit cases of "addTags"
// (DescriptorServerTest.ts:186-238) on a Descriptor with the TagList
// feature and a TagList of tag(1), createTagged (:193-195). "requires the
// TagList feature" is cluster/core:TestParityMatterJS_DescriptorServer_TagListGatedByFeatureMap;
// here AddTags without the feature also fails with ErrTagListNotEnabled.
func TestDescriptorServerAddTags(t *testing.T) {
	t.Parallel()
	createTagged := func(t *testing.T) *core.Descriptor {
		t.Helper()
		d, err := core.NewDescriptor([]core.DeviceTypeStruct{{DeviceType: 1, Revision: 1}}, nil, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		d.EnableTagList(dsTag(1))
		return d
	}
	addTags := func(t *testing.T, d *core.Descriptor, tags ...core.SemanticTag) {
		t.Helper()
		if err := d.AddTags(tags...); err != nil {
			t.Fatal(err)
		}
	}
	expectTags := func(t *testing.T, d *core.Descriptor, want ...dsTagLabel) {
		t.Helper()
		if got := tagsOf(t, d); !slices.Equal(got, want) {
			t.Errorf("TagList = %v, want %v", got, want)
		}
	}

	t.Run("adds a tag that is already listed only once", func(t *testing.T) {
		t.Parallel()
		d := createTagged(t)
		addTags(t, d, dsTag(1), dsTag(2), dsTag(2))
		expectTags(t, d, dsTagLabel{1, ""}, dsTagLabel{2, ""})
	})
	t.Run("does not add a listed tag again when the label is the same", func(t *testing.T) {
		t.Parallel()
		d := createTagged(t)
		addTags(t, d, dsTag(1, dsLabel("Kitchen")))
		addTags(t, d, dsTag(1, dsLabel("Kitchen")))
		expectTags(t, d, dsTagLabel{1, "Kitchen"})
	})
	t.Run("replaces the label of a listed tag", func(t *testing.T) {
		t.Parallel()
		d := createTagged(t)
		addTags(t, d, dsTag(1, dsLabel("Kitchen")))
		addTags(t, d, dsTag(1, dsLabel("Bedroom")))
		expectTags(t, d, dsTagLabel{1, "Bedroom"})
	})
	t.Run("keeps the label when a listed tag is added without one", func(t *testing.T) {
		t.Parallel()
		d := createTagged(t)
		addTags(t, d, dsTag(1, dsLabel("Kitchen")))
		addTags(t, d, dsTag(1))
		addTags(t, d, dsTag(1, nil))
		expectTags(t, d, dsTagLabel{1, "Kitchen"})
	})
	t.Run("requires the TagList feature", func(t *testing.T) {
		t.Parallel()
		d, err := core.NewDescriptor([]core.DeviceTypeStruct{{DeviceType: 1, Revision: 1}}, nil, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := d.AddTags(dsTag(1)); !errors.Is(err, core.ErrTagListNotEnabled) {
			t.Errorf("AddTags without the feature = %v, want ErrTagListNotEnabled", err)
		}
		if fm, _ := createTagged(t).MatterRead(0xFFFC); fm != uint32(1) {
			t.Errorf("FeatureMap with TagList = %v, want TAGLIST (1)", fm)
		}
	})
}

// TestDescriptorServerComposition ports the three cases of "composition"
// (DescriptorServerTest.ts:248-350): an aggregator (1) over a bridged node
// (2) over a light (3) over a light (4).
func TestDescriptorServerComposition(t *testing.T) {
	t.Parallel()

	// :249-321 — the Aggregator and the bridged node list every
	// descendant, the light its child.
	t.Run("lists every descendant for an aggregator and a bridged node and only children for a light", func(t *testing.T) {
		t.Parallel()
		d := newDSTree(t)
		topo := d.assemble(dsSpec("bridged", dsDtBridgedNode, dsSpec("light", dsDtOnOffLight, dsSpec("sublight", dsDtOnOffLight))))
		bridged, light, sub := idOf(t, topo, "bridged"), idOf(t, topo, "light"), idOf(t, topo, "sublight")
		expectPartsList(t, topo, 1, bridged, light, sub)
		expectPartsList(t, topo, bridged, light, sub)
		expectPartsList(t, topo, light, sub)
	})

	// The aggregator of :323-350, whose device type list changes, is the
	// part "composer" here: under it a bridged-node part over a light over
	// a light, as in createBridgedNodeTree (:284-321).
	tree := func(composer uint16) endpoint.Spec {
		return dsSpec("host", dsDtOnOffLight,
			dsSpec("composer", composer,
				dsSpec("node", dsDtBridgedNode, dsSpec("light", dsDtOnOffLight, dsSpec("sublight", dsDtOnOffLight)))))
	}

	// :323-336
	t.Run("lists every descendant once a full-family device type is added at runtime", func(t *testing.T) {
		t.Parallel()
		d := newDSTree(t)
		topo := d.assemble(tree(dsDtOnOffLight))
		composer, node := idOf(t, topo, "composer"), idOf(t, topo, "node")
		light, sub := idOf(t, topo, "light"), idOf(t, topo, "sublight")
		expectPartsList(t, topo, composer, node)

		topo = d.assemble(tree(dsDtAggregator))

		expectPartsList(t, topo, composer, node, light, sub)
	})

	// :338-349
	t.Run("lists only children once the full-family device type is replaced at runtime", func(t *testing.T) {
		t.Parallel()
		d := newDSTree(t)
		topo := d.assemble(tree(dsDtAggregator))
		composer, node := idOf(t, topo, "composer"), idOf(t, topo, "node")
		light, sub := idOf(t, topo, "light"), idOf(t, topo, "sublight")
		expectPartsList(t, topo, composer, node, light, sub)

		topo = d.assemble(tree(dsDtOnOffLight))

		expectPartsList(t, topo, composer, node)
	})
}

// TestDescriptorServerIndexedHierarchy ports "adds parts automatically with
// indexed grandparent and parent" (DescriptorServerTest.ts:352-520): the
// root (the grandparent) lists the Aggregator (the parent) and its child,
// the Aggregator its child, plus any additional child
// (expectFullPartsLists, :353-367).
func TestDescriptorServerIndexedHierarchy(t *testing.T) {
	t.Parallel()
	expectFull := func(t *testing.T, topo *endpoint.Topology, children ...uint16) {
		t.Helper()
		slices.Sort(children)
		expectPartsList(t, topo, 0, append([]uint16{1}, children...)...)
		expectPartsList(t, topo, 1, children...)
	}
	child := func() endpoint.Spec { return dsSpec("child", dsDtOnOffLight) }

	t.Run("when constructed with full hierarchy (auto ID)", func(t *testing.T) {
		t.Parallel()
		topo := newDSTree(t).assemble(child())
		expectFull(t, topo, idOf(t, topo, "child"))
	})
	t.Run("when constructed with full hierarchy (manual ID)", func(t *testing.T) {
		t.Parallel()
		d := newDSTree(t)
		d.number("child", dsDtOnOffLight, 2)
		expectFull(t, d.assemble(child()), 2)
	})
	// The child exists before its parent is installed: the first assembly
	// carries it.
	t.Run("when child is added before parent (auto ID)", func(t *testing.T) {
		t.Parallel()
		topo := newDSTree(t).assemble(child())
		expectFull(t, topo, idOf(t, topo, "child"))
	})
	t.Run("when child is added before parent (manual ID)", func(t *testing.T) {
		t.Parallel()
		d := newDSTree(t)
		d.number("child", dsDtOnOffLight, 2)
		expectFull(t, d.assemble(child()), 2)
	})
	// The parent is installed first: an assembly without the child, then
	// one with it.
	t.Run("when parent is added before child (auto ID)", func(t *testing.T) {
		t.Parallel()
		d := newDSTree(t)
		expectFull(t, d.assemble())
		topo := d.assemble(child())
		expectFull(t, topo, idOf(t, topo, "child"))
	})
	t.Run("when parent is added before child (manual ID)", func(t *testing.T) {
		t.Parallel()
		d := newDSTree(t)
		d.number("child", dsDtOnOffLight, 2)
		expectFull(t, d.assemble())
		expectFull(t, d.assemble(child()), 2)
	})
	t.Run("when additional child is added (auto ID)", func(t *testing.T) {
		t.Parallel()
		d := newDSTree(t)
		topo := d.assemble(child())
		first := idOf(t, topo, "child")
		expectFull(t, topo, first)
		topo = d.assemble(child(), dsSpec("secondChild", dsDtOnOffLightSwitch))
		expectFull(t, topo, first, idOf(t, topo, "secondChild"))
	})
	t.Run("when additional child is added (manual ID)", func(t *testing.T) {
		t.Parallel()
		d := newDSTree(t)
		topo := d.assemble(child())
		first := idOf(t, topo, "child")
		expectFull(t, topo, first)
		d.number("secondChild", dsDtOnOffLightSwitch, 3)
		expectFull(t, d.assemble(child(), dsSpec("secondChild", dsDtOnOffLightSwitch)), first, 3)
	})
}

// TestDescriptorServerMembershipChangesThatKeepTheCount ports "updates
// ${name}" (DescriptorServerTest.ts:522-635): of two children 2 and 3, 3
// is closed and 4 added; the parent settles on [2, 4], the root on the
// parent and [2, 4]. matter.js runs four timing windows (the replacement
// added 14, 20, 29 or 40 microtasks into the close, while the closed child
// is still a part or after); a Go assembly applies the close and the add in
// one step, so the two parent types are the cases: the Aggregator (endpoint
// 1, as matter.js's parent; the root settles on [1, 2, 4]) and a composed
// parent, a part listing its children (the root lists it and the
// Aggregator besides).
func TestDescriptorServerMembershipChangesThatKeepTheCount(t *testing.T) {
	t.Parallel()
	sensor := func(key string) endpoint.Spec { return dsSpec(key, dsDtTempSensor) }

	t.Run("updates an aggregator", func(t *testing.T) {
		t.Parallel()
		d := newDSTree(t)
		d.number("c1", dsDtTempSensor, 2)
		d.number("c2", dsDtTempSensor, 3)
		expectPartsList(t, d.assemble(sensor("c1"), sensor("c2")), 1, 2, 3)
		// The replacement's number, pinned once the assembly before it can
		// no longer collect it as a vanished source.
		d.number("c3", dsDtTempSensor, 4)

		topo := d.assemble(sensor("c1"), sensor("c3"))

		expectPartsList(t, topo, 1, 2, 4)
		expectPartsList(t, topo, 0, 1, 2, 4)
	})

	t.Run("updates a composed parent", func(t *testing.T) {
		t.Parallel()
		d := newDSTree(t)
		d.number("host", dsDtOnOffLight, 5)
		d.number("parent", dsDtOnOffLight, 6)
		d.number("c1", dsDtTempSensor, 2)
		d.number("c2", dsDtTempSensor, 3)
		composed := func(children ...endpoint.Spec) endpoint.Spec {
			return dsSpec("host", dsDtOnOffLight, dsSpec("parent", dsDtOnOffLight, children...))
		}
		expectPartsList(t, d.assemble(composed(sensor("c1"), sensor("c2"))), 6, 2, 3)
		d.number("c3", dsDtTempSensor, 4)

		topo := d.assemble(composed(sensor("c1"), sensor("c3")))

		expectPartsList(t, topo, 6, 2, 4)
		expectPartsList(t, topo, 0, 1, 2, 4, 5, 6)
	})
}

// TestDescriptorServerOrdersPartsListNumerically ports "orders PartsList
// numerically" (DescriptorServerTest.ts:637-647): lights numbered 10, 9, 8,
// 6, 4, 2 and 1 added in that order list [1, 2, 4, 6, 8, 9, 10] on the root.
// Endpoint 1 is the Go Aggregator, so the lights take the six other
// numbers, added in matter.js's descending order.
func TestDescriptorServerOrdersPartsListNumerically(t *testing.T) {
	t.Parallel()
	d := newDSTree(t)
	specs := make([]endpoint.Spec, 0, 6)
	for _, n := range []uint16{10, 9, 8, 6, 4, 2} {
		key := "light" + string(rune('0'+n%10)) + string(rune('0'+n/10))
		d.number(key, dsDtOnOffLight, n)
		specs = append(specs, dsSpec(key, dsDtOnOffLight))
	}
	topo := d.assemble(specs...)
	expectPartsList(t, topo, 0, 1, 2, 4, 6, 8, 9, 10)
	expectPartsList(t, topo, 1, 2, 4, 6, 8, 9, 10)
}
