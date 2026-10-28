// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// TlsCertificateManagement: a port of connectedhomeip
// src/app/clusters/tls-certificate-management-server/tests/
// TestTLSCertificateManagementCluster.cpp at 6170af84, by meaning. chip's
// ClusterTester invokes on the test fabric (151, :471); here the server's
// MatterInvoke runs on a peer context of that fabric. chip's
// MockTLSCertificateManagementDelegate and MockCertificateTable (:72-468)
// hold the tables in the test; here the server holds them, and mockKeys is
// the CertificatePort. The mock hands out ids from 1 (:229-230); the
// server's ids are chip's IncrementingIdHelper's, from 0.
package tls_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/spec"
	tcmdef "github.com/SukramJ/go-fabric/cluster/spec/tlscertificatemanagement"
	"github.com/SukramJ/go-fabric/cluster/tls"
	"github.com/SukramJ/go-fabric/im"
)

// The fixture (:42-44, :471).
const (
	testFabric    uint8  = 151
	testMaxRoot          = 10
	testMaxClient        = 10
	testPeerNode  uint64 = 0x1122
	otherFabric   uint8  = 152
)

// mockKeys is the CertificatePort: dummy CSRs and signatures as chip's
// mock GenerateClientCsr (:282-305), every certificate valid, every key a
// match, the removed keys recorded.
type mockKeys struct {
	mu      sync.Mutex
	invalid map[string]bool
	csrErr  error
	removed []uint16
	csrs    int
}

func (m *mockKeys) GenerateCSR(_ context.Context, _ uint8, _ uint16, _ bool, _ []byte) (csr, sig []byte, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.csrErr != nil {
		return nil, nil, m.csrErr
	}
	m.csrs++
	csr = make([]byte, 32)
	csr[0], csr[1], csr[2] = 1, 2, 3
	sig = make([]byte, 32)
	sig[0], sig[1], sig[2] = 4, 5, 6
	return csr, sig, nil
}

func (m *mockKeys) CertificateValid(_ context.Context, cert []byte) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return !m.invalid[string(cert)]
}

func (m *mockKeys) ClientKeyMatches(context.Context, uint8, uint16, []byte) bool { return true }

func (m *mockKeys) RemoveClientKey(_ context.Context, _ uint8, ccdid uint16) {
	m.mu.Lock()
	m.removed = append(m.removed, ccdid)
	m.mu.Unlock()
}

// testCertificate is chip's GenerateTestCertificate (:46-70): a
// self-signed certificate valid from the CHIP epoch on, with a serial
// that makes each one distinct.
func testCertificate(tb testing.TB, serial int64) []byte {
	tb.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		tb.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(serial),
		Subject:      pkix.Name{CommonName: "1234ABCD"},
		NotBefore:    time.Date(2000, 1, 1, 0, 0, 1, 0, time.UTC),
		NotAfter:     time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		tb.Fatal(err)
	}
	return der
}

func peer(fabric uint8) context.Context {
	return im.WithSubject(im.WithFabricFilter(context.Background(), false, fabric), testPeerNode, nil)
}

func statusOf(err error) im.StatusCode {
	if err == nil {
		return im.StatusSuccess
	}
	var sc im.StatusCodeError
	if errors.As(err, &sc) {
		return sc.MatterStatusCode()
	}
	return im.StatusFailure
}

func wantStatus(t *testing.T, what string, err error, want im.StatusCode) {
	t.Helper()
	if got := statusOf(err); got != want {
		t.Fatalf("%s: status %s (%v), want %s", what, got, err, want)
	}
}

