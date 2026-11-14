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

// listFailingStore is a store whose ListFabrics fails while fail is set:
// fabricAlreadyInstalled then reports no conflict and the duplicate pair
// reaches the store's own uniqueness verdict.
type listFailingStore struct {
	restartStore
	fail atomic.Bool
}

func (s *listFailingStore) ListFabrics(ctx context.Context) ([]mstore.FabricRecord, error) {
	if s.fail.Load() {
		return nil, errors.New("list: busy")
	}
	return s.restartStore.ListFabrics(ctx)
}

// TestOpcreds_RefusedAddNOCMarkerNeverRemovesACommittedFabric pins
// R2-P1-dr-sec-01 on the fabric row (owner decision 36): an AddNOC for a
// committed fabric's (FabricID, root) pair whose fabric list cannot be
// read is refused before it writes anything (R2-P2-comm-10). Whatever a
// crash at any write leaves, the next start keeps the committed fabric
// and finds no row left pending: a start rolls back pending rows only,
// never a fabric by its pair.
func TestOpcreds_RefusedAddNOCMarkerNeverRemovesACommittedFabric(t *testing.T) {
	t.Parallel()
	for name, open := range restartStores {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// crashAfter -1: no crash, the refusal leaves no marker.
			for crashAfter := -1; crashAfter <= 3; crashAfter++ {
				t.Run("", func(t *testing.T) {
					t.Parallel()
					testRefusedAddNOCKeepsCommitted(t, open(t), crashAfter)
				})
			}
		})
	}
}

func testRefusedAddNOCKeepsCommitted(t *testing.T, inner restartStore, crashAfter int) {
	t.Helper()
	ctx := context.Background()
	cs := &crashingStore{restartStore: inner, crashAfter: -1}
	st := &listFailingStore{restartStore: cs}
	oc, err := core.NewOperationalCredentials(st, core.OpcredsConfig{SupportedFabrics: 5, FailSafe: armedFailSafe(0)})
	if err != nil {
		t.Fatal(err)
	}
	rootRaw, rootPriv, idx := commissionTestFabric(ctx, t, oc)
	commitForTest(t, oc) // CommissioningComplete
	before, err := inner.GetIdentity(ctx, idx)
	if err != nil {
		t.Fatal(err)
	}
	// A new fail-safe; a second AddNOC for the same fabric under the same root.
	oc.ClearPendingState()
	if _, err := oc.MatterInvoke(ctx, 0x0B, core.AddTrustedRootCertificateRequest{RootCACertificate: rootRaw}); err != nil {
		t.Fatalf("AddTrustedRootCertificate: %v", err)
	}
	pub := issueCSRPendingPubKey(ctx, t, oc, false)
	noc := buildCoreSignedCertForPubKey(t, pub, false, rootPriv, testDefaultFabricID, testDefaultNodeID+1)
	cs.mu.Lock()
	if crashAfter >= 0 {
		cs.crashAfter = cs.writes + crashAfter
	}
	cs.mu.Unlock()
	st.fail.Store(true)
	resp, err := oc.MatterInvoke(ctx, 0x06, core.AddNOCRequest{NOCValue: noc, IPKValue: make([]byte, 16), CaseAdminSubject: 0xABCD, AdminVendorID: 0x1234})
	st.fail.Store(false)
	if nr, _ := resp.(core.NOCResponse); err == nil && nr.StatusCode == core.NOCStatusOK {
		t.Fatalf("AddNOC of a committed fabric's pair answered OK")
	}
	restartOpcreds(t, inner)
	after, err := inner.GetIdentity(ctx, idx)
	if err != nil {
		t.Fatalf("crash after write %d: the committed fabric %d lost its identity at the restart: %v", crashAfter, idx, err)
	}
	if _, err := inner.GetFabric(ctx, idx); err != nil {
		t.Fatalf("crash after write %d: the committed fabric %d did not survive the restart: %v", crashAfter, idx, err)
	}
	if !bytes.Equal(after.NOC, before.NOC) {
		t.Fatalf("crash after write %d: the committed fabric's NOC changed", crashAfter)
	}
	if acl, _ := inner.ListACL(ctx, idx); len(acl) != 1 {
		t.Fatalf("crash after write %d: the committed fabric has %d ACL entries after the restart, want 1", crashAfter, len(acl))
	}
	if fabrics, _ := inner.ListFabrics(ctx); len(fabrics) != 1 || fabrics[0].Pending != mstore.FabricCommitted {
		t.Fatalf("crash after write %d: fabrics after the restart = %+v, want the one committed fabric", crashAfter, fabrics)
	}
}

