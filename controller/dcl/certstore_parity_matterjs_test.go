// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package dcl

// A port of matter.js packages/protocol/test/dcl/DclCertificateServiceTest.ts at
// 862ad75f: the DCL trust store's fetch, persistence, lookups, deletion,
// updates, test-certificate policy, CD signers and revocation checks.

import (
	"bytes"
	"context"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	secattestation "github.com/SukramJ/go-fabric/secure/attestation"
)

// The CHIP test PAAs and their ledger records (DclCertificateServiceTest.ts:28-89).
const (
	dclCertSvcNoVIDSKID      = "785CE705B86B8F4E6FC793AA60CB43EA696882D5"
	dclCertSvcFFF1SKID       = "6AFD22771F511FECBF1641976710DCDC31A1717E"
	dclCertSvcNoVIDColons    = "78:5C:E7:05:B8:6B:8F:4E:6F:C7:93:AA:60:CB:43:EA:69:68:82:D5"
	dclCertSvcFFF1Colons     = "6A:FD:22:77:1F:51:1F:EC:BF:16:41:97:67:10:DC:DC:31:A1:71:7E"
	dclCertSvcNoVIDSubject   = "MDAxGDAWBgNVBAMMD01hdHRlciBUZXN0IFBBQQ=="
	dclCertSvcFFF1Subject    = "MDAEFjAUBgorBgEEAYKefAIBDARGRkYx"
	dclCertSvcNoVIDPath      = "/dcl/pki/certificates/MDAxGDAWBgNVBAMMD01hdHRlciBUZXN0IFBBQQ%3D%3D/78%3A5C%3AE7%3A05%3AB8%3A6B%3A8F%3A4E%3A6F%3AC7%3A93%3AAA%3A60%3ACB%3A43%3AEA%3A69%3A68%3A82%3AD5"
	dclCertSvcFFF1Path       = "/dcl/pki/certificates/MDAEFjAUBgorBgEEAYKefAIBDARGRkYx/6A%3AFD%3A22%3A77%3A1F%3A51%3A1F%3AEC%3ABF%3A16%3A41%3A97%3A67%3A10%3ADC%3ADC%3A31%3AA1%3A71%3A7E"
	dclCertSvcProdHost       = "on.dcl.csa-iot.org"
	dclCertSvcTestHost       = "on.test-net.dcl.csa-iot.org"
	dclCertSvcRootListPath   = "/dcl/pki/root-certificates"
	dclCertSvcGitHubPAADir   = "api.github.com/repos/project-chip/connectedhomeip/contents/credentials/development/paa-root-certs"
	dclCertSvcGitHubRawNoVID = "raw.githubusercontent.com/project-chip/connectedhomeip/master/credentials/development/paa-root-certs/Chip-Test-PAA-NoVID-Cert.der"
	dclCertSvcGitHubRawFFF1  = "raw.githubusercontent.com/project-chip/connectedhomeip/master/credentials/development/paa-root-certs/Chip-Test-PAA-FFF1-Cert.der"
)

// dclCertSvcCDSigner is TestCMS_SignerCertificate (CertificationDeclaration.ts:50-52).
var dclCertSvcCDSigner, _ = hex.DecodeString("308201b33082015aa003020102020845daf39de47aa08f300a06082a8648ce3d040302302b3129302706035504030c204d61747465722054657374204344205369676e696e6720417574686f726974793020170d3231303632383134323334335a180f39393939313233313233353935395a302b3129302706035504030c204d61747465722054657374204344205369676e696e6720417574686f726974793059301306072a8648ce3d020106082a8648ce3d030107034200043c398922452b55caf389c25bd1bca4656952ccb90e8869249ad8474653014cbf95d687965e036b521c51037e6b8cedefca1eb44046694fa08882eed6519decbaa366306430120603551d130101ff040830060101ff020101300e0603551d0f0101ff040403020106301d0603551d0e0416041462fa823359acfaa9963e1cfa140addf504f37160301f0603551d2304183016801462fa823359acfaa9963e1cfa140addf504f37160300a06082a8648ce3d040302034700304402202c545ce4e457d8a6f0d9d9bbebd6ece1ddfe7f8c6d9a6cf375321fc6fac713840220540778e8743972527eedebaf58686220b54078f2cd4e62a76ae7cbb92ff54c8b")

func dclCertSvcPEM(der []byte) string {
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

func dclCertSvcRootList(refs ...[2]string) map[string]any {
	certs := make([]any, 0, len(refs))
	for _, r := range refs {
		certs = append(certs, map[string]any{"subject": r[0], "subjectKeyId": r[1]})
	}
	return map[string]any{"approvedRootCertificates": map[string]any{"schemaVersion": 0, "certs": certs}}
}

// dclCertSvcMockList is mockDclRootCertificateList (:29-43).
func dclCertSvcMockList() map[string]any {
	return dclCertSvcRootList([2]string{dclCertSvcNoVIDSubject, dclCertSvcNoVIDColons}, [2]string{dclCertSvcFFF1Subject, dclCertSvcFFF1Colons})
}

func dclCertSvcRecord(der []byte, serial, subject, text, skid string, vid uint16) map[string]any {
	return map[string]any{
		"pemCert": dclCertSvcPEM(der), "serialNumber": serial, "subject": subject, "subjectAsText": text,
		"subjectKeyId": skid, "isRoot": true, "owner": "cosmos1...", "approvals": []any{}, "rejects": []any{},
		"vid": vid, "schemaVersion": 0,
	}
}

func dclCertSvcApproved(subject, skid string, certs ...map[string]any) map[string]any {
	list := make([]any, 0, len(certs))
	for _, c := range certs {
		list = append(list, c)
	}
	return map[string]any{"approvedCertificates": map[string]any{"subject": subject, "subjectKeyId": skid, "schemaVersion": 0, "certs": list}}
}

// dclCertSvcNoVIDRecord and dclCertSvcFFF1Record are the records of
// mockDclCertificateNoVID (:45-65) and mockDclCertificateFFF1 (:67-87).
func dclCertSvcNoVIDRecord() map[string]any {
	return dclCertSvcRecord(secattestation.TestPAANoVIDCert, "0B8FBAA8DD86EE", dclCertSvcNoVIDSubject, "CN=Matter Test PAA", dclCertSvcNoVIDColons, 0)
}

func dclCertSvcFFF1Record() map[string]any {
	return dclCertSvcRecord(secattestation.TestPAAFFF1Cert, "4EA8E83182D41C1C", dclCertSvcFFF1Subject, "CN=Matter Test PAA, VID=FFF1", dclCertSvcFFF1Colons, 0xfff1)
}

func dclCertSvcNoVID() map[string]any {
	return dclCertSvcApproved(dclCertSvcNoVIDSubject, dclCertSvcNoVIDColons, dclCertSvcNoVIDRecord())
}

func dclCertSvcFFF1() map[string]any {
	return dclCertSvcApproved(dclCertSvcFFF1Subject, dclCertSvcFFF1Colons, dclCertSvcFFF1Record())
}

// dclCertSvcGitHubFiles is mockGitHubFileList (:90-95).
var dclCertSvcGitHubFiles = []any{
	map[string]any{"name": "Chip-Test-PAA-NoVID-Cert.der", "type": "file"},
	map[string]any{"name": "Chip-Test-PAA-FFF1-Cert.der", "type": "file"},
	map[string]any{"name": "dcld_mirror_test.der", "type": "file"},
	map[string]any{"name": "README.md", "type": "file"},
}

// dclCertSvcFetch is matter.js MockFetch as an http.RoundTripper: a
// request is answered by the most recently added mock whose URL contains
// the mock's string; none is a transport error. gate is gateFetch (:115-131).
type dclCertSvcFetch struct {
	mu    sync.Mutex
	mocks []dclCertSvcMock
	calls []string
	gated *dclCertSvcGate
}

type dclCertSvcMock struct {
	url    string
	status int
	body   []byte
}

type dclCertSvcGate struct {
	once    sync.Once
	started chan struct{}
	done    chan struct{}
	err     error
}

func (g *dclCertSvcGate) settle(err error) {
	g.err = err
	close(g.done)
}

// add answers url with body: []byte as is, anything else as JSON.
func (f *dclCertSvcFetch) add(url string, body any, status ...int) {
	b, ok := body.([]byte)
	if !ok {
		var err error
		if b, err = json.Marshal(body); err != nil {
			panic(err)
		}
	}
	m := dclCertSvcMock{url: url, status: http.StatusOK, body: b}
	if len(status) > 0 {
		m.status = status[0]
	}
	f.mu.Lock()
	f.mocks = append(f.mocks, m)
	f.mu.Unlock()
}

// addMocks is the beforeEach set of most cases: the list and both records.
func (f *dclCertSvcFetch) addMocks(host string) {
	f.add(host+dclCertSvcRootListPath, dclCertSvcMockList())
	f.add(host+dclCertSvcNoVIDPath, dclCertSvcNoVID())
	f.add(host+dclCertSvcFFF1Path, dclCertSvcFFF1())
}

func (f *dclCertSvcFetch) gate() *dclCertSvcGate {
	g := &dclCertSvcGate{started: make(chan struct{}), done: make(chan struct{})}
	f.mu.Lock()
	f.gated = g
	f.mu.Unlock()
	return g
}

func (f *dclCertSvcFetch) RoundTrip(r *http.Request) (*http.Response, error) {
	u := r.URL.String()
	f.mu.Lock()
	f.calls = append(f.calls, u)
	g := f.gated
	f.mu.Unlock()
	if g != nil {
		g.once.Do(func() { close(g.started) })
		select {
		case <-g.done:
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
		if g.err != nil {
			return nil, g.err
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.mocks) - 1; i >= 0; i-- {
		if m := f.mocks[i]; strings.Contains(u, m.url) {
			return &http.Response{
				StatusCode: m.status, Status: http.StatusText(m.status), Header: http.Header{},
				Body: io.NopCloser(bytes.NewReader(m.body)), Request: r,
			}, nil
		}
	}
	return nil, errors.New("No mock found for URL: " + u)
}

func (f *dclCertSvcFetch) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *dclCertSvcFetch) called(s string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if strings.Contains(c, s) {
			n++
		}
	}
	return n
}

func (f *dclCertSvcFetch) clear() {
	f.mu.Lock()
	f.calls = nil
	f.mu.Unlock()
}

// client is the production ledger's client over f; the test ledger is
// the same client against TestNetURL (clientFor).
func (f *dclCertSvcFetch) client() *Client {
	return &Client{HTTPClient: &http.Client{Transport: f}}
}

// dclCertSvcLogs keeps every record: its level and its text with the
// attributes.
type dclCertSvcLogs struct {
	mu   sync.Mutex
	recs []dclCertSvcRec
}

type dclCertSvcRec struct {
	level slog.Level
	text  string
}

func (l *dclCertSvcLogs) Enabled(context.Context, slog.Level) bool { return true }

func (l *dclCertSvcLogs) Handle(_ context.Context, r slog.Record) error {
	var b strings.Builder
	b.WriteString(r.Message)
	r.Attrs(func(a slog.Attr) bool {
		b.WriteString(" " + a.Key + "=" + a.Value.String())
		return true
	})
	l.mu.Lock()
	l.recs = append(l.recs, dclCertSvcRec{level: r.Level, text: b.String()})
	l.mu.Unlock()
	return nil
}

func (l *dclCertSvcLogs) WithAttrs([]slog.Attr) slog.Handler { return l }
func (l *dclCertSvcLogs) WithGroup(string) slog.Handler      { return l }

func (l *dclCertSvcLogs) reset() {
	l.mu.Lock()
	l.recs = nil
	l.mu.Unlock()
}

func (l *dclCertSvcLogs) find(s string) (dclCertSvcRec, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, r := range l.recs {
		if strings.Contains(r.text, s) {
			return r, true
		}
	}
	return dclCertSvcRec{}, false
}

// dclCertSvcSaves is a MemoryStore counting its saves.
type dclCertSvcSaves struct {
	MemoryStore
	mu sync.Mutex
	n  int
}

func (s *dclCertSvcSaves) Save(ctx context.Context, data []byte) error {
	s.mu.Lock()
	s.n++
	s.mu.Unlock()
	return s.MemoryStore.Save(ctx, data)
}

func (s *dclCertSvcSaves) saves() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.n
}

