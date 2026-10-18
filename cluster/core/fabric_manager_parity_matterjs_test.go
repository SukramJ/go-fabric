// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

// A port of matter.js packages/protocol/test/fabric/FabricManagerTest.ts at
// 862ad75f: replacing a fabric (UpdateNOC) keeps its group and access
// control state, a restore keeps group changes made meanwhile, and a
// replacement from another root is refused.
//
// matter.js FabricManager.replaceFabric swaps the Fabric object and hands it
// the previous one's groups, access control and ICD state. A Go fabric is a
// store row; UpdateNOC rewrites its identity and node id, the group and ACL
// rows stay keyed by the fabric index, and the fail-safe expiry restores the
// previous identity. The replacement is reported through OnFabricUpdated.
// This module registers no ICD clients (ICDManagement has no CIP feature),
// so the ICD part of the matter.js case has nothing to carry over.

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/hex"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/groups"
	"github.com/SukramJ/go-fabric/im"
	mstore "github.com/SukramJ/go-fabric/store"
)

const (
	fabricManagerGroup    = uint16(0x0101) // FabricManagerTest.ts:22
	fabricManagerKeySetID = uint16(0x1a1)  // FabricManagerTest.ts:23
)

// fabricManagerReplaceable is replaceable() (FabricManagerTest.ts:96-101)
// with provisionGroup (:31-46): a commissioned fabric with key set 0x1a1,
// group 0x0101 mapped to it with endpoint 1 and the IanaAddr policy.
type fabricManagerReplaceable struct {
	fs       *fakeStore
	oc       *core.OperationalCredentials
	mgr      *groups.Manager
	rootPriv *ecdsa.PrivateKey
	idx      uint8
	fabCtx   context.Context
}

func newFabricManagerReplaceable(t *testing.T) *fabricManagerReplaceable {
	t.Helper()
	ctx := context.Background()
	r := &fabricManagerReplaceable{fs: newFakeStore()}
	oc, err := core.NewOperationalCredentials(r.fs, core.OpcredsConfig{SupportedFabrics: 5})
	if err != nil {
		t.Fatal(err)
	}
	r.oc = oc
	_, r.rootPriv, r.idx = commissionTestFabric(ctx, t, r.oc)
	r.fabCtx = im.WithFabricFilter(ctx, true, r.idx)
	epoch, err := hex.DecodeString("d0d1d2d3d4d5d6d7d8d9dadbdcdddedf") // FabricManagerTest.ts:36
	if err != nil {
		t.Fatal(err)
	}
	for _, err := range []error{
		r.fs.UpsertGroupKeySet(ctx, mstore.GroupKeySet{
			FabricIndex: r.idx, GroupKeySetID: fabricManagerKeySetID,
			SecurityPolicy: mstore.SecurityPolicyTrustFirst, EpochKey0: epoch, EpochStart0: 1,
		}),
		r.fs.SetGroupKeyMapping(ctx, mstore.GroupKeyMapping{FabricIndex: r.idx, GroupID: fabricManagerGroup, GroupKeySetID: fabricManagerKeySetID}),
		r.fs.UpsertGroupTableEntry(ctx, mstore.GroupTableEntry{FabricIndex: r.idx, GroupID: fabricManagerGroup, Endpoints: []uint16{1}}),
		r.fs.UpsertGroupcastGroup(ctx, mstore.GroupcastGroup{FabricIndex: r.idx, GroupID: fabricManagerGroup, McastAddrPolicy: groups.PolicyIanaAddr}),
	} {
		if err != nil {
			t.Fatal(err)
		}
	}
	if r.mgr, err = groups.NewManager(r.fs, nil); err != nil {
		t.Fatal(err)
	}
	return r
}

