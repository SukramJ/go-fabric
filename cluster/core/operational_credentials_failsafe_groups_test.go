// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/groups"
	"github.com/SukramJ/go-fabric/im"
)

// TestFailSafeExpiryRevertRunsTheFabricRemovalFanOut: a fail-safe that
// expires after AddNOC reverts the fabric with "the equivalent effect of
// invoking the RemoveFabric command" (core§11.9.7.2 step 6; matter.js
// FailsafeContext.rollback deletes the fabric, and FabricManager's deleted
// events reach every fabric-scoped holder). The revert therefore runs the
// OnFabricRemoved hook — the one surface through which the host forgets
// the fabric's sessions, subscriptions and group state — and before it did,
// a controller that provisioned groups ahead of CommissioningComplete left
// the reverted fabric's keys and group table loaded in the group state.
func TestFailSafeExpiryRevertRunsTheFabricRemovalFanOut(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newFakeStore()
	mgr, err := groups.NewManager(st, nil)
	if err != nil {
		t.Fatal(err)
	}
	oc, err := core.NewOperationalCredentials(st, core.OpcredsConfig{SupportedFabrics: 5})
	if err != nil {
		t.Fatal(err)
	}
	var removed []uint8
	oc.SetOnFabricRemoved(func(_ context.Context, idx uint8) {
		removed = append(removed, idx)
		mgr.ForgetFabric(idx) // what Bridge.EmitFabricRemoved does with it
	})

	rootPriv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := oc.MatterInvoke(ctx, 0x0B, core.AddTrustedRootCertificateRequest{
		RootCACertificate: buildCoreSignedCert(t, rootPriv, true, rootPriv),
	}); err != nil {
		t.Fatal(err)
	}
	pendingPub := issueCSRPendingPubKey(ctx, t, oc, false)
	resp, err := oc.MatterInvoke(ctx, 0x06, core.AddNOCRequest{
		NOCValue:         buildCoreSignedCertForPubKey(t, pendingPub, false, rootPriv, testDefaultFabricID, testDefaultNodeID),
		IPKValue:         make([]byte, 16),
		CaseAdminSubject: 0x0001_0203_0405_0607,
		AdminVendorID:    0x1234,
	})
	if err != nil || resp.(core.NOCResponse).StatusCode != core.NOCStatusOK {
		t.Fatalf("AddNOC: %+v, %v", resp, err)
	}
	idx := resp.(core.NOCResponse).FabricIndex

	// The controller provisions a group before CommissioningComplete.
	gkm, err := core.NewGroupKeyManagement(st, core.GroupKeyMgmtConfig{Groups: mgr})
	if err != nil {
		t.Fatal(err)
	}
	fctx := im.WithFabricFilter(ctx, true, idx)
	if _, err := gkm.MatterInvoke(fctx, 0x00, core.KeySetWriteRequest{GroupKeySet: core.GroupKeySetStruct{
		GroupKeySetID: 0x10, EpochKey0: make([]byte, 16), EpochStartTime0: 1,
	}}); err != nil {
		t.Fatal(err)
	}
	if err := gkm.MatterWrite(fctx, 0x0000, []core.GroupKeyMapStruct{{GroupID: 1, GroupKeySetID: 0x10}}); err != nil {
		t.Fatal(err)
	}
	if err := mgr.AddEndpointForGroup(ctx, idx, 1, 3, ""); err != nil {
		t.Fatal(err)
	}

	oc.OnFailSafeExpiry(ctx, idx)
	if len(removed) != 1 || removed[0] != idx {
		t.Fatalf("OnFabricRemoved ran for %v, want once for the reverted fabric %d", removed, idx)
	}
	if _, err := mgr.GroupTable(ctx, idx); !errors.Is(err, groups.ErrNoFabric) {
		t.Fatalf("group state of the reverted fabric still loaded (GroupTable err = %v)", err)
	}

	// A clean expiry with nothing pending reverts nothing and removes nothing.
	oc.OnFailSafeExpiry(ctx, 0)
	if len(removed) != 1 {
		t.Fatalf("an expiry without a pending fabric ran OnFabricRemoved again: %v", removed)
	}
}
