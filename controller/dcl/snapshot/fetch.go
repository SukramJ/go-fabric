// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package snapshot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/SukramJ/go-fabric/controller/dcl"
)

// CDSignerKeyIDs are the key identifiers of the production Certification
// Declaration signers the snapshot fetches: the CSA's "Certification
// Declaration Signing Key 001" to "005", the official keys of
// connectedhomeip's DefaultDeviceAttestationVerifier (gCdSigningKey001Kid
// … gCdSigningKey005Kid,
// src/credentials/attestation_verifier/DefaultDeviceAttestationVerifier.cpp:146-256
// at the harness pin 6170af84). matter.js lists none — it fetches a CD
// signer from the ledger by key identifier when a declaration names it
// (DclCertificateService.getOrFetchCdSigner, :851-891 at 0ceb202d); the
// ledger has no listing of CD signers, so the set is named here. A CD
// signer missing from it is still fetched online with --dcl.
var CDSignerKeyIDs = []string{
	"FE343F959947763B61EE4539131338494FE67D8E",
	"DD04DB585B214C1C581587E6568DF487B6DDC701",
	"471035E7C04EAAA8BE7C4D4C13E3E4C20995A84B",
	"F68603A3692E981072419EA1E1AB3854BD7795D3",
	"637F2634AD62EAFE6AF662EFB96F6FD2FCBFFC2F",
}

// Fetch builds a snapshot from the ledger client asks into dir — the
// work of `make update-dcl-snapshot`: every approved root certificate
// (RootCertificates, then RootCertificateBySubject, as
// CertificateStore.fetchDclCertificates does) and the CD signers of
// cdSigners (CertificatesBySubjectKeyID), each kept once its DER parses
// as its kind and carries the key identifier the ledger states, written
// as <dir>/paa/<SKID>.der and <dir>/cd-signers/<SKID>.der with the SOURCE
// manifest. dir's paa/ and cd-signers/ are replaced. It is a review step:
// the diff of dir is read before it is committed.
func Fetch(ctx context.Context, client *dcl.Client, cdSigners []string, dir string, now time.Time) (Source, error) {
	src := Source{Ledger: client.BaseURL, BuiltAt: now.UTC().Truncate(time.Second)}
	if src.Ledger == "" {
		src.Ledger = dcl.ProductionURL
	}
	files := map[string][]byte{}
	refs, err := client.RootCertificates(ctx)
	if err != nil {
		return Source{}, fmt.Errorf("snapshot: root certificates: %w", err)
	}
	for _, ref := range refs {
		certs, err := client.RootCertificateBySubject(ctx, ref)
		if err != nil {
			return Source{}, fmt.Errorf("snapshot: root %s: %w", ref.SubjectKeyID, err)
		}
		for i := range certs {
			if e, der, ok := entryOf(&certs[i], dcl.KindPAA, PAADir); ok && !hasEntry(src.PAA, e) {
				src.PAA.Entries, files[e.File] = append(src.PAA.Entries, e), der
			}
		}
	}
	for _, skid := range cdSigners {
		certs, err := client.CertificatesBySubjectKeyID(ctx, skid)
		if err != nil {
			return Source{}, fmt.Errorf("snapshot: CD signer %s: %w", skid, err)
		}
		for i := range certs {
			if e, der, ok := entryOf(&certs[i], dcl.KindCDSigner, CDSignerDir); ok && !hasEntry(src.CDSigners, e) {
				src.CDSigners.Entries, files[e.File] = append(src.CDSigners.Entries, e), der
			}
		}
	}
	if len(src.PAA.Entries) == 0 {
		return Source{}, errors.New("snapshot: the ledger returned no usable root certificate")
	}
	for _, sec := range []*Section{&src.PAA, &src.CDSigners} {
		slices.SortFunc(sec.Entries, func(a, b Entry) int { return strings.Compare(a.File, b.File) })
		sec.ExpectedCount = len(sec.Entries)
	}
	for _, sub := range []string{PAADir, CDSignerDir} {
		if err := os.RemoveAll(filepath.Join(dir, sub)); err != nil {
			return Source{}, err
		}
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil { //nolint:gosec // G301: a source tree directory
			return Source{}, err
		}
	}
	for name, der := range files {
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(name)), der, 0o644); err != nil { //nolint:gosec // G306: public certificates in the source tree
			return Source{}, err
		}
	}
	b, err := json.MarshalIndent(src, "", "  ")
	if err != nil {
		return Source{}, err
	}
	if err := os.WriteFile(filepath.Join(dir, SourceFile), append(b, '\n'), 0o644); err != nil { //nolint:gosec // G306: see above
		return Source{}, err
	}
	return src, nil
}

// entryOf is one ledger certificate as a snapshot entry, ok false for one
// that is not usable as kind.
func entryOf(c *dcl.Certificate, kind dcl.Kind, dir string) (Entry, []byte, bool) {
	skid := strings.ToUpper(strings.ReplaceAll(c.SubjectKeyID, ":", ""))
	if skid == "" {
		return Entry{}, nil, false
	}
	der, err := dcl.PEMToDER(c.PEMCert)
	if err != nil {
		return Entry{}, nil, false
	}
	x, err := dcl.CheckTrustStoreCertificate(der, kind, skid)
	if err != nil {
		return Entry{}, nil, false
	}
	sum := sha256.Sum256(der)
	return Entry{
		File: path.Join(dir, skid+".der"), SHA256: hex.EncodeToString(sum[:]), SubjectKeyID: skid, Kind: KindProduction,
		NotBefore: x.NotBefore.UTC().Format(time.RFC3339), NotAfter: x.NotAfter.UTC().Format(time.RFC3339),
	}, der, true
}

func hasEntry(sec Section, e Entry) bool {
	return slices.ContainsFunc(sec.Entries, func(x Entry) bool { return x.File == e.File })
}
