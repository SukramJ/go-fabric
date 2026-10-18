// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package snapshot

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // key identifiers are SHA-1 of the key
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/SukramJ/go-fabric/controller/attestation"
	secattestation "github.com/SukramJ/go-fabric/secure/attestation"
)

var testNow = time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)

// cdSignerDER mints a CD signer certificate under the test PAA FFF1.
func cdSignerDER(t *testing.T) (der []byte, skid string, key *ecdsa.PrivateKey) {
	t.Helper()
	paa, err := x509.ParseCertificate(secattestation.TestPAAFFF1Cert)
	if err != nil {
		t.Fatal(err)
	}
	key, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pub, _ := key.PublicKey.Bytes()
	id := sha1.Sum(pub) //nolint:gosec // see the import
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(7), Subject: pkix.Name{CommonName: "Certification Declaration Signing Key 900"},
		NotBefore: testNow.Add(-time.Hour), NotAfter: testNow.Add(24 * time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, BasicConstraintsValid: true, SubjectKeyId: id[:],
	}
	der, err = x509.CreateCertificate(rand.Reader, tmpl, paa, &key.PublicKey, secattestation.TestPAAFFF1PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	return der, strings.ToUpper(hex.EncodeToString(id[:])), key
}

func entry(dir, skid string, der []byte, kind string) Entry {
	sum := sha256.Sum256(der)
	return Entry{File: dir + "/" + skid + ".der", SHA256: hex.EncodeToString(sum[:]), SubjectKeyID: skid, Kind: kind}
}

func mapFS(t *testing.T, src Source, files map[string][]byte) fstest.MapFS {
	t.Helper()
	b, err := json.Marshal(src)
	if err != nil {
		t.Fatal(err)
	}
	m := fstest.MapFS{SourceFile: {Data: b}}
	for name, data := range files {
		m[name] = &fstest.MapFile{Data: data}
	}
	return m
}

func section(entries ...Entry) Section { return Section{ExpectedCount: len(entries), Entries: entries} }

// TestEmbedded: the committed snapshot loads without a malformed entry,
// names the production ledger and holds the official CD signers.
func TestEmbedded(t *testing.T) {
	s, err := Embedded()
	if err != nil {
		t.Fatal(err)
	}
	paas, cds := s.Len()
	src := s.Source()
	if src.Ledger == "" || src.BuiltAt.IsZero() || paas != src.PAA.ExpectedCount || paas == 0 {
		t.Fatalf("source %s at %s: %d PAAs of %d", src.Ledger, src.BuiltAt, paas, src.PAA.ExpectedCount)
	}
	if cds != len(CDSignerKeyIDs) {
		t.Fatalf("%d CD signers, want %d", cds, len(CDSignerKeyIDs))
	}
	for _, id := range CDSignerKeyIDs {
		skid, _ := hex.DecodeString(id)
		if _, ok := s.CDSigner(skid); !ok {
			t.Errorf("CD signer %s missing", id)
		}
	}
	for _, e := range src.PAA.Entries {
		skid, _ := hex.DecodeString(e.SubjectKeyID)
		if p, ok := s.PAA(skid); !ok || !p.Production || !p.FetchedAt.Equal(src.BuiltAt) {
			t.Errorf("PAA %s: %+v %v", e.SubjectKeyID, p.Production, ok)
		}
	}
}

// TestLoad: the per-entry rules of #consumeCertSeed.
func TestLoad(t *testing.T) {
	paaSKID := strings.ToUpper(hex.EncodeToString(secattestation.TestPAAFFF1SKID))
	cdDER, cdSKID, cdKey := cdSignerDER(t)
	good := entry(PAADir, paaSKID, secattestation.TestPAAFFF1Cert, KindProduction)
	cd := entry(CDSignerDir, cdSKID, cdDER, KindProduction)
	files := map[string][]byte{good.File: secattestation.TestPAAFFF1Cert, cd.File: cdDER}

	s, err := LoadAt(mapFS(t, Source{Ledger: "l", BuiltAt: testNow, PAA: section(good), CDSigners: section(cd)}, files), testNow)
	if err != nil {
		t.Fatal(err)
	}
	if p, ok := s.PAA(secattestation.TestPAAFFF1SKID); !ok || !p.Production || !p.FetchedAt.Equal(testNow) {
		t.Fatalf("PAA %+v %v", p, ok)
	}
	skid, _ := hex.DecodeString(cdSKID)
	if k, ok := s.CDSigner(skid); !ok || !k.Equal(&cdKey.PublicKey) {
		t.Fatal("CD signer missing")
	}
	if s.AcceptsTestCertificates() {
		t.Fatal("a snapshot accepts test certificates")
	}

	// No SOURCE: no snapshot, no error.
	if s, err := load(fstest.MapFS{}, testNow); err != nil || s.Source().Ledger != "" {
		t.Fatalf("empty: %v", err)
	}

	// Skipped: a test entry, an expired one, a SKID that is not the DER's.
	test := entry(PAADir, paaSKID, secattestation.TestPAAFFF1Cert, KindTest)
	expired := good
	expired.NotAfter = testNow.Add(-time.Hour).Format(time.RFC3339)
	wrongSKID := entry(PAADir, strings.Repeat("AB", 20), secattestation.TestPAAFFF1Cert, KindProduction)
	f2 := map[string][]byte{wrongSKID.File: secattestation.TestPAAFFF1Cert, good.File: secattestation.TestPAAFFF1Cert}
	s, err = load(mapFS(t, Source{PAA: section(test, expired, wrongSKID)}, f2), testNow)
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := s.Len(); n != 0 || s.Skipped() != 3 {
		t.Fatalf("%d PAAs loaded, %d skipped", n, s.Skipped())
	}

	// Malformed: the load of the kind stops; what came before is kept.
	bad := good
	bad.SHA256 = strings.Repeat("0", 64)
	other := entry(PAADir, strings.Repeat("CD", 20), cdDER, KindProduction)
	s, err = load(mapFS(t, Source{PAA: section(good, bad), CDSigners: section(cd)}, files), testNow)
	if !errors.Is(err, ErrMalformed) {
		t.Fatalf("SHA-256 mismatch: %v", err)
	}
	if n, c := s.Len(); n != 1 || c != 1 {
		t.Fatalf("after the malformed entry: %d PAAs, %d CD signers", n, c)
	}
	for name, sec := range map[string]Section{
		"missing file":   section(other),
		"bad shape":      section(Entry{File: "elsewhere/x.der", SubjectKeyID: paaSKID, Kind: KindProduction}),
		"bad kind":       section(Entry{File: good.File, SHA256: good.SHA256, SubjectKeyID: paaSKID, Kind: "dev"}),
		"count mismatch": {ExpectedCount: 2, Entries: []Entry{good}},
	} {
		if _, err := load(mapFS(t, Source{PAA: sec}, files), testNow); !errors.Is(err, ErrMalformed) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := load(fstest.MapFS{SourceFile: {Data: []byte("{")}}, testNow); err == nil {
		t.Fatal("an unreadable SOURCE loaded")
	}
}

// TestSnapshotIsNoRevocationSource: the snapshot never answers a
// revocation question, so the online store keeps the CRL path (Matter PKI
// Certificate Policy §1.4.5, §4.9.6).
func TestSnapshotIsNoRevocationSource(t *testing.T) {
	var store attestation.PAAStore = &Snapshot{}
	if _, ok := store.(attestation.Revocations); ok {
		t.Fatal("the snapshot implements attestation.Revocations")
	}
}
