// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package dcl

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/controller/attestation"
)

// DefaultUpdateInterval is how often the trust store refreshes — matter.js
// DclCertificateService's updateInterval default, Days.one
// (DclCertificateService.ts:345-351).
const DefaultUpdateInterval = 24 * time.Hour

// CertificateOptions configures a [CertificateStore] — the subset of
// matter.js DclCertificateService.Options (DclCertificateService.ts:
// 2016-2071) this port has.
type CertificateOptions struct {
	// Client asks the production ledger. nil is matter.js's offline
	// (:2061-2070): no update, no CRL, no certificate fetched on demand —
	// the store holds what Store and AddCertificate gave it.
	Client *Client
	// TestClient asks the test-net ledger when FetchTestCertificates is
	// set; nil is Client's settings against [TestNetURL] (DclConfig.test).
	TestClient *Client
	// FetchTestCertificates also fetches the test-net ledger's roots
	// (fetchTestCertificates, default false).
	FetchTestCertificates bool
	// AcceptTestCertificates trusts test roots as such; nil is
	// FetchTestCertificates (acceptsTestCertificates, :418-420).
	AcceptTestCertificates *bool
	// FetchGitHubCertificates also fetches connectedhomeip's development
	// PAAs and CD signers from GitHub, with FetchTestCertificates. Off by
	// default — matter.js turns it on with fetchTestCertificates
	// (:1000-1001); this module asks no third party unless told to.
	FetchGitHubCertificates bool
	// GitHub is the development-certificate repository; the zero value is
	// [DefaultGitHubConfig].
	GitHub GitHubConfig
	// UpdateInterval is the refresh period; 0 is [DefaultUpdateInterval],
	// a negative one refreshes never (updateInterval null).
	UpdateInterval time.Duration
	// Store keeps the trust store between runs; nil is a [MemoryStore].
	Store Store
	// Clock times the refresh and stamps FetchedAt; nil is the system
	// clock.
	Clock exchange.Clock
	// Logger receives the store's log; nil discards it.
	Logger *slog.Logger
	// Revocations is revocation information from outside the DCL,
	// installed before the first update as InstallRevocations does —
	// matter.js's revocations option (DclCertificateService.ts:328-330).
	Revocations []RevocationSetEntry
}

// CertificateMetadata is a stored certificate's record — matter.js
// DclCertificateService.CertificateMetadata (DclCertificateService.ts:
// 2094-2108).
type CertificateMetadata struct {
	Subject       string    `json:"subject,omitempty"`
	SubjectAsText string    `json:"subjectAsText,omitempty"`
	SubjectKeyID  string    `json:"subjectKeyId"`
	SerialNumber  string    `json:"serialNumber"`
	VID           uint16    `json:"vid"`
	IsRoot        bool      `json:"isRoot"`
	IsProduction  bool      `json:"isProduction"`
	Kind          Kind      `json:"kind,omitempty"`
	FetchedAt     time.Time `json:"fetchedAt"`
}

func (m CertificateMetadata) kind() Kind {
	if m.Kind == "" {
		return KindPAA
	}
	return m.Kind
}

// storedCertificate is one persisted entry: the metadata and the DER.
type storedCertificate struct {
	CertificateMetadata
	DER []byte `json:"der"`
}

// storedIndex is the persisted trust store.
type storedIndex struct {
	Certificates []storedCertificate `json:"certificates"`
}

// CertificateStore is the controller's DCL trust store — matter.js
// DclCertificateService (packages/protocol/src/dcl/DclCertificateService.ts
// at 0ceb202d) for what the attestation check needs: the PAAs and CD
// signers fetched from the ledger, persisted and refreshed daily, the
// GitHub development-certificate fallback behind an option, and the CRL
// revocation check (revocation.go). It implements attestation.PAAStore
// and attestation.Revocations; commissioner.Context.Attestation takes it.
type CertificateStore struct {
	opts   CertificateOptions
	clock  exchange.Clock
	log    *slog.Logger
	store  Store
	github *githubClient

	// mu guards closed, githubRateLimited, index and timer.
	mu      sync.Mutex
	index   map[string]storedCertificate
	closed  bool
	timer   exchange.Timer
	fetchMu sync.Mutex // the in-flight update (#fetchPromise, :300)
	// githubRateLimited skips GitHub for the rest of one update (:297,
	// :1165-1172).
	githubRateLimited bool

	revocations *revocationCache

	// periodic is the context of the timer's updates, cancelled by Close
	// so a shutdown does not wait out a multi-minute update (W4-ctlint-12).
	periodic       context.Context
	cancelPeriodic context.CancelFunc
}

