// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"bytes"
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/im"
	mstore "github.com/SukramJ/go-fabric/store"
)

// A fail-safe's rollback that the store refuses, on the fabric row (owner
// decision 36): the row stays pending — listed, served and rolled back by
// the next fresh arm, fail-safe end or start — instead of an owed revert
// in memory and a marker in the settings (review II, R2-P1-dr-sec-02).

var errFault = errors.New("store fault")

// fabricCredentials is what an UpdateNOC and a VID statement inside its
// fail-safe change, and the rollback must put back.
type fabricCredentials struct {
	noc, key        []byte
	nodeID          uint64
	vendorID        uint16
	statement, vvsc []byte
}

func readCredentials(ctx context.Context, t *testing.T, st restartStore, idx uint8) fabricCredentials {
	t.Helper()
	id, err := st.GetIdentity(ctx, idx)
	if err != nil {
		t.Fatal(err)
	}
	fab, err := st.GetFabric(ctx, idx)
	if err != nil {
		t.Fatal(err)
	}
	return fabricCredentials{
		noc: id.NOC, key: id.PrivateKey, nodeID: fab.NodeID, vendorID: fab.VendorID,
		statement: fab.VIDVerificationStatement, vvsc: fab.VVSC,
	}
}

func (c fabricCredentials) equal(o fabricCredentials) bool {
	return bytes.Equal(c.noc, o.noc) && bytes.Equal(c.key, o.key) && c.nodeID == o.nodeID &&
		c.vendorID == o.vendorID && bytes.Equal(c.statement, o.statement) && bytes.Equal(c.vvsc, o.vvsc)
}

// pendingOf is the commit state of fabric idx.
func pendingOf(ctx context.Context, t *testing.T, st restartStore, idx uint8) mstore.FabricPending {
	t.Helper()
	fab, err := st.GetFabric(ctx, idx)
	if err != nil {
		t.Fatal(err)
	}
	return fab.Pending
}

// rollbackFailingStore is a store whose rollback fails while fail is set:
// RollbackFabric, and the reads and writes of fail.
type rollbackFailingStore struct {
	restartStore
	fail atomic.Bool
}

func (s *rollbackFailingStore) RollbackFabric(ctx context.Context, fabricIndex uint8) (mstore.FabricPending, error) {
	if s.fail.Load() {
		return mstore.FabricCommitted, errFault
	}
	return s.restartStore.RollbackFabric(ctx, fabricIndex)
}

// identityFailingStore also fails UpsertIdentity, removeFailingStore
// RemoveFabric: the writes the rollback made before the row carried its
// state.
type identityFailingStore struct{ rollbackFailingStore }

func (s *identityFailingStore) UpsertIdentity(ctx context.Context, rec mstore.IdentityRecord) error {
	if s.fail.Load() {
		return errFault
	}
	return s.restartStore.UpsertIdentity(ctx, rec)
}

type removeFailingStore struct{ rollbackFailingStore }

func (s *removeFailingStore) RemoveFabric(ctx context.Context, fabricIndex uint8) error {
	if s.fail.Load() {
		return errFault
	}
	return s.restartStore.RemoveFabric(ctx, fabricIndex)
}

// updateInFailSafe commits a fabric, then updates its NOC and sets a VID
// statement and a vendor id inside the update's fail-safe; it returns the
// fabric and its credentials before the update.
func updateInFailSafe(ctx context.Context, t *testing.T, oc *core.OperationalCredentials, st restartStore) (uint8, fabricCredentials) {
	t.Helper()
	_, rootPriv, idx := commissionTestFabric(ctx, t, oc)
	commitForTest(t, oc) // CommissioningComplete
	before := readCredentials(ctx, t, st, idx)
	fabCtx := im.WithFabricFilter(ctx, true, idx)
	nocRaw := mintUpdateNOC(fabCtx, t, oc, rootPriv, testDefaultFabricID, testDefaultNodeID+7)
	if resp, err := oc.MatterInvoke(fabCtx, 0x07, core.UpdateNOCRequest{NOCValue: nocRaw}); err != nil || resp.(core.NOCResponse).StatusCode != core.NOCStatusOK {
		t.Fatalf("UpdateNOC: %+v, %v", resp, err)
	}
	if _, err := oc.MatterInvoke(fabCtx, 0x0C, core.SetVidVerificationStatementRequest{
		HasVendorID: true, VendorID: 0x1235,
		HasVidVerificationStatement: true, VidVerificationStatement: bytes.Repeat([]byte{7}, 85),
	}); err != nil {
		t.Fatalf("SetVIDVerificationStatement: %v", err)
	}
	if readCredentials(ctx, t, st, idx).equal(before) {
		t.Fatal("the update moved nothing: the rollback has nothing to restore")
	}
	return idx, before
}

