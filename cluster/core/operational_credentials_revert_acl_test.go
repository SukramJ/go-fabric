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
	mstore "github.com/SukramJ/go-fabric/store"
)

// aclRevertStore fails UpsertIdentity — the first AddNOC step after
// AddFabric has already allocated a FabricIndex — and records which index
// was allocated, so the test never has to assume the allocation order of
// the fake store.
type aclRevertStore struct {
	*fakeStore
	upsertIdentityErr error
	allocatedIndex    uint8
}

func (s *aclRevertStore) AddFabric(ctx context.Context, rec mstore.FabricRecord) (uint8, error) {
	idx, err := s.fakeStore.AddFabric(ctx, rec)
	s.allocatedIndex = idx
	return idx, err
}

func (s *aclRevertStore) UpsertIdentity(_ context.Context, _ mstore.IdentityRecord) error {
	return s.upsertIdentityErr
}

// TestPin_RevertAddNOC_ACLCleanup pins that the AddNOC rollback path wipes
// the Access Control entries belonging to the reverted FabricIndex.
//
// A failed AddNOC that leaves ACL rows behind is a live authorization
// hole: FabricIndex values are reused, so the next commissioner to be
// allocated that index inherits Administer privileges granted to the
// CaseAdminSubject of a fabric that never finished commissioning. Mirrors
// chip operational-credentials-server.cpp HandleAddNOC needRevert step 3
// (accessControl.DeleteAllEntriesForFabric).
//
// The store is seeded with ACL rows on several fabric indices before the
// failing AddNOC runs. The rows on the untouched indices are the negative
// control: they must survive, which proves both that the seed is real and
// that the cleanup is scoped to the reverted fabric rather than clearing
// the table wholesale.
func TestPin_RevertAddNOC_ACLCleanup(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	inner := newFakeStore()
	wrapped := &aclRevertStore{
		fakeStore:         inner,
		upsertIdentityErr: errors.New("injected UpsertIdentity failure"),
	}

	// Seed ACL rows on the indices AddFabric can hand out first. The fake
	// store's RemoveFabric deliberately does not cascade into the ACL
	// table, so only an explicit cleanup can remove these.
	const seededIndices = 3
	for fi := uint8(1); fi <= seededIndices; fi++ {
		if err := inner.ReplaceACL(ctx, fi, []mstore.ACLEntry{{
			FabricIndex: fi,
			Privilege:   mstore.PrivilegeAdminister,
			AuthMode:    mstore.AuthModeCASE,
			Subjects:    []uint64{0x0001_0203_0405_0607},
		}}); err != nil {
			t.Fatalf("seed ReplaceACL(fi=%d): %v", fi, err)
		}
	}

	oc, err := core.NewOperationalCredentials(wrapped, core.OpcredsConfig{SupportedFabrics: 5})
	if err != nil {
		t.Fatalf("NewOperationalCredentials: %v", err)
	}
	oc.SetIsFailSafeArmed(func() bool { return true })

	rootPriv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey root: %v", err)
	}
	rootRaw := buildCoreSignedCert(t, rootPriv, true, rootPriv)

	if _, err := oc.MatterInvoke(ctx, 0x0B,
		core.AddTrustedRootCertificateRequest{RootCACertificate: rootRaw}); err != nil {
		t.Fatalf("AddTrustedRootCertificate: %v", err)
	}

	// The NOC must certify the cluster's real pending CSR key, or AddNOC
	// is rejected before AddFabric and no rollback happens at all.
	pendingPub := issueCSRPendingPubKey(ctx, t, oc, false)
	nocRaw := buildCoreSignedCertForPubKey(t, pendingPub, false, rootPriv, testDefaultFabricID, testDefaultNodeID)

	ipk := make([]byte, 16)
	for i := range ipk {
		ipk[i] = 0xAA
	}
	resp, err := oc.MatterInvoke(ctx, 0x06, core.AddNOCRequest{
		NOCValue:         nocRaw,
		IPKValue:         ipk,
		CaseAdminSubject: 0x0001_0203_0405_0607,
		AdminVendorID:    0x1234,
	})
	if err != nil {
		t.Fatalf("AddNOC: unexpected IM error: %v", err)
	}
	nocResp, ok := resp.(core.NOCResponse)
	if !ok {
		t.Fatalf("AddNOC response type = %T, want core.NOCResponse", resp)
	}
	if nocResp.StatusCode == core.NOCStatusOK {
		t.Fatal("AddNOC: expected failure (injected UpsertIdentity error), got OK")
	}

	reverted := wrapped.allocatedIndex
	if reverted < 1 || reverted > seededIndices {
		t.Fatalf("AddFabric allocated index %d, outside the seeded range 1..%d — the fixture cannot measure the cleanup",
			reverted, seededIndices)
	}

	entries, err := inner.ListACL(ctx, reverted)
	if err != nil {
		t.Fatalf("ListACL(fi=%d): %v", reverted, err)
	}
	if len(entries) != 0 {
		t.Errorf("ACL entries for reverted fabricIndex=%d: got %d, want 0 — rollback left admin grants behind",
			reverted, len(entries))
	}

	for fi := uint8(1); fi <= seededIndices; fi++ {
		if fi == reverted {
			continue
		}
		other, lerr := inner.ListACL(ctx, fi)
		if lerr != nil {
			t.Fatalf("ListACL(fi=%d): %v", fi, lerr)
		}
		if len(other) == 0 {
			t.Errorf("ACL entries for untouched fabricIndex=%d were removed; the rollback must be fabric-scoped", fi)
		}
	}
}
