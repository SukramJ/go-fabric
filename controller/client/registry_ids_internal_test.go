// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

import (
	"strconv"
	"testing"

	"github.com/SukramJ/go-fabric/client/handshake"
)

// TestCallerChosenIDIsReserved (review II, R2-P1-ctl-01): a local id the
// caller names in ForDevice or ForAddress is reserved like a stored one,
// so the counter never hands it to a second node, and an allocation skips
// an id a node already holds. Before, the caller's "peer1" stayed free
// and the next allocation returned it again: two nodes under one id and
// one set of store rows.
func TestCallerChosenIDIsReserved(t *testing.T) {
	t.Parallel()
	h := newRegHarness(t)
	r := h.registry(t, nil)
	assertUnique := func(t *testing.T) {
		t.Helper()
		seen := map[string]bool{}
		for _, n := range r.Nodes() {
			if seen[n.ID()] {
				t.Fatalf("two nodes hold local id %q: %v", n.ID(), ids(r.Nodes()))
			}
			seen[n.ID()] = true
		}
	}

	// ForDevice: the caller's "peer1", then an allocation.
	own, err := r.ForDevice(t.Context(), device("A"), "peer1")
	if err != nil || own.ID() != "peer1" {
		t.Fatalf("ForDevice with an own id: %v %v", own, err)
	}
	next, err := r.ForDevice(t.Context(), device("B"), "")
	if err != nil {
		t.Fatal(err)
	}
	assertUnique(t)
	if next.ID() != "peer2" {
		t.Fatalf("allocated %q after the caller's peer1, want peer2", next.ID())
	}

	// ForAddress: the caller's "peer7" moves the counter past it.
	if n, err := r.ForAddress(handshake.PeerAddress{FabricIndex: 1, NodeID: 0x40}, "peer7"); err != nil || n.ID() != "peer7" {
		t.Fatalf("ForAddress with an own id: %v %v", n, err)
	}
	n8, err := r.ForAddress(handshake.PeerAddress{FabricIndex: 1, NodeID: 0x41}, "")
	if err != nil {
		t.Fatal(err)
	}
	assertUnique(t)
	if n8.ID() != "peer8" {
		t.Fatalf("allocated %q after the caller's peer7, want peer8", n8.ID())
	}

	// An id a node holds is skipped even where the counter did not see it
	// reserved: a node registered by a path that does not reserve.
	r.mu.Lock()
	held := r.nextID
	r.mu.Unlock()
	if _, err := r.ForDevice(t.Context(), device("C"), "peer"+strconv.Itoa(held)); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	r.nextID = held // as if the id had not been reserved
	r.mu.Unlock()
	if _, err := r.ForDevice(t.Context(), device("D"), ""); err != nil {
		t.Fatal(err)
	}
	assertUnique(t)
}
