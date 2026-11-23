// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package dcl

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // key identifiers are SHA-1 of the key, as chip derives them
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/clock/clocktest"
)

var (
	oidVID = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 37244, 2, 1}
	oidPID = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 37244, 2, 2}
	oidCN  = asn1.ObjectIdentifier{2, 5, 4, 3}
)

func keyID(pub *ecdsa.PublicKey) []byte {
	b, _ := pub.Bytes()
	s := sha1.Sum(b) //nolint:gosec // see the import
	return s[:]
}

func matterName(cn string, vid, pid *uint16) pkix.Name {
	extra := []pkix.AttributeTypeAndValue{{Type: oidCN, Value: cn}}
	if vid != nil {
		extra = append(extra, pkix.AttributeTypeAndValue{Type: oidVID, Value: fmt.Sprintf("%04X", *vid)})
	}
	if pid != nil {
		extra = append(extra, pkix.AttributeTypeAndValue{Type: oidPID, Value: fmt.Sprintf("%04X", *pid)})
	}
	return pkix.Name{ExtraNames: extra}
}

func u16(v uint16) *uint16 { return &v }

// testCert is a minted certificate and its key.
type testCert struct {
	der  []byte
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
}

func (c testCert) skid() []byte    { return c.cert.SubjectKeyId }
func (c testCert) skidHex() string { return hexUpper(c.cert.SubjectKeyId) }

func (c testCert) pem() string {
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c.der}))
}

// certSpec describes a certificate to mint.
type certSpec struct {
	cn       string
	vid, pid *uint16
	ca       bool
	usage    x509.KeyUsage
	serial   int64
	akid     []byte
	skid     []byte
	key      *ecdsa.PrivateKey
	noAKID   bool
	notAfter time.Time
	// pathLen0 sets a CA's pathLen to 0, as the PAI profile requires.
	pathLen0 bool
}

// mint signs spec with parent (nil: self-signed).
func mint(t *testing.T, spec certSpec, parent *testCert) testCert {
	t.Helper()
	key := spec.key
	if key == nil {
		key, _ = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	}
	skid := spec.skid
	if skid == nil {
		skid = keyID(&key.PublicKey)
	}
	usage := spec.usage
	if usage == 0 {
		usage = x509.KeyUsageCertSign | x509.KeyUsageCRLSign
	}
	serial := spec.serial
	if serial == 0 {
		serial = 0x1001
	}
	notAfter := spec.notAfter
	if notAfter.IsZero() {
		notAfter = time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(serial),
		Subject:               matterName(spec.cn, spec.vid, spec.pid),
		NotBefore:             time.Date(2022, 1, 1, 0, 0, 0, 0, time.UTC),
		NotAfter:              notAfter,
		KeyUsage:              usage,
		BasicConstraintsValid: true,
		IsCA:                  spec.ca,
		MaxPathLenZero:        spec.pathLen0,
		SubjectKeyId:          skid,
		SignatureAlgorithm:    x509.ECDSAWithSHA256,
	}
	signer, parentCert := key, tmpl
	if parent != nil {
		signer, parentCert = parent.key, parent.cert
	}
	akid := spec.akid
	if akid == nil && !spec.noAKID {
		akid = parentCert.SubjectKeyId
	}
	tmpl.AuthorityKeyId = akid
	// crypto/x509 takes the AKID from the parent's SKID: a parent copy
	// carries the override.
	p := *parentCert
	p.SubjectKeyId = akid
	der, err := x509.CreateCertificate(rand.Reader, tmpl, &p, &key.PublicKey, signer)
	if err != nil {
		t.Fatal(err)
	}
	c, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return testCert{der: der, cert: c, key: key}
}

// fakeDCL is the ledger's REST API with DclRestApiTypes.ts's shapes, plus
// the CRL distribution points and a GitHub tree.
type fakeDCL struct {
	t   *testing.T
	srv *httptest.Server

	mu       sync.Mutex
	hits     map[string]int
	routes   map[string]func(w http.ResponseWriter, r *http.Request)
	roots    []fakeRoot
	delay    time.Duration
	failPath map[string]int
}

type fakeRoot struct {
	cert    testCert
	subject string
}

func newFakeDCL(t *testing.T) *fakeDCL {
	t.Helper()
	f := &fakeDCL{t: t, hits: map[string]int{}, routes: map[string]func(http.ResponseWriter, *http.Request){}, failPath: map[string]int{}}
	f.srv = httptest.NewTLSServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeDCL) client() *Client {
	return &Client{BaseURL: f.srv.URL, HTTPClient: f.srv.Client()}
}

func (f *fakeDCL) url(path string) string { return f.srv.URL + path }

func (f *fakeDCL) hit(path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hits[path]
}

// route answers a path (RequestURI) with fn.
func (f *fakeDCL) route(path string, fn func(w http.ResponseWriter, r *http.Request)) {
	f.mu.Lock()
	f.routes[path] = fn
	f.mu.Unlock()
}

