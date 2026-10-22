// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package peer

import (
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/handshake"
)

// TestAddKnownPeerCallsFabricsWithoutTheLock pins G2-13 (ADR 0026 P6):
// the host's fabric lookup (Context.Fabrics) runs without the set's lock,
// so a lookup that reads the set itself does not deadlock.
func TestAddKnownPeerCallsFabricsWithoutTheLock(t *testing.T) {
	t.Parallel()
	var set *Set
	h := newHarness(t, func(c *Context) {
		inner := c.Fabrics
		c.Fabrics = func(a handshake.PeerAddress) (handshake.Fabric, bool) {
			_ = set.Size() // takes the set's lock
			return inner(a)
		}
	})
	set = h.set
	done := make(chan error, 1)
	go func() {
		_, err := h.set.AddKnownPeer(Descriptor{Address: testAddr, OperationalAddress: udpAddr(addr1)})
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("AddKnownPeer deadlocked: Fabrics ran under the set's lock")
	}
	if h.set.Size() != 1 {
		t.Fatalf("%d peers, want 1", h.set.Size())
	}
}
