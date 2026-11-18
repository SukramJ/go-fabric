// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package peer

import (
	"context"
	"testing"
	"testing/synctest"
)

// TestAddingASessionDoesNotHoldThePeerAcrossTheDiscoveryLoop: a session
// added while the discovery loop is busy waits for the loop without the
// peer's lock, so Sessions and NewestSession — every interaction's
// Session() — are not held up by a slow loop (review II, R2-P6-conc-2112).
func TestAddingASessionDoesNotHoldThePeerAcrossTheDiscoveryLoop(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, nil)
		p, err := h.set.AddKnownPeer(Descriptor{Address: testAddr})
		if err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		_, release := h.mdns.hold()
		defer release()
		// A browse makes the loop send a query, which the fake holds.
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go func() { _, _ = h.names.Browse(ctx, "_held._tcp.local") }()
		synctest.Wait()
		s := h.secureSession(t, 0x0201, udpAddr("[fd00::1]:5540"))
		go h.set.Adopt(s)
		synctest.Wait() // Adopt waits for the loop
		if !p.mu.TryLock() {
			t.Fatal("the peer's lock is held while the session waits for the discovery loop")
		}
		p.mu.Unlock()
		release()
		cancel()
		synctest.Wait()
		if got := p.NewestSession(); got != s {
			t.Fatalf("NewestSession = %v, want the adopted session", got)
		}
	})
}
