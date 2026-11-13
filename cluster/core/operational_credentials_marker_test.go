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
// R2-P1-dr-sec-01: an AddNOC for a committed fabric's (FabricID, root)
// pair that reaches the store — ListFabrics failed, so the pair check saw
// no conflict — is refused by the store's uniqueness verdict. Whatever
// marker a crash at any of its writes leaves standing, the next start
// keeps the committed fabric: the marker names what this AddNOC wrote,
// never a fabric by its pair alone.
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
	oc.ClearPendingState() // CommissioningComplete
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
	if err != nil {
		t.Fatalf("AddNOC: %v", err)
	}
	if nr, _ := resp.(core.NOCResponse); nr.StatusCode == core.NOCStatusOK {
		t.Fatalf("AddNOC of a committed fabric's pair answered OK")
	}
	if crashAfter < 0 {
		if v, _, _ := inner.GetSetting(ctx, pendingNOCKey); v != "" {
			t.Fatalf("the refused AddNOC left the marker %s", v)
		}
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
	if v, _, _ := inner.GetSetting(ctx, pendingNOCKey); v != "" {
		t.Fatalf("crash after write %d: the restart left the marker %s", crashAfter, v)
	}
}
