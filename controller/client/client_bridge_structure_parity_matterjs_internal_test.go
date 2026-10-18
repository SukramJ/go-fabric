// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/node/ClientBridgeStructureTest.ts
// at 862ad75f, the cases that feed the client structure Descriptor reports:
// how the controller builds the tree of a bridge peer — across two
// interactions, with a root that omits an endpoint, with lists that change,
// with an aggregator below an aggregator, with bridged devices of several
// endpoints and claimants to rank. matter.js mutates the structure of a
// commissioned peer whose device is a bare root; the port applies the same
// reports to a node whose image holds that root (Node.apply), one apply per
// interaction. The case over a real bridge is in
// client_bridge_structure_parity_matterjs_test.go.
package client

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/cluster/spec/descriptor"
	"github.com/SukramJ/go-fabric/controller/interaction"
)

// The endpoints and device types of ClientBridgeStructureTest.ts.
const (
	bsAggregator   = 20
	bsComposed     = 21
	dtTemperature  = 0x0302
	dtPowerSource  = 0x0011
	dtDimmable     = 0x0101
	dtRefrigerator = 0x0070
	dtCabinet      = 0x0071
	dtHumidity     = 0x0307
	dtLight        = 0x0100
)

var bsSensors = []uint16{22, 23}

// dtRev is a DeviceTypeList entry.
type dtRev struct{ dt, rev uint64 }

// deviceTypeList is a DeviceTypeList of entries.
func deviceTypeList(entries ...dtRev) spec.Node {
	n := listNode()
	for _, e := range entries {
		n.Children = append(n.Children, structNode(e.dt, e.rev))
	}
	return n
}

// descriptorAttr is ClientBridgeStructureTest.ts:137-150: one Descriptor
// attribute of an endpoint.
func descriptorAttr(ep uint16, attr uint32, v spec.Node, version uint32) *report {
	return (&report{}).attr(ep, descriptor.ClusterID, attr, v, version)
}

// partsAttr is descriptorAttr of a PartsList.
func partsAttr(ep uint16, version uint32, parts ...uint16) *report {
	return descriptorAttr(ep, descriptor.AttrPartsList, listNode(ids16(parts...)...), version)
}

// bsDescriptor is ClientBridgeStructureTest.ts:152-172 descriptorReports:
// a whole Descriptor of an endpoint.
func bsDescriptor(ep uint16, dts []dtRev, parts []uint16, version uint32) *report {
	b := &report{}
	attr := func(id uint32, v spec.Node) { b.attr(ep, descriptor.ClusterID, id, v, version) }
	attr(spec.AttrClusterRevision, uintNode(3))
	attr(spec.AttrFeatureMap, uintNode(0))
	attr(spec.AttrAttributeList, listNode(0, 1, 2, 3, 65528, 65529, 65531, 65532, 65533))
	attr(spec.AttrAcceptedCommandList, listNode())
	attr(spec.AttrGeneratedCommandList, listNode())
	attr(descriptor.AttrDeviceTypeList, deviceTypeList(dts...))
	attr(descriptor.AttrServerList, listNode(uint64(descriptor.ClusterID)))
	attr(descriptor.AttrClientList, listNode())
	attr(descriptor.AttrPartsList, listNode(ids16(parts...)...))
	return b
}

// one is a DeviceTypeList of one entry.
func one(dt, rev uint64) []dtRev { return []dtRev{{dt, rev}} }

// rev1 is ClientBridgeStructureTest.ts:1078-1080 deviceTypes: each at revision 1.
func rev1(dts ...uint64) []dtRev {
	out := make([]dtRev, 0, len(dts))
	for _, d := range dts {
		out = append(out, dtRev{d, 1})
	}
	return out
}

// oneInteraction is one interaction's reports, chunks in order.
func oneInteraction(chunks ...*report) *report {
	out := &report{}
	for _, c := range chunks {
		out.r.Attributes = append(out.r.Attributes, c.r.Attributes...)
	}
	return out
}

