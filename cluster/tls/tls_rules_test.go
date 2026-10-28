// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// The rules of the TLS servers no chip case reaches, each against the
// connectedhomeip line it checks (TLSCertificateManagementCluster.cpp,
// TLSClientManagementCluster.cpp and the example delegates at 6170af84).
package tls_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	tcmdef "github.com/SukramJ/go-fabric/cluster/spec/tlscertificatemanagement"
	tcldef "github.com/SukramJ/go-fabric/cluster/spec/tlsclientmanagement"
	"github.com/SukramJ/go-fabric/cluster/tls"
	"github.com/SukramJ/go-fabric/im"
)

// mapStore is a Store in memory.
type mapStore struct {
	mu      sync.Mutex
	m       map[string]string
	failSet bool
}

func (s *mapStore) GetSetting(_ context.Context, key string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[key]
	return v, ok, nil
}

func (s *mapStore) SetSetting(_ context.Context, key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failSet {
		return errors.New("disk full")
	}
	if s.m == nil {
		s.m = map[string]string{}
	}
	s.m[key] = value
	return nil
}

func TestConfigRefusals(t *testing.T) {
	keys := &mockKeys{}
	for _, cfg := range []tls.CertificateManagementConfig{
		{MaxRootCertificates: 4, MaxClientCertificates: 5, Port: keys}, // :70
		{MaxRootCertificates: 5, MaxClientCertificates: 4, Port: keys}, // :71
	} {
		if _, err := tls.NewCertificateManagement(context.Background(), cfg); !errors.Is(err, tls.ErrCertificateConfig) {
			t.Fatalf("%+v: %v", cfg, err)
		}
	}
	if _, err := tls.NewCertificateManagement(context.Background(), tls.CertificateManagementConfig{MaxRootCertificates: 5, MaxClientCertificates: 5}); !errors.Is(err, tls.ErrNoCertificatePort) {
		t.Fatal(err)
	}
	for _, n := range []uint8{4, 255} { // :59-60
		if _, err := tls.NewClientManagement(context.Background(), tls.ClientManagementConfig{MaxProvisioned: n}); !errors.Is(err, tls.ErrClientConfig) {
			t.Fatalf("MaxProvisioned %d: %v", n, err)
		}
	}
}

func TestProvisionRootRules(t *testing.T) {
	c, keys := newCerts(t, func(cfg *tls.CertificateManagementConfig) { cfg.MaxRootCertificates = 5 })
	ctx := peer(testFabric)
	req := func(cert []byte, caid spec.Nullable[uint16]) tcmdef.ProvisionRootCertificateRequest {
		return tcmdef.ProvisionRootCertificateRequest{Certificate: cert, Caid: caid}
	}
	_, err := c.MatterInvoke(ctx, tcmdef.CmdProvisionRootCertificate, req(make([]byte, 3001), spec.NullOf[uint16]())) // :236
	wantStatus(t, "oversized", err, im.StatusConstraintError)
	cert := testCertificate(t, 1)
	caid := provisionRoot(t, c, cert)
	_, err = c.MatterInvoke(ctx, tcmdef.CmdProvisionRootCertificate, req(cert, spec.NullOf[uint16]())) // :245
	wantStatus(t, "duplicate", err, im.StatusAlreadyExists)
	_, err = c.MatterInvoke(ctx, tcmdef.CmdProvisionRootCertificate, req(testCertificate(t, 2), spec.ValueOf[uint16](65535))) // :256
	wantStatus(t, "CAID 65535", err, im.StatusConstraintError)
	_, err = c.MatterInvoke(ctx, tcmdef.CmdProvisionRootCertificate, req(testCertificate(t, 2), spec.ValueOf[uint16](77))) // :257
	wantStatus(t, "unknown CAID", err, im.StatusNotFound)
	bad := testCertificate(t, 3)
	keys.invalid[string(bad)] = true
	_, err = c.MatterInvoke(ctx, tcmdef.CmdProvisionRootCertificate, req(bad, spec.NullOf[uint16]())) // :263
	wantStatus(t, "invalid", err, im.StatusDynamicConstraintError)
	// An update replaces the entry under its CAID.
	next := testCertificate(t, 4)
	resp, err := invoke[tcmdef.ProvisionRootCertificateResponse](t, c, ctx, tcmdef.CmdProvisionRootCertificate, req(next, spec.ValueOf(caid)))
	wantStatus(t, "update", err, im.StatusSuccess)
	if got, _ := c.RootCertificate(testFabric, caid); resp.Caid != caid || string(*got.Certificate) != string(next) {
		t.Fatalf("update: CAID %d", resp.Caid)
	}
	for i := int64(10); i < 14; i++ {
		provisionRoot(t, c, testCertificate(t, i))
	}
	_, err = c.MatterInvoke(ctx, tcmdef.CmdProvisionRootCertificate, req(testCertificate(t, 20), spec.NullOf[uint16]())) // :251
	wantStatus(t, "capacity", err, im.StatusResourceExhausted)
	// The capacity is per fabric; the ids are the node's.
	resp, err = invoke[tcmdef.ProvisionRootCertificateResponse](t, c, peer(otherFabric), tcmdef.CmdProvisionRootCertificate, req(cert, spec.NullOf[uint16]()))
	wantStatus(t, "other fabric", err, im.StatusSuccess)
	if resp.Caid != 5 {
		t.Fatalf("other fabric's CAID %d, want 5", resp.Caid)
	}
}

