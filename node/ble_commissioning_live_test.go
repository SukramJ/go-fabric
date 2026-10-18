// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

// The proof of brief BA item 2, after commissioner_control_live_test.go:
// the module's commissioner finds the module's node by its BLE
// advertisement, dials it over an in-memory BTP pair, runs PASE and the
// commissioning flow over that channel without MRP, and reaches the node
// over IP (CASE) afterwards. The node's side of the BTP pair is served
// through bridgetest.ServeConn — a node never advertises over BLE (ADR
// 0024, decision 1).

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/miekg/dns"

	"github.com/SukramJ/go-fabric/bridge/bridgetest"
	"github.com/SukramJ/go-fabric/cluster/spec"
	basicinfo "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/controller/ble"
	"github.com/SukramJ/go-fabric/controller/ble/bletest"
	"github.com/SukramJ/go-fabric/controller/commissioner"
	"github.com/SukramJ/go-fabric/controller/discovery"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/transport/btp"
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

// blePeripheralOf is the node's BLE side: its advertisement, and every
// BTP session served by the node's bridge.
func blePeripheralOf(t *testing.T, n *node.Node) *bletest.Peripheral {
	t.Helper()
	return &bletest.Peripheral{
		Address: "C0:FF:EE:00:0B:1E", Discriminator: 3840, VendorID: 0xFFF1, ProductID: 0x8001,
		Serve: func(conn *btp.Conn) (func([]byte), func()) {
			srv, err := bridgetest.ServeConn(n.Bridge(), conn)
			if err != nil {
				t.Error(err)
				return nil, nil
			}
			return srv.Deliver, srv.Close
		},
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
	n := liveNode(t, "node-ble", nil)
	central := bletest.NewCentral(blePeripheralOf(t, n))
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
