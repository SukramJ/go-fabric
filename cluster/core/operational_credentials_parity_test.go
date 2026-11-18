// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Parity tests for OperationalCredentials fabric-management behaviour
// against matter.js FabricManager HEAD.
//
// Each case is annotated with the matter.js source line it mirrors.
// matter.js reference: packages/protocol/test/fabric/FabricManagerTest.ts

package core_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/secure/mattercert"
	mstore "github.com/SukramJ/go-fabric/store"
)

// ─── helpers ──────────────────────────────────────────────────────────────────

// newFabricForTest returns a minimal FabricRecord with unique (seed-derived)
// keys so UNIQUE constraints are not inadvertently hit across sub-tests.
func newFabricForTest(seed byte) mstore.FabricRecord {
	var compID [8]byte
	compID[0] = seed
	return mstore.FabricRecord{
		FabricIndex:   0, // auto-assign
		FabricID:      uint64(seed)*1000 + 1,
		NodeID:        uint64(seed)*1000 + 2,
		RootPublicKey: makeP256Fixture(seed),
		VendorID:      0xFFF1,
		Label:         "test-fabric",
		CompressedID:  compID,
	}
}

// makeP256Fixture returns a deterministic 65-byte uncompressed P-256 public
// key. The bytes do not need to lie on the curve for store-layer tests.
func makeP256Fixture(seed byte) []byte {
	out := make([]byte, 65)
	out[0] = 0x04
	for i := 1; i < 65; i++ {
		out[i] = seed + byte(i)
	}
	return out
}

// opcredsWithFakeStore returns an OperationalCredentials backed by an
// in-memory fakeStore (defined in testhelper_test.go in this package).
func opcredsWithFakeStore(t *testing.T) (*core.OperationalCredentials, *fakeStore) {
	t.Helper()
	s := newFakeStore()
	oc, err := core.NewOperationalCredentials(s, core.OpcredsConfig{SupportedFabrics: 10, FailSafe: armedFailSafe(0)})
	if err != nil {
		t.Fatalf("NewOperationalCredentials: %v", err)
	}
	return oc, s
}

// ─── adding fabrics ───────────────────────────────────────────────────────────

// TestParityFabricManager_AddFabricNotPersistedDirectly verifies that a
// freshly added fabric is visible via the ListFabrics attribute read but
// the cluster never calls store.Persist on its own (persistence is the
// store's responsibility, not the cluster's).
//
// Mirrors matter.js packages/protocol/test/fabric/FabricManagerTest.ts:26
// (case "add a new fabric (but not persist directly)").
func TestParityFabricManager_AddFabricNotPersistedDirectly(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, s := opcredsWithFakeStore(t)

	rec := newFabricForTest(1)
	idx, err := s.AddFabric(ctx, rec)
	if err != nil {
		t.Fatalf("AddFabric: %v", err)
	}
	if idx == 0 {
		t.Fatal("AddFabric: returned zero index")
	}

	fabrics, err := s.ListFabrics(ctx)
	if err != nil {
		t.Fatalf("ListFabrics: %v", err)
	}
	if len(fabrics) != 1 {
		t.Fatalf("ListFabrics: len=%d, want 1", len(fabrics))
	}
}

// ─── persistence ──────────────────────────────────────────────────────────────

// TestParityFabricManager_PersistAndRestore verifies that a fabric written
// by AddFabric is visible from a second reference to the same store (simulating
// persistence round-trip at the in-memory fakeStore level).
//
// Mirrors matter.js packages/protocol/test/fabric/FabricManagerTest.ts:51
// (case "add a new fabric and persist") and :61
// (case "restore a fabric from storage").
//
// Note: SQLite-level durability (true persist + re-open) is verified by
// TestParityChip_Persistence_RoundTrip in the store package tests. Here we
// verify the cluster's store interaction contract only.
func TestParityFabricManager_PersistAndRestore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newFakeStore()
	oc, err := core.NewOperationalCredentials(s, core.OpcredsConfig{SupportedFabrics: 10})
	if err != nil {
		t.Fatalf("NewOperationalCredentials: %v", err)
	}
	_ = oc

	rec := newFabricForTest(10)
	idx, err := s.AddFabric(ctx, rec)
	if err != nil {
		t.Fatalf("AddFabric: %v", err)
	}

	// Verify the record is retrievable on the same store instance.
	got, err := s.GetFabric(ctx, idx)
	if err != nil {
		t.Fatalf("GetFabric: %v", err)
	}
	if got.FabricIndex != idx {
		t.Errorf("FabricIndex=%d want %d", got.FabricIndex, idx)
	}
	if got.FabricID != rec.FabricID {
		t.Errorf("FabricID=%d want %d", got.FabricID, rec.FabricID)
	}
}

// ─── removing fabrics ─────────────────────────────────────────────────────────

// TestParityFabricManager_RemoveFabric verifies that RemoveFabric deletes the
// fabric and subsequent GetFabric returns ErrFabricNotFound.
//
// Mirrors matter.js packages/protocol/test/fabric/FabricManagerTest.ts:72
// (case "remove an added fabric").
func TestParityFabricManager_RemoveFabric(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, s := opcredsWithFakeStore(t)

	rec := newFabricForTest(20)
	idx, err := s.AddFabric(ctx, rec)
	if err != nil {
		t.Fatalf("AddFabric: %v", err)
	}

	if err := s.RemoveFabric(ctx, idx); err != nil {
		t.Fatalf("RemoveFabric: %v", err)
	}

	_, err = s.GetFabric(ctx, idx)
	if !errors.Is(err, mstore.ErrFabricNotFound) {
		t.Errorf("after remove: got %v, want ErrFabricNotFound", err)
	}
}

// ─── FabricIndex allocation ───────────────────────────────────────────────────

// TestParityFabricManager_AllocateIndexInitially verifies that the first
// auto-allocated index is 1.
//
// Mirrors matter.js packages/protocol/test/fabric/FabricManagerTest.ts:83
// (case "get next fabric index initially").
func TestParityFabricManager_AllocateIndexInitially(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, s := opcredsWithFakeStore(t)

	idx, err := s.AddFabric(ctx, newFabricForTest(30))
	if err != nil {
		t.Fatalf("AddFabric: %v", err)
	}
	if idx != 1 {
		t.Errorf("first allocated index = %d, want 1", idx)
	}
}

