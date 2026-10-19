// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behaviors/group-key-management/GroupKeyManagementServerTest.ts
// at 862ad75f: the provisional Groupcast feature and GroupcastAdoption
// attribute, the key-count limit, and the multicast policy KeySetWrite
// ignores and KeySetRead reports as PerGroupID.
//
// matter.js runs MockServerNode / MockSite pairs. The policy cases run on a
// fabrictest pair over the wire and read the device's stored key set, as
// stateOf does. The limit case sets maxGroupKeysPerFabric 3 on the node
// (the server state option); node.Config carries no such option, so it runs
// on the server with GroupKeyMgmtConfig.MaxGroupKeysPerFabric 3 and the
// same three writes. The two feature cases run on the server: the Go
// GroupKeyManagement has no feature selection, so the provisional Groupcast
// feature cannot be selected (matter.js refuses it at initialize, "Groupcast
// feature of GroupKeyManagement is provisional"); the Go assertion is the
// FeatureMap without GCAST and GroupcastAdoption (0x0004, conformance
// "P, GCAST") neither listed nor served.
//
// "prunes GroupKeyMap entries that reference a removed key set" is
// store:TestRemoveGroupKeySet_DropsOnlyItsMappings and
// bridge:TestGroupKeyManagementCommandsOverTheWire. Missing (register):
// "drops the policy from key sets stored by earlier versions" —
// js-runtime-only: matter.js's persisted behavior state of earlier
// matter.js versions carried the policy; the Go key-set table
// (store.GroupKeySet) has no policy column and never had one.
package core_test

import (
	"context"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	mstore "github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/tlv"
)

// The test's key material (GroupKeyManagementServerTest.ts:51-58).
var (
	gkmEpochKey0 = []byte{0xd0, 0xd1, 0xd2, 0xd3, 0xd4, 0xd5, 0xd6, 0xd7, 0xd8, 0xd9, 0xda, 0xdb, 0xdc, 0xdd, 0xde, 0xdf}
	gkmEpochKey1 = []byte{0xd1, 0xd1, 0xd2, 0xd3, 0xd4, 0xd5, 0xd6, 0xd7, 0xd8, 0xd9, 0xda, 0xdb, 0xdc, 0xdd, 0xde, 0xdf}
	gkmEpochKey2 = []byte{0xd2, 0xd1, 0xd2, 0xd3, 0xd4, 0xd5, 0xd6, 0xd7, 0xd8, 0xd9, 0xda, 0xdb, 0xdc, 0xdd, 0xde, 0xdf}
)

const (
	gkmEpochStart0 uint64 = 18446744073709551612
	gkmEpochStart1 uint64 = 18446744073709551613
	gkmEpochStart2 uint64 = 18446744073709551614
	// gkmPolicyAllNodes is GroupKeyMulticastPolicy AllNodes (1).
	gkmPolicyAllNodes uint8 = 1
)

// TestGroupKeyManagementProvisionalGroupcast ports "rejects the provisional
// Groupcast feature" (GroupKeyManagementServerTest.ts:19-25) and "does not
// implement the provisional GroupcastAdoption attribute by default"
// (:27-31) on the server, as the file comment says.
func TestGroupKeyManagementProvisionalGroupcast(t *testing.T) {
	t.Parallel()
	gkm, err := core.NewGroupKeyManagement(newFakeStore(), core.GroupKeyMgmtConfig{})
	if err != nil {
		t.Fatal(err)
	}
	t.Run("rejects the provisional Groupcast feature", func(t *testing.T) {
		if v, ok := gkm.MatterRead(cluster.AttrGlobalFeatureMap); !ok || v.(uint32)&0x2 != 0 {
			t.Errorf("FeatureMap = %v, %v; want no Groupcast (GCAST, bit 1)", v, ok)
		}
	})
	t.Run("does not implement the provisional GroupcastAdoption attribute by default", func(t *testing.T) {
		if v, ok := gkm.MatterRead(0x0004); ok {
			t.Errorf("GroupcastAdoption served: %v", v)
		}
		list, _ := gkm.MatterRead(cluster.AttrGlobalAttributeList)
		if slices.Contains(list.([]uint32), 0x0004) || slices.Contains(gkm.MatterAttributes(), 0x0004) {
			t.Error("GroupcastAdoption listed")
		}
	})
}

// gkmKeySet is the limit case's key set with three epoch keys.
func gkmKeySet(id uint16) core.GroupKeySetStruct {
	return core.GroupKeySetStruct{
		GroupKeySetID: id, GroupKeySecurityPolicy: 0,
		EpochKey0: gkmEpochKey0, EpochStartTime0: gkmEpochStart0,
		EpochKey1: gkmEpochKey1, EpochStartTime1: gkmEpochStart1,
		EpochKey2: gkmEpochKey2, EpochStartTime2: gkmEpochStart2,
	}
}

// TestGroupKeyManagementTooManyGroupKeys ports "prevents too many group
// keys" (GroupKeyManagementServerTest.ts:33-91): with
// maxGroupKeysPerFabric 3, key sets 1 and 2 are written and key set 3 is
// refused with ResourceExhausted (code 137) — the IPK key set counts as the
// third (GroupKeyManagementServer.ts keySetWrite).
func TestGroupKeyManagementTooManyGroupKeys(t *testing.T) {
	t.Parallel()
	fs := newFakeStore()
	fabric, err := fs.AddFabric(context.Background(), mstore.FabricRecord{FabricID: 1, CompressedID: [8]byte{1}})
	if err != nil {
		t.Fatal(err)
	}
	gkm, err := core.NewGroupKeyManagement(fs, core.GroupKeyMgmtConfig{MaxGroupKeysPerFabric: 3})
	if err != nil {
		t.Fatal(err)
	}
	ctx := im.WithFabricFilter(context.Background(), true, fabric)
	for _, id := range []uint16{1, 2} {
		if _, err := gkm.MatterInvoke(ctx, 0x00, core.KeySetWriteRequest{GroupKeySet: gkmKeySet(id)}); err != nil {
			t.Fatalf("KeySetWrite %d: %v", id, err)
		}
	}
	_, err = gkm.MatterInvoke(ctx, 0x00, core.KeySetWriteRequest{GroupKeySet: gkmKeySet(3)})
	if statusOf(err) != im.StatusResourceExhausted { // 0x89, code 137
		t.Errorf("KeySetWrite 3: %v, want ResourceExhausted (137)", err)
	}
}