func TestLookupAndFindRules(t *testing.T) {
	c, _ := newCerts(t)
	ctx := peer(testFabric)
	cert := testCertificate(t, 1)
	caid := provisionRoot(t, c, cert)
	sum := sha256.Sum256(cert)
	resp, err := invoke[tcmdef.LookupRootCertificateResponse](t, c, ctx, tcmdef.CmdLookupRootCertificate, tcmdef.LookupRootCertificateRequest{Fingerprint: sum[:]})
	wantStatus(t, "lookup", err, im.StatusSuccess)
	if resp.Caid != caid {
		t.Fatalf("lookup CAID %d", resp.Caid)
	}
	_, err = c.MatterInvoke(ctx, tcmdef.CmdLookupRootCertificate, tcmdef.LookupRootCertificateRequest{Fingerprint: make([]byte, 65)}) // :337
	wantStatus(t, "long fingerprint", err, im.StatusConstraintError)
	_, err = c.MatterInvoke(ctx, tcmdef.CmdLookupRootCertificate, tcmdef.LookupRootCertificateRequest{Fingerprint: make([]byte, 32)}) // :350
	wantStatus(t, "unknown fingerprint", err, im.StatusNotFound)
	_, err = c.MatterInvoke(peer(otherFabric), tcmdef.CmdLookupRootCertificate, tcmdef.LookupRootCertificateRequest{Fingerprint: sum[:]})
	wantStatus(t, "other fabric's lookup", err, im.StatusNotFound)
	all, err := invoke[tcmdef.FindRootCertificateResponse](t, c, ctx, tcmdef.CmdFindRootCertificate, tcmdef.FindRootCertificateRequest{Caid: spec.NullOf[uint16]()})
	wantStatus(t, "find all", err, im.StatusSuccess)
	if len(all.CertificateDetails) != 1 {
		t.Fatalf("find all %+v", all)
	}
	_, err = c.MatterInvoke(peer(otherFabric), tcmdef.CmdFindRootCertificate, tcmdef.FindRootCertificateRequest{Caid: spec.NullOf[uint16]()}) // :291-294
	wantStatus(t, "find none", err, im.StatusNotFound)
	_, err = c.MatterInvoke(ctx, tcmdef.CmdFindRootCertificate, tcmdef.FindRootCertificateRequest{Caid: spec.ValueOf[uint16](65535)}) // :308
	wantStatus(t, "find CAID 65535", err, im.StatusConstraintError)
	_, err = c.MatterInvoke(ctx, tcmdef.CmdRemoveRootCertificate, tcmdef.RemoveRootCertificateRequest{Caid: 65535}) // :363
	wantStatus(t, "remove CAID 65535", err, im.StatusConstraintError)
	_, err = c.MatterInvoke(ctx, tcmdef.CmdFindClientCertificate, tcmdef.FindClientCertificateRequest{Ccdid: spec.ValueOf[uint16](65535)}) // :484
	wantStatus(t, "find CCDID 65535", err, im.StatusConstraintError)
	_, err = c.MatterInvoke(ctx, tcmdef.CmdRemoveClientCertificate, tcmdef.RemoveClientCertificateRequest{Ccdid: 65535}) // :540
	wantStatus(t, "remove CCDID 65535", err, im.StatusConstraintError)
	_, err = c.MatterInvoke(ctx, tcmdef.CmdLookupClientCertificate, tcmdef.LookupClientCertificateRequest{Fingerprint: make([]byte, 65)}) // :513
	wantStatus(t, "client long fingerprint", err, im.StatusConstraintError)
}