func newCerts(t *testing.T, edit ...func(*tls.CertificateManagementConfig)) (*tls.CertificateManagement, *mockKeys) {
	t.Helper()
	keys := &mockKeys{invalid: map[string]bool{}}
	cfg := tls.CertificateManagementConfig{MaxRootCertificates: testMaxRoot, MaxClientCertificates: testMaxClient, Port: keys}
	for _, e := range edit {
		e(&cfg)
	}
	c, err := tls.NewCertificateManagement(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	return c, keys
}

func readAs[T any](t *testing.T, read func(uint32) (any, bool), attrID uint32) T {
	t.Helper()
	v, ok := read(attrID)
	if !ok {
		t.Fatalf("attribute 0x%04X not readable", attrID)
	}
	var got T
	if !spec.Assign(&got, v) {
		t.Fatalf("attribute 0x%04X is %T", attrID, v)
	}
	return got
}

func invoke[R any](ctx context.Context, t *testing.T, c interface {
	MatterInvoke(context.Context, uint32, any) (any, error)
}, cmd uint32, req any,
) (R, error) {
	t.Helper()
	resp, err := c.MatterInvoke(ctx, cmd, req)
	var r R
	if err == nil && resp != nil {
		var ok bool
		if r, ok = resp.(R); !ok {
			t.Fatalf("response %T", resp)
		}
	}
	return r, err
}

func provisionRoot(t *testing.T, c *tls.CertificateManagement, cert []byte) uint16 {
	t.Helper()
	resp, err := invoke[tcmdef.ProvisionRootCertificateResponse](peer(testFabric), t, c, tcmdef.CmdProvisionRootCertificate,
		tcmdef.ProvisionRootCertificateRequest{Certificate: cert, Caid: spec.NullOf[uint16]()})
	wantStatus(t, "ProvisionRootCertificate", err, im.StatusSuccess)
	return resp.Caid
}

func clientCSR(t *testing.T, c *tls.CertificateManagement) tcmdef.ClientCsrResponse {
	t.Helper()
	nonce := make([]byte, 32)
	nonce[0], nonce[1], nonce[2] = 1, 2, 3
	resp, err := invoke[tcmdef.ClientCsrResponse](peer(testFabric), t, c, tcmdef.CmdClientCsr,
		tcmdef.ClientCsrRequest{Nonce: nonce, Ccdid: spec.NullOf[uint16]()})
	wantStatus(t, "ClientCSR", err, im.StatusSuccess)
	return resp
}

// TestReadMaxRootCertificates (:493-498).
func TestReadMaxRootCertificates(t *testing.T) {
	c, _ := newCerts(t)
	if got := readAs[uint8](t, c.MatterRead, tcmdef.AttrMaxRootCertificates); got != testMaxRoot {
		t.Fatalf("MaxRootCertificates %d", got)
	}
}

// TestReadMaxClientCertificates (:500-505).
func TestReadMaxClientCertificates(t *testing.T) {
	c, _ := newCerts(t)
	if got := readAs[uint8](t, c.MatterRead, tcmdef.AttrMaxClientCertificates); got != testMaxClient {
		t.Fatalf("MaxClientCertificates %d", got)
	}
}

// TestReadClusterRevision (:507-512).
func TestReadClusterRevision(t *testing.T) {
	c, _ := newCerts(t)
	if got := readAs[uint16](t, c.MatterRead, 0xFFFD); got != tcmdef.Revision {
		t.Fatalf("ClusterRevision %d", got)
	}
}

// TestReadFeatureMap (:514-519).
func TestReadFeatureMap(t *testing.T) {
	c, _ := newCerts(t)
	if got := readAs[uint32](t, c.MatterRead, 0xFFFC); got != 0 {
		t.Fatalf("FeatureMap %d", got)
	}
}

// TestReadProvisionedRootCertificatesEmpty (:521-528).
func TestReadProvisionedRootCertificatesEmpty(t *testing.T) {
	c, _ := newCerts(t)
	if got := readAs[[]tcmdef.TLSCertStruct](t, c.MatterRead, tcmdef.AttrProvisionedRootCertificates); len(got) != 0 {
		t.Fatalf("ProvisionedRootCertificates %v", got)
	}
}

// TestReadProvisionedClientCertificatesEmpty (:530-537).
func TestReadProvisionedClientCertificatesEmpty(t *testing.T) {
	c, _ := newCerts(t)
	if got := readAs[[]tcmdef.TLSClientCertificateDetailStruct](t, c.MatterRead, tcmdef.AttrProvisionedClientCertificates); len(got) != 0 {
		t.Fatalf("ProvisionedClientCertificates %v", got)
	}
}

// TestProvisionRootCertificateSuccess (:539-578): the certificate is
// stored under the first CAID.
func TestProvisionRootCertificateSuccess(t *testing.T) {
	c, _ := newCerts(t)
	cert := testCertificate(t, 1)
	if caid := provisionRoot(t, c, cert); caid != 0 {
		t.Fatalf("CAID %d, want the first id 0", caid)
	}
	roots := readAs[[]tcmdef.TLSCertStruct](t, c.MatterRead, tcmdef.AttrProvisionedRootCertificates)
	if len(roots) != 1 || roots[0].Caid != 0 || roots[0].FabricIndex != testFabric || !bytes.Equal(*roots[0].Certificate, cert) {
		t.Fatalf("ProvisionedRootCertificates %+v", roots)
	}
}

// TestFindRootCertificateSuccess (:580-615).
func TestFindRootCertificateSuccess(t *testing.T) {
	c, _ := newCerts(t)
	cert := testCertificate(t, 1)
	caid := provisionRoot(t, c, cert)
	resp, err := invoke[tcmdef.FindRootCertificateResponse](peer(testFabric), t, c, tcmdef.CmdFindRootCertificate,
		tcmdef.FindRootCertificateRequest{Caid: spec.ValueOf(caid)})
	wantStatus(t, "FindRootCertificate", err, im.StatusSuccess)
	if len(resp.CertificateDetails) != 1 || resp.CertificateDetails[0].Caid != caid || !bytes.Equal(*resp.CertificateDetails[0].Certificate, cert) {
		t.Fatalf("FindRootCertificateResponse %+v", resp)
	}
}

// TestFindRootCertificateNotFound (:617-628).
func TestFindRootCertificateNotFound(t *testing.T) {
	c, _ := newCerts(t)
	_, err := c.MatterInvoke(peer(testFabric), tcmdef.CmdFindRootCertificate, tcmdef.FindRootCertificateRequest{Caid: spec.ValueOf[uint16](999)})
	wantStatus(t, "FindRootCertificate", err, im.StatusNotFound)
}

// TestRemoveRootCertificateSuccess (:630-662).
func TestRemoveRootCertificateSuccess(t *testing.T) {
	c, _ := newCerts(t)
	caid := provisionRoot(t, c, testCertificate(t, 1))
	_, err := c.MatterInvoke(peer(testFabric), tcmdef.CmdRemoveRootCertificate, tcmdef.RemoveRootCertificateRequest{Caid: caid})
	wantStatus(t, "RemoveRootCertificate", err, im.StatusSuccess)
	if roots := readAs[[]tcmdef.TLSCertStruct](t, c.MatterRead, tcmdef.AttrProvisionedRootCertificates); len(roots) != 0 {
		t.Fatalf("ProvisionedRootCertificates %+v", roots)
	}
}

// TestRemoveRootCertificateNotFound (:664-675).
func TestRemoveRootCertificateNotFound(t *testing.T) {
	c, _ := newCerts(t)
	_, err := c.MatterInvoke(peer(testFabric), tcmdef.CmdRemoveRootCertificate, tcmdef.RemoveRootCertificateRequest{Caid: 999})
	wantStatus(t, "RemoveRootCertificate", err, im.StatusNotFound)
}

// TestGenerateClientCsrSuccess (:677-692): a CSR, a signature and the
// first CCDID.
func TestGenerateClientCsrSuccess(t *testing.T) {
	c, _ := newCerts(t)
	resp := clientCSR(t, c)
	if len(resp.Csr) == 0 || len(resp.NonceSignature) == 0 || resp.Ccdid != 0 {
		t.Fatalf("ClientCsrResponse %+v", resp)
	}
}

// TestProvisionClientCertificateSuccess (:694-730).
func TestProvisionClientCertificateSuccess(t *testing.T) {
	c, _ := newCerts(t)
	ccdid := clientCSR(t, c).Ccdid
	cert := testCertificate(t, 2)
	_, err := c.MatterInvoke(peer(testFabric), tcmdef.CmdProvisionClientCertificate,
		tcmdef.ProvisionClientCertificateRequest{Ccdid: ccdid, ClientCertificate: cert, IntermediateCertificates: [][]byte{}})
	wantStatus(t, "ProvisionClientCertificate", err, im.StatusSuccess)
	got, ok := c.ClientCertificate(testFabric, ccdid)
	if !ok || got.ClientCertificate.Null || !bytes.Equal(got.ClientCertificate.Value, cert) {
		t.Fatalf("client certificate %+v %v", got, ok)
	}
}

// TestFindClientCertificateSuccess (:732-776).
func TestFindClientCertificateSuccess(t *testing.T) {
	c, _ := newCerts(t)
	ccdid := clientCSR(t, c).Ccdid
	cert := testCertificate(t, 2)
	_, err := c.MatterInvoke(peer(testFabric), tcmdef.CmdProvisionClientCertificate,
		tcmdef.ProvisionClientCertificateRequest{Ccdid: ccdid, ClientCertificate: cert, IntermediateCertificates: [][]byte{}})
	wantStatus(t, "ProvisionClientCertificate", err, im.StatusSuccess)
	resp, err := invoke[tcmdef.FindClientCertificateResponse](peer(testFabric), t, c, tcmdef.CmdFindClientCertificate,
		tcmdef.FindClientCertificateRequest{Ccdid: spec.ValueOf(ccdid)})
	wantStatus(t, "FindClientCertificate", err, im.StatusSuccess)
	if len(resp.CertificateDetails) != 1 || resp.CertificateDetails[0].Ccdid != ccdid ||
		!bytes.Equal(resp.CertificateDetails[0].ClientCertificate.Value, cert) {
		t.Fatalf("FindClientCertificateResponse %+v", resp)
	}
}

// TestFindClientCertificateNotFound (:778-789).
func TestFindClientCertificateNotFound(t *testing.T) {
	c, _ := newCerts(t)
	_, err := c.MatterInvoke(peer(testFabric), tcmdef.CmdFindClientCertificate, tcmdef.FindClientCertificateRequest{Ccdid: spec.ValueOf[uint16](999)})
	wantStatus(t, "FindClientCertificate", err, im.StatusNotFound)
}

// TestRemoveClientCertificateSuccess (:791-832): the entry leaves and the
// port drops its key.
func TestRemoveClientCertificateSuccess(t *testing.T) {
	c, keys := newCerts(t)
	ccdid := clientCSR(t, c).Ccdid
	_, err := c.MatterInvoke(peer(testFabric), tcmdef.CmdProvisionClientCertificate,
		tcmdef.ProvisionClientCertificateRequest{Ccdid: ccdid, ClientCertificate: testCertificate(t, 2), IntermediateCertificates: [][]byte{}})
	wantStatus(t, "ProvisionClientCertificate", err, im.StatusSuccess)
	_, err = c.MatterInvoke(peer(testFabric), tcmdef.CmdRemoveClientCertificate, tcmdef.RemoveClientCertificateRequest{Ccdid: ccdid})
	wantStatus(t, "RemoveClientCertificate", err, im.StatusSuccess)
	if _, ok := c.ClientCertificate(testFabric, ccdid); ok {
		t.Fatal("the client certificate is still held")
	}
	if len(keys.removed) != 1 || keys.removed[0] != ccdid {
		t.Fatalf("removed keys %v", keys.removed)
	}
}

// TestRemoveClientCertificateNotFound (:834-845).
func TestRemoveClientCertificateNotFound(t *testing.T) {
	c, _ := newCerts(t)
	_, err := c.MatterInvoke(peer(testFabric), tcmdef.CmdRemoveClientCertificate, tcmdef.RemoveClientCertificateRequest{Ccdid: 999})
	wantStatus(t, "RemoveClientCertificate", err, im.StatusNotFound)
}
