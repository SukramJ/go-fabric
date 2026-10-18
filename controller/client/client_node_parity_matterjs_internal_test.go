// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/node/ClientNodeTest.ts at 862ad75f,
// the cases that feed the client structure a report directly
// (clientStructureOf(peer).mutate, seedPeerCache): the dropped optional
// attribute, Schrödinger's cluster and the version filters of a cluster
// with changes-omitted attributes. The Go image takes a report through
// Node.apply; the image of a commissioned peer is the report of its
// root and its OnOffLight. The fabrictest cases are in
// client_node_parity_matterjs_test.go.
package client

import (
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/cluster/spec/descriptor"
	"github.com/SukramJ/go-fabric/cluster/spec/levelcontrol"
	"github.com/SukramJ/go-fabric/cluster/spec/timesynchronization"
	"github.com/SukramJ/go-fabric/tlv"
)

// peerImage is the image of a commissioned peer: the root and, on
// endpoint 1, an OnOffLight whose server list is ClientNodeTest.ts:4241
// EP1_SERVER_LIST (identify, groups, onOff, scenesManagement, descriptor).
func peerImage(t *testing.T, st Store) (*Node, *recorder, *logRecorder) {
	t.Helper()
	n, rec, logs := testNode(t, st)
	b := &report{}
	b.endpoint(0, []uint64{dtRootNode}, []uint32{descriptor.ClusterID, clBasicInfo}, 1)
	b.endpoint(1, []uint64{dtOnOffLight}, ep1ServerList)
	n.apply(wildcard, &b.r)
	rec.take()
	return n, rec, logs
}

var ep1ServerList = []uint32{3, 4, 6, 0x62, descriptor.ClusterID}

// filteredOn is ClientNodeTest.ts filteredClusters: the clusters of
// endpoint ep a wildcard read's injected filters name.
func filteredOn(n *Node, ep uint16, refreshChangesOmitted bool) []uint32 {
	n.mu.RLock()
	defer n.mu.RUnlock()
	var out []uint32
	for _, f := range n.s.injectVersionFilters(wildcard, refreshChangesOmitted) {
		if f.Endpoint == ep {
			out = append(out, f.Cluster)
		}
	}
	return out
}

func TestClientNodeChangesOmittedVersionFiltersParity(t *testing.T) {
	t.Parallel()
	t.Run("omits version filters for a cluster with changes-omitted attributes when the peer reports no AttributeList", func(t *testing.T) {
		t.Parallel()
		n, _, _ := testNode(t, &MemoryStore{})
		const timeSync = timesynchronization.ClusterID
		b := &report{}
		b.cluster(0, descriptor.ClusterID, 0, map[uint32]spec.Node{
			descriptor.AttrDeviceTypeList: deviceTypes(dtRootNode),
			descriptor.AttrServerList:     listNode(uint64(descriptor.ClusterID), uint64(timeSync)),
			descriptor.AttrClientList:     listNode(),
			descriptor.AttrPartsList:      listNode(),
		})
		// Time Synchronization with an empty AttributeList and without
		// UTCTime, its changes-omitted attribute (ClientNodeTest.ts:3234-3246).
		b.attr(0, timeSync, spec.AttrClusterRevision, uintNode(2), 7)
		b.attr(0, timeSync, spec.AttrFeatureMap, uintNode(0), 7)
		b.attr(0, timeSync, spec.AttrAttributeList, listNode(), 7)
		b.attr(0, timeSync, spec.AttrAcceptedCommandList, listNode(), 7)
		b.attr(0, timeSync, spec.AttrGeneratedCommandList, listNode(), 7)
		b.attr(0, timeSync, timesynchronization.AttrGranularity, uintNode(0), 7)
		n.apply(wildcard, &b.r)
		if !slices.Contains(filteredOn(n, 0, false), timeSync) {
			t.Fatalf("filters %v lack TimeSynchronization", filteredOn(n, 0, false))
		}
		if slices.Contains(filteredOn(n, 0, true), timeSync) {
			t.Fatalf("refreshing filters %v keep TimeSynchronization", filteredOn(n, 0, true))
		}
	})
}

// levelControlList is the AttributeList of ClientNodeTest.ts:3612-3621
// WithOnOffTransition's LevelControl: the globals, CurrentLevel, OnLevel and
// the three transition times.
func levelControlList(without ...uint32) []uint64 {
	ids := []uint32{
		0x0000, levelcontrol.AttrOnOffTransitionTime, levelcontrol.AttrOnLevel, levelcontrol.AttrOnTransitionTime, levelcontrol.AttrOffTransitionTime,
		spec.AttrGeneratedCommandList, spec.AttrAcceptedCommandList, spec.AttrAttributeList, spec.AttrFeatureMap, spec.AttrClusterRevision,
	}
	var out []uint64
	for _, id := range ids {
		if !slices.Contains(without, id) {
			out = append(out, uint64(id))
		}
	}
	return out
}