var (
	_ attestation.PAAStore    = (*CertificateStore)(nil)
	_ attestation.Revocations = (*CertificateStore)(nil)
)

// OpenCertificateStore loads the stored trust store, runs the first update
// and starts the periodic one — the service's construction
// (DclCertificateService.ts:332-352). A failed update is logged, not
// returned (update :952-972); a store that cannot be read is an error.
func OpenCertificateStore(ctx context.Context, opts CertificateOptions) (*CertificateStore, error) {
	s := &CertificateStore{opts: opts, clock: opts.Clock, log: opts.Logger, store: opts.Store, index: map[string]storedCertificate{}}
	s.periodic, s.cancelPeriodic = context.WithCancel(context.Background())
	if s.clock == nil {
		s.clock = exchange.SystemClock{}
	}
	if s.log == nil {
		s.log = slog.New(slog.DiscardHandler)
	}
	if s.store == nil {
		s.store = &MemoryStore{}
	}
	if opts.FetchTestCertificates && opts.FetchGitHubCertificates && opts.Client != nil {
		s.github = newGitHubClient(opts.GitHub, opts.Client.httpClient())
	}
	s.revocations = newRevocationCache(s)
	if opts.Revocations != nil {
		if err := s.InstallRevocations(opts.Revocations); err != nil {
			return nil, err
		}
	}
	s.log.Info("dcl.certificates.init",
		slog.Bool("offline", s.offline()), slog.Bool("test", opts.FetchTestCertificates),
		slog.Bool("github", s.github != nil), slog.Bool("accept_test", s.AcceptsTestCertificates()))
	if err := s.loadIndex(ctx); err != nil {
		return nil, fmt.Errorf("dcl: load the trust store: %w", err)
	}
	// update answers an offline store by doing nothing (:342-343).
	s.Update(ctx, false)
	if !s.offline() && opts.UpdateInterval >= 0 {
		s.scheduleUpdate() //nolint:contextcheck // the periodic update outlives the opening context
	}
	return s, nil
}

func (s *CertificateStore) offline() bool { return s.opts.Client == nil }

func (s *CertificateStore) interval() time.Duration {
	if s.opts.UpdateInterval == 0 {
		return DefaultUpdateInterval
	}
	return s.opts.UpdateInterval
}

// scheduleUpdate arms the periodic update ("DCL Certificate Update",
// :348-350).
func (s *CertificateStore) scheduleUpdate() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.timer = s.clock.AfterFunc(s.interval(), func() {
		s.Update(s.periodic, false)
		s.scheduleUpdate()
	})
}

// Close stops the periodic update and waits for a running one — close
// (:896-906), which awaits #fetchPromise. A running periodic update is
// cancelled first, so Close waits for its fetches to return, not for
// every remaining root to be fetched or time out (W4-ctlint-12); an
// update a caller started runs under the caller's context.
func (s *CertificateStore) Close() {
	s.mu.Lock()
	s.closed = true
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	s.mu.Unlock()
	if s.cancelPeriodic != nil {
		s.cancelPeriodic()
	}
	s.awaitUpdate()
	s.revocations.close()
}

// awaitUpdate waits for a running update to settle (close :902-903).
func (s *CertificateStore) awaitUpdate() {
	s.fetchMu.Lock()
	s.fetchMu.Unlock() //nolint:gocritic,staticcheck // an empty critical section: it only waits for a running update
}

func (s *CertificateStore) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// AcceptsTestCertificates implements attestation.PAAStore —
// acceptsTestCertificates (:418-420).
func (s *CertificateStore) AcceptsTestCertificates() bool {
	if s.opts.AcceptTestCertificates != nil {
		return *s.opts.AcceptTestCertificates
	}
	return s.opts.FetchTestCertificates
}

// isRelevant is #isRelevant (:380-387): a production certificate or a CD
// signer always, a test PAA when test certificates are considered.
func (s *CertificateStore) isRelevant(m CertificateMetadata, considerTest bool) bool {
	return m.kind() != KindPAA || m.IsProduction || considerTest
}

