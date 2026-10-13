// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

//go:build chiptool

package chiptool

import (
	"context"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/spec"
	basicinfo "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/controller/ca"
	"github.com/SukramJ/go-fabric/controller/commissioner"
	"github.com/SukramJ/go-fabric/controller/discovery"
	"github.com/SukramJ/go-fabric/controller/exchange"
	"github.com/SukramJ/go-fabric/controller/interaction"
	"github.com/SukramJ/go-fabric/controller/peer"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/transport/udp"
)

// goControllerFabricID and goControllerNodeID are the fabric and node id
// of the module's own commissioner here, apart from chip-tool's.
const (
	goControllerFabricID uint64 = 0x0000_0000_0000_0A11
	goControllerNodeID   uint64 = 0x0000_0000_0001_B669
)

// TestGoControllerCommissionsReferenceBridge: the module's own
// commissioner (controller/commissioner, ADR 0020 stage 2) finds the
// reference daemon over the host's multicast DNS-SD, commissions it —
// PASE, attestation against the CSA test trust store, the CSR, the
// certificates, the operational CASE session, CommissioningComplete — and
// reads BasicInformation.VendorID back over CASE. It needs the daemon
// binary and the host's multicast, nothing of the CHIP harness, and skips
// where multicast is unavailable, as TestMatterJSController skips without
// its prerequisites.
func TestGoControllerCommissionsReferenceBridge(t *testing.T) {
	bin := requireBridgeBinary(t)
	flags := requireBridgeFlags(t, bin, "db", "listen")
	br := startBridge(t, bin, flags)

	names, err := discovery.Open(nil)
	if err != nil {
		t.Skipf("no multicast here: %v", err)
	}
	l, err := udp.New(udp.Config{LocalAddr: "[::]:0"})
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.DiscardHandler)
	mgr, err := exchange.NewManager(l, exchange.Config{Logger: log})
	if err != nil {
		t.Fatal(err)
	}
	serveCtx, stopServe := context.WithCancel(context.Background())
	served := make(chan struct{})
	go func() {
		defer close(served)
		_ = l.Serve(serveCtx, mgr.HandleDatagram)
	}()
	authority, err := ca.New(ca.Config{Store: &ca.MemoryStore{}})
	if err != nil {
		t.Fatal(err)
	}
	fabric, err := commissioner.NewFabric(authority, commissioner.FabricConfig{
		FabricID: goControllerFabricID, RootNodeID: goControllerNodeID, VendorID: 0xFFF1, Label: "go-fabric controller",
	})
	if err != nil {
		t.Fatal(err)
	}
	set, err := peer.NewSet(peer.Context{Exchanges: mgr, Names: names, Fabrics: fabric.Lookup, Resumption: &peer.MemoryResumptionStore{}, Logger: log})
	if err != nil {
		t.Fatal(err)
	}
	scanner := discovery.NewScanner(names)
	t.Cleanup(func() {
		scanner.Close()
		set.Close(context.Background())
		mgr.Close()
		stopServe()
		<-served
		_ = names.Close()
	})
	com, err := commissioner.New(commissioner.Context{Exchanges: mgr, Peers: set, Attestation: attestation.TestStore(), Logger: log})
	if err != nil {
		t.Fatal(err)
	}

	findCtx, cancelFind := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelFind()
	var found *discovery.Device
	_, err = scanner.FindCommissionable(findCtx, discovery.ByLongDiscriminator(br.info.discriminator), func(d discovery.Device) {
		found = &d
		cancelFind()
	})
	if err != nil || found == nil {
		t.Skipf("the daemon's commissionable record was not seen: multicast unavailable here (%v)", err)
	}
	addrs := make([]string, 0, len(found.Addresses))
	for _, a := range found.Addresses {
		addrs = append(addrs, a.String())
	}
	t.Logf("found the daemon: instance %s, addresses %v", found.InstanceID, addrs)

	// The reference daemon presents the CSA test chain and a test CD: the
	// one finding is CertificationTypeTest (info). Anything else fails.
	var findings []attestation.Finding
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	res, err := com.Commission(ctx, commissioner.Options{
		Fabric: fabric, Passcode: br.info.passcode, Addresses: udpAddrsOf(*found), DiscoveryData: found,
		OnAttestationFailure: attestation.Policy{Decide: func(f []attestation.Finding) (bool, string, error) {
			findings = f
			for _, x := range f {
				if x.Level != attestation.LevelInfo {
					return false, "unexpected attestation finding " + string(x.Check) + ": " + x.Message, nil
				}
			}
			return true, "", nil
		}},
	})
	if err != nil {
		br.dump(t)
		t.Fatalf("Commission: %v", err)
	}
	if len(findings) != 1 || findings[0].Check != attestation.CertificationTypeTest {
		t.Errorf("attestation findings %+v, want one CertificationTypeTest", findings)
	}
	t.Logf("commissioned as node 0x%016X, fabric index on the daemon %d", res.Address.NodeID, res.FabricIndexOnPeer)

	p := set.Get(res.Address)
	if p == nil || p.NewestSession() == nil {
		t.Fatal("no CASE session with the commissioned daemon")
	}
	report, err := interaction.New(mgr, p.NewestSession(), log).Read(ctx, im.ConcreteAttributePath{
		Endpoint: 0, Cluster: basicinfo.ClusterID, Attribute: basicinfo.AttrVendorId, HasEndpoint: true, HasCluster: true, HasAttribute: true,
	})
	if err != nil {
		br.dump(t)
		t.Fatalf("Read over CASE: %v", err)
	}
	v, ok := report.Value(0, basicinfo.ClusterID, basicinfo.AttrVendorId)
	if !ok {
		t.Fatal("no VendorID in the report")
	}
	if vid, err := spec.DecodeUint[uint16](16)(v); err != nil || int64(vid) != testVendorID {
		t.Fatalf("VendorID = 0x%X (%v), want 0x%X", vid, err, testVendorID)
	}
}

// udpAddrsOf is the device's addresses as UDP addresses.
func udpAddrsOf(d discovery.Device) []*net.UDPAddr {
	out := make([]*net.UDPAddr, 0, len(d.Addresses))
	for _, a := range d.Addresses {
		out = append(out, a.UDPAddr())
	}
	return out
}
