// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package dcl

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/asn1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/bdx/otaimage"
	"github.com/SukramJ/go-fabric/controller/attestation"
)

// The CRL revocation check of DclCertificateService.isRevoked
// (DclCertificateService.ts:691-726) and its lazy, hour-long cache
// (:302-326, :1434-1747), Matter Core §6.2.6.1.

// revocationCacheExpiry is the cache's expiration, Hours(1) (:322-326);
// an entry lives an hour past its last use (AsyncCache, packages/general/
// src/util/Cache.ts:72-78, :114-137).
const revocationCacheExpiry = time.Hour

// revocationTimeout bounds each request of a revocation lookup, Seconds(3)
// (:1436).
const revocationTimeout = 3 * time.Second

// revocationEntry is DclCertificateService.RevocationEntry (:2117-2120):
// one authority's revoked serials (uppercase hex) and its Name DER as hex.
type revocationEntry struct {
	serials  map[string]struct{}
	issuerDN string
}

// revocationLookup is RevocationLookup (:153-156): incomplete when a
// distribution point failed transiently.
type revocationLookup struct {
	entries  []revocationEntry
	complete bool
}

// crlRejectedError is CrlRejectedError (:165-166): a distribution point
// §6.2.6.1 says to skip — unlike a download failure, retrying cannot
// change it.
type crlRejectedError struct {
	msg   string
	cause error
}

func (e *crlRejectedError) Error() string {
	if e.cause != nil {
		return e.msg + ": " + e.cause.Error()
	}
	return e.msg
}

func (e *crlRejectedError) Unwrap() error { return e.cause }

func rejected(format string, args ...any) error {
	return &crlRejectedError{msg: fmt.Sprintf(format, args...)}
}

func rejectedBy(cause error, msg string) error { return &crlRejectedError{msg: msg, cause: cause} }

func isRejected(err error) bool {
	var re *crlRejectedError
	return errors.As(err, &re)
}

type cachedLookup struct {
	lookup   revocationLookup
	lastUsed time.Time
}

type inflightLookup struct {
	done   chan struct{}
	lookup revocationLookup
	err    error
}

// revocationCache is the AsyncCache of lookups by authority key
// identifier: a value is reused while used within the hour, one fill runs
// per key, a failed fill is not cached.
type revocationCache struct {
	s *CertificateStore
	// mu guards inflight and values.
	mu       sync.Mutex
	values   map[string]*cachedLookup
	inflight map[string]*inflightLookup
	// installed is #installedRevocations (DclCertificateService.ts:311-312):
	// revocation information supplied out of band, by normalized AKID, for
	// the life of the store and never persisted.
	installed map[string][]revocationEntry
}

func newRevocationCache(s *CertificateStore) *revocationCache {
	return &revocationCache{
		s: s, values: map[string]*cachedLookup{}, inflight: map[string]*inflightLookup{},
		installed: map[string][]revocationEntry{},
	}
}

func (c *revocationCache) get(ctx context.Context, akid string) (revocationLookup, error) {
	now := c.s.clock.Now()
	c.mu.Lock()
	if v, ok := c.values[akid]; ok {
		if now.Sub(v.lastUsed) < revocationCacheExpiry {
			v.lastUsed = now
			c.mu.Unlock()
			return v.lookup, nil
		}
		delete(c.values, akid)
	}
	call, ok := c.inflight[akid]
	if !ok {
		call = &inflightLookup{done: make(chan struct{})}
		c.inflight[akid] = call
		c.mu.Unlock()
		call.lookup, call.err = c.s.fetchAndValidateRevocation(ctx, akid)
		c.mu.Lock()
		delete(c.inflight, akid)
		if call.err == nil {
			c.values[akid] = &cachedLookup{lookup: call.lookup, lastUsed: c.s.clock.Now()}
		}
		close(call.done)
		c.mu.Unlock()
		return call.lookup, call.err
	}
	c.mu.Unlock()
	select {
	case <-call.done:
		return call.lookup, call.err
	case <-ctx.Done():
		return revocationLookup{}, ctx.Err()
	}
}

func (c *revocationCache) delete(akid string) {
	c.mu.Lock()
	delete(c.values, akid)
	c.mu.Unlock()
}

