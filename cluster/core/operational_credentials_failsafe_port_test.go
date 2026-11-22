// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"thingfabric.dev/go-fabric/cluster/core"
	"thingfabric.dev/go-fabric/im"
)

// TestOpCredsWithoutFailSafePortFailsClosed pins what a missing fail-safe
// port means (review II, W2-sec-06): the four commands that need an armed
// fail-safe answer FailsafeRequired. Before, a nil
// [core.OpcredsConfig.FailSafe] skipped the guard, so a host that forgot
// the port ran CSRRequest, AddTrustedRootCertificate, AddNOC and
// UpdateNOC outside any fail-safe. chip's guard is unconditional —
// OperationalCredentialsCluster.cpp:341 (CSRRequest), :472 (AddNOC), :640
// (UpdateNOC), :760 (AddTrustedRootCertificate) at 6170af84,
// `VerifyOrExit(failSafeContext.IsFailSafeArmed(…), … Status::FailsafeRequired)`.
func TestOpCredsWithoutFailSafePortFailsClosed(t *testing.T) {
	t.Parallel()
	type statusCoder interface{ MatterStatusCode() im.StatusCode }
	for _, tc := range []struct {
		name    string
		command uint32
		fields  any
	}{
		{"CSRRequest", 0x04, core.CSRRequest{CSRNonce: make([]byte, 32)}},
		{"AddNOC", 0x06, core.AddNOCRequest{IPKValue: make([]byte, 16), AdminVendorID: 0xFFF1}},
		{"UpdateNOC", 0x07, core.UpdateNOCRequest{}},
		{"AddTrustedRootCertificate", 0x0B, core.AddTrustedRootCertificateRequest{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			oc, err := core.NewOperationalCredentials(newFakeStore(), core.OpcredsConfig{SupportedFabrics: 5})
			if err != nil {
				t.Fatalf("NewOperationalCredentials: %v", err)
			}
			// The request arrives on a session: the commands that sign refuse
			// one without its attestation challenge before they look at the
			// fail-safe (matter.js asserts the session first,
			// OperationalCredentialsServer.ts:126 at db021da0).
			_, err = oc.MatterInvoke(sessionChallengeCtx(context.Background()), tc.command, tc.fields)
			var sc statusCoder
			if !errors.As(err, &sc) || sc.MatterStatusCode() != im.StatusFailsafeRequired {
				t.Fatalf("%s without a fail-safe port = %v, want FailsafeRequired (0xCA)", tc.name, err)
			}
		})
	}
}

// TestOpCredsWithDisarmedFailSafePortFailsClosed is the wired half of the
// test above: a fail-safe port that is present but reports "not armed"
// must give the four commands FailsafeRequired as well — chip's guard
// asks IsFailSafeArmed, not whether a port exists (the lines above). The
// UpdateNOC case also leaves a CSR key for UpdateNOC in place from a
// window that has since closed: without the armed check UpdateNOC would
// go on to consume it (R2-P5-tmut-07).
func TestOpCredsWithDisarmedFailSafePortFailsClosed(t *testing.T) {
	t.Parallel()
	type statusCoder interface{ MatterStatusCode() im.StatusCode }
	for _, tc := range []struct {
		name       string
		command    uint32
		fields     any
		pendingCSR bool
	}{
		{"CSRRequest", 0x04, core.CSRRequest{CSRNonce: make([]byte, 32)}, false},
		{"AddNOC", 0x06, core.AddNOCRequest{IPKValue: make([]byte, 16), AdminVendorID: 0xFFF1}, false},
		{"UpdateNOC", 0x07, core.UpdateNOCRequest{}, false},
		{"UpdateNOC with a CSR key left from a closed window", 0x07, core.UpdateNOCRequest{}, true},
		{"AddTrustedRootCertificate", 0x0B, core.AddTrustedRootCertificateRequest{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var armed atomic.Bool
			oc, err := core.NewOperationalCredentials(newFakeStore(), core.OpcredsConfig{
				SupportedFabrics: 5,
				FailSafe: func() (uint8, bool) {
					if armed.Load() {
						return 1, true
					}
					return 0, false
				},
			})
			if err != nil {
				t.Fatalf("NewOperationalCredentials: %v", err)
			}
			ctx := context.Background()
			if tc.pendingCSR {
				// A CSR for UpdateNOC is issued over CASE on the fabric the
				// window was armed for.
				ctx = core.WithInvokeSessionID(im.WithFabricFilter(ctx, true, 1), 100)
				armed.Store(true)
				issueCSRPendingPubKey(ctx, t, oc, true)
				armed.Store(false)
			}
			_, err = oc.MatterInvoke(sessionChallengeCtx(ctx), tc.command, tc.fields)
			var sc statusCoder
			if !errors.As(err, &sc) || sc.MatterStatusCode() != im.StatusFailsafeRequired {
				t.Fatalf("%s with a disarmed fail-safe port = %v, want FailsafeRequired (0xCA)", tc.name, err)
			}
		})
	}
}