// gkmWireKeySet is the describe "GroupKeyMulticastPolicy"'s keySet
// (:94-103) with GroupKeyMulticastPolicy AllNodes, as KeySetWrite carries
// it: TrustFirst, one epoch key, the other epochs null.
type gkmWireKeySet struct{}

func (gkmWireKeySet) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) {
	enc.StartStruct(tag)
	enc.StartStruct(tlv.ContextTag(0))
	enc.PutUint(tlv.ContextTag(0), 1)
	enc.PutUint(tlv.ContextTag(1), 0)
	enc.PutOctets(tlv.ContextTag(2), gkmEpochKey0)
	enc.PutUint(tlv.ContextTag(3), gkmEpochStart0)
	for _, f := range []uint8{4, 5, 6, 7} {
		enc.PutNull(tlv.ContextTag(f))
	}
	enc.PutUint(tlv.ContextTag(8), uint64(gkmPolicyAllNodes))
	_ = enc.EndContainer()
	_ = enc.EndContainer()
}

// gkmKeySetRead is KeySetRead's request.
type gkmKeySetRead uint16

func (id gkmKeySetRead) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) {
	enc.StartStruct(tag)
	enc.PutUint(tlv.ContextTag(0), uint64(id))
	_ = enc.EndContainer()
}

// gkmPair is a commissioned pair whose controller wrote the describe's key
// set with the AllNodes policy.
func gkmPair(t *testing.T) *fabrictest.Pair {
	t.Helper()
	p := fabrictest.NewSite(t, fabrictest.Options{}).CommissionedPair(fabrictest.PairOptions{})
	write := im.ConcreteCommandPath{Endpoint: 0, Cluster: 0x003F, Command: 0x00}
	if _, err := fabrictest.Invoke(context.Background(), p, write, gkmWireKeySet{}); err != nil {
		t.Fatalf("KeySetWrite with the AllNodes policy: %v", err)
	}
	return p
}

// TestGroupKeyManagementMulticastPolicy ports the describe
// "GroupKeyMulticastPolicy" (GroupKeyManagementServerTest.ts:93-168) but
// "drops the policy from key sets stored by earlier versions" (see the file
// comment): KeySetWrite accepts a key set whose policy is AllNodes and
// stores it without the policy, and KeySetRead reports PerGroupID for it
// and for the IPK key set 0 (GroupKeyManagementServer.ts
// withoutMulticastPolicy :64-72, keySetRead).
func TestGroupKeyManagementMulticastPolicy(t *testing.T) {
	t.Parallel()
	t.Run("accepts a key set whose multicast policy is not PerGroupID and does not store the policy", func(t *testing.T) {
		t.Parallel()
		p := gkmPair(t)
		fabrics, err := p.Device.Node().Fabrics(context.Background())
		if err != nil || len(fabrics) != 1 {
			t.Fatalf("fabrics %v, %v", fabrics, err)
		}
		// The stored key set: store.GroupKeySet has no policy field.
		stored, err := p.Device.Store().GetGroupKeySet(context.Background(), fabrics[0].FabricIndex, 1)
		if err != nil {
			t.Fatalf("stored key set 1: %v", err)
		}
		if stored.GroupKeySetID != 1 {
			t.Errorf("stored GroupKeySetID %d, want 1", stored.GroupKeySetID)
		}
	})
	t.Run("reports PerGroupID for a written key set and for the IPK key set", func(t *testing.T) {
		t.Parallel()
		p := gkmPair(t)
		read := im.ConcreteCommandPath{Endpoint: 0, Cluster: 0x003F, Command: 0x01}
		for _, id := range []uint16{0, 1} {
			res, err := fabrictest.Invoke(context.Background(), p, read, gkmKeySetRead(id))
			if err != nil {
				t.Fatalf("KeySetRead %d: %v", id, err)
			}
			set, ok := gkmField(res.Fields, 0)
			if !ok {
				t.Fatalf("KeySetReadResponse %d without GroupKeySet: %+v", id, res)
			}
			gotID, okID := gkmField(set, 0)
			policy, okPolicy := gkmField(set, 8)
			if !okID || gotID.El.Uint != uint64(id) {
				t.Errorf("KeySetRead %d: GroupKeySetID %+v", id, gotID.El)
			}
			if !okPolicy || policy.El.Uint != uint64(core.GroupKeyMulticastPolicyPerGroupID) {
				t.Errorf("KeySetRead %d: GroupKeyMulticastPolicy %+v, %v; want PerGroupID", id, policy.El, okPolicy)
			}
		}
	})
}

// gkmField is the member of a structure with the context tag.
func gkmField(n spec.Node, tag uint32) (spec.Node, bool) {
	for _, c := range n.Children {
		if c.El.Tag.Kind == tlv.TagKindContext && c.El.Tag.Number == tag {
			return c, true
		}
	}
	return spec.Node{}, false
}