func (c *revocationCache) close() {
	c.mu.Lock()
	c.values = map[string]*cachedLookup{}
	c.mu.Unlock()
}

// matchRevocation is :221-233: an entry for the issuer names the serial;
// an entry or query without an issuer name matches any issuer.
func matchRevocation(entries []revocationEntry, serial, issuerDN string) bool {
	for i := range entries {
		e := &entries[i]
		if _, ok := e.serials[serial]; !ok {
			continue
		}
		if issuerDN == "" || e.issuerDN == "" || sameNameHex(e.issuerDN, issuerDN) {
			return true
		}
	}
	return false
}

// IsRevoked implements attestation.Revocations — isRevoked (:691-726):
// the CRLs the ledger names for the authority akid, fetched and validated
// on first use and cached for an hour, list serial (the DER INTEGER
// content) under the issuer issuerDN (uppercase hex of its Name DER). An
// offline store, or a lookup that failed, reports not revoked; a lookup
// that was incomplete is not kept.
func (s *CertificateStore) IsRevoked(akid, serial []byte, issuerDN string) (bool, error) {
	// A closing store must not answer "not revoked" for a lookup it never
	// ran. Mirrors matter.js
	// packages/protocol/src/dcl/DclCertificateService.ts:706-710.
	if s.isClosed() {
		return false, ErrStoreClosed
	}
	id := hexUpper(akid)
	// An installed set answers first, offline too; it can only add
	// revocations. Mirrors matter.js
	// packages/protocol/src/dcl/DclCertificateService.ts:713-724.
	if s.revocations.installedMatch(id, canonicalSerial(hexUpper(serial)), issuerDN) {
		return true, nil
	}
	if s.offline() {
		return false, nil
	}
	lookup, err := s.revocations.get(context.Background(), id)
	if err != nil {
		// The fetch logged the cause; an unreachable CRL is not-revoked
		// (:715-718).
		return false, nil //nolint:nilerr // matter.js answers an unreachable CRL with not revoked
	}
	if !lookup.complete {
		s.revocations.delete(id)
	}
	return matchRevocation(lookup.entries, hexUpper(serial), issuerDN), nil
}

// fetchAndValidateRevocation is #fetchAndValidateRevocation (:1434-1504):
// the issuer's CRL distribution points, each processed; NOT_FOUND is no
// CRL (cached), a transient failure that left nothing usable an error (not
// cached).
func (s *CertificateStore) fetchAndValidateRevocation(ctx context.Context, akid string) (revocationLookup, error) {
	empty := revocationLookup{complete: true}
	client := s.opts.Client.withTimeout(revocationTimeout)
	points, err := client.RevocationPointsByIssuer(ctx, akid)
	if err != nil {
		if IsNotFound(err) {
			s.log.Debug("dcl.revocation.none", slog.String("akid", akid))
			return empty, nil
		}
		s.log.Warn("dcl.revocation.lookup_failed", slog.String("akid", akid), slog.Any("err", err),
			slog.String("message", "DCL revocation lookup failed for AKID "+akid+" (will retry on next check)"))
		return revocationLookup{}, err
	}
	// CRL entries for exactly the issuer asked about (§11.23.11.7).
	var crlPoints []RevocationPoint
	for i := range points {
		if p := &points[i]; p.RevocationType == RevocationTypeCRL && normalizeKeyID(p.IssuerSubjectKeyID) == akid {
			crlPoints = append(crlPoints, *p)
		}
	}
	if len(crlPoints) == 0 {
		return empty, nil
	}
	// Every entry contributes: an issuer may partition its revocations over
	// several CRLs (§6.2.6.1); step 7.2 applies to entries sharing VendorID
	// and IssuerSubjectKeyID (§11.23.11.8).
	type result struct {
		entry revocationEntry
		err   error
	}
	results := make([]result, len(crlPoints))
	var wg sync.WaitGroup
	for i := range crlPoints {
		p := crlPoints[i]
		sameVID := 0
		for j := range crlPoints {
			if crlPoints[j].VID == p.VID {
				sameVID++
			}
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			e, err := s.processRevocationPoint(ctx, p, sameVID > 1)
			results[i] = result{e, err}
		}()
	}
	wg.Wait()
	var lookup revocationLookup
	transient := 0
	for i := range results {
		r := &results[i]
		if r.err == nil {
			lookup.entries = append(lookup.entries, r.entry)
			continue
		}
		rej := isRejected(r.err)
		if !rej {
			transient++
		}
		verb := "Failed to process"
		if rej {
			verb = "Ignoring"
		}
		s.log.Warn("dcl.revocation.point_failed", slog.String("akid", crlPoints[i].IssuerSubjectKeyID), slog.String("url", crlPoints[i].DataURL),
			slog.Any("err", r.err), slog.String("message", verb+" revocation point for "+crlPoints[i].IssuerSubjectKeyID+" at "+crlPoints[i].DataURL))
	}
	if len(lookup.entries) == 0 && transient > 0 {
		return revocationLookup{}, dclErrorf("All CRL downloads failed for AKID %s", akid)
	}
	lookup.complete = transient == 0
	return lookup, nil
}

