// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package dcl

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net"
	"path/filepath"
	"slices"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/controller/ca"
	"github.com/SukramJ/go-fabric/controller/commissioner"
	"github.com/SukramJ/go-fabric/controller/discovery"
	"github.com/SukramJ/go-fabric/controller/exchange"
	"github.com/SukramJ/go-fabric/controller/handshake"
	"github.com/SukramJ/go-fabric/controller/peer"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/node"
	secattestation "github.com/SukramJ/go-fabric/secure/attestation"
	"github.com/SukramJ/go-fabric/secure/sessionparams"
	"github.com/SukramJ/go-fabric/transport/udp"
)

type silentMDNS struct{ out chan mdns.QueryResponse }

func (s *silentMDNS) Send([]byte) error                    { return nil }
func (s *silentMDNS) Responses() <-chan mdns.QueryResponse { return s.out }
func (s *silentMDNS) Close() error                         { return nil }

// harnessNode starts a node.Node on loopback with chain (nil: the CSA test
// chain), as the controller/commissioner harness does.
func harnessNode(t *testing.T, chain *secattestation.Chain) *net.UDPAddr {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "node.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	n, err := node.New(node.Config{
		Network:            node.Network{Listen: "127.0.0.1:0", PreferIPv4: true},
		Advertiser:         mdns.NewNoop(),
		Commissioning:      node.Commissioning{Passcode: 20202021, Discriminator: 3840, PBKDFIterations: 1000, Salt: []byte("SPAKE2P Key Salt")},
		ProductDescription: node.ProductDescription{VendorID: 0xFFF1, ProductID: 0x8001},
		BasicInformation: node.BasicInformation{
			VendorName: "go-fabric test", ProductName: "dcl test", NodeLabel: "dcl", SerialNumber: "DCL-1",
			HardwareVersion: 1, HardwareVersionString: "1", SoftwareVersion: 1, SoftwareVersionString: "1",
		},
		Attestation: node.Attestation{Chain: chain},
		Store:       node.Store{DB: db},
		Logger:      slog.New(slog.DiscardHandler),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := n.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = n.Stop(context.Background()) })
	addr, err := net.ResolveUDPAddr("udp", n.Bridge().LocalAddr())
	if err != nil {
		t.Fatal(err)
	}
	return addr
}

// harnessCommissioner is the controller side with store as its trust
// store.
func harnessCommissioner(t *testing.T, store attestation.PAAStore) (*commissioner.Commissioner, *commissioner.Fabric) {
	t.Helper()
	log := slog.New(slog.DiscardHandler)
	l, err := udp.New(udp.Config{LocalAddr: "127.0.0.1:0", PreferIPv4: true})
	if err != nil {
		t.Fatal(err)
	}
	mgr, err := exchange.NewManager(l, exchange.Config{Logger: log})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan struct{})
	go func() {
		defer close(served)
		_ = l.Serve(ctx, mgr.HandleDatagram)
	}()
	names := discovery.NewNames(&silentMDNS{out: make(chan mdns.QueryResponse)}, log)
	authority, err := ca.New(ca.Config{Store: &ca.MemoryStore{}})
	if err != nil {
		t.Fatal(err)
	}
	fabric, err := commissioner.NewFabric(authority, commissioner.FabricConfig{FabricID: 0xD1, RootNodeID: 112233, VendorID: 0xFFF1, Label: "dcl"})
	if err != nil {
		t.Fatal(err)
	}
	set, err := peer.NewSet(peer.Context{Exchanges: mgr, Names: names, Fabrics: fabric.Lookup, Resumption: &peer.MemoryResumptionStore{}, Logger: log})
	if err != nil {
		t.Fatal(err)
	}
	com, err := commissioner.New(commissioner.Context{Exchanges: mgr, Peers: set, Attestation: store, Logger: log})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		com.Close()
		set.Close(context.Background())
		mgr.Close()
		cancel()
		<-served
		_ = names.Close()
	})
	return com, fabric
}

// commission runs the flow up to the operational connection, which
// FinalizeCommissioning replaces: attestation is behind it by then.
func commission(t *testing.T, com *commissioner.Commissioner, fabric *commissioner.Fabric, addr *net.UDPAddr, policy attestation.Policy) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	_, err := com.Commission(ctx, commissioner.Options{
		Fabric: fabric, Passcode: 20202021, Addresses: []*net.UDPAddr{addr}, OnAttestationFailure: policy,
		FinalizeCommissioning: func(context.Context, handshake.PeerAddress, *discovery.Device, *sessionparams.Parameters) error {
			return nil
		},
	})
	return err
}

