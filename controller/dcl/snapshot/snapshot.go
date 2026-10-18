// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package snapshot is the controller's offline DCL trust set: the
// production PAAs and Certification Declaration signers of the CSA
// Distributed Compliance Ledger as DER files with a SOURCE manifest,
// embedded from data/ and refreshed by `make update-dcl-snapshot` — a
// review step like the schema refresh, never automatic. It is the seed of
// matter.js's DclCertificateService (options.seed.paaRoots and
// options.seed.cdSigners, consumed by #consumeCertSeed,
// packages/protocol/src/dcl/DclCertificateService.ts:336-340, :1352-1422
// at 0ceb202d; the manifest's fields are SeedTypes.ts SeedSource and
// CertSeedEntry), as a [attestation.PAAStore] of its own that
// attestation.Trust layers with the online dcl.CertificateStore.
//
// The snapshot holds trust anchors only. Revocation stays online (a relying
// party's duty under the Matter PKI Certificate Policy, §1.4.5, §4.9.6):
// a [Snapshot] is no attestation.Revocations, so it never answers a CRL
// question; the online store does.
package snapshot

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/controller/dcl"
)

// SourceFile is the manifest's name in the snapshot directory.
const SourceFile = "SOURCE"

// The two directories of the snapshot.
const (
	PAADir      = "paa"
	CDSignerDir = "cd-signers"
)

// Entry kinds — CertSeedEntry.kind.
const (
	KindProduction = "production"
	KindTest       = "test"
)

//go:embed data
var embedded embed.FS

// Source is the SOURCE manifest — matter.js SeedSource (SeedTypes.ts:7-11)
// for each of the two kinds, with where and when the set was fetched.
type Source struct {
	// Ledger is the ledger's base URL.
	Ledger string `json:"ledger"`
	// BuiltAt is when the set was fetched (UTC) — builtAt, the FetchedAt
	// of every certificate of the snapshot.
	BuiltAt time.Time `json:"builtAt"`
	// PAA and CDSigners are the two sets.
	PAA       Section `json:"paa"`
	CDSigners Section `json:"cdSigners"`
}

// Section is one kind's entries — SeedSource's expectedCount and entries.
type Section struct {
	ExpectedCount int     `json:"expectedCount"`
	Entries       []Entry `json:"entries"`
}

// Entry is one certificate — matter.js CertSeedEntry (SeedTypes.ts:13-19)
// with the DER in a file of the snapshot (derHex there) and its SHA-256.
type Entry struct {
	// File is the DER file's path in the snapshot ("paa/<SKID>.der").
	File string `json:"file"`
	// SHA256 is the file's SHA-256, lowercase hex.
	SHA256 string `json:"sha256"`
	// SubjectKeyID is the key identifier the ledger states, uppercase hex.
	SubjectKeyID string `json:"subjectKeyId"`
	// Kind is "production" or "test".
	Kind string `json:"kind"`
	// NotBefore and NotAfter are the certificate's validity (RFC 3339).
	NotBefore string `json:"notBefore,omitempty"`
	NotAfter  string `json:"notAfter,omitempty"`
}

// ErrMalformed is a snapshot entry that does not fit its manifest: the
// load of that kind stops there, as #consumeCertSeed aborts the stream
// (:1407-1410); the entries before it are kept.
var ErrMalformed = errors.New("snapshot: malformed entry")

// Snapshot is a loaded trust set. It is an [attestation.PAAStore] and
// safe for concurrent use (it does not change after Load).
type Snapshot struct {
	src       Source
	paas      map[string]attestation.PAA
	cdSigners map[string]*ecdsa.PublicKey
	skipped   int
}

var _ attestation.PAAStore = (*Snapshot)(nil)

// Embedded loads the snapshot embedded from data/.
func Embedded() (*Snapshot, error) {
	sub, err := fs.Sub(embedded, "data")
	if err != nil {
		return nil, err
	}
	return Load(sub)
}

// Load reads the snapshot of fsys: SOURCE, then each entry's file. No
// SOURCE is an empty snapshot ("no snapshot"), not an error. Per entry, as
// #consumeCertSeed (:1352-1422): a test entry is skipped (the production
// set, fetchTestCertificates off), an expired one is skipped, one whose
// DER does not parse as its kind or does not carry its stated key
// identifier is skipped (#parseAndCheckSkid, :492-515), a key identifier
// already loaded is skipped; an entry of the wrong shape, a missing file
// or a file whose SHA-256 differs from the manifest stops that kind's load
// with [ErrMalformed] — the snapshot returned beside the error holds the
// entries loaded until then.
func Load(fsys fs.FS) (*Snapshot, error) { return load(fsys, clock.System{}.Now()) }

