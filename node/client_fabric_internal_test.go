// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"
	"log/slog"
	"testing"

	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/client/handshake"
	"github.com/SukramJ/go-fabric/client/peer"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/secure/sigma"
)

// quietMDNS is an mDNS socket that sends nowhere and receives nothing.
type quietMDNS struct{ out chan mdns.QueryResponse }

func (s *quietMDNS) Send([]byte) error                    { return nil }
func (s *quietMDNS) Responses() <-chan mdns.QueryResponse { return s.out }
func (s *quietMDNS) Close() error                         { return nil }

// clientNode is a node with a client stack whose DNS-SD cache is silent
// and whose peer set takes a peer of any fabric index.
func clientNode(t *testing.T) *Node {
	t.Helper()
	names := discovery.NewNames(&quietMDNS{out: make(chan mdns.QueryResponse)}, slog.New(slog.DiscardHandler))
	t.Cleanup(func() { _ = names.Close() })
	cfg := mdnsTestConfig(t)
	cfg.Network.MDNS.Off = true
	cfg.Client = &ClientConfig{Listen: "127.0.0.1:0", Names: names}
	n := newMDNSTestNode(t, cfg)
	anyFabric := func(handshake.PeerAddress) (handshake.Fabric, bool) {
		return handshake.Fabric{Identity: &sigma.Identity{}}, true
	}
	peers, err := peer.NewSet(peer.Context{
		Exchanges: n.client.exchanges, Names: n.client.names, Fabrics: anyFabric,
		Resumption: n.client.resumption, Logger: n.logger, Clock: n.cfg.Clock,
	})
	if err != nil {
		t.Fatal(err)
	}
	n.client.peers.Close(context.Background())
	n.client.peers = peers
	return n
}

// TestFabricRemovalDeletesTheClientPeers: a removed fabric's peers leave
// the client stack's peer set with their resumption records; another
// fabric's stay (review II, R2-P2-fabric-02).
func TestFabricRemovalDeletesTheClientPeers(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	n := clientNode(t)
	gone, kept := handshake.PeerAddress{FabricIndex: 3, NodeID: 0x77}, handshake.PeerAddress{FabricIndex: 4, NodeID: 0x78}
	for _, a := range []handshake.PeerAddress{gone, kept} {
		if _, err := n.client.peers.For(a); err != nil {
			t.Fatal(err)
		}
		if err := n.client.resumption.SaveResumptionRecord(handshake.ResumptionRecord{Peer: a, ResumptionID: make([]byte, 16)}); err != nil {
			t.Fatal(err)
		}
	}
	n.forgetClientPeers(ctx, gone.FabricIndex)
	if n.client.peers.Get(gone) != nil {
		t.Error("the removed fabric's peer is still in the peer set")
	}
	if _, ok := n.client.resumption.FindResumptionRecord(gone); ok {
		t.Error("the removed fabric's resumption record survived")
	}
	if n.client.peers.Get(kept) == nil {
		t.Error("another fabric's peer left the peer set")
	}
	if _, ok := n.client.resumption.FindResumptionRecord(kept); !ok {
		t.Error("another fabric's resumption record went")
	}
}

// TestFabricUpdateForgetsTheClientResumptions: after UpdateNOC the
// client stack forgets the fabric's resumption records and keeps its
// peers, which connect again under the new NOC (R2-P2-fabric-02).
func TestFabricUpdateForgetsTheClientResumptions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	n := clientNode(t)
	updated, other := handshake.PeerAddress{FabricIndex: 3, NodeID: 0x77}, handshake.PeerAddress{FabricIndex: 4, NodeID: 0x78}
	for _, a := range []handshake.PeerAddress{updated, other} {
		if _, err := n.client.peers.For(a); err != nil {
			t.Fatal(err)
		}
		if err := n.client.resumption.SaveResumptionRecord(handshake.ResumptionRecord{Peer: a, ResumptionID: make([]byte, 16)}); err != nil {
			t.Fatal(err)
		}
	}
	n.closeClientFabricSessions(ctx, updated.FabricIndex)
	if _, ok := n.client.resumption.FindResumptionRecord(updated); ok {
		t.Error("the updated fabric's resumption record survived UpdateNOC")
	}
	if n.client.peers.Get(updated) == nil {
		t.Error("the updated fabric's peer left the peer set")
	}
	if _, ok := n.client.resumption.FindResumptionRecord(other); !ok {
		t.Error("another fabric's resumption record went")
	}
}