// bridgePeer is ClientBridgeStructureTest.ts peerOf: the image of a
// commissioned peer whose device is a bare root.
func bridgePeer(t *testing.T) (*Node, *recorder, *warnRecorder) {
	t.Helper()
	rec, logs := &recorder{}, &warnRecorder{}
	n, err := New(Config{ID: "n", Store: &MemoryStore{}, Interaction: interaction.Bind(nil, interaction.Config{}), Hooks: rec.hooks(), Logger: slog.New(logs)})
	if err != nil {
		t.Fatal(err)
	}
	b := &report{}
	b.endpoint(0, []uint64{dtRootNode}, []uint32{descriptor.ClusterID, clBasicInfo})
	n.apply(wildcard, &b.r)
	rec.take()
	return n, rec, logs
}

// treeOf is ClientBridgeStructureTest.ts treeOf: the depth-first paths of
// the parts.
func treeOf(n *Node) []string {
	var out []string
	var walk func(ep *Endpoint, path string)
	walk = func(ep *Endpoint, path string) {
		for _, p := range ep.Parts() {
			s := fmt.Sprintf("%s/ep%d", path, p.Number())
			out = append(out, s)
			walk(p, s)
		}
	}
	walk(n.Endpoint(0), "")
	return out
}

// parentsOf is ClientBridgeStructureTest.ts:1097-1105.
func parentsOf(n *Node) map[uint16]uint16 {
	out := map[uint16]uint16{}
	var walk func(ep *Endpoint)
	walk = func(ep *Endpoint) {
		for _, p := range ep.Parts() {
			out[p.Number()] = ep.Number()
			walk(p)
		}
	}
	walk(n.Endpoint(0))
	return out
}

// warnRecorder keeps the WARN records with their attributes.
type warnRecorder struct {
	mu   sync.Mutex
	recs []slog.Record
}

func (h *warnRecorder) Enabled(_ context.Context, l slog.Level) bool { return l >= slog.LevelWarn }
func (h *warnRecorder) WithAttrs([]slog.Attr) slog.Handler           { return h }
func (h *warnRecorder) WithGroup(string) slog.Handler                { return h }
func (h *warnRecorder) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	h.recs = append(h.recs, r.Clone())
	h.mu.Unlock()
	return nil
}

// severalParents are the several_parents warnings, their attributes as text.
func (h *warnRecorder) severalParents() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []string
	for _, r := range h.recs {
		if r.Message != "matter.controller.client.several_parents" {
			continue
		}
		s := ""
		r.Attrs(func(a slog.Attr) bool {
			s += a.Key + "=" + a.Value.String() + " "
			return true
		})
		out = append(out, s)
	}
	return out
}

func treeIs(t *testing.T, n *Node, want ...string) {
	t.Helper()
	if got := treeOf(n); !slices.Equal(got, want) {
		t.Fatalf("tree %v, want %v", got, want)
	}
}

func parentsAre(t *testing.T, n *Node, want map[uint16]uint16) {
	t.Helper()
	if got := parentsOf(n); !maps.Equal(got, want) {
		t.Fatalf("parents %v, want %v", got, want)
	}
}

