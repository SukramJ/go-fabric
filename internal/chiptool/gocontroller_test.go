// SPDX-License-Identifier: Apache-2.0
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
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/tlv"
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

// TestGoControllerSubscribesReferenceBridge: the module's own controller
// (ADR 0020, stage 3) commissions the reference daemon, then — over the
// peer's CASE sessions — subscribes to OnOff on a bridged light, toggles
// it with an Invoke and receives the pushed report, writes NodeLabel and
// reads it back; the daemon restarts on its database and re-establishes
// the subscription by opening CASE to the controller, which answers as the
// responder, advertised on the host's DNS-SD; the re-established report
// and a Toggle's arrive over that inbound session.
func TestGoControllerSubscribesReferenceBridge(t *testing.T) {
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
		FabricID: goControllerFabricID + 1, RootNodeID: goControllerNodeID, VendorID: 0xFFF1, Label: "go-fabric subscriber",
	})
	if err != nil {
		t.Fatal(err)
	}
	resumption := &peer.MemoryResumptionStore{}
	set, err := peer.NewSet(peer.Context{Exchanges: mgr, Names: names, Fabrics: fabric.Lookup, Resumption: resumption, Logger: log})
	if err != nil {
		t.Fatal(err)
	}
	reg := interaction.NewSubscriptions(interaction.SubscriptionsConfig{Logger: log})
	scanner := discovery.NewScanner(names)
	com, err := commissioner.New(commissioner.Context{
		Exchanges: mgr, Peers: set, Attestation: attestation.TestStore(), Logger: log,
		Subscriptions: reg, Fabrics: []*commissioner.Fabric{fabric}, Resumption: resumption,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Our operational record: the restarted daemon resolves us by it.
	adv := mdns.NewZeroconf()
	t.Cleanup(func() {
		_ = reg.Close(context.Background())
		com.Close()
		_ = adv.Close()
		scanner.Close()
		set.Close(context.Background())
		mgr.Close()
		stopServe()
		<-served
		_ = names.Close()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	if err := adv.Publish(ctx, mdns.BuildOperationalService(mdns.OperationalServiceConfig{
		CompressedFabricID: fabric.Identity.CompressedFabricID, NodeID: goControllerNodeID, Port: uint16(l.LocalAddr().Port), //nolint:gosec // a bound port
	})); err != nil {
		t.Skipf("cannot advertise on the host's DNS-SD: %v", err)
	}

	findCtx, cancelFind := context.WithTimeout(ctx, 30*time.Second)
	var found *discovery.Device
	_, err = scanner.FindCommissionable(findCtx, discovery.ByLongDiscriminator(br.info.discriminator), func(d discovery.Device) {
		found = &d
		cancelFind()
	})
	cancelFind()
	if err != nil || found == nil {
		t.Skipf("the daemon's commissionable record was not seen: multicast unavailable here (%v)", err)
	}
	res, err := com.Commission(ctx, commissioner.Options{
		Fabric: fabric, Passcode: br.info.passcode, Addresses: udpAddrsOf(*found), DiscoveryData: found, OnAttestationFailure: attestation.Accept(),
	})
	if err != nil {
		br.dump(t)
		t.Fatalf("Commission: %v", err)
	}
	p := set.Get(res.Address)
	c := com.Interaction(p)

	light := br.endpointFor(t, 0x0100) // OnOffLight
	if light == 0 {
		t.Fatal("the daemon has no OnOffLight")
	}
	onOff := im.ConcreteAttributePath{Endpoint: light, Cluster: 0x0006, Attribute: 0x0000, HasEndpoint: true, HasCluster: true, HasAttribute: true}
	reports := make(chan bool, 32)
	start := time.Now()
	sub, err := c.Subscribe(ctx, interaction.SubscribeRequest{
		Attributes: []im.ConcreteAttributePath{onOff}, Sustain: true,
		MinIntervalFloor: interaction.Interval(0), MaxIntervalCeiling: interaction.Interval(time.Minute),
		Updated: func(r *interaction.Report) {
			if v, ok := r.Value(light, 0x0006, 0x0000); ok {
				reports <- v.El.Bool
			}
		},
	})
	if err != nil {
		br.dump(t)
		t.Fatalf("Subscribe: %v", err)
	}
	expect := func(what string, want bool) {
		t.Helper()
		deadline := time.After(60 * time.Second)
		for {
			select {
			case v := <-reports:
				if v == want {
					t.Logf("%s: OnOff = %v after %s", what, v, time.Since(start).Round(time.Millisecond))
					return
				}
			case <-deadline:
				br.dump(t)
				t.Fatalf("%s: no report with OnOff = %v", what, want)
			}
		}
	}
	first := <-reports
	t.Logf("priming report: OnOff = %v", first)
	toggle := func() {
		t.Helper()
		if _, err := c.Invoke(ctx, im.ConcreteCommandPath{Endpoint: light, Cluster: 0x0006, Command: 0x02}, nil, interaction.InvokeOptions{}); err != nil {
			br.dump(t)
			t.Fatalf("Toggle: %v", err)
		}
	}
	toggle()
	expect("pushed report after Toggle", !first)

	label := im.ConcreteAttributePath{Endpoint: 0, Cluster: basicinfo.ClusterID, Attribute: basicinfo.AttrNodeLabel, HasEndpoint: true, HasCluster: true, HasAttribute: true}
	written, err := c.Write(ctx, []interaction.WriteRequest{{Path: label, Value: interaction.Raw(spec.Node{El: tlv.Element{Type: tlv.TypeUTF8Str1, String: "go controller"}})}}, interaction.WriteOptions{})
	if err != nil || written.Err() != nil {
		br.dump(t)
		t.Fatalf("Write NodeLabel: %+v, %v", written, err)
	}
	r, err := c.Read(ctx, label)
	if err != nil {
		t.Fatalf("Read NodeLabel: %v", err)
	}
	if v, ok := r.Value(0, basicinfo.ClusterID, basicinfo.AttrNodeLabel); !ok || v.El.String != "go controller" {
		t.Fatalf("NodeLabel %+v after the write", v)
	}

	id := sub.ID()
	start = time.Now()
	br = br.restart(t)
	// The daemon does not keep the light's state across the restart: the
	// re-established priming report carries whatever it starts with.
	var after bool
	select {
	case after = <-reports:
		t.Logf("re-established subscription's report: OnOff = %v after %s", after, time.Since(start).Round(time.Millisecond))
	case <-time.After(60 * time.Second):
		br.dump(t)
		t.Fatal("no report of the re-established subscription")
	}
	if s := p.NewestSession(); s == nil || !sub.Active() || sub.ID() != id {
		t.Fatalf("after the restart: session %v, active %v, id %d (was %d)", s, sub.Active(), sub.ID(), id)
	}
	toggle()
	expect("pushed report after the restart", !after)
}
