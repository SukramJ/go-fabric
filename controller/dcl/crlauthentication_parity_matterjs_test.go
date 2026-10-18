// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package dcl

// A port of matter.js packages/protocol/test/dcl/DclCrlAuthenticationTest.ts
// at 862ad75f: the CRL authentication of DclCertificateService.isRevoked
// (Matter Core §6.2.6.1) — signer forms, vendor and product, the CRL
// checks, the authority name, partitions, retrying and ML-DSA.
//
// The Go surface is CertificateStore.IsRevoked over the shared fakeDCL
// (loopback, as the package's other revocation tests): a matter.js
// `revoked(skid, points, serial, trusted)` is dclCrlRevoked, MockFetch's
// call log is fakeDCL.hit. Certificates and CRLs are built as the test's
// issue() and TestHelpers.ts's buildSignedTestCrl build them — UTF8String
// names, one attribute per RDN — by hand, since crypto/x509 cannot state
// a foreign issuer name, a forged signature or a malformed CRL.

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/mldsa"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // key identifiers are SHA-1 of the key, as the test computes them
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"testing"
	"time"
)

// DclCrlAuthenticationTest.ts:33-34.
const (
	dclCrlRevokedSerial = "01AB"
	dclCrlOtherRevoked  = "02CD"
)

// dclCrlIDs are the Matter vendor and product id of a Name.
type dclCrlIDs struct{ vid, pid *uint16 }

// dclCrlTLV is one DER TLV.
func dclCrlTLV(tag byte, parts ...[]byte) []byte {
	var content []byte
	for _, p := range parts {
		content = append(content, p...)
	}
	out := []byte{tag}
	if l := len(content); l < 0x80 {
		out = append(out, byte(l))
	} else {
		var lb []byte
		for v := l; v > 0; v >>= 8 {
			lb = append([]byte{byte(v & 0xff)}, lb...)
		}
		out = append(out, 0x80|byte(len(lb)))
		out = append(out, lb...)
	}
	return append(out, content...)
}

func dclCrlSeq(parts ...[]byte) []byte { return dclCrlTLV(derSequence, parts...) }

func dclCrlOID(hexOID string) []byte {
	b, err := hex.DecodeString(hexOID)
	if err != nil {
		panic(err)
	}
	return dclCrlTLV(derObjectID, b)
}

// dclCrlAttr is one RDN of one attribute, X520.CommonName or
// GenericMatterObject (asn.ts:60-79): SET { SEQUENCE { OID, string } }.
func dclCrlAttr(oidHex, value string, tag byte) []byte {
	return dclCrlTLV(0x31, dclCrlSeq(dclCrlOID(oidHex), dclCrlTLV(tag, []byte(value))))
}

// dclCrlName is nameOf (DclCrlAuthenticationTest.ts:47-53) as DER: the
// common name and the ids as UTF8String, or the common name alone as
// PrintableString when printable (X520.CommonName(…, true)).
func dclCrlName(cn string, ids dclCrlIDs, printable bool) []byte {
	tag := byte(derUTF8String)
	if printable {
		tag = derPrintableString
	}
	rdns := [][]byte{dclCrlAttr("550403", cn, tag)}
	if ids.vid != nil {
		rdns = append(rdns, dclCrlAttr("2b0601040182a27c0201", fmt.Sprintf("%04X", *ids.vid), derUTF8String))
	}
	if ids.pid != nil {
		rdns = append(rdns, dclCrlAttr("2b0601040182a27c0202", fmt.Sprintf("%04X", *ids.pid), derUTF8String))
	}
	return dclCrlSeq(rdns...)
}

// dclCrlSigner is TestCrlSigner (TestHelpers.ts:151-156).
type dclCrlSigner struct {
	key        crypto.Signer // *ecdsa.PrivateKey or *mldsa.PrivateKey
	skid       []byte
	subjectDER []byte
}

// dclCrlIssued is Issued (DclCrlAuthenticationTest.ts:38-45).
type dclCrlIssued struct {
	der  []byte
	key  *ecdsa.PrivateKey
	skid []byte
	name string
	ids  dclCrlIDs
}

func (c dclCrlIssued) crlSigner() dclCrlSigner {
	return dclCrlSigner{key: c.key, skid: c.skid, subjectDER: dclCrlName(c.name, c.ids, false)}
}

// dclCrlIssueOptions are issue()'s options (:61-78).
type dclCrlIssueOptions struct {
	issuer             *dclCrlIssued
	ca                 bool
	noAKID             bool
	key, signingKey    *ecdsa.PrivateKey
	vid, pid           *uint16
	withoutCRLSign     bool
	withoutKeyCertSign bool
	issuerName         string
	serial             int64
}

// dclCrlIssue is issue() (:59-114): an EC certificate, self-signed without
// an issuer.
func dclCrlIssue(t *testing.T, name string, o dclCrlIssueOptions) dclCrlIssued {
	t.Helper()
	key := o.key
	if key == nil {
		key, _ = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	}
	skid := keyID(&key.PublicKey)
	ids := dclCrlIDs{o.vid, o.pid}
	issuerName, issuerIDs, akid, signKey := name, ids, skid, key
	if o.issuer != nil {
		issuerName, issuerIDs, akid, signKey = o.issuer.name, o.issuer.ids, o.issuer.skid, o.issuer.key
	}
	if o.issuerName != "" {
		issuerName = o.issuerName
	}
	if o.signingKey != nil {
		signKey = o.signingKey
	}
	var usage x509.KeyUsage
	switch {
	case o.ca:
		if !o.withoutKeyCertSign {
			usage |= x509.KeyUsageCertSign
		}
		if !o.withoutCRLSign {
			usage |= x509.KeyUsageCRLSign
		}
	case o.withoutCRLSign:
		usage = x509.KeyUsageDigitalSignature
	default:
		usage = x509.KeyUsageCRLSign
	}
	serial := o.serial
	if serial == 0 {
		serial = 1
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(serial),
		RawSubject:   dclCrlName(name, ids, false),
		// A day either side of the store's clock (:94).
		NotBefore:             time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC),
		NotAfter:              time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC),
		KeyUsage:              usage,
		BasicConstraintsValid: true,
		IsCA:                  o.ca,
		SubjectKeyId:          skid,
		SignatureAlgorithm:    x509.ECDSAWithSHA256,
	}
	if o.ca {
		tmpl.MaxPathLen, tmpl.MaxPathLenZero = 0, o.issuer != nil
		if o.issuer == nil {
			tmpl.MaxPathLen = 1
		}
	}
	// crypto/x509 states the parent's subject as issuer and, for a
	// certificate it does not see as self-signed, the parent's SKID as AKID.
	parent := &x509.Certificate{RawSubject: dclCrlName(issuerName, issuerIDs, false), PublicKey: &signKey.PublicKey}
	if !o.noAKID {
		tmpl.AuthorityKeyId, parent.SubjectKeyId = akid, akid
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, &key.PublicKey, signKey)
	if err != nil {
		t.Fatal(err)
	}
	return dclCrlIssued{der: der, key: key, skid: skid, name: name, ids: ids}
}