func TestClientBridgeStructureTwoInteractionsParity(t *testing.T) {
	t.Parallel()
	all := append([]uint16{bsAggregator, bsComposed}, bsSensors...)
	sensors := func(version uint32) []*report {
		out := []*report{}
		for _, s := range bsSensors {
			out = append(out, bsDescriptor(s, one(dtTemperature, 2), nil, version))
		}
		return out
	}

	t.Run("waits for the lists that say who owns what rather than reading the root's as parenthood", func(t *testing.T) {
		t.Parallel()
		n, _, _ := bridgePeer(t)
		n.apply(wildcard, &partsAttr(0, 10, all...).r)
		n.apply(wildcard, &oneInteraction(append([]*report{
			bsDescriptor(bsAggregator, one(dtAggregator, 3), append([]uint16{bsComposed}, bsSensors...), 11),
			bsDescriptor(bsComposed, one(dtBridged, 1), bsSensors, 11),
		}, sensors(11)...)...).r)
		treeIs(t, n, "/ep20", "/ep20/ep21", "/ep20/ep21/ep22", "/ep20/ep21/ep23")
	})

	t.Run("does not install a part the root has stopped naming while its owner was unknown", func(t *testing.T) {
		t.Parallel()
		n, _, _ := bridgePeer(t)
		n.apply(wildcard, &partsAttr(0, 10, all...).r)
		kept, dropped := bsSensors[0], bsSensors[1]
		n.apply(wildcard, &oneInteraction(
			partsAttr(0, 11, bsAggregator, bsComposed, kept),
			bsDescriptor(bsAggregator, one(dtAggregator, 3), []uint16{bsComposed, kept}, 11),
			bsDescriptor(bsComposed, one(dtBridged, 1), []uint16{kept, dropped}, 11),
			bsDescriptor(kept, one(dtTemperature, 2), nil, 11),
			bsDescriptor(dropped, one(dtTemperature, 2), nil, 11),
		).r)
		treeIs(t, n, "/ep20", "/ep20/ep21", "/ep20/ep21/ep22")
	})

	t.Run("takes the device types of an endpoint that has only utility ones", func(t *testing.T) {
		t.Parallel()
		n, _, _ := bridgePeer(t)
		n.apply(wildcard, &partsAttr(0, 10, bsComposed).r)
		n.apply(wildcard, &bsDescriptor(bsComposed, []dtRev{{dtBridged, 1}, {dtPowerSource, 1}}, nil, 11).r)
		if ep := n.Endpoint(bsComposed); ep == nil || ep.DeviceType() != dtBridged {
			t.Fatalf("composed endpoint %v", treeOf(n))
		}
	})

	t.Run("installs nothing from a full-family list alone", func(t *testing.T) {
		t.Parallel()
		n, _, _ := bridgePeer(t)
		n.apply(wildcard, &partsAttr(0, 10, all...).r)
		treeIs(t, n)
	})
}

func TestClientBridgeStructureRootOmitsParity(t *testing.T) {
	t.Parallel()
	plugs := []uint16{2, 3}
	reports := func(rootFirst bool) *report {
		root := partsAttr(0, 10, plugs...)
		agg := bsDescriptor(1, one(dtAggregator, 1), plugs, 10)
		ps := []*report{bsDescriptor(2, one(dtPlug, 3), nil, 10), bsDescriptor(3, one(dtPlug, 3), nil, 10)}
		if rootFirst {
			return oneInteraction(append([]*report{root, agg}, ps...)...)
		}
		return oneInteraction(append([]*report{agg, root}, ps...)...)
	}
	for _, c := range []struct {
		name      string
		rootFirst bool
	}{
		{"leaves it out, and keeps the endpoints it claims", true},
		{"does the same when it reports itself before the root", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			n, _, _ := bridgePeer(t)
			n.apply(wildcard, &reports(c.rootFirst).r)
			treeIs(t, n, "/ep2", "/ep3")
		})
	}
}

