// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

// A port of matter.js packages/protocol/test/common/FailsafeContextTest.ts at
// 862ad75f: the fail-safe context's admin subject check when it builds the
// fabric.
//
// matter.js calls FailsafeContext.buildFabric; the Go fabric is built by the
// AddNOC handler of the Operational Credentials server, so the cases send
// AddNOC with the test's empty NOC and its subject and read the NOCResponse:
// InvalidAdminSubject is MatterFabricInvalidAdminSubjectError.

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
)

// failsafeContextBuildFabric sends AddNOC under an armed fail-safe with a
// pending CSR and a trusted root, an empty NOC (FailsafeContextTest.ts:83)
// and caseAdminSubject, and returns the response status.
func failsafeContextBuildFabric(t *testing.T, caseAdminSubject uint64) uint8 {
	t.Helper()
	ctx := context.Background()
	oc, err := core.NewOperationalCredentials(newFakeStore(), core.OpcredsConfig{SupportedFabrics: 5})
	if err != nil {
		t.Fatalf("NewOperationalCredentials: %v", err)
	}
	oc.SetIsFailSafeArmed(func() bool { return true })
	rootPriv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	if _, err := oc.MatterInvoke(ctx, 0x0B, core.AddTrustedRootCertificateRequest{
		RootCACertificate: buildCoreSignedCert(t, rootPriv, true, rootPriv),
	}); err != nil {
		t.Fatalf("AddTrustedRootCertificate: %v", err)
	}
	issueCSRPendingPubKey(ctx, t, oc, false)
	resp, err := oc.MatterInvoke(ctx, 0x06, core.AddNOCRequest{
		NOCValue:         []byte{},
		IPKValue:         make([]byte, 16),
		CaseAdminSubject: caseAdminSubject,
		AdminVendorID:    0xFFF1,
	})
	if err != nil {
		t.Fatalf("AddNOC: %v", err)
	}
	noc, ok := resp.(core.NOCResponse)
	if !ok {
		t.Fatalf("AddNOC response %T, want NOCResponse", resp)
	}
	return noc.StatusCode
}

func TestFailsafeContextBuildFabric(t *testing.T) {
	t.Parallel()

	t.Run("refuses a CAT admin subject with version 0", func(t *testing.T) {
		t.Parallel()
		if got := failsafeContextBuildFabric(t, 0xffff_fffd_0001_0000); got != core.NOCStatusInvalidAdminSubject {
			t.Fatalf("status = %d, want InvalidAdminSubject", got)
		}
	})

	t.Run("does not refuse a CAT admin subject with a version other than 0", func(t *testing.T) {
		t.Parallel()
		// The empty NOC still fails, only not for the subject.
		got := failsafeContextBuildFabric(t, 0xffff_fffd_0001_0001)
		if got == core.NOCStatusOK || got == core.NOCStatusInvalidAdminSubject {
			t.Fatalf("status = %d, want an error other than InvalidAdminSubject", got)
		}
	})

	t.Run("refuses an admin subject that is neither an operational node ID nor a CAT", func(t *testing.T) {
		t.Parallel()
		if got := failsafeContextBuildFabric(t, 0xffff_ffff_0000_0001); got != core.NOCStatusInvalidAdminSubject {
			t.Fatalf("status = %d, want InvalidAdminSubject", got)
		}
	})
}