// processRevocationPoint is #processRevocationPoint (:1510-1565): the
// signer chain, the CRL download, its size and digest, its authentication.
func (s *CertificateStore) processRevocationPoint(ctx context.Context, p RevocationPoint, partitioned bool) (revocationEntry, error) {
	auth, err := s.anchorCRLSigner(p)
	if err != nil {
		if isRejected(err) {
			return revocationEntry{}, err
		}
		return revocationEntry{}, rejectedBy(err, "CRL signer certificate is unusable")
	}
	crl, err := s.downloadCRL(ctx, p.DataURL, p.DataFileSize)
	if err != nil {
		return revocationEntry{}, err
	}
	if p.DataFileSize > 0 && uint64(len(crl)) != p.DataFileSize {
		return revocationEntry{}, rejected("CRL size mismatch: expected %d bytes, got %d", p.DataFileSize, len(crl))
	}
	if p.DataDigest != "" && p.DataDigestType != 0 {
		if h := otaimage.NewDigest(p.DataDigestType); h != nil {
			h.Write(crl)
			if actual := base64.StdEncoding.EncodeToString(h.Sum(nil)); actual != p.DataDigest {
				return revocationEntry{}, rejected("CRL digest mismatch: expected %s, got %s", p.DataDigest, actual)
			}
		} else {
			s.log.Info("dcl.revocation.digest_unsupported", slog.Int("type", int(p.DataDigestType)),
				slog.String("message", fmt.Sprintf("Skipping CRL digest verification: unsupported digest type %d", p.DataDigestType)))
		}
	}
	entry, err := authenticateCRL(p, crl, auth, partitioned)
	if err != nil {
		if isRejected(err) {
			return revocationEntry{}, err
		}
		return revocationEntry{}, rejectedBy(err, "CRL from "+p.DataURL+" is malformed")
	}
	return entry, nil
}

// maxCRLSize bounds a CRL download whose revocation point states no
// DataFileSize; 1 MiB holds any Matter CRL.
const maxCRLSize = 1 << 20

// downloadCRL fetches dataUrl (:1524-1534): a client error is a rejection,
// a server error, request timeout or rate limit transient. The body is
// read up to the point's DataFileSize plus one byte — a longer body then
// fails processRevocationPoint's size check — and, without a
// DataFileSize, up to maxCRLSize, past which it is a rejection. matter.js
// reads the whole body (DclCertificateService.ts:1543-1552, fetch +
// arrayBuffer) and rejects a size mismatch afterwards (:1559); the bound
// keeps that verdict without buffering what a hostile CRL host sends past
// it.
func (s *CertificateStore) downloadCRL(ctx context.Context, dataURL string, dataFileSize uint64) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, revocationTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, dataURL, http.NoBody)
	if err != nil {
		return nil, err
	}
	resp, err := s.opts.Client.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		msg := fmt.Sprintf("Failed to fetch CRL from %s: %d", dataURL, resp.StatusCode)
		if resp.StatusCode >= 500 || resp.StatusCode == http.StatusRequestTimeout || resp.StatusCode == http.StatusTooManyRequests {
			return nil, &Error{Message: msg}
		}
		return nil, rejected("%s", msg)
	}
	limit := uint64(maxCRLSize)
	if dataFileSize > 0 {
		limit = dataFileSize
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(min(limit, 1<<62))+1)) //nolint:gosec // bounded above
	if err != nil {
		return nil, err
	}
	if dataFileSize == 0 && uint64(len(body)) > limit {
		return nil, rejected("CRL from %s exceeds %d bytes", dataURL, limit)
	}
	return body, nil
}

