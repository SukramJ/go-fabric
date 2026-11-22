// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

import (
	"fmt"
	"net"
	"net/netip"
	"testing"
	"testing/synctest"

	"thingfabric.dev/go-fabric/client/handshake"
	"thingfabric.dev/go-fabric/client/peer"
)

// TestRegistryRecordFollowsDescriptorSave: the record follows every save
// of the peer's descriptor, not only a new session — CommissioningClient
// observes peer.updated (CommissioningClient.ts:743, #update :753-773 at
// db021da0), which Peer.#save emits (Peer.ts:576-580).
func TestRegistryRecordFollowsDescriptorSave(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newRegHarness(t)
		h.seed(t, "peer1", commissionedRecord(0x10), settings{})
		r := h.registry(t, nil)
		n := r.Get("peer1")
		addr := handshake.PeerAddress{FabricIndex: 1, NodeID: 0x10}
		// A known-peer merge with a new operational address saves the
		// descriptor without a session (PeerSet.ts:315-337).
		if _, err := h.set.AddKnownPeer(peer.Descriptor{Address: addr, OperationalAddress: &net.UDPAddr{IP: net.ParseIP("fd00::20"), Port: 5540}}); err != nil {
			t.Fatal(err)
		}
		want := netip.MustParseAddr("fd00::20")
		synctest.Wait()
		if a := n.Record().Addresses; len(a) != 1 || a[0].IP != want {
			t.Fatalf("the record's addresses %v did not follow the saved descriptor", n.Record().Addresses)
		}
	})
}

// TestRemovalConfirmedByFabricRemoved: CommissioningClient.ts:437-449 at
// db021da0 counts FabricRemovedError among the causes that confirm a
// RemoveFabric without a response.
func TestRemovalConfirmedByFabricRemoved(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		if !removalConfirmedBy(fmt.Errorf("invoke: %w", ErrFabricRemoved)) {
			t.Fatal("ErrFabricRemoved does not confirm the removal")
		}
		if removalConfirmedBy(fmt.Errorf("invoke: %w", ErrPeerShutdown)) {
			t.Fatal("a shutdown confirms the removal")
		}
	})
}