// Certificates lists every stored certificate's metadata (certificates,
// :406-409).
func (s *CertificateStore) Certificates() []CertificateMetadata {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]CertificateMetadata, 0, len(s.index))
	for _, k := range slices.Sorted(maps.Keys(s.index)) {
		out = append(out, s.index[k].CertificateMetadata)
	}
	return out
}

// lookup is #certificateIndex.get with the DER.
func (s *CertificateStore) lookup(skid string) (storedCertificate, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.index[skid]
	return e, ok
}

// PAA implements attestation.PAAStore: the certificate whose key
// identifier is skid, test certificates considered — the validator's
// getCertificate and getCertificateAsDer with considerTestCertificates
// (DeviceAttestationValidator.ts:235, :269), the self-heal of
// [CertificateStore.CertificateDER] included.
func (s *CertificateStore) PAA(skid []byte) (attestation.PAA, bool) {
	id := hexUpper(skid)
	der, err := s.CertificateDER(context.Background(), id, GetCertificateOptions{ConsiderTestCertificates: &considerAll})
	if err != nil {
		return attestation.PAA{}, false
	}
	e, _ := s.lookup(id)
	return attestation.PAA{DER: der, Production: e.IsProduction, FetchedAt: e.FetchedAt}, true
}

// considerAll is the validator's considerTestCertificates: true.
var considerAll = true

// ErrCertificateNotFound is matter.js's MatterDclError "Certificate not
// found" (DclCertificateService.ts:604-610 at 862ad75f): no certificate
// with that key identifier, or one the trust policy filters out.
var ErrCertificateNotFound = errors.New("dcl: certificate not found")

// GetCertificateOptions is DclCertificateService.GetCertificateOptions
// (DclCertificateService.ts:2026-2032 at 862ad75f).
type GetCertificateOptions struct {
	// ConsiderTestCertificates also returns test PAAs; nil is
	// [CertificateStore.AcceptsTestCertificates].
	ConsiderTestCertificates *bool
}

// considers is #isRelevant's considerTestCertificates — mirrors matter.js
// packages/protocol/src/dcl/DclCertificateService.ts:387-394.
func (s *CertificateStore) considers(o GetCertificateOptions) bool {
	if o.ConsiderTestCertificates != nil {
		return *o.ConsiderTestCertificates
	}
	return s.AcceptsTestCertificates()
}

// Certificate is a stored certificate's metadata by subject key
// identifier — hex, colons allowed, any case — unless the trust policy
// filters it out. Mirrors matter.js packages/protocol/src/dcl/
// DclCertificateService.ts:400-407 (getCertificate) with :375-380
// (#normalizeSubjectKeyId).
func (s *CertificateStore) Certificate(skid string, o GetCertificateOptions) (CertificateMetadata, bool) {
	e, ok := s.lookup(normalizeKeyID(skid))
	if !ok || !s.isRelevant(e.CertificateMetadata, s.considers(o)) {
		return CertificateMetadata{}, false
	}
	return e.CertificateMetadata, true
}

// storedDER is #getCertificateDer — mirrors matter.js packages/protocol/
// src/dcl/DclCertificateService.ts:604-617.
func (s *CertificateStore) storedDER(id string, o GetCertificateOptions) (storedCertificate, error) {
	e, ok := s.lookup(id)
	if !ok || !s.isRelevant(e.CertificateMetadata, s.considers(o)) {
		return storedCertificate{}, fmt.Errorf("%w: %s", ErrCertificateNotFound, id)
	}
	if len(e.DER) == 0 {
		return storedCertificate{}, fmt.Errorf("dcl: certificate data of %s not found in storage", id)
	}
	return e, nil
}

// CertificatePEM is the certificate as PEM text, [ErrCertificateNotFound]
// when the store holds none the trust policy lets through. Mirrors
// matter.js packages/protocol/src/dcl/DclCertificateService.ts:433-436
// (getCertificateAsPem).
func (s *CertificateStore) CertificatePEM(skid string, o GetCertificateOptions) (string, error) {
	e, err := s.storedDER(normalizeKeyID(skid), o)
	if err != nil {
		return "", err
	}
	return pemEncode(e.DER), nil
}

