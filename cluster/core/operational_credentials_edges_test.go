// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"thingfabric.dev/go-fabric/cluster/core"
	"thingfabric.dev/go-fabric/cluster/spec"
	"thingfabric.dev/go-fabric/im"
	mstore "thingfabric.dev/go-fabric/store"
)

// The edges of the OperationalCredentials server that no other test
// reaches (R2-P5-tmut-18 rest).

// listRefusingStore is the fake store whose ListFabrics fails once down is
// set.
type listRefusingStore struct {
	*fakeStore
	down atomic.Bool
}

func (s *listRefusingStore) ListFabrics(ctx context.Context) ([]mstore.FabricRecord, error) {
	if s.down.Load() {
		return nil, errors.New("store down")
	}
	return s.fakeStore.ListFabrics(ctx)
}

// addFailingStore is the fake store with AddFabric failing.
type addFailingStore struct{ *fakeStore }

func (addFailingStore) AddFabric(context.Context, mstore.FabricRecord) (uint8, error) {
	return 0, errors.New("store full")
}

// TestOpcredsInitialFabricLabelOf32Bytes: a 32-byte InitialFabricLabel is
// the longest accepted.
func TestOpcredsInitialFabricLabelOf32Bytes(t *testing.T) {
	t.Parallel()
	if _, err := core.NewOperationalCredentials(newFakeStore(), core.OpcredsConfig{InitialFabricLabel: strings.Repeat("a", 32)}); err != nil {
		t.Errorf("a 32-byte InitialFabricLabel: %v", err)
	}
	if _, err := core.NewOperationalCredentials(newFakeStore(), core.OpcredsConfig{InitialFabricLabel: strings.Repeat("a", 33)}); err == nil {
		t.Error("a 33-byte InitialFabricLabel was accepted")
	}
}

// TestOpcredsReadsFailWithTheStore: a fabric-list attribute is unreadable
// while the store cannot list the fabrics, rather than read as empty.
func TestOpcredsReadsFailWithTheStore(t *testing.T) {
	t.Parallel()
	st := &listRefusingStore{fakeStore: newFakeStore()}
	oc, err := core.NewOperationalCredentials(st, core.OpcredsConfig{SupportedFabrics: 5})
	if err != nil {
		t.Fatal(err)
	}
	st.down.Store(true)
	for _, attr := range []uint32{0x0000, 0x0004} { // NOCs, TrustedRootCertificates
		if v, ok := oc.MatterRead(attr); ok {
			t.Errorf("MatterRead(0x%04X) with a failing store = %v, want unreadable", attr, v)
		}
	}
	if v, ok := oc.MatterReadFiltered(im.WithFabricFilter(context.Background(), true, 1), 0x0000); ok {
		t.Errorf("filtered NOCs with a failing store = %v, want unreadable", v)
	}
}

// TestOpcredsCommandsRefuseTheWrongRequestType: every command handler
// refuses a payload of the wrong Go type with INVALID_COMMAND naming the
// type, before it reads anything of it.
func TestOpcredsCommandsRefuseTheWrongRequestType(t *testing.T) {
	t.Parallel()
	for _, cmd := range []uint32{0x00, 0x02, 0x04, 0x06, 0x07, 0x09, 0x0A, 0x0B} {
		oc := newOpcreds(t)
		_, err := oc.MatterInvoke(sessionChallengeCtx(im.WithFabricFilter(context.Background(), true, 1)), cmd, "wrong")
		if spec.StatusOf(err) != im.StatusInvalidCommand || !strings.Contains(err.Error(), "got string") {
			t.Errorf("command 0x%02X with a string: %v (status %v), want INVALID_COMMAND naming the type", cmd, err, spec.StatusOf(err))
		}
	}
}