// crlAuthority is CrlAuthority (:159-163): the validated CRL signer and the
// Name of the authority whose revocations its CRL speaks for.
type crlAuthority struct {
	signer *trustCert
	name   []byte
}

// authenticateCRL is #authenticateCrl (:1568-1624), Matter Core §6.2.6.1
// steps 7-10.
func authenticateCRL(p RevocationPoint, crlDER []byte, auth crlAuthority, partitioned bool) (revocationEntry, error) {
	crl, err := parseCRL(crlDER, auth.name)
	if err != nil {
		return revocationEntry{}, err
	}
	// RFC 5280 §6.3.3: delta CRLs and other unknown critical extensions
	// forbid use.
	if crl.unsupportedCriticalExtension != "" {
		return revocationEntry{}, rejected("CRL carries unsupported critical extension %s", crl.unsupportedCriticalExtension)
	}
	// Step 7.1.
	signerSKID := hexUpper(auth.signer.x509.SubjectKeyId)
	if crl.authorityKeyID != signerSKID {
		got := crl.authorityKeyID
		if got == "" {
			got = "(none)"
		}
		return revocationEntry{}, rejected("CRL authority key identifier %s does not match CRL signer %s", got, signerSKID)
	}
	// RFC 5280 §5.1.2.3: the CRL names the signer as its issuer.
	issuer, ok := decodeHex(crl.issuerDN)
	if crl.issuerDN == "" || !ok || !sameName(issuer, auth.signer.x509.RawSubject) {
		return revocationEntry{}, rejected("CRL issuer name is not the CRL signer's subject")
	}
	// Step 7.2.
	if partitioned && crl.issuingDistributionPointURI != p.DataURL {
		named := crl.issuingDistributionPointURI
		if named == "" {
			named = "no single URI"
		}
		return revocationEntry{}, rejected("CRL from %s is one of several for its issuer but its Issuing Distribution Point names %s", p.DataURL, named)
	}
	// Step 8, RFC 5280 §6.3 with the CRL signer as trust anchor.
	if crl.tbs == nil || crl.signatureAlgorithm == nil || crl.signatureValue == nil {
		return revocationEntry{}, rejected("CRL from %s carries no valid signature", p.DataURL)
	}
	if err := verifyCRLSignature(auth.signer.x509, crl.tbs, crl.signatureAlgorithm, crl.signatureValue); err != nil {
		return revocationEntry{}, rejectedBy(err, "CRL signature verification failed against CRLSignerCertificate")
	}
	// Steps 9-10: the revocations belong to the authority, whatever issuer
	// name an indirect CRL states.
	return revocationEntry{serials: crl.serials, issuerDN: hexUpper(auth.name)}, nil
}

// oidECDSAWithSHA256 is X962.EcdsaWithSHA256 (1.2.840.10045.4.3.2).
var oidECDSAWithSHA256 = asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 3, 2}

// verifyCRLSignature is certificateSignatureOf and verifyCertificateSignature
// (kinds/CertificateSignature.ts:67-120) for an EC P-256 signer: the
// algorithm must be ecdsa-with-SHA256 (ML-DSA is PQC Phase 1, not
// admitted here) and the DER signature must verify over the TBS.
func verifyCRLSignature(signer *x509.Certificate, tbs, algorithm, value []byte) error {
	alg, err := derDecode(algorithm)
	if err != nil || len(alg.elements) == 0 || alg.elements[0].tag != derObjectID {
		return errors.New("dcl: signature algorithm identifier holds no OID")
	}
	var oid asn1.ObjectIdentifier
	if _, err := asn1.Unmarshal(derEncode(alg.elements[0]), &oid); err != nil || !oid.Equal(oidECDSAWithSHA256) {
		return fmt.Errorf("dcl: signature algorithm %s is neither ecdsa-with-SHA256 nor ML-DSA", strings.ToLower(hexUpper(alg.elements[0].bytes)))
	}
	key, err := attestation.P256Key(signer)
	if err != nil {
		return errors.New("dcl: signature is ecdsa-with-SHA256 but the signer key is not ECDSA-P256")
	}
	digest := sha256.Sum256(tbs)
	if !ecdsa.VerifyASN1(key, digest[:], value) {
		return errors.New("dcl: signature does not verify")
	}
	return nil
}