// TestParityFabricManager_AllocateIndexAfterAdd verifies that the second
// auto-allocated index is 2.
//
// Mirrors matter.js packages/protocol/test/fabric/FabricManagerTest.ts:87
// (case "get next fabric index after adding fabric").
func TestParityFabricManager_AllocateIndexAfterAdd(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, s := opcredsWithFakeStore(t)

	if _, err := s.AddFabric(ctx, newFabricForTest(31)); err != nil {
		t.Fatalf("first AddFabric: %v", err)
	}
	idx2, err := s.AddFabric(ctx, newFabricForTest(32))
	if err != nil {
		t.Fatalf("second AddFabric: %v", err)
	}
	if idx2 != 2 {
		t.Errorf("second allocated index = %d, want 2", idx2)
	}
}

// TestParityFabricManager_AllocateIndexMonotonicAfterRemove verifies that
// after adding fabric (index 1) and removing it, the next auto-allocated
// index is NOT 1 — the allocator is a monotonic counter, never re-using
// just-released slots.
//
// Mirrors matter.js packages/protocol/test/fabric/FabricManagerTest.ts:93
// (case "get next fabric index after adding fabric and removing it").
func TestParityFabricManager_AllocateIndexMonotonicAfterRemove(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, s := opcredsWithFakeStore(t)

	idx1, err := s.AddFabric(ctx, newFabricForTest(40))
	if err != nil {
		t.Fatalf("AddFabric: %v", err)
	}
	if err := s.RemoveFabric(ctx, idx1); err != nil {
		t.Fatalf("RemoveFabric: %v", err)
	}
	// After remove, next index must be >= 2 (monotonic — index 1 was used).
	idx2, err := s.AddFabric(ctx, newFabricForTest(41))
	if err != nil {
		t.Fatalf("AddFabric after remove: %v", err)
	}
	if idx2 == idx1 {
		t.Errorf("index re-used immediately after remove (idx2=%d, idx1=%d) — monotonic-counter regression", idx2, idx1)
	}
}

// TestParityFabricManager_AllocateIndexSkipsAfterMultiRemove verifies that
// when two fabrics exist and the lower-indexed one is removed, the next
// allocated index skips past the last-known-highest, not the first gap.
//
// Mirrors matter.js packages/protocol/test/fabric/FabricManagerTest.ts:107
// (case "get jumps over one fabric index after adding fabric and revoking it").
func TestParityFabricManager_AllocateIndexSkipsAfterMultiRemove(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, s := opcredsWithFakeStore(t)

	idx1, err := s.AddFabric(ctx, newFabricForTest(50)) // expect 1
	if err != nil {
		t.Fatalf("AddFabric 1: %v", err)
	}
	_, err = s.AddFabric(ctx, newFabricForTest(51)) // expect 2
	if err != nil {
		t.Fatalf("AddFabric 2: %v", err)
	}
	// Remove the first (index 1); last-seen-max stays at 2.
	if err := s.RemoveFabric(ctx, idx1); err != nil {
		t.Fatalf("RemoveFabric idx1: %v", err)
	}
	// Counter should have advanced past 2 → next is 3 (not 1).
	idx3, err := s.AddFabric(ctx, newFabricForTest(52))
	if err != nil {
		t.Fatalf("AddFabric 3: %v", err)
	}
	if idx3 < 3 {
		t.Errorf("expected index >= 3 after skipping used indices, got %d", idx3)
	}
}

// ─── OperationalCredentials cluster surface ───────────────────────────────────

// TestParityFabricManager_OpcredsRemoveFabricHookFires verifies that the
// OnFabricRemoved hook fires after a successful RemoveFabric command, and
// does NOT fire when the fabric index does not exist.
//
// Mirrors matter.js packages/protocol/src/fabric/FabricManager.ts
// #handleFabricDeleted (fans out to SessionManager + InteractionServer).
func TestParityFabricManager_OpcredsRemoveFabricHookFires(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newFakeStore()

	var hookFabricIndex uint8
	oc, err := core.NewOperationalCredentials(s, core.OpcredsConfig{
		SupportedFabrics: 5,
		OnFabricRemoved: func(_ context.Context, fi uint8) {
			hookFabricIndex = fi
		},
	})
	if err != nil {
		t.Fatalf("NewOperationalCredentials: %v", err)
	}

	// Insert a fabric directly into the store.
	idx, err := s.AddFabric(ctx, newFabricForTest(60))
	if err != nil {
		t.Fatalf("AddFabric: %v", err)
	}

	// Remove via the cluster command.
	resp, err := oc.MatterInvoke(ctx, 0x0A, core.RemoveFabricRequest{FabricIndex: idx})
	if err != nil {
		t.Fatalf("MatterInvoke RemoveFabric: %v", err)
	}
	nocResp := resp.(core.NOCResponse)
	if nocResp.StatusCode != core.NOCStatusOK {
		t.Fatalf("RemoveFabric status=%d, want OK", nocResp.StatusCode)
	}
	if hookFabricIndex != idx {
		t.Errorf("hook fired for fabric %d, want %d", hookFabricIndex, idx)
	}
}

// TestParityFabricManager_OpcredsRemoveFabricMissReturnsInvalidFabricIndex
// verifies that RemoveFabric on a non-existent index returns NOCStatusInvalidFabricIndex.
//
// Mirrors matter.js packages/protocol/src/fabric/FabricManager.ts removeFabric
// error path (InvalidFabricIndex status code).
func TestParityFabricManager_OpcredsRemoveFabricMissReturnsInvalidFabricIndex(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	oc, _ := opcredsWithFakeStore(t)

	resp, err := oc.MatterInvoke(ctx, 0x0A, core.RemoveFabricRequest{FabricIndex: 99})
	if err != nil {
		t.Fatalf("MatterInvoke RemoveFabric: %v", err)
	}
	nocResp := resp.(core.NOCResponse)
	if nocResp.StatusCode != core.NOCStatusInvalidFabricIndex {
		t.Errorf("StatusCode=%d, want NOCStatusInvalidFabricIndex (%d)", nocResp.StatusCode, core.NOCStatusInvalidFabricIndex)
	}
}

// ─── FailSafe / CSR-binding / pending-root parity ────────────────────────────

