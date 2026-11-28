// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"testing"

	"thingfabric.dev/go-fabric/cluster/core"
	mstore "thingfabric.dev/go-fabric/store"
)

// TestFailedAddNOCAcksOnlyItsOwnOwedEvent (review II, P10-store-6803): an
// earlier rollback of fabric index 1 is still owed — its delivery's hooks
// wait in the node's queue — when an AddNOC gets index 1 again (the
// double hands the index out again, which a real store does only after
// its counter wrapped) and fails after AddFabric. The failed AddNOC
// acknowledges the removal its own rollback recorded, not the earlier
// one.
func TestFailedAddNOCAcksOnlyItsOwnOwedEvent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	inner := newFakeStore()
	inner.owedID = 1
	inner.owed = []mstore.OwedFabricEvent{{ID: 1, Kind: mstore.FabricPendingAdd, Fabric: mstore.FabricRecord{FabricIndex: 1}}}
	wrapped := &upsertIdentityFailStore{fakeStore: inner, upsertIdentityErr: errors.New("injected UpsertIdentity failure")}
	oc, err := core.NewOperationalCredentials(wrapped, core.OpcredsConfig{SupportedFabrics: 5})
	if err != nil {
		t.Fatal(err)
	}
	oc.SetFailSafe(func() (uint8, bool) { return 0, true })
	rootPriv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := oc.MatterInvoke(ctx, 0x0B, core.AddTrustedRootCertificateRequest{RootCACertificate: buildCoreSignedCert(t, rootPriv, true, rootPriv)}); err != nil {
		t.Fatal(err)
	}
	nocRaw := buildCoreSignedCertForPubKey(t, issueCSRPendingPubKey(ctx, t, oc, false), false, rootPriv, testDefaultFabricID, testDefaultNodeID)
	resp, err := oc.MatterInvoke(ctx, 0x06, core.AddNOCRequest{
		NOCValue: nocRaw, IPKValue: make([]byte, 16), CaseAdminSubject: 0x0001_0203_0405_0607, AdminVendorID: 0x1234,
	})
	if err != nil {
		t.Fatal(err)
	}
	if r, ok := resp.(core.NOCResponse); !ok || r.StatusCode == core.NOCStatusOK {
		t.Fatalf("AddNOC = %+v, want the injected failure", resp)
	}
	owed, err := inner.OwedFabricEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(owed) != 1 || owed[0].ID != 1 {
		t.Fatalf("owed after the failed AddNOC = %+v, want the earlier rollback's event 1 alone", owed)
	}
}