// anchorCRLSigner is #anchorCrlSigner (:1632-1713): the CRL signer of a
// distribution point and the authority whose revocations it signs, per
// Matter Core §6.2.6.1 steps 2-5 and 9 and the signer forms of
// §11.23.11.6.
func (s *CertificateStore) anchorCRLSigner(p RevocationPoint) (crlAuthority, error) {
	entrySKID := normalizeKeyID(p.IssuerSubjectKeyID)

	// Step 2.
	signerDER, err := pemToDER(p.CRLSignerCertificate)
	if err != nil {
		return crlAuthority{}, err
	}
	signer, err := parseCert(signerDER, p.IsPAA)
	if err != nil {
		return crlAuthority{}, err
	}
	var delegator *trustCert
	if p.CRLSignerDelegator != "" {
		der, err := pemToDER(p.CRLSignerDelegator)
		if err != nil {
			return crlAuthority{}, err
		}
		if delegator, err = parseCert(der, false); err != nil {
			return crlAuthority{}, err
		}
	}
	selfSigned := sameName(signer.x509.RawSubject, signer.x509.RawIssuer) && signsItself(signer)
	if err := checkSignerForm(p, signer, delegator, selfSigned); err != nil {
		return crlAuthority{}, err
	}
	authority, err := s.crlAuthorityOf(p, signer, signerDER, delegator, selfSigned)
	if err != nil {
		return crlAuthority{}, err
	}
	if authoritySKID := hexUpper(authority.x509.SubjectKeyId); authoritySKID != entrySKID {
		return crlAuthority{}, rejected("CRL authority %s is not the entry's issuer %s", authoritySKID, entrySKID)
	}
	return crlAuthority{signer: signer, name: authority.x509.RawSubject}, nil
}

// checkSignerForm is :1642-1670: the signer forms of §11.23.11.6 and the
// vendor and product of §6.2.6.1 steps 3-4.
func checkSignerForm(p RevocationPoint, signer, delegator *trustCert, selfSigned bool) error {
	// RFC 5280 §6.3.3(f) and the §11.23.11.6 formats: every signer may sign
	// CRLs, and only a PAA or PAI signs as CA.
	if signer.x509.KeyUsage&x509.KeyUsageCRLSign == 0 {
		return rejected("CRL signer certificate lacks the cRLSign key usage")
	}
	delegated := delegator != nil
	if p.IsPAA {
		delegated = !selfSigned
	}
	if signer.x509.IsCA == delegated {
		if delegated {
			return rejected("A delegated CRL signer must not be a CA certificate")
		}
		return rejected("A CRL signer that is not delegated must be the PAA or PAI itself")
	}
	if delegator != nil && !delegator.x509.IsCA {
		return rejected("CRLSignerDelegator is not a PAI")
	}
	// Steps 3-4: a PAA may omit its VendorID (step 3.1); a PAI states
	// exactly one, which must match (step 4.1).
	vidHolder := signer
	if !p.IsPAA && delegator != nil {
		vidHolder = delegator
	}
	if vid := vidHolder.vendorID; (vid != nil || !p.IsPAA) && (vid == nil || *vid != p.VID) {
		return rejected("CRL signer VendorID %s does not match entry VendorID %d", idText(vid), p.VID)
	}
	// The PAI's ProductID; the entry's absent ProductID reads as 0, which a
	// PAI may state too.
	if !p.IsPAA && vidHolder.productID != nil && *vidHolder.productID != p.PID {
		return rejected("CRL signer ProductID %d does not match entry ProductID %d", *vidHolder.productID, p.PID)
	}
	return nil
}