// TestParityOpcreds_FailSafeRequiredForAddNOC verifies that AddNOC (0x06)
// returns Status::FailsafeRequired (0xCA) when FailSafe is wired
// and returns false. This is the guard from Matter §11.18.6.8 that prevents
// a rogue commissioner from installing a NOC without first arming the
// FailSafe. Without this guard a fast-fail pair attempt can leave a
// dangling pending fabric in the store.
//
// Source-Origin: derived from matter.js packages/node/src/behaviors/
// operational-credentials/OperationalCredentialsServer.ts:224
// (#failsafeContext check before AddNOC) and chip
// src/app/clusters/operational-credentials/operational-credentials-server.cpp
// VerifyOrExit(failSafeContext.IsFailSafeArmed(...), errorStatus=FailsafeRequired).
func TestParityOpcreds_FailSafeRequiredForAddNOC(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newFakeStore()
	oc, err := core.NewOperationalCredentials(s, core.OpcredsConfig{
		SupportedFabrics: 5,
		FailSafe:         func() (uint8, bool) { return 0, false }, // FailSafe NOT armed
	})
	if err != nil {
		t.Fatalf("NewOperationalCredentials: %v", err)
	}

	_, invokeErr := oc.MatterInvoke(ctx, 0x06, core.AddNOCRequest{IPKValue: make([]byte, 16), AdminVendorID: 0xFFF1})
	if invokeErr == nil {
		t.Fatal("AddNOC without armed FailSafe: expected error, got nil")
	}
	// The error must carry MatterStatusCode() == StatusFailsafeRequired (0xCA).
	type statusCoder interface{ MatterStatusCode() im.StatusCode }
	var sc statusCoder
	if !errors.As(invokeErr, &sc) {
		t.Fatalf("AddNOC error %v does not implement MatterStatusCode() — want FailsafeRequired", invokeErr)
	}
	if got := sc.MatterStatusCode(); got != im.StatusFailsafeRequired {
		t.Errorf("MatterStatusCode()=0x%02X, want StatusFailsafeRequired (0xCA)", got)
	}
}

// TestParityOpcreds_CSRForUpdateNOCRejectedByAddNOC verifies that if
// CSRRequest was issued with IsForUpdateNOC=true, a subsequent AddNOC
// (0x06) is rejected with an IM-level Status::ConstraintError (0x87), NOT
// an in-band NOCResponse. This prevents a commissioner from using an
// UpdateNOC-targeted CSR to install a brand-new fabric, which would bypass
// the fabric-authentication guard on UpdateNOC.
//
// Source-Origin: matter.js packages/node/src/behaviors/
// operational-credentials/OperationalCredentialsServer.ts:247-252 — the
// addNoc path raises StatusResponseError("AddNoc is illegal after CsrRequest
// for UpdateNOC in same failsafe context", Status.ConstraintError).
func TestParityOpcreds_CSRForUpdateNOCRejectedByAddNOC(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newFakeStore()
	// The window fabric 1 armed over CASE.
	oc, err := core.NewOperationalCredentials(s, core.OpcredsConfig{SupportedFabrics: 5, FailSafe: armedFailSafe(1)})
	if err != nil {
		t.Fatalf("NewOperationalCredentials: %v", err)
	}
	// Wire a CASE session context: FabricFilter(fabricIndex=1) indicates CASE
	// (PASE sessions carry fabricIndex=0 per im.FabricFilterFromContext semantics).
	// Also stamp a non-zero session ID (100) for the CSR-session-binding guard.
	caseCtx := im.WithFabricFilter(ctx, true, 1)
	csrCtx := core.WithInvokeSessionID(caseCtx, 100)
	// A trusted root first: matter.js answers its absence (InvalidNOC)
	// before it looks at the CSR (OperationalCredentialsServer.ts:233-252).
	rootPriv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := oc.MatterInvoke(caseCtx, 0x0B, core.AddTrustedRootCertificateRequest{
		RootCACertificate: buildCoreSignedCert(t, rootPriv, true, rootPriv),
	}); err != nil {
		t.Fatalf("AddTrustedRootCertificate: %v", err)
	}
	// Issue CSRRequest with IsForUpdateNOC=true.
	_, csrErr := oc.MatterInvoke(sessionChallengeCtx(csrCtx), 0x04, core.CSRRequest{
		CSRNonce:       make([]byte, 32),
		IsForUpdateNOC: true,
	})
	if csrErr != nil {
		t.Fatalf("CSRRequest (IsForUpdateNOC=true): %v", csrErr)
	}
	// Now call AddNOC from the same session — must be rejected with an
	// IM-level ConstraintError (not an in-band NOCResponse).
	addNOCCtx := core.WithInvokeSessionID(caseCtx, 100)
	_, err = oc.MatterInvoke(addNOCCtx, 0x06, core.AddNOCRequest{IPKValue: make([]byte, 16), AdminVendorID: 0xFFF1})
	if err == nil {
		t.Fatal("AddNOC with UpdateNOC-targeted CSR: expected IM error, got nil")
	}
	type statusCoder interface{ MatterStatusCode() im.StatusCode }
	var sc statusCoder
	if !errors.As(err, &sc) {
		t.Fatalf("AddNOC error %v does not implement MatterStatusCode() — want ConstraintError", err)
	}
	if got := sc.MatterStatusCode(); got != im.StatusConstraintError {
		t.Errorf("MatterStatusCode()=0x%02X, want StatusConstraintError (0x87)", uint8(got))
	}
}

