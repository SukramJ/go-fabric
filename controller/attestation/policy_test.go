// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package attestation_test

import (
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/controller/attestation"
)

// TestPolicyTable: matter.js #resolveAttestationFindings
// (ControllerCommissioningFlow.ts:1341-1368).
func TestPolicyTable(t *testing.T) {
	t.Parallel()
	findings := []attestation.Finding{{Level: attestation.LevelWarning, Check: attestation.CdSignerVerificationSkipped, Message: "m"}}
	boom := errors.New("boom")
	rows := []struct {
		name    string
		policy  attestation.Policy
		proceed bool
		reason  string
		err     error
	}{
		{"undefined accepts (backward compatible)", attestation.Policy{}, true, "", nil},
		{"true accepts", attestation.Accept(), true, "", nil},
		{"false rejects", attestation.Reject(), false, "", nil},
		{"function true", attestation.Policy{Decide: func([]attestation.Finding) (bool, string, error) { return true, "", nil }}, true, "", nil},
		{"function false", attestation.Policy{Decide: func([]attestation.Finding) (bool, string, error) { return false, "", nil }}, false, "", nil},
		{"function string", attestation.Policy{Decide: func([]attestation.Finding) (bool, string, error) { return false, "no", nil }}, false, "no", nil},
		{"function throws", attestation.Policy{Decide: func([]attestation.Finding) (bool, string, error) { return false, "", boom }}, false, "", boom},
		{"function decides over the boolean", attestation.Policy{OnFailure: attestation.Reject().OnFailure, Decide: func([]attestation.Finding) (bool, string, error) { return true, "", nil }}, true, "", nil},
	}
	for _, r := range rows {
		proceed, reason, err := r.policy.Resolve(findings, nil)
		if proceed != r.proceed || reason != r.reason || !errors.Is(err, r.err) {
			t.Errorf("%s: (%v, %q, %v), want (%v, %q, %v)", r.name, proceed, reason, err, r.proceed, r.reason, r.err)
		}
	}
	e := &attestation.Error{Check: attestation.CertificateRevoked, Message: "m", Err: boom}
	if !errors.Is(e, boom) || e.Error() != "attestation: CertificateRevoked: m" {
		t.Fatalf("Error %q", e.Error())
	}
}