// TestOpcredsAttestationMaterialEdges: the provider's error reaches the
// request; material missing any one of its four parts is refused; a
// CertificateChainRequest fails with the material.
func TestOpcredsAttestationMaterialEdges(t *testing.T) {
	t.Parallel()
	dacKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	full := core.OpcredsAttestation{DACPrivateKey: dacKey, DAC: []byte{1}, PAI: []byte{2}, CertificationDeclaration: []byte{3}}
	ctx := sessionChallengeCtx(context.Background())
	attest := core.AttestationRequest{AttestationNonce: make([]byte, 32)}
	with := func(m core.OpcredsAttestation, perr error) *core.OperationalCredentials {
		oc, err := core.NewOperationalCredentials(newFakeStore(), core.OpcredsConfig{
			FailSafe: armedFailSafe(0), SupportedFabrics: 5,
			AttestationProvider: func(context.Context) (core.OpcredsAttestation, error) { return m, perr },
		})
		if err != nil {
			t.Fatal(err)
		}
		return oc
	}

	down := errors.New("provider down")
	if _, err := with(full, down).MatterInvoke(ctx, 0x00, attest); !errors.Is(err, down) {
		t.Errorf("AttestationRequest with a failing provider: %v, want its error", err)
	}
	if _, err := with(full, down).MatterInvoke(ctx, 0x02, core.CertificateChainRequest{CertificateType: 1}); !errors.Is(err, down) {
		t.Errorf("CertificateChainRequest with a failing provider: %v, want its error", err)
	}
	for name, m := range map[string]core.OpcredsAttestation{
		"no DAC key": {DAC: full.DAC, PAI: full.PAI, CertificationDeclaration: full.CertificationDeclaration},
		"no DAC":     {DACPrivateKey: dacKey, PAI: full.PAI, CertificationDeclaration: full.CertificationDeclaration},
		"no PAI":     {DACPrivateKey: dacKey, DAC: full.DAC, CertificationDeclaration: full.CertificationDeclaration},
		"no CD":      {DACPrivateKey: dacKey, DAC: full.DAC, PAI: full.PAI},
	} {
		// resolveAttestation's plain error: the IM layer's FAILURE.
		if _, err := with(m, nil).MatterInvoke(ctx, 0x00, attest); spec.StatusOf(err) != im.StatusFailure {
			t.Errorf("%s: AttestationRequest over incomplete material: %v (status %v), want FAILURE", name, err, spec.StatusOf(err))
		}
	}
	if _, err := with(full, nil).MatterInvoke(ctx, 0x00, attest); err != nil {
		t.Errorf("complete material: %v", err)
	}
}

// TestOpcredsCSRForUpdateNeedsAFabric: a CSRRequest for UpdateNOC without
// an accessing fabric is INVALID_COMMAND.
func TestOpcredsCSRForUpdateNeedsAFabric(t *testing.T) {
	t.Parallel()
	oc := newOpcreds(t)
	_, err := oc.MatterInvoke(sessionChallengeCtx(context.Background()), 0x04, core.CSRRequest{CSRNonce: make([]byte, 32), IsForUpdateNOC: true})
	if spec.StatusOf(err) != im.StatusInvalidCommand {
		t.Fatalf("CSRRequest(IsForUpdateNOC) without a fabric: %v, want INVALID_COMMAND", err)
	}
}

// TestOpcredsAddNOCOnAStoreThatRefusesTheFabric: AddNOC answers
// FabricConflict when the store does not take the fabric.
func TestOpcredsAddNOCOnAStoreThatRefusesTheFabric(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	oc, err := core.NewOperationalCredentials(addFailingStore{newFakeStore()}, core.OpcredsConfig{SupportedFabrics: 5, FailSafe: armedFailSafe(0)})
	if err != nil {
		t.Fatal(err)
	}
	rootPriv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	rootRaw := buildCoreSignedCert(t, rootPriv, true, rootPriv)
	if _, err := oc.MatterInvoke(ctx, 0x0B, core.AddTrustedRootCertificateRequest{RootCACertificate: rootRaw}); err != nil {
		t.Fatal(err)
	}
	pub := issueCSRPendingPubKey(ctx, t, oc, false)
	noc := buildCoreSignedCertForPubKey(t, pub, false, rootPriv, testDefaultFabricID, testDefaultNodeID)
	resp, err := oc.MatterInvoke(ctx, 0x06, core.AddNOCRequest{NOCValue: noc, IPKValue: make([]byte, 16), CaseAdminSubject: 0xABCD, AdminVendorID: 0x1234})
	if err != nil {
		t.Fatal(err)
	}
	if got := resp.(core.NOCResponse).StatusCode; got != core.NOCStatusFabricConflict {
		t.Fatalf("AddNOC on a refusing store: StatusCode %d, want FabricConflict", got)
	}
}