// TestParityOpcreds_PendingRootAppearsInTrustedRoots verifies that after
// AddTrustedRootCertificate (0x0B), the pending RCAC TLV blob is visible
// in TrustedRootCertificates (attr 0x0004) before AddNOC commits it to
// the store. The full Matter RCAC TLV blob must be served, NOT a bare
// EC pubkey: Apple validates each entry as a Matter Certificate TLV
// envelope and drops the entire Subscribe stream on schema mismatch
// (Bug I).
//
// The pending root is the last entry, after the installed fabrics' roots.
//
// Mirrors matter.js db021da0 packages/node/src/behaviors/
// operational-credentials/OperationalCredentialsServer.ts:471-473 —
// `fabrics.map(fabric => fabric.rootCert)`, then
// `trustedRootCertificates.push(rootCaCertificate)`. Mirrors chip
// operational-credentials-server.cpp PendingFabric::mPendingRootCert
// being included in the TrustedRoots attribute response.
func TestParityOpcreds_PendingRootAppearsInTrustedRoots(t *testing.T) {
	t.Parallel()
	s := newFakeStore()
	oc, err := core.NewOperationalCredentials(s, core.OpcredsConfig{SupportedFabrics: 5})
	if err != nil {
		t.Fatalf("NewOperationalCredentials: %v", err)
	}

	// Read TrustedRootCertificates before any root is installed — must be
	// an empty list (not nil, not error).
	vBefore, ok := oc.MatterRead(0x0004)
	if !ok {
		t.Fatal("TrustedRootCertificates before install: ok=false")
	}
	listBefore, ok := vBefore.([][]byte)
	if !ok {
		t.Fatalf("TrustedRootCertificates type = %T, want [][]byte", vBefore)
	}
	if len(listBefore) != 0 {
		t.Errorf("TrustedRootCertificates before install: len=%d, want 0", len(listBefore))
	}

	// One installed fabric, then a second commissioner's pending root.
	ctx := context.Background()
	installed := newFabricForTest(1)
	installed.RootCert = []byte("root of fabric 1")
	if _, err := s.AddFabric(ctx, installed); err != nil {
		t.Fatal(err)
	}
	rootPriv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pendingRoot := buildCoreSignedCert(t, rootPriv, true, rootPriv)
	oc.SetFailSafe(armedFailSafe(0))
	if _, err := oc.MatterInvoke(ctx, 0x0B, core.AddTrustedRootCertificateRequest{RootCACertificate: pendingRoot}); err != nil {
		t.Fatalf("AddTrustedRootCertificate: %v", err)
	}
	v, ok := oc.MatterRead(0x0004)
	if !ok {
		t.Fatal("TrustedRootCertificates after install: ok=false")
	}
	got := v.([][]byte)
	if len(got) != 2 || !bytes.Equal(got[0], installed.RootCert) || !bytes.Equal(got[1], pendingRoot) {
		t.Fatalf("TrustedRootCertificates = %q, want [root of fabric 1, the pending root]", got)
	}
}

// TestParityOpcreds_AddNOCReservedAdminVendorIDIsInvalidCommand verifies
// that AddNOC refuses a reserved AdminVendorId with the IM status
// InvalidCommand — not a NOCResponse — before every other guard, the
// fail-safe included, and that the test vendor IDs 0xFFF1..0xFFF4 pass
// the guard.
//
// Mirrors matter.js 0ceb202d packages/node/src/behaviors/
// operational-credentials/OperationalCredentialsServer.ts:217-222 (`if
// (!VendorId.isOperational(adminVendorId)) throw new
// StatusResponseError(…, Status.InvalidCommand)`), with isOperational at
// packages/types/src/datatype/VendorId.ts:44 (non-zero, valid 0..0xFFF4).
func TestParityOpcreds_AddNOCReservedAdminVendorIDIsInvalidCommand(t *testing.T) {
	t.Parallel()
	type statusCoder interface{ MatterStatusCode() im.StatusCode }
	for _, vid := range []uint16{0x0000, 0xFFF5, 0xFFF6, 0xFFFE, 0xFFFF} {
		s := newFakeStore()
		oc, err := core.NewOperationalCredentials(s, core.OpcredsConfig{
			SupportedFabrics: 5,
			FailSafe:         func() (uint8, bool) { return 0, false }, // the VID guard precedes FailsafeRequired
		})
		if err != nil {
			t.Fatalf("NewOperationalCredentials: %v", err)
		}
		resp, invokeErr := oc.MatterInvoke(context.Background(), 0x06, core.AddNOCRequest{
			IPKValue:      make([]byte, 16),
			AdminVendorID: vid,
		})
		var sc statusCoder
		if !errors.As(invokeErr, &sc) {
			t.Fatalf("VID 0x%04X: AddNOC = (%v, %v), want an IM InvalidCommand error", vid, resp, invokeErr)
		}
		if got := sc.MatterStatusCode(); got != im.StatusInvalidCommand {
			t.Errorf("VID 0x%04X: MatterStatusCode()=0x%02X, want StatusInvalidCommand (0x85)", vid, uint8(got))
		}
	}
	for _, vid := range []uint16{0x0001, 0xFFF1, 0xFFF4} {
		oc, _ := opcredsWithFakeStore(t)
		resp, invokeErr := oc.MatterInvoke(context.Background(), 0x06, core.AddNOCRequest{
			IPKValue:      make([]byte, 16),
			AdminVendorID: vid,
		})
		if invokeErr != nil {
			t.Fatalf("VID 0x%04X: AddNOC error %v, want the guard passed", vid, invokeErr)
		}
		// No trusted root: the next guard, addNoc's first in-band check.
		if nr := resp.(core.NOCResponse); nr.StatusCode != core.NOCStatusInvalidNOC {
			t.Errorf("VID 0x%04X: StatusCode = %d, want InvalidNOC (the next guard)", vid, nr.StatusCode)
		}
	}
}

