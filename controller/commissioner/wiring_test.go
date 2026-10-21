// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioner

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"net"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/client/handshake"
	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/client/peer"
	"github.com/SukramJ/go-fabric/controller/ca"
	"github.com/SukramJ/go-fabric/transport/udp"
)

// loopbackManager is a manager serving a loopback socket.
func loopbackManager(t *testing.T) (*exchange.Manager, *net.UDPAddr) {
	t.Helper()
	l, err := udp.New(udp.Config{LocalAddr: "127.0.0.1:0", PreferIPv4: true})
	if err != nil {
		t.Fatal(err)
	}
	mgr, err := exchange.NewManager(l, exchange.Config{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan struct{})
	go func() {
		defer close(served)
		_ = l.Serve(ctx, mgr.HandleDatagram)
	}()
	t.Cleanup(func() {
		mgr.Close()
		cancel()
		<-served
	})
	return mgr, l.LocalAddr()
}

// TestContextWiring: with Fabrics the commissioner answers CASE a peer
// opens to it and the session joins the peer set (matter.js's CaseServer
// beside the peer set, SecureChannelProtocol.ts:96-100, PeerSet.ts:113-125);
// Interaction binds the registry; Close removes the responder.
func TestContextWiring(t *testing.T) {
	t.Parallel()
	mgr, addr := loopbackManager(t)
	authority, err := ca.New(ca.Config{Store: &ca.MemoryStore{}})
	if err != nil {
		t.Fatal(err)
	}
	fabric, err := NewFabric(authority, FabricConfig{FabricID: testFabricID, RootNodeID: controllerNodeID, VendorID: 0xFFF1})
	if err != nil {
		t.Fatal(err)
	}
	names := discovery.NewNames(newFakeMDNS(), discard())
	t.Cleanup(func() { _ = names.Close() })
	resumption := &peer.MemoryResumptionStore{}
	set, err := peer.NewSet(peer.Context{Exchanges: mgr, Names: names, Fabrics: fabric.Lookup, Resumption: resumption})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { set.Close(context.Background()) })
	reg := interaction.NewSubscriptions(interaction.SubscriptionsConfig{})
	com, err := New(Context{Exchanges: mgr, Peers: set, Subscriptions: reg, Fabrics: []*Fabric{fabric}, Resumption: resumption})
	if err != nil {
		t.Fatal(err)
	}

	// A node of the fabric, its NOC from the same CA.
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pub, _ := key.PublicKey.Bytes()
	noc, err := authority.GenerateNOC(pub, testFabricID, commissionedNode, nil)
	if err != nil {
		t.Fatal(err)
	}
	id := *fabric.Identity
	id.NOC, id.PrivateKey, id.NodeID, id.FabricIndex = noc, key, commissionedNode, 2
	nodeFabric := handshake.Fabric{Identity: &id, RootPublicKey: fabric.RootPublicKey, Verifier: fabric.Verifier}
	nodeMgr, _ := loopbackManager(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, _, err := handshake.CASE(ctx, nodeMgr, addr, nodeFabric, controllerNodeID, handshake.CASEOptions{}); err != nil {
		t.Fatalf("CASE to the commissioner: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	var p *peer.Peer
	for p == nil || !p.HasSession() {
		if time.Now().After(deadline) {
			t.Fatal("the inbound session did not join the peer set")
		}
		time.Sleep(5 * time.Millisecond)
		p = set.Get(fabric.AddressOf(commissionedNode))
	}
	if addr, ok := com.Interaction(p).Provider().PeerAddress(); !ok || addr != p.Address() {
		t.Fatalf("Interaction peer address %+v", addr)
	}
	if _, ok := resumption.FindResumptionRecord(fabric.AddressOf(commissionedNode)); !ok {
		t.Fatal("no resumption record of the inbound session")
	}
	com.Close()
	short, cancelShort := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelShort()
	if _, _, err := handshake.CASE(short, nodeMgr, addr, nodeFabric, controllerNodeID, handshake.CASEOptions{}); err == nil {
		t.Fatal("CASE succeeded after Close")
	}
}