// CertificateDER is the certificate's DER, [ErrCertificateNotFound] as
// [CertificateStore.CertificatePEM]. A stored certificate that no longer
// parses is re-fetched once from the ledger; if that fails the stored
// bytes are returned for the caller's validation to reject. Mirrors
// matter.js packages/protocol/src/dcl/DclCertificateService.ts:449-480
// (getCertificateAsDer).
func (s *CertificateStore) CertificateDER(ctx context.Context, skid string, o GetCertificateOptions) ([]byte, error) {
	id := normalizeKeyID(skid)
	e, err := s.storedDER(id, o)
	if err != nil {
		return nil, err
	}
	if _, err := parseTrustStoreCertificate(e.DER, e.kind()); err != nil {
		s.log.Info("dcl.certificates.cached_unparseable", slog.String("skid", id), slog.Any("err", err),
			slog.String("message", "Cached certificate failed to parse; attempting one re-fetch from DCL to recover"))
		refetched, ok := s.tryRefetchRootCertificate(ctx, id, e.IsProduction, s.considers(o))
		if !ok {
			return e.DER, nil
		}
		if _, err := parseTrustStoreCertificate(refetched, e.kind()); err != nil {
			s.log.Warn("dcl.certificates.refetched_unparseable", slog.String("skid", id), slog.Any("err", err),
				slog.String("message", "Re-fetched certificate also failed to parse"))
			return e.DER, nil
		}
		return refetched, nil
	}
	return e.DER, nil
}

// GetOrFetchCertificateOptions is getOrFetchCertificate's options
// (DclCertificateService.ts:749 at 862ad75f).
type GetOrFetchCertificateOptions struct {
	GetCertificateOptions
	// IsProduction asks the production ledger, false the test ledger; nil
	// is the production one.
	IsProduction *bool
}

// GetOrFetchCertificate is a certificate's metadata, fetched from the
// ledger's roots and stored when the store lacks it and is not offline;
// false when neither has it, the trust policy filters it out or the ledger
// fails. Mirrors matter.js packages/protocol/src/dcl/
// DclCertificateService.ts:747-792 (getOrFetchCertificate).
func (s *CertificateStore) GetOrFetchCertificate(ctx context.Context, skid string, o GetOrFetchCertificateOptions) (CertificateMetadata, bool) {
	id := normalizeKeyID(skid)
	if m, ok := s.Certificate(id, o.GetCertificateOptions); ok {
		return m, true
	}
	if s.offline() {
		return CertificateMetadata{}, false
	}
	if !s.fetchMu.TryLock() {
		// An update is running: its outcome is the answer (:764-768 at
		// 862ad75f).
		s.awaitUpdate()
		return s.Certificate(id, o.GetCertificateOptions)
	}
	defer s.fetchMu.Unlock()
	isProduction := o.IsProduction == nil || *o.IsProduction
	stored, err := s.fetchAndStoreRootCertificateBySkid(ctx, id, isProduction, false)
	if err != nil {
		s.log.Debug("dcl.certificates.fetch_failed", slog.String("skid", id), slog.Any("err", err))
		return CertificateMetadata{}, false
	}
	if !stored {
		s.log.Debug("dcl.certificates.not_in_dcl", slog.String("skid", id), slog.Bool("production", isProduction))
		return CertificateMetadata{}, false
	}
	return s.Certificate(id, o.GetCertificateOptions)
}

// DeleteCertificate removes a certificate from the store; one it does not
// hold is no error. Mirrors matter.js packages/protocol/src/dcl/
// DclCertificateService.ts:797-812 (deleteCertificate).
func (s *CertificateStore) DeleteCertificate(ctx context.Context, skid string) {
	id := normalizeKeyID(skid)
	s.mu.Lock()
	delete(s.index, id)
	s.mu.Unlock()
	s.saveIndex(ctx)
	s.log.Debug("dcl.certificates.deleted", slog.String("skid", id))
}

// pemEncode is Pem.encode for a certificate: base64 in lines of 64, no
// trailing newline. Mirrors matter.js packages/general/src/crypto/
// Pem.ts:18-33.
func pemEncode(der []byte) string {
	b := base64.StdEncoding.EncodeToString(der)
	var lines []string
	for len(b) > 64 {
		lines = append(lines, b[:64])
		b = b[64:]
	}
	lines = append(lines, b)
	return "-----BEGIN CERTIFICATE-----\n" + strings.Join(lines, "\n") + "\n-----END CERTIFICATE-----"
}

