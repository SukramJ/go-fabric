// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package dcl

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"log/slog"
	"math/big"
	"net/http"
	"strings"
	"testing"
	"time"
)

// revocationFixture is a store over a ledger that knows pki's PAA.
type revocationFixture struct {
	log   *logRecorder
	f     *fakeDCL
	pki   testPKI
	clock *manualClock
	s     *CertificateStore
}

func newRevocationFixture(t *testing.T) *revocationFixture {
	t.Helper()
	f := newFakeDCL(t)
	pki := newTestPKI(t)
	f.addRoot(pki.paa)
	clock := newManualClock()
	log := &logRecorder{}
	s := openStore(t, CertificateOptions{Client: f.client(), Clock: clock, UpdateInterval: -1, Logger: slog.New(log)})
	return &revocationFixture{log: log, f: f, pki: pki, clock: clock, s: s}
}

// dacRevoked asks the store about the fixture's DAC as the validator does
// (DeviceAttestationValidator.ts:343-355).
func (x *revocationFixture) dacRevoked(t *testing.T) bool {
	t.Helper()
	dac := x.pki.dac.cert
	revoked, err := x.s.IsRevoked(dac.AuthorityKeyId, serialContent(dac.SerialNumber), hexUpper(dac.RawIssuer))
	if err != nil {
		t.Fatal(err)
	}
	return revoked
}

// paiCRL publishes a CRL the PAI signs itself, revoking serials.
func (x *revocationFixture) paiCRL(t *testing.T, spec crlSpec) (path string, crl []byte) {
	t.Helper()
	crl = signCRL(t, x.pki.pai, spec)
	path = "/crl/pai.crl"
	x.f.bytes(path, crl)
	x.f.publishPoints(x.pki.pai, revocationPoint(x.pki.pai, x.pki.pai, nil, false, 0xFFF1, 0x8000, x.f.url(path), crl))
	return path, crl
}

func pointsPath(issuer testCert) string { return "/dcl/pki/revocation-points/" + issuer.skidHex() }

// TestRevocationPAISignedCRL: a CRL the PAI signs (§6.2.6.1, a PAI that is
// not delegated) revokes the DAC serial it lists, under the PAI's name;
// another serial or issuer name is not revoked. The lookup is cached and
// lives an hour past its last use (:322-326).
func TestRevocationPAISignedCRL(t *testing.T) {
	t.Parallel()
	x := newRevocationFixture(t)
	crlPath, _ := x.paiCRL(t, crlSpec{revoked: []*big.Int{x.pki.dac.cert.SerialNumber, big.NewInt(7)}})
	if !x.dacRevoked(t) {
		t.Fatal("the revoked DAC is not revoked")
	}
	dac := x.pki.dac.cert
	if r, _ := x.s.IsRevoked(dac.AuthorityKeyId, []byte{0x08}, hexUpper(dac.RawIssuer)); r {
		t.Fatal("an unlisted serial is revoked")
	}
	if r, _ := x.s.IsRevoked(dac.AuthorityKeyId, serialContent(dac.SerialNumber), hexUpper(x.pki.paa.cert.RawSubject)); r {
		t.Fatal("revoked under another issuer name")
	}
	// No issuer name matches any issuer (matchRevocation :221-233).
	if r, _ := x.s.IsRevoked(dac.AuthorityKeyId, serialContent(dac.SerialNumber), ""); !r {
		t.Fatal("not revoked without an issuer name")
	}
	if x.f.hit(pointsPath(x.pki.pai)) != 1 || x.f.hit(crlPath) != 1 {
		t.Fatalf("lookups %d/%d, want one each (cached)", x.f.hit(pointsPath(x.pki.pai)), x.f.hit(crlPath))
	}
	// Used within the hour, the entry lives on.
	x.clock.advance(59 * time.Minute)
	x.dacRevoked(t)
	x.clock.advance(59 * time.Minute)
	x.dacRevoked(t)
	if x.f.hit(pointsPath(x.pki.pai)) != 1 {
		t.Fatal("a cached lookup was fetched again")
	}
	// An hour unused, it is fetched again.
	x.clock.advance(61 * time.Minute)
	x.dacRevoked(t)
	if x.f.hit(pointsPath(x.pki.pai)) != 2 {
		t.Fatalf("lookups %d, want a second after the hour", x.f.hit(pointsPath(x.pki.pai)))
	}
}