func TestClientCSRRules(t *testing.T) {
	c, keys := newCerts(t, func(cfg *tls.CertificateManagementConfig) { cfg.MaxClientCertificates = 5 })
	ctx := peer(testFabric)
	_, err := c.MatterInvoke(ctx, tcmdef.CmdClientCsr, tcmdef.ClientCsrRequest{Nonce: make([]byte, 31), Ccdid: spec.NullOf[uint16]()}) // :383
	wantStatus(t, "short nonce", err, im.StatusConstraintError)
	_, err = c.MatterInvoke(ctx, tcmdef.CmdClientCsr, tcmdef.ClientCsrRequest{Nonce: make([]byte, 32), Ccdid: spec.ValueOf[uint16](9)}) // CertificateTableImpl.cpp:475
	wantStatus(t, "unknown CCDID", err, im.StatusFailure)
	first := clientCSR(t, c).Ccdid
	// A CSR of an existing CCDID adds no entry.
	resp, err := invoke[tcmdef.ClientCsrResponse](t, c, ctx, tcmdef.CmdClientCsr, tcmdef.ClientCsrRequest{Nonce: make([]byte, 32), Ccdid: spec.ValueOf(first)})
	wantStatus(t, "renew", err, im.StatusSuccess)
	if resp.Ccdid != first || len(readAs[[]tcmdef.TLSClientCertificateDetailStruct](t, c.MatterRead, tcmdef.AttrProvisionedClientCertificates)) != 1 {
		t.Fatalf("renew %+v", resp)
	}
	keys.csrErr = spec.Errorf(im.StatusBusy, "busy")
	_, err = c.MatterInvoke(ctx, tcmdef.CmdClientCsr, tcmdef.ClientCsrRequest{Nonce: make([]byte, 32), Ccdid: spec.NullOf[uint16]()})
	wantStatus(t, "port status", err, im.StatusBusy)
	keys.csrErr = errors.New("no key")
	_, err = c.MatterInvoke(ctx, tcmdef.CmdClientCsr, tcmdef.ClientCsrRequest{Nonce: make([]byte, 32), Ccdid: spec.NullOf[uint16]()})
	wantStatus(t, "port error", err, im.StatusFailure)
	keys.csrErr = nil
	if next := clientCSR(t, c).Ccdid; next != first+1 {
		t.Fatalf("a failed CSR took an id: %d", next)
	}
	for range 3 {
		clientCSR(t, c)
	}
	_, err = c.MatterInvoke(ctx, tcmdef.CmdClientCsr, tcmdef.ClientCsrRequest{Nonce: make([]byte, 32), Ccdid: spec.NullOf[uint16]()}) // :392
	wantStatus(t, "capacity", err, im.StatusResourceExhausted)
}

type mismatchedKeys struct{ *mockKeys }

func (mismatchedKeys) ClientKeyMatches(context.Context, uint8, uint16, []byte) bool { return false }