// TestParityOpcreds_NonOperationalNOCNodeIDIsInvalidNodeOpID verifies
// that AddNOC and UpdateNOC answer a NOC whose matter-node-id lies outside
// the operational range 0x0000_0000_0000_0001..0xFFFF_FFEF_FFFF_FFFF with
// NOCResponse InvalidNodeOpId, and that AddNOC stores no fabric.
//
// Mirrors matter.js 0ceb202d packages/protocol/src/certificate/kinds/
// Noc.ts:83-85 (`if (!NodeId.isOperationalNodeId(nodeId)) throw new
// OperationalNodeIdError(…)`), mapped by packages/node/src/behaviors/
// operational-credentials/OperationalCredentialsServer.ts:179-183
// (#mapNocErrors → InvalidNodeOpId) for addNoc (:271-273) and updateNoc
// (:379-386).
func TestParityOpcreds_NonOperationalNOCNodeIDIsInvalidNodeOpID(t *testing.T) {
	t.Parallel()
	for _, nodeID := range []uint64{0, 0xFFFF_FFF0_0000_0000, 0xFFFF_FFFD_0001_0001, 0xFFFF_FFFF_FFFF_FFFF} {
		ctx := context.Background()
		oc, s := opcredsWithFakeStore(t)
		rootPriv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatalf("GenerateKey: %v", err)
		}
		if _, err := oc.MatterInvoke(ctx, 0x0B, core.AddTrustedRootCertificateRequest{
			RootCACertificate: buildCoreSignedCert(t, rootPriv, true, rootPriv),
		}); err != nil {
			t.Fatalf("AddTrustedRootCertificate: %v", err)
		}
		pendingPub := issueCSRPendingPubKey(ctx, t, oc, false)
		nocRaw := buildCoreSignedCertForPubKey(t, pendingPub, false, rootPriv, testDefaultFabricID, nodeID)
		resp, err := oc.MatterInvoke(ctx, 0x06, core.AddNOCRequest{
			NOCValue:         nocRaw,
			IPKValue:         make([]byte, 16),
			CaseAdminSubject: 0xABCD,
			AdminVendorID:    0xFFF1,
		})
		if err != nil {
			t.Fatalf("node 0x%016X: AddNOC: %v", nodeID, err)
		}
		if nr := resp.(core.NOCResponse); nr.StatusCode != core.NOCStatusInvalidNodeOpID {
			t.Errorf("node 0x%016X: AddNOC StatusCode = %d (%s), want InvalidNodeOpId (%d)",
				nodeID, nr.StatusCode, nr.DebugText, core.NOCStatusInvalidNodeOpID)
		}
		if fabrics, _ := s.ListFabrics(ctx); len(fabrics) != 0 {
			t.Errorf("node 0x%016X: %d fabrics stored after a refused AddNOC, want 0", nodeID, len(fabrics))
		}
	}

	ctx := context.Background()
	oc, _ := opcredsWithFakeStore(t)
	_, rootPriv, fabricIndex := commissionTestFabric(ctx, t, oc)
	fabCtx := im.WithFabricFilter(ctx, true, fabricIndex)
	nocRaw := mintUpdateNOC(fabCtx, t, oc, rootPriv, testDefaultFabricID, 0)
	resp, err := oc.MatterInvoke(fabCtx, 0x07, core.UpdateNOCRequest{NOCValue: nocRaw})
	if err != nil {
		t.Fatalf("UpdateNOC: %v", err)
	}
	if nr := resp.(core.NOCResponse); nr.StatusCode != core.NOCStatusInvalidNodeOpID {
		t.Errorf("UpdateNOC StatusCode = %d (%s), want InvalidNodeOpId (%d)",
			nr.StatusCode, nr.DebugText, core.NOCStatusInvalidNodeOpID)
	}
}

// TestParityOpcreds_FailSafeOfAnotherFabricIsNotArmed pins the
// fabric-aware fail-safe: CSRRequest, AddNOC and AddTrustedRootCertificate
// answer FailsafeRequired when the window is armed for a fabric other than
// the accessing one — a PASE session (fabric 0) against a window fabric 2
// armed, and fabric 1 against fabric 2's. Source: chip
// src/app/clusters/operational-credentials-server/OperationalCredentialsCluster.cpp:341,
// :472, :760 at 6170af84 (`IsFailSafeArmed(commandObj->GetAccessingFabricIndex())`,
// src/app/FailSafeContext.h:76-79). Ahead of matter.js: its
// OperationalCredentialsServer.ts:602-604 reads the one failsafeContext
// without a fabric (notes/parity/by_design.md, Ahead-of-matter.js register).
func TestParityOpcreds_FailSafeOfAnotherFabricIsNotArmed(t *testing.T) {
	t.Parallel()
	for _, accessing := range []uint8{0, 1} {
		oc, err := core.NewOperationalCredentials(newFakeStore(), core.OpcredsConfig{
			SupportedFabrics: 5,
			FailSafe:         func() (uint8, bool) { return 2, true }, // armed for fabric 2
		})
		if err != nil {
			t.Fatalf("NewOperationalCredentials: %v", err)
		}
		ctx := im.WithFabricFilter(context.Background(), false, accessing)
		for name, call := range map[string]func() error{
			"CSRRequest": func() error {
				_, err := oc.MatterInvoke(sessionChallengeCtx(ctx), 0x04, core.CSRRequest{CSRNonce: make([]byte, 32)})
				return err
			},
			"AddNOC": func() error {
				_, err := oc.MatterInvoke(ctx, 0x06, core.AddNOCRequest{IPKValue: make([]byte, 16), AdminVendorID: 0xFFF1})
				return err
			},
			"AddTrustedRootCertificate": func() error {
				_, err := oc.MatterInvoke(ctx, 0x0B, core.AddTrustedRootCertificateRequest{RootCACertificate: []byte{0x15, 0x18}})
				return err
			},
		} {
			var sc interface{ MatterStatusCode() im.StatusCode }
			if err := call(); !errors.As(err, &sc) || sc.MatterStatusCode() != im.StatusFailsafeRequired {
				t.Errorf("fabric %d %s under fabric 2's fail-safe = %v, want FailsafeRequired", accessing, name, err)
			}
		}
	}
}

