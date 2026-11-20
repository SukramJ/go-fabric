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
	// b25ed960). The case ported here asserts nothing about it: for this
	// corruption the assertion is Go's own.
	if c := sr.reg.Commissioned(); len(c) != 1 || c[0] != healthy {
		t.Fatalf("commissioned %v, want the healthy node alone", c)
	}
}

// failedToLoad is a registry whose stored peer1, the node {1, 0x2222},
// fails to load: its commissionedAt is not a timestamp.
func failedToLoad(t *testing.T) (*scriptedRegistry, *client.Node) {
	t.Helper()
	sr := newScriptedRegistry(t, func(st *client.MemoryStore) {
		seedCommissioned(1)(st)
		corruptCommissionedAt(t, st, "peer1")
	}, nil)
	broken := sr.reg.Get("peer1")
	if broken == nil || !errors.Is(broken.LoadError(), client.ErrLoadFailed) {
		t.Fatalf("peer1 %v, want a node that failed to load", broken)
	}
	return sr, broken
}

// expectFailedNodeKept: the node that failed to load is still held, found
// by its stored address, and its stored data is untouched.
func expectFailedNodeKept(t *testing.T, sr *scriptedRegistry, broken *client.Node) {
	t.Helper()
	if sr.reg.Get("peer1") != broken || sr.reg.GetByAddress(scriptedAddr) != broken || sr.reg.Size() != 1 {
		t.Fatalf("after the refusal: peer1 held %v, found by address %v, %d nodes; want the failed node kept and none created",
			sr.reg.Get("peer1") == broken, sr.reg.GetByAddress(scriptedAddr) == broken, sr.reg.Size())
	}
	if stored, err := sr.store.Load("peer1"); err != nil || len(stored) == 0 {
		t.Fatalf("the node's store: %v, %d endpoints; want it kept", err, len(stored))
	}
}

// TestCompleteCommissioningOnNodeThatFailedToLoad: CompleteCommissioning
// on the address of a stored node that failed to load fails with its load
// error and erases nothing. Mirrors matter.js at b25ed960, read, not a
// ported case: the already-commissioned check passes (Peers.ts:269-272,
// lifecycle.isCommissioned false), forAddress returns the failed node
// through its stored peerAddress (:278, :468-470), and runCommissioning
// rejects it before the operation runs (#runFabricOperation, :638-656,
// assertNotGone at :641; EndpointLifecycle.ts:146-175 CrashedDependencyError
// with the construction error), so the node.delete() of the operation's
// catch (:348-360) is never reached.
func TestCompleteCommissioningOnNodeThatFailedToLoad(t *testing.T) {
	t.Parallel()
	sr, broken := failedToLoad(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	n, err := sr.reg.CompleteCommissioning(ctx, scriptedAddr.NodeID, client.CompleteOptions{})
	if !errors.Is(err, client.ErrLoadFailed) || n != nil {
		t.Fatalf("CompleteCommissioning: %v, %v; want the load error of the node ForAddress returned", n, err)
	}
	expectFailedNodeKept(t, sr, broken)
}

// TestDecommissionOfNodeThatFailedToLoad: Decommission of a node that
// failed to load fails with its load error and erases nothing — matter.js
// ClientNode.decommission (ClientNode.ts:200-210 at b25ed960) rejects in
// its #peers getter (:175-178, assertNotGone) before runDecommissioning,
// and runDecommissioning itself asserts the same (Peers.ts:619-621,
// :641). Forget stays the way to drop it.
func TestDecommissionOfNodeThatFailedToLoad(t *testing.T) {
	t.Parallel()
	sr, broken := failedToLoad(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := sr.reg.Decommission(ctx, broken); !errors.Is(err, client.ErrLoadFailed) {
		t.Fatalf("Decommission: %v, want the load error", err)
	}
	expectFailedNodeKept(t, sr, broken)
	if err := sr.reg.Forget(ctx, broken); err != nil {
		t.Fatalf("Forget: %v", err)
	}
	if sr.reg.Size() != 0 {
		t.Fatalf("%d nodes after Forget, want none", sr.reg.Size())
	}
	if stored, err := sr.store.Load("peer1"); err == nil && len(stored) != 0 {
		t.Fatalf("the node's store kept %d endpoints after Forget, want it erased", len(stored))
	}
}