func TestClientBridgeStructureChangesParity(t *testing.T) {
	t.Parallel()
	sensors := func(version uint32, numbers ...uint16) []*report {
		out := []*report{}
		for _, s := range numbers {
			out = append(out, bsDescriptor(s, one(dtTemperature, 2), nil, version))
		}
		return out
	}

	t.Run("keeps an endpoint the root stopped naming while it holds endpoints the root names", func(t *testing.T) {
		t.Parallel()
		n, _, _ := bridgePeer(t)
		n.apply(wildcard, &oneInteraction(append([]*report{
			partsAttr(0, 10, append([]uint16{bsAggregator}, bsSensors...)...),
			bsDescriptor(bsAggregator, one(dtAggregator, 3), bsSensors, 10),
		}, sensors(10, bsSensors...)...)...).r)
		treeIs(t, n, "/ep20", "/ep20/ep22", "/ep20/ep23")
		n.apply(wildcard, &partsAttr(0, 11, bsSensors...).r)
		treeIs(t, n, "/ep20", "/ep20/ep22", "/ep20/ep23")
		n.apply(wildcard, &partsAttr(0, 12).r)
		treeIs(t, n)
	})

	t.Run("places an aggregator's devices in the same interaction when they have lower numbers", func(t *testing.T) {
		t.Parallel()
		n, _, _ := bridgePeer(t)
		n.apply(wildcard, &oneInteraction(append([]*report{
			partsAttr(0, 10, 40, 41, 50),
			bsDescriptor(50, one(dtAggregator, 3), []uint16{40, 41}, 10),
		}, sensors(10, 40, 41)...)...).r)
		treeIs(t, n, "/ep50", "/ep50/ep40", "/ep50/ep41")
	})

	t.Run("waits for a claimant's device types before reading its list as parenthood", func(t *testing.T) {
		t.Parallel()
		n, _, _ := bridgePeer(t)
		n.apply(wildcard, &oneInteraction(append([]*report{
			partsAttr(0, 10, bsAggregator, bsComposed, 22, 23),
			partsAttr(bsAggregator, 10, bsComposed, 22, 23),
			partsAttr(bsComposed, 10, bsSensors...),
		}, sensors(10, bsSensors...)...)...).r)
		treeIs(t, n, "/ep20")
		n.apply(wildcard, &oneInteraction(
			bsDescriptor(bsAggregator, one(dtAggregator, 3), []uint16{bsComposed, 22, 23}, 11),
			bsDescriptor(bsComposed, one(dtBridged, 1), bsSensors, 11),
		).r)
		treeIs(t, n, "/ep20", "/ep20/ep21", "/ep20/ep21/ep22", "/ep20/ep21/ep23")
	})

	t.Run("reads a full-family device type that follows an application one", func(t *testing.T) {
		t.Parallel()
		n, _, _ := bridgePeer(t)
		n.apply(wildcard, &oneInteraction(append([]*report{
			partsAttr(0, 10, bsAggregator, bsComposed, 22, 23),
			bsDescriptor(bsAggregator, []dtRev{{dtOnOffLight, 3}, {dtAggregator, 3}}, []uint16{bsComposed, 22, 23}, 10),
			bsDescriptor(bsComposed, one(dtBridged, 1), bsSensors, 10),
		}, sensors(10, bsSensors...)...)...).r)
		treeIs(t, n, "/ep20", "/ep20/ep21", "/ep20/ep21/ep22", "/ep20/ep21/ep23")
	})

	t.Run("does not let an endpoint keep a claim its own list has dropped", func(t *testing.T) {
		t.Parallel()
		n, _, _ := bridgePeer(t)
		kept, moved := bsSensors[0], bsSensors[1]
		n.apply(wildcard, &oneInteraction(append([]*report{
			partsAttr(0, 10, bsComposed, 22, 23),
			partsAttr(bsComposed, 10, bsSensors...),
		}, sensors(10, bsSensors...)...)...).r)
		treeIs(t, n, "/ep21")
		n.apply(wildcard, &bsDescriptor(bsComposed, one(dtBridged, 1), []uint16{kept}, 11).r)
		parentsAre(t, n, map[uint16]uint16{bsComposed: 0, kept: bsComposed, moved: 0})
	})
}

// The nested bridge of ClientBridgeStructureTest.ts:706-818.
const (
	zigbeeAggregator = 11
	daliAggregator   = 14
	zwaveAggregator  = 31
)

var (
	zigbeeLights = []uint16{12, 13}
	daliLights   = []uint16{21, 22, 23}
	zwaveLights  = []uint16{32, 33}
	bridgedLight = []dtRev{{dtBridged, 3}, {dtDimmable, 3}}
	nestedTree   = []string{
		"/ep11", "/ep11/ep12", "/ep11/ep13", "/ep11/ep14", "/ep11/ep14/ep21", "/ep11/ep14/ep22", "/ep11/ep14/ep23",
		"/ep31", "/ep31/ep32", "/ep31/ep33",
	}
	rootParts = []uint16{11, 12, 13, 14, 21, 22, 23, 31, 32, 33}
)