// operationalKey is the operational key of group 0x0101's key set as the
// store holds it now.
func (r *fabricManagerReplaceable) operationalKey(t *testing.T) []byte {
	t.Helper()
	ctx := context.Background()
	fab, err := r.fs.GetFabric(ctx, r.idx)
	if err != nil {
		t.Fatal(err)
	}
	ks, err := r.fs.GetGroupKeySet(ctx, r.idx, fabricManagerKeySetID)
	if err != nil {
		t.Fatal(err)
	}
	key, err := groups.OperationalKey(ks.EpochKey0, fab.CompressedID)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

// replace sends UpdateNOC with a NOC of the same root for nodeID.
func (r *fabricManagerReplaceable) replace(t *testing.T, signer *ecdsa.PrivateKey, nodeID uint64) core.NOCResponse {
	t.Helper()
	noc := mintUpdateNOC(r.fabCtx, t, r.oc, signer, testDefaultFabricID, nodeID)
	resp, err := r.oc.MatterInvoke(r.fabCtx, 0x07, core.UpdateNOCRequest{NOCValue: noc})
	if err != nil {
		t.Fatalf("UpdateNOC: %v", err)
	}
	return resp.(core.NOCResponse)
}

// groupState is what the replaced listener reads (FabricManagerTest.ts:107-113).
type fabricManagerGroupState struct {
	keySetID  uint16
	endpoints []uint16
	address   string
}

func (r *fabricManagerReplaceable) groupState(t *testing.T, ctx context.Context) fabricManagerGroupState {
	t.Helper()
	if err := r.mgr.Reload(ctx, r.idx); err != nil {
		t.Fatal(err)
	}
	var s fabricManagerGroupState
	ms, err := r.mgr.GroupcastMemberships(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range ms {
		if m.FabricIndex == r.idx && m.GroupID == fabricManagerGroup {
			s.keySetID, s.endpoints = m.KeySetID, m.Endpoints
		}
	}
	addr, err := r.mgr.MulticastAddressFor(ctx, r.idx, fabricManagerGroup)
	if err != nil {
		t.Fatal(err)
	}
	s.address = addr.String()
	return s
}

func TestFabricManagerReplacingFabrics(t *testing.T) {
	t.Parallel()

	t.Run("shares groups, access control and ICD state with the replacement before it reports it", func(t *testing.T) {
		t.Parallel()
		r := newFabricManagerReplaceable(t)
		key := r.operationalKey(t)
		aclBefore, err := r.fs.ListACL(context.Background(), r.idx)
		if err != nil || len(aclBefore) == 0 {
			t.Fatalf("ACL before = %v, %v; want the AddNOC admin entry", aclBefore, err)
		}
		var seen *fabricManagerGroupState
		var aclSeen []mstore.ACLEntry
		r.oc.SetOnFabricUpdated(func(ctx context.Context, idx uint8) {
			s := r.groupState(t, ctx)
			seen = &s
			aclSeen, _ = r.fs.ListACL(ctx, idx)
		})

		if nr := r.replace(t, r.rootPriv, testDefaultNodeID+1); nr.StatusCode != core.NOCStatusOK {
			t.Fatalf("UpdateNOC = %d (%s), want OK", nr.StatusCode, nr.DebugText)
		}

		want := fabricManagerGroupState{keySetID: fabricManagerKeySetID, endpoints: []uint16{1}, address: "ff05::fa"} // :117
		if seen == nil || seen.keySetID != want.keySetID || !slices.Equal(seen.endpoints, want.endpoints) || seen.address != want.address {
			t.Fatalf("replaced listener saw %+v, want %+v", seen, want)
		}
		if len(aclSeen) != len(aclBefore) || !slices.Equal(aclSeen[0].Subjects, aclBefore[0].Subjects) || aclSeen[0].Privilege != aclBefore[0].Privilege {
			t.Fatalf("access control after the replacement = %+v, want %+v", aclSeen, aclBefore)
		}
		if !bytes.Equal(r.operationalKey(t), key) {
			t.Fatal("the group's operational key changed with the replacement")
		}
	})

	t.Run("keeps group changes made while replaced when the previous fabric is restored", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		r := newFabricManagerReplaceable(t)
		before, err := r.fs.GetFabric(ctx, r.idx)
		if err != nil {
			t.Fatal(err)
		}
		if nr := r.replace(t, r.rootPriv, testDefaultNodeID+1); nr.StatusCode != core.NOCStatusOK {
			t.Fatalf("UpdateNOC = %d (%s), want OK", nr.StatusCode, nr.DebugText)
		}
		if err := r.fs.RemoveGroupKeyMapping(ctx, r.idx, fabricManagerGroup); err != nil {
			t.Fatal(err)
		}

		r.oc.OnFailSafeExpiry(ctx, r.idx) // restores the previous fabric

		if f, _ := r.fs.GetFabric(ctx, r.idx); f.NodeID != before.NodeID {
			t.Fatalf("restored fabric node id 0x%X, want 0x%X", f.NodeID, before.NodeID)
		}
		maps, err := r.fs.ListGroupKeyMappings(ctx, r.idx)
		if err != nil {
			t.Fatal(err)
		}
		if slices.ContainsFunc(maps, func(m mstore.GroupKeyMapping) bool { return m.GroupID == fabricManagerGroup }) {
			t.Fatal("the group mapping removed while replaced came back with the restore")
		}
	})

	t.Run("refuses a replacement from another root", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		r := newFabricManagerReplaceable(t)
		before, _ := r.fs.GetIdentity(ctx, r.idx)
		other, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		if nr := r.replace(t, other, testDefaultNodeID); nr.StatusCode == core.NOCStatusOK {
			t.Fatal("UpdateNOC from another root accepted")
		}
		if after, _ := r.fs.GetIdentity(ctx, r.idx); !bytes.Equal(after.NOC, before.NOC) {
			t.Fatal("the refused replacement changed the fabric")
		}
	})
}