// lightingFeature is LevelControl's Lighting (LT) feature bit.
const lightingFeature = 0x2

// levelControlImage is peerImage with a LevelControl on the light:
// OnOffTransitionTime 5 (when withOnOffTransition), OnTransitionTime 10,
// OffTransitionTime 20 (ClientNodeTest.ts:3612-3621).
func levelControlImage(t *testing.T, st Store, withOnOffTransition bool) (*Node, *recorder) {
	t.Helper()
	n, rec, _ := testNode(t, st)
	b := &report{}
	b.endpoint(0, []uint64{dtRootNode}, []uint32{descriptor.ClusterID, clBasicInfo}, 1)
	b.endpoint(1, []uint64{dtOnOffLight}, []uint32{descriptor.ClusterID, 6})
	n.apply(wildcard, &b.r)
	b = &report{}
	b.attr(1, descriptor.ClusterID, descriptor.AttrServerList, listNode(uint64(descriptor.ClusterID), 6, uint64(levelcontrol.ClusterID)), 2)
	list := levelControlList()
	if !withOnOffTransition {
		list = levelControlList(levelcontrol.AttrOnOffTransitionTime)
	}
	lc := levelcontrol.ClusterID
	b.attr(1, lc, spec.AttrClusterRevision, uintNode(6), 1)
	b.attr(1, lc, spec.AttrFeatureMap, uintNode(lightingFeature), 1)
	b.attr(1, lc, spec.AttrAttributeList, listNode(list...), 1)
	b.attr(1, lc, spec.AttrAcceptedCommandList, listNode(), 1)
	b.attr(1, lc, spec.AttrGeneratedCommandList, listNode(), 1)
	b.attr(1, lc, 0x0000, uintNode(0), 1)
	b.attr(1, lc, levelcontrol.AttrOnLevel, uintNode(0), 1)
	if withOnOffTransition {
		b.attr(1, lc, levelcontrol.AttrOnOffTransitionTime, uintNode(5), 1)
	}
	b.attr(1, lc, levelcontrol.AttrOnTransitionTime, uintNode(10), 1)
	b.attr(1, lc, levelcontrol.AttrOffTransitionTime, uintNode(20), 1)
	n.apply(wildcard, &b.r)
	rec.take()
	return n, rec
}

// levelValue is the image's value of a LevelControl attribute of the light.
func levelValue(n *Node, attr uint32) (uint64, bool) {
	c := n.Endpoint(1).Cluster(levelcontrol.ClusterID)
	if c == nil {
		return 0, false
	}
	v, ok := c.Value(attr)
	return v.El.Uint, ok
}

// reportLevelControl is ClientNodeTest.ts:3783-3808 reportLevelControl: one
// report of the light's LevelControl AttributeList and values, version
// 0x1234.
func reportLevelControl(n *Node, list []uint64, values map[uint32]uint64) {
	b := &report{}
	b.attr(1, levelcontrol.ClusterID, spec.AttrAttributeList, listNode(list...), 0x1234)
	for id, v := range values {
		b.attr(1, levelcontrol.ClusterID, id, uintNode(v), 0x1234)
	}
	n.apply(wildcard, &b.r)
}

// expectNoOnOffTransition is ClientNodeTest.ts:3675-3680.
func expectNoOnOffTransition(t *testing.T, n *Node) {
	t.Helper()
	c := n.Endpoint(1).Cluster(levelcontrol.ClusterID)
	if slices.Contains(c.AttributeList(), levelcontrol.AttrOnOffTransitionTime) {
		t.Fatal("the attribute list keeps OnOffTransitionTime")
	}
	if _, ok := levelValue(n, levelcontrol.AttrOnOffTransitionTime); ok {
		t.Fatal("the image keeps OnOffTransitionTime")
	}
	if v, _ := levelValue(n, levelcontrol.AttrOnTransitionTime); v != 10 {
		t.Fatalf("OnTransitionTime %d, want 10", v)
	}
	if v, _ := levelValue(n, levelcontrol.AttrOffTransitionTime); v != 20 {
		t.Fatalf("OffTransitionTime %d, want 20", v)
	}
}

