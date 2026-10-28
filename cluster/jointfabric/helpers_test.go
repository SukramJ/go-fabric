// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package jointfabric

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"errors"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/client/jcm"
	opcreds "github.com/SukramJ/go-fabric/cluster/spec/operationalcredentials"
	"github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/controller/ca"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/secure/mattercert"
	"github.com/SukramJ/go-fabric/store"
)

// adminCAT and anchorCAT are version 1 of the Joint Fabric CATs.
const (
	adminCAT  uint32 = 0xFFFF_0001
	anchorCAT uint32 = 0xFFFE_0001
)

// jfFabric is a 3-tier fabric (RCAC, ICAC, NOC) whose NOC holder is an
// administrator; icacKey is the ICAC's key, for an AddICAC.
type jfFabric struct {
	index    uint8
	fabricID uint64
	nodeID   uint64
	vendorID uint16
	rcac     []byte
	icac     []byte
	noc      []byte
	key      *ecdsa.PrivateKey
	icacKey  *ecdsa.PrivateKey
	rootPub  []byte
}

func newJFFabric(t *testing.T, index uint8, cats ...uint32) *jfFabric {
	t.Helper()
	icacKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	c, err := ca.New(ca.Config{IntermediateCA: true, ICACSigner: icacKey})
	if err != nil {
		t.Fatal(err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pub, err := key.PublicKey.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	f := &jfFabric{index: index, fabricID: 0x0B0B, nodeID: 0x1234, vendorID: 0xFFF1, rcac: c.RootCert(), icac: c.ICACCert(), key: key, icacKey: icacKey, rootPub: c.RootPublicKey()}
	if f.noc, err = c.GenerateNOC(pub, f.fabricID, f.nodeID, cats); err != nil {
		t.Fatal(err)
	}
	return f
}

// record is the fabric as a node's store holds it.
func (f *jfFabric) record() store.FabricRecord {
	return store.FabricRecord{FabricIndex: f.index, FabricID: f.fabricID, NodeID: f.nodeID, RootPublicKey: f.rootPub, RootCert: f.rcac, VendorID: f.vendorID}
}

// csr is a PKCS#10 request of the ICAC's key.
func (f *jfFabric) csr(t *testing.T) []byte {
	t.Helper()
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{}, f.icacKey)
	if err != nil {
		t.Fatal(err)
	}
	return der
}

// fakeFabrics is a node's fabric table.
type fakeFabrics struct {
	fabrics []store.FabricRecord
	ids     map[uint8]store.IdentityRecord
}

func (f *fakeFabrics) GetFabric(_ context.Context, idx uint8) (store.FabricRecord, error) {
	for _, r := range f.fabrics {
		if r.FabricIndex == idx {
			return r, nil
		}
	}
	return store.FabricRecord{}, store.ErrFabricNotFound
}

func (f *fakeFabrics) ListFabrics(context.Context) ([]store.FabricRecord, error) {
	return f.fabrics, nil
}

func (f *fakeFabrics) GetIdentity(_ context.Context, idx uint8) (store.IdentityRecord, error) {
	if id, ok := f.ids[idx]; ok {
		return id, nil
	}
	return store.IdentityRecord{}, store.ErrIdentityNotFound
}

// fakeWindow is the node's commissioning window.
type fakeWindow struct {
	mu     sync.Mutex
	open   bool
	opened []wire.OpenWindowParams
	err    error
}

func (w *fakeWindow) CurrentWindow() wire.WindowStatusSnapshot {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.open {
		return wire.WindowStatusSnapshot{Status: wire.WindowStatusEnhanced}
	}
	return wire.WindowStatusSnapshot{Status: wire.WindowStatusClosed, AdminFabricIsNull: true, AdminVendorIsNull: true}
}

func (w *fakeWindow) OpenWindow(_ context.Context, p wire.OpenWindowParams) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.err != nil {
		return w.err
	}
	w.open = true
	w.opened = append(w.opened, p)
	return nil
}

func (w *fakeWindow) RevokeWindow(context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.open = false
	return nil
}

// fakeReach is the announcing commissioner, administrator of f on its own
// node at index adminIndex.
type fakeReach struct {
	f          *jfFabric
	adminIndex uint8
	null       bool
	readErr    error
	fabricsErr error
	// block makes ReadAdministratorFabricIndex wait for ctx to end.
	block   bool
	waiting chan struct{}
	signs   int
	mu      sync.Mutex
}

