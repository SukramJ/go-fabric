// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package attestation_test

import (
	"bytes"
	"errors"
	"testing"

	"thingfabric.dev/go-fabric/controller/attestation"
	secattestation "thingfabric.dev/go-fabric/secure/attestation"
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
	// The test CD signer is built in and trusted while test certificates
	// are (CdSigners.ts:76-83 at db91886c), so no store lists it.
	if s, ok := attestation.KnownCDSigner(secattestation.TestCMSSignerSKID); !ok || !s.IsTestSigner {
		t.Fatal("the test CD signer is not built in")
	}
	if _, ok := dev.CDSigner(secattestation.TestCMSSignerSKID); ok {
		t.Fatal("Development lists the built-in test CD signer")
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

// errRevoker is a Revocations layer whose lookup fails.
type errRevoker struct{ attestation.StaticStore }

func (*errRevoker) IsRevoked(_, _ []byte, _ string) (bool, error) {
	return false, errLookup
}

var errLookup = errors.New("lookup failed")

// TestLayeredRevocationAsksEveryLayer holds R2-C2-sec-1802: a Layered asks
// every layer that is Revocations — a nested Layered without a revoker,
// a snapshot that is itself Layered, a second revoker behind a first that
// says "not revoked" — and a failed lookup in any layer is the answer.
func TestLayeredRevocationAsksEveryLayer(t *testing.T) {
	snap := &attestation.StaticStore{}
	cases := []struct {
		name  string
		store func(online *onlineStore) attestation.PAAStore
	}{
		{"flat", func(o *onlineStore) attestation.PAAStore { return attestation.Layered{snap, o} }},
		{"nested", func(o *onlineStore) attestation.PAAStore {
			return attestation.Layered{attestation.Layered{snap}, o}
		}},
		{"layered snapshot via Trust", func(o *onlineStore) attestation.PAAStore {
			return attestation.Trust(attestation.Production, attestation.Layered{snap}, o)
		}},
		{"second revoker", func(o *onlineStore) attestation.PAAStore {
			return attestation.Layered{&onlineStore{}, o}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			online := &onlineStore{revoked: true}
			r, ok := c.store(online).(attestation.Revocations)
			if !ok {
				t.Fatal("store is not Revocations")
			}
			revoked, err := r.IsRevoked([]byte{7}, []byte{1}, "DN")
			if err != nil || !revoked || len(online.asked) != 1 {
				t.Fatalf("IsRevoked = %v, %v (online asked %d), want true, nil (asked 1)", revoked, err, len(online.asked))
			}
		})
	}
	t.Run("error in a later layer", func(t *testing.T) {
		l := attestation.Layered{&onlineStore{}, &errRevoker{}}
		if revoked, err := l.IsRevoked([]byte{7}, []byte{1}, "DN"); !errors.Is(err, errLookup) {
			t.Fatalf("IsRevoked = %v, %v, want errLookup", revoked, err)
		}
	})
}