func TestClientNodeDropsOptionalAttributeParity(t *testing.T) {
	t.Parallel()

	t.Run("when the peer reports an unchanged attribute list", func(t *testing.T) {
		t.Parallel()
		st := &MemoryStore{}
		n, _ := levelControlImage(t, st, false)
		// seedPeerCache: the value as if reported, without a list.
		b := &report{}
		b.attr(1, levelcontrol.ClusterID, levelcontrol.AttrOnOffTransitionTime, uintNode(5), 1)
		n.apply(wildcard, &b.r)
		if v, ok := levelValue(n, levelcontrol.AttrOnOffTransitionTime); !ok || v != 5 {
			t.Fatalf("seeded OnOffTransitionTime %d %v, want 5", v, ok)
		}
		reportLevelControl(n, n.Endpoint(1).Cluster(levelcontrol.ClusterID).attributeListIDs(), nil)
		expectNoOnOffTransition(t, n)
		if values, _, _ := stored(t, st, 1, levelcontrol.ClusterID); values != nil {
			if _, ok := values[levelcontrol.AttrOnOffTransitionTime]; ok {
				t.Fatal("the store keeps OnOffTransitionTime")
			}
		}
	})

	t.Run("but keeps a value the peer reports alongside a list that omits it", func(t *testing.T) {
		t.Parallel()
		n, _ := levelControlImage(t, &MemoryStore{}, true)
		reportLevelControl(n, levelControlList(levelcontrol.AttrOnOffTransitionTime, levelcontrol.AttrOnTransitionTime),
			map[uint32]uint64{levelcontrol.AttrOnOffTransitionTime: 7})
		if v, ok := levelValue(n, levelcontrol.AttrOnOffTransitionTime); !ok || v != 7 {
			t.Fatalf("OnOffTransitionTime %d %v, want 7", v, ok)
		}
		if _, ok := levelValue(n, levelcontrol.AttrOnTransitionTime); ok {
			t.Fatal("the image keeps the unlisted OnTransitionTime")
		}
	})

	t.Run("but not when the peer reports an empty attribute list", func(t *testing.T) {
		t.Parallel()
		st := &MemoryStore{}
		n, _ := levelControlImage(t, st, true)
		reportLevelControl(n, nil, nil)
		if v, ok := levelValue(n, levelcontrol.AttrOnOffTransitionTime); !ok || v != 5 {
			t.Fatalf("OnOffTransitionTime %d %v after an empty list, want 5", v, ok)
		}
		reloaded, _, _ := loadedNode(t, st)
		if v, ok := levelValue(reloaded, levelcontrol.AttrOnOffTransitionTime); !ok || v != 5 {
			t.Fatalf("reloaded OnOffTransitionTime %d %v, want 5", v, ok)
		}
	})

	t.Run("but keeps a global attribute a non-compliant list omits", func(t *testing.T) {
		t.Parallel()
		n, _ := levelControlImage(t, &MemoryStore{}, true)
		reportLevelControl(n, levelControlList(spec.AttrFeatureMap), nil)
		if f := n.Endpoint(1).Cluster(levelcontrol.ClusterID).Features(); !slices.Contains(f, "LT") {
			t.Fatalf("features %v, want Lighting", f)
		}
	})
}

// neo is ClientNodeTest.ts:4236 NEO, a manufacturer cluster the peer serves
// data for while its descriptor's server list omits it.
const neo uint32 = 0x125dfc11

// neoReports is ClientNodeTest.ts:4253-4274 neoReports, with the
// revision, extra attributes and accepted commands of its variants
// (:4285-4341).
func neoReports(b *report, version uint32, revision uint64, extraAttr bool, accepted ...uint64) *report {
	list := []uint64{0, uint64(spec.AttrGeneratedCommandList), uint64(spec.AttrAcceptedCommandList), uint64(spec.AttrAttributeList), uint64(spec.AttrFeatureMap), uint64(spec.AttrClusterRevision)}
	if extraAttr {
		list = slices.Insert(list, 1, 2)
	}
	b.attr(1, neo, spec.AttrClusterRevision, uintNode(revision), version)
	b.attr(1, neo, spec.AttrFeatureMap, uintNode(0), version)
	b.attr(1, neo, spec.AttrAttributeList, listNode(list...), version)
	b.attr(1, neo, spec.AttrAcceptedCommandList, listNode(accepted...), version)
	b.attr(1, neo, spec.AttrGeneratedCommandList, listNode(), version)
	b.attr(1, neo, 0, spec.Node{El: uint16El(1234)}, version)
	if extraAttr {
		b.attr(1, neo, 2, spec.Node{El: uint16El(5678)}, version)
	}
	return b
}

// serverList is the descriptor server list report of endpoint 1, with NEO
// when withNeo (ClientNodeTest.ts:4276-4282).
func serverList(b *report, version uint32, withNeo bool) *report {
	ids := make([]uint64, 0, len(ep1ServerList)+1)
	for _, c := range ep1ServerList {
		ids = append(ids, uint64(c))
	}
	if withNeo {
		ids = append(ids, uint64(neo))
	}
	b.attr(1, descriptor.ClusterID, descriptor.AttrServerList, listNode(ids...), version)
	return b
}

func neoCluster(n *Node) *Cluster { return n.Endpoint(1).Cluster(neo) }