// TestOpcredsUpdateNOCEdges: UpdateNOC without a CSR is MissingCsr; without
// an accessing fabric UNSUPPORTED_ACCESS; for a fabric the store does not
// hold InvalidFabricIndex; over a stored root that does not parse
// InvalidNOC.
func TestOpcredsUpdateNOCEdges(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("no CSR", func(t *testing.T) {
		t.Parallel()
		oc := newOpcreds(t)
		resp, err := oc.MatterInvoke(ctx, 0x07, core.UpdateNOCRequest{NOCValue: []byte{1}})
		if err != nil {
			t.Fatal(err)
		}
		if got := resp.(core.NOCResponse).StatusCode; got != core.NOCStatusMissingCsr {
			t.Fatalf("StatusCode %d, want MissingCsr", got)
		}
	})
	t.Run("no accessing fabric", func(t *testing.T) {
		t.Parallel()
		oc := newOpcreds(t)
		oc.SetFailSafe(armedFailSafe(1))
		issueCSRPendingPubKey(im.WithFabricFilter(ctx, true, 1), t, oc, true)
		oc.SetFailSafe(armedFailSafe(0))
		_, err := oc.MatterInvoke(ctx, 0x07, core.UpdateNOCRequest{NOCValue: []byte{1}})
		if spec.StatusOf(err) != im.StatusUnsupportedAccess {
			t.Fatalf("%v, want UNSUPPORTED_ACCESS", err)
		}
	})
	t.Run("fabric not held", func(t *testing.T) {
		t.Parallel()
		oc := newOpcreds(t)
		oc.SetFailSafe(armedFailSafe(5))
		fab := im.WithFabricFilter(ctx, true, 5)
		issueCSRPendingPubKey(fab, t, oc, true)
		resp, err := oc.MatterInvoke(fab, 0x07, core.UpdateNOCRequest{NOCValue: []byte{1}})
		if err != nil {
			t.Fatal(err)
		}
		if got := resp.(core.NOCResponse).StatusCode; got != core.NOCStatusInvalidFabricIndex {
			t.Fatalf("StatusCode %d, want InvalidFabricIndex", got)
		}
	})
	t.Run("stored root does not parse", func(t *testing.T) {
		t.Parallel()
		s := newFakeStore()
		rec := newFabricForTest(1)
		rec.RootCert = []byte{0x15, 0x18}
		idx, err := s.AddFabric(ctx, rec)
		if err != nil {
			t.Fatal(err)
		}
		oc, err := core.NewOperationalCredentials(s, core.OpcredsConfig{SupportedFabrics: 5, FailSafe: armedFailSafe(idx)})
		if err != nil {
			t.Fatal(err)
		}
		fab := im.WithFabricFilter(ctx, true, idx)
		pub := issueCSRPendingPubKey(fab, t, oc, true)
		signer, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		noc := buildCoreSignedCertForPubKey(t, pub, false, signer, rec.FabricID, rec.NodeID)
		resp, err := oc.MatterInvoke(fab, 0x07, core.UpdateNOCRequest{NOCValue: noc})
		if err != nil {
			t.Fatal(err)
		}
		if got := resp.(core.NOCResponse).StatusCode; got != core.NOCStatusInvalidNOC {
			t.Fatalf("StatusCode %d, want InvalidNOC", got)
		}
	})
}

// TestOpcredsUpdateNOCOnALegacyFabricRow: a fabric row that holds the
// root's public key but not its certificate verifies an UpdateNOC against
// the key; the change listeners hear of its revert, a nil listener
// registering nothing.
func TestOpcredsUpdateNOCOnALegacyFabricRow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fs := newFakeStore()
	oc, err := core.NewOperationalCredentials(fs, core.OpcredsConfig{SupportedFabrics: 5, FailSafe: armedFailSafe(0)})
	if err != nil {
		t.Fatal(err)
	}
	_, rootPriv, idx := commissionTestFabric(ctx, t, oc)
	fs.mu.Lock()
	rec := fs.fabrics[idx]
	if len(rec.RootPublicKey) != 65 {
		fs.mu.Unlock()
		t.Fatalf("stored root public key of %d bytes", len(rec.RootPublicKey))
	}
	rec.RootCert = nil
	fs.fabrics[idx] = rec
	fs.mu.Unlock()
	if unsubscribe := oc.OnMatterValueChanged(nil); unsubscribe == nil {
		t.Fatal("OnMatterValueChanged(nil) returned no unsubscribe")
	}
	changed := 0
	oc.OnMatterValueChanged(func() { changed++ })

	fab := im.WithFabricFilter(ctx, true, idx)
	noc := mintUpdateNOC(fab, t, oc, rootPriv, testDefaultFabricID, testDefaultNodeID)
	resp, err := oc.MatterInvoke(fab, 0x07, core.UpdateNOCRequest{NOCValue: noc})
	if err != nil {
		t.Fatal(err)
	}
	if nr := resp.(core.NOCResponse); nr.StatusCode != core.NOCStatusOK {
		t.Fatalf("UpdateNOC on a row without its root certificate: StatusCode %d (%s), want OK", nr.StatusCode, nr.DebugText)
	}
	// The fail-safe expires: the update is reverted, and the listeners —
	// the nil one registered nothing — hear of it.
	oc.OnFailSafeExpiry(ctx, idx)
	if changed == 0 {
		t.Error("the change listener did not hear of the reverted UpdateNOC")
	}
}