// crlAuthorityOf is :1672-1705, step 5 and the step 9 authority: the
// approved PAA a self-signed PAA signer must be byte for byte, the PAA
// that issued a delegated PAA signer, or the PAI a PAI entry names.
func (s *CertificateStore) crlAuthorityOf(p RevocationPoint, signer *trustCert, signerDER []byte, delegator *trustCert, selfSigned bool) (*trustCert, error) {
	switch {
	case p.IsPAA && selfSigned:
		approved, err := s.trustedPAADER(hexUpper(signer.x509.SubjectKeyId))
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(approved, signerDER) {
			return nil, rejected("Self-signed CRL signer is not an approved PAA")
		}
		return signer, nil
	case p.IsPAA:
		// A PAA-delegated signer: the entry names the PAA, which issued it.
		paa, err := s.trustedPAA(normalizeKeyID(p.IssuerSubjectKeyID))
		if err != nil {
			return nil, err
		}
		if err := assertIssuedBy(signer, paa); err != nil {
			return nil, err
		}
		return paa, nil
	case selfSigned:
		return nil, rejected("Self-signed CRL signer on an entry that is not for a PAA")
	}
	// A PAI, or a signer the delegating PAI issued; either way the entry
	// names that PAI.
	pai := signer
	if delegator != nil {
		pai = delegator
		if err := assertIssuedBy(signer, delegator); err != nil {
			return nil, err
		}
	}
	if len(pai.x509.AuthorityKeyId) == 0 {
		return nil, rejected("CRL signing PAI names no authority key identifier")
	}
	paa, err := s.trustedPAA(hexUpper(pai.x509.AuthorityKeyId))
	if err != nil {
		return nil, err
	}
	if err := assertIssuedBy(pai, paa); err != nil {
		return nil, err
	}
	return pai, nil
}

// trustedPAA is Paa.fromAsn1 of #trustedPaaDer.
func (s *CertificateStore) trustedPAA(skid string) (*trustCert, error) {
	der, err := s.trustedPAADER(skid)
	if err != nil {
		return nil, err
	}
	return parseCert(der, true)
}

func idText(id *uint16) string {
	if id == nil {
		return "undefined"
	}
	return strconv.FormatUint(uint64(*id), 10)
}

// signsItself is #signsItself (:1716-1723).
func signsItself(c *trustCert) bool {
	key, err := attestation.P256Key(c.x509)
	return err == nil && attestation.VerifySignedBy(c.x509, key) == nil
}

// trustedPAADER is #trustedPaaDer (:1725-1731): the stored PAA with that
// key identifier, test PAAs honoured; an entry of another kind is a
// rejection, an absent one an error.
func (s *CertificateStore) trustedPAADER(skid string) ([]byte, error) {
	e, ok := s.lookup(skid)
	if ok && e.kind() != KindPAA {
		return nil, rejected("CRL signer chain names %s, which is not a PAA", skid)
	}
	if !ok {
		return nil, dclErrorf("Certificate not found (skid %s)", skid)
	}
	return e.DER, nil
}

// assertIssuedBy is #assertIssuedBy (:1734-1747): an issuer that may sign
// certificates, matching names and its signature.
func assertIssuedBy(subject, issuer *trustCert) error {
	// RFC 5280 §6.1.4(n).
	if issuer.x509.KeyUsage&x509.KeyUsageCertSign == 0 {
		return rejected("CRL signer chain issuer lacks the keyCertSign key usage")
	}
	if !sameName(subject.x509.RawIssuer, issuer.x509.RawSubject) {
		return rejected("CRL signer chain names do not match")
	}
	key, err := attestation.P256Key(issuer.x509)
	if err == nil {
		err = attestation.VerifySignedBy(subject.x509, key)
	}
	if err != nil {
		return rejectedBy(err, "CRL signer chain signature does not verify")
	}
	return nil
}

// Revocation information installed from outside the DCL.
// Mirrors matter.js packages/protocol/src/dcl/DclCertificateService.ts:640-687
// (installRevocations, installedRevocations) and :1767-1800
// (parseRevocationSet).

// RevocationSetEntry is DclCertificateService.RevocationSetEntry
// (DclCertificateService.ts:2092-2104): one authority's revoked serial
// numbers, as a revocation set states them.
type RevocationSetEntry struct {
	// IssuerSubjectKeyID is the key identifier of the certificate that
	// issued the revoked ones, hex with or without colons.
	IssuerSubjectKeyID string
	// IssuerName is base64 of the issuer's Name DER; "" matches on the key
	// identifier alone.
	IssuerName string
	// RevokedSerialNumbers are hex, with or without colons.
	RevokedSerialNumbers []string
}