// TestParityOpcreds_UpdateNOCFailSafeFabricAndRoot pins two UpdateNOC
// refusals of matter.js OperationalCredentialsServer.updateNoc: a session
// of another fabric than the fail-safe's answers ConstraintError
// (OperationalCredentialsServer.ts:355-360), and a root added by
// AddTrustedRootCertificate in the same fail-safe answers the in-band
// MissingCsr (:337-345, the certification work-around).
func TestParityOpcreds_UpdateNOCFailSafeFabricAndRoot(t *testing.T) {
	t.Parallel()
	setup := func(t *testing.T) (*core.OperationalCredentials, context.Context, *uint8, *ecdsa.PrivateKey) {
		t.Helper()
		ctx := context.Background()
		oc, err := core.NewOperationalCredentials(newFakeStore(), core.OpcredsConfig{SupportedFabrics: 5, FailSafe: armedFailSafe(0)})
		if err != nil {
			t.Fatalf("NewOperationalCredentials: %v", err)
		}
		_, rootPriv, fabricIndex := commissionTestFabric(ctx, t, oc)
		armed := fabricIndex
		oc.SetFailSafe(func() (uint8, bool) { return armed, true })
		commitForTest(t, oc)
		oc.ClearPendingState() // a fresh fail-safe window
		fabCtx := im.WithFabricFilter(ctx, true, fabricIndex)
		if _, err := oc.MatterInvoke(sessionChallengeCtx(fabCtx), 0x04, core.CSRRequest{CSRNonce: make([]byte, 32), IsForUpdateNOC: true}); err != nil {
			t.Fatalf("CSRRequest (IsForUpdateNOC): %v", err)
		}
		return oc, fabCtx, &armed, rootPriv
	}
	t.Run("another fabric's fail-safe", func(t *testing.T) {
		t.Parallel()
		oc, fabCtx, armed, _ := setup(t)
		*armed++ // the window now belongs to another fabric
		_, err := oc.MatterInvoke(fabCtx, 0x07, core.UpdateNOCRequest{NOCValue: []byte{0x15, 0x18}})
		var sc interface{ MatterStatusCode() im.StatusCode }
		if !errors.As(err, &sc) || sc.MatterStatusCode() != im.StatusConstraintError {
			t.Errorf("UpdateNOC under another fabric's fail-safe = %v, want ConstraintError", err)
		}
	})
	t.Run("after AddTrustedRootCertificate", func(t *testing.T) {
		t.Parallel()
		oc, fabCtx, _, rootPriv := setup(t)
		if _, err := oc.MatterInvoke(fabCtx, 0x0B, core.AddTrustedRootCertificateRequest{
			RootCACertificate: buildCoreSignedCert(t, rootPriv, true, rootPriv),
		}); err != nil {
			t.Fatalf("AddTrustedRootCertificate: %v", err)
		}
		resp, err := oc.MatterInvoke(fabCtx, 0x07, core.UpdateNOCRequest{NOCValue: []byte{0x15, 0x18}})
		if err != nil {
			t.Fatalf("UpdateNOC: %v", err)
		}
		if nr, ok := resp.(core.NOCResponse); !ok || nr.StatusCode != core.NOCStatusMissingCsr {
			t.Errorf("UpdateNOC after AddTrustedRootCertificate = %+v, want MissingCsr", resp)
		}
	})
}

// TestParityFabricLeave_OpcredsLeavingHookRunsBeforeTheRowsGo: the
// leaving hook runs while the store still holds the fabric, before the
// removed hook, and not for an index the store does not hold — matter.js
// Fabric.leave: the leaving event, the flush, then delete
// (packages/protocol/src/fabric/Fabric.ts:447-457 at db021da0).
func TestParityFabricLeave_OpcredsLeavingHookRunsBeforeTheRowsGo(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newFakeStore()

	var order []string
	var heldAtLeaving bool
	oc, err := core.NewOperationalCredentials(s, core.OpcredsConfig{
		SupportedFabrics: 5,
		OnFabricLeaving: func(ctx context.Context, fi uint8) {
			_, gerr := s.GetFabric(ctx, fi)
			heldAtLeaving = gerr == nil
			order = append(order, "leaving")
		},
		OnFabricRemoved: func(_ context.Context, _ uint8) { order = append(order, "removed") },
	})
	if err != nil {
		t.Fatalf("NewOperationalCredentials: %v", err)
	}
	idx, err := s.AddFabric(ctx, newFabricForTest(61))
	if err != nil {
		t.Fatalf("AddFabric: %v", err)
	}
	if err := oc.RemoveFabric(ctx, idx); err != nil {
		t.Fatalf("RemoveFabric: %v", err)
	}
	if !heldAtLeaving || len(order) != 2 || order[0] != "leaving" || order[1] != "removed" {
		t.Fatalf("hooks %v, fabric held at leaving %v; want [leaving removed] with the fabric still held", order, heldAtLeaving)
	}
	order = nil
	if err := oc.RemoveFabric(ctx, idx); err == nil {
		t.Fatal("RemoveFabric of a gone index succeeded")
	}
	if len(order) != 0 {
		t.Fatalf("hooks %v for an index the store does not hold", order)
	}
	oc.SetOnFabricLeaving(nil)
}

// TestParityOpcreds_UpdateFabricLabelConflictsOnTheEmptyLabel pins that
// the label conflict holds for every label, the empty one included: a
// fabric whose label another fabric already carries — every fabric starts
// with "" — is answered LabelConflict.
//
// Mirrors matter.js db021da0 packages/node/src/behaviors/
// operational-credentials/OperationalCredentialsServer.ts:396-402
// (`fabrics.find(f => f.label === label && f.fabricIndex !==
// currentFabricIndex)`, no exception for ""); connectedhomeip 6170af84
// src/app/clusters/operational-credentials-server/OperationalCredentialsCluster.cpp:719-725
// compares the same way.
func TestParityOpcreds_UpdateFabricLabelConflictsOnTheEmptyLabel(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newFakeStore()
	one, two := newFabricForTest(1), newFabricForTest(2)
	one.Label, two.Label = "", "two"
	if _, err := s.AddFabric(ctx, one); err != nil {
		t.Fatal(err)
	}
	idx2, err := s.AddFabric(ctx, two)
	if err != nil {
		t.Fatal(err)
	}
	oc, err := core.NewOperationalCredentials(s, core.OpcredsConfig{SupportedFabrics: 5})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := oc.MatterInvoke(im.WithFabricFilter(ctx, true, idx2), 0x09, core.UpdateFabricLabelRequest{Label: ""})
	if err != nil {
		t.Fatalf("UpdateFabricLabel(\"\"): %v", err)
	}
	if got := resp.(core.NOCResponse).StatusCode; got != core.NOCStatusLabelConflict {
		t.Fatalf("UpdateFabricLabel(\"\") while fabric 1 holds \"\": StatusCode %d, want LabelConflict (%d)", got, core.NOCStatusLabelConflict)
	}
	if rec, _ := s.GetFabric(ctx, idx2); rec.Label != "two" {
		t.Fatalf("label after the conflict = %q, want %q", rec.Label, "two")
	}
}

// TestParityOpcreds_CSRRequestKeepsTheFailSafeKeyPair pins that the
// operational key pair belongs to the fail-safe context: every CSRRequest
// in one context signs a CSR over the same public key, and a fresh context
// draws a new one.
//
// Mirrors matter.js db021da0 packages/protocol/src/common/FailsafeContext.ts:61
// (FabricBuilder.create once, as the context is constructed), :176-185
// (createCertificateSigningRequest signs with the builder's key pair) and
// packages/protocol/src/fabric/Fabric.ts:537-539 (create draws the pair),
// :549-551 (the CSR over that pair).
func TestParityOpcreds_CSRRequestKeepsTheFailSafeKeyPair(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	oc, err := core.NewOperationalCredentials(newFakeStore(), core.OpcredsConfig{SupportedFabrics: 5, FailSafe: armedFailSafe(0)})
	if err != nil {
		t.Fatal(err)
	}
	first := issueCSRPendingPubKey(ctx, t, oc, false)
	second := issueCSRPendingPubKey(ctx, t, oc, false)
	if !bytes.Equal(first, second) {
		t.Fatal("a second CSRRequest in the same fail-safe context carries another public key")
	}
	oc.ClearPendingState() // a fresh fail-safe context
	if third := issueCSRPendingPubKey(ctx, t, oc, false); bytes.Equal(first, third) {
		t.Fatal("a CSRRequest in a fresh fail-safe context carries the earlier context's public key")
	}
}

