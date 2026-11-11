// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

// The proof of brief BA item 2, after commissioner_control_live_test.go:
// the module's commissioner finds the module's node by its BLE
// advertisement, dials it over the in-memory BLE model, runs PASE and the
// commissioning flow over that channel without MRP, and reaches the node
// over IP (CASE) afterwards. The node's side is its own BLE radio
// (Network.BLE, an in-memory bletest radio): the node advertises, serves
// BTP through its bridge and leaves the radio once commissioned (brief
// 28a).

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/miekg/dns"

	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/cluster/spec"
	basicinfo "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/controller/ble"
	"github.com/SukramJ/go-fabric/controller/ble/bletest"
	"github.com/SukramJ/go-fabric/controller/commissioner"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/node"
)

const (
	bleFabricID uint64 = 0x0000_0000_0000_0B1E
	bleAdmin    uint64 = 0xB0
	bleNodeID   uint64 = 0xB1
)

// announceOperational puts the node's operational record into the
// fabric's DNS-SD cache, as ccFabric.commission does.
func (f *ccFabric) announceOperational(t *testing.T, addr *net.UDPAddr, nodeID uint64, host string) {
	t.Helper()
	op := discovery.OperationalQName(f.fab.Identity.CompressedFabricID, nodeID)
	hdr := func(name string, typ uint16) dns.RR_Header {
		return dns.RR_Header{Name: dns.Fqdn(name), Rrtype: typ, Class: dns.ClassINET | 0x8000, Ttl: 120}
	}
	msg := new(dns.Msg)
	msg.Response = true
	msg.Answer = []dns.RR{
		&dns.SRV{Hdr: hdr(op, dns.TypeSRV), Target: host + ".local.", Port: uint16(addr.Port)}, //nolint:gosec // a test port
		&dns.TXT{Hdr: hdr(op, dns.TypeTXT), Txt: []string{"SII=500", "SAI=300"}},
		&dns.A{Hdr: hdr(host+".local", dns.TypeA), A: net.ParseIP("127.0.0.1").To4()},
	}
	b, err := msg.Pack()
	if err != nil {
		t.Fatal(err)
	}
	f.dns.out <- mdns.QueryResponse{Msg: b}
}

// nodePeripheral is a node's BLE radio as the central reaches it; the
// node is built with its Radio (Network.BLE).
func nodePeripheral() *bletest.Peripheral {
	return &bletest.Peripheral{Address: "C0:FF:EE:00:0B:1E", Discriminator: 3840, VendorID: 0xFFF1, ProductID: 0x8001}
}

// offTheRadio waits until the node no longer serves or advertises on r.
func offTheRadio(t *testing.T, r *bletest.Radio) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		serving, _ := r.Serving()
		_, advertising := r.Advertising()
		if !serving && !advertising {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the commissioned node is still on the radio: serving %v, advertising %v", serving, advertising)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// dialBLE finds the node by its long discriminator and dials it.
func dialBLE(ctx context.Context, t *testing.T, central *bletest.Central) *ble.Channel {
	t.Helper()
	sc := ble.NewScanner(central, ble.ScannerOptions{})
	defer func() { _ = sc.Close(context.Background()) }()
	devs, err := sc.Find(ctx, discovery.ByLongDiscriminator(3840), 5*time.Second, false)
	if err != nil || len(devs) != 1 {
		t.Fatalf("Find: %v, %v", devs, err)
	}
	if devs[0].VendorID != 0xFFF1 || devs[0].ProductID != 0x8001 || devs[0].ShortDiscriminator() != 15 {
		t.Fatalf("device %+v", devs[0])
	}
	ch, err := ble.Dial(ctx, central, devs[0].Peripheral, ble.DialOptions{})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	if !ch.Exchanges().Reliable() || !ch.Conn().Reliable() {
		t.Fatal("the BLE channel runs MRP")
	}
	return ch
}

func TestLiveCommissioningOverBLE(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	f := newCCFabric(t, bleFabricID, bleAdmin, "ble")
	p := nodePeripheral()
	n := liveNode(t, "node-ble", func(c *node.Config) { c.Network.BLE = p.Radio() })
	central := bletest.NewCentral(p)
	ch := dialBLE(ctx, t, central)

	f.announceOperational(t, udpAddrOf(t, n), bleNodeID, "node-ble")
	res, err := f.com.Commission(ctx, commissioner.Options{
		Fabric: f.fab, Passcode: otaLivePasscode, NodeID: bleNodeID, Channel: ch,
		OnAttestationFailure: attestation.Accept(), Timeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("Commission over BLE: %v", err)
	}
	select {
	case <-ch.Closed():
	case <-time.After(5 * time.Second):
		t.Fatal("the BLE channel outlived the commissioning")
	}
	offTheRadio(t, p.Radio())

	// Over IP now: CASE to the node, a read.
	ia := f.com.Interaction(f.set.Get(res.Address))
	report, err := ia.Read(ctx, im.ConcreteAttributePath{Endpoint: 0, Cluster: basicinfo.ClusterID, Attribute: basicinfo.AttrNodeLabel, HasEndpoint: true, HasCluster: true, HasAttribute: true})
	if err != nil {
		t.Fatalf("Read over CASE: %v", err)
	}
	v, ok := report.Value(0, basicinfo.ClusterID, basicinfo.AttrNodeLabel)
	if !ok {
		t.Fatal("no NodeLabel")
	}
	if label, err := spec.DecodeString(v); err != nil || label != "node-ble" {
		t.Fatalf("NodeLabel %q, %v", label, err)
	}
}