// cdSignerCert is a certificate for the CSA test CD signer key with its
// key identifier (secure/attestation TestCMSSigner*), as the ledger would
// serve it by that identifier.
func cdSignerCert(t *testing.T) testCert {
	t.Helper()
	paa := testCert{key: secattestation.TestPAANoVIDPrivateKey}
	var err error
	if paa.cert, err = x509.ParseCertificate(secattestation.TestPAANoVIDCert); err != nil {
		t.Fatal(err)
	}
	return mint(t, certSpec{cn: "Matter Test CD Signing Authority", usage: x509.KeyUsageDigitalSignature, key: secattestation.TestCMSSignerPrivateKey, skid: secattestation.TestCMSSignerSKID}, &paa)
}

// TestCommissionWithDCLStore: the module's commissioner attests the
// reference node against a CertificateStore fed by a ledger that holds
// the CSA test PAA and serves the test CD signer by key identifier —
// matter.js's DclCertificateService in DeviceAttestationValidator's place
// (ControllerCommissioner.ts:665). The PAA is found, the CD signature is
// verified with the fetched signer; the findings are the test CD's info
// alone. A ledger without the PAA is PaaNotTrusted.
func TestCommissionWithDCLStore(t *testing.T) {
	f := newFakeDCL(t)
	paa := testCert{der: secattestation.TestPAANoVIDCert}
	var err error
	if paa.cert, err = x509.ParseCertificate(paa.der); err != nil {
		t.Fatal(err)
	}
	f.addRoot(paa)
	f.addBySKID(cdSignerCert(t))
	store := openStore(t, CertificateOptions{Client: f.client(), UpdateInterval: -1})
	com, fabric := harnessCommissioner(t, store)

	var findings []attestation.Finding
	err = commission(t, com, fabric, harnessNode(t, nil), attestation.Policy{Decide: func(got []attestation.Finding) (bool, string, error) {
		findings = got
		return true, "", nil
	}})
	if err != nil {
		t.Fatalf("Commission: %v", err)
	}
	for _, f := range findings {
		if f.Check != attestation.CertificationTypeTest && f.Check != attestation.PaaTrustStoreTimeMismatch {
			t.Errorf("finding %s: %s", f.Check, f.Message)
		}
	}
	if !slices.ContainsFunc(store.Certificates(), func(m CertificateMetadata) bool { return m.Kind == KindCDSigner && m.IsProduction }) {
		t.Fatal("the CD signer was not stored")
	}

	// Without the PAA in the ledger.
	empty := newFakeDCL(t)
	empty.json("/dcl/pki/root-certificates", 200, map[string]any{"approvedRootCertificates": map[string]any{"certs": []any{}, "schemaVersion": 0}})
	none := openStore(t, CertificateOptions{Client: empty.client(), UpdateInterval: -1})
	com2, fabric2 := harnessCommissioner(t, none)
	err = commission(t, com2, fabric2, harnessNode(t, nil), attestation.Reject())
	var ae *attestation.Error
	if !errors.As(err, &ae) || ae.Check != attestation.PaaNotTrusted {
		t.Fatalf("no PAA: %v", err)
	}
}

// TestCommissionRevokedDAC: the node's DAC is on its PAI's CRL, which the
// ledger names: the commissioning ends in CertificateRevoked
// (DeviceAttestationValidator.ts:343-355).
func TestCommissionRevokedDAC(t *testing.T) {
	paaCert, err := x509.ParseCertificate(secattestation.TestPAANoVIDCert)
	if err != nil {
		t.Fatal(err)
	}
	paa := testCert{der: secattestation.TestPAANoVIDCert, cert: paaCert, key: secattestation.TestPAANoVIDPrivateKey}
	pai := mint(t, certSpec{cn: "Matter Dev PAI 0xFFF1", vid: u16(0xFFF1), ca: true, serial: 0x3003}, &paa)
	dacKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	dac := mint(t, certSpec{cn: "Matter Dev DAC 0xFFF1/0x8001", vid: u16(0xFFF1), pid: u16(0x8001), usage: x509.KeyUsageDigitalSignature, serial: 0x4004, key: dacKey}, &pai)
	if dac.cert.NotBefore.After(time.Now()) {
		t.Fatal("DAC not yet valid")
	}

	f := newFakeDCL(t)
	f.addRoot(paa)
	f.addBySKID(cdSignerCert(t))
	crl := signCRL(t, pai, crlSpec{revoked: []*big.Int{dac.cert.SerialNumber}})
	f.bytes("/crl/pai", crl)
	f.publishPoints(pai, revocationPoint(pai, pai, nil, false, 0xFFF1, 0, f.url("/crl/pai"), crl))
	store := openStore(t, CertificateOptions{Client: f.client(), UpdateInterval: -1})
	com, fabric := harnessCommissioner(t, store)
	err = commission(t, com, fabric, harnessNode(t, &secattestation.Chain{DACKey: dacKey, DAC: dac.der, PAI: pai.der}), attestation.Reject())
	var ae *attestation.Error
	if !errors.As(err, &ae) || ae.Check != attestation.CertificateRevoked {
		t.Fatalf("revoked DAC: %v", fmt.Sprint(err))
	}
}
