// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
)

// addNOCLabel commissions one fabric through cfg and returns the Label
// AddNOC stored for it.
func addNOCLabel(t *testing.T, cfg core.OpcredsConfig) string {
	t.Helper()
	ctx := context.Background()
	store := newFakeStore()
	oc, err := core.NewOperationalCredentials(store, cfg)
	if err != nil {
		t.Fatalf("NewOperationalCredentials: %v", err)
	}
	rootPriv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	rootRaw := buildCoreSignedCert(t, rootPriv, true, rootPriv)
	if _, err := oc.MatterInvoke(ctx, 0x0B, core.AddTrustedRootCertificateRequest{RootCACertificate: rootRaw}); err != nil {
		t.Fatalf("AddTrustedRootCertificate: %v", err)
	}
	pendingPub := issueCSRPendingPubKey(ctx, t, oc, false)
	nocRaw := buildCoreSignedCertForPubKey(t, pendingPub, false, rootPriv, testDefaultFabricID, testDefaultNodeID)
	resp, err := oc.MatterInvoke(ctx, 0x06, core.AddNOCRequest{
		NOCValue: nocRaw, IPKValue: make([]byte, 16), CaseAdminSubject: testDefaultNodeID, AdminVendorID: 0xFFF1,
	})
	if err != nil {
		t.Fatalf("AddNOC: %v", err)
	}
	noc, ok := resp.(core.NOCResponse)
	if !ok || noc.StatusCode != core.NOCStatusOK {
		t.Fatalf("AddNOC = %#v, want OK", resp)
	}
	fabrics, err := store.ListFabrics(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fabrics {
		if f.FabricIndex == noc.FabricIndex {
			return f.Label
		}
	}
	t.Fatalf("fabric %d not stored", noc.FabricIndex)
	return ""
}

// TestAddNOC_FabricLabelStartsEmpty: a fabric AddNOC installs has the
// empty Label matter.js's FabricBuilder starts with (Fabric.ts
// `#label = ""`), which TC-OPCREDS-3.7 reads right after commissioning;
// a host-configured InitialFabricLabel replaces it.
func TestAddNOC_FabricLabelStartsEmpty(t *testing.T) {
	t.Parallel()
	if got := addNOCLabel(t, core.OpcredsConfig{}); got != "" {
		t.Errorf("Label after AddNOC = %q, want \"\"", got)
	}
	if got := addNOCLabel(t, core.OpcredsConfig{InitialFabricLabel: "home"}); got != "home" {
		t.Errorf("Label after AddNOC with InitialFabricLabel = %q, want \"home\"", got)
	}
}

// TestNewOperationalCredentials_RejectsALongInitialLabel: the Label
// constraint is 32 bytes.
func TestNewOperationalCredentials_RejectsALongInitialLabel(t *testing.T) {
	t.Parallel()
	if _, err := core.NewOperationalCredentials(newFakeStore(), core.OpcredsConfig{InitialFabricLabel: strings.Repeat("x", 33)}); err == nil {
		t.Fatal("a 33-byte InitialFabricLabel was accepted")
	}
}