// The signature algorithm identifiers: X962.EcdsaWithSHA256, ecdsa-with-
// SHA384 (:462) and MlDsa.AlgorithmIdentifier (MlDsa.ts:68-76). Its OIDs
// are the PARAMETERS table (MlDsa.ts:30-35).
var (
	dclCrlECDSAWithSHA256 = dclCrlSeq(dclCrlOID("2a8648ce3d040302"))
	dclCrlECDSAWithSHA384 = dclCrlSeq(dclCrlOID("2a8648ce3d040303"))
	dclCrlMLDSA44         = dclCrlSeq(dclCrlOID("608648016503040311"))
)

// dclCrlExtension is DerObject(oid, { critical?, value }).
func dclCrlExtension(oidHex string, critical bool, value []byte) []byte {
	parts := [][]byte{dclCrlOID(oidHex)}
	if critical {
		parts = append(parts, dclCrlTLV(derBoolean, []byte{0xff}))
	}
	return dclCrlSeq(append(parts, dclCrlTLV(derOctetString, value))...)
}

// dclCrlOptions are buildSignedTestCrl's options (TestHelpers.ts:180-196).
type dclCrlOptions struct {
	issuerDN              []byte
	distributionPoints    []string
	distributionPointWeak bool // distributionPointCritical: false
	withoutSignature      bool
	signaturePadding      byte
	signatureAlgorithm    []byte
	tbsSignatureAlgorithm []byte
	extensions            [][]byte
	// entryExtensions apply to every entry; perEntry, when set, to the
	// entry of its index (nil: none).
	entryExtensions [][]byte
	perEntry        [][][]byte
}