// TestRevocationLookupFailures: an issuer without points (NOT_FOUND) is
// cached as not revoked (:1447-1451); a ledger failure is not revoked
// and not cached (:1452-1457); a CRL the server refuses (4xx) is a
// rejected point, cached; one it fails (5xx) is transient, not cached
// (:1528-1533).
func TestRevocationLookupFailures(t *testing.T) {
	t.Parallel()
	x := newRevocationFixture(t)
	// NOT_FOUND (the fake's default).
	if first, second := x.dacRevoked(t), x.dacRevoked(t); first || second || x.f.hit(pointsPath(x.pki.pai)) != 1 {
		t.Fatalf("NOT_FOUND looked up %d times", x.f.hit(pointsPath(x.pki.pai)))
	}

	y := newRevocationFixture(t)
	y.f.mu.Lock()
	y.f.failPath[pointsPath(y.pki.pai)] = 500
	y.f.mu.Unlock()
	if first, second := y.dacRevoked(t), y.dacRevoked(t); first || second || y.f.hit(pointsPath(y.pki.pai)) != 2 {
		t.Fatalf("a failed lookup was cached (%d)", y.f.hit(pointsPath(y.pki.pai)))
	}

	z := newRevocationFixture(t)
	crlPath, _ := z.paiCRL(t, crlSpec{revoked: []*big.Int{z.pki.dac.cert.SerialNumber}})
	z.f.route(crlPath, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusGone) })
	if first, second := z.dacRevoked(t), z.dacRevoked(t); first || second || z.f.hit(crlPath) != 1 {
		t.Fatalf("a refused CRL: %d downloads, want 1 (rejected, cached)", z.f.hit(crlPath))
	}

	w := newRevocationFixture(t)
	crlPath, _ = w.paiCRL(t, crlSpec{revoked: []*big.Int{w.pki.dac.cert.SerialNumber}})
	w.f.route(crlPath, func(rw http.ResponseWriter, _ *http.Request) { rw.WriteHeader(http.StatusServiceUnavailable) })
	if first, second := w.dacRevoked(t), w.dacRevoked(t); first || second || w.f.hit(crlPath) != 2 {
		t.Fatalf("a failing CRL: %d downloads, want 2 (transient)", w.f.hit(crlPath))
	}
}

