// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/im"
	mstore "github.com/SukramJ/go-fabric/store"
)

// midUpdateStore runs during once, in the GetIdentity UpdateNOC makes
// between its read of the fabric row and its one write.
type midUpdateStore struct {
	restartStore
	during atomic.Pointer[func()]
}

func (s *midUpdateStore) GetIdentity(ctx context.Context, fabricIndex uint8) (mstore.IdentityRecord, error) {
	if f := s.during.Swap(nil); f != nil {
		(*f)()
	}
	return s.restartStore.GetIdentity(ctx, fabricIndex)
}

// TestOpcreds_UpdateNOCKeepsAVendorIDWrittenDuringIt is R2-P2-dr-noc-02:
// a vendor id written while an UpdateNOC runs — after it read the fabric
// row, before its write — is the fabric's once the update commits. The
// update writes what it changes only, not the vendor id it read.
func TestOpcreds_UpdateNOCKeepsAVendorIDWrittenDuringIt(t *testing.T) {
	t.Parallel()
	for name, open := range restartStores {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			st := &midUpdateStore{restartStore: open(t)}
			oc, err := core.NewOperationalCredentials(st, core.OpcredsConfig{SupportedFabrics: 5, FailSafe: armedFailSafe(0)})
			if err != nil {
				t.Fatal(err)
			}
			_, rootPriv, idx := commissionTestFabric(ctx, t, oc)
			commitForTest(t, oc)
			fabCtx := im.WithFabricFilter(ctx, true, idx)
			nocRaw := mintUpdateNOC(fabCtx, t, oc, rootPriv, testDefaultFabricID, testDefaultNodeID+7)
			const written = 0x1357
			during := func() {
				if err := st.UpdateFabricVendorID(ctx, idx, written); err != nil {
					t.Errorf("UpdateFabricVendorID: %v", err)
				}
			}
			st.during.Store(&during)
			if resp, err := oc.MatterInvoke(fabCtx, 0x07, core.UpdateNOCRequest{NOCValue: nocRaw}); err != nil || resp.(core.NOCResponse).StatusCode != core.NOCStatusOK {
				t.Fatalf("UpdateNOC: %+v, %v", resp, err)
			}
			if st.during.Load() != nil {
				t.Fatal("the write between UpdateNOC's read and its write did not run")
			}
			commitForTest(t, oc) // CommissioningComplete
			fab, err := st.GetFabric(ctx, idx)
			if err != nil {
				t.Fatal(err)
			}
			if fab.VendorID != written {
				t.Fatalf("vendor id after the committed UpdateNOC = %#x, want %#x (written during it)", fab.VendorID, written)
			}
		})
	}
}
