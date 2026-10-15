// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package dcl

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/controller/attestation"
)

func openStore(t *testing.T, opts CertificateOptions) *CertificateStore {
	t.Helper()
	s, err := OpenCertificateStore(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

// TestCertificateStoreRoundTrip: the production roots are fetched and
// stored, persisted through the Store and found by an offline store over
// the same Store; FetchedAt survives (:1340-1343).
func TestCertificateStoreRoundTrip(t *testing.T) {
	t.Parallel()
	f := newFakeDCL(t)
	pki := newTestPKI(t)
	f.addRoot(pki.paa)
	clock := newManualClock()
	file := FileStore{Path: filepath.Join(t.TempDir(), "dcl.json")}
	s := openStore(t, CertificateOptions{Client: f.client(), Store: file, Clock: clock})
	p, ok := s.PAA(pki.paa.skid())
	if !ok || !bytes.Equal(p.DER, pki.paa.der) || !p.Production || !p.FetchedAt.Equal(clock.Now()) {
		t.Fatalf("PAA %+v %v", p, ok)
	}
	if s.AcceptsTestCertificates() {
		t.Fatal("test certificates accepted by default")
	}
	if got := s.Certificates(); len(got) != 1 || got[0].SubjectKeyID != pki.paa.skidHex() || got[0].Kind != KindPAA || !got[0].IsRoot {
		t.Fatalf("Certificates %+v", got)
	}
	s.Close()

	// Offline: no Client, the stored roots only, nothing fetched.
	clock.advance(time.Hour)
	hits := f.hit("/dcl/pki/root-certificates")
	off := openStore(t, CertificateOptions{Store: file, Clock: clock})
	p2, ok := off.PAA(pki.paa.skid())
	if !ok || !p2.FetchedAt.Equal(p.FetchedAt) || f.hit("/dcl/pki/root-certificates") != hits {
		t.Fatalf("offline PAA %+v %v", p2, ok)
	}
	if _, ok := off.CDSigner(pki.pai.skid()); ok {
		t.Fatal("an offline store fetched a CD signer")
	}
	if revoked, err := off.IsRevoked(pki.pai.skid(), []byte{1}, ""); revoked || err != nil {
		t.Fatal("an offline store checked revocation")
	}
	if !off.Update(context.Background(), true) {
		t.Fatal("an offline update failed")
	}
}

// TestCertificateStoreDailyRefresh: the periodic update runs a day later
// (Days.one, :345-351) and stores a root added meanwhile; a negative
// interval schedules none; Close stops it.
func TestCertificateStoreDailyRefresh(t *testing.T) {
	t.Parallel()
	f := newFakeDCL(t)
	pki := newTestPKI(t)
	other := newTestPKI(t)
	f.addRoot(pki.paa)
	clock := newManualClock()
	s := openStore(t, CertificateOptions{Client: f.client(), Clock: clock})
	if _, ok := s.PAA(other.paa.skid()); ok {
		t.Fatal("a root before it was published")
	}
	f.addRoot(other.paa)
	clock.advance(DefaultUpdateInterval - time.Minute)
	if _, ok := s.PAA(other.paa.skid()); ok {
		t.Fatal("refreshed before a day")
	}
	clock.advance(time.Minute)
	if _, ok := s.PAA(other.paa.skid()); !ok {
		t.Fatal("not refreshed after a day")
	}
	if clock.pending() != 1 {
		t.Fatalf("%d timers after the refresh, want the next one", clock.pending())
	}
	s.Close()
	if clock.pending() != 0 {
		t.Fatal("Close left the timer")
	}

	never := newManualClock()
	openStore(t, CertificateOptions{Client: f.client(), Clock: never, UpdateInterval: -1})
	if never.pending() != 0 {
		t.Fatal("a negative interval scheduled an update")
	}
}

// TestCertificateStoreTestNet: test-net roots are fetched with
// FetchTestCertificates, stored as test, accepted by default then
// (acceptsTestCertificates :418-420), upgraded when the production ledger
// lists them too (:1075-1082) and never downgraded (:1334-1338).
func TestCertificateStoreTestNet(t *testing.T) {
	t.Parallel()
	prod := newFakeDCL(t)
	test := newFakeDCL(t)
	pki := newTestPKI(t)
	f := newFakeDCL(t) // empty production ledger
	f.json("/dcl/pki/root-certificates", 200, map[string]any{"approvedRootCertificates": map[string]any{"certs": []any{}, "schemaVersion": 0}})
	test.addRoot(pki.paa)
	clock := newManualClock()
	s := openStore(t, CertificateOptions{Client: f.client(), TestClient: test.client(), FetchTestCertificates: true, Clock: clock})
	p, ok := s.PAA(pki.paa.skid())
	if !ok || p.Production {
		t.Fatalf("test root %+v %v", p, ok)
	}
	if !s.AcceptsTestCertificates() {
		t.Fatal("FetchTestCertificates does not accept test certificates")
	}
	no := false
	if (&CertificateStore{opts: CertificateOptions{FetchTestCertificates: true, AcceptTestCertificates: &no}}).AcceptsTestCertificates() {
		t.Fatal("AcceptTestCertificates false ignored")
	}

	// The same root in the production ledger: upgraded.
	prod.addRoot(pki.paa)
	s2 := openStore(t, CertificateOptions{
		Client: prod.client(), TestClient: test.client(), FetchTestCertificates: true, Clock: clock,
		Store: storeOf(t, s),
	})
	if p, ok := s2.PAA(pki.paa.skid()); !ok || !p.Production {
		t.Fatalf("not upgraded %+v", p)
	}
	// A forced update from the test ledger keeps it production.
	s2.Update(context.Background(), true)
	if p, ok := s2.PAA(pki.paa.skid()); !ok || !p.Production {
		t.Fatalf("downgraded %+v", p)
	}

	// The production ledger unreachable: the update fails, logged.
	prod.mu.Lock()
	prod.failPath["/dcl/pki/root-certificates"] = 500
	prod.mu.Unlock()
	if s2.Update(context.Background(), false) {
		t.Fatal("a failed update reported success")
	}
}

// storeOf copies s's persisted trust store into a fresh MemoryStore.
func storeOf(t *testing.T, s *CertificateStore) (copied Store) {
	t.Helper()
	data, err := s.store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	m := &MemoryStore{}
	_ = m.Save(context.Background(), data)
	return m
}

// TestCertificateStoreRejectsBadRoots: a root whose DER carries another
// key identifier than the ledger states (:505-512), one that is not PEM
// and one that does not parse are skipped.
func TestCertificateStoreRejectsBadRoots(t *testing.T) {
	t.Parallel()
	f := newFakeDCL(t)
	pki := newTestPKI(t)
	other := newTestPKI(t)
	f.addRoot(pki.paa)
	// The ledger's record for pki.paa names other.paa's certificate.
	subject := f.roots[0].subject
	path := "/dcl/pki/certificates/" + encodeURIComponent(subject) + "/" + encodeURIComponent(colonHex(pki.paa.skid()))
	mismatch := certRecord(other.paa, subject, true)
	mismatch["subjectKeyId"] = colonHex(pki.paa.skid())
	notPEM := certRecord(pki.paa, subject, true)
	notPEM["pemCert"] = "garbage"
	notCert := certRecord(pki.paa, subject, true)
	notCert["pemCert"] = "-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----\n"
	noSKID := certRecord(pki.paa, subject, true)
	noSKID["subjectKeyId"] = ""
	f.json(path, 200, map[string]any{"approvedCertificates": map[string]any{
		"subject": subject, "subjectKeyId": colonHex(pki.paa.skid()), "schemaVersion": 0,
		"certs": []any{mismatch, notPEM, notCert, noSKID},
	}})
	s := openStore(t, CertificateOptions{Client: f.client(), Clock: newManualClock()})
	if len(s.Certificates()) != 0 {
		t.Fatalf("stored %+v", s.Certificates())
	}
}

// TestCertificateStoreCDSigner: a CD signer absent locally is fetched by
// key identifier from the production ledger and stored as a production
// CD signer (:870-883); the next lookup is local. A PAA is not a CD
// signer; a key identifier the ledger lacks is none.
func TestCertificateStoreCDSigner(t *testing.T) {
	t.Parallel()
	f := newFakeDCL(t)
	pki := newTestPKI(t)
	signer := mint(t, certSpec{cn: "CD signer", ca: false, usage: 1}, &pki.paa)
	f.addRoot(pki.paa)
	f.addBySKID(signer)
	s := openStore(t, CertificateOptions{Client: f.client(), Clock: newManualClock()})
	key, ok := s.CDSigner(signer.skid())
	if !ok || !key.Equal(signer.cert.PublicKey) {
		t.Fatal("CD signer not fetched")
	}
	path := "/dcl/pki/all-certificates?subjectKeyId=" + encodeURIComponent(colonHex(signer.skid()))
	if _, ok := s.CDSigner(signer.skid()); !ok || f.hit(path) != 1 {
		t.Fatalf("the second lookup asked the ledger (%d)", f.hit(path))
	}
	var stored *CertificateMetadata
	for _, m := range s.Certificates() {
		if m.SubjectKeyID == signer.skidHex() {
			stored = &m
		}
	}
	if stored == nil || stored.Kind != KindCDSigner || !stored.IsProduction || stored.IsRoot {
		t.Fatalf("stored %+v", stored)
	}
	// The PAA is in the store but is no CD signer: the ledger is asked.
	if _, ok := s.CDSigner(pki.paa.skid()); ok {
		t.Fatal("a PAA served as a CD signer")
	}
	if _, ok := s.CDSigner([]byte{1, 2}); ok {
		t.Fatal("an unknown CD signer was found")
	}
	// AddCertificate refuses a duplicate.
	if added, err := s.AddCertificate(context.Background(), signer.der, KindCDSigner, true); added || err != nil {
		t.Fatalf("duplicate added %v %v", added, err)
	}
	if _, err := s.AddCertificate(context.Background(), []byte{1}, KindPAA, false); err == nil {
		t.Fatal("garbage added")
	}
	// The PAA lookup returns a CD signer too, as getCertificate does
	// (DeviceAttestationValidator.ts:235): the validator's parse then
	// rejects it.
	if p, ok := s.PAA(signer.skid()); !ok || !p.Production {
		t.Fatal("PAA lookup of a CD signer")
	}
	var _ attestation.PAAStore = s
}

// TestCertificateStoreSelfHeal: a stored root that no longer parses is
// re-fetched once (getCertificateAsDer :442-473).
func TestCertificateStoreSelfHeal(t *testing.T) {
	t.Parallel()
	f := newFakeDCL(t)
	pki := newTestPKI(t)
	f.addRoot(pki.paa)
	mem := &MemoryStore{}
	// A stored index whose DER is truncated.
	broken, _ := json.Marshal(storedIndex{Certificates: []storedCertificate{{
		CertificateMetadata: CertificateMetadata{SubjectKeyID: pki.paa.skidHex(), IsProduction: true, Kind: KindPAA, IsRoot: true},
		DER:                 pki.paa.der[:40],
	}, {CertificateMetadata: CertificateMetadata{SubjectKeyID: "AA"}}}})
	_ = mem.Save(context.Background(), broken)
	s := openStore(t, CertificateOptions{Client: f.client(), Store: mem, Clock: newManualClock(), UpdateInterval: -1})
	// The first update skipped the stored root (already present).
	p, ok := s.PAA(pki.paa.skid())
	if !ok || !bytes.Equal(p.DER, pki.paa.der) {
		t.Fatalf("not healed: %v", ok)
	}
	// The ledger without it: the broken bytes come back for the validator.
	_ = mem.Save(context.Background(), broken)
	empty := newFakeDCL(t)
	empty.json("/dcl/pki/root-certificates", 200, map[string]any{"approvedRootCertificates": map[string]any{"certs": []any{}, "schemaVersion": 0}})
	s2 := openStore(t, CertificateOptions{Client: empty.client(), Store: mem, Clock: newManualClock(), UpdateInterval: -1})
	if p, ok := s2.PAA(pki.paa.skid()); !ok || len(p.DER) != 40 {
		t.Fatal("the broken root vanished")
	}
	// A store whose data is not JSON fails to open.
	bad := &MemoryStore{}
	_ = bad.Save(context.Background(), []byte("{"))
	if _, err := OpenCertificateStore(context.Background(), CertificateOptions{Store: bad}); err == nil {
		t.Fatal("a corrupt store opened")
	}
}

// TestCertificateStoreGitHub: the development-certificate fallback is off
// by default — GitHub is never asked — and, turned on with
// FetchTestCertificates, stores the PAAs (but the dcld_mirror_ files)
// and CD signers of the repository as test certificates (:1177-1323); a
// rate-limited GitHub skips the rest of the update (:1165-1172).
func TestCertificateStoreGitHub(t *testing.T) {
	t.Parallel()
	f := newFakeDCL(t)
	gh := newFakeDCL(t)
	pki := newTestPKI(t)
	mirror := newTestPKI(t)
	signer := mint(t, certSpec{cn: "CD signer", usage: 1}, &pki.paa)
	f.json("/dcl/pki/root-certificates", 200, map[string]any{"approvedRootCertificates": map[string]any{"certs": []any{}, "schemaVersion": 0}})
	tree := func(path string, entries ...map[string]any) {
		gh.json(path, 200, map[string]any{"sha": "x", "tree": entries})
	}
	entry := func(name, typ, path string) map[string]any {
		return map[string]any{"path": name, "type": typ, "url": gh.url(path)}
	}
	tree("/repos/project-chip/connectedhomeip/git/trees/master", entry("credentials", "tree", "/t/credentials"))
	tree("/t/credentials", entry("development", "tree", "/t/dev"))
	tree("/t/dev", entry("paa-root-certs", "tree", "/t/paa"), entry("cd-certs", "tree", "/t/cd"))
	tree("/t/paa", entry("Chip-Test-PAA.der", "blob", "/b/paa"), entry("dcld_mirror_x.der", "blob", "/b/mirror"), entry("README.md", "blob", "/b/readme"))
	tree("/t/cd", entry("Chip-Test-CD-Signing-Cert.der", "blob", "/b/cd"))
	gh.bytes("/b/paa", pki.paa.der)
	gh.bytes("/b/mirror", mirror.paa.der)
	gh.bytes("/b/cd", signer.der)

	cfg := GitHubConfig{APIURL: gh.srv.URL, Timeout: 5 * time.Second}
	off := openStore(t, CertificateOptions{Client: f.client(), TestClient: f.client(), FetchTestCertificates: true, GitHub: cfg, Clock: newManualClock()})
	if len(off.Certificates()) != 0 || gh.hit("/repos/project-chip/connectedhomeip/git/trees/master") != 0 {
		t.Fatal("GitHub asked without FetchGitHubCertificates")
	}

	on := openStore(t, CertificateOptions{Client: f.client(), TestClient: f.client(), FetchTestCertificates: true, FetchGitHubCertificates: true, GitHub: cfg, Clock: newManualClock()})
	if p, ok := on.PAA(pki.paa.skid()); !ok || p.Production {
		t.Fatalf("GitHub PAA %+v %v", p, ok)
	}
	if _, ok := on.PAA(mirror.paa.skid()); ok {
		t.Fatal("a dcld_mirror_ file was stored")
	}
	if k, ok := on.CDSigner(signer.skid()); !ok || !k.Equal(signer.cert.PublicKey) {
		t.Fatal("GitHub CD signer missing")
	}
	for _, m := range on.Certificates() {
		if m.SubjectKeyID == signer.skidHex() && (m.Kind != KindCDSigner || m.IsProduction || m.Subject != "Chip-Test-CD-Signing-Cert") {
			t.Fatalf("CD signer metadata %+v", m)
		}
	}

	// Rate limited: the PAA directory fails 403, the CD signer one is
	// skipped in the same update.
	gh2 := newFakeDCL(t)
	gh2.route("/repos/project-chip/connectedhomeip/git/trees/master", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) })
	openStore(t, CertificateOptions{
		Client: f.client(), TestClient: f.client(), FetchTestCertificates: true, FetchGitHubCertificates: true,
		GitHub: GitHubConfig{APIURL: gh2.srv.URL}, Clock: newManualClock(),
	})
	if n := gh2.hit("/repos/project-chip/connectedhomeip/git/trees/master"); n != 1 {
		t.Fatalf("GitHub asked %d times while rate limited", n)
	}
	if (&gitHubHTTPError{status: 429}).isRateLimit() != true || (&gitHubHTTPError{status: 500}).isRateLimit() {
		t.Fatal("isRateLimit")
	}
}

