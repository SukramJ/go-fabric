// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"reflect"
	"sync"
	"testing"

	"thingfabric.dev/go-fabric/cluster/core"
	"thingfabric.dev/go-fabric/cluster/spec"
	"thingfabric.dev/go-fabric/im"
	mstore "thingfabric.dev/go-fabric/store"
)

// A NOC command that finds a row an earlier rollback could not land
// settles it at its entry; the fabric-removed hook of that settle runs
// after nocMu is released, as the rule at nocMu says, so a host hook that
// calls back into the server does not block (review II, R2-P6-conc-1003).
func TestSettleReportRunsWithoutNOCLock(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		fabric uint8 // the accessing fabric and the fail-safe's
		cmd    uint32
		fields any
	}{
		{"AddNOC", 0, 0x06, core.AddNOCRequest{IPKValue: make([]byte, 16), AdminVendorID: 0xFFF1}},
		{"UpdateNOC", 1, 0x07, core.UpdateNOCRequest{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			st := newFakeStore()
			oc, err := core.NewOperationalCredentials(st, core.OpcredsConfig{SupportedFabrics: 5, FailSafe: armedFailSafe(tc.fabric)})
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			if _, err := st.AddFabric(ctx, mstore.FabricRecord{FabricID: 1, RootPublicKey: []byte{4, 1}}); err != nil {
				t.Fatal(err)
			}
			// A row an earlier rollback could not land: still pending.
			if _, err := st.AddFabric(ctx, mstore.FabricRecord{FabricID: 2, RootPublicKey: []byte{4, 2}, Pending: mstore.FabricPendingAdd}); err != nil {
				t.Fatal(err)
			}
			var called, free bool
			oc.SetOnFabricRemoved(func(context.Context, uint8) {
				called = true
				free = oc.NOCLockFree()
			})
			if tc.fabric != 0 {
				ctx = im.WithFabricFilter(ctx, false, tc.fabric)
			}
			if _, err := oc.MatterInvoke(ctx, tc.cmd, tc.fields); err != nil {
				t.Logf("%s: %v", tc.name, err)
			}
			if !called {
				t.Fatal("the settle reported no removal")
			}
			if !free {
				t.Error("the fabric-removed hook of the settle ran with nocMu held")
			}
		})
	}
}

// A fail-safe that ends between AddTrustedRootCertificate's fail-safe
// check and its store of the root waits for the command (nocMu) and then
// clears the root: no root outlives the window that admitted it (review
// II, R2-P6-conc-1005).
func TestAddTrustedRootExpiryInBetweenLeavesNoRoot(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	oc, err := core.NewOperationalCredentials(newFakeStore(), core.OpcredsConfig{SupportedFabrics: 5})
	if err != nil {
		t.Fatal(err)
	}
	rootPriv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	rootRaw := buildCoreSignedCert(t, rootPriv, true, rootPriv)
	expired := make(chan struct{})
	var once sync.Once
	oc.SetFailSafe(func() (uint8, bool) {
		first := false
		once.Do(func() { first = true })
		if !first {
			return 0, true
		}
		// The window ends while the command is at its fail-safe check: the
		// rollback either waits for the command (it holds nocMu) or runs
		// to its end before the command goes on.
		waits := make(chan struct{})
		oc.OnRollbackWaitsForNOC(func() { close(waits) })
		go func() {
			oc.OnFailSafeExpiry(ctx, 0)
			close(expired)
		}()
		select {
		case <-waits:
		case <-expired:
		}
		return 0, true
	})
	if _, err := oc.MatterInvoke(ctx, 0x0B, core.AddTrustedRootCertificateRequest{RootCACertificate: rootRaw}); err != nil {
		t.Fatal(err)
	}
	<-expired
	v, _ := oc.MatterRead(0x0004)
	if roots := reflect.ValueOf(v); roots.Len() != 0 {
		t.Errorf("TrustedRootCertificates after the window ended = %d entries, want none", roots.Len())
	}
}