// InstalledRevocation is DclCertificateService.InstalledRevocation
// (:2128-2132), as [CertificateStore.InstalledRevocations] reports it.
type InstalledRevocation struct {
	// Serials are the revoked serials as canonical uppercase hex, sorted.
	Serials []string
	// IssuerDN is uppercase hex of the issuer Name DER; "" when the entry
	// named none.
	IssuerDN string
}

// ErrInvalidRevocationSet wraps every refusal of [ParseRevocationSet] and
// [CertificateStore.InstallRevocations] (matter.js ImplementationError).
var ErrInvalidRevocationSet = errors.New("dcl: invalid revocation set")

// ErrStoreClosed is IsRevoked's answer on a store that is closing or
// closed — matter.js's ClosedError (DclCertificateService.ts:706-710).
var ErrStoreClosed = errors.New("dcl: certificate store closed")

func revocationSetErrorf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidRevocationSet, fmt.Sprintf(format, args...))
}

// canonicalSerial is canonicalSerial (:276-288): uppercase hex without
// separators or leading zero bytes, so a certificate's DER INTEGER content
// (00E1…) and a set's number (E1…) agree.
func canonicalSerial(serial string) string {
	h := strings.ToUpper(strings.ReplaceAll(serial, ":", ""))
	for strings.HasPrefix(h, "00") {
		h = h[2:]
	}
	if h == "" {
		return "00"
	}
	return h
}

// statedHex is statedHex (:60-82): a string field without colons, refused
// unless it is a non-empty, even number of hex digits.
func statedHex(value any, field string, index int) (string, error) {
	str, ok := value.(string)
	if !ok {
		return "", revocationSetErrorf("Revocation set entry %d states %s as something other than a string", index, field)
	}
	stated := strings.ReplaceAll(str, ":", "")
	if _, ok := decodeHex(stated); stated == "" || !ok {
		return "", revocationSetErrorf("Revocation set entry %d states %s as %q, which is not an even number of hex digits", index, field, stated)
	}
	return stated, nil
}

// statedIssuerName is statedIssuerName and issuerDnOf (:93-127): the Name
// DER an entry states as base64, nil when it states none or an empty one.
func statedIssuerName(value any, present bool, index int) ([]byte, error) {
	if !present || value == "" {
		return nil, nil
	}
	str, ok := value.(string)
	if !ok {
		return nil, revocationSetErrorf("Revocation set entry %d states issuer_name as something other than a string", index)
	}
	der, err := forgivingBase64(str)
	if err != nil {
		return nil, revocationSetErrorf("Revocation set entry %d states an issuer_name that is not base64: %v", index, err)
	}
	return der, nil
}

// forgivingBase64 is atob, which Bytes.fromBase64 (packages/general/src/
// util/Bytes.ts:115-123) decodes with: ASCII whitespace dropped, padding
// optional.
func forgivingBase64(s string) ([]byte, error) {
	s = strings.Map(func(r rune) rune {
		if strings.ContainsRune(" \t\n\f\r", r) {
			return -1
		}
		return r
	}, s)
	if len(s)%4 == 0 {
		s = strings.TrimSuffix(strings.TrimSuffix(s, "="), "=")
	}
	return base64.RawStdEncoding.DecodeString(s)
}

// InstallRevocations is installRevocations (:640-673): revocation
// information that did not come from the DCL, trusted as it stands (no
// signer chain, no CRL signature), held in memory for the life of the
// store and never persisted. A listed serial is revoked; every other one
// still goes to the DCL. Nothing is installed unless every entry reads;
// installing a set twice changes nothing. An entry that names no issuer
// matches on its key identifier alone.
func (s *CertificateStore) InstallRevocations(entries []RevocationSetEntry) error {
	type read struct {
		akid, issuerDN string
		serials        []string
	}
	reads := make([]read, 0, len(entries))
	for i := range entries {
		e := &entries[i]
		akid, err := statedHex(e.IssuerSubjectKeyID, "issuerSubjectKeyId", i)
		if err != nil {
			return err
		}
		name, err := statedIssuerName(e.IssuerName, true, i)
		if err != nil {
			return err
		}
		r := read{akid: strings.ToUpper(akid), serials: make([]string, len(e.RevokedSerialNumbers))}
		if name != nil {
			r.issuerDN = hexUpper(name)
		}
		for j, serial := range e.RevokedSerialNumbers {
			h, err := statedHex(serial, fmt.Sprintf("revokedSerialNumbers[%d]", j), i)
			if err != nil {
				return err
			}
			r.serials[j] = canonicalSerial(h)
		}
		reads = append(reads, r)
	}
	c := s.revocations
	c.mu.Lock()
	for _, r := range reads {
		installed := c.installed[r.akid]
		k := slices.IndexFunc(installed, func(e revocationEntry) bool { return e.issuerDN == r.issuerDN })
		if k < 0 {
			installed = append(installed, revocationEntry{serials: map[string]struct{}{}, issuerDN: r.issuerDN})
			k = len(installed) - 1
			c.installed[r.akid] = installed
		}
		for _, serial := range r.serials {
			installed[k].serials[serial] = struct{}{}
		}
	}
	authorities := len(c.installed)
	c.mu.Unlock()
	if len(reads) > 0 {
		s.log.Info("dcl.revocation.installed", slog.Int("entries", len(reads)), slog.Int("authorities", authorities),
			slog.String("message", "Installed revocation information from outside the DCL"))
	}
	return nil
}