// dclCrlBuild is buildSignedTestCrl and crlTbs (TestHelpers.ts:176-296).
func dclCrlBuild(t *testing.T, signer dclCrlSigner, serials []string, o dclCrlOptions) []byte {
	t.Helper()
	alg := o.signatureAlgorithm
	if alg == nil {
		alg = dclCrlECDSAWithSHA256
		if _, ok := signer.key.(*mldsa.PrivateKey); ok {
			alg = dclCrlMLDSA44
		}
	}
	exts := [][]byte{dclCrlExtension(oidAuthorityKeyID, false, dclCrlSeq(dclCrlTLV(derKeyIdentifier, signer.skid)))}
	exts = append(exts, o.extensions...)
	if o.distributionPoints != nil {
		uris := make([]byte, 0, 64*len(o.distributionPoints))
		for _, u := range o.distributionPoints {
			uris = append(uris, dclCrlTLV(derURIGeneralName, []byte(u))...)
		}
		exts = append(exts, dclCrlExtension(oidIssuingDistributionPt, !o.distributionPointWeak,
			dclCrlSeq(dclCrlTLV(derContext0, dclCrlTLV(derContext0, uris)))))
	}
	tbsAlg := o.tbsSignatureAlgorithm
	if tbsAlg == nil {
		tbsAlg = alg
	}
	issuer := o.issuerDN
	if issuer == nil {
		issuer = signer.subjectDER
	}
	utc := func(s string) []byte { return dclCrlTLV(derUTCTime, []byte(s)) }
	tbsParts := [][]byte{dclCrlTLV(derInteger, []byte{1}), tbsAlg, issuer, utc("250101000000Z"), utc("260101000000Z")}
	if len(serials) > 0 {
		entries := make([][]byte, 0, len(serials))
		for i, s := range serials {
			serial, err := hex.DecodeString(s)
			if err != nil {
				t.Fatal(err)
			}
			entry := [][]byte{dclCrlTLV(derInteger, serial), utc("250101000000Z")}
			entryExts := o.entryExtensions
			if o.perEntry != nil {
				entryExts = o.perEntry[i]
			}
			if entryExts != nil {
				entry = append(entry, dclCrlSeq(entryExts...))
			}
			entries = append(entries, dclCrlSeq(entry...))
		}
		tbsParts = append(tbsParts, dclCrlSeq(entries...))
	}
	tbsParts = append(tbsParts, dclCrlTLV(derContext0, dclCrlSeq(exts...)))
	tbs := dclCrlSeq(tbsParts...)
	var signature []byte
	if !o.withoutSignature {
		var err error
		switch k := signer.key.(type) {
		case *mldsa.PrivateKey:
			signature, err = k.Sign(nil, tbs, nil)
		case *ecdsa.PrivateKey:
			digest := sha256.Sum256(tbs)
			signature, err = ecdsa.SignASN1(rand.Reader, k, digest[:])
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	return dclCrlSeq(tbs, alg, dclCrlTLV(derBitString, append([]byte{o.signaturePadding}, signature...)))
}

// dclCrlPoint is Point (DclCrlAuthenticationTest.ts:118-130); path is the
// CRL's path on the fake ledger, "/<index>.crl" when empty.
type dclCrlPoint struct {
	signer     []byte
	delegator  []byte
	isPAA      bool
	vid, pid   *uint16
	issuerSKID []byte
	dataDigest string
	path       string
	crl        []byte
}

// dclCrlTrusted is a trust-store entry serve() adds (:159).
type dclCrlTrusted struct {
	der  []byte
	kind Kind
}

func dclCrlPAA(der []byte) dclCrlTrusted { return dclCrlTrusted{der: der, kind: KindPAA} }

func dclCrlPEM(der []byte) string {
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

func dclCrlPath(p dclCrlPoint, i int) string {
	if p.path != "" {
		return p.path
	}
	return fmt.Sprintf("/%d.crl", i)
}

// dclCrlServe is serve() (:156-198): the ledger's points for issuerSKID,
// their CRLs, and a store trusting trusted.
func dclCrlServe(t *testing.T, f *fakeDCL, issuerSKID []byte, points []dclCrlPoint, trusted ...dclCrlTrusted) *CertificateStore {
	t.Helper()
	skid := hexUpper(issuerSKID)
	f.json("/dcl/pki/root-certificates", 200, map[string]any{
		"approvedRootCertificates": map[string]any{"schemaVersion": 0, "certs": []any{}},
	})
	raw := make([]any, 0, len(points))
	for i := range points {
		p := &points[i]
		vid := uint16(0xFFF1)
		if p.vid != nil {
			vid = *p.vid
		}
		rec := map[string]any{
			"vid": vid, "isPAA": p.isPAA, "label": fmt.Sprintf("point-%d", i), "crlSignerDelegator": "",
			"crlSignerCertificate": dclCrlPEM(p.signer), "issuerSubjectKeyID": skid, "dataURL": f.url(dclCrlPath(*p, i)),
			"dataFileSize": "", "dataDigest": p.dataDigest, "dataDigestType": 0, "revocationType": 1, "schemaVersion": 0,
		}
		if p.pid != nil {
			rec["pid"] = *p.pid
		}
		if p.delegator != nil {
			rec["crlSignerDelegator"] = dclCrlPEM(p.delegator)
		}
		if p.issuerSKID != nil {
			rec["issuerSubjectKeyID"] = hexUpper(p.issuerSKID)
		}
		if p.dataDigest != "" {
			rec["dataDigestType"] = 1
		}
		raw = append(raw, rec)
		f.bytes(dclCrlPath(*p, i), p.crl)
	}
	f.json("/dcl/pki/revocation-points/"+skid, 200, map[string]any{
		"pkiRevocationDistributionPointsByIssuerSubjectKeyID": map[string]any{
			"issuerSubjectKeyID": skid, "points": raw, "schemaVersion": 0,
		},
	})
	log := &logRecorder{}
	s := openStore(t, CertificateOptions{Client: f.client(), Clock: newManualClock(), UpdateInterval: -1, Logger: slog.New(log)})
	// Why a point was ignored, for `go test -v`.
	t.Cleanup(func() {
		if all := log.all(); all != "" {
			t.Log(all)
		}
	})
	for _, tr := range trusted {
		if _, err := s.AddCertificate(context.Background(), tr.der, tr.kind, false); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

// dclCrlIsRevoked is dcl.isRevoked(hexOf(skid), serial[, issuerDn]).
func dclCrlIsRevoked(t *testing.T, s *CertificateStore, skid []byte, serial string, issuerDN []byte) bool {
	t.Helper()
	b, err := hex.DecodeString(serial)
	if err != nil {
		t.Fatal(err)
	}
	var dn string
	if issuerDN != nil {
		dn = hexUpper(issuerDN)
	}
	revoked, err := s.IsRevoked(skid, b, dn)
	if err != nil {
		t.Fatal(err)
	}
	return revoked
}

// dclCrlRevoked is revoked() (:200-207) with the default serial.
func dclCrlRevoked(t *testing.T, skid []byte, points []dclCrlPoint, trusted ...dclCrlTrusted) bool {
	t.Helper()
	s := dclCrlServe(t, newFakeDCL(t), skid, points, trusted...)
	return dclCrlIsRevoked(t, s, skid, dclCrlRevokedSerial, nil)
}

func dclCrlWant(t *testing.T, got, want bool) {
	t.Helper()
	if got != want {
		t.Fatalf("revoked = %v, want %v", got, want)
	}
}

// crlBy (:209-210).
func dclCrlBy(t *testing.T, signer dclCrlSigner, o dclCrlOptions, serials ...string) []byte {
	t.Helper()
	if serials == nil {
		serials = []string{dclCrlRevokedSerial}
	}
	return dclCrlBuild(t, signer, serials, o)
}

// TestDclCrlAuthenticationSignerForms is describe("signer forms
// (§11.23.11.6)") (:212-373).
func TestDclCrlAuthenticationSignerForms(t *testing.T) {
	t.Parallel()
	paa := dclCrlIssue(t, "Test PAA", dclCrlIssueOptions{ca: true})
	vid := u16(0xFFF1)
	by := func(t *testing.T, c dclCrlIssued) []byte {
		t.Helper()
		return dclCrlBy(t, c.crlSigner(), dclCrlOptions{})
	}

	t.Run("accepts a self-signed PAA that is approved", func(t *testing.T) {
		t.Parallel()
		dclCrlWant(t, dclCrlRevoked(t, paa.skid, []dclCrlPoint{{signer: paa.der, isPAA: true, crl: by(t, paa)}}, dclCrlPAA(paa.der)), true)
	})
	t.Run("accepts an approved self-signed PAA without an authority key identifier", func(t *testing.T) {
		t.Parallel()
		bare := dclCrlIssue(t, "Bare PAA", dclCrlIssueOptions{ca: true, noAKID: true})
		dclCrlWant(t, dclCrlRevoked(t, bare.skid, []dclCrlPoint{{signer: bare.der, isPAA: true, crl: by(t, bare)}}, dclCrlPAA(bare.der)), true)
	})
	t.Run("ignores a self-signed signer that is not an approved PAA", func(t *testing.T) {
		t.Parallel()
		stranger := dclCrlIssue(t, "Stranger", dclCrlIssueOptions{ca: true})
		dclCrlWant(t, dclCrlRevoked(t, stranger.skid, []dclCrlPoint{{signer: stranger.der, isPAA: true, crl: by(t, stranger)}}, dclCrlPAA(paa.der)), false)
	})
	t.Run("ignores a self-signed signer that differs from the approved PAA with its key", func(t *testing.T) {
		t.Parallel()
		reissued := dclCrlIssue(t, paa.name, dclCrlIssueOptions{ca: true, key: paa.key, serial: 2})
		if hexUpper(reissued.skid) != hexUpper(paa.skid) {
			t.Fatal("the re-issued PAA has another key identifier")
		}
		dclCrlWant(t, dclCrlRevoked(t, paa.skid, []dclCrlPoint{{signer: reissued.der, isPAA: true, crl: by(t, paa)}}, dclCrlPAA(paa.der)), false)
	})
	t.Run("ignores a self-signed signer on an entry for a PAI", func(t *testing.T) {
		t.Parallel()
		dclCrlWant(t, dclCrlRevoked(t, paa.skid, []dclCrlPoint{{signer: paa.der, isPAA: false, crl: by(t, paa)}}, dclCrlPAA(paa.der)), false)
	})
	for _, withAKID := range []bool{true, false} {
		word := "without"
		if withAKID {
			word = "with"
		}
		t.Run("accepts a signer the entry's PAA delegated "+word+" an authority key identifier", func(t *testing.T) {
			t.Parallel()
			delegate := dclCrlIssue(t, "PAA CRL signer", dclCrlIssueOptions{issuer: &paa, noAKID: !withAKID})
			dclCrlWant(t, dclCrlRevoked(t, paa.skid, []dclCrlPoint{{signer: delegate.der, isPAA: true, crl: by(t, delegate)}}, dclCrlPAA(paa.der)), true)
		})
	}
	t.Run("ignores a PAA-delegated signer that names another issuer", func(t *testing.T) {
		t.Parallel()
		delegate := dclCrlIssue(t, "PAA CRL signer", dclCrlIssueOptions{issuer: &paa, issuerName: "Not the PAA"})
		dclCrlWant(t, dclCrlRevoked(t, paa.skid, []dclCrlPoint{{signer: delegate.der, isPAA: true, crl: by(t, delegate)}}, dclCrlPAA(paa.der)), false)
	})
	t.Run("accepts a PAA-delegated signer that shares its PAA's name", func(t *testing.T) {
		t.Parallel()
		delegate := dclCrlIssue(t, paa.name, dclCrlIssueOptions{issuer: &paa})
		dclCrlWant(t, dclCrlRevoked(t, paa.skid, []dclCrlPoint{{signer: delegate.der, isPAA: true, crl: by(t, delegate)}}, dclCrlPAA(paa.der)), true)
	})
	t.Run("ignores a PAA-delegated signer with the PAA's name but another key's signature", func(t *testing.T) {
		t.Parallel()
		forger, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		delegate := dclCrlIssue(t, "PAA CRL signer", dclCrlIssueOptions{issuer: &paa, signingKey: forger})
		dclCrlWant(t, dclCrlRevoked(t, paa.skid, []dclCrlPoint{{signer: delegate.der, isPAA: true, crl: by(t, delegate)}}, dclCrlPAA(paa.der)), false)
	})
	t.Run("ignores a PAI posing as a PAA-delegated signer", func(t *testing.T) {
		t.Parallel()
		pai := dclCrlIssue(t, "Test PAI", dclCrlIssueOptions{issuer: &paa, ca: true, vid: vid})
		dclCrlWant(t, dclCrlRevoked(t, paa.skid, []dclCrlPoint{{signer: pai.der, isPAA: true, crl: by(t, pai)}}, dclCrlPAA(paa.der)), false)
	})
	t.Run("ignores a signer without the cRLSign key usage", func(t *testing.T) {
		t.Parallel()
		delegate := dclCrlIssue(t, "PAA CRL signer", dclCrlIssueOptions{issuer: &paa, withoutCRLSign: true})
		dclCrlWant(t, dclCrlRevoked(t, paa.skid, []dclCrlPoint{{signer: delegate.der, isPAA: true, crl: by(t, delegate)}}, dclCrlPAA(paa.der)), false)
	})
	t.Run("ignores a delegated signer anchored at a CD signer rather than a PAA", func(t *testing.T) {
		t.Parallel()
		delegate := dclCrlIssue(t, "PAA CRL signer", dclCrlIssueOptions{issuer: &paa})
		dclCrlWant(t, dclCrlRevoked(t, paa.skid, []dclCrlPoint{{signer: delegate.der, isPAA: true, crl: by(t, delegate)}},
			dclCrlTrusted{der: paa.der, kind: KindCDSigner}), false)
	})
	t.Run("ignores a PAA-delegated signer another PAA issued", func(t *testing.T) {
		t.Parallel()
		otherPAA := dclCrlIssue(t, "Other PAA", dclCrlIssueOptions{ca: true})
		delegate := dclCrlIssue(t, "PAA CRL signer", dclCrlIssueOptions{issuer: &otherPAA})
		dclCrlWant(t, dclCrlRevoked(t, paa.skid, []dclCrlPoint{{signer: delegate.der, isPAA: true, crl: by(t, delegate)}},
			dclCrlPAA(paa.der), dclCrlPAA(otherPAA.der)), false)
	})
	t.Run("accepts a PAI signing its own CRL", func(t *testing.T) {
		t.Parallel()
		pai := dclCrlIssue(t, "Test PAI", dclCrlIssueOptions{issuer: &paa, ca: true, vid: vid})
		dclCrlWant(t, dclCrlRevoked(t, pai.skid, []dclCrlPoint{{signer: pai.der, crl: by(t, pai)}}, dclCrlPAA(paa.der)), true)
	})
	t.Run("ignores a PAI that does not chain to an approved PAA", func(t *testing.T) {
		t.Parallel()
		strangerPAA := dclCrlIssue(t, "Stranger PAA", dclCrlIssueOptions{ca: true})
		pai := dclCrlIssue(t, "Test PAI", dclCrlIssueOptions{issuer: &strangerPAA, ca: true, vid: vid})
		dclCrlWant(t, dclCrlRevoked(t, pai.skid, []dclCrlPoint{{signer: pai.der, crl: by(t, pai)}}, dclCrlPAA(paa.der)), false)
	})
	t.Run("ignores a PAI signing the CRL of another issuer", func(t *testing.T) {
		t.Parallel()
		pai := dclCrlIssue(t, "Test PAI", dclCrlIssueOptions{issuer: &paa, ca: true, vid: vid})
		otherPAI := dclCrlIssue(t, "Other PAI", dclCrlIssueOptions{issuer: &paa, ca: true, vid: vid})
		dclCrlWant(t, dclCrlRevoked(t, otherPAI.skid, []dclCrlPoint{{signer: pai.der, crl: by(t, pai)}}, dclCrlPAA(paa.der)), false)
	})
	t.Run("accepts a signer a PAI delegated, given that PAI as delegator", func(t *testing.T) {
		t.Parallel()
		pai := dclCrlIssue(t, "Test PAI", dclCrlIssueOptions{issuer: &paa, ca: true, vid: vid})
		delegate := dclCrlIssue(t, "PAI CRL signer", dclCrlIssueOptions{issuer: &pai})
		dclCrlWant(t, dclCrlRevoked(t, pai.skid, []dclCrlPoint{{signer: delegate.der, delegator: pai.der, crl: by(t, delegate)}}, dclCrlPAA(paa.der)), true)
	})
	t.Run("ignores a PAI-delegated signer given without its delegator", func(t *testing.T) {
		t.Parallel()
		pai := dclCrlIssue(t, "Test PAI", dclCrlIssueOptions{issuer: &paa, ca: true, vid: vid})
		delegate := dclCrlIssue(t, "PAI CRL signer", dclCrlIssueOptions{issuer: &pai})
		dclCrlWant(t, dclCrlRevoked(t, pai.skid, []dclCrlPoint{{signer: delegate.der, crl: by(t, delegate)}}, dclCrlPAA(paa.der)), false)
	})
	t.Run("ignores a delegator that is not a PAI", func(t *testing.T) {
		t.Parallel()
		// Issued by the approved PAA, so only its missing CA flag
		// disqualifies it (:343).
		notPAI := dclCrlIssue(t, "Not a PAI", dclCrlIssueOptions{issuer: &paa, vid: vid})
		delegate := dclCrlIssue(t, "PAI CRL signer", dclCrlIssueOptions{issuer: &notPAI})
		dclCrlWant(t, dclCrlRevoked(t, notPAI.skid, []dclCrlPoint{{signer: delegate.der, delegator: notPAI.der, crl: by(t, delegate)}}, dclCrlPAA(paa.der)), false)
	})
	t.Run("ignores a delegator that may not sign certificates", func(t *testing.T) {
		t.Parallel()
		pai := dclCrlIssue(t, "Test PAI", dclCrlIssueOptions{issuer: &paa, ca: true, vid: vid, withoutKeyCertSign: true})
		delegate := dclCrlIssue(t, "PAI CRL signer", dclCrlIssueOptions{issuer: &pai})
		dclCrlWant(t, dclCrlRevoked(t, pai.skid, []dclCrlPoint{{signer: delegate.der, delegator: pai.der, crl: by(t, delegate)}}, dclCrlPAA(paa.der)), false)
	})
	t.Run("ignores a PAI-delegated signer the delegator did not issue", func(t *testing.T) {
		t.Parallel()
		pai := dclCrlIssue(t, "Test PAI", dclCrlIssueOptions{issuer: &paa, ca: true, vid: vid})
		otherPAI := dclCrlIssue(t, "Other PAI", dclCrlIssueOptions{issuer: &paa, ca: true, vid: vid})
		delegate := dclCrlIssue(t, "PAI CRL signer", dclCrlIssueOptions{issuer: &otherPAI})
		dclCrlWant(t, dclCrlRevoked(t, pai.skid, []dclCrlPoint{{signer: delegate.der, delegator: pai.der, crl: by(t, delegate)}}, dclCrlPAA(paa.der)), false)
	})
}

// TestDclCrlAuthenticationVendorAndProduct is describe("vendor and product
// (steps 3 and 4)") (:375-426).
func TestDclCrlAuthenticationVendorAndProduct(t *testing.T) {
	t.Parallel()
	paa := dclCrlIssue(t, "Test PAA", dclCrlIssueOptions{ca: true})
	by := func(t *testing.T, c dclCrlIssued) []byte {
		t.Helper()
		return dclCrlBy(t, c.crlSigner(), dclCrlOptions{})
	}

	t.Run("ignores a PAA signer of another vendor", func(t *testing.T) {
		t.Parallel()
		vendorPAA := dclCrlIssue(t, "Vendor PAA", dclCrlIssueOptions{ca: true, vid: u16(0xFFF2)})
		dclCrlWant(t, dclCrlRevoked(t, vendorPAA.skid, []dclCrlPoint{{signer: vendorPAA.der, isPAA: true, vid: u16(0xFFF1), crl: by(t, vendorPAA)}}, dclCrlPAA(vendorPAA.der)), false)
	})
	t.Run("ignores a PAI without a VendorID", func(t *testing.T) {
		t.Parallel()
		pai := dclCrlIssue(t, "Test PAI", dclCrlIssueOptions{issuer: &paa, ca: true})
		dclCrlWant(t, dclCrlRevoked(t, pai.skid, []dclCrlPoint{{signer: pai.der, crl: by(t, pai)}}, dclCrlPAA(paa.der)), false)
	})
	t.Run("accepts a PAA without a VendorID", func(t *testing.T) {
		t.Parallel()
		dclCrlWant(t, dclCrlRevoked(t, paa.skid, []dclCrlPoint{{signer: paa.der, isPAA: true, crl: by(t, paa)}}, dclCrlPAA(paa.der)), true)
	})
	t.Run("accepts a PAI of the entry's vendor and product", func(t *testing.T) {
		t.Parallel()
		pai := dclCrlIssue(t, "Test PAI", dclCrlIssueOptions{issuer: &paa, ca: true, vid: u16(0xFFF1), pid: u16(0x8000)})
		dclCrlWant(t, dclCrlRevoked(t, pai.skid, []dclCrlPoint{{signer: pai.der, vid: u16(0xFFF1), pid: u16(0x8000), crl: by(t, pai)}}, dclCrlPAA(paa.der)), true)
	})
	t.Run("accepts a PAI with product ID 0 on an entry the DCL states without product", func(t *testing.T) {
		t.Parallel()
		pai := dclCrlIssue(t, "Test PAI", dclCrlIssueOptions{issuer: &paa, ca: true, vid: u16(0xFFF1), pid: u16(0)})
		dclCrlWant(t, dclCrlRevoked(t, pai.skid, []dclCrlPoint{{signer: pai.der, vid: u16(0xFFF1), crl: by(t, pai)}}, dclCrlPAA(paa.der)), true)
	})
	for _, tc := range []struct {
		description string
		pid         *uint16
	}{{"another product", u16(0x8001)}, {"no product", nil}} {
		t.Run("ignores a PAI with a product ID on an entry for "+tc.description, func(t *testing.T) {
			t.Parallel()
			pai := dclCrlIssue(t, "Test PAI", dclCrlIssueOptions{issuer: &paa, ca: true, vid: u16(0xFFF1), pid: u16(0x8000)})
			dclCrlWant(t, dclCrlRevoked(t, pai.skid, []dclCrlPoint{{signer: pai.der, vid: u16(0xFFF1), pid: tc.pid, crl: by(t, pai)}}, dclCrlPAA(paa.der)), false)
		})
	}
}

// TestDclCrlAuthenticationCRLChecks is describe("CRL checks") (:428-487).
func TestDclCrlAuthenticationCRLChecks(t *testing.T) {
	t.Parallel()
	paa := dclCrlIssue(t, "Test PAA", dclCrlIssueOptions{ca: true})
	other, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	otherSigner := paa.crlSigner()
	otherSigner.key = other
	zeroAKID := paa.crlSigner()
	zeroAKID.skid = make([]byte, 20)
	critical := func(oid string) [][]byte {
		return [][]byte{dclCrlExtension(oid, true, dclCrlTLV(derInteger, []byte{1}))}
	}
	for _, tc := range []struct {
		name   string
		signer dclCrlSigner
		o      dclCrlOptions
		want   bool
	}{
		{"ignores a CRL whose authority key identifier names another signer (step 7.1)", zeroAKID, dclCrlOptions{}, false},
		{
			"accepts a CRL whose issuer encodes the signer's name as another string type (RFC 5280 §7.1)", paa.crlSigner(),
			dclCrlOptions{issuerDN: dclCrlName("TEST PAA", dclCrlIDs{}, true)},
			true,
		},
		{
			"ignores a CRL whose issuer is not the signer (RFC 5280 §5.1.2.3)", paa.crlSigner(),
			dclCrlOptions{issuerDN: dclCrlName("Someone else", dclCrlIDs{}, false)},
			false,
		},
		{"ignores a CRL without a signature (step 8)", paa.crlSigner(), dclCrlOptions{withoutSignature: true}, false},
		{"ignores a CRL whose signature BIT STRING states unused bits (step 8)", paa.crlSigner(), dclCrlOptions{signaturePadding: 1}, false},
		{
			"ignores a CRL whose outer signature algorithm differs from the signed one (step 8)", paa.crlSigner(),
			dclCrlOptions{tbsSignatureAlgorithm: dclCrlMLDSA44},
			false,
		},
		{
			// ecdsa-with-SHA384 stated, while the key signs with SHA-256 (:461).
			"ignores a CRL whose signature algorithm is neither ecdsa-with-SHA256 nor ML-DSA (step 8)", paa.crlSigner(),
			dclCrlOptions{signatureAlgorithm: dclCrlECDSAWithSHA384},
			false,
		},
		{"ignores a CRL signed with a key other than the signer's (step 8)", otherSigner, dclCrlOptions{}, false},
		{
			"ignores a CRL with an unsupported critical extension such as a delta CRL indicator", paa.crlSigner(),
			dclCrlOptions{extensions: critical("551d1b")},
			false,
		},
		{
			"ignores a CRL whose entry carries an unsupported critical extension", paa.crlSigner(),
			dclCrlOptions{entryExtensions: critical("2a0304")},
			false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			crl := dclCrlBy(t, tc.signer, tc.o)
			dclCrlWant(t, dclCrlRevoked(t, paa.skid, []dclCrlPoint{{signer: paa.der, isPAA: true, crl: crl}}, dclCrlPAA(paa.der)), tc.want)
		})
	}
}

// dclCrlCertificateIssuer is certificateIssuer() (:510-519): a
// certificateIssuer entry extension naming commonName names times.
func dclCrlCertificateIssuer(commonName string, critical bool, names int) []byte {
	gn := make([]byte, 0, 64*names)
	for range names {
		gn = append(gn, dclCrlTLV(derDirectoryName, dclCrlName(commonName, dclCrlIDs{}, false))...)
	}
	return dclCrlExtension(oidCertificateIssuerEntry, critical, dclCrlSeq(gn))
}

// TestDclCrlAuthenticationAuthorityName is describe("authority name (steps
// 9 and 10)") (:489-574).
func TestDclCrlAuthenticationAuthorityName(t *testing.T) {
	t.Parallel()
	paa := dclCrlIssue(t, "Test PAA", dclCrlIssueOptions{ca: true})
	serve := func(t *testing.T, crl []byte) *CertificateStore {
		t.Helper()
		return dclCrlServe(t, newFakeDCL(t), paa.skid, []dclCrlPoint{{signer: paa.der, isPAA: true, crl: crl}}, dclCrlPAA(paa.der))
	}

	t.Run("files the revocations of an indirect CRL under the delegating PAI's name", func(t *testing.T) {
		t.Parallel()
		pai := dclCrlIssue(t, "Test PAI", dclCrlIssueOptions{issuer: &paa, ca: true, vid: u16(0xFFF1)})
		delegate := dclCrlIssue(t, "PAI CRL signer", dclCrlIssueOptions{issuer: &pai})
		crl := dclCrlBy(t, delegate.crlSigner(), dclCrlOptions{issuerDN: dclCrlName(delegate.name, dclCrlIDs{}, false)})
		dcl := dclCrlServe(t, newFakeDCL(t), pai.skid, []dclCrlPoint{{signer: delegate.der, delegator: pai.der, crl: crl}}, dclCrlPAA(paa.der))
		dclCrlWant(t, dclCrlIsRevoked(t, dcl, pai.skid, dclCrlRevokedSerial, dclCrlName(pai.name, pai.ids, false)), true)
		dclCrlWant(t, dclCrlIsRevoked(t, dcl, pai.skid, dclCrlRevokedSerial, dclCrlName(delegate.name, dclCrlIDs{}, false)), false)
	})
	t.Run("finds revocations for an issuer name the certificate encodes as another string type", func(t *testing.T) {
		t.Parallel()
		dcl := serve(t, dclCrlBy(t, paa.crlSigner(), dclCrlOptions{}))
		dclCrlWant(t, dclCrlIsRevoked(t, dcl, paa.skid, dclCrlRevokedSerial, dclCrlName(paa.name, dclCrlIDs{}, true)), true)
	})
	for _, tc := range []struct {
		description string
		critical    bool
		names       int
	}{{"is not critical", false, 1}, {"names more than one issuer", true, 2}} {
		t.Run("ignores a CRL whose certificateIssuer "+tc.description+" (RFC 5280 §5.3.3)", func(t *testing.T) {
			t.Parallel()
			// Naming the authority itself, so only the malformed shape can
			// refuse the CRL (:526).
			crl := dclCrlBy(t, paa.crlSigner(), dclCrlOptions{entryExtensions: [][]byte{dclCrlCertificateIssuer(paa.name, tc.critical, tc.names)}})
			dclCrlWant(t, dclCrlRevoked(t, paa.skid, []dclCrlPoint{{signer: paa.der, isPAA: true, crl: crl}}, dclCrlPAA(paa.der)), false)
		})
	}
	t.Run("ignores entries whose certificateIssuer names another authority (step 10.1)", func(t *testing.T) {
		t.Parallel()
		crl := dclCrlBy(t, paa.crlSigner(), dclCrlOptions{perEntry: [][][]byte{{dclCrlCertificateIssuer("Someone else", true, 1)}, nil}},
			dclCrlOtherRevoked, dclCrlRevokedSerial)
		// The first entry's issuer carries over to the second (:540).
		dcl := serve(t, crl)
		dclCrlWant(t, dclCrlIsRevoked(t, dcl, paa.skid, dclCrlOtherRevoked, nil), false)
		dclCrlWant(t, dclCrlIsRevoked(t, dcl, paa.skid, dclCrlRevokedSerial, nil), false)
	})
	t.Run("keeps entries whose certificateIssuer encodes the authority's name as another string type", func(t *testing.T) {
		t.Parallel()
		printable := dclCrlExtension(oidCertificateIssuerEntry, true,
			dclCrlSeq(dclCrlTLV(derDirectoryName, dclCrlName(paa.name, dclCrlIDs{}, true))))
		crl := dclCrlBy(t, paa.crlSigner(), dclCrlOptions{entryExtensions: [][]byte{printable}})
		dclCrlWant(t, dclCrlRevoked(t, paa.skid, []dclCrlPoint{{signer: paa.der, isPAA: true, crl: crl}}, dclCrlPAA(paa.der)), true)
	})
	t.Run("keeps entries once certificateIssuer names the authority again (step 10.1)", func(t *testing.T) {
		t.Parallel()
		crl := dclCrlBy(t, paa.crlSigner(), dclCrlOptions{perEntry: [][][]byte{
			{dclCrlCertificateIssuer("Someone else", true, 1)},
			{dclCrlCertificateIssuer(paa.name, true, 1)},
		}}, dclCrlOtherRevoked, dclCrlRevokedSerial)
		dcl := serve(t, crl)
		dclCrlWant(t, dclCrlIsRevoked(t, dcl, paa.skid, dclCrlOtherRevoked, nil), false)
		dclCrlWant(t, dclCrlIsRevoked(t, dcl, paa.skid, dclCrlRevokedSerial, nil), true)
	})
}

// TestDclCrlAuthenticationForeignEntry is it("ignores an entry for another
// issuer in the response (§11.23.11.7)") (:576-586).
func TestDclCrlAuthenticationForeignEntry(t *testing.T) {
	t.Parallel()
	paa := dclCrlIssue(t, "Test PAA", dclCrlIssueOptions{ca: true})
	// A genuine CRL of the other issuer, returned for the wrong lookup.
	other := dclCrlIssue(t, "Other PAA", dclCrlIssueOptions{ca: true})
	dcl := dclCrlServe(t, newFakeDCL(t), paa.skid,
		[]dclCrlPoint{{signer: other.der, isPAA: true, issuerSKID: other.skid, crl: dclCrlBy(t, other.crlSigner(), dclCrlOptions{})}},
		dclCrlPAA(paa.der), dclCrlPAA(other.der))
	dclCrlWant(t, dclCrlIsRevoked(t, dcl, paa.skid, dclCrlRevokedSerial, nil), false)
}

// TestDclCrlAuthenticationPartitions is describe("partitioned revocation
// lists (step 7.2)") (:588-631).
func TestDclCrlAuthenticationPartitions(t *testing.T) {
	t.Parallel()
	paa := dclCrlIssue(t, "Test PAA", dclCrlIssueOptions{ca: true})
	const a, b = "/a.crl", "/b.crl"
	partition := func(t *testing.T, path string, o dclCrlOptions, serial string) dclCrlPoint {
		t.Helper()
		return dclCrlPoint{signer: paa.der, isPAA: true, path: path, crl: dclCrlBy(t, paa.crlSigner(), o, serial)}
	}
	both := func(t *testing.T, f *fakeDCL, pa, pb dclCrlPoint) (revoked, otherRevoked bool) {
		t.Helper()
		dcl := dclCrlServe(t, f, paa.skid, []dclCrlPoint{pa, pb}, dclCrlPAA(paa.der))
		return dclCrlIsRevoked(t, dcl, paa.skid, dclCrlRevokedSerial, nil), dclCrlIsRevoked(t, dcl, paa.skid, dclCrlOtherRevoked, nil)
	}

	t.Run("combines the revocations of every partition", func(t *testing.T) {
		t.Parallel()
		f := newFakeDCL(t)
		r, o := both(t, f,
			partition(t, a, dclCrlOptions{distributionPoints: []string{f.url(a)}}, dclCrlRevokedSerial),
			partition(t, b, dclCrlOptions{distributionPoints: []string{f.url(b)}}, dclCrlOtherRevoked))
		dclCrlWant(t, r, true)
		dclCrlWant(t, o, true)
	})
	for _, tc := range []struct {
		description string
		options     func(f *fakeDCL) dclCrlOptions
	}{
		{"does not name its URL", func(f *fakeDCL) dclCrlOptions {
			return dclCrlOptions{distributionPoints: []string{f.url("/elsewhere.crl")}}
		}},
		{"is not critical", func(f *fakeDCL) dclCrlOptions {
			return dclCrlOptions{distributionPoints: []string{f.url(b)}, distributionPointWeak: true}
		}},
		{"names more than its URL", func(f *fakeDCL) dclCrlOptions {
			return dclCrlOptions{distributionPoints: []string{f.url(b), f.url("/c.crl")}}
		}},
		{"is missing", func(*fakeDCL) dclCrlOptions { return dclCrlOptions{} }},
	} {
		t.Run("ignores a partition whose Issuing Distribution Point "+tc.description, func(t *testing.T) {
			t.Parallel()
			f := newFakeDCL(t)
			r, o := both(t, f,
				partition(t, a, dclCrlOptions{distributionPoints: []string{f.url(a)}}, dclCrlRevokedSerial),
				partition(t, b, tc.options(f), dclCrlOtherRevoked))
			dclCrlWant(t, o, false)
			dclCrlWant(t, r, true)
		})
	}
	t.Run("requires no Issuing Distribution Point from entries of different vendors", func(t *testing.T) {
		t.Parallel()
		pa := partition(t, a, dclCrlOptions{}, dclCrlRevokedSerial)
		pa.vid = u16(0xFFF1)
		pb := partition(t, b, dclCrlOptions{}, dclCrlOtherRevoked)
		pb.vid = u16(0xFFF2)
		r, o := both(t, newFakeDCL(t), pa, pb)
		dclCrlWant(t, r, true)
		dclCrlWant(t, o, true)
	})
}

// TestDclCrlAuthenticationRetrying is describe("retrying") (:633-701).
func TestDclCrlAuthenticationRetrying(t *testing.T) {
	t.Parallel()
	paa := dclCrlIssue(t, "Test PAA", dclCrlIssueOptions{ca: true})
	status := func(f *fakeDCL, path string, code int, body string) {
		f.route(path, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(code)
			_, _ = w.Write([]byte(body))
		})
	}
	// crlDownloads (:634-635).
	downloads := func(f *fakeDCL, paths ...string) int {
		n := 0
		for _, p := range paths {
			n += f.hit(p)
		}
		return n
	}
	twice := func(t *testing.T, dcl *CertificateStore, want bool) {
		t.Helper()
		dclCrlWant(t, dclCrlIsRevoked(t, dcl, paa.skid, dclCrlRevokedSerial, nil), want)
		dclCrlWant(t, dclCrlIsRevoked(t, dcl, paa.skid, dclCrlRevokedSerial, nil), want)
	}
	single := func(t *testing.T, f *fakeDCL, p dclCrlPoint) *CertificateStore {
		t.Helper()
		return dclCrlServe(t, f, paa.skid, []dclCrlPoint{p}, dclCrlPAA(paa.der))
	}
	want := func(t *testing.T, got, n int) {
		t.Helper()
		if got != n {
			t.Fatalf("%d CRL downloads, want %d", got, n)
		}
	}

	t.Run("does not download a rejected CRL again", func(t *testing.T) {
		t.Parallel()
		f := newFakeDCL(t)
		dcl := single(t, f, dclCrlPoint{signer: paa.der, isPAA: true, crl: dclCrlBy(t, paa.crlSigner(), dclCrlOptions{withoutSignature: true})})
		twice(t, dcl, false)
		want(t, downloads(f, "/0.crl"), 1)
	})
	t.Run("uses the partitions that did load, and loads the others again next time", func(t *testing.T) {
		t.Parallel()
		f := newFakeDCL(t)
		const a, b = "/a.crl", "/b.crl"
		dcl := dclCrlServe(t, f, paa.skid, []dclCrlPoint{
			{signer: paa.der, isPAA: true, path: a, crl: dclCrlBy(t, paa.crlSigner(), dclCrlOptions{distributionPoints: []string{f.url(a)}})},
			{signer: paa.der, isPAA: true, path: b, crl: dclCrlBy(t, paa.crlSigner(), dclCrlOptions{distributionPoints: []string{f.url(b)}}, dclCrlOtherRevoked)},
		}, dclCrlPAA(paa.der))
		status(f, b, http.StatusServiceUnavailable, "unavailable")
		twice(t, dcl, true)
		want(t, downloads(f, a, b), 4)
	})
	for _, tc := range []struct {
		description string
		notFound    bool
	}{{"a CRL the server does not have", true}, {"a CRL that does not match the DCL's digest", false}} {
		t.Run("does not download "+tc.description+" again", func(t *testing.T) {
			t.Parallel()
			f := newFakeDCL(t)
			p := dclCrlPoint{signer: paa.der, isPAA: true, crl: dclCrlBy(t, paa.crlSigner(), dclCrlOptions{})}
			if !tc.notFound {
				p.dataDigest = "AAAA"
			}
			dcl := single(t, f, p)
			if tc.notFound {
				status(f, "/0.crl", http.StatusNotFound, "gone")
			}
			twice(t, dcl, false)
			want(t, downloads(f, "/0.crl"), 1)
		})
	}
	for _, code := range []int{http.StatusRequestTimeout, http.StatusTooManyRequests} {
		t.Run(fmt.Sprintf("downloads again after HTTP %d", code), func(t *testing.T) {
			t.Parallel()
			f := newFakeDCL(t)
			dcl := single(t, f, dclCrlPoint{signer: paa.der, isPAA: true, crl: dclCrlBy(t, paa.crlSigner(), dclCrlOptions{})})
			status(f, "/0.crl", code, "later")
			twice(t, dcl, false)
			want(t, downloads(f, "/0.crl"), 2)
		})
	}
	t.Run("downloads again after a transient failure", func(t *testing.T) {
		t.Parallel()
		f := newFakeDCL(t)
		dcl := single(t, f, dclCrlPoint{signer: paa.der, isPAA: true, crl: dclCrlBy(t, paa.crlSigner(), dclCrlOptions{})})
		status(f, "/0.crl", http.StatusServiceUnavailable, "unavailable")
		twice(t, dcl, false)
		want(t, downloads(f, "/0.crl"), 2)
	})
}

// dclCrlDelegatedMLDSASigner is delegatedMlDsaCrlSigner() (:704-730): an
// ML-DSA-44 CRL signer the EC PAA delegated, and a CRL it signed. crypto/
// x509 marshals no ML-DSA key, so the certificate is built by hand.
func dclCrlDelegatedMLDSASigner(t *testing.T, paa dclCrlIssued) (der, crl []byte) {
	t.Helper()
	key, err := mldsa.GenerateKey(mldsa.MLDSA44())
	if err != nil {
		t.Fatal(err)
	}
	pub := key.PublicKey().Bytes()
	sum := sha1.Sum(pub) //nolint:gosec // see the import
	skid := sum[:]
	const name = "PQC Delegated CRL Signer"
	utc := func(s string) []byte { return dclCrlTLV(derUTCTime, []byte(s)) }
	ext := dclCrlExtension
	tbs := dclCrlSeq(
		dclCrlTLV(derContext0, dclCrlTLV(derInteger, []byte{2})),
		dclCrlTLV(derInteger, []byte{3}),
		dclCrlECDSAWithSHA256,
		dclCrlName(paa.name, paa.ids, false),
		dclCrlSeq(utc("261013120000Z"), utc("261015120000Z")),
		dclCrlName(name, dclCrlIDs{}, false),
		// MlDsa.SubjectPublicKeyInfo("ML-DSA-44", key).
		dclCrlSeq(dclCrlMLDSA44, dclCrlTLV(derBitString, append([]byte{0}, pub...))),
		dclCrlTLV(0xa3, dclCrlSeq(
			ext("551d13", true, dclCrlSeq()),                           // basicConstraints { isCa: false }
			ext("551d0f", true, dclCrlTLV(derBitString, []byte{1, 2})), // keyUsage { cRLSign }
			ext("551d0e", false, dclCrlTLV(derOctetString, skid)),
			ext(oidAuthorityKeyID, false, dclCrlSeq(dclCrlTLV(derKeyIdentifier, paa.skid))),
		)),
	)
	digest := sha256.Sum256(tbs)
	sig, err := ecdsa.SignASN1(rand.Reader, paa.key, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	der = dclCrlSeq(tbs, dclCrlECDSAWithSHA256, dclCrlTLV(derBitString, append([]byte{0}, sig...)))
	crl = dclCrlBy(t, dclCrlSigner{key: key, skid: skid, subjectDER: dclCrlName(name, dclCrlIDs{}, false)}, dclCrlOptions{})
	return der, crl
}

// TestDclCrlAuthenticationMLDSAForwardFeaturesOff is describe("ML-DSA while
// forward Matter features are off") (:798-810): the port admits no ML-DSA
// (PQC Phase 1), as matter.js with its forward features off. Both refuse
// the signer at step 2: matter.js's Paa.fromAsn1 raises
// MlDsaAttestationDisabledError (AttestationCertificates.ts:30-38), the
// port's parseCert its 600-byte limit, which no ML-DSA certificate fits.
func TestDclCrlAuthenticationMLDSAForwardFeaturesOff(t *testing.T) {
	t.Parallel()
	paa := dclCrlIssue(t, "Test PAA", dclCrlIssueOptions{ca: true})
	t.Run("ignores a CRL from an ML-DSA signer the approved PAA delegated", func(t *testing.T) {
		t.Parallel()
		der, crl := dclCrlDelegatedMLDSASigner(t, paa)
		dclCrlWant(t, dclCrlRevoked(t, paa.skid, []dclCrlPoint{{signer: der, isPAA: true, crl: crl}}, dclCrlPAA(paa.der)), false)
	})
}
