// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package binding

import "testing"

// TestPeerCreatedForAnEntryIsReleasedWithItsLastEntry: a peer the manager
// created for a client entry is released — its connection process
// stopped, the peer closed when it has no session — once the last entry
// naming that node goes; a peer the set already held is left alone.
// matter.js keeps the peer it created (BindingManager.ts:584-585 with
// :613-627 at db021da0); the rule is this module's (review II,
// R2-P6-bound-2104, TM R3.4).
func TestPeerCreatedForAnEntryIsReleasedWithItsLastEntry(t *testing.T) {
	t.Parallel()
	h := newHarness(t, nil)
	h.listen()
	e1 := Entry{FabricIndex: 1, Node: 2, Endpoint: 1, Cluster: 0x0006}
	e2 := Entry{FabricIndex: 1, Node: 2, Endpoint: 3, Cluster: 0x0006}
	h.m.Register(1, e1)
	h.m.Register(1, e2)
	h.flush()
	p, _ := h.peers.For(peerAddr(1, 2))
	h.m.Unregister(1, e1)
	h.flush()
	if n := h.link.released(p); n != 0 {
		t.Fatalf("released %d times while an entry still names the node", n)
	}
	h.m.Unregister(1, e2)
	h.flush()
	waitFor(t, func() bool { return h.link.released(p) == 1 })

	// A peer the set held before the entry is not the manager's to release.
	known, _ := h.peers.For(peerAddr(1, 3))
	e3 := Entry{FabricIndex: 1, Node: 3, Endpoint: 1, Cluster: 0x0006}
	h.m.Register(1, e3)
	h.flush()
	h.m.Unregister(1, e3)
	h.flush()
	if n := h.link.released(known); n != 0 {
		t.Fatalf("a peer the set already held was released %d times", n)
	}
}
