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
)

// TestPeersLoadFailureParity ports matter.js's PeersLoadFailureTest
// (packages/node/test/node/client/PeersLoadFailureTest.ts at 862ad75f):
// a stored node whose peer names a fabric the controller does not have —
// a peer store and a fabric store restored from different backups — is
// kept, unavailable with the cause (Peers.ts:144-161), the warning logged
// once and naming it; the fabric's other node loads and is the one
// commissioned node.
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