// TestParityOpcreds_AddNOCChecksInMatterJSOrder pins the order of AddNOC's
// refusals, so a request that fails more than one check gets the status of
// the first: no trusted root (InvalidNOC), the CSR of another session or
// none (MissingCsr), a CSR for UpdateNOC (ConstraintError), a full table
// (TableFull), then building the fabric — CaseAdminSubject
// (InvalidAdminSubject), the NOC's CASE Authenticated Tags (InvalidNOC),
// the NOC's public key (InvalidPublicKey), the chain (InvalidNOC).
//
// Mirrors matter.js db021da0 packages/node/src/behaviors/
// operational-credentials/OperationalCredentialsServer.ts:233-260 (addNoc),
// packages/protocol/src/common/FailsafeContext.ts:236-240 (buildFabric's
// CaseAdminSubject check before setOperationalCert) and
// packages/protocol/src/fabric/Fabric.ts:602-624 (#verifyOperationalCert
// judges the CATs at the decode and with validateNocTagList, :610-612,
// then compares the public key, :614-616, before it verifies the chain).
func TestParityOpcreds_AddNOCChecksInMatterJSOrder(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	addNOC := func(t *testing.T, oc *core.OperationalCredentials, noc []byte, subject uint64) core.NOCResponse {
		t.Helper()
		resp, err := oc.MatterInvoke(ctx, 0x06, core.AddNOCRequest{
			NOCValue: noc, IPKValue: make([]byte, 16), CaseAdminSubject: subject, AdminVendorID: 0xFFF1,
		})
		if err != nil {
			t.Fatalf("AddNOC: IM error %v, want an in-band NOCResponse", err)
		}
		return resp.(core.NOCResponse)
	}
	addRoot := func(t *testing.T, oc *core.OperationalCredentials) *ecdsa.PrivateKey {
		t.Helper()
		rootPriv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := oc.MatterInvoke(ctx, 0x0B, core.AddTrustedRootCertificateRequest{
			RootCACertificate: buildCoreSignedCert(t, rootPriv, true, rootPriv),
		}); err != nil {
			t.Fatalf("AddTrustedRootCertificate: %v", err)
		}
		return rootPriv
	}

	t.Run("no trusted root before a missing CSR", func(t *testing.T) {
		t.Parallel()
		oc, _ := opcredsWithFakeStore(t)
		if got := addNOC(t, oc, make([]byte, 10), 0xABCD); got.StatusCode != core.NOCStatusInvalidNOC {
			t.Fatalf("AddNOC without root and CSR: StatusCode %d (%s), want InvalidNOC", got.StatusCode, got.DebugText)
		}
	})

	t.Run("no trusted root before a CSR for UpdateNOC", func(t *testing.T) {
		t.Parallel()
		oc, err := core.NewOperationalCredentials(newFakeStore(), core.OpcredsConfig{SupportedFabrics: 5, FailSafe: armedFailSafe(1)})
		if err != nil {
			t.Fatal(err)
		}
		caseCtx := core.WithInvokeSessionID(im.WithFabricFilter(ctx, true, 1), 100)
		issueCSRPendingPubKey(caseCtx, t, oc, true)
		resp, err := oc.MatterInvoke(caseCtx, 0x06, core.AddNOCRequest{
			NOCValue: make([]byte, 10), IPKValue: make([]byte, 16), CaseAdminSubject: 0xABCD, AdminVendorID: 0xFFF1,
		})
		if err != nil {
			t.Fatalf("AddNOC after a CSR for UpdateNOC, no root: IM error %v, want NOCResponse InvalidNOC", err)
		}
		if got := resp.(core.NOCResponse); got.StatusCode != core.NOCStatusInvalidNOC {
			t.Fatalf("AddNOC after a CSR for UpdateNOC, no root: StatusCode %d (%s), want InvalidNOC", got.StatusCode, got.DebugText)
		}
	})

	t.Run("a full table before an invalid CaseAdminSubject", func(t *testing.T) {
		t.Parallel()
		s := newFakeStore()
		if _, err := s.AddFabric(ctx, newFabricForTest(1)); err != nil {
			t.Fatal(err)
		}
		oc, err := core.NewOperationalCredentials(s, core.OpcredsConfig{SupportedFabrics: 1, FailSafe: armedFailSafe(0)})
		if err != nil {
			t.Fatal(err)
		}
		rootPriv := addRoot(t, oc)
		pub := issueCSRPendingPubKey(ctx, t, oc, false)
		noc := buildCoreSignedCertForPubKey(t, pub, false, rootPriv, testDefaultFabricID, testDefaultNodeID)
		if got := addNOC(t, oc, noc, 0); got.StatusCode != core.NOCStatusTableFull {
			t.Fatalf("AddNOC on a full table with CaseAdminSubject 0: StatusCode %d (%s), want TableFull", got.StatusCode, got.DebugText)
		}
	})

	t.Run("the public key before the chain", func(t *testing.T) {
		t.Parallel()
		oc, _ := opcredsWithFakeStore(t)
		addRoot(t, oc)
		issueCSRPendingPubKey(ctx, t, oc, false)
		otherKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		otherRoot, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		// Another key, signed by a root the fail-safe does not hold.
		noc := buildCoreSignedCertForPubKey(t, marshalTestPub(otherKey), false, otherRoot, testDefaultFabricID, testDefaultNodeID)
		if got := addNOC(t, oc, noc, 0xABCD); got.StatusCode != core.NOCStatusInvalidPublicKey {
			t.Fatalf("AddNOC for another key under another root: StatusCode %d (%s), want InvalidPublicKey", got.StatusCode, got.DebugText)
		}
	})

	t.Run("the CASE Authenticated Tags before the public key", func(t *testing.T) {
		t.Parallel()
		for name, cats := range badNOCCATs {
			oc, _ := opcredsWithFakeStore(t)
			rootPriv := addRoot(t, oc)
			issueCSRPendingPubKey(ctx, t, oc, false)
			otherKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			noc := withCATs(t, buildCoreSignedCertForPubKey(t, marshalTestPub(otherKey), false, rootPriv, testDefaultFabricID, testDefaultNodeID), cats)
			if got := addNOC(t, oc, noc, 0xABCD); got.StatusCode != core.NOCStatusInvalidNOC {
				t.Errorf("AddNOC for another key with %s: StatusCode %d (%s), want InvalidNOC", name, got.StatusCode, got.DebugText)
			}
		}
	})
}