// tryRefetchRootCertificate is #tryRefetchRootCertificate (:523-555):
// best effort, never an error; the re-fetched entry is filtered as the
// lookup was (:556-559 at 862ad75f).
func (s *CertificateStore) tryRefetchRootCertificate(ctx context.Context, id string, isProduction, considerTest bool) ([]byte, bool) {
	if s.offline() {
		return nil, false
	}
	s.fetchMu.Lock()
	defer s.fetchMu.Unlock()
	stored, err := s.fetchAndStoreRootCertificateBySkid(ctx, id, isProduction, true)
	if err != nil {
		s.log.Warn("dcl.certificates.refetch_failed", slog.String("skid", id), slog.Any("err", err))
		return nil, false
	}
	if !stored {
		return nil, false
	}
	e, ok := s.lookup(id)
	if !ok || !s.isRelevant(e.CertificateMetadata, considerTest) || len(e.DER) == 0 {
		return nil, false
	}
	return e.DER, true
}

// fetchAndStoreRootCertificateBySkid is :562-593.
func (s *CertificateStore) fetchAndStoreRootCertificateBySkid(ctx context.Context, id string, isProduction, force bool) (bool, error) {
	client := s.clientFor(isProduction)
	refs, err := client.RootCertificates(ctx)
	if err != nil {
		return false, err
	}
	withColons := strings.ToUpper(strings.Join(hexPairs.FindAllString(id, -1), ":"))
	i := slices.IndexFunc(refs, func(r SubjectReference) bool { return r.SubjectKeyID == withColons })
	if i < 0 {
		return false, nil
	}
	stored := s.fetchAndStoreCertificate(ctx, client, refs[i], isProduction, force)
	s.saveIndex(ctx)
	return stored, nil
}

// clientFor is the ledger of a production or test certificate.
func (s *CertificateStore) clientFor(isProduction bool) *Client {
	if isProduction || s.opts.TestClient != nil {
		if isProduction {
			return s.opts.Client
		}
		return s.opts.TestClient
	}
	c := *s.opts.Client
	c.BaseURL = TestNetURL
	return &c
}

// CDSigner implements attestation.PAAStore — getOrFetchCdSigner
// (:851-891): a stored CD signer, else the production ledger's
// certificate with that key identifier, stored as a production CD signer.
func (s *CertificateStore) CDSigner(skid []byte) (*ecdsa.PublicKey, bool) {
	id := hexUpper(skid)
	if e, ok := s.lookup(id); ok && e.kind() == KindCDSigner {
		c, err := parseCert(e.DER, false)
		if err != nil {
			s.log.Info("dcl.certificates.cd_signer_unparseable", slog.String("skid", id), slog.Any("err", err))
			return nil, false
		}
		k, err := attestation.P256Key(c.x509)
		return k, err == nil
	}
	if s.offline() {
		return nil, false
	}
	key, err := s.fetchCDSigner(context.Background(), id)
	if err != nil {
		s.log.Info("dcl.certificates.cd_signer_fetch_failed", slog.String("skid", id), slog.Any("err", err),
			slog.String("message", "Failed to fetch CD signer "+id+" from DCL"))
		return nil, false
	}
	return key, key != nil
}

// fetchCDSigner is the DCL fallback of getOrFetchCdSigner (:870-890).
func (s *CertificateStore) fetchCDSigner(ctx context.Context, id string) (*ecdsa.PublicKey, error) {
	certs, err := s.opts.Client.CertificatesBySubjectKeyID(ctx, id)
	if err != nil {
		return nil, err
	}
	if len(certs) == 0 {
		return nil, nil
	}
	der, err := pemToDER(certs[0].PEMCert)
	if err != nil {
		return nil, err
	}
	if _, err := s.AddCertificate(ctx, der, KindCDSigner, true); err != nil {
		return nil, err
	}
	c, err := parseCert(der, false)
	if err != nil {
		return nil, err
	}
	return attestation.P256Key(c.x509)
}