// TestRevocationRejectedPoints: §6.2.6.1 and the metadata checks reject a
// point — the revocations it lists do not count.
func TestRevocationRejectedPoints(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, want string
		setup      func(t *testing.T, x *revocationFixture)
	}{
		{"size mismatch (:1537-1543)", "CRL size mismatch", func(t *testing.T, x *revocationFixture) {
			t.Helper()
			crl := signCRL(t, x.pki.pai, crlSpec{revoked: []*big.Int{x.pki.dac.cert.SerialNumber}})
			x.f.bytes("/crl/x", crl)
			p := revocationPoint(x.pki.pai, x.pki.pai, nil, false, 0xFFF1, 0x8000, x.f.url("/crl/x"), crl)
			p["dataFileSize"] = "1"
			x.f.publishPoints(x.pki.pai, p)
		}},
		{"digest mismatch (:1544-1552)", "CRL digest mismatch", func(t *testing.T, x *revocationFixture) {
			t.Helper()
			crl := signCRL(t, x.pki.pai, crlSpec{revoked: []*big.Int{x.pki.dac.cert.SerialNumber}})
			x.f.bytes("/crl/x", crl)
			p := revocationPoint(x.pki.pai, x.pki.pai, nil, false, 0xFFF1, 0x8000, x.f.url("/crl/x"), crl)
			p["dataDigest"] = "AAAA"
			x.f.publishPoints(x.pki.pai, p)
		}},
		{"unsupported critical extension (:1576-1581)", "unsupported critical extension 551d1b", func(t *testing.T, x *revocationFixture) {
			t.Helper()
			x.paiCRL(t, crlSpec{
				revoked:    []*big.Int{x.pki.dac.cert.SerialNumber},
				extensions: []pkix.Extension{{Id: asn1.ObjectIdentifier{2, 5, 29, 27}, Critical: true, Value: []byte{0x02, 0x01, 0x01}}},
			})
		}},
		{"signer of another vendor (step 4.1)", "does not match entry VendorID 65522", func(t *testing.T, x *revocationFixture) {
			t.Helper()
			crl := signCRL(t, x.pki.pai, crlSpec{revoked: []*big.Int{x.pki.dac.cert.SerialNumber}})
			x.f.bytes("/crl/x", crl)
			x.f.publishPoints(x.pki.pai, revocationPoint(x.pki.pai, x.pki.pai, nil, false, 0xFFF2, 0x8000, x.f.url("/crl/x"), crl))
		}},
		{"signer of another product (step 4.1)", "does not match entry ProductID 32769", func(t *testing.T, x *revocationFixture) {
			t.Helper()
			crl := signCRL(t, x.pki.pai, crlSpec{revoked: []*big.Int{x.pki.dac.cert.SerialNumber}})
			x.f.bytes("/crl/x", crl)
			x.f.publishPoints(x.pki.pai, revocationPoint(x.pki.pai, x.pki.pai, nil, false, 0xFFF1, 0x8001, x.f.url("/crl/x"), crl))
		}},
		{"CRL signed by another key (step 8)", "CRL signature verification failed", func(t *testing.T, x *revocationFixture) {
			t.Helper()
			other := mint(t, certSpec{cn: "Test PAI", vid: u16(0xFFF1), pid: u16(0x8000), ca: true, skid: x.pki.pai.skid()}, &x.pki.paa)
			crl := signCRL(t, other, crlSpec{revoked: []*big.Int{x.pki.dac.cert.SerialNumber}})
			x.f.bytes("/crl/x", crl)
			x.f.publishPoints(x.pki.pai, revocationPoint(x.pki.pai, x.pki.pai, nil, false, 0xFFF1, 0x8000, x.f.url("/crl/x"), crl))
		}},
		{"CRL of another authority key (step 7.1)", "CRL authority key identifier", func(t *testing.T, x *revocationFixture) {
			t.Helper()
			other := mint(t, certSpec{cn: "Test PAI", vid: u16(0xFFF1), pid: u16(0x8000), ca: true}, &x.pki.paa)
			crl := signCRL(t, other, crlSpec{revoked: []*big.Int{x.pki.dac.cert.SerialNumber}})
			x.f.bytes("/crl/x", crl)
			x.f.publishPoints(x.pki.pai, revocationPoint(x.pki.pai, x.pki.pai, nil, false, 0xFFF1, 0x8000, x.f.url("/crl/x"), crl))
		}},
		{"signer without cRLSign (RFC 5280 §6.3.3(f))", "lacks the cRLSign key usage", func(t *testing.T, x *revocationFixture) {
			t.Helper()
			weak := mint(t, certSpec{cn: "Test PAI", vid: u16(0xFFF1), pid: u16(0x8000), ca: true, usage: x509.KeyUsageCertSign, key: x.pki.pai.key, skid: x.pki.pai.skid()}, &x.pki.paa)
			crl := signCRL(t, x.pki.pai, crlSpec{revoked: []*big.Int{x.pki.dac.cert.SerialNumber}})
			x.f.bytes("/crl/x", crl)
			x.f.publishPoints(x.pki.pai, revocationPoint(x.pki.pai, weak, nil, false, 0xFFF1, 0x8000, x.f.url("/crl/x"), crl))
		}},
		{"PAI under an untrusted PAA (step 5)", "CRL signer certificate is unusable", func(t *testing.T, x *revocationFixture) {
			t.Helper()
			rogue := newTestPKI(t)
			crl := signCRL(t, rogue.pai, crlSpec{revoked: []*big.Int{x.pki.dac.cert.SerialNumber}})
			x.f.bytes("/crl/x", crl)
			p := revocationPoint(rogue.pai, rogue.pai, nil, false, 0xFFF1, 0x8000, x.f.url("/crl/x"), crl)
			p["issuerSubjectKeyID"] = x.pki.pai.skidHex()
			x.f.publishPoints(x.pki.pai, p)
		}},
		{"partitioned without an Issuing Distribution Point (step 7.2)", "Issuing Distribution Point names no single URI", func(t *testing.T, x *revocationFixture) {
			t.Helper()
			crl := signCRL(t, x.pki.pai, crlSpec{revoked: []*big.Int{x.pki.dac.cert.SerialNumber}})
			x.f.bytes("/crl/a", crl)
			x.f.bytes("/crl/b", crl)
			x.f.publishPoints(x.pki.pai,
				revocationPoint(x.pki.pai, x.pki.pai, nil, false, 0xFFF1, 0x8000, x.f.url("/crl/a"), crl),
				revocationPoint(x.pki.pai, x.pki.pai, nil, false, 0xFFF1, 0x8000, x.f.url("/crl/b"), crl))
		}},
		{"self-signed signer on a PAI entry", "Self-signed CRL signer on an entry that is not for a PAA", func(t *testing.T, x *revocationFixture) {
			t.Helper()
			key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			id := keyID(&key.PublicKey)
			self := mint(t, certSpec{cn: "Self", vid: u16(0xFFF1), pid: u16(0x8000), ca: true, key: key, skid: id, akid: id}, nil)
			crl := signCRL(t, self, crlSpec{revoked: []*big.Int{x.pki.dac.cert.SerialNumber}})
			x.f.bytes("/crl/x", crl)
			p := revocationPoint(x.pki.pai, self, nil, false, 0xFFF1, 0x8000, x.f.url("/crl/x"), crl)
			x.f.publishPoints(x.pki.pai, p)
		}},
		{"not a CRL", "is malformed", func(t *testing.T, x *revocationFixture) {
			t.Helper()
			x.f.bytes("/crl/x", []byte{0x30, 0x05, 0x01})
			p := revocationPoint(x.pki.pai, x.pki.pai, nil, false, 0xFFF1, 0x8000, x.f.url("/crl/x"), []byte{0x30, 0x05, 0x01})
			x.f.publishPoints(x.pki.pai, p)
		}},
		{"signer not PEM", "No BEGIN line", func(t *testing.T, x *revocationFixture) {
			t.Helper()
			p := revocationPoint(x.pki.pai, x.pki.pai, nil, false, 0xFFF1, 0x8000, x.f.url("/crl/x"), nil)
			p["crlSignerCertificate"] = "nope"
			x.f.publishPoints(x.pki.pai, p)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			x := newRevocationFixture(t)
			tc.setup(t, x)
			if x.dacRevoked(t) {
				t.Fatal("a rejected point revoked the DAC")
			}
			if !x.log.has(tc.want) {
				t.Fatalf("no %q among the logged errors:\n%s", tc.want, x.log.all())
			}
		})
	}
}