// nestedBridgeReports is ClientBridgeStructureTest.ts:726-776, one report
// per endpoint, the endpoint first.
func nestedBridgeReports(version uint32) []struct {
	ep uint16
	r  *report
} {
	type item = struct {
		ep uint16
		r  *report
	}
	var out []item
	out = append(out, item{zigbeeAggregator, bsDescriptor(zigbeeAggregator, one(dtAggregator, 2), append(append(slices.Clone(zigbeeLights), daliAggregator), daliLights...), version)})
	for _, l := range zigbeeLights {
		out = append(out, item{l, bsDescriptor(l, bridgedLight, nil, version)})
	}
	out = append(out, item{daliAggregator, bsDescriptor(daliAggregator, []dtRev{{dtBridged, 3}, {dtAggregator, 2}}, daliLights, version)})
	for _, l := range daliLights {
		out = append(out, item{l, bsDescriptor(l, bridgedLight, nil, version)})
	}
	out = append(out, item{zwaveAggregator, bsDescriptor(zwaveAggregator, one(dtAggregator, 2), zwaveLights, version)})
	for _, l := range zwaveLights {
		out = append(out, item{l, bsDescriptor(l, bridgedLight, nil, version)})
	}
	return out
}

// nestedInteraction is the root's PartsList and the nested reports but
// those skip names.
func nestedInteraction(skip func(ep uint16) bool, extra ...*report) *report {
	chunks := []*report{partsAttr(0, 10, rootParts...)}
	for _, it := range nestedBridgeReports(10) {
		if skip == nil || !skip(it.ep) {
			chunks = append(chunks, it.r)
		}
	}
	return oneInteraction(append(chunks, extra...)...)
}

func without(tree []string, suffixes ...string) []string {
	var out []string
	for _, p := range tree {
		drop := false
		for _, s := range suffixes {
			if len(p) >= len(s) && p[len(p)-len(s):] == s {
				drop = true
			}
		}
		if !drop {
			out = append(out, p)
		}
	}
	return out
}