// TestStores: the FileStore and SettingStore round trips.
func TestStores(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fs := FileStore{Path: filepath.Join(t.TempDir(), "x.json")}
	if b, err := fs.Load(ctx); b != nil || err != nil {
		t.Fatalf("empty file store %q %v", b, err)
	}
	if err := fs.Save(ctx, []byte("abc")); err != nil {
		t.Fatal(err)
	}
	if b, _ := fs.Load(ctx); string(b) != "abc" {
		t.Fatalf("file store %q", b)
	}
	if err := (FileStore{Path: filepath.Join(t.TempDir(), "no", "dir")}).Save(ctx, nil); err == nil {
		t.Fatal("saved into a missing directory")
	}
	ss := SettingStore{Settings: &mapSettings{m: map[string]string{}}, Key: "dcl"}
	if b, err := ss.Load(ctx); b != nil || err != nil {
		t.Fatal("empty setting store")
	}
	_ = ss.Save(ctx, []byte("xyz"))
	if b, _ := ss.Load(ctx); string(b) != "xyz" {
		t.Fatalf("setting store %q", b)
	}
}

type mapSettings struct{ m map[string]string }

func (s *mapSettings) GetSetting(_ context.Context, k string) (value string, ok bool, err error) {
	value, ok = s.m[k]
	return value, ok, nil
}

func (s *mapSettings) SetSetting(_ context.Context, k, v string) error { s.m[k] = v; return nil }
