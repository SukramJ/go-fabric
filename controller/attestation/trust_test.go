// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package attestation_test

import (
	"bytes"
	"testing"

	"github.com/SukramJ/go-fabric/controller/attestation"
	secattestation "github.com/SukramJ/go-fabric/secure/attestation"
)

// onlineStore is a PAAStore with revocations, recording each question.
type onlineStore struct {
	attestation.StaticStore
	asked   [][]byte
	revoked bool
}

func (o *onlineStore) IsRevoked(akid, _ []byte, _ string) (bool, error) {
	o.asked = append(o.asked, akid)
	return o.revoked, nil
}

func TestTrustModes(t *testing.T) {
	snap := &attestation.StaticStore{PAAs: []attestation.PAA{{DER: secattestation.TestPAAFFF1Cert, Production: true}}}
	online := &onlineStore{revoked: true}

	if attestation.Trust(attestation.None, snap, online) != nil {
		t.Fatal("None has a trust store")
	}
	prod := attestation.Trust(attestation.Production, snap, online)
	if p, ok := prod.PAA(secattestation.TestPAAFFF1SKID); !ok || !p.Production {
		t.Fatal("Production misses the snapshot's root")
	}
	if _, ok := prod.PAA(secattestation.TestPAANoVIDSKID); ok || prod.AcceptsTestCertificates() {
		t.Fatal("Production trusts the test roots")
	}
	dev := attestation.Trust(attestation.Development, snap, nil)
	if _, ok := dev.PAA(secattestation.TestPAANoVIDSKID); !ok || !dev.AcceptsTestCertificates() {
		t.Fatal("Development misses the test roots")
	}
	if _, ok := dev.CDSigner(secattestation.TestCMSSignerSKID); !ok {
		t.Fatal("Development misses the test CD signer")
	}

	// The snapshot holds the root, yet the revocation question goes to
	// the online store: the snapshot never short-circuits the CRL path.
	r, ok := prod.(attestation.Revocations)
	if !ok {
		t.Fatal("the layered store is no revocation source")
	}
	revoked, err := r.IsRevoked(secattestation.TestPAAFFF1SKID, []byte{1}, "DN")
	if err != nil || !revoked || len(online.asked) != 1 || !bytes.Equal(online.asked[0], secattestation.TestPAAFFF1SKID) {
		t.Fatalf("revocation: %v %v %d", revoked, err, len(online.asked))
	}
	// Without an online store nothing is revoked (no CRL is known offline).
	if revoked, err := attestation.Trust(attestation.Production, snap, nil).(attestation.Revocations).IsRevoked([]byte{1}, []byte{1}, ""); revoked || err != nil {
		t.Fatalf("offline revocation: %v %v", revoked, err)
	}

	for _, m := range []attestation.TrustMode{attestation.Development, attestation.Production, attestation.None} {
		if got, err := attestation.ParseTrustMode(m.String()); err != nil || got != m {
			t.Fatalf("%v: %v %v", m, got, err)
		}
	}
	if _, err := attestation.ParseTrustMode("off"); err == nil {
		t.Fatal("an unknown mode parsed")
	}
	if attestation.TrustMode(9).String() == "" {
		t.Fatal("no name for an unknown mode")
	}
}
