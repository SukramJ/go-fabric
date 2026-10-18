// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package snapshot

// A port of matter.js packages/protocol/test/dcl/DclCertificateServiceSeedTest.ts at
// 862ad75f: the trust set seeded from a snapshot. The Go seed is a
// [Snapshot], a PAA store of its own that holds the production set alone
// (fetchTestCertificates off) and is offline by construction.

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/SukramJ/go-fabric/controller/dcl"
	secattestation "github.com/SukramJ/go-fabric/secure/attestation"
)

// DclCertificateServiceSeedTest.ts:12-13.
const (
	dclCertSvcSeedNoVID = "785CE705B86B8F4E6FC793AA60CB43EA696882D5"
	dclCertSvcSeedFFF1  = "6AFD22771F511FECBF1641976710DCDC31A1717E"
)

// dclCertSvcSeedEntry is makePaaEntry (:17-24) for dir: the key identifier
// lowercase, kind production.
func dclCertSvcSeedEntry(dir string, der []byte, skid string) (e Entry, file string) {
	sum := sha256.Sum256(der)
	file = dir + "/" + skid + ".der"
	return Entry{File: file, SHA256: hex.EncodeToString(sum[:]), SubjectKeyID: strings.ToLower(skid), Kind: KindProduction}, file
}

// dclCertSvcSeed is makeSeedSource (:41-43) of PAA and CD signer entries
// with their files.
type dclCertSvcSeed struct {
	builtAt   time.Time
	paa, cd   []Entry
	files     map[string][]byte
	paaSource *Section
}

func newDclCertSvcSeed() *dclCertSvcSeed {
	return &dclCertSvcSeed{builtAt: time.Date(2026, 5, 13, 2, 0, 0, 0, time.UTC), files: map[string][]byte{}}
}

func (s *dclCertSvcSeed) addPAA(der []byte, skid string, edit func(*Entry)) {
	e, file := dclCertSvcSeedEntry(PAADir, der, skid)
	if edit != nil {
		edit(&e)
	}
	s.paa = append(s.paa, e)
	s.files[file] = der
}

func (s *dclCertSvcSeed) addCD(der []byte, skid string) {
	e, file := dclCertSvcSeedEntry(CDSignerDir, der, skid)
	s.cd = append(s.cd, e)
	s.files[file] = der
}

func (s *dclCertSvcSeed) load(t *testing.T) (*Snapshot, error) {
	t.Helper()
	paa := section(s.paa...)
	if s.paaSource != nil {
		paa = *s.paaSource
	}
	src := Source{Ledger: dcl.ProductionURL, BuiltAt: s.builtAt, PAA: paa, CDSigners: section(s.cd...)}
	return LoadAt(mapFS(t, src, s.files), testNow)
}

func (s *dclCertSvcSeed) mustLoad(t *testing.T) *Snapshot {
	t.Helper()
	snap, err := s.load(t)
	if err != nil {
		t.Fatal(err)
	}
	return snap
}

