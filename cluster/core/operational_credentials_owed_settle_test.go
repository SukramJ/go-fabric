// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/im"
	mstore "github.com/SukramJ/go-fabric/store"
)

// TestOpcreds_UpdateNOCSettlesTheOwedRevertBeforeItsRecord pins review II
// R2-P1-rw-01 on the UpdateNOC side: an UpdateNOC revert the expiry could
// not land (owed), whose retry at the next fresh arm failed again, lands
// inside the next UpdateNOC once the store recovered. That UpdateNOC's own
// revert record must name the restored credentials, not the half-applied
// ones the retry just removed — else its fail-safe's expiry "reverts" the
// fabric to an UpdateNOC that was never committed.
func TestOpcreds_UpdateNOCSettlesTheOwedRevertBeforeItsRecord(t *testing.T) {
	t.Parallel()
	for name, open := range restartStores {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			inner := open(t)
			st := &identityFailingStore{rollbackFailingStore{restartStore: inner}}
			oc, err := core.NewOperationalCredentials(st, core.OpcredsConfig{SupportedFabrics: 5, FailSafe: armedFailSafe(0)})
			if err != nil {
				t.Fatal(err)
			}
			_, rootPriv, idx := commissionTestFabric(ctx, t, oc)
			commitForTest(t, oc) // CommissioningComplete
			before := readCredentials(ctx, t, inner, idx)
			fabCtx := im.WithFabricFilter(ctx, true, idx)
			nocRaw := mintUpdateNOC(fabCtx, t, oc, rootPriv, testDefaultFabricID, testDefaultNodeID+7)
			if resp, err := oc.MatterInvoke(fabCtx, 0x07, core.UpdateNOCRequest{NOCValue: nocRaw}); err != nil || resp.(core.NOCResponse).StatusCode != core.NOCStatusOK {
				t.Fatalf("UpdateNOC: %+v, %v", resp, err)
			}
			st.fail.Store(true)
			oc.OnFailSafeExpiry(ctx, idx) // the revert fails: owed
			half := readCredentials(ctx, t, inner, idx)
			// mintUpdateNOC arms afresh (ClearPendingState): its retry fails too.
			nocRaw2 := mintUpdateNOC(fabCtx, t, oc, rootPriv, testDefaultFabricID, testDefaultNodeID+8)
			st.fail.Store(false)
			resp, err := oc.MatterInvoke(fabCtx, 0x07, core.UpdateNOCRequest{NOCValue: nocRaw2})
			if nr, _ := resp.(core.NOCResponse); err != nil || nr.StatusCode != core.NOCStatusOK {
				t.Fatalf("UpdateNOC over a healthy store with an owed revert: %+v, %v; want OK (the settle lands it)", resp, err)
			}
			oc.OnFailSafeExpiry(ctx, idx)
			if got := readCredentials(ctx, t, inner, idx); !got.equal(before) {
				t.Fatalf("after the second fail-safe expired: node 0x%X (the half-applied update's was 0x%X), want 0x%X; equal to the half-applied update: %v",
					got.nodeID, half.nodeID, before.nodeID, got.equal(half))
			}
			restartOpcreds(t, inner)
			if got := readCredentials(ctx, t, inner, idx); !got.equal(before) {
				t.Fatalf("after a restart: node 0x%X, want 0x%X", got.nodeID, before.nodeID)
			}
		})
	}
}