func TestClientBridgeStructureNestedAggregatorParity(t *testing.T) {
	t.Parallel()

	t.Run("gives each aggregator the endpoints it alone names", func(t *testing.T) {
		t.Parallel()
		n, _, _ := bridgePeer(t)
		n.apply(wildcard, &nestedInteraction(nil).r)
		treeIs(t, n, nestedTree...)
	})

	t.Run("places the DALI lights once the nested aggregator says what it is", func(t *testing.T) {
		t.Parallel()
		n, _, _ := bridgePeer(t)
		n.apply(wildcard, &nestedInteraction(func(ep uint16) bool { return ep == daliAggregator }, partsAttr(daliAggregator, 10, daliLights...)).r)
		treeIs(t, n, without(nestedTree, "/ep21", "/ep22", "/ep23")...)
		for _, it := range nestedBridgeReports(10) {
			if it.ep == daliAggregator {
				n.apply(wildcard, &it.r.r)
			}
		}
		treeIs(t, n, nestedTree...)
	})

	silent := daliLights[len(daliLights)-1]

	t.Run("waits for a silent DALI light before placing any of the bridge", func(t *testing.T) {
		t.Parallel()
		n, _, _ := bridgePeer(t)
		n.apply(wildcard, &nestedInteraction(func(ep uint16) bool { return ep == silent }).r)
		treeIs(t, n)
		n.apply(wildcard, &bsDescriptor(silent, bridgedLight, nil, 10).r)
		treeIs(t, n, nestedTree...)
	})

	t.Run("announces no endpoint below an aggregator that is not on the node yet", func(t *testing.T) {
		t.Parallel()
		n, _, _ := bridgePeer(t)
		var mu sync.Mutex
		var announced, detached []uint16
		// endpointInstalled(BridgedNodeEndpoint): an endpoint whose
		// DeviceTypeList names BridgedNode.
		n.cfg.Hooks.EndpointInstalled = func(ep *Endpoint) {
			c := ep.Cluster(descriptor.ClusterID)
			if c == nil {
				return
			}
			v, _ := c.Value(descriptor.AttrDeviceTypeList)
			bridged := false
			for _, e := range v.Children {
				for _, f := range e.Children {
					bridged = bridged || (f.El.Tag.Number == 0 && f.El.Uint == dtBridged)
				}
			}
			if !bridged {
				return
			}
			mu.Lock()
			defer mu.Unlock()
			announced = append(announced, ep.Number())
			a := ep
			for a != nil && a.Number() != 0 {
				a = a.Owner()
			}
			if a == nil {
				detached = append(detached, ep.Number())
			}
		}
		n.apply(wildcard, &nestedInteraction(func(ep uint16) bool { return ep == silent }).r)
		mu.Lock()
		if len(announced) != 0 {
			t.Fatalf("announced %v while an endpoint said nothing", announced)
		}
		mu.Unlock()
		n.apply(wildcard, &bsDescriptor(silent, bridgedLight, nil, 10).r)
		mu.Lock()
		defer mu.Unlock()
		slices.Sort(announced)
		if len(detached) != 0 || !slices.Equal(slices.Compact(announced), []uint16{12, 13, 14, 21, 22, 23, 32, 33}) {
			t.Fatalf("announced %v, detached %v", announced, detached)
		}
	})

	t.Run("takes the aggregator device type of an endpoint that is also a bridged node", func(t *testing.T) {
		t.Parallel()
		n, _, _ := bridgePeer(t)
		n.apply(wildcard, &nestedInteraction(nil).r)
		if dali := n.Endpoint(daliAggregator); dali == nil || dali.DeviceType() != dtAggregator || dali.Owner() == nil || dali.Owner().Number() != zigbeeAggregator {
			t.Fatalf("DALI aggregator %v", treeOf(n))
		}
	})

	t.Run("keeps an endpoint the nested aggregator drops while the peer still names it", func(t *testing.T) {
		t.Parallel()
		n, _, _ := bridgePeer(t)
		n.apply(wildcard, &nestedInteraction(nil).r)
		treeIs(t, n, nestedTree...)
		dropped, kept := daliLights[0], daliLights[1:]
		n.apply(wildcard, &partsAttr(daliAggregator, 11, kept...).r)
		treeIs(t, n, nestedTree...)
		root := slices.DeleteFunc(slices.Clone(rootParts), func(e uint16) bool { return e == dropped })
		n.apply(wildcard, &oneInteraction(
			partsAttr(0, 12, root...),
			partsAttr(zigbeeAggregator, 12, append(append(slices.Clone(zigbeeLights), daliAggregator), kept...)...),
		).r)
		treeIs(t, n, without(nestedTree, "/ep21")...)
	})

	t.Run("drops an endpoint the root stops naming while the nested aggregator still does", func(t *testing.T) {
		t.Parallel()
		n, _, _ := bridgePeer(t)
		n.apply(wildcard, &nestedInteraction(nil).r)
		dropped := daliLights[0]
		n.apply(wildcard, &partsAttr(0, 11, slices.DeleteFunc(slices.Clone(rootParts), func(e uint16) bool { return e == dropped })...).r)
		treeIs(t, n, without(nestedTree, "/ep21")...)
	})
}