// TestOpcredsUpdateFabricLabelEdges: a label no other fabric carries is
// taken whatever the other fabrics' labels; the fabric is reported changed
// only when its label changed.
func TestOpcredsUpdateFabricLabelEdges(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newFakeStore()
	one, two := newFabricForTest(1), newFabricForTest(2)
	one.Label, two.Label = "one", "two"
	idx1, err := s.AddFabric(ctx, one)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddFabric(ctx, two); err != nil {
		t.Fatal(err)
	}
	oc, err := core.NewOperationalCredentials(s, core.OpcredsConfig{SupportedFabrics: 5})
	if err != nil {
		t.Fatal(err)
	}
	var changed []uint8
	oc.SetOnFabricChanged(func(_ context.Context, idx uint8) { changed = append(changed, idx) })
	fab := im.WithFabricFilter(ctx, true, idx1)
	for _, tc := range []struct {
		label   string
		changed int
	}{{"uno", 1}, {"uno", 1}} {
		resp, err := oc.MatterInvoke(fab, 0x09, core.UpdateFabricLabelRequest{Label: tc.label})
		if err != nil {
			t.Fatal(err)
		}
		if got := resp.(core.NOCResponse).StatusCode; got != core.NOCStatusOK {
			t.Fatalf("UpdateFabricLabel(%q): StatusCode %d, want OK", tc.label, got)
		}
		if len(changed) != tc.changed {
			t.Fatalf("UpdateFabricLabel(%q): fabric reported changed %d times, want %d", tc.label, len(changed), tc.changed)
		}
	}
}

// TestOpcredsUpdateNOCVerifiesAgainstTheStoredRootCertificate: a fabric
// row that holds its root certificate verifies an UpdateNOC against the
// certificate, not the separately stored public key.
func TestOpcredsUpdateNOCVerifiesAgainstTheStoredRootCertificate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fs := newFakeStore()
	oc, err := core.NewOperationalCredentials(fs, core.OpcredsConfig{SupportedFabrics: 5, FailSafe: armedFailSafe(0)})
	if err != nil {
		t.Fatal(err)
	}
	_, rootPriv, idx := commissionTestFabric(ctx, t, oc)
	fs.mu.Lock()
	rec := fs.fabrics[idx]
	if len(rec.RootCert) == 0 {
		fs.mu.Unlock()
		t.Fatal("the commissioned row holds no root certificate")
	}
	rec.RootPublicKey = append([]byte{0x04}, make([]byte, 64)...) // not a point on the curve
	fs.fabrics[idx] = rec
	fs.mu.Unlock()

	fab := im.WithFabricFilter(ctx, true, idx)
	noc := mintUpdateNOC(fab, t, oc, rootPriv, testDefaultFabricID, testDefaultNodeID)
	resp, err := oc.MatterInvoke(fab, 0x07, core.UpdateNOCRequest{NOCValue: noc})
	if err != nil {
		t.Fatal(err)
	}
	if nr := resp.(core.NOCResponse); nr.StatusCode != core.NOCStatusOK {
		t.Fatalf("UpdateNOC: StatusCode %d (%s), want OK", nr.StatusCode, nr.DebugText)
	}
}

// TestOpcredsLocalReadOfCurrentFabricIndex: a filtered read without a
// request behind it is the local, unfiltered read.
func TestOpcredsLocalReadOfCurrentFabricIndex(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	oc := newOpcreds(t)
	commissionTestFabric(ctx, t, oc)
	want, _ := oc.MatterRead(0x0005)
	if got, ok := oc.MatterReadFiltered(ctx, 0x0005); !ok || got != want {
		t.Fatalf("local CurrentFabricIndex = %v (%v), want MatterRead's %v", got, ok, want)
	}
	if want == uint8(0) {
		t.Fatal("MatterRead's CurrentFabricIndex is 0 after AddNOC: the case shows nothing")
	}
}