// TestOpcreds_FailedUpdateRevertKeepsTheMarker pins R2-P1-dr-sec-02 on the
// row: the fail-safe's rollback of an UpdateNOC — with a VID statement and
// a new vendor id set inside the same fail-safe — meets a store fault.
// The fabric then still holds the update and its row stays pending; the
// second attempt — the next start, or the next fresh arm in the same run —
// restores every value and commits the row.
func TestOpcreds_FailedUpdateRevertKeepsTheMarker(t *testing.T) {
	t.Parallel()
	for name, open := range restartStores {
		for _, second := range []string{"restart", "arm"} {
			t.Run(name+"/"+second, func(t *testing.T) {
				t.Parallel()
				ctx := context.Background()
				inner := open(t)
				st := &rollbackFailingStore{restartStore: inner}
				oc, err := core.NewOperationalCredentials(st, core.OpcredsConfig{SupportedFabrics: 5, FailSafe: armedFailSafe(0)})
				if err != nil {
					t.Fatal(err)
				}
				idx, before := updateInFailSafe(ctx, t, oc, inner)
				st.fail.Store(true)
				oc.OnFailSafeExpiry(ctx, idx)
				st.fail.Store(false)
				if got := pendingOf(ctx, t, inner, idx); got != mstore.FabricPendingUpdate {
					t.Fatalf("after the failed rollback the row is %v, want pending update", got)
				}
				if second == "restart" {
					restartOpcreds(t, inner)
				} else {
					oc.ClearPendingState()
				}
				if got := readCredentials(ctx, t, inner, idx); !got.equal(before) {
					t.Fatalf("after the %s the fabric holds %+v, want %+v", second, got, before)
				}
				if got := pendingOf(ctx, t, inner, idx); got != mstore.FabricCommitted {
					t.Fatalf("after the %s the row is %v, want committed", second, got)
				}
			})
		}
	}
}

// TestOpcreds_OwedUpdateRevertOutlivesTheNextCommissioning is the in-run
// half of R2-P1-dr-sec-02 on the row: an UpdateNOC rollback the fail-safe's
// end could not land stays pending through the next fail-safe. Its retry
// at the fresh arm fails again; that window commissions another fabric,
// and its CommissioningComplete commits only that fabric's row — never the
// stale update's. Once the store recovers, the next arm lands the rollback.
func TestOpcreds_OwedUpdateRevertOutlivesTheNextCommissioning(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	inner := restartStores["memstore"](t)
	st := &rollbackFailingStore{restartStore: inner}
	oc, err := core.NewOperationalCredentials(st, core.OpcredsConfig{SupportedFabrics: 5, FailSafe: armedFailSafe(0)})
	if err != nil {
		t.Fatal(err)
	}
	idx, before := updateInFailSafe(ctx, t, oc, inner)
	st.fail.Store(true)
	oc.OnFailSafeExpiry(ctx, idx)
	oc.SetFailSafe(armedFailSafe(0))
	oc.ClearPendingState() // the next fresh arm: the retry fails again
	_, _, other := commissionTestFabric(ctx, t, oc)
	commitForTest(t, oc) // that window's CommissioningComplete
	if got := pendingOf(ctx, t, inner, other); got != mstore.FabricCommitted {
		t.Fatalf("the commissioned fabric %d is %v, want committed", other, got)
	}
	if got := pendingOf(ctx, t, inner, idx); got != mstore.FabricPendingUpdate {
		t.Fatalf("the stale update of fabric %d is %v after another fabric's commit, want still pending", idx, got)
	}
	st.fail.Store(false)
	oc.ClearPendingState() // the next arm, over a healthy store
	if got := readCredentials(ctx, t, inner, idx); !got.equal(before) {
		t.Fatalf("after the healthy arm the fabric holds %+v, want %+v", got, before)
	}
}

// TestOpcreds_OwedAddRevertKeepsItsMarker is the AddNOC side of
// R2-P1-dr-sec-02 on the row: a fail-safe's end whose rollback of the
// added fabric fails leaves its row pending; the next fresh arm retries
// it and fails again; the one after it, over a healthy store, removes the
// fabric — and a start would have, too.
func TestOpcreds_OwedAddRevertKeepsItsMarker(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	inner := restartStores["memstore"](t)
	st := &removeFailingStore{rollbackFailingStore{restartStore: inner}}
	oc, err := core.NewOperationalCredentials(st, core.OpcredsConfig{SupportedFabrics: 5, FailSafe: armedFailSafe(0)})
	if err != nil {
		t.Fatal(err)
	}
	_, _, idx := commissionTestFabric(ctx, t, oc)
	st.fail.Store(true)
	oc.OnFailSafeExpiry(ctx, idx)
	oc.ClearPendingState() // the next fresh arm: the removal fails again
	if got := pendingOf(ctx, t, inner, idx); got != mstore.FabricPendingAdd {
		t.Fatalf("after the failed rollbacks the row is %v, want pending add", got)
	}
	st.fail.Store(false)
	oc.ClearPendingState()
	if _, err := inner.GetFabric(ctx, idx); !errors.Is(err, mstore.ErrFabricNotFound) {
		t.Fatalf("the healthy arm left the uncommitted fabric: %v", err)
	}
}
