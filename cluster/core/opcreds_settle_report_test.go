// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"reflect"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/im"
	mstore "github.com/SukramJ/go-fabric/store"
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
