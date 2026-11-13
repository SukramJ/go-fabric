// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/im"
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