func TestClientNodeSchrodingersClusterParity(t *testing.T) {
	t.Parallel()

	t.Run("survives a re-interview where the descriptor precedes the absent cluster's data", func(t *testing.T) {
		t.Parallel()
		n, _, _ := peerImage(t, &MemoryStore{})
		n.apply(wildcard, &serverList(neoReports(&report{}, 10, 1, false), 10, false).r)
		if neoCluster(n) == nil {
			t.Fatal("NEO should be active after the initial interaction")
		}
		n.apply(wildcard, &neoReports(serverList(&report{}, 11, false), 11, 1, false).r)
		if neoCluster(n) == nil {
			t.Fatal("NEO must survive a re-interview that serves its data")
		}
	})

	t.Run("erases persisted storage when the peer genuinely drops the cluster", func(t *testing.T) {
		t.Parallel()
		st := &MemoryStore{}
		n, _, _ := peerImage(t, st)
		n.apply(wildcard, &serverList(neoReports(&report{}, 10, 1, false), 10, false).r)
		if _, _, ok := stored(t, st, 1, neo); neoCluster(n) == nil || !ok {
			t.Fatalf("NEO active %v, stored %v", neoCluster(n) != nil, ok)
		}
		n.apply(wildcard, &serverList(&report{}, 11, false).r)
		if neoCluster(n) != nil {
			t.Fatal("NEO kept though the descriptor alone dropped it")
		}
		if _, _, ok := stored(t, st, 1, neo); ok {
			t.Fatal("NEO's storage survived its removal")
		}
	})

	t.Run("re-installs a cluster the peer drops and later serves again", func(t *testing.T) {
		t.Parallel()
		n, _, _ := peerImage(t, &MemoryStore{})
		n.apply(wildcard, &neoReports(serverList(&report{}, 10, true), 10, 1, false).r)
		if neoCluster(n) == nil {
			t.Fatal("NEO not installed")
		}
		n.apply(wildcard, &serverList(&report{}, 11, false).r)
		if neoCluster(n) != nil {
			t.Fatal("NEO not dropped")
		}
		n.apply(wildcard, &neoReports(serverList(&report{}, 12, true), 12, 1, false).r)
		if neoCluster(n) == nil {
			t.Fatal("NEO not installed again")
		}
	})

	// A rebuilt cluster is matter.js's new behavior type; the Go image
	// announces it with ClusterReplaced.
	rebuilt := func(t *testing.T, change func(b *report)) (*Node, []string) {
		t.Helper()
		n, rec, _ := peerImage(t, &MemoryStore{})
		n.apply(wildcard, &neoReports(serverList(&report{}, 10, true), 10, 1, false).r)
		if neoCluster(n) == nil {
			t.Fatal("NEO not installed")
		}
		rec.take()
		b := serverList(&report{}, 11, true)
		change(b)
		n.apply(wildcard, &b.r)
		return n, rec.take()
	}
	replacedNeo := "replaced 1/Cluster$125dfc11"

	t.Run("rebuilds a cluster when the peer changes its attribute list without a feature change", func(t *testing.T) {
		t.Parallel()
		n, hooks := rebuilt(t, func(b *report) { neoReports(b, 11, 1, true) })
		if neoCluster(n) == nil || !slices.Contains(hooks, replacedNeo) {
			t.Fatalf("hooks %v, want %q", hooks, replacedNeo)
		}
	})

	t.Run("rebuilds a cluster when the peer changes its revision without a feature change", func(t *testing.T) {
		t.Parallel()
		n, hooks := rebuilt(t, func(b *report) { neoReports(b, 11, 2, false) })
		if c := neoCluster(n); c == nil || c.Revision() != 2 || !slices.Contains(hooks, replacedNeo) {
			t.Fatalf("hooks %v, want %q and revision 2", hooks, replacedNeo)
		}
	})

	t.Run("rebuilds a cluster when the peer changes its accepted command list without a feature change", func(t *testing.T) {
		t.Parallel()
		n, hooks := rebuilt(t, func(b *report) { neoReports(b, 11, 1, false, 1) })
		if c := neoCluster(n); c == nil || !c.SupportsCommand(1) || !slices.Contains(hooks, replacedNeo) {
			t.Fatalf("hooks %v, want %q and command 1", hooks, replacedNeo)
		}
	})
}

// attributeListIDs is the cluster's AttributeList as list values.
func (c *Cluster) attributeListIDs() []uint64 {
	var out []uint64
	for _, id := range c.AttributeList() {
		out = append(out, uint64(id))
	}
	return out
}

// uint16El is a two-byte unsigned TLV element.
func uint16El(v uint64) tlv.Element { return tlv.Element{Type: tlv.TypeUnsignedInt2, Uint: v} }