func TestProvisionClientRules(t *testing.T) {
	c, keys := newCerts(t)
	ctx := peer(testFabric)
	ccdid := clientCSR(t, c).Ccdid
	req := func(cert []byte, inter [][]byte) tcmdef.ProvisionClientCertificateRequest {
		return tcmdef.ProvisionClientCertificateRequest{Ccdid: ccdid, ClientCertificate: cert, IntermediateCertificates: inter}
	}
	cases := []struct {
		name string
		req  tcmdef.ProvisionClientCertificateRequest
		want im.StatusCode
	}{
		{"CCDID 65535", tcmdef.ProvisionClientCertificateRequest{Ccdid: 65535, ClientCertificate: []byte{1}}, im.StatusConstraintError},     // :419
		{"oversized", req(make([]byte, 3001), nil), im.StatusConstraintError},                                                               // :420
		{"eleven intermediates", req(testCertificate(t, 2), make([][]byte, 11)), im.StatusConstraintError},                                  // :431
		{"oversized intermediate", req(testCertificate(t, 2), [][]byte{make([]byte, 3001)}), im.StatusConstraintError},                      // :437
		{"unknown CCDID", tcmdef.ProvisionClientCertificateRequest{Ccdid: 50, ClientCertificate: testCertificate(t, 3)}, im.StatusNotFound}, // :451
	}
	for _, tc := range cases {
		_, err := c.MatterInvoke(ctx, tcmdef.CmdProvisionClientCertificate, tc.req)
		wantStatus(t, tc.name, err, tc.want)
	}
	bad := testCertificate(t, 4)
	keys.invalid[string(bad)] = true
	_, err := c.MatterInvoke(ctx, tcmdef.CmdProvisionClientCertificate, req(bad, nil)) // :425
	wantStatus(t, "invalid", err, im.StatusDynamicConstraintError)
	_, err = c.MatterInvoke(ctx, tcmdef.CmdProvisionClientCertificate, req(testCertificate(t, 5), [][]byte{bad})) // :438
	wantStatus(t, "invalid intermediate", err, im.StatusDynamicConstraintError)
	cert := testCertificate(t, 6)
	_, err = c.MatterInvoke(ctx, tcmdef.CmdProvisionClientCertificate, req(cert, [][]byte{testCertificate(t, 7)}))
	wantStatus(t, "provision", err, im.StatusSuccess)
	_, err = c.MatterInvoke(ctx, tcmdef.CmdProvisionClientCertificate, req(cert, nil)) // :450
	wantStatus(t, "duplicate", err, im.StatusAlreadyExists)
	sum := sha256.Sum256(cert)
	resp, err := invoke[tcmdef.LookupClientCertificateResponse](t, c, ctx, tcmdef.CmdLookupClientCertificate, tcmdef.LookupClientCertificateRequest{Fingerprint: sum[:]})
	wantStatus(t, "lookup", err, im.StatusSuccess)
	if resp.Ccdid != ccdid {
		t.Fatalf("lookup CCDID %d", resp.Ccdid)
	}
	m, err := tls.NewCertificateManagement(context.Background(), tls.CertificateManagementConfig{MaxRootCertificates: 5, MaxClientCertificates: 5, Port: mismatchedKeys{keys}})
	if err != nil {
		t.Fatal(err)
	}
	id := clientCSR(t, m).Ccdid
	_, err = m.MatterInvoke(ctx, tcmdef.CmdProvisionClientCertificate, tcmdef.ProvisionClientCertificateRequest{Ccdid: id, ClientCertificate: testCertificate(t, 8)}) // CertificateTableImpl.cpp:526-529
	wantStatus(t, "key mismatch", err, im.StatusDynamicConstraintError)
}