// UpdateFabricLabel without an accessing fabric answers UnsupportedAccess
// on a direct call after an AddNOC: the fabric the last AddNOC installed
// never stands in for the request's (review II, R2-P6-sec-1006). UpdateNOC
// lost the same fallback; its own guards refuse a request without a fabric
// before it (the CSR for an update needs one, and a session fabric 0 does
// not match a fabric's fail-safe).
func TestFabricScopedOpcredsCommandsWithoutFabricAreRefused(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newFakeStore()
	oc, err := core.NewOperationalCredentials(st, core.OpcredsConfig{SupportedFabrics: 5, FailSafe: armedFailSafe(0)})
	if err != nil {
		t.Fatal(err)
	}
	rootPriv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := oc.MatterInvoke(ctx, 0x0B, core.AddTrustedRootCertificateRequest{RootCACertificate: buildCoreSignedCert(t, rootPriv, true, rootPriv)}); err != nil {
		t.Fatalf("AddTrustedRootCertificate: %v", err)
	}
	pendingPub := issueCSRPendingPubKey(ctx, t, oc, false)
	resp, err := oc.MatterInvoke(ctx, 0x06, core.AddNOCRequest{
		NOCValue: buildCoreSignedCertForPubKey(t, pendingPub, false, rootPriv, testDefaultFabricID, testDefaultNodeID),
		IPKValue: make([]byte, 16), CaseAdminSubject: testDefaultNodeID, AdminVendorID: 0xFFF1,
	})
	if noc, ok := resp.(core.NOCResponse); err != nil || !ok || noc.StatusCode != core.NOCStatusOK {
		t.Fatalf("AddNOC = %#v, %v", resp, err)
	}
	for _, c := range []struct {
		name   string
		cmd    uint32
		fields any
	}{
		{"UpdateFabricLabel", 0x09, core.UpdateFabricLabelRequest{Label: "relabelled"}},
	} {
		_, err := oc.MatterInvoke(ctx, c.cmd, c.fields)
		if got := spec.StatusOf(err); got != im.StatusUnsupportedAccess {
			t.Errorf("%s without an accessing fabric: %v (status %v), want UnsupportedAccess", c.name, err, got)
		}
	}
	fabrics, err := st.ListFabrics(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for i := range fabrics {
		if fabrics[i].Label != "" {
			t.Errorf("fabric %d relabelled to %q", fabrics[i].FabricIndex, fabrics[i].Label)
		}
	}
}

// removeFabricFailStore fails the removal of a fabric it holds.
type removeFabricFailStore struct {
	*fakeStore
}

func (s *removeFabricFailStore) RemoveFabric(ctx context.Context, fabricIndex uint8) error {
	if _, err := s.GetFabric(ctx, fabricIndex); err != nil {
		return err
	}
	return errors.New("injected RemoveFabric I/O failure")
}

// RemoveFabric answers InvalidFabricIndex only for a fabric the store does
// not hold; a store failure on a fabric it holds is the command's FAILURE
// (review II, R2-P6-err-1027).
func TestRemoveFabricStoreFailureIsFailure(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := &removeFabricFailStore{fakeStore: newFakeStore()}
	oc, err := core.NewOperationalCredentials(st, core.OpcredsConfig{SupportedFabrics: 5})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddFabric(ctx, mstore.FabricRecord{FabricID: 1, RootPublicKey: []byte{4, 1}}); err != nil {
		t.Fatal(err)
	}
	resp, err := oc.MatterInvoke(fabricCtx(1), 0x0A, core.RemoveFabricRequest{FabricIndex: 1})
	if got := spec.StatusOf(err); got != im.StatusFailure {
		t.Errorf("RemoveFabric over a failing store = %#v, %v (status %v), want FAILURE", resp, err, got)
	}
	resp, err = oc.MatterInvoke(fabricCtx(1), 0x0A, core.RemoveFabricRequest{FabricIndex: 9})
	if noc, ok := resp.(core.NOCResponse); err != nil || !ok || noc.StatusCode != core.NOCStatusInvalidFabricIndex {
		t.Errorf("RemoveFabric of an unknown index = %#v, %v, want NOCResponse InvalidFabricIndex", resp, err)
	}
}

// A CSRRequest whose CSR build fails leaves no key pair behind it: the
// pair it drew goes with the failure (review II, N4-coresec-2).
func TestCSRBuildFailureDropsTheDrawnKeyPair(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	oc, err := core.NewOperationalCredentials(newFakeStore(), core.OpcredsConfig{SupportedFabrics: 5, FailSafe: armedFailSafe(0)})
	if err != nil {
		t.Fatal(err)
	}
	oc.FailCSRBuild(errors.New("injected random-source failure"))
	if _, err := oc.MatterInvoke(sessionChallengeCtx(ctx), 0x04, core.CSRRequest{CSRNonce: make([]byte, 32)}); spec.StatusOf(err) != im.StatusFailure {
		t.Fatalf("CSRRequest with a failing CSR build = %v, want FAILURE", err)
	}
	if oc.CSRKeyPending() {
		t.Error("the fail-safe context holds a key pair no CSR was issued for")
	}
}

// The fail-safe's rollback removes a pending AddNOC row whatever fabric the
// expiry names — 0 included, as for an AddNOC the expiry overtook before
// it re-stamped the fail-safe (review II, R2-P6-fsm-1024).
func TestFailSafeExpiryRollsBackThePendingRowWhateverFabricItNames(t *testing.T) {
	t.Parallel()
	for _, named := range []uint8{0, 1, 7} {
		ctx := context.Background()
		st := newFakeStore()
		oc, err := core.NewOperationalCredentials(st, core.OpcredsConfig{SupportedFabrics: 5})
		if err != nil {
			t.Fatal(err)
		}
		idx, err := st.AddFabric(ctx, mstore.FabricRecord{FabricID: 2, RootPublicKey: []byte{4, 2}, Pending: mstore.FabricPendingAdd})
		if err != nil {
			t.Fatal(err)
		}
		oc.OnFailSafeExpiry(ctx, named)
		if _, err := st.GetFabric(ctx, idx); !errors.Is(err, mstore.ErrFabricNotFound) {
			t.Errorf("expiry naming fabric %d: the pending row %d stays (%v)", named, idx, err)
		}
	}
}