// TestRevocationPartitioned: several CRLs of one issuer and vendor each
// name their own URL in a critical Issuing Distribution Point (§6.2.6.1
// step 7.2) and contribute; points of another revocation type or another
// issuer are not read (:1460-1468).
func TestRevocationPartitioned(t *testing.T) {
	t.Parallel()
	x := newRevocationFixture(t)
	a := signCRL(t, x.pki.pai, crlSpec{revoked: []*big.Int{big.NewInt(1)}, extensions: []pkix.Extension{idpExtension(t, x.f.url("/crl/a"))}})
	b := signCRL(t, x.pki.pai, crlSpec{revoked: []*big.Int{x.pki.dac.cert.SerialNumber}, extensions: []pkix.Extension{idpExtension(t, x.f.url("/crl/b"))}})
	x.f.bytes("/crl/a", a)
	x.f.bytes("/crl/b", b)
	other := revocationPoint(x.pki.pai, x.pki.pai, nil, false, 0xFFF1, 0x8000, x.f.url("/crl/c"), a)
	other["revocationType"] = 2
	foreign := revocationPoint(x.pki.pai, x.pki.pai, nil, false, 0xFFF1, 0x8000, x.f.url("/crl/d"), a)
	foreign["issuerSubjectKeyID"] = "00"
	x.f.publishPoints(x.pki.pai,
		revocationPoint(x.pki.pai, x.pki.pai, nil, false, 0xFFF1, 0x8000, x.f.url("/crl/a"), a),
		revocationPoint(x.pki.pai, x.pki.pai, nil, false, 0xFFF1, 0x8000, x.f.url("/crl/b"), b),
		other, foreign)
	if !x.dacRevoked(t) {
		t.Fatal("the second partition's revocation is missing")
	}
	if x.f.hit("/crl/c") != 0 || x.f.hit("/crl/d") != 0 {
		t.Fatal("a point of another type or issuer was downloaded")
	}
}

