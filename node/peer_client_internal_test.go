// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"
	"net/netip"
	"runtime"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/handshake"
	"github.com/SukramJ/go-fabric/client/peer"
)

// TestPeerClientFollowsTheFabricHooks: the client stack a node builds for
// its peer path (Config.Client nil) is reached by fabric removal and by
// UpdateNOC as the configured one is — its peers of a removed fabric leave
// with their records, an updated fabric's records go — and Stop closes it;
// none is built after (docs/adr/0032; R2-P2-fabric-02 for the configured
// stack).
func TestPeerClientFollowsTheFabricHooks(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	n := clientNode(t)
	// The configured stack becomes the peer path's own.
	n.peerClient.cs, n.client = n.client, nil
	cs := n.peerClient.cs
	removed, updated := handshake.PeerAddress{FabricIndex: 3, NodeID: 0x77}, handshake.PeerAddress{FabricIndex: 4, NodeID: 0x78}
	for _, a := range []handshake.PeerAddress{removed, updated} {
		if _, err := cs.peers.For(a); err != nil {
			t.Fatal(err)
		}
		if err := cs.resumption.SaveResumptionRecord(handshake.ResumptionRecord{Peer: a, ResumptionID: make([]byte, 16)}); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := n.peerStack(); err != nil || got != cs {
		t.Fatalf("peerStack = %p, %v; want the built stack", got, err)
	}
	n.forgetClientPeers(ctx, removed.FabricIndex)
	if cs.peers.Get(removed) != nil {
		t.Error("the removed fabric's peer is still in the peer path's stack")
	}
	n.closeClientFabricSessions(ctx, updated.FabricIndex)
	if _, ok := cs.resumption.FindResumptionRecord(updated); ok {
		t.Error("the updated fabric's resumption record survived UpdateNOC in the peer path's stack")
	}
	work := countWork(n.cfg.Clock, "test")
	n.closeClientStacks(ctx, work)
	work.pause()
	if n.existingPeerStack() != nil {
		t.Error("the peer path's stack survived Stop")
	}
	if _, err := n.peerStack(); err == nil {
		t.Error("a stopped node built a client stack")
	}
}

// TestFabricUpdateEndsAHandshakeInFlight: UpdateNOC ends a client-stack
// connection process to a peer of the fabric — a handshake that started
// under the old NOC does not complete after the sweep; a caller still
// waiting connects again in a new process, under the new identity
// (review II, Fable P-3). A connection process takes an address hint once
// (peer.Peer.HintAddresses), so a hint taken again shows the new process.
func TestFabricUpdateEndsAHandshakeInFlight(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	n := clientNode(t)
	addr := handshake.PeerAddress{FabricIndex: 3, NodeID: 0x77}
	p, err := n.client.peers.For(addr)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = p.Connect(ctx, peer.ConnectOptions{ConnectionTimeout: time.Hour})
	}()
	// The process runs once it takes the hint (the silent DNS-SD cache
	// answers nothing; nobody answers at the address either).
	hint := netip.MustParseAddrPort("[::1]:9")
	for p.HintAddresses(hint) == 0 {
		runtime.Gosched()
	}
	if p.HintAddresses(hint) != 0 {
		t.Fatal("one process took the same hint twice")
	}
	n.closeClientFabricSessions(ctx, addr.FabricIndex)
	deadline := time.Now().Add(5 * time.Second)
	for p.HintAddresses(hint) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("UpdateNOC left the connection process under the old NOC running")
		}
		runtime.Gosched()
	}
	cancel()
	<-done
}
