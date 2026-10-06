// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"testing"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/store"
)

// TestFabriclessPASESessionReadsNoFabricSensitiveData is the second
// commissioner over the wire: a fabric is installed and its controller has
// written its ACL and an Extension, then another commissioner opens a PASE
// session — no AddNOC yet, so the session has no fabric. Its reads go
// through the production receive pipeline (Bridge.dispatch, the IM read
// handler, the endpoint dispatcher and its ACL gate) and must carry nothing
// fabric-sensitive of the existing fabric:
//
//   - a fabric-filtered read of a fabric-scoped list is empty (matter.js
//     ListManager FabricFilteredListProxyHandler filters to
//     `session.fabric`, which a PASE session before AddNOC lacks);
//   - an unfiltered read of Acl / Extension carries the entries redacted to
//     their FabricIndex (protocol/src/action/server/AccessControl.ts mayRead
//     refuses a fabric-sensitive field to a session without a fabric),
//     where it used to answer with the last written fabric's entries whole;
//   - CurrentFabricIndex reads 0 (OperationalCredentialsServer.ts
//     `session.fabric ?? FabricIndex.NO_FABRIC`).
//
// The reads still pass the access gate: a PASE session holds the implicit
// Administer privilege (FabricAccessControl.ts).
func TestFabriclessPASESessionReadsNoFabricSensitiveData(t *testing.T) {
	t.Parallel()
	var acl *mattercore.AccessControl
	h := newSecureHarness(t, nil, func(st *store.Store, _ uint8) []contract.ClusterServer {
		var err error
		if acl, err = mattercore.NewAccessControl(st); err != nil {
			t.Fatal(err)
		}
		oc, err := mattercore.NewOperationalCredentials(st, mattercore.OpcredsConfig{SupportedFabrics: 5})
		if err != nil {
			t.Fatal(err)
		}
		gkm, err := mattercore.NewGroupKeyManagement(st, mattercore.GroupKeyMgmtConfig{})
		if err != nil {
			t.Fatal(err)
		}
		return []contract.ClusterServer{acl, oc, gkm}
	})
	h.allowAll()
	ctx := context.Background()
	if err := h.store.UpsertIdentity(ctx, store.IdentityRecord{FabricIndex: h.fabric, PrivateKey: []byte{0x01}, IPK: make([]byte, 16), NOC: []byte{0x15, 0x18}, ICAC: []byte{0x15, 0x18}}); err != nil {
		t.Fatal(err)
	}
	if err := h.store.SetGroupKeyMapping(ctx, store.GroupKeyMapping{FabricIndex: h.fabric, GroupID: 0x0101, GroupKeySetID: 0x0042}); err != nil {
		t.Fatal(err)
	}
	// The existing controller writes its Extension over its CASE session;
	// that write is also what the old fall-back read answered with.
	if err := acl.MatterWrite(im.WithFabricFilter(ctx, true, h.fabric), 0x0001,
		[]mattercore.AccessControlExtensionEntry{{Data: []byte{0x17, 0x18}}}); err != nil {
		t.Fatal(err)
	}
	acl.SetCurrentFabric(h.fabric)

	// Control: the owning fabric's CASE session sees its entry whole.
	if own := aclRead(t, h, 0x0000, true); len(own.Children) != 1 || len(own.Children[0].Children) < 2 {
		t.Fatalf("CASE Acl read = %+v, want the fabric's entry whole", own)
	}

	h.asPASE()

	for _, attr := range []uint32{0x0000, 0x0001} { // Acl, Extension
		if filtered := aclRead(t, h, attr, true); len(filtered.Children) != 0 {
			t.Errorf("PASE fabric-filtered read of AccessControl 0x%04X carries %d entries, want none", attr, len(filtered.Children))
		}
		all := aclRead(t, h, attr, false)
		if len(all.Children) != 1 {
			t.Fatalf("PASE unfiltered read of AccessControl 0x%04X carries %d entries, want 1", attr, len(all.Children))
		}
		if got := fabricIndexesOf(t, all); got[0] != uint64(h.fabric) || len(all.Children[0].Children) != 1 {
			t.Errorf("PASE unfiltered read of AccessControl 0x%04X = %+v, want the entry redacted to FabricIndex %d", attr, all, h.fabric)
		}
	}

	const opcreds, gkmCluster uint32 = 0x003E, 0x003F
	if data, _, isStatus := h.readAttribute(0, opcreds, 0x0005); isStatus || data.El.Uint != 0 {
		t.Errorf("PASE CurrentFabricIndex = %+v (status %v), want 0", data, isStatus)
	}
	for _, attr := range []uint32{0x0000, 0x0001} { // NOCs, Fabrics
		if l := h.readAttributeData(t, 0, opcreds, attr, true); len(l.Children) != 0 {
			t.Errorf("PASE fabric-filtered read of OperationalCredentials 0x%04X carries %d entries, want none", attr, len(l.Children))
		}
		if l := h.readAttributeData(t, 0, opcreds, attr, false); len(l.Children) != 1 {
			t.Errorf("PASE unfiltered read of OperationalCredentials 0x%04X carries %d entries, want 1", attr, len(l.Children))
		}
	}
	if l := h.readAttributeData(t, 0, gkmCluster, 0x0000, true); len(l.Children) != 0 {
		t.Errorf("PASE fabric-filtered GroupKeyMap carries %d entries, want none", len(l.Children))
	}
	if l := h.readAttributeData(t, 0, gkmCluster, 0x0000, false); len(l.Children) != 1 {
		t.Errorf("PASE unfiltered GroupKeyMap carries %d entries, want 1", len(l.Children))
	}
}
