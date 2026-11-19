// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client_test

import (
	"context"
	"errors"
	"log/slog"
	"net/netip"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/handshake"
	"github.com/SukramJ/go-fabric/client/peer"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/tlv"
)

// TestPeersLoadFailureParity ports matter.js's PeersLoadFailureTest
// (packages/node/test/node/client/PeersLoadFailureTest.ts at 862ad75f):
// a stored node whose peer names a fabric the controller does not have —
// a peer store and a fabric store restored from different backups — is
// kept, unavailable with the cause (Peers.ts:144-161 at db021da0), the
// warning logged once and naming it; the fabric's other node loads and is
// the one commissioned node.
func TestPeersLoadFailureParity(t *testing.T) {
	t.Parallel()
	log := &msgLog{}
	orphan := handshake.PeerAddress{FabricIndex: 9, NodeID: 0x3333}
	sr := newScriptedRegistry(t, func(st *client.MemoryStore) {
		seedCommissioned(1)(st)
		_ = client.SaveRecord(st, "peer2", client.Record{
			PeerAddress:  &orphan,
			Addresses:    []client.NetworkAddress{{IP: netip.MustParseAddr("fd00::2"), Port: 5540}},
			DiscoveredAt: time.UnixMilli(1_800_000_000_000),
		})
	}, func(c *client.RegistryConfig) { c.Logger = slog.New(log) })

	nodes := sr.reg.Nodes()
	ids := make([]string, 0, len(nodes))
	for _, n := range nodes {
		ids = append(ids, n.ID())
	}
	if len(ids) != 2 || ids[0] != "peer1" || ids[1] != "peer2" {
		t.Fatalf("nodes %v, want peer1 and peer2", ids)
	}
	broken, healthy := sr.reg.Get("peer2"), sr.reg.Get("peer1")
	if err := broken.LoadError(); !errors.Is(err, client.ErrLoadFailed) || !errors.Is(err, peer.ErrNoFabric) {
		t.Fatalf("load error %v, want ErrLoadFailed naming the missing fabric", err)
	}
	if err := broken.Start(context.Background()); !errors.Is(err, client.ErrLoadFailed) {
		t.Fatalf("Start of the unavailable node: %v", err)
	}
	if healthy.LoadError() != nil || !healthy.IsCommissioned() {
		t.Fatalf("the healthy node: load error %v, commissioned %v", healthy.LoadError(), healthy.IsCommissioned())
	}
	if c := sr.reg.Commissioned(); len(c) != 1 || c[0] != healthy {
		t.Fatalf("commissioned %v, want the healthy node alone", c)
	}
	log.mu.Lock()
	warnings := 0
	for _, m := range log.msgs {
		if m == "matter.controller.client.registry.load_failed" {
			warnings++
		}
	}
	log.mu.Unlock()
	if warnings != 1 {
		t.Fatalf("%d load warnings, want 1", warnings)
	}
	// The stored data stays until the node is erased.
	if stored, err := sr.store.Load("peer2"); err != nil || len(stored) == 0 {
		t.Fatalf("the unavailable node's store: %v, %d endpoints", err, len(stored))
	}
}

// TestPeersLoadFailureByAddressParity ports PeersLoadFailureTest.ts:147-169
// at b25ed960, "finds a peer that cannot load and the peers
// after it by address": peer1's stored commissionedAt is not a timestamp,
// so it loads unavailable; its stored peer address still finds it
// (ClientNode.peerAddress reads storage when the commissioning state did
// not load, ClientNode.ts:388-421), and peer2 after it is found too.
func TestPeersLoadFailureByAddressParity(t *testing.T) {
	t.Parallel()
	address1, address2 := scriptedAddr, handshake.PeerAddress{FabricIndex: scriptedAddr.FabricIndex, NodeID: 0x3333}
	sr := newScriptedRegistry(t, func(st *client.MemoryStore) {
		seedCommissioned(1)(st)
		_ = client.SaveRecord(st, "peer2", client.Record{
			PeerAddress:  &address2,
			Addresses:    []client.NetworkAddress{{IP: netip.MustParseAddr("fd00::2"), Port: 5540}},
			DiscoveredAt: time.UnixMilli(1_800_000_000_000),
		})
		// The record is cluster 0xFFFF_FFFE on endpoint 0, commissionedAt
		// its field 5 (record.go:28, :39), a uint64 of milliseconds.
		e := tlv.NewEncoder()
		e.PutUTF8(tlv.AnonymousTag(), "not a timestamp")
		b, err := e.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		if err := st.Save("peer1", 0, 0xFFFF_FFFE, map[uint32][]byte{5: b}, nil); err != nil {
			t.Fatal(err)
		}
	}, nil)

	broken, healthy := sr.reg.Get("peer1"), sr.reg.Get("peer2")
	if broken == nil || healthy == nil {
		t.Fatalf("nodes %v, %v", broken, healthy)
	}
	if !errors.Is(broken.LoadError(), client.ErrLoadFailed) {
		t.Fatalf("load error %v, want ErrLoadFailed", broken.LoadError())
	}
	if got := sr.reg.GetByAddress(address1); got != broken {
		t.Fatalf("by address1: %v, want the node that failed to load", got)
	}
	if got := sr.reg.GetByAddress(address2); got != healthy {
		t.Fatalf("by address2: %v, want the healthy node", got)
	}
	if got, err := sr.reg.ForAddress(address2, ""); err != nil || got != healthy {
		t.Fatalf("ForAddress(address2): %v, %v; want the healthy node", got, err)
	}
	// Peers.commissioned lists the healthy node alone, as the first case
	// asserts for an orphaned fabric (PeersLoadFailureTest.ts:123-125 at
	// b25ed960); the case ported here asserts nothing about it, so for the
	// commissionedAt corruption the assertion is Go's own.
	if c := sr.reg.Commissioned(); len(c) != 1 || c[0] != healthy {
		t.Fatalf("commissioned %v, want the healthy node alone", c)
	}
}
