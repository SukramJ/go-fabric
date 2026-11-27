// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client_test

import (
	"context"
	"errors"
	"net/netip"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/controller/client"
	"thingfabric.dev/go-fabric/tlv"
)

// corruptCommissionedAt stores a commissionedAt (field 5 of the record,
// cluster 0xFFFF_FFFE on endpoint 0) that is not a timestamp, so id loads
// unavailable while its other stored fields stay as they were.
func corruptCommissionedAt(t *testing.T, st *client.MemoryStore, id string) {
	t.Helper()
	e := tlv.NewEncoder()
	e.PutUTF8(tlv.AnonymousTag(), "not a timestamp")
	b, err := e.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Save(id, 0, 0xFFFF_FFFE, map[uint32][]byte{5: b}, nil); err != nil {
		t.Fatal(err)
	}
}

// TestPeerAddressOfNodeThatFailedToLoad: Node.PeerAddress answers the
// stored address of a node that failed to load, as matter.js's
// ClientNode.peerAddress reads storage when the commissioning state did
// not load (packages/node/src/node/ClientNode.ts:408-418 at db91886c),
// while the node stays out of the commissioned predicate
// (lifecycle.isCommissioned, NodeLifecycle.ts:94-96 at db91886c;
// Peers.commissioned, Peers.ts:416-419). Go's own case (F-b25ed960-24):
// matter.js has no test of the getter on a failed node; its by-address
// lookup is ported in TestPeersLoadFailureByAddressParity.
func TestPeerAddressOfNodeThatFailedToLoad(t *testing.T) {
	t.Parallel()
	sr := newScriptedRegistry(t, func(st *client.MemoryStore) {
		seedCommissioned(1)(st)
		corruptCommissionedAt(t, st, "peer1")
	}, nil)
	broken := sr.reg.Get("peer1")
	if broken == nil || !errors.Is(broken.LoadError(), client.ErrLoadFailed) {
		t.Fatalf("peer1 %v, want a node that failed to load", broken)
	}

	// It answers the stored address.
	if a, ok := broken.PeerAddress(); !ok || a != scriptedAddr {
		t.Fatalf("PeerAddress %v, %v; want the stored %v", a, ok, scriptedAddr)
	}
	if got := sr.reg.GetByAddress(scriptedAddr); got != broken {
		t.Fatalf("GetByAddress %v, want the node that failed to load", got)
	}

	// It is not commissioned.
	if broken.IsCommissioned() {
		t.Fatal("IsCommissioned true for a node that failed to load")
	}
	if c := sr.reg.Commissioned(); len(c) != 0 {
		t.Fatalf("Commissioned %v, want none", c)
	}

	// The stored address goes with the node.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := sr.reg.Forget(ctx, broken); err != nil {
		t.Fatalf("Forget: %v", err)
	}
	if a, ok := broken.PeerAddress(); ok {
		t.Fatalf("PeerAddress after Forget %v, want none", a)
	}
	if got := sr.reg.GetByAddress(scriptedAddr); got != nil {
		t.Fatalf("GetByAddress after Forget %v, want nil", got)
	}
}

// TestPeerAddressOfNodeThatFailedToLoadWithoutStoredAddress: a failed
// node whose stored record names no address has none
// (ClientNode.ts:420 at db91886c, undefined).
func TestPeerAddressOfNodeThatFailedToLoadWithoutStoredAddress(t *testing.T) {
	t.Parallel()
	sr := newScriptedRegistry(t, func(st *client.MemoryStore) {
		_ = client.SaveRecord(st, "peer1", client.Record{
			Addresses:    []client.NetworkAddress{{IP: netip.MustParseAddr("fd00::1"), Port: 5540}},
			DiscoveredAt: time.UnixMilli(1_800_000_000_000),
		})
		corruptCommissionedAt(t, st, "peer1")
	}, nil)
	broken := sr.reg.Get("peer1")
	if broken == nil || !errors.Is(broken.LoadError(), client.ErrLoadFailed) {
		t.Fatalf("peer1 %v, want a node that failed to load", broken)
	}
	if a, ok := broken.PeerAddress(); ok {
		t.Fatalf("PeerAddress %v, want none", a)
	}
	if broken.IsCommissioned() {
		t.Fatal("IsCommissioned true")
	}
}
