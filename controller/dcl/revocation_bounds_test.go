// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package dcl

import (
	"crypto/x509/pkix"
	"math/big"
	"testing"
	"time"
)

// TestRevocationCacheExpiresUnaskedAuthorities (review II,
// R2-P6-bound-1904): an authority's lookup unused for the hour goes when
// another authority is asked, not only when it is asked again.
func TestRevocationCacheExpiresUnaskedAuthorities(t *testing.T) {
	t.Parallel()
	x := newRevocationFixture(t)
	x.paiCRL(t, crlSpec{revoked: []*big.Int{big.NewInt(7)}})
	x.dacRevoked(t) // caches the PAI's lookup
	paiID := hexUpper(x.pki.dac.cert.AuthorityKeyId)
	cached := func(id string) bool {
		x.s.revocations.mu.Lock()
		defer x.s.revocations.mu.Unlock()
		_, ok := x.s.revocations.values[id]
		return ok
	}
	if !cached(paiID) {
		t.Fatal("the PAI's lookup is not cached")
	}
	x.clock.advance(61 * time.Minute)
	// Another authority (the PAA: no points, cached as empty).
	if _, err := x.s.IsRevoked(x.pki.paa.skid(), []byte{1}, ""); err != nil {
		t.Fatal(err)
	}
	if cached(paiID) {
		t.Fatal("an authority unused for the hour keeps its lookup")
	}
}

// TestRevocationPointsPerIssuerBounded (review II, R2-P6-bound-1905): of a
// ledger answer that lists more CRL points for one issuer than
// maxCRLPointsPerIssuer, only that many are fetched, the cut is logged
// once and the lookup is not cached; the CRLs it fetched still count.
func TestRevocationPointsPerIssuerBounded(t *testing.T) {
	t.Parallel()
	x := newRevocationFixture(t)
	const path = "/crl/pai.crl"
	// The points share a vendor, so each CRL names its point (§6.2.6.1).
	crl := signCRL(t, x.pki.pai, crlSpec{revoked: []*big.Int{x.pki.dac.cert.SerialNumber}, extensions: []pkix.Extension{idpExtension(t, x.f.url(path))}})
	x.f.bytes(path, crl)
	point := revocationPoint(x.pki.pai, x.pki.pai, nil, false, 0xFFF1, 0x8000, x.f.url(path), crl)
	points := make([]map[string]any, 3*maxCRLPointsPerIssuer)
	for i := range points {
		points[i] = point
	}
	x.f.publishPoints(x.pki.pai, points...)
	start := time.Now()
	if !x.dacRevoked(t) {
		t.Fatal("the CRLs fetched do not count")
	}
	if d := time.Since(start); d > revocationLookupTimeout {
		t.Fatalf("the lookup took %v, beyond its timeout", d)
	}
	if n := x.f.hit(path); n > maxCRLPointsPerIssuer {
		t.Fatalf("%d CRL downloads, want at most %d", n, maxCRLPointsPerIssuer)
	}
	if !x.log.hasMessage("dcl.revocation.too_many_points") {
		t.Fatal("the cut is not logged")
	}
	x.dacRevoked(t)
	if n := x.f.hit(pointsPath(x.pki.pai)); n != 2 {
		t.Fatalf("%d ledger lookups, want 2: an incomplete lookup is not cached", n)
	}
}
