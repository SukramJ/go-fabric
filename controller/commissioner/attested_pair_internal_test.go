// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioner

import (
	"testing"

	"github.com/SukramJ/go-fabric/controller/attestation"
)

// TestAttestedPairWithheld: an attestation that returned no error records
// its vendor/product pair only when the chain was verified and no error
// finding stands — no trust store, a CD authorized_paa_list that could not
// be checked without one, and an accepted LevelError finding each withhold
// it; an informational or other warning finding does not.
func TestAttestedPairWithheld(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name     string
		findings []attestation.Finding
		withheld bool
	}{
		{"no finding", nil, false},
		{"no trust store", []attestation.Finding{{Level: attestation.LevelWarning, Check: attestation.DclServiceUnavailable, Message: "no trust store"}}, true},
		{"authorized_paa_list unchecked", []attestation.Finding{{Level: attestation.LevelInfo, Check: attestation.CertificationTypeTest}, {Level: attestation.LevelWarning, Check: attestation.DclServiceUnavailable, Message: "CD has authorized_paa_list"}}, true},
		{"an error finding", []attestation.Finding{{Level: attestation.LevelError, Check: attestation.TrustedAsTestCertificate, Message: "test PAA"}}, true},
		{"informational and warning findings", []attestation.Finding{{Level: attestation.LevelInfo, Check: attestation.PaaTrustStoreTimeMismatch}, {Level: attestation.LevelWarning, Check: attestation.CertificationTypeProvisional}}, false},
	} {
		why, withheld := attestedPairWithheld(c.findings)
		if withheld != c.withheld || (withheld && why == "") {
			t.Errorf("%s: withheld %v (%q), want %v", c.name, withheld, why, c.withheld)
		}
	}
}