// LoadAt is [Load] judging expiry at now instead of the system clock's
// time — the seam a host that injects a [clock.Clock] uses.
func LoadAt(fsys fs.FS, now time.Time) (*Snapshot, error) { return load(fsys, now) }

func load(fsys fs.FS, now time.Time) (*Snapshot, error) {
	s := &Snapshot{paas: map[string]attestation.PAA{}, cdSigners: map[string]*ecdsa.PublicKey{}}
	b, err := fs.ReadFile(fsys, SourceFile)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &s.src); err != nil {
		return nil, fmt.Errorf("snapshot: %s: %w", SourceFile, err)
	}
	errPAA := s.consume(fsys, s.src.PAA, dcl.KindPAA, PAADir, now)
	errCD := s.consume(fsys, s.src.CDSigners, dcl.KindCDSigner, CDSignerDir, now)
	return s, errors.Join(errPAA, errCD)
}

func (s *Snapshot) consume(fsys fs.FS, sec Section, kind dcl.Kind, dir string, now time.Time) error {
	if sec.ExpectedCount != len(sec.Entries) {
		return fmt.Errorf("%w: %s lists %d entries, expectedCount %d", ErrMalformed, dir, len(sec.Entries), sec.ExpectedCount)
	}
	for _, e := range sec.Entries {
		skid := strings.ToUpper(strings.ReplaceAll(e.SubjectKeyID, ":", ""))
		if skid == "" || !strings.HasPrefix(e.File, dir+"/") || (e.Kind != KindProduction && e.Kind != KindTest) {
			return fmt.Errorf("%w: %s entry %+v: invalid entry shape", ErrMalformed, dir, e)
		}
		der, err := fs.ReadFile(fsys, e.File)
		if err != nil {
			return fmt.Errorf("%w: %s: %w", ErrMalformed, e.File, err)
		}
		if sum := sha256.Sum256(der); hex.EncodeToString(sum[:]) != strings.ToLower(e.SHA256) {
			return fmt.Errorf("%w: %s: SHA-256 differs from %s", ErrMalformed, e.File, SourceFile)
		}
		if e.Kind == KindTest {
			s.skipped++
			continue
		}
		if e.NotAfter != "" {
			if t, err := time.Parse(time.RFC3339, e.NotAfter); err == nil && t.Before(now) {
				s.skipped++
				continue
			}
		}
		c, err := dcl.CheckTrustStoreCertificate(der, kind, skid)
		if err != nil {
			s.skipped++
			continue
		}
		if _, ok := s.paas[skid]; ok {
			continue
		}
		if _, ok := s.cdSigners[skid]; ok {
			continue
		}
		if kind == dcl.KindPAA {
			s.paas[skid] = attestation.PAA{DER: der, Production: true, FetchedAt: s.src.BuiltAt}
			continue
		}
		key, err := attestation.P256Key(c)
		if err != nil {
			s.skipped++
			continue
		}
		s.cdSigners[skid] = key
	}
	return nil
}

// Source is the manifest the snapshot was loaded from; the zero value for
// an empty snapshot.
func (s *Snapshot) Source() Source { return s.src }

// Len is the number of PAAs and CD signers loaded.
func (s *Snapshot) Len() (paas, cdSigners int) { return len(s.paas), len(s.cdSigners) }

// Skipped is the number of entries skipped (test, expired, unusable).
func (s *Snapshot) Skipped() int { return s.skipped }

// PAA implements [attestation.PAAStore].
func (s *Snapshot) PAA(skid []byte) (attestation.PAA, bool) {
	p, ok := s.paas[strings.ToUpper(hex.EncodeToString(skid))]
	return p, ok
}

// AcceptsTestCertificates implements [attestation.PAAStore]: a snapshot
// holds production certificates only.
func (s *Snapshot) AcceptsTestCertificates() bool { return false }

// CDSigner implements [attestation.PAAStore].
func (s *Snapshot) CDSigner(skid []byte) (*ecdsa.PublicKey, bool) {
	k, ok := s.cdSigners[strings.ToUpper(hex.EncodeToString(skid))]
	return k, ok
}