func (r *fakeReach) ReadAdministratorFabricIndex(ctx context.Context, _ CommissionerPeer, _ uint16) (fabricIndex uint8, null bool, err error) {
	if r.block {
		close(r.waiting)
		<-ctx.Done()
		return 0, false, ctx.Err()
	}
	return r.adminIndex, r.null, r.readErr
}

func (r *fakeReach) ReadFabrics(context.Context, CommissionerPeer) ([]opcreds.FabricDescriptorStruct, error) {
	if r.fabricsErr != nil {
		return nil, r.fabricsErr
	}
	return []opcreds.FabricDescriptorStruct{{RootPublicKey: r.f.rootPub, VendorId: r.f.vendorID, FabricId: r.f.fabricID, NodeId: r.f.nodeID, FabricIndex: r.adminIndex}}, nil
}

func (r *fakeReach) ReadTrustedRootCertificates(context.Context, CommissionerPeer) ([][]byte, error) {
	return [][]byte{r.f.rcac}, nil
}

func (r *fakeReach) ReadNOCs(context.Context, CommissionerPeer) ([]opcreds.NOCStruct, error) {
	n := opcreds.NOCStruct{Noc: r.f.noc, FabricIndex: r.adminIndex}
	n.Icac.Value = r.f.icac
	return []opcreds.NOCStruct{n}, nil
}

func (r *fakeReach) SignVIDVerification(_ context.Context, _ CommissionerPeer, fabricIndex uint8, challenge []byte) (opcreds.SignVidVerificationResponse, []byte, error) {
	r.mu.Lock()
	r.signs++
	r.mu.Unlock()
	if fabricIndex != r.adminIndex {
		return opcreds.SignVidVerificationResponse{}, nil, errors.New("reach: no fabric for the index")
	}
	att := make([]byte, 16)
	_, _ = rand.Read(att)
	rcac, err := mattercert.Decode(r.f.rcac)
	if err != nil {
		return opcreds.SignVidVerificationResponse{}, nil, err
	}
	tbs, err := jcm.ToBeSigned(fabricIndex, challenge, att, rcac.PublicKey, r.f.fabricID, r.f.vendorID)
	if err != nil {
		return opcreds.SignVidVerificationResponse{}, nil, err
	}
	digest := sha256.Sum256(tbs)
	rr, ss, err := ecdsa.Sign(rand.Reader, r.f.key, digest[:])
	if err != nil {
		return opcreds.SignVidVerificationResponse{}, nil, err
	}
	sig := make([]byte, 64)
	rr.FillBytes(sig[:32])
	ss.FillBytes(sig[32:])
	return opcreds.SignVidVerificationResponse{FabricIndex: fabricIndex, FabricBindingVersion: 1, Signature: sig}, att, nil
}

// fakePort is the administrator key.
type fakePort struct {
	csr []byte
	err error
}

func (p *fakePort) IcacCsr(context.Context) ([]byte, error) { return p.csr, p.err }

// caseCtx is an invoke over CASE on the fabric.
func caseCtx(ctx context.Context, fabricIndex uint8) context.Context {
	return im.WithFabricFilter(ctx, true, fabricIndex)
}

// adminFixture is an Administrator on a node with fabric f, its window
// and its fail-safe.
type adminFixture struct {
	a      *Administrator
	f      *jfFabric
	window *fakeWindow
	armed  bool
	mu     sync.Mutex
}

func newAdminFixture(t *testing.T, cfg AdministratorConfig) *adminFixture {
	t.Helper()
	fx := &adminFixture{f: newJFFabric(t, 3, adminCAT), window: &fakeWindow{}}
	if cfg.Port == nil {
		cfg.Port = &fakePort{csr: fx.f.csr(t)}
	}
	if cfg.Reach == nil {
		cfg.Reach = &fakeReach{f: fx.f, adminIndex: 3}
	}
	a, err := NewAdministrator(cfg)
	if err != nil {
		t.Fatal(err)
	}
	fx.a = a
	a.AttachNode(&fakeFabrics{fabrics: []store.FabricRecord{fx.f.record()}, ids: map[uint8]store.IdentityRecord{3: {FabricIndex: 3, NOC: fx.f.noc, ICAC: fx.f.icac}}}, fx.window)
	a.AttachFailSafe(func() bool { fx.mu.Lock(); defer fx.mu.Unlock(); return fx.armed }, func(uint64) {})
	return fx
}

func (fx *adminFixture) arm(on bool) {
	fx.mu.Lock()
	fx.armed = on
	fx.mu.Unlock()
}