// TestRevocationPAASignedCRL: a PAA signs the CRL of its PAIs itself (the
// approved PAA, byte for byte) or through a delegated signer it issued
// (§6.2.6.1 step 5); the PAI's serial is revoked.
func TestRevocationPAASignedCRL(t *testing.T) {
	t.Parallel()
	for _, delegated := range []bool{false, true} {
		x := newRevocationFixture(t)
		signer := x.pki.paa
		if delegated {
			signer = mint(t, certSpec{cn: "PAA CRL signer", vid: u16(0xFFF1), usage: x509.KeyUsageCRLSign}, &x.pki.paa)
		}
		crl := signCRL(t, signer, crlSpec{revoked: []*big.Int{x.pki.pai.cert.SerialNumber}})
		x.f.bytes("/crl/paa", crl)
		x.f.publishPoints(x.pki.paa, revocationPoint(x.pki.paa, signer, nil, true, 0xFFF1, 0, x.f.url("/crl/paa"), crl))
		pai := x.pki.pai.cert
		revoked, err := x.s.IsRevoked(pai.AuthorityKeyId, serialContent(pai.SerialNumber), hexUpper(pai.RawIssuer))
		if err != nil || !revoked {
			t.Fatalf("delegated=%v: PAI not revoked (%v)", delegated, err)
		}
	}

	// A self-signed signer that is not the approved PAA.
	x := newRevocationFixture(t)
	impostor := mint(t, certSpec{cn: "Test PAA", vid: u16(0xFFF1), ca: true, noAKID: true, skid: x.pki.paa.skid()}, nil)
	crl := signCRL(t, impostor, crlSpec{revoked: []*big.Int{x.pki.pai.cert.SerialNumber}})
	x.f.bytes("/crl/paa", crl)
	x.f.publishPoints(x.pki.paa, revocationPoint(x.pki.paa, impostor, nil, true, 0xFFF1, 0, x.f.url("/crl/paa"), crl))
	pai := x.pki.pai.cert
	if revoked, _ := x.s.IsRevoked(pai.AuthorityKeyId, serialContent(pai.SerialNumber), hexUpper(pai.RawIssuer)); revoked {
		t.Fatal("an impostor PAA's CRL revoked the PAI")
	}
	// A delegated PAA signer that is a CA.
	y := newRevocationFixture(t)
	caSigner := mint(t, certSpec{cn: "CA signer", vid: u16(0xFFF1), ca: true}, &y.pki.paa)
	crl = signCRL(t, caSigner, crlSpec{revoked: []*big.Int{y.pki.pai.cert.SerialNumber}})
	y.f.bytes("/crl/paa", crl)
	y.f.publishPoints(y.pki.paa, revocationPoint(y.pki.paa, caSigner, nil, true, 0xFFF1, 0, y.f.url("/crl/paa"), crl))
	pai = y.pki.pai.cert
	if revoked, _ := y.s.IsRevoked(pai.AuthorityKeyId, serialContent(pai.SerialNumber), hexUpper(pai.RawIssuer)); revoked {
		t.Fatal("a CA delegated signer's CRL counted")
	}
}

