// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package peer

import (
	"context"
	"testing"
	"testing/synctest"
	"time"
)

// TestMonitorTakesOnlyUnicastDestinations: the address monitor applies
// the connection's filter (unicastDestination) to the discovered
// addresses: an awaited LIT ICD adopts a unicast address and never a
// multicast or unspecified one, a set of only such addresses leaves its
// session where it is, and the probe walk never probes one (review II,
// R3-bound-3551, the residual of R2-P3-ctl-lan-09).
func TestMonitorTakesOnlyUnicastDestinations(t *testing.T) {
	t.Parallel()
	lit := func() bool { return true }
	t.Run("ICD adopts the unicast address", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			h, p, n := monitored(t)
			p.SetICDAwait(lit)
			h.clock.Advance(3 * time.Minute)
			h.announceAddrs(t, "ff02::1", "fd00::2")
			eventually(t, "the addresses", func() bool { return len(p.Service().Addresses()) == 2 })
			h.udp.drain()
			if r := waitVerify(t, verifyAsync(context.Background(), p, ReasonAddressChange)); !r.ok {
				t.Fatal("verification failed")
			}
			if got := n.s.Addr().String(); got != addr2 {
				t.Fatalf("session at %v, want %v", got, addr2)
			}
		})
	})
	t.Run("ICD keeps its address without a unicast one", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			h, p, n := monitored(t)
			p.SetICDAwait(lit)
			h.clock.Advance(3 * time.Minute)
			h.announceAddrs(t, "ff02::1", "::")
			eventually(t, "the addresses", func() bool { return len(p.Service().Addresses()) == 2 })
			h.udp.drain()
			if r := waitVerify(t, verifyAsync(context.Background(), p, ReasonAddressChange)); !r.ok {
				t.Fatal("verification failed")
			}
			if got := n.s.Addr().String(); got != addr1 {
				t.Fatalf("session moved to %v, want it kept at %v", got, addr1)
			}
		})
	})
	t.Run("the probe walk passes non-unicast addresses over", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			h, p, n := monitored(t)
			h.clock.Advance(time.Hour)
			h.announceAddrs(t, "ff02::1", "fd00::2")
			eventually(t, "the addresses", func() bool { return len(p.Service().Addresses()) == 2 })
			h.udp.drain()
			r, to := failProbes(t, h, verifyAsync(context.Background(), p, ReasonAddressChange))
			if r.ok || !n.s.IsPeerLost() {
				t.Fatalf("ok %v lost %v", r.ok, n.s.IsPeerLost())
			}
			seen := map[string]bool{}
			for _, a := range to {
				seen[a] = true
			}
			if seen["[ff02::1]:5540"] || !seen[addr1] || !seen[addr2] {
				t.Fatalf("probed %v, want %v and %v and no multicast address", to, addr1, addr2)
			}
		})
	})
}