// dclCertSvcOpen is new DclCertificateService(environment, options) with
// await construction: the store over f, a fake clock and opts.
func dclCertSvcOpen(t *testing.T, f *dclCertSvcFetch, opts CertificateOptions) *CertificateStore {
	t.Helper()
	if opts.Client == nil && f != nil {
		opts.Client = f.client()
	}
	if opts.Clock == nil {
		opts.Clock = newManualClock()
	}
	s, err := OpenCertificateStore(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

// dclCertSvcCorrupt is corruptStoredCertificate (:663-666): the stored DER
// of skid becomes 30 03 01 02 03. The live store keeps the DER in its index.
func dclCertSvcCorrupt(s *CertificateStore, skid string) []byte {
	corrupt := []byte{0x30, 0x03, 0x01, 0x02, 0x03}
	s.mu.Lock()
	e := s.index[skid]
	e.DER = corrupt
	s.index[skid] = e
	s.mu.Unlock()
	return corrupt
}

func dclCertSvcBool(b bool) *bool { return &b }

func dclCertSvcConsider(b bool) GetCertificateOptions {
	return GetCertificateOptions{ConsiderTestCertificates: dclCertSvcBool(b)}
}

func dclCertSvcMustCert(t *testing.T, s *CertificateStore, skid string) CertificateMetadata {
	t.Helper()
	m, ok := s.Certificate(skid, GetCertificateOptions{})
	if !ok {
		t.Fatalf("certificate %s not found", skid)
	}
	return m
}

func dclCertSvcIsProduction(s *CertificateStore, skid string, o GetCertificateOptions) (production, found bool) {
	m, ok := s.Certificate(skid, o)
	return m.IsProduction, ok
}

// TestDclCertificateServiceInitialization ports describe("initialization").
func TestDclCertificateServiceInitialization(t *testing.T) {
	t.Parallel()
	t.Run("doesn't duplicate certificates on re-initialization", func(t *testing.T) {
		t.Parallel()
		f := &dclCertSvcFetch{}
		f.addMocks("")
		clock := newManualClock()
		s := dclCertSvcOpen(t, f, CertificateOptions{Clock: clock})
		if n := len(s.Certificates()); n != 2 {
			t.Fatalf("%d certificates", n)
		}
		clock.advance(24 * time.Hour)
		if n := len(s.Certificates()); n != 2 {
			t.Fatalf("%d certificates after the update, want 2", n)
		}
	})
}

// TestDclCertificateServicePeriodicUpdates ports describe("periodic updates").
func TestDclCertificateServicePeriodicUpdates(t *testing.T) {
	t.Parallel()
	t.Run("uses custom update interval when provided", func(t *testing.T) {
		t.Parallel()
		f := &dclCertSvcFetch{}
		f.addMocks("")
		clock := newManualClock()
		dclCertSvcOpen(t, f, CertificateOptions{Clock: clock, UpdateInterval: 5 * time.Minute})
		initial := f.callCount()
		clock.advance(5 * time.Minute)
		if f.callCount() <= initial {
			t.Fatal("no update after 5 minutes")
		}
	})
	t.Run("stops updates after close()", func(t *testing.T) {
		t.Parallel()
		f := &dclCertSvcFetch{}
		f.addMocks("")
		clock := newManualClock()
		s := dclCertSvcOpen(t, f, CertificateOptions{Clock: clock, UpdateInterval: time.Minute})
		s.Close()
		after := f.callCount()
		clock.advance(time.Minute)
		if f.callCount() != after {
			t.Fatal("an update after Close")
		}
	})
	// The Go store's storage is the host's Store, which has no close: the
	// port asserts that nothing is saved once Close began.
	t.Run("stops the timer and closes storage when the in-flight fetch fails during close()", func(t *testing.T) {
		t.Parallel()
		f := &dclCertSvcFetch{}
		f.add(dclCertSvcRootListPath, dclCertSvcMockList())
		clock := newManualClock()
		store := &dclCertSvcSaves{}
		s := dclCertSvcOpen(t, f, CertificateOptions{Clock: clock, Store: store, UpdateInterval: time.Minute})
		if clock.pending() != 1 {
			t.Fatalf("%d update timers", clock.pending())
		}
		g := f.gate()
		updating := make(chan bool)
		go func() { updating <- s.Update(context.Background(), true) }()
		<-g.started
		closing := make(chan struct{})
		go func() { s.Close(); close(closing) }()
		for !s.isClosed() {
			runtime.Gosched()
		}
		saves := store.saves()
		g.settle(errors.New("network down"))
		<-closing
		<-updating
		if clock.pending() != 0 {
			t.Fatal("the update timer survived Close")
		}
		if store.saves() != saves {
			t.Fatal("saved after Close")
		}
	})
	t.Run("resolves concurrent close() calls only after cleanup is done", func(t *testing.T) {
		t.Parallel()
		f := &dclCertSvcFetch{}
		f.add(dclCertSvcRootListPath, dclCertSvcMockList())
		clock := newManualClock()
		store := &dclCertSvcSaves{}
		s := dclCertSvcOpen(t, f, CertificateOptions{Clock: clock, Store: store, UpdateInterval: time.Minute})
		g := f.gate()
		updating := make(chan bool)
		go func() { updating <- s.Update(context.Background(), true) }()
		<-g.started
		first, second := make(chan struct{}), make(chan struct{})
		go func() { s.Close(); close(first) }()
		go func() { s.Close(); close(second) }()
		for !s.isClosed() {
			runtime.Gosched()
		}
		for range 100 {
			runtime.Gosched()
		}
		select {
		case <-first:
			t.Fatal("Close returned while the update ran")
		case <-second:
			t.Fatal("Close returned while the update ran")
		default:
		}
		saves := store.saves()
		g.settle(nil)
		<-first
		<-second
		<-updating
		if clock.pending() != 0 {
			t.Fatal("the update timer survived Close")
		}
		if store.saves() != saves {
			t.Fatal("saved after Close")
		}
	})
}

// TestDclCertificateServiceErrorHandling ports describe("error handling").
func TestDclCertificateServiceErrorHandling(t *testing.T) {
	t.Parallel()
	t.Run("handles DCL fetch errors gracefully", func(t *testing.T) {
		t.Parallel()
		f := &dclCertSvcFetch{}
		f.add(dclCertSvcRootListPath, dclCertSvcMockList(), 500)
		if n := len(dclCertSvcOpen(t, f, CertificateOptions{}).Certificates()); n != 0 {
			t.Fatalf("%d certificates", n)
		}
	})
	t.Run("continues processing after individual certificate fetch failure", func(t *testing.T) {
		t.Parallel()
		f := &dclCertSvcFetch{}
		f.add(dclCertSvcRootListPath, dclCertSvcMockList())
		f.add(dclCertSvcNoVIDPath, map[string]any{"error": "Not found"}, 404)
		f.add(dclCertSvcFFF1Path, dclCertSvcFFF1())
		if n := len(dclCertSvcOpen(t, f, CertificateOptions{}).Certificates()); n != 1 {
			t.Fatalf("%d certificates, want 1", n)
		}
	})
	t.Run("stores the valid record from a subject whose records include a malformed PEM entry", func(t *testing.T) {
		t.Parallel()
		f := &dclCertSvcFetch{}
		f.add(dclCertSvcRootListPath, dclCertSvcRootList([2]string{dclCertSvcNoVIDSubject, dclCertSvcNoVIDColons}))
		bad := dclCertSvcNoVIDRecord()
		bad["pemCert"] = "not a valid PEM certificate"
		f.add(dclCertSvcNoVIDPath, dclCertSvcApproved(dclCertSvcNoVIDSubject, dclCertSvcNoVIDColons, bad, dclCertSvcNoVIDRecord()))
		s := dclCertSvcOpen(t, f, CertificateOptions{})
		if n := len(s.Certificates()); n != 1 {
			t.Fatalf("%d certificates", n)
		}
		if m := dclCertSvcMustCert(t, s, dclCertSvcNoVIDSKID); m.SubjectAsText != "CN=Matter Test PAA" {
			t.Fatalf("subjectAsText %q", m.SubjectAsText)
		}
	})
	t.Run("skips a record whose certificate SKID differs from the record's stated SKID", func(t *testing.T) {
		t.Parallel()
		const mismatched = "FF:FF:FF:FF:FF:FF:FF:FF:FF:FF:FF:FF:FF:FF:FF:FF:FF:FF:FF:FF"
		const normalized = "FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF"
		f := &dclCertSvcFetch{}
		f.add(dclCertSvcRootListPath, dclCertSvcRootList([2]string{dclCertSvcNoVIDSubject, mismatched}))
		rec := dclCertSvcNoVIDRecord()
		rec["pemCert"] = dclCertSvcPEM(secattestation.TestPAAFFF1Cert)
		rec["subjectKeyId"] = mismatched
		f.add("/dcl/pki/certificates/"+encodeURIComponent(dclCertSvcNoVIDSubject)+"/"+encodeURIComponent(mismatched),
			dclCertSvcApproved(dclCertSvcNoVIDSubject, mismatched, rec))
		s := dclCertSvcOpen(t, f, CertificateOptions{UpdateInterval: -1})
		if n := len(s.Certificates()); n != 0 {
			t.Fatalf("%d certificates", n)
		}
		if _, ok := s.Certificate(normalized, GetCertificateOptions{}); ok {
			t.Fatal("the mismatched certificate is held")
		}
		if _, ok := s.GetOrFetchCertificate(context.Background(), normalized, GetOrFetchCertificateOptions{}); ok {
			t.Fatal("the mismatched certificate was fetched")
		}
	})
	// GitHub is opt-in in Go (by_design BD-Matter-DCL-OptIn): the option
	// is set where matter.js's default fetches it.
	t.Run("handles GitHub fetch errors gracefully when test certs enabled", func(t *testing.T) {
		t.Parallel()
		f := &dclCertSvcFetch{}
		f.addMocks(dclCertSvcProdHost)
		f.add(dclCertSvcTestHost+dclCertSvcRootListPath, dclCertSvcRootList())
		f.add(dclCertSvcGitHubPAADir, map[string]any{"error": "Rate limit exceeded"}, 429)
		s := dclCertSvcOpen(t, f, CertificateOptions{FetchTestCertificates: true, FetchGitHubCertificates: true})
		if n := len(s.Certificates()); n != 2 {
			t.Fatalf("%d certificates, want 2", n)
		}
	})
}

// TestDclCertificateServiceCertificateRetrieval ports describe("certificate retrieval").
func TestDclCertificateServiceCertificateRetrieval(t *testing.T) {
	t.Parallel()
	open := func(t *testing.T) (*CertificateStore, *dclCertSvcFetch) {
		t.Helper()
		f := &dclCertSvcFetch{}
		f.addMocks("")
		return dclCertSvcOpen(t, f, CertificateOptions{}), f
	}
	t.Run("retrieves certificate by subject key ID", func(t *testing.T) {
		t.Parallel()
		s, _ := open(t)
		if m := dclCertSvcMustCert(t, s, dclCertSvcNoVIDSKID); m.SubjectKeyID != dclCertSvcNoVIDSKID {
			t.Fatalf("subjectKeyId %q", m.SubjectKeyID)
		}
	})
	t.Run("returns undefined for non-existent certificate", func(t *testing.T) {
		t.Parallel()
		s, _ := open(t)
		if _, ok := s.Certificate("NONEXISTENT", GetCertificateOptions{}); ok {
			t.Fatal("found")
		}
	})
	t.Run("retrieves all certificates", func(t *testing.T) {
		t.Parallel()
		s, _ := open(t)
		certs := s.Certificates()
		if len(certs) != 2 {
			t.Fatalf("%d certificates", len(certs))
		}
		for _, c := range certs {
			if c.SubjectKeyID == "" || c.SerialNumber == "" {
				t.Fatalf("incomplete %+v", c)
			}
		}
	})
	t.Run("retrieves certificate as PEM", func(t *testing.T) {
		t.Parallel()
		s, _ := open(t)
		p, err := s.CertificatePEM(dclCertSvcNoVIDSKID, GetCertificateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(p, "\n")
		if len(lines) <= 2 || lines[0] != "-----BEGIN CERTIFICATE-----" || lines[len(lines)-1] != "-----END CERTIFICATE-----" {
			t.Fatalf("PEM %q", p)
		}
	})
	t.Run("throws error for non-existent certificate when getting PEM", func(t *testing.T) {
		t.Parallel()
		s, _ := open(t)
		if _, err := s.CertificatePEM("NONEXISTENT", GetCertificateOptions{}); err == nil {
			t.Fatal("no error")
		}
	})
	t.Run("force re-fetches a corrupted cached certificate and returns valid DER", func(t *testing.T) {
		t.Parallel()
		s, _ := open(t)
		good, err := s.CertificateDER(context.Background(), dclCertSvcNoVIDSKID, GetCertificateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := parseTrustStoreCertificate(good, KindPAA); err != nil {
			t.Fatal(err)
		}
		dclCertSvcCorrupt(s, dclCertSvcNoVIDSKID)
		healed, err := s.CertificateDER(context.Background(), dclCertSvcNoVIDSKID, GetCertificateOptions{})
		if err != nil || !bytes.Equal(healed, good) {
			t.Fatalf("not healed: %v", err)
		}
	})
	t.Run("returns the original bytes when a corrupted cache cannot be re-fetched", func(t *testing.T) {
		t.Parallel()
		s, f := open(t)
		corrupt := dclCertSvcCorrupt(s, dclCertSvcNoVIDSKID)
		f.add(dclCertSvcRootListPath, dclCertSvcRootList())
		got, err := s.CertificateDER(context.Background(), dclCertSvcNoVIDSKID, GetCertificateOptions{})
		if err != nil || !bytes.Equal(got, corrupt) {
			t.Fatalf("got %x %v", got, err)
		}
		if _, err := parseTrustStoreCertificate(got, KindPAA); err == nil {
			t.Fatal("the corrupt bytes parse")
		}
	})
	t.Run("returns the cached bytes without a second parse warning when the re-fetched certificate is unusable", func(t *testing.T) {
		t.Parallel()
		f := &dclCertSvcFetch{}
		f.addMocks("")
		logs := &dclCertSvcLogs{}
		s := dclCertSvcOpen(t, f, CertificateOptions{Logger: slog.New(logs)})
		corrupt := dclCertSvcCorrupt(s, dclCertSvcNoVIDSKID)
		rec := dclCertSvcNoVIDRecord()
		rec["pemCert"] = dclCertSvcPEM(corrupt)
		f.add("/dcl/pki/certificates/MDAxGDAWBgNVBAMMD01hdHRlciBUZXN0IFBBQQ%3D%3D", dclCertSvcApproved(dclCertSvcNoVIDSubject, dclCertSvcNoVIDColons, rec))
		f.clear()
		logs.reset()
		got, err := s.CertificateDER(context.Background(), dclCertSvcNoVIDSKID, GetCertificateOptions{})
		if n := f.called("/dcl/pki/certificates/"); n != 1 {
			t.Fatalf("%d certificate downloads", n)
		}
		if err != nil || !bytes.Equal(got, corrupt) {
			t.Fatalf("got %x %v", got, err)
		}
		if _, ok := logs.find("Re-fetched certificate also failed to parse"); ok {
			t.Fatal("a second parse warning")
		}
	})
}

// TestDclCertificateServiceCertificateDeletion ports describe("certificate deletion").
func TestDclCertificateServiceCertificateDeletion(t *testing.T) {
	t.Parallel()
	t.Run("deletes certificate successfully", func(t *testing.T) {
		t.Parallel()
		f := &dclCertSvcFetch{}
		f.addMocks("")
		s := dclCertSvcOpen(t, f, CertificateOptions{})
		if n := len(s.Certificates()); n != 2 {
			t.Fatalf("%d certificates", n)
		}
		s.DeleteCertificate(context.Background(), dclCertSvcNoVIDSKID)
		if n := len(s.Certificates()); n != 1 {
			t.Fatalf("%d certificates after the deletion", n)
		}
		if _, ok := s.Certificate(dclCertSvcNoVIDSKID, GetCertificateOptions{}); ok {
			t.Fatal("still retrievable")
		}
	})
	// matter.js expects no rejection; the Go method has no error.
	t.Run("throws error when deleting non-existent certificate", func(t *testing.T) {
		t.Parallel()
		f := &dclCertSvcFetch{}
		f.addMocks("")
		s := dclCertSvcOpen(t, f, CertificateOptions{})
		s.DeleteCertificate(context.Background(), "NONEXISTENT")
		if n := len(s.Certificates()); n != 2 {
			t.Fatalf("%d certificates", n)
		}
	})
	t.Run("updates index after deletion", func(t *testing.T) {
		t.Parallel()
		f := &dclCertSvcFetch{}
		f.addMocks("")
		store := &MemoryStore{}
		s := dclCertSvcOpen(t, f, CertificateOptions{Store: store})
		s.DeleteCertificate(context.Background(), dclCertSvcNoVIDSKID)
		s.Close()
		// The stored index lacks it (an offline store reads it alone).
		if n := len(dclCertSvcOpen(t, nil, CertificateOptions{Store: store}).Certificates()); n != 1 {
			t.Fatalf("%d stored certificates", n)
		}
		f2 := &dclCertSvcFetch{}
		f2.addMocks("")
		if n := len(dclCertSvcOpen(t, f2, CertificateOptions{Store: store}).Certificates()); n != 2 {
			t.Fatalf("%d certificates after the re-fetch", n)
		}
	})
}

// TestDclCertificateServiceManualUpdate ports describe("manual certificate update").
func TestDclCertificateServiceManualUpdate(t *testing.T) {
	t.Parallel()
	t.Run("triggers certificate update manually", func(t *testing.T) {
		t.Parallel()
		f := &dclCertSvcFetch{}
		f.addMocks("")
		s := dclCertSvcOpen(t, f, CertificateOptions{})
		initial := f.callCount()
		s.Update(context.Background(), false)
		if f.callCount() <= initial {
			t.Fatal("no request")
		}
	})
	t.Run("force mode re-downloads and overwrites existing certificates", func(t *testing.T) {
		t.Parallel()
		f := &dclCertSvcFetch{}
		f.addMocks("")
		s := dclCertSvcOpen(t, f, CertificateOptions{})
		initial := f.callCount()
		if n := len(s.Certificates()); n != 2 {
			t.Fatalf("%d certificates", n)
		}
		s.Update(context.Background(), false)
		normal := f.callCount()
		if normal != initial+1 {
			t.Fatalf("%d requests for an update, want the list alone", normal-initial)
		}
		s.Update(context.Background(), true)
		if n := f.callCount(); n != normal+3 {
			t.Fatalf("%d requests for a forced update, want 3", n-normal)
		}
		if n := len(s.Certificates()); n != 2 {
			t.Fatalf("%d certificates after the forced update", n)
		}
	})
}

// TestDclCertificateServiceGetOrFetchCertificate ports describe("getOrFetchCertificate").
func TestDclCertificateServiceGetOrFetchCertificate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	t.Run("returns existing certificate from index", func(t *testing.T) {
		t.Parallel()
		f := &dclCertSvcFetch{}
		f.addMocks("")
		s := dclCertSvcOpen(t, f, CertificateOptions{})
		initial := f.callCount()
		m, ok := s.GetOrFetchCertificate(ctx, dclCertSvcNoVIDSKID, GetOrFetchCertificateOptions{})
		if !ok || m.SubjectKeyID != dclCertSvcNoVIDSKID {
			t.Fatalf("%+v %v", m, ok)
		}
		if f.callCount() != initial {
			t.Fatal("asked the ledger")
		}
	})
	t.Run("accepts subject key ID with colons", func(t *testing.T) {
		t.Parallel()
		f := &dclCertSvcFetch{}
		f.addMocks("")
		s := dclCertSvcOpen(t, f, CertificateOptions{})
		m, ok := s.GetOrFetchCertificate(ctx, dclCertSvcNoVIDColons, GetOrFetchCertificateOptions{})
		if !ok || m.SubjectKeyID != dclCertSvcNoVIDSKID {
			t.Fatalf("%+v %v", m, ok)
		}
	})
	// The store opens on an empty ledger, so the certificate is truly
	// absent; then the ledger lists it.
	t.Run("fetches certificate from DCL if not in index", func(t *testing.T) {
		t.Parallel()
		f := &dclCertSvcFetch{}
		f.add(dclCertSvcRootListPath, dclCertSvcRootList())
		s := dclCertSvcOpen(t, f, CertificateOptions{UpdateInterval: -1})
		if _, ok := s.Certificate(dclCertSvcNoVIDSKID, GetCertificateOptions{}); ok {
			t.Fatal("held before the fetch")
		}
		f.add(dclCertSvcRootListPath, dclCertSvcMockList())
		f.add(dclCertSvcNoVIDPath, dclCertSvcNoVID())
		m, ok := s.GetOrFetchCertificate(ctx, dclCertSvcNoVIDSKID, GetOrFetchCertificateOptions{})
		if !ok || m.SubjectKeyID != dclCertSvcNoVIDSKID {
			t.Fatalf("%+v %v", m, ok)
		}
		dclCertSvcMustCert(t, s, dclCertSvcNoVIDSKID)
	})
	t.Run("returns undefined for non-existent certificate", func(t *testing.T) {
		t.Parallel()
		f := &dclCertSvcFetch{}
		f.add(dclCertSvcRootListPath, dclCertSvcRootList())
		s := dclCertSvcOpen(t, f, CertificateOptions{UpdateInterval: -1})
		if _, ok := s.GetOrFetchCertificate(ctx, "NONEXISTENTCERTIFICATE", GetOrFetchCertificateOptions{}); ok {
			t.Fatal("found")
		}
	})
	t.Run("returns undefined on DCL error", func(t *testing.T) {
		t.Parallel()
		f := &dclCertSvcFetch{}
		f.add(dclCertSvcRootListPath, map[string]any{"error": "Server error"}, 500)
		s := dclCertSvcOpen(t, f, CertificateOptions{UpdateInterval: -1})
		if _, ok := s.GetOrFetchCertificate(ctx, dclCertSvcNoVIDSKID, GetOrFetchCertificateOptions{}); ok {
			t.Fatal("found")
		}
	})
}

// TestDclCertificateServiceBytesSubjectKeyIDs ports describe("Bytes support
// for subject key IDs"): the Go methods take the key identifier as hex
// text, so the bytes arrive as hex.EncodeToString's lowercase hex.
func TestDclCertificateServiceBytesSubjectKeyIDs(t *testing.T) {
	t.Parallel()
	raw, _ := hex.DecodeString(dclCertSvcNoVIDSKID)
	skid := hex.EncodeToString(raw)
	open := func(t *testing.T) *CertificateStore {
		t.Helper()
		f := &dclCertSvcFetch{}
		f.addMocks("")
		return dclCertSvcOpen(t, f, CertificateOptions{})
	}
	t.Run("getCertificate accepts Bytes", func(t *testing.T) {
		t.Parallel()
		if m, ok := open(t).Certificate(skid, GetCertificateOptions{}); !ok || m.SubjectKeyID != dclCertSvcNoVIDSKID {
			t.Fatalf("%+v %v", m, ok)
		}
		// The attestation surface takes bytes as such.
		if _, ok := open(t).PAA(raw); !ok {
			t.Fatal("PAA by bytes")
		}
	})
	t.Run("getCertificateAsPem accepts Bytes", func(t *testing.T) {
		t.Parallel()
		p, err := open(t).CertificatePEM(skid, GetCertificateOptions{})
		if err != nil || !strings.Contains(p, "-----BEGIN CERTIFICATE-----") || !strings.Contains(p, "-----END CERTIFICATE-----") {
			t.Fatalf("%q %v", p, err)
		}
	})
	t.Run("getOrFetchCertificate accepts Bytes", func(t *testing.T) {
		t.Parallel()
		m, ok := open(t).GetOrFetchCertificate(context.Background(), skid, GetOrFetchCertificateOptions{})
		if !ok || m.SubjectKeyID != dclCertSvcNoVIDSKID {
			t.Fatalf("%+v %v", m, ok)
		}
	})
	t.Run("deleteCertificate accepts Bytes", func(t *testing.T) {
		t.Parallel()
		s := open(t)
		if n := len(s.Certificates()); n != 2 {
			t.Fatalf("%d certificates", n)
		}
		s.DeleteCertificate(context.Background(), skid)
		if n := len(s.Certificates()); n != 1 {
			t.Fatalf("%d certificates after the deletion", n)
		}
	})
}

// TestDclCertificateServiceCustomDclConfig ports describe("custom DCL
// config"): dclConfig and testDclConfig are the Client and TestClient.
func TestDclCertificateServiceCustomDclConfig(t *testing.T) {
	t.Parallel()
	t.Run("uses custom production URL when dclConfig provided", func(t *testing.T) {
		t.Parallel()
		f := &dclCertSvcFetch{}
		f.add("custom-prod.dcl/dcl/pki/root-certificates", dclCertSvcRootList())
		c := f.client()
		c.BaseURL = "https://custom-prod.dcl"
		dclCertSvcOpen(t, f, CertificateOptions{Client: c})
		if f.called("custom-prod.dcl") == 0 || f.called(dclCertSvcProdHost) != 0 {
			t.Fatalf("calls %v", f.calls)
		}
	})
	t.Run("uses custom test URL when testDclConfig provided", func(t *testing.T) {
		t.Parallel()
		f := &dclCertSvcFetch{}
		f.add(dclCertSvcProdHost+dclCertSvcRootListPath, dclCertSvcRootList())
		f.add("custom-test.dcl/dcl/pki/root-certificates", dclCertSvcRootList())
		f.add(dclCertSvcGitHubPAADir, []any{})
		tc := f.client()
		tc.BaseURL = "https://custom-test.dcl"
		dclCertSvcOpen(t, f, CertificateOptions{TestClient: tc, FetchTestCertificates: true, FetchGitHubCertificates: true})
		if f.called("custom-test.dcl") == 0 || f.called(dclCertSvcTestHost) != 0 {
			t.Fatalf("calls %v", f.calls)
		}
	})
	t.Run("uses defaults when no config provided", func(t *testing.T) {
		t.Parallel()
		f := &dclCertSvcFetch{}
		f.add(dclCertSvcRootListPath, dclCertSvcRootList())
		dclCertSvcOpen(t, f, CertificateOptions{})
		if f.called(dclCertSvcProdHost) == 0 {
			t.Fatalf("calls %v", f.calls)
		}
	})
}

// TestDclCertificateServiceFetchGithubCertificatesOption ports
// describe("fetchGithubCertificates option").
func TestDclCertificateServiceFetchGithubCertificatesOption(t *testing.T) {
	t.Parallel()
	t.Run("skips GitHub when fetchGithubCertificates is false", func(t *testing.T) {
		t.Parallel()
		f := &dclCertSvcFetch{}
		f.add(dclCertSvcProdHost+dclCertSvcRootListPath, dclCertSvcRootList())
		f.add(dclCertSvcTestHost+dclCertSvcRootListPath, dclCertSvcRootList())
		dclCertSvcOpen(t, f, CertificateOptions{FetchTestCertificates: true, FetchGitHubCertificates: false})
		if f.called(dclCertSvcTestHost) == 0 || f.called("api.github.com") != 0 {
			t.Fatalf("calls %v", f.calls)
		}
	})
	t.Run("GitHub option has no effect when fetchTestCertificates is false", func(t *testing.T) {
		t.Parallel()
		f := &dclCertSvcFetch{}
		f.add(dclCertSvcRootListPath, dclCertSvcRootList())
		dclCertSvcOpen(t, f, CertificateOptions{FetchTestCertificates: false, FetchGitHubCertificates: true})
		if f.called("test-net.dcl") != 0 || f.called("api.github.com") != 0 {
			t.Fatalf("calls %v", f.calls)
		}
	})
}

// TestDclCertificateServiceCustomGithubConfig ports describe("custom
// githubConfig"); GitHub is turned on as matter.js's default has it.
func TestDclCertificateServiceCustomGithubConfig(t *testing.T) {
	t.Parallel()
	t.Run("uses custom GitHub repo when githubConfig provided", func(t *testing.T) {
		t.Parallel()
		f := &dclCertSvcFetch{}
		f.add(dclCertSvcProdHost+dclCertSvcRootListPath, dclCertSvcRootList())
		f.add(dclCertSvcTestHost+dclCertSvcRootListPath, dclCertSvcRootList())
		f.add("api.github.com/repos/my-org/my-repo/contents/certs/paa", []any{})
		f.add("api.github.com/repos/my-org/my-repo/contents/certs/cd-signers", []any{})
		dclCertSvcOpen(t, f, CertificateOptions{FetchTestCertificates: true, FetchGitHubCertificates: true, GitHub: GitHubConfig{
			Owner: "my-org", Repo: "my-repo", Branch: "main", CertPath: "certs/paa", CDSignerCertPath: "certs/cd-signers",
		}})
		if f.called("my-org/my-repo") == 0 || f.called("project-chip/connectedhomeip") != 0 {
			t.Fatalf("calls %v", f.calls)
		}
	})
	t.Run("uses defaults when githubConfig not provided", func(t *testing.T) {
		t.Parallel()
		f := &dclCertSvcFetch{}
		f.add(dclCertSvcProdHost+dclCertSvcRootListPath, dclCertSvcRootList())
		f.add(dclCertSvcTestHost+dclCertSvcRootListPath, dclCertSvcRootList())
		f.add(dclCertSvcGitHubPAADir, []any{})
		dclCertSvcOpen(t, f, CertificateOptions{FetchTestCertificates: true, FetchGitHubCertificates: true})
		if f.called("project-chip/connectedhomeip") == 0 {
			t.Fatalf("calls %v", f.calls)
		}
	})
}

// TestDclCertificateServiceIsProductionFlag ports describe("isProduction flag protection").
func TestDclCertificateServiceIsProductionFlag(t *testing.T) {
	t.Parallel()
	both := func(t *testing.T, s *CertificateStore, want bool) {
		t.Helper()
		for _, skid := range []string{dclCertSvcNoVIDSKID, dclCertSvcFFF1SKID} {
			if p, ok := dclCertSvcIsProduction(s, skid, GetCertificateOptions{}); !ok || p != want {
				t.Fatalf("%s isProduction %v (found %v), want %v", skid, p, ok, want)
			}
		}
	}
	t.Run("upgrades test certificate to production when found in production DCL", func(t *testing.T) {
		t.Parallel()
		store := &MemoryStore{}
		f := &dclCertSvcFetch{}
		f.add(dclCertSvcProdHost+dclCertSvcRootListPath, dclCertSvcRootList())
		f.addMocks(dclCertSvcTestHost)
		f.add(dclCertSvcGitHubPAADir, []any{})
		s1 := dclCertSvcOpen(t, f, CertificateOptions{Store: store, FetchTestCertificates: true, FetchGitHubCertificates: true})
		both(t, s1, false)
		s1.Close()

		f2 := &dclCertSvcFetch{}
		f2.addMocks(dclCertSvcProdHost)
		f2.add(dclCertSvcTestHost+dclCertSvcRootListPath, dclCertSvcRootList())
		f2.add(dclCertSvcGitHubPAADir, []any{})
		both(t, dclCertSvcOpen(t, f2, CertificateOptions{Store: store, FetchTestCertificates: true, FetchGitHubCertificates: true}), true)
	})
	t.Run("preserves isProduction flag when force-updating with test certificates", func(t *testing.T) {
		t.Parallel()
		f := &dclCertSvcFetch{}
		f.addMocks(dclCertSvcProdHost)
		f.addMocks(dclCertSvcTestHost)
		f.add(dclCertSvcGitHubPAADir, []any{})
		s := dclCertSvcOpen(t, f, CertificateOptions{FetchTestCertificates: true, FetchGitHubCertificates: true})
		both(t, s, true)
		s.Update(context.Background(), true)
		both(t, s, true)
	})
	t.Run("does not downgrade production cert when same cert loaded from GitHub", func(t *testing.T) {
		t.Parallel()
		f := &dclCertSvcFetch{}
		f.addMocks(dclCertSvcProdHost)
		f.add(dclCertSvcTestHost+dclCertSvcRootListPath, dclCertSvcRootList())
		f.add(dclCertSvcGitHubPAADir, dclCertSvcGitHubFiles)
		f.add(dclCertSvcGitHubRawNoVID, secattestation.TestPAANoVIDCert)
		f.add(dclCertSvcGitHubRawFFF1, secattestation.TestPAAFFF1Cert)
		both(t, dclCertSvcOpen(t, f, CertificateOptions{FetchTestCertificates: true, FetchGitHubCertificates: true}), true)
	})
}

// TestDclCertificateServiceAddCertificate ports describe("addCertificate").
func TestDclCertificateServiceAddCertificate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	open := func(t *testing.T) *CertificateStore {
		t.Helper()
		f := &dclCertSvcFetch{}
		f.addMocks("")
		return dclCertSvcOpen(t, f, CertificateOptions{})
	}
	t.Run("adds a CD signer and returns true", func(t *testing.T) {
		t.Parallel()
		if added, err := open(t).AddCertificate(ctx, dclCertSvcCDSigner, KindCDSigner, false); !added || err != nil {
			t.Fatalf("added %v %v", added, err)
		}
	})
	t.Run("returns false if same SKID already exists", func(t *testing.T) {
		t.Parallel()
		s := open(t)
		first, err1 := s.AddCertificate(ctx, dclCertSvcCDSigner, KindCDSigner, false)
		second, err2 := s.AddCertificate(ctx, dclCertSvcCDSigner, KindCDSigner, false)
		if !first || second || err1 != nil || err2 != nil {
			t.Fatalf("first %v second %v (%v, %v)", first, second, err1, err2)
		}
	})
	t.Run("respects explicit isProduction flag", func(t *testing.T) {
		t.Parallel()
		s := open(t)
		if _, err := s.AddCertificate(ctx, dclCertSvcCDSigner, KindCDSigner, true); err != nil {
			t.Fatal(err)
		}
		m := dclCertSvcMustCert(t, s, hex.EncodeToString(secattestation.TestCMSSignerSKID))
		if !m.IsProduction || m.Kind != KindCDSigner {
			t.Fatalf("%+v", m)
		}
	})
}

// TestDclCertificateServiceGetOrFetchCdSigner ports describe("getOrFetchCdSigner"):
// CDSigner answers the key; the stored metadata answers isProduction.
func TestDclCertificateServiceGetOrFetchCdSigner(t *testing.T) {
	t.Parallel()
	signerSKID := hexUpper(secattestation.TestCMSSignerSKID)
	signerKey := &secattestation.TestCMSSignerPrivateKey.PublicKey
	t.Run("returns locally injected CD signer without DCL lookup", func(t *testing.T) {
		t.Parallel()
		f := &dclCertSvcFetch{}
		f.addMocks("")
		s := dclCertSvcOpen(t, f, CertificateOptions{})
		if _, err := s.AddCertificate(context.Background(), dclCertSvcCDSigner, KindCDSigner, false); err != nil {
			t.Fatal(err)
		}
		before := f.callCount()
		key, ok := s.CDSigner(secattestation.TestCMSSignerSKID)
		if !ok || !key.Equal(signerKey) {
			t.Fatal("CD signer key")
		}
		if dclCertSvcMustCert(t, s, signerSKID).IsProduction {
			t.Fatal("isProduction")
		}
		if f.callCount() != before {
			t.Fatal("asked the ledger")
		}
	})
	t.Run("fetches CD signer from DCL on cache miss", func(t *testing.T) {
		t.Parallel()
		f := &dclCertSvcFetch{}
		f.addMocks("")
		colons := colonHex(secattestation.TestCMSSignerSKID)
		f.add("/dcl/pki/all-certificates?subjectKeyId="+encodeURIComponent(colons), map[string]any{"certificates": []any{map[string]any{
			"subject": "", "subjectKeyId": colons, "schemaVersion": 0,
			"certs": []any{map[string]any{"pemCert": dclCertSvcPEM(dclCertSvcCDSigner), "schemaVersion": 0}},
		}}})
		s := dclCertSvcOpen(t, f, CertificateOptions{})
		key, ok := s.CDSigner(secattestation.TestCMSSignerSKID)
		if !ok || !key.Equal(signerKey) {
			t.Fatal("CD signer key")
		}
		if !dclCertSvcMustCert(t, s, signerSKID).IsProduction {
			t.Fatal("a fetched CD signer is not production")
		}
	})
	t.Run("returns undefined when DCL has no CD signer for the SKID", func(t *testing.T) {
		t.Parallel()
		f := &dclCertSvcFetch{}
		f.addMocks("")
		f.add("/dcl/pki/all-certificates?subjectKeyId=", map[string]any{"certificates": []any{}})
		s := dclCertSvcOpen(t, f, CertificateOptions{})
		id, _ := hex.DecodeString("DEADBEEFDEADBEEFDEADBEEFDEADBEEFDEADBEEF")
		if _, ok := s.CDSigner(id); ok {
			t.Fatal("found")
		}
	})
}

// dclCertSvcTestCRL is buildTestCrl (TestHelpers.ts:140-148, crlTbs
// :250-296): an unsigned CRL of the given raw serials by "Test Issuer".
func dclCertSvcTestCRL(t *testing.T, serials ...string) []byte {
	t.Helper()
	ecdsaSHA256 := pkix.AlgorithmIdentifier{Algorithm: asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 3, 2}}
	issuer, err := asn1.Marshal(pkix.Name{CommonName: "Test Issuer"}.ToRDNSequence())
	if err != nil {
		t.Fatal(err)
	}
	utc := func(s string) asn1.RawValue { return asn1.RawValue{Tag: asn1.TagUTCTime, Bytes: []byte(s)} }
	type entry struct {
		Serial asn1.RawValue
		Date   asn1.RawValue
	}
	type tbs struct {
		Version    int
		Signature  pkix.AlgorithmIdentifier
		Issuer     asn1.RawValue
		ThisUpdate asn1.RawValue
		NextUpdate asn1.RawValue
		Revoked    []entry `asn1:"optional,omitempty"`
	}
	list := tbs{Version: 1, Signature: ecdsaSHA256, Issuer: asn1.RawValue{FullBytes: issuer}, ThisUpdate: utc("250101000000Z"), NextUpdate: utc("260101000000Z")}
	for _, s := range serials {
		b, err := hex.DecodeString(s)
		if err != nil {
			t.Fatal(err)
		}
		list.Revoked = append(list.Revoked, entry{Serial: asn1.RawValue{Tag: asn1.TagInteger, Bytes: b}, Date: utc("250101000000Z")})
	}
	der, err := asn1.Marshal(struct {
		TBS       tbs
		Algorithm pkix.AlgorithmIdentifier
		Signature asn1.BitString
	}{TBS: list, Algorithm: ecdsaSHA256})
	if err != nil {
		t.Fatal(err)
	}
	return der
}

// dclCertSvcChipPAA is the CHIP test PAA without vendor ID with its key:
// chipTestPaaCrlSigner (TestHelpers.ts:164-171).
func dclCertSvcChipPAA(t *testing.T) testCert {
	t.Helper()
	c, err := x509.ParseCertificate(secattestation.TestPAANoVIDCert)
	if err != nil {
		t.Fatal(err)
	}
	return testCert{der: secattestation.TestPAANoVIDCert, cert: c, key: secattestation.TestPAANoVIDPrivateKey}
}

// dclCertSvcPoints is the by-issuer answer of one distribution point (:1787-1810).
func dclCertSvcPoints(skid, dataURL string, revocationType int) map[string]any {
	return map[string]any{"pkiRevocationDistributionPointsByIssuerSubjectKeyID": map[string]any{
		"issuerSubjectKeyID": skid, "schemaVersion": 0,
		"points": []any{map[string]any{
			"vid": 0xfff1, "pid": 0, "isPAA": true, "label": "test-label", "crlSignerDelegator": "",
			"crlSignerCertificate": dclCertSvcPEM(secattestation.TestPAANoVIDCert), "issuerSubjectKeyID": skid,
			"dataURL": dataURL, "dataFileSize": "", "dataDigest": "", "dataDigestType": 0,
			"revocationType": revocationType, "schemaVersion": 0,
		}},
	}}
}

// TestDclCertificateServiceRevocationSupport ports describe("revocation support").
// parseCrlRevokedSerials and parseCrl are parseCRL's serials and result.
func TestDclCertificateServiceRevocationSupport(t *testing.T) {
	t.Parallel()
	t.Run("parseCrlRevokedSerials extracts serial numbers from CRL", func(t *testing.T) {
		t.Parallel()
		r, err := parseCRL(dclCertSvcTestCRL(t, "01AB", "02CD", "FF00FF"), nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(r.serials) != 3 {
			t.Fatalf("serials %v", r.serials)
		}
		for _, s := range []string{"01AB", "02CD", "FF00FF"} {
			if _, ok := r.serials[s]; !ok {
				t.Fatalf("%s missing from %v", s, r.serials)
			}
		}
	})
	t.Run("parseCrlRevokedSerials returns empty set for CRL with no revoked entries", func(t *testing.T) {
		t.Parallel()
		r, err := parseCRL(dclCertSvcTestCRL(t), nil)
		if err != nil || len(r.serials) != 0 {
			t.Fatalf("serials %v %v", r.serials, err)
		}
	})
	t.Run("parseCrl returns serials and issuerDnDerHex", func(t *testing.T) {
		t.Parallel()
		r, err := parseCRL(dclCertSvcTestCRL(t, "01AB", "02CD"), nil)
		if err != nil {
			t.Fatal(err)
		}
		_, a := r.serials["01AB"]
		_, b := r.serials["02CD"]
		if len(r.serials) != 2 || !a || !b || r.issuerDN == "" {
			t.Fatalf("%+v", r)
		}
	})
	t.Run("parseCrl returns tbsDer and signatureValue for signature verification", func(t *testing.T) {
		t.Parallel()
		r, err := parseCRL(dclCertSvcTestCRL(t, "AABB"), nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(r.tbs) == 0 || r.signatureValue == nil {
			t.Fatalf("tbs %d bytes, signatureValue %v", len(r.tbs), r.signatureValue)
		}
	})
	t.Run("isRevoked returns false when DCL has no revocation data for issuer", func(t *testing.T) {
		t.Parallel()
		f := &dclCertSvcFetch{}
		f.addMocks("")
		f.add("/dcl/pki/revocation-points/AABBCCDD", map[string]any{"code": 404, "message": "not found"}, 404)
		s := dclCertSvcOpen(t, f, CertificateOptions{})
		if revoked, err := s.IsRevoked([]byte{0xAA, 0xBB, 0xCC, 0xDD}, []byte{0x01, 0xAB}, ""); revoked || err != nil {
			t.Fatalf("revoked %v %v", revoked, err)
		}
	})
	signed := func(t *testing.T, url string, serial int64) *CertificateStore {
		t.Helper()
		crl := signCRL(t, dclCertSvcChipPAA(t), crlSpec{revoked: []*big.Int{big.NewInt(serial)}})
		f := &dclCertSvcFetch{}
		f.addMocks("")
		f.add("/dcl/pki/revocation-points/"+dclCertSvcNoVIDSKID, dclCertSvcPoints(dclCertSvcNoVIDSKID, url, RevocationTypeCRL))
		f.add(url, crl)
		return dclCertSvcOpen(t, f, CertificateOptions{})
	}
	akid, _ := hex.DecodeString(dclCertSvcNoVIDSKID)
	t.Run("isRevoked fetches CRL on demand and checks serial", func(t *testing.T) {
		t.Parallel()
		s := signed(t, "https://example.com/test.crl", 0x0123456789ABCDEF)
		on, _ := hex.DecodeString("0123456789ABCDEF")
		off, _ := hex.DecodeString("FEDCBA9876543210")
		if revoked, err := s.IsRevoked(akid, on, ""); !revoked || err != nil {
			t.Fatalf("revoked serial: %v %v", revoked, err)
		}
		if revoked, err := s.IsRevoked(akid, off, ""); revoked || err != nil {
			t.Fatalf("other serial: %v %v", revoked, err)
		}
	})
	// IsRevoked takes the key identifier and the serial as bytes.
	t.Run("isRevoked accepts Bytes for authority key identifier and serial number", func(t *testing.T) {
		t.Parallel()
		s := signed(t, "https://example.com/bytes-test.crl", 0x01AB)
		if revoked, err := s.IsRevoked(akid, []byte{0x01, 0xAB}, ""); !revoked || err != nil {
			t.Fatalf("revoked serial: %v %v", revoked, err)
		}
		if revoked, err := s.IsRevoked(akid, []byte{0xFF, 0xFF}, ""); revoked || err != nil {
			t.Fatalf("other serial: %v %v", revoked, err)
		}
	})
	const other = "AABBCCDDEEFF00112233445566778899AABBCCDD"
	otherID, _ := hex.DecodeString(other)
	t.Run("skips non-CRL revocation types", func(t *testing.T) {
		t.Parallel()
		f := &dclCertSvcFetch{}
		f.addMocks("")
		f.add("/dcl/pki/revocation-points/"+other, dclCertSvcPoints(other, "https://example.com/should-not-be-fetched.crl", 2))
		s := dclCertSvcOpen(t, f, CertificateOptions{})
		if revoked, err := s.IsRevoked(otherID, []byte{0x01, 0xAB}, ""); revoked || err != nil {
			t.Fatalf("revoked %v %v", revoked, err)
		}
		if f.called("should-not-be-fetched") != 0 {
			t.Fatal("the CRL was fetched")
		}
	})
	t.Run("handles CRL fetch failure gracefully", func(t *testing.T) {
		t.Parallel()
		f := &dclCertSvcFetch{}
		f.addMocks("")
		f.add("/dcl/pki/revocation-points/"+other, dclCertSvcPoints(other, "https://example.com/broken.crl", RevocationTypeCRL))
		f.add("https://example.com/broken.crl", map[string]any{"error": "Not found"}, 404)
		s := dclCertSvcOpen(t, f, CertificateOptions{})
		if n := len(s.Certificates()); n != 2 {
			t.Fatalf("%d certificates", n)
		}
		if revoked, err := s.IsRevoked(otherID, []byte{0x01, 0xAB}, ""); revoked || err != nil {
			t.Fatalf("revoked %v %v", revoked, err)
		}
	})
}

// TestDclCertificateServiceTestCertificateVisibility ports describe("test
// certificate visibility") with its two nested groups.
func TestDclCertificateServiceTestCertificateVisibility(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	// seed is seedStorageWithTestPaas (:2006-2027): a store holding both
	// test PAAs from a fetchTestCertificates run.
	seed := func(t *testing.T) Store {
		t.Helper()
		store := &MemoryStore{}
		f := &dclCertSvcFetch{}
		f.add(dclCertSvcProdHost+dclCertSvcRootListPath, dclCertSvcRootList())
		f.addMocks(dclCertSvcTestHost)
		f.add(dclCertSvcGitHubPAADir, []any{})
		s := dclCertSvcOpen(t, f, CertificateOptions{Store: store, FetchTestCertificates: true, FetchGitHubCertificates: true})
		for _, skid := range []string{dclCertSvcNoVIDSKID, dclCertSvcFFF1SKID} {
			if p, ok := dclCertSvcIsProduction(s, skid, GetCertificateOptions{}); !ok || p {
				t.Fatalf("seed %s: production %v found %v", skid, p, ok)
			}
		}
		s.Close()
		return store
	}
	// open is openServiceAfterSeed (:2032-2046): empty ledgers.
	open := func(t *testing.T, store Store, fetchTest bool, accept *bool) *CertificateStore {
		t.Helper()
		f := &dclCertSvcFetch{}
		f.add(dclCertSvcProdHost+dclCertSvcRootListPath, dclCertSvcRootList())
		if fetchTest {
			f.add(dclCertSvcTestHost+dclCertSvcRootListPath, dclCertSvcRootList())
			f.add(dclCertSvcGitHubPAADir, []any{})
		}
		if store == nil {
			store = &MemoryStore{}
		}
		return dclCertSvcOpen(t, f, CertificateOptions{Store: store, FetchTestCertificates: fetchTest, FetchGitHubCertificates: fetchTest, AcceptTestCertificates: accept})
	}
	none := GetCertificateOptions{}
	t.Run("hides cached test certificates from getCertificate when fetchTestCertificates is disabled", func(t *testing.T) {
		t.Parallel()
		s := open(t, seed(t), false, nil)
		for _, skid := range []string{dclCertSvcNoVIDSKID, dclCertSvcFFF1SKID} {
			if _, ok := s.Certificate(skid, none); ok {
				t.Fatalf("%s visible", skid)
			}
		}
		if n := len(s.Certificates()); n != 2 {
			t.Fatalf("%d certificates enumerated", n)
		}
	})
	t.Run("returns cached test certificates again when fetchTestCertificates is re-enabled", func(t *testing.T) {
		t.Parallel()
		s := open(t, seed(t), true, nil)
		for _, skid := range []string{dclCertSvcNoVIDSKID, dclCertSvcFFF1SKID} {
			if p, ok := dclCertSvcIsProduction(s, skid, none); !ok || p {
				t.Fatalf("%s: production %v found %v", skid, p, ok)
			}
		}
	})
	t.Run("rejects DER/PEM lookups for hidden test certificates", func(t *testing.T) {
		t.Parallel()
		s := open(t, seed(t), false, nil)
		if _, err := s.CertificateDER(ctx, dclCertSvcNoVIDSKID, none); !errors.Is(err, ErrCertificateNotFound) {
			t.Fatalf("DER: %v", err)
		}
		if _, err := s.CertificatePEM(dclCertSvcNoVIDSKID, none); !errors.Is(err, ErrCertificateNotFound) {
			t.Fatalf("PEM: %v", err)
		}
	})
	t.Run("does not affect production certificates", func(t *testing.T) {
		t.Parallel()
		f := &dclCertSvcFetch{}
		f.addMocks(dclCertSvcProdHost)
		s := dclCertSvcOpen(t, f, CertificateOptions{})
		for _, skid := range []string{dclCertSvcNoVIDSKID, dclCertSvcFFF1SKID} {
			if p, ok := dclCertSvcIsProduction(s, skid, none); !ok || !p {
				t.Fatalf("%s: production %v found %v", skid, p, ok)
			}
		}
	})

	// The nested group considerTestCertificates per-call override.
	t.Run("returns cached test certificate when override is true and fetchTestCertificates is disabled", func(t *testing.T) {
		t.Parallel()
		s := open(t, seed(t), false, nil)
		if _, ok := s.Certificate(dclCertSvcNoVIDSKID, none); ok {
			t.Fatal("visible without the override")
		}
		if p, ok := dclCertSvcIsProduction(s, dclCertSvcNoVIDSKID, dclCertSvcConsider(true)); !ok || p {
			t.Fatalf("production %v found %v", p, ok)
		}
	})
	t.Run("filters cached test certificate when override is false even if fetchTestCertificates is enabled", func(t *testing.T) {
		t.Parallel()
		s := open(t, seed(t), true, nil)
		if _, ok := s.Certificate(dclCertSvcNoVIDSKID, none); !ok {
			t.Fatal("hidden without the override")
		}
		if _, ok := s.Certificate(dclCertSvcNoVIDSKID, dclCertSvcConsider(false)); ok {
			t.Fatal("visible with the override false")
		}
	})
	t.Run("getCertificateAsDer returns DER when override is true", func(t *testing.T) {
		t.Parallel()
		s := open(t, seed(t), false, nil)
		if _, err := s.CertificateDER(ctx, dclCertSvcNoVIDSKID, none); !errors.Is(err, ErrCertificateNotFound) {
			t.Fatalf("without the override: %v", err)
		}
		der, err := s.CertificateDER(ctx, dclCertSvcNoVIDSKID, dclCertSvcConsider(true))
		if err != nil || len(der) == 0 {
			t.Fatalf("%d bytes %v", len(der), err)
		}
	})
	t.Run("getCertificateAsDer throws when explicit override is false even if fetchTestCertificates is on", func(t *testing.T) {
		t.Parallel()
		s := open(t, seed(t), true, nil)
		if _, err := s.CertificateDER(ctx, dclCertSvcNoVIDSKID, dclCertSvcConsider(false)); !errors.Is(err, ErrCertificateNotFound) {
			t.Fatalf("%v", err)
		}
	})
	t.Run("getCertificateAsPem returns PEM when override is true", func(t *testing.T) {
		t.Parallel()
		s := open(t, seed(t), false, nil)
		if _, err := s.CertificatePEM(dclCertSvcNoVIDSKID, none); !errors.Is(err, ErrCertificateNotFound) {
			t.Fatalf("without the override: %v", err)
		}
		p, err := s.CertificatePEM(dclCertSvcNoVIDSKID, dclCertSvcConsider(true))
		if err != nil || !strings.Contains(p, "-----BEGIN CERTIFICATE-----") {
			t.Fatalf("%q %v", p, err)
		}
	})
	t.Run("getOrFetchCertificate respects the considerTestCertificates override on local hits", func(t *testing.T) {
		t.Parallel()
		s := open(t, seed(t), false, nil)
		if _, ok := s.GetOrFetchCertificate(ctx, dclCertSvcNoVIDSKID, GetOrFetchCertificateOptions{}); ok {
			t.Fatal("visible without the override")
		}
		m, ok := s.GetOrFetchCertificate(ctx, dclCertSvcNoVIDSKID, GetOrFetchCertificateOptions{GetCertificateOptions: dclCertSvcConsider(true)})
		if !ok || m.IsProduction {
			t.Fatalf("%+v %v", m, ok)
		}
	})
	t.Run("getOrFetchCertificate filters a post-fetch test cert per the considerTestCertificates override", func(t *testing.T) {
		t.Parallel()
		f := &dclCertSvcFetch{}
		f.add(dclCertSvcProdHost+dclCertSvcRootListPath, dclCertSvcRootList())
		f.add(dclCertSvcTestHost+dclCertSvcRootListPath, dclCertSvcMockList())
		f.add(dclCertSvcTestHost+dclCertSvcNoVIDPath, dclCertSvcNoVID())
		s := dclCertSvcOpen(t, f, CertificateOptions{UpdateInterval: -1})
		if _, ok := s.Certificate(dclCertSvcNoVIDSKID, dclCertSvcConsider(true)); ok {
			t.Fatal("held before the fetch")
		}
		test := dclCertSvcBool(false)
		if _, ok := s.GetOrFetchCertificate(ctx, dclCertSvcNoVIDSKID, GetOrFetchCertificateOptions{IsProduction: test}); ok {
			t.Fatal("a fetched test certificate passed the policy")
		}
		m, ok := s.GetOrFetchCertificate(ctx, dclCertSvcNoVIDSKID, GetOrFetchCertificateOptions{IsProduction: test, GetCertificateOptions: dclCertSvcConsider(true)})
		if !ok || m.IsProduction {
			t.Fatalf("%+v %v", m, ok)
		}
	})

	// The nested group acceptTestCertificates trust policy.
	t.Run("defaults acceptsTestCertificates to false when neither flag is set", func(t *testing.T) {
		t.Parallel()
		if open(t, nil, false, nil).AcceptsTestCertificates() {
			t.Fatal("accepts")
		}
	})
	t.Run("defaults acceptsTestCertificates to fetchTestCertificates when only fetch is set", func(t *testing.T) {
		t.Parallel()
		if !open(t, nil, true, nil).AcceptsTestCertificates() {
			t.Fatal("does not accept")
		}
	})
	t.Run("accepts (and makes visible) test certificates without fetching them", func(t *testing.T) {
		t.Parallel()
		s := open(t, seed(t), false, dclCertSvcBool(true))
		if !s.AcceptsTestCertificates() {
			t.Fatal("does not accept")
		}
		for _, skid := range []string{dclCertSvcNoVIDSKID, dclCertSvcFFF1SKID} {
			if p, ok := dclCertSvcIsProduction(s, skid, none); !ok || p {
				t.Fatalf("%s: production %v found %v", skid, p, ok)
			}
		}
	})
	t.Run("fetches but does not accept test certificates when acceptTestCertificates is false", func(t *testing.T) {
		t.Parallel()
		s := open(t, seed(t), true, dclCertSvcBool(false))
		if s.AcceptsTestCertificates() {
			t.Fatal("accepts")
		}
		if _, ok := s.Certificate(dclCertSvcNoVIDSKID, none); ok {
			t.Fatal("visible to the default lookup")
		}
		if p, ok := dclCertSvcIsProduction(s, dclCertSvcNoVIDSKID, dclCertSvcConsider(true)); !ok || p {
			t.Fatalf("production %v found %v", p, ok)
		}
	})
}

// TestDclCertificateServiceGitHubRateLimitLogging ports describe("GitHub
// rate-limit logging"); GitHub is turned on as matter.js's default has it.
func TestDclCertificateServiceGitHubRateLimitLogging(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	// open is mockDclWithGithubRateLimited (:2186-2193) and the service.
	open := func(t *testing.T) (*CertificateStore, *dclCertSvcFetch, *dclCertSvcLogs) {
		t.Helper()
		f := &dclCertSvcFetch{}
		f.add(dclCertSvcProdHost+dclCertSvcRootListPath, dclCertSvcRootList())
		f.add(dclCertSvcTestHost+dclCertSvcRootListPath, dclCertSvcRootList())
		f.add("api.github.com", map[string]any{}, 403)
		logs := &dclCertSvcLogs{}
		return dclCertSvcOpen(t, f, CertificateOptions{FetchTestCertificates: true, FetchGitHubCertificates: true, Logger: slog.New(logs)}), f, logs
	}
	level := func(t *testing.T, s *CertificateStore, logs *dclCertSvcLogs) slog.Level {
		t.Helper()
		logs.reset()
		s.Update(ctx, true)
		r, ok := logs.find("Failed to fetch certificates from GitHub")
		if !ok {
			t.Fatal("no GitHub failure logged")
		}
		return r.level
	}
	t.Run("logs the GitHub fetch failure at info when no test certificate is cached", func(t *testing.T) {
		t.Parallel()
		s, _, logs := open(t)
		if l := level(t, s, logs); l != slog.LevelInfo {
			t.Fatalf("level %v", l)
		}
	})
	t.Run("logs the GitHub fetch failure at debug when a test certificate is already cached", func(t *testing.T) {
		t.Parallel()
		s, _, logs := open(t)
		if _, err := s.AddCertificate(ctx, secattestation.TestPAANoVIDCert, KindPAA, false); err != nil {
			t.Fatal(err)
		}
		if l := level(t, s, logs); l != slog.LevelDebug {
			t.Fatalf("level %v", l)
		}
	})
	t.Run("logs the GitHub fetch failure at info when only a production certificate is cached", func(t *testing.T) {
		t.Parallel()
		s, _, logs := open(t)
		if _, err := s.AddCertificate(ctx, secattestation.TestPAANoVIDCert, KindPAA, true); err != nil {
			t.Fatal(err)
		}
		if l := level(t, s, logs); l != slog.LevelInfo {
			t.Fatalf("level %v", l)
		}
	})
	t.Run("skips the remaining GitHub fetches for the run after a rate-limit response", func(t *testing.T) {
		t.Parallel()
		s, f, _ := open(t)
		if n := f.called("api.github.com"); n != 1 {
			t.Fatalf("%d GitHub requests", n)
		}
		f.clear()
		s.Update(ctx, true)
		if n := f.called("api.github.com"); n != 1 {
			t.Fatalf("%d GitHub requests in the next run", n)
		}
	})
}