// TestRevocationPAIDelegated: a CRL signer the PAI issued, with the PAI as
// CRLSignerDelegator (§11.23.11.6), revokes the DAC; the delegator must
// be a CA.
func TestRevocationPAIDelegated(t *testing.T) {
	t.Parallel()
	x := newRevocationFixture(t)
	signer := mint(t, certSpec{cn: "PAI CRL signer", vid: u16(0xFFF1), pid: u16(0x8000), usage: x509.KeyUsageCRLSign}, &x.pki.pai)
	crl := signCRL(t, signer, crlSpec{revoked: []*big.Int{x.pki.dac.cert.SerialNumber}})
	x.f.bytes("/crl/d", crl)
	pai := x.pki.pai
	x.f.publishPoints(x.pki.pai, revocationPoint(x.pki.pai, signer, &pai, false, 0xFFF1, 0x8000, x.f.url("/crl/d"), crl))
	if !x.dacRevoked(t) {
		t.Fatal("the delegated CRL did not revoke the DAC")
	}
	y := newRevocationFixture(t)
	signer = mint(t, certSpec{cn: "PAI CRL signer", vid: u16(0xFFF1), pid: u16(0x8000), usage: x509.KeyUsageCRLSign}, &y.pki.pai)
	crl = signCRL(t, signer, crlSpec{revoked: []*big.Int{y.pki.dac.cert.SerialNumber}})
	y.f.bytes("/crl/d", crl)
	notCA := signer
	y.f.publishPoints(y.pki.pai, revocationPoint(y.pki.pai, signer, &notCA, false, 0xFFF1, 0x8000, y.f.url("/crl/d"), crl))
	if y.dacRevoked(t) {
		t.Fatal("a delegator that is not a PAI was accepted")
	}
}

// TestRevocationExpiredCRL: a CRL past its nextUpdate still counts.
// matter.js parses nextUpdate (DclCertificateService.ts:1849-1874) and
// returns it (nextUpdateMs) but no line of the service reads it; the
// port does the same.
func TestRevocationExpiredCRL(t *testing.T) {
	t.Parallel()
	x := newRevocationFixture(t)
	_, crl := x.paiCRL(t, crlSpec{revoked: []*big.Int{x.pki.dac.cert.SerialNumber}, nextUpdate: time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)})
	if !x.dacRevoked(t) {
		t.Fatal("an expired CRL was ignored")
	}
	r, err := parseCRL(crl, nil)
	if err != nil || !r.nextUpdate.Equal(time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("nextUpdate %v (%v)", r.nextUpdate, err)
	}
}