// json answers a path with a JSON body.
func (f *fakeDCL) json(path string, status int, body any) {
	f.route(path, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	})
}

// bytes answers a path with raw bytes.
func (f *fakeDCL) bytes(path string, body []byte) {
	f.route(path, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(body) })
}

func (f *fakeDCL) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.hits[r.URL.RequestURI()]++
	fn := f.routes[r.URL.RequestURI()]
	delay := f.delay
	status := f.failPath[r.URL.RequestURI()]
	f.mu.Unlock()
	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-r.Context().Done():
			return
		}
	}
	if status != 0 {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"code":13,"message":"internal","details":[]}`))
		return
	}
	if fn == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"code":5,"message":"not found","details":[]}`))
		return
	}
	fn(w, r)
}

func colonHex(b []byte) string {
	h := hexUpper(b)
	var parts []string
	for i := 0; i < len(h); i += 2 {
		parts = append(parts, h[i:i+2])
	}
	return strings.Join(parts, ":")
}

func certRecord(c testCert, subject string, root bool) map[string]any {
	return map[string]any{
		"pemCert": c.pem(), "serialNumber": c.cert.SerialNumber.String(), "issuer": "", "authorityKeyId": "",
		"rootSubject": "", "rootSubjectKeyId": "", "isRoot": root, "owner": "cosmos1owner",
		"subject": subject, "subjectAsText": c.cert.Subject.String(), "subjectKeyId": colonHex(c.skid()),
		"approvals": []any{}, "rejects": []any{}, "certificateType": "DeviceAttestationPKI", "vid": 0, "schemaVersion": 0,
	}
}

// addRoot publishes c as an approved root: in the root list and under its
// subject and key identifier.
func (f *fakeDCL) addRoot(c testCert) {
	subject := base64.StdEncoding.EncodeToString(c.cert.RawSubject)
	f.mu.Lock()
	f.roots = append(f.roots, fakeRoot{cert: c, subject: subject})
	roots := append([]fakeRoot(nil), f.roots...)
	f.mu.Unlock()
	certs := make([]map[string]any, 0, len(roots))
	for _, r := range roots {
		certs = append(certs, map[string]any{"subject": r.subject, "subjectKeyId": colonHex(r.cert.skid())})
	}
	f.json("/dcl/pki/root-certificates", 200, map[string]any{
		"approvedRootCertificates": map[string]any{"certs": certs, "schemaVersion": 0},
	})
	f.json("/dcl/pki/certificates/"+encodeURIComponent(subject)+"/"+encodeURIComponent(colonHex(c.skid())), 200, map[string]any{
		"approvedCertificates": map[string]any{
			"subject": subject, "subjectKeyId": colonHex(c.skid()), "schemaVersion": 0,
			"certs": []any{certRecord(c, subject, true)},
		},
	})
}

// addBySKID publishes c under /dcl/pki/all-certificates?subjectKeyId=.
func (f *fakeDCL) addBySKID(c testCert) {
	subject := base64.StdEncoding.EncodeToString(c.cert.RawSubject)
	f.json("/dcl/pki/all-certificates?subjectKeyId="+encodeURIComponent(colonHex(c.skid())), 200, map[string]any{
		"certificates": []any{map[string]any{
			"subject": subject, "subjectKeyId": colonHex(c.skid()), "schemaVersion": 0,
			"certs": []any{certRecord(c, subject, false)},
		}},
	})
}

// manualClock is the test clock, a [clocktest.Fake]: timers fire when the
// test advances it.
type manualClock struct{ *clocktest.Fake }

func newManualClock() *manualClock {
	return &manualClock{clocktest.New(time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC))}
}

// advance moves the clock by d and runs the timers due, in order.
func (c *manualClock) advance(d time.Duration) { c.Advance(d) }

// testPKI is a PAA, a PAI under it and a DAC under the PAI.
type testPKI struct {
	paa, pai, dac testCert
}

func newTestPKI(t *testing.T) testPKI {
	t.Helper()
	paa := mint(t, certSpec{cn: "Test PAA", vid: u16(0xFFF1), ca: true, noAKID: true}, nil)
	pai := mint(t, certSpec{cn: "Test PAI", vid: u16(0xFFF1), pid: u16(0x8000), ca: true, serial: 0x2002}, &paa)
	dac := mint(t, certSpec{cn: "Test DAC", vid: u16(0xFFF1), pid: u16(0x8000), usage: x509.KeyUsageDigitalSignature, serial: 0xC0FFEE}, &pai)
	return testPKI{paa: paa, pai: pai, dac: dac}
}

// hexUpper is the key identifier in the ledger's uppercase hex.
func hexUpper(b []byte) string { return strings.ToUpper(hex.EncodeToString(b)) }