func TestDependencyAndFabricRemoval(t *testing.T) {
	m, certs, roots := newClient(t)
	ctx := peer(testFabric)
	ccdid := clientCSR(t, certs).Ccdid
	req := provisionRequest(roots[0])
	req.Ccdid = spec.ValueOf(ccdid)
	provisionEndpoint(t, m, req)
	_, err := certs.MatterInvoke(ctx, tcmdef.CmdRemoveRootCertificate, tcmdef.RemoveRootCertificateRequest{Caid: roots[0]}) // :366
	wantStatus(t, "root in use", err, im.StatusInvalidInState)
	_, err = certs.MatterInvoke(ctx, tcmdef.CmdRemoveClientCertificate, tcmdef.RemoveClientCertificateRequest{Ccdid: ccdid}) // :543
	wantStatus(t, "client in use", err, im.StatusInvalidInState)
	_, err = certs.MatterInvoke(ctx, tcmdef.CmdRemoveRootCertificate, tcmdef.RemoveRootCertificateRequest{Caid: roots[1]})
	wantStatus(t, "root unused", err, im.StatusSuccess)
	// Fabric isolation of the reads.
	if v, _ := m.MatterReadFiltered(peer(otherFabric), tcldef.AttrProvisionedEndpoints); len(v.(spec.List[tcldef.TLSEndpointStruct])) != 0 {
		t.Fatal("another fabric reads the endpoint")
	}
	if v, _ := certs.MatterReadFiltered(peer(testFabric), tcmdef.AttrProvisionedRootCertificates); len(v.(spec.List[tcmdef.TLSCertStruct])) != 1 {
		t.Fatal("the fabric's own read")
	}
	m.MatterFabricRemoved(testFabric)     // TLSClientManagementCluster.cpp:272-275
	certs.MatterFabricRemoved(testFabric) // TLSCertificateManagementCluster.cpp:571-575
	if len(readAs[[]tcldef.TLSEndpointStruct](t, m.MatterRead, tcldef.AttrProvisionedEndpoints)) != 0 ||
		len(readAs[[]tcmdef.TLSCertStruct](t, certs.MatterRead, tcmdef.AttrProvisionedRootCertificates)) != 0 ||
		len(readAs[[]tcmdef.TLSClientCertificateDetailStruct](t, certs.MatterRead, tcmdef.AttrProvisionedClientCertificates)) != 0 {
		t.Fatal("the fabric's entries survived its removal")
	}
}

func TestProvisionEndpointRules(t *testing.T) {
	m, _, roots := newClient(t)
	ctx := peer(testFabric)
	id := provisionEndpoint(t, m, provisionRequest(roots[0]))
	_, err := m.MatterInvoke(ctx, tcldef.CmdProvisionEndpoint, provisionRequest(roots[1])) // instance :280-288
	if code, ok := clusterCode(err); !ok || code != uint8(tcldef.StatusCodeEndpointAlreadyInstalled) {
		t.Fatalf("same host and port: %v", err)
	}
	long := provisionRequest(roots[0])
	long.Hostname = make([]byte, 254)
	_, err = m.MatterInvoke(ctx, tcldef.CmdProvisionEndpoint, long) // :182-185
	wantStatus(t, "long hostname", err, im.StatusConstraintError)
	big := provisionRequest(65535)
	_, err = m.MatterInvoke(ctx, tcldef.CmdProvisionEndpoint, big) // :187-190
	wantStatus(t, "CAID 65535", err, im.StatusConstraintError)
	upd := provisionRequest(roots[1])
	upd.Port = 8443
	upd.EndpointId = spec.ValueOf[uint16](42)
	_, err = m.MatterInvoke(ctx, tcldef.CmdProvisionEndpoint, upd) // instance :309-311
	wantStatus(t, "unknown endpoint", err, im.StatusNotFound)
	if err := m.MutateEndpointReferenceCount(testFabric, id, 2); err != nil {
		t.Fatal(err)
	}
	_, err = m.MatterInvoke(ctx, tcldef.CmdRemoveEndpoint, tcldef.RemoveEndpointRequest{EndpointId: id}) // instance :352
	wantStatus(t, "in use", err, im.StatusInvalidInState)
	// An update keeps the id and resets the count (instance :315-321).
	upd.EndpointId = spec.ValueOf(id)
	resp, err := invoke[tcldef.ProvisionEndpointResponse](t, m, ctx, tcldef.CmdProvisionEndpoint, upd)
	wantStatus(t, "update", err, im.StatusSuccess)
	if e, _ := m.ProvisionedEndpoint(testFabric, id); resp.EndpointId != id || e.ReferenceCount != 0 || e.Port != 8443 {
		t.Fatalf("update %+v", e)
	}
	if err := m.MutateEndpointReferenceCount(testFabric, id, -5); err != nil { // instance :426
		t.Fatal(err)
	}
	if err := m.MutateEndpointReferenceCount(testFabric, id, 127); err != nil {
		t.Fatal(err)
	}
	if err := m.MutateEndpointReferenceCount(testFabric, id, 127); err != nil {
		t.Fatal(err)
	}
	if err := m.MutateEndpointReferenceCount(testFabric, id, 2); !errors.Is(err, tls.ErrReferenceCount) { // instance :420-421
		t.Fatal(err)
	}
	if err := m.MutateEndpointReferenceCount(testFabric, 99, 1); !errors.Is(err, tls.ErrNoEndpoint) {
		t.Fatal(err)
	}
	for i := range 9 {
		r := provisionRequest(roots[0])
		r.Port = uint16(1000 + i)
		provisionEndpoint(t, m, r)
	}
	r := provisionRequest(roots[0])
	r.Port = 2000
	_, err = m.MatterInvoke(ctx, tcldef.CmdProvisionEndpoint, r) // instance :296-297
	wantStatus(t, "capacity", err, im.StatusResourceExhausted)
}