// TestParseCRLIndirect: an indirect CRL's entries after a certificateIssuer
// naming another issuer are not the authority's (§6.2.6.1 step 10.1); a
// certificateIssuer that is not critical makes the CRL unusable.
func TestParseCRLIndirect(t *testing.T) {
	t.Parallel()
	pki := newTestPKI(t)
	other := mint(t, certSpec{cn: "Another PAI", vid: u16(0xFFF1), ca: true}, &pki.paa)
	gn, _ := asn1.Marshal(asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: 4, IsCompound: true, Bytes: other.cert.RawSubject})
	names, _ := asn1.Marshal(asn1.RawValue{Class: asn1.ClassUniversal, Tag: asn1.TagSequence, IsCompound: true, Bytes: gn})
	mk := func(critical bool) []byte {
		der, err := x509.CreateRevocationList(randReader{}, &x509.RevocationList{
			Number: big.NewInt(1), ThisUpdate: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), NextUpdate: time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC),
			RevokedCertificateEntries: []x509.RevocationListEntry{
				{SerialNumber: big.NewInt(1), RevocationTime: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)},
				{
					SerialNumber: big.NewInt(2), RevocationTime: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
					ExtraExtensions: []pkix.Extension{{Id: asn1.ObjectIdentifier{2, 5, 29, 29}, Critical: critical, Value: names}},
				},
				{SerialNumber: big.NewInt(3), RevocationTime: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)},
			},
		}, pki.pai.cert, pki.pai.key)
		if err != nil {
			t.Fatal(err)
		}
		return der
	}
	r, err := parseCRL(mk(true), pki.pai.cert.RawSubject)
	if err != nil || r.unsupportedCriticalExtension != "" {
		t.Fatalf("parse %v %q", err, r.unsupportedCriticalExtension)
	}
	if _, ok := r.serials["01"]; !ok || len(r.serials) != 1 {
		t.Fatalf("serials %v, want 01 alone", r.serials)
	}
	r, _ = parseCRL(mk(false), pki.pai.cert.RawSubject)
	if r.unsupportedCriticalExtension != "551d1d" {
		t.Fatalf("a non-critical certificateIssuer: %q", r.unsupportedCriticalExtension)
	}
	if _, err := parseCRL([]byte{0x30, 0x81}, nil); err == nil {
		t.Fatal("a truncated CRL parsed")
	}
	if r, err := parseCRL([]byte{0x30, 0x00}, nil); err != nil || r.tbs != nil {
		t.Fatal("an empty CRL")
	}
}

type randReader struct{}

func (randReader) Read(b []byte) (int, error) {
	for i := range b {
		b[i] = byte(i*31 + 7)
	}
	return len(b), nil
}

// TestSameName: RFC 5280 §7.1 as sameName reads it — case, whitespace and
// PrintableString versus UTF8String do not matter; values do.
func TestSameName(t *testing.T) {
	t.Parallel()
	enc := func(tag int, s string) []byte {
		v, _ := asn1.Marshal(asn1.RawValue{Class: asn1.ClassUniversal, Tag: tag, Bytes: []byte(s)})
		oid, _ := asn1.Marshal(oidCN)
		atv, _ := asn1.Marshal(asn1.RawValue{Class: asn1.ClassUniversal, Tag: asn1.TagSequence, IsCompound: true, Bytes: append(oid, v...)})
		set, _ := asn1.Marshal(asn1.RawValue{Class: asn1.ClassUniversal, Tag: asn1.TagSet, IsCompound: true, Bytes: atv})
		name, _ := asn1.Marshal(asn1.RawValue{Class: asn1.ClassUniversal, Tag: asn1.TagSequence, IsCompound: true, Bytes: set})
		return name
	}
	if !sameName(enc(asn1.TagPrintableString, "Matter  PAA "), enc(asn1.TagUTF8String, "matter paa")) {
		t.Fatal("equivalent names differ")
	}
	if sameName(enc(asn1.TagUTF8String, "Matter PAA"), enc(asn1.TagUTF8String, "Matter PAI")) {
		t.Fatal("different names match")
	}
	if !sameName(enc(asn1.TagOctetString, "x"), enc(asn1.TagOctetString, "x")) || sameName(enc(asn1.TagOctetString, "x"), enc(asn1.TagOctetString, "y")) {
		t.Fatal("non-string attributes")
	}
	if sameName([]byte{0x30, 0x81}, []byte{0x30, 0x00}) || sameNameHex("zz", "00") || sameNameHex("0", "0") {
		t.Fatal("malformed names match")
	}
	if !strings.HasPrefix(idText(nil), "undefined") || idText(u16(5)) != "5" {
		t.Fatal("idText")
	}
}
