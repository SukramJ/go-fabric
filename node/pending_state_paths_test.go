// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"path/filepath"
	"testing"

	mattercore "thingfabric.dev/go-fabric/cluster/core"
	"thingfabric.dev/go-fabric/controller/ca"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/node"
)

// TestEachPathClearsThePendingCredentialState (review II, N4-fixex-1,
// N-sweep-1): OperationalCredentials' pending state — the CSR a
// commissioning left — is cleared by three paths the node wires between
// GeneralCommissioning and OperationalCredentials (root.go): P1
// CommissioningComplete's commit (CommitFailSafe; on a Wi-Fi node through
// wifiFailSafeCommit), P2 a fresh fail-safe's start (PrepareFreshFailSafe)
// and P3 the fail-safe's expiry (OnFailSafeExpiry). The only way to see
// the state from outside is an AddNOC under a new fail-safe answering
// MissingCsr, and the arm of that fail-safe runs P2 — so the example's
// test (examples/reference-bridge/wiring_test.go) sees P1 only through
// P2. Here each scenario runs with the masking hook left unwired, once
// with the path under test wired (MissingCsr) and once without it (the
// CSR survives: the scenario would see a lost path).
func TestEachPathClearsThePendingCredentialState(t *testing.T) {
	t.Parallel()
	const (
		complete = iota
		expire
	)
	cases := []struct {
		name string
		wifi bool
		// how the commissioning's fail-safe ends
		end int
		// the hook under test, and the one that would mask it
		path, mask node.FailSafeHooksForTest
	}{
		{"P1 CommissioningComplete's commit", false, complete, node.FailSafeHookCommit, node.FailSafeHookStarting},
		{"P1 CommissioningComplete's commit on a Wi-Fi node", true, complete, node.FailSafeHookCommit, node.FailSafeHookStarting},
		{"P2 a fresh fail-safe's start", false, complete, node.FailSafeHookStarting, node.FailSafeHookCommit},
		{"P3 the fail-safe's expiry", false, expire, node.FailSafeHookExpiry, node.FailSafeHookStarting},
	}
	for _, tc := range cases {
		for _, wired := range []bool{true, false} {
			name := tc.name + "/wired"
			without := tc.mask
			if !wired {
				name = tc.name + "/unwired"
				without |= tc.path
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				cfg := minimalConfig(openDB(t, filepath.Join(t.TempDir(), "node.db")))
				if tc.wifi {
					cfg.Network.WiFi = &node.WiFi{Driver: acceptingWiFi{}}
				}
				node.WithoutFailSafeHooks(&cfg, without)
				status := pendingCSRAfter(t, newNode(t, cfg).Root(), tc.end == expire)
				if wired && status != mattercore.NOCStatusMissingCsr {
					t.Fatalf("AddNOC under the next fail-safe = status %d, want MissingCsr (%d): the path left the finished commissioning's CSR pending",
						status, mattercore.NOCStatusMissingCsr)
				}
				if !wired && status == mattercore.NOCStatusMissingCsr {
					t.Fatal("AddNOC under the next fail-safe answered MissingCsr with the path unwired: another path cleared the CSR, the scenario does not see this one")
				}
			})
		}
	}
}

// pendingCSRAfter runs a commissioning's credential steps on root — a
// fail-safe armed, a CSR issued — ends that fail-safe by
// CommissioningComplete or by its expiry, arms the next one as a PASE
// establishment does, gives it a trusted root and answers the status of an
// AddNOC under it: MissingCsr when the CSR was cleared.
func pendingCSRAfter(t *testing.T, root *node.RootClusters, expire bool) uint8 {
	t.Helper()
	ctx := t.Context()
	const fabric = 1
	if err := root.GeneralCommissioning.ArmFailSafeFor(ctx, 60, fabric); err != nil {
		t.Fatalf("ArmFailSafeFor: %v", err)
	}
	// CSRRequest signs over the invoking session's attestation challenge,
	// which the bridge stamps on a real invoke.
	csrCtx := mattercore.WithInvokeAttestationChallenge(im.WithFabricFilter(ctx, false, fabric), make([]byte, 16))
	if _, err := root.OperationalCredentials.MatterInvoke(csrCtx, 0x04, mattercore.CSRRequest{CSRNonce: make([]byte, 32)}); err != nil {
		t.Fatalf("CSRRequest: %v", err)
	}
	if expire {
		root.GeneralCommissioning.ExpireFailSafe(ctx)
	} else {
		raw, err := root.GeneralCommissioning.MatterInvoke(im.WithFabricFilter(ctx, false, fabric), 0x04, nil)
		if err != nil {
			t.Fatalf("CommissioningComplete: %v", err)
		}
		if resp, ok := raw.(mattercore.CommissioningCompleteResponse); !ok || resp.ErrorCode != mattercore.CommissioningErrorOK {
			t.Fatalf("CommissioningComplete = %#v, want ErrorCode OK", raw)
		}
	}
	if err := root.GeneralCommissioning.ArmOnPaseEstablished(ctx); err != nil { //nolint:staticcheck // the deprecated unnamed arm, kept for its window
		t.Fatalf("the PASE arm: %v", err)
	}
	// AddNOC checks the trusted root before the CSR (matter.js's order):
	// without one it answers "no trusted root" whether or not the CSR
	// survived.
	authority, err := ca.New(ca.Config{})
	if err != nil {
		t.Fatalf("ca.New: %v", err)
	}
	if _, err := root.OperationalCredentials.MatterInvoke(ctx, 0x0B, mattercore.AddTrustedRootCertificateRequest{RootCACertificate: authority.RootCert()}); err != nil {
		t.Fatalf("AddTrustedRootCertificate: %v", err)
	}
	raw, err := root.OperationalCredentials.MatterInvoke(ctx, 0x06, mattercore.AddNOCRequest{NOCValue: []byte{0x15}, IPKValue: make([]byte, 16), AdminVendorID: 0xFFF1})
	if err != nil {
		t.Fatalf("AddNOC: %v", err)
	}
	resp, ok := raw.(mattercore.NOCResponse)
	if !ok {
		t.Fatalf("AddNOC returned %T, want NOCResponse", raw)
	}
	return resp.StatusCode
}