// AddCertificate puts a PAA or CD signer into the trust store — matter.js
// addCertificate (:810-840). It reports false when a certificate with the
// same key identifier is stored already (the store is then unchanged).
func (s *CertificateStore) AddCertificate(ctx context.Context, der []byte, kind Kind, production bool) (bool, error) {
	c, err := parseTrustStoreCertificate(der, kind)
	if err != nil {
		return false, err
	}
	id := hexUpper(c.x509.SubjectKeyId)
	if _, ok := s.lookup(id); ok {
		return false, nil
	}
	s.storeCertificate(id, der, CertificateMetadata{
		Subject: c.x509.Subject.CommonName, SubjectKeyID: id, SerialNumber: hex.EncodeToString(serialContent(c.x509.SerialNumber)),
		VID: vidOrZero(c), IsRoot: kind == KindPAA, IsProduction: production, Kind: kind,
	})
	s.saveIndex(ctx)
	s.log.Info("dcl.certificates.added", slog.String("kind", string(kind)), slog.String("skid", id), slog.Bool("production", production))
	return true, nil
}

func vidOrZero(c *trustCert) uint16 {
	if c.vendorID == nil {
		return 0
	}
	return *c.vendorID
}

// loadIndex is #loadIndex (:911-947): every stored entry with its DER; an
// entry without one is dropped with a warning.
func (s *CertificateStore) loadIndex(ctx context.Context) error {
	data, err := s.store.Load(ctx)
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return nil
	}
	var idx storedIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return err
	}
	valid, invalid := 0, 0
	s.mu.Lock()
	for i := range idx.Certificates {
		e := &idx.Certificates[i]
		if len(e.DER) == 0 || e.SubjectKeyID == "" {
			s.log.Warn("dcl.certificates.missing_in_storage", slog.String("skid", e.SubjectKeyID),
				slog.String("message", "Certificate referenced in index but not found in storage"))
			invalid++
			continue
		}
		s.index[e.SubjectKeyID] = *e
		valid++
	}
	s.mu.Unlock()
	s.log.Info("dcl.certificates.loaded", slog.Int("count", valid), slog.Int("missing", invalid))
	return nil
}

// saveIndex is #saveIndex (:1046-1055); a failure is logged.
func (s *CertificateStore) saveIndex(ctx context.Context) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	idx := storedIndex{Certificates: make([]storedCertificate, 0, len(s.index))}
	for _, k := range slices.Sorted(maps.Keys(s.index)) {
		idx.Certificates = append(idx.Certificates, s.index[k])
	}
	s.mu.Unlock()
	data, err := json.Marshal(idx)
	if err == nil {
		err = s.store.Save(ctx, data)
	}
	if err != nil {
		s.log.Warn("dcl.certificates.save_failed", slog.Any("err", err))
	}
}

// storeCertificate is #storeCertificate (:1328-1350): never downgrade a
// production certificate to test, keep the first FetchedAt.
func (s *CertificateStore) storeCertificate(id string, der []byte, m CertificateMetadata) {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.index[id]
	if ok && existing.IsProduction && !m.IsProduction {
		m.IsProduction = true
	}
	if m.FetchedAt.IsZero() {
		if ok && !existing.FetchedAt.IsZero() {
			m.FetchedAt = existing.FetchedAt
		} else {
			m.FetchedAt = s.clock.Now().UTC().Truncate(time.Millisecond)
		}
	}
	s.index[id] = storedCertificate{CertificateMetadata: m, DER: bytes.Clone(der)}
}

// Update fetches the ledger's roots (and, with the options, the test
// ledger's and GitHub's) — update (:952-972). One update runs at a time;
// an offline or closed store does nothing. It reports false when the
// update failed (the failure is logged).
func (s *CertificateStore) Update(ctx context.Context, force bool) bool {
	if s.isClosed() || s.offline() {
		return true
	}
	s.fetchMu.Lock()
	defer s.fetchMu.Unlock()
	s.log.Debug("dcl.certificates.update", slog.Bool("force", force))
	if err := s.fetchCertificates(ctx, force); err != nil {
		s.log.Info("dcl.certificates.update_failed", slog.Any("err", err), slog.String("message", "Certificate update failed"))
		return false
	}
	return true
}