func TestNoCertificatesLookup(t *testing.T) {
	m, err := tls.NewClientManagement(context.Background(), tls.ClientManagementConfig{MaxProvisioned: 5})
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.MatterInvoke(peer(testFabric), tcldef.CmdProvisionEndpoint, provisionRequest(0))
	if code, ok := clusterCode(err); !ok || code != uint8(tcldef.StatusCodeRootCertificateNotFound) {
		t.Fatalf("without certificates: %v", err)
	}
}

func TestPersistence(t *testing.T) {
	st := &mapStore{}
	keys := &mockKeys{invalid: map[string]bool{}}
	ccfg := tls.CertificateManagementConfig{MaxRootCertificates: 5, MaxClientCertificates: 5, Port: keys, Store: st, Key: "root"}
	c, err := tls.NewCertificateManagement(context.Background(), ccfg)
	if err != nil {
		t.Fatal(err)
	}
	caid := provisionRoot(t, c, testCertificate(t, 1))
	m, err := tls.NewClientManagement(context.Background(), tls.ClientManagementConfig{MaxProvisioned: 5, Certificates: c, Store: st, Key: "root"})
	if err != nil {
		t.Fatal(err)
	}
	id := provisionEndpoint(t, m, provisionRequest(caid))
	c2, err := tls.NewCertificateManagement(context.Background(), ccfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := c2.RootCertificate(testFabric, caid); !ok {
		t.Fatal("the root certificate was not restored")
	}
	if next := provisionRoot(t, c2, testCertificate(t, 2)); next != caid+1 {
		t.Fatalf("the restored allocator gave %d", next)
	}
	m2, err := tls.NewClientManagement(context.Background(), tls.ClientManagementConfig{MaxProvisioned: 5, Certificates: c2, Store: st, Key: "root"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m2.ProvisionedEndpoint(testFabric, id); !ok {
		t.Fatal("the endpoint was not restored")
	}
	// A failed save keeps the table as it was and answers FAILURE.
	st.failSet = true
	_, err = c2.MatterInvoke(peer(testFabric), tcmdef.CmdProvisionRootCertificate,
		tcmdef.ProvisionRootCertificateRequest{Certificate: testCertificate(t, 3), Caid: spec.NullOf[uint16]()})
	wantStatus(t, "failed save", err, im.StatusFailure)
	if n := len(readAs[[]tcmdef.TLSCertStruct](t, c2.MatterRead, tcmdef.AttrProvisionedRootCertificates)); n != 2 {
		t.Fatalf("%d root certificates after a failed save", n)
	}
	st.m["tls.certificates:broken"] = "{"
	if _, err := tls.NewCertificateManagement(context.Background(), tls.CertificateManagementConfig{MaxRootCertificates: 5, MaxClientCertificates: 5, Port: keys, Store: st, Key: "broken"}); !errors.Is(err, tls.ErrStore) {
		t.Fatal(err)
	}
}

func TestHostInterfaces(t *testing.T) {
	m, certs, _ := newClient(t)
	var _ tcmdef.Host = certs
	var _ tcldef.Host = m
	certs.SetDependencyChecker(nil)
	if _, ok := certs.ClientCertificate(testFabric, 3); ok {
		t.Fatal("unknown client certificate found")
	}
}