func TestClientBridgeStructureSeveralEndpointsParity(t *testing.T) {
	t.Parallel()
	parentsAfter := func(t *testing.T, chunks ...*report) (map[uint16]uint16, *warnRecorder) {
		t.Helper()
		n, _, logs := bridgePeer(t)
		n.apply(wildcard, &oneInteraction(chunks...).r)
		return parentsOf(n), logs
	}
	expect := func(t *testing.T, want map[uint16]uint16, chunks ...*report) {
		t.Helper()
		if got, _ := parentsAfter(t, chunks...); !maps.Equal(got, want) {
			t.Fatalf("parents %v, want %v", got, want)
		}
	}

	t.Run("puts a part of a bridged composed device below that device", func(t *testing.T) {
		t.Parallel()
		expect(t, map[uint16]uint16{1: 0, 41: 1, 42: 41, 43: 42},
			partsAttr(0, 10, 1, 41, 42, 43),
			bsDescriptor(1, one(dtAggregator, 2), []uint16{41, 42, 43}, 10),
			bsDescriptor(41, one(dtBridged, 3), []uint16{42, 43}, 10),
			bsDescriptor(42, one(dtRefrigerator, 2), []uint16{43}, 10),
			bsDescriptor(43, one(dtCabinet, 3), nil, 10))
	})

	t.Run("reads a bridged node that lists only its children the same way", func(t *testing.T) {
		t.Parallel()
		expect(t, map[uint16]uint16{1: 0, 41: 1, 42: 41, 43: 42},
			partsAttr(0, 10, 1, 41, 42, 43),
			bsDescriptor(1, one(dtAggregator, 2), []uint16{41, 42, 43}, 10),
			bsDescriptor(41, one(dtBridged, 3), []uint16{42}, 10),
			bsDescriptor(42, one(dtRefrigerator, 2), []uint16{43}, 10),
			bsDescriptor(43, one(dtCabinet, 3), nil, 10))
	})

	t.Run("prefers an endpoint listing its children over a bridged node as the parent", func(t *testing.T) {
		t.Parallel()
		expect(t, map[uint16]uint16{1: 0, 41: 1, 42: 1, 43: 42},
			partsAttr(0, 10, 1, 41, 42, 43),
			bsDescriptor(1, one(dtAggregator, 2), []uint16{41, 42, 43}, 10),
			bsDescriptor(41, one(dtBridged, 3), []uint16{43}, 10),
			bsDescriptor(42, one(dtLight, 3), []uint16{43}, 10),
			bsDescriptor(43, one(dtLight, 3), nil, 10))
	})

	t.Run("does not wait for an endpoint the root no longer names", func(t *testing.T) {
		t.Parallel()
		expect(t, map[uint16]uint16{1: 0, 41: 1, 42: 41},
			partsAttr(0, 10, 1, 41, 42),
			bsDescriptor(1, one(dtAggregator, 2), []uint16{41, 42}, 10),
			bsDescriptor(41, one(dtBridged, 3), []uint16{42, 43}, 10),
			bsDescriptor(42, one(dtLight, 3), nil, 10))
	})

	for _, order := range []string{"ascending", "descending"} {
		claimants := func(rs ...*report) []*report {
			if order == "descending" {
				slices.Reverse(rs)
			}
			return rs
		}
		t.Run("takes the claimant naming the fewest other claimants as the parent, reported "+order, func(t *testing.T) {
			t.Parallel()
			chunks := append([]*report{partsAttr(0, 10, 10, 11, 12, 13, 14)}, claimants(
				bsDescriptor(10, one(dtLight, 3), []uint16{11, 12}, 10),
				bsDescriptor(11, one(dtLight, 3), []uint16{12, 13, 14}, 10))...)
			for _, l := range []uint16{12, 13, 14} {
				chunks = append(chunks, bsDescriptor(l, one(dtLight, 3), nil, 10))
			}
			expect(t, map[uint16]uint16{10: 0, 11: 10, 12: 11, 13: 11, 14: 11}, chunks...)
		})
		t.Run("takes the claimant with the shorter list as the parent, reported "+order, func(t *testing.T) {
			t.Parallel()
			chunks := append([]*report{partsAttr(0, 10, 10, 11, 12, 13)}, claimants(
				bsDescriptor(10, one(dtLight, 3), []uint16{12, 13}, 10),
				bsDescriptor(11, one(dtLight, 3), []uint16{12}, 10))...)
			chunks = append(chunks, bsDescriptor(12, one(dtLight, 3), nil, 10), bsDescriptor(13, one(dtLight, 3), nil, 10))
			expect(t, map[uint16]uint16{10: 0, 11: 0, 12: 11, 13: 10}, chunks...)
		})
		t.Run("takes the bridged node with the lower number as the parent, reported "+order, func(t *testing.T) {
			t.Parallel()
			chunks := append([]*report{partsAttr(0, 10, 1, 41, 42, 43), bsDescriptor(1, one(dtAggregator, 2), []uint16{41, 42, 43}, 10)}, claimants(
				bsDescriptor(41, one(dtBridged, 3), []uint16{43}, 10),
				bsDescriptor(42, one(dtBridged, 3), []uint16{43}, 10))...)
			chunks = append(chunks, bsDescriptor(43, one(dtLight, 3), nil, 10))
			expect(t, map[uint16]uint16{1: 0, 41: 1, 42: 1, 43: 41}, chunks...)
		})
		t.Run("takes the claimant with the lower number as the parent, reported "+order, func(t *testing.T) {
			t.Parallel()
			chunks := append([]*report{partsAttr(0, 10, 10, 11, 12)}, claimants(
				bsDescriptor(10, one(dtLight, 3), []uint16{12}, 10),
				bsDescriptor(11, one(dtLight, 3), []uint16{12}, 10))...)
			chunks = append(chunks, bsDescriptor(12, one(dtLight, 3), nil, 10))
			expect(t, map[uint16]uint16{10: 0, 11: 0, 12: 10}, chunks...)
		})
	}

	t.Run("warns once about the other claimants of an endpoint it places", func(t *testing.T) {
		t.Parallel()
		_, logs := parentsAfter(t,
			partsAttr(0, 10, 10, 11, 12),
			bsDescriptor(10, one(dtLight, 3), []uint16{12}, 10),
			bsDescriptor(11, one(dtLight, 3), []uint16{12}, 10),
			bsDescriptor(12, one(dtLight, 3), nil, 10))
		// matter.js: "Taking ep10 as the parent of ep12: the peer also named
		// it a part of ep11, …"; the Go warning carries the same as attributes.
		w := logs.severalParents()
		if len(w) != 1 || w[0] != "parent=10 part=12 also_named_by=[11] reason=an endpoint has a single parent " {
			t.Fatalf("warnings %q", w)
		}
	})

	t.Run("does not warn about an endpoint the root no longer names", func(t *testing.T) {
		t.Parallel()
		_, logs := parentsAfter(t,
			partsAttr(0, 10, 10, 11),
			bsDescriptor(10, one(dtLight, 3), []uint16{12}, 10),
			bsDescriptor(11, one(dtLight, 3), []uint16{12}, 10),
			bsDescriptor(12, one(dtLight, 3), nil, 10))
		if w := logs.severalParents(); len(w) != 0 {
			t.Fatalf("warnings %q", w)
		}
	})

	t.Run("places every endpoint of a bridge with bridged nodes of both patterns", func(t *testing.T) {
		t.Parallel()
		expect(t, map[uint16]uint16{1: 0, 2: 0, 12: 0, 3: 2, 4: 2, 7: 2, 5: 4, 6: 4, 8: 7, 9: 7, 10: 7, 11: 7, 13: 12},
			partsAttr(0, 10, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13),
			bsDescriptor(1, one(dtLight, 3), nil, 10),
			bsDescriptor(2, one(dtAggregator, 2), []uint16{3, 4, 5, 6, 7, 8, 9, 10, 11}, 10),
			bsDescriptor(3, rev1(dtLight, dtBridged), nil, 10),
			bsDescriptor(4, rev1(dtTemperature, dtBridged), []uint16{5, 6}, 10),
			bsDescriptor(5, one(dtTemperature, 2), nil, 10),
			bsDescriptor(6, one(dtHumidity, 2), nil, 10),
			bsDescriptor(7, rev1(dtAggregator, dtBridged), []uint16{8, 9, 10, 11}, 10),
			bsDescriptor(8, rev1(dtLight, dtBridged), nil, 10),
			bsDescriptor(9, rev1(dtTemperature, dtBridged), nil, 10),
			bsDescriptor(10, one(dtLight, 3), nil, 10),
			bsDescriptor(11, one(dtLight, 3), nil, 10),
			bsDescriptor(12, one(dtAggregator, 2), []uint16{13}, 10),
			bsDescriptor(13, rev1(dtLight, dtBridged), nil, 10))
	})
}