// InstalledRevocations is installedRevocations (:675-687): a copy of the
// installed revocation information by the key identifier it was stated
// for; the store's own can only be added to, through InstallRevocations.
func (s *CertificateStore) InstalledRevocations() map[string][]InstalledRevocation {
	c := s.revocations
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string][]InstalledRevocation, len(c.installed))
	for akid, entries := range c.installed {
		list := make([]InstalledRevocation, len(entries))
		for i, e := range entries {
			serials := make([]string, 0, len(e.serials))
			for serial := range e.serials {
				serials = append(serials, serial)
			}
			slices.Sort(serials)
			list[i] = InstalledRevocation{Serials: serials, IssuerDN: e.issuerDN}
		}
		out[akid] = list
	}
	return out
}

func (c *revocationCache) installedMatch(akid, serial, issuerDN string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return matchRevocation(c.installed[akid], serial, issuerDN)
}

// ParseRevocationSet is parseRevocationSet (:1767-1800): the per-issuer
// list of revoked serial numbers connectedhomeip's
// generate_revocation_set.py writes — that tool's format, not a DCL wire
// format. Fields beyond the ones InstallRevocations needs are ignored.
func ParseRevocationSet(data []byte) ([]RevocationSetEntry, error) {
	var parsed any
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, revocationSetErrorf("Revocation set is not JSON: %v", err)
	}
	list, ok := parsed.([]any)
	if !ok {
		return nil, revocationSetErrorf("Revocation set must be an array of entries")
	}
	out := make([]RevocationSetEntry, 0, len(list))
	for i, item := range list {
		entry, ok := item.(map[string]any)
		if !ok {
			return nil, revocationSetErrorf("Revocation set entry %d is not an object", i)
		}
		// statedType (:113-122).
		if t, ok := entry["type"]; ok && t != "revocation_set" {
			return nil, revocationSetErrorf("Revocation set entry %d states type %q rather than %q", i, jsString(t), "revocation_set")
		}
		akid, err := statedHex(entry["issuer_subject_key_id"], "issuer_subject_key_id", i)
		if err != nil {
			return nil, err
		}
		name, present := entry["issuer_name"]
		if _, err := statedIssuerName(name, present, i); err != nil {
			return nil, err
		}
		issuerName, _ := name.(string)
		// statedSerials (:84-91).
		stated, ok := entry["revoked_serial_numbers"].([]any)
		if !ok {
			return nil, revocationSetErrorf("Revocation set entry %d states revoked_serial_numbers as something other than a list", i)
		}
		serials := make([]string, len(stated))
		for j, v := range stated {
			if serials[j], err = statedHex(v, fmt.Sprintf("revoked_serial_numbers[%d]", j), i); err != nil {
				return nil, err
			}
		}
		out = append(out, RevocationSetEntry{IssuerSubjectKeyID: akid, IssuerName: issuerName, RevokedSerialNumbers: serials})
	}
	return out, nil
}

// jsString is String(value) for a decoded JSON value.
func jsString(v any) string {
	if v == nil {
		return "null"
	}
	if s, ok := v.(string); ok {
		return s
	}
	b, _ := json.Marshal(v)
	return string(b)
}