// nodeIDFailingStore is a store whose node id write — UpdateFabricNodeID,
// or UpdateNOC's UpdateFabricCredentials that carries it — fails while
// fail is set.
type nodeIDFailingStore struct {
	restartStore
	fail atomic.Bool
}

func (s *nodeIDFailingStore) UpdateFabricNodeID(ctx context.Context, fabricIndex uint8, nodeID uint64) error {
	if s.fail.Load() {
		return errors.New("node id: busy")
	}
	return s.restartStore.UpdateFabricNodeID(ctx, fabricIndex, nodeID)
}

func (s *nodeIDFailingStore) UpdateFabricCredentials(ctx context.Context, fabricIndex uint8, c mstore.FabricCredentials) error {
	if s.fail.Load() {
		return errors.New("node id: busy")
	}
	return s.restartStore.UpdateFabricCredentials(ctx, fabricIndex, c)
}

// TestOpcreds_RestartAfterFailedUpdateNOCAndAddNOC_RevertsBoth pins
// R2-P1-dr-sec-04 on the fabric row (owner decision 36): an UpdateNOC
// whose one write fails writes nothing and does not count as the
// fail-safe's NOC command, so an AddNOC in the same fail-safe is admitted.
// A node that goes down then keeps the updated fabric's credentials and
// removes the added fabric, whose row is pending.
func TestOpcreds_RestartAfterFailedUpdateNOCAndAddNOC_RevertsBoth(t *testing.T) {
	t.Parallel()
	for name, open := range restartStores {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			inner := open(t)
			st := &nodeIDFailingStore{restartStore: inner}
			oc, err := core.NewOperationalCredentials(st, core.OpcredsConfig{SupportedFabrics: 5, FailSafe: armedFailSafe(0)})
			if err != nil {
				t.Fatal(err)
			}
			_, rootPriv, idx := commissionTestFabric(ctx, t, oc)
			commitForTest(t, oc) // CommissioningComplete
			before, _ := inner.GetIdentity(ctx, idx)
			fabCtx := im.WithFabricFilter(ctx, true, idx)
			nocRaw := mintUpdateNOC(fabCtx, t, oc, rootPriv, testDefaultFabricID, testDefaultNodeID+7)
			st.fail.Store(true)
			resp, err := oc.MatterInvoke(fabCtx, 0x07, core.UpdateNOCRequest{NOCValue: nocRaw})
			st.fail.Store(false)
			if nr, _ := resp.(core.NOCResponse); err != nil || nr.StatusCode != core.NOCStatusInvalidNOC {
				t.Fatalf("UpdateNOC with a failing node id write: %+v, %v; want InvalidNOC", resp, err)
			}
			if after, _ := inner.GetIdentity(ctx, idx); !bytes.Equal(after.NOC, before.NOC) {
				t.Fatal("the failed UpdateNOC changed the identity")
			}
			resp, err = oc.MatterInvoke(fabCtx, 0x06, prepareAddNOC(fabCtx, t, oc))
			if nr, _ := resp.(core.NOCResponse); err != nil || nr.StatusCode != core.NOCStatusOK {
				t.Fatalf("AddNOC after the failed UpdateNOC: %+v, %v; want OK", resp, err)
			}
			if fabrics, _ := inner.ListFabrics(ctx); len(fabrics) != 2 {
				t.Fatalf("%d fabrics before the restart, want 2", len(fabrics))
			}

			restartOpcreds(t, inner)
			if fabrics, _ := inner.ListFabrics(ctx); len(fabrics) != 1 || fabrics[0].FabricIndex != idx {
				t.Fatalf("fabrics after the restart: %+v, want only the committed fabric %d", fabrics, idx)
			}
			after, err := inner.GetIdentity(ctx, idx)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(after.NOC, before.NOC) || !bytes.Equal(after.PrivateKey, before.PrivateKey) {
				t.Fatal("the restart left the updated fabric with other credentials than before the failed UpdateNOC")
			}
		})
	}
}