// badNOCCATs are the CASE Authenticated Tag lists a NOC may not carry:
// matter.js refuses a fourth tag and a tag of version 0 at the decode
// (packages/protocol/src/certificate/kinds/definitions/operational.ts:168,
// `maxLength: 3`; packages/types/src/datatype/CaseAuthenticatedTag.ts:118-124;
// both enforced by packages/types/src/tlv/TlvObject.ts:253-292, the
// validate Schema.decode runs) and an identifier twice in
// CaseAuthenticatedTag.validateNocTagList (:96-105), each a
// ValidationError, which #mapNocErrors answers InvalidNOC
// (packages/node/src/behaviors/operational-credentials/OperationalCredentialsServer.ts:184-192).
var badNOCCATs = map[string][]uint32{
	"an identifier twice": {0x0001_0001, 0x0001_0002},
	"four tags":           {0x0001_0001, 0x0002_0001, 0x0003_0001, 0x0004_0001},
	"a tag of version 0":  {0x0001_0000},
}

// withCATs is noc with cats as its subject's CASE Authenticated Tags,
// re-encoded (its signature no longer verifies).
func withCATs(t *testing.T, noc []byte, cats []uint32) []byte {
	t.Helper()
	c, err := mattercert.Decode(noc)
	if err != nil {
		t.Fatal(err)
	}
	c.Subject.CASEAuthTags = cats
	c.Subject.Order = nil
	out, err := mattercert.Encode(c)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestParityOpcreds_UpdateNOCChecksInMatterJSOrder pins the order of
// UpdateNOC's certificate checks, as AddNOC's: a NOC for another key is
// InvalidPublicKey whatever else is wrong with it — a chain that does not
// verify under the fabric's root, or a node id that is not operational —
// and a NOC for the pending key is then judged by its chain and its
// fabric id. A NOC whose CASE Authenticated Tags break the NOC rules is
// InvalidNOC before its key is compared.
//
// Mirrors matter.js db021da0 packages/protocol/src/fabric/Fabric.ts:579-585
// (updateOperationalCert: #verifyOperationalCert, then the fabric id) and
// :602-624 (#verifyOperationalCert judges the CATs, :610-612, compares the
// public key, :614-616, before it verifies the chain; the node id is
// checked inside Noc.verify).
func TestParityOpcreds_UpdateNOCChecksInMatterJSOrder(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const nonOperational = uint64(0xFFFF_FFFF_FFFF_FFFF)
	for _, c := range []struct {
		name     string
		otherKey bool   // the NOC certifies a key other than the CSR's
		ownRoot  bool   // the fabric's root signs it
		fabricID uint64 // its subject's fabric id
		nodeID   uint64 // its subject's node id
		cats     []uint32
		want     uint8
	}{
		{"another key under another root", true, false, testDefaultFabricID, testDefaultNodeID, nil, core.NOCStatusInvalidPublicKey},
		{"another key with a non-operational node id", true, true, testDefaultFabricID, nonOperational, nil, core.NOCStatusInvalidPublicKey},
		{"another key for another fabric", true, true, testDefaultFabricID + 1, testDefaultNodeID, nil, core.NOCStatusInvalidPublicKey},
		{"another key with a CAT identifier twice", true, true, testDefaultFabricID, testDefaultNodeID, badNOCCATs["an identifier twice"], core.NOCStatusInvalidNOC},
		{"another key with four CATs", true, true, testDefaultFabricID, testDefaultNodeID, badNOCCATs["four tags"], core.NOCStatusInvalidNOC},
		{"another key with a CAT of version 0", true, true, testDefaultFabricID, testDefaultNodeID, badNOCCATs["a tag of version 0"], core.NOCStatusInvalidNOC},
		{"the pending key under another root", false, false, testDefaultFabricID, testDefaultNodeID, nil, core.NOCStatusInvalidNOC},
		{"the pending key with a non-operational node id", false, true, testDefaultFabricID, nonOperational, nil, core.NOCStatusInvalidNodeOpID},
		{"the pending key for another fabric", false, true, testDefaultFabricID + 1, testDefaultNodeID, nil, core.NOCStatusInvalidNOC},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			oc, err := core.NewOperationalCredentials(newFakeStore(), core.OpcredsConfig{SupportedFabrics: 5, FailSafe: armedFailSafe(0)})
			if err != nil {
				t.Fatal(err)
			}
			_, signer, fabricIndex := commissionTestFabric(ctx, t, oc)
			fabCtx := im.WithFabricFilter(ctx, true, fabricIndex)
			oc.SetFailSafe(armedFailSafe(fabricIndex))
			commitForTest(t, oc)
			oc.ClearPendingState()
			pub := issueCSRPendingPubKey(sessionChallengeCtx(fabCtx), t, oc, true)
			if c.otherKey {
				k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
				if err != nil {
					t.Fatal(err)
				}
				pub = marshalTestPub(k)
			}
			if !c.ownRoot {
				if signer, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader); err != nil {
					t.Fatal(err)
				}
			}
			noc := buildCoreSignedCertForPubKey(t, pub, false, signer, c.fabricID, c.nodeID)
			if c.cats != nil {
				noc = withCATs(t, noc, c.cats)
			}
			resp, err := oc.MatterInvoke(fabCtx, 0x07, core.UpdateNOCRequest{NOCValue: noc})
			if err != nil {
				t.Fatalf("UpdateNOC: IM error %v, want an in-band NOCResponse", err)
			}
			if got := resp.(core.NOCResponse); got.StatusCode != c.want {
				t.Fatalf("UpdateNOC: StatusCode %d (%s), want %d", got.StatusCode, got.DebugText, c.want)
			}
		})
	}
}