// fetchCertificates is #fetchCertificates (:977-1023).
func (s *CertificateStore) fetchCertificates(ctx context.Context, force bool) error {
	if s.isClosed() {
		return nil
	}
	s.mu.Lock()
	initial := len(s.index)
	s.githubRateLimited = false
	s.mu.Unlock()
	if err := s.fetchDclCertificates(ctx, true, force); err != nil {
		return err
	}
	if s.isClosed() {
		return nil
	}
	if s.opts.FetchTestCertificates {
		if err := s.fetchDclCertificates(ctx, false, force); err != nil {
			return err
		}
		if s.isClosed() {
			return nil
		}
		if s.github != nil {
			s.fetchGitHubCertificates(ctx, force)
			if s.isClosed() {
				return nil
			}
			s.fetchGitHubCdSignerCertificates(ctx, force)
		}
	}
	if s.isClosed() {
		return nil
	}
	s.saveIndex(ctx)
	s.mu.Lock()
	total := len(s.index)
	s.mu.Unlock()
	s.log.Info("dcl.certificates.updated", slog.Int("new", total-initial), slog.Int("total", total))
	return nil
}

// fetchDclCertificates is #fetchDclCertificates (:1026-1043).
func (s *CertificateStore) fetchDclCertificates(ctx context.Context, isProduction, force bool) error {
	client := s.clientFor(isProduction)
	refs, err := client.RootCertificates(ctx)
	if err != nil {
		return err
	}
	s.log.Debug("dcl.certificates.roots", slog.Bool("production", isProduction), slog.Int("count", len(refs)))
	for _, ref := range refs {
		if s.isClosed() {
			return nil
		}
		s.fetchAndStoreCertificate(ctx, client, ref, isProduction, force)
	}
	return nil
}

// fetchAndStoreCertificate is #fetchAndStoreCertificate (:1061-1148): one
// root's certificates, each stored under its own key identifier once its
// DER parses as a PAA and carries the key identifier the ledger states.
// It reports whether the store holds a usable certificate for ref.
func (s *CertificateStore) fetchAndStoreCertificate(ctx context.Context, client *Client, ref SubjectReference, isProduction, force bool) bool {
	refID := normalizeKeyID(ref.SubjectKeyID)
	if !force {
		s.mu.Lock()
		existing, ok := s.index[refID]
		if ok {
			// A test certificate found in the production ledger is upgraded
			// (:1075-1082).
			if isProduction && !existing.IsProduction {
				existing.IsProduction = true
				s.index[refID] = existing
			}
			s.mu.Unlock()
			return true
		}
		s.mu.Unlock()
	}
	certs, err := client.RootCertificateBySubject(ctx, ref)
	if err != nil {
		s.log.Info("dcl.certificates.fetch_failed", slog.String("subject", ref.Subject), slog.String("skid", ref.SubjectKeyID), slog.Any("err", err))
		return false
	}
	stored := false
	for i := range certs {
		cert := &certs[i]
		if cert.SubjectKeyID == "" {
			s.log.Warn("dcl.certificates.missing_skid", slog.String("subject", cert.Subject))
			continue
		}
		id := normalizeKeyID(cert.SubjectKeyID)
		der, err := pemToDER(cert.PEMCert)
		if err != nil {
			s.log.Info("dcl.certificates.unusable", slog.String("skid", id), slog.Any("err", err),
				slog.String("message", "DCL: certificate cannot be used as a PAA, skipped"))
			continue
		}
		if !s.parseAndCheckSkid(der, KindPAA, id, "DCL") {
			continue
		}
		s.storeCertificate(id, der, CertificateMetadata{
			Subject: cert.Subject, SubjectAsText: cert.SubjectAsText, SubjectKeyID: id, SerialNumber: cert.SerialNumber,
			VID: cert.VID, IsRoot: cert.IsRoot, IsProduction: isProduction, Kind: KindPAA,
		})
		stored = stored || id == refID
	}
	return stored
}

// parseAndCheckSkid is #parseAndCheckSkid (:492-515): the DER parses as
// kind and its own key identifier is the one stated for it.
func (s *CertificateStore) parseAndCheckSkid(der []byte, kind Kind, statedSkid, source string) bool {
	c, err := parseTrustStoreCertificate(der, kind)
	if err != nil {
		s.log.Info("dcl.certificates.unusable", slog.String("source", source), slog.String("kind", string(kind)),
			slog.String("skid", statedSkid), slog.Any("err", err))
		return false
	}
	if derived := hexUpper(c.x509.SubjectKeyId); derived != statedSkid {
		s.log.Warn("dcl.certificates.skid_mismatch", slog.String("source", source), slog.String("kind", string(kind)),
			slog.String("message", fmt.Sprintf("%s: SKID mismatch entry=%s der-derived=%s, skipped", source, statedSkid, derived)))
		return false
	}
	return true
}