// TestOpcreds_AddNOCSettlesTheOwedRevertBeforeItsChecks pins review II
// R2-P1-rw-01 on the AddNOC side: the removal of an uncommitted fabric
// the expiry and the next fresh arm could not land is owed. A
// commissioner's retry once the store recovered must find the table as it
// is after that removal: its TableFull guard and its conflict check run
// after the settle, not over the fabric the settle removes.
func TestOpcreds_AddNOCSettlesTheOwedRevertBeforeItsChecks(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		supported uint8
		sameRoot  bool
	}{
		{name: "the same root and fabric id", supported: 5, sameRoot: true},
		{name: "a full table", supported: 1, sameRoot: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			inner := restartStores["memstore"](t)
			st := &removeFailingStore{rollbackFailingStore{restartStore: inner}}
			oc, err := core.NewOperationalCredentials(st, core.OpcredsConfig{SupportedFabrics: tc.supported, FailSafe: armedFailSafe(0)})
			if err != nil {
				t.Fatal(err)
			}
			rootRaw, rootPriv, idx := commissionTestFabric(ctx, t, oc)
			st.fail.Store(true)
			oc.OnFailSafeExpiry(ctx, idx) // the removal fails: owed
			oc.ClearPendingState()        // the next fresh arm: the retry fails too
			if _, err := inner.GetFabric(ctx, idx); err != nil {
				t.Fatalf("the failing removal removed the fabric: %v", err)
			}
			st.fail.Store(false)

			if !tc.sameRoot {
				rootPriv, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
				if err != nil {
					t.Fatal(err)
				}
				rootRaw = buildCoreSignedCert(t, rootPriv, true, rootPriv)
			}
			if _, err := oc.MatterInvoke(ctx, 0x0B, core.AddTrustedRootCertificateRequest{RootCACertificate: rootRaw}); err != nil {
				t.Fatalf("AddTrustedRootCertificate: %v", err)
			}
			pendingPub := issueCSRPendingPubKey(ctx, t, oc, false)
			resp, err := oc.MatterInvoke(ctx, 0x06, core.AddNOCRequest{
				NOCValue:         buildCoreSignedCertForPubKey(t, pendingPub, false, rootPriv, testDefaultFabricID, testDefaultNodeID),
				IPKValue:         make([]byte, 16),
				CaseAdminSubject: 0xABCD,
				AdminVendorID:    0x1234,
			})
			if nr, _ := resp.(core.NOCResponse); err != nil || nr.StatusCode != core.NOCStatusOK {
				t.Fatalf("the commissioner's retried AddNOC: %+v, %v; want OK — its checks ran over the fabric the owed removal settles", resp, err)
			}
			if fabrics, _ := inner.ListFabrics(ctx); len(fabrics) != 1 {
				t.Fatalf("%d fabrics after the retried AddNOC, want 1", len(fabrics))
			}
		})
	}
}

// TestOpcreds_OwedUpdateRevertOfARemovedFabricIsSettled pins review II
// R2-P1-rw-02 on the row: an UpdateNOC rollback the fail-safe's end could
// not land leaves the row pending; the administrator then removes that
// fabric, and the pending state leaves with its row. The next arm finds
// nothing left to roll back — no identity is written for the removed
// index — and an UpdateNOC on another fabric is admitted.
func TestOpcreds_OwedUpdateRevertOfARemovedFabricIsSettled(t *testing.T) {
	t.Parallel()
	for name, open := range restartStores {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			inner := open(t)
			st := &identityFailingStore{rollbackFailingStore{restartStore: inner}}
			oc, err := core.NewOperationalCredentials(st, core.OpcredsConfig{SupportedFabrics: 5, FailSafe: armedFailSafe(0)})
			if err != nil {
				t.Fatal(err)
			}
			_, rootPriv, idx := commissionTestFabric(ctx, t, oc)
			commitForTest(t, oc) // CommissioningComplete
			_, otherRootPriv, other := commissionTestFabric(ctx, t, oc)
			commitForTest(t, oc) // CommissioningComplete
			fabCtx := im.WithFabricFilter(ctx, true, idx)
			nocRaw := mintUpdateNOC(fabCtx, t, oc, rootPriv, testDefaultFabricID, testDefaultNodeID+7)
			if resp, err := oc.MatterInvoke(fabCtx, 0x07, core.UpdateNOCRequest{NOCValue: nocRaw}); err != nil || resp.(core.NOCResponse).StatusCode != core.NOCStatusOK {
				t.Fatalf("UpdateNOC: %+v, %v", resp, err)
			}
			st.fail.Store(true)
			oc.OnFailSafeExpiry(ctx, idx) // the revert fails: owed
			st.fail.Store(false)
			if err := oc.RemoveFabric(ctx, idx); err != nil {
				t.Fatalf("RemoveFabric: %v", err)
			}
			oc.ClearPendingState() // the next fresh arm, over a healthy store
			if fabrics, _ := inner.ListFabrics(ctx); len(fabrics) != 1 || fabrics[0].Pending != mstore.FabricCommitted {
				t.Fatalf("fabrics after the removal and a healthy arm = %+v, want only the other committed fabric", fabrics)
			}
			if _, err := inner.GetIdentity(ctx, idx); err == nil {
				t.Fatalf("the retry wrote an identity for the removed fabric %d", idx)
			}
			otherCtx := im.WithFabricFilter(ctx, true, other)
			nocOther := mintUpdateNOC(otherCtx, t, oc, otherRootPriv, testDefaultFabricID, testDefaultNodeID+9)
			if resp, err := oc.MatterInvoke(otherCtx, 0x07, core.UpdateNOCRequest{NOCValue: nocOther}); err != nil || resp.(core.NOCResponse).StatusCode != core.NOCStatusOK {
				t.Fatalf("UpdateNOC on fabric %d after the removed fabric's revert: %+v, %v; want OK", other, resp, err)
			}
		})
	}
}
