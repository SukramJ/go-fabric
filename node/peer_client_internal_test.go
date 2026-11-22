// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"bytes"
	"context"
	"net"
	"net/netip"
	"runtime"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/client/exchange"
	"thingfabric.dev/go-fabric/client/handshake"
	"thingfabric.dev/go-fabric/client/peer"
	mattercore "thingfabric.dev/go-fabric/cluster/core"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/secure/channel"
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

// clientSessionTo opens a secure session of the node's configured client
// stack to addr and adopts it into the peer set: a session the node opened.
// Nobody answers at its address, so a failed send does not count as the
// peer's loss (SuppressPeerLoss): only the sweep under test closes it.
func clientSessionTo(t *testing.T, n *Node, id uint16, addr handshake.PeerAddress) *exchange.Session {
	t.Helper()
	key := bytes.Repeat([]byte{byte(id)}, 16)
	s, err := n.client.exchanges.NewSecureSession(exchange.SecureSessionConfig{
		ID: id, Addr: &net.UDPAddr{IP: net.IPv6loopback, Port: 5540}, FabricIndex: addr.FabricIndex,
		Channel:          channel.Config{EncryptKey: key, DecryptKey: key, LocalNodeID: 0x1111, PeerNodeID: addr.NodeID, PeerSessionID: 0x7000 + id},
		SuppressPeerLoss: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if n.client.peers.Adopt(s) == nil {
		t.Fatal("the session was not adopted")
	}
	return s
}

// TestClientSessionCommandGetsItsResponse: an UpdateNOC or a RemoveFabric
// invoked on a client-stack session sweeps the fabric's client sessions —
// and its own after its response (im.DeferAfterResponse), as the bridge
// closes its invoking session; another session of the fabric goes at once
// (review II, Fable R2-1).
func TestClientSessionCommandGetsItsResponse(t *testing.T) {
	t.Parallel()
	addr := handshake.PeerAddress{FabricIndex: 3, NodeID: 0x77}
	invokeOn := func(s *exchange.Session) (context.Context, *im.AfterResponse) {
		after := &im.AfterResponse{}
		return im.WithAfterResponse(mattercore.WithInvokeSession(context.Background(), s), after), after
	}
	t.Run("UpdateNOC", func(t *testing.T) {
		t.Parallel()
		n := clientNode(t)
		invoking := clientSessionTo(t, n, 0x0301, addr)
		other := clientSessionTo(t, n, 0x0302, addr)
		if invoking.IsClosed() || other.IsClosed() {
			t.Fatalf("closed before the sweep: invoking %v, other %v", invoking.IsClosed(), other.IsClosed())
		}
		ctx, after := invokeOn(invoking)
		n.closeClientFabricSessions(ctx, addr.FabricIndex)
		if invoking.IsClosed() {
			t.Fatal("the invoking session closed before the response")
		}
		if !other.IsClosed() {
			t.Error("another session of the fabric outlived the sweep")
		}
		after.Run()
		if !invoking.IsClosed() {
			t.Error("the invoking session outlived the response")
		}
	})
	t.Run("RemoveFabric", func(t *testing.T) {
		t.Parallel()
		n := clientNode(t)
		invoking := clientSessionTo(t, n, 0x0303, addr)
		ctx, after := invokeOn(invoking)
		n.forgetClientPeers(ctx, addr.FabricIndex)
		if invoking.IsClosed() || n.client.peers.Get(addr) == nil {
			t.Fatal("the invoking session's peer went before the response")
		}
		after.Run()
		if !invoking.IsClosed() || n.client.peers.Get(addr) != nil {
			t.Error("the invoking session's peer outlived the response")
		}
	})
}

// TestClientServerStampsTheSessionIdentity: a request on a client-stack
// session carries the *exchange.Session as its identity and no bridge id
// (review II, Fable R2-1).
func TestClientServerStampsTheSessionIdentity(t *testing.T) {
	t.Parallel()
	n := clientNode(t)
	s := clientSessionTo(t, n, 0x0304, handshake.PeerAddress{FabricIndex: 3, NodeID: 0x77})
	ctx := n.clientServerConfig().RequestContext(context.Background(), s)
	if got, _ := mattercore.InvokeSessionFromContext(ctx).(*exchange.Session); got != s {
		t.Errorf("identity %v, want the session", mattercore.InvokeSessionFromContext(ctx))
	}
	if id := mattercore.InvokeSessionIDFromContext(ctx); id != 0 {
		t.Errorf("bridge id %d stamped on a client-stack request", id)
	}
}