// hasTestCertificatesOfKind is #hasTestCertificatesOfKind (:1150-1157).
func (s *CertificateStore) hasTestCertificatesOfKind(kind Kind) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k := range s.index {
		if e := s.index[k]; !e.IsProduction && e.kind() == kind {
			return true
		}
	}
	return false
}

// logGitHubFetchFailure is #logGithubFetchFailure (:1165-1172): a rate
// limit flags the rest of the update to skip GitHub and is debug-worthy
// only when test certificates of the kind are held already.
func (s *CertificateStore) logGitHubFetchFailure(ctx context.Context, message string, err error, kind Kind) {
	var he *gitHubHTTPError
	rateLimited := errors.As(err, &he) && he.isRateLimit()
	if rateLimited {
		s.mu.Lock()
		s.githubRateLimited = true
		s.mu.Unlock()
	}
	level := slog.LevelInfo
	if rateLimited && s.hasTestCertificatesOfKind(kind) {
		level = slog.LevelDebug
	}
	s.log.Log(ctx, level, "dcl.certificates.github_failed", slog.String("message", message), slog.Any("err", err))
}

func (s *CertificateStore) rateLimited() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.githubRateLimited
}

// fetchGitHubCertificates is #fetchGitHubCertificates (:1177-1206): the
// .der files of the PAA directory but the dcld_mirror_ ones, which the
// ledger serves itself.
func (s *CertificateStore) fetchGitHubCertificates(ctx context.Context, force bool) {
	if s.rateLimited() {
		return
	}
	dir, err := s.github.repo().cd(ctx, s.github.cfg.CertPath)
	var files []string
	if err == nil {
		files, err = dir.ls(ctx)
	}
	if err != nil {
		s.logGitHubFetchFailure(ctx, "Failed to fetch certificates from GitHub", err, KindPAA)
		return
	}
	for _, name := range files {
		if !strings.HasSuffix(name, ".der") || strings.HasPrefix(name, "dcld_mirror_") {
			continue
		}
		if s.isClosed() {
			return
		}
		s.fetchGitHubCertificate(ctx, dir, name, KindPAA, force)
	}
}

// fetchGitHubCdSignerCertificates is #fetchGitHubCdSignerCertificates
// (:1212-1237): every .der file of the CD signer directory.
func (s *CertificateStore) fetchGitHubCdSignerCertificates(ctx context.Context, force bool) {
	if s.rateLimited() {
		return
	}
	dir, err := s.github.repo().cd(ctx, s.github.cfg.CDSignerCertPath)
	var files []string
	if err == nil {
		files, err = dir.ls(ctx)
	}
	if err != nil {
		s.logGitHubFetchFailure(ctx, "Failed to fetch CD signer certificates from GitHub", err, KindCDSigner)
		return
	}
	for _, name := range files {
		if !strings.HasSuffix(name, ".der") {
			continue
		}
		if s.isClosed() {
			return
		}
		s.fetchGitHubCertificate(ctx, dir, name, KindCDSigner, force)
	}
}

// fetchGitHubCertificate is #fetchGitHubCertificate and
// #fetchGitHubCdSignerCertificate (:1242-1323): stored as a test
// certificate under its own key identifier, named by its file.
func (s *CertificateStore) fetchGitHubCertificate(ctx context.Context, dir *githubDir, name string, kind Kind, force bool) {
	der, err := dir.getBinary(ctx, name)
	var c *trustCert
	if err == nil {
		c, err = parseTrustStoreCertificate(der, kind)
	}
	if err != nil {
		s.log.Info("dcl.certificates.github_certificate_failed", slog.String("file", name), slog.Any("err", err))
		return
	}
	id := hexUpper(c.x509.SubjectKeyId)
	if _, ok := s.lookup(id); ok && !force {
		return
	}
	s.storeCertificate(id, der, CertificateMetadata{
		Subject: strings.Replace(name, ".der", "", 1), SubjectKeyID: id, SerialNumber: hex.EncodeToString(serialContent(c.x509.SerialNumber)),
		VID: vidOrZero(c), IsRoot: kind == KindPAA, IsProduction: false, Kind: kind,
	})
}
