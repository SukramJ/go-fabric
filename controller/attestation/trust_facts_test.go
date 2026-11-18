// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package attestation_test

import (
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/controller/attestation"
	secattestation "github.com/SukramJ/go-fabric/secure/attestation"
)

// TestResultCarriesTheTrustFacts: Validate says whether the chain ended
// in a root the store holds as not production (TestPAA) and whether the
// Certification Declaration was signed by the Matter test CD signer
// (TestCDSigner) — the two facts the commissioner records the attested
// pair on — without changing the findings (review II, R2-P6-sec-1803,
// owner decisions 192 and 214).
func TestResultCarriesTheTrustFacts(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name                  string
		store                 func() *attestation.StaticStore
		cd                    func(*cdSpec)
		testPAA, testCDSigner bool
		findings              []string
	}{
		{"test root, test CD signer", attestation.TestStore, nil, true, true, []string{"info:CertificationTypeTest"}},
		{"production root, test CD signer", func() *attestation.StaticStore {
			s := attestation.TestStore()
			s.PAAs = []attestation.PAA{{DER: secattestation.TestPAAFFF1Cert, Production: true}}
			return s
		}, nil, false, true, []string{"info:CertificationTypeTest"}},
		{"test root, store CD signer", storeWithSigner, storeSigned, true, false, []string{"info:CertificationTypeTest"}},
		{"production root, store CD signer", func() *attestation.StaticStore {
			s := storeWithSigner()
			s.PAAs = []attestation.PAA{{DER: secattestation.TestPAAFFF1Cert, Production: true}}
			return s
		}, storeSigned, false, false, []string{"info:CertificationTypeTest"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			cd := defaultCD()
			if c.cd != nil {
				c.cd(&cd)
			}
			f := newFixture(t, defaultSpec(), cd)
			f.ctx.Store = c.store()
			res := davPass(t, f)
			if res.TestPAA != c.testPAA || res.TestCDSigner != c.testCDSigner {
				t.Fatalf("TestPAA %v TestCDSigner %v, want %v %v", res.TestPAA, res.TestCDSigner, c.testPAA, c.testCDSigner)
			}
			if got := checks(res.Findings); !slices.Equal(got, c.findings) {
				t.Fatalf("findings %v, want %v (unchanged)", got, c.findings)
			}
		})
	}
}