func dclCertSvcSeedID(t *testing.T, skid string) []byte {
	t.Helper()
	b, err := hex.DecodeString(skid)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestDclCertificateServiceSeed ports describe("DclCertificateService seed").
func TestDclCertificateServiceSeed(t *testing.T) {
	t.Parallel()
	noVID, fff1 := dclCertSvcSeedID(t, dclCertSvcSeedNoVID), dclCertSvcSeedID(t, dclCertSvcSeedFFF1)
	testKind := func(e *Entry) { e.Kind = KindTest }

	t.Run("inserts paaRoots from seed into storage", func(t *testing.T) {
		t.Parallel()
		seed := newDclCertSvcSeed()
		seed.addPAA(secattestation.TestPAANoVIDCert, dclCertSvcSeedNoVID, nil)
		seed.addPAA(secattestation.TestPAAFFF1Cert, dclCertSvcSeedFFF1, nil)
		snap := seed.mustLoad(t)
		for _, id := range [][]byte{noVID, fff1} {
			if p, ok := snap.PAA(id); !ok || !p.Production {
				t.Fatalf("%X: %+v %v", id, p, ok)
			}
		}
	})
	t.Run("sets fetchedAt to snapshot builtAt for seeded certs", func(t *testing.T) {
		t.Parallel()
		seed := newDclCertSvcSeed()
		seed.addPAA(secattestation.TestPAANoVIDCert, dclCertSvcSeedNoVID, nil)
		if p, _ := seed.mustLoad(t).PAA(noVID); !p.FetchedAt.Equal(time.Date(2026, 5, 13, 2, 0, 0, 0, time.UTC)) {
			t.Fatalf("fetchedAt %v", p.FetchedAt)
		}
	})
	t.Run("kind=production maps to isProduction=true", func(t *testing.T) {
		t.Parallel()
		seed := newDclCertSvcSeed()
		seed.addPAA(secattestation.TestPAANoVIDCert, dclCertSvcSeedNoVID, nil)
		if p, ok := seed.mustLoad(t).PAA(noVID); !ok || !p.Production {
			t.Fatalf("%+v %v", p, ok)
		}
	})
	t.Run("skips test-kind entries when fetchTestCertificates is false", func(t *testing.T) {
		t.Parallel()
		seed := newDclCertSvcSeed()
		seed.addPAA(secattestation.TestPAAFFF1Cert, dclCertSvcSeedFFF1, testKind)
		if _, ok := seed.mustLoad(t).PAA(fff1); ok {
			t.Fatal("a test entry was seeded")
		}
	})
	t.Run("skips expired certs (notAfter in the past)", func(t *testing.T) {
		t.Parallel()
		seed := newDclCertSvcSeed()
		seed.addPAA(secattestation.TestPAANoVIDCert, dclCertSvcSeedNoVID, func(e *Entry) { e.NotAfter = "2000-01-01T00:00:00Z" })
		if _, ok := seed.mustLoad(t).PAA(noVID); ok {
			t.Fatal("an expired entry was seeded")
		}
	})
	t.Run("warns and skips entry where subjectKeyId does not match DER-derived SKID", func(t *testing.T) {
		t.Parallel()
		seed := newDclCertSvcSeed()
		seed.addPAA(secattestation.TestPAAFFF1Cert, dclCertSvcSeedFFF1, func(e *Entry) { e.SubjectKeyID = strings.ToLower(dclCertSvcSeedNoVID) })
		snap := seed.mustLoad(t)
		if _, ok := snap.PAA(noVID); ok {
			t.Fatal("seeded under the stated key identifier")
		}
		if _, ok := snap.PAA(fff1); ok {
			t.Fatal("seeded under the DER's key identifier")
		}
		if snap.Skipped() != 1 {
			t.Fatalf("%d skipped", snap.Skipped())
		}
	})
	// The CD signer entry is the NoVID PAA's DER, as matter.js's (:214-219).
	t.Run("aborts paaRoots stream on malformed entry; cdSigners stream unaffected", func(t *testing.T) {
		t.Parallel()
		seed := newDclCertSvcSeed()
		seed.paaSource = &Section{ExpectedCount: 1, Entries: []Entry{{}}}
		seed.addCD(secattestation.TestPAANoVIDCert, dclCertSvcSeedNoVID)
		snap, err := seed.load(t)
		if !errors.Is(err, ErrMalformed) {
			t.Fatalf("err %v", err)
		}
		if _, ok := snap.CDSigner(noVID); !ok {
			t.Fatal("the CD signer stream was not consumed")
		}
		if _, ok := snap.PAA(noVID); ok {
			t.Fatal("the CD signer was seeded as a PAA")
		}
		if _, ok := snap.PAA(fff1); ok {
			t.Fatal("a PAA from the aborted stream")
		}
	})
	// A snapshot is not stored: seeding twice is loading the set twice, and
	// a set naming the entry twice.
	t.Run("is idempotent: seeding twice produces same single cert", func(t *testing.T) {
		t.Parallel()
		seed := newDclCertSvcSeed()
		seed.addPAA(secattestation.TestPAANoVIDCert, dclCertSvcSeedNoVID, nil)
		seed.mustLoad(t)
		if paas, _ := seed.mustLoad(t).Len(); paas != 1 {
			t.Fatalf("%d PAAs", paas)
		}
		seed.addPAA(secattestation.TestPAANoVIDCert, dclCertSvcSeedNoVID, nil)
		snap := seed.mustLoad(t)
		if _, ok := snap.PAA(noVID); !ok {
			t.Fatal("not seeded")
		}
		if paas, cds := snap.Len(); paas != 1 || cds != 0 {
			t.Fatalf("%d PAAs %d CD signers", paas, cds)
		}
	})
	t.Run("no-seed: existing behavior unchanged", func(t *testing.T) {
		t.Parallel()
		snap, err := Load(fstest.MapFS{})
		if err != nil {
			t.Fatal(err)
		}
		if paas, cds := snap.Len(); paas != 0 || cds != 0 {
			t.Fatalf("%d PAAs %d CD signers", paas, cds)
		}
	})
	// A Snapshot has no ledger client: it fetches nothing.
	t.Run("offline: getOrFetchCertificate serves the seed and fetches nothing it lacks", func(t *testing.T) {
		t.Parallel()
		seed := newDclCertSvcSeed()
		seed.addPAA(secattestation.TestPAANoVIDCert, dclCertSvcSeedNoVID, nil)
		snap := seed.mustLoad(t)
		if _, ok := snap.PAA(noVID); !ok {
			t.Fatal("the seed is not served")
		}
		if _, ok := snap.PAA(fff1); ok {
			t.Fatal("a certificate the seed lacks")
		}
	})
	// matter.js holds the test entry and filters it; the Go snapshot skips
	// it on load (Skipped), with the same answer and nothing fetched.
	t.Run("offline: getOrFetchCertificate fetches nothing for a held certificate the trust policy filters out", func(t *testing.T) {
		t.Parallel()
		seed := newDclCertSvcSeed()
		seed.addPAA(secattestation.TestPAANoVIDCert, dclCertSvcSeedNoVID, testKind)
		snap := seed.mustLoad(t)
		if snap.AcceptsTestCertificates() {
			t.Fatal("accepts test certificates")
		}
		if _, ok := snap.PAA(noVID); ok {
			t.Fatal("the filtered certificate is served")
		}
		if snap.Skipped() != 1 {
			t.Fatalf("%d skipped", snap.Skipped())
		}
	})
}
