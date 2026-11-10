// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package peer

import (
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

// TestProvenHostLearnedFromDiscovery: a session made from the stored
// address before discovery resolved proves no host; when discovery then
// names the target that owns the operational address, that target becomes
// the proven host, saved with the descriptor, and the next reconnect starts
// at its address although another target's ranks first in the parity
// order (issue 488).
func TestProvenHostLearnedFromDiscovery(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		var updates atomic.Int32
		h := newHarness(t, func(c *Context) { c.Updated = func(*Peer) { updates.Add(1) } })
		p, _ := h.set.For(testAddr)
		s := h.secureSession(t, 0x51, udpAddr(addr1))
		if h.set.Adopt(s) != p {
			t.Fatal("the session's peer")
		}
		eventually(t, "the saved address", func() bool { return updates.Load() > 0 })
		if got := p.Descriptor().OperationalHost; got != "" {
			t.Fatalf("OperationalHost = %q before discovery", got)
		}
		saved := updates.Load()
		h.announceTwoHosts(t)
		eventually(t, "the learned host", func() bool { return p.Descriptor().OperationalHost == h.hostQ })
		eventually(t, "the saved host", func() bool {
			ds, _ := h.store.LoadPeers(testAddr.FabricIndex)
			return updates.Load() > saved && len(ds) == 1 && ds[0].OperationalHost == h.hostQ
		})
		s.Close()
		eventually(t, "the session to go", func() bool { return !p.HasSession() })
		began := h.clock.Now()
		connectAsync(p, ConnectOptions{ConnectionTimeout: -1})
		h.sigma1(t, addr1)
		if d := h.clock.Now().Sub(began); d != 0 {
			t.Fatalf("the proven host's address began after %v", d)
		}
	})
}

// TestProvenHostAbsentTriesTheOperationalAddressFirst: the proven host is
// known, but the discovered addresses are only another target's — the
// node's SRV left the cache for the router's cache-flush answer. The
// reconnect starts at the last operational address; the router's address
// follows DelayBeforeNextAddress later, not before. The proven host's
// address arriving meanwhile at the operational address is the running
// attempt: none is started a second time (issue 488).
func TestProvenHostAbsentTriesTheOperationalAddressFirst(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, nil)
		p, err := h.set.AddKnownPeer(Descriptor{Address: testAddr, OperationalAddress: udpAddr(addr1), OperationalHost: "node1234.local"})
		if err != nil {
			t.Fatal(err)
		}
		h.mdns.announce(t, srvRR(h.qname, routerHostQ, 5540, 120), aaaaRR(routerHostQ, "fe80::1", 120))
		eventually(t, "the router's address", func() bool { return len(p.Service().Addresses()) == 1 })
		began := h.clock.Now()
		connectAsync(p, ConnectOptions{ConnectionTimeout: -1})
		h.sigma1(t, addr1)
		if d := h.clock.Now().Sub(began); d != 0 {
			t.Fatalf("the operational address began after %v", d)
		}
		c := process(p)
		key := attemptKey{ap: addressOf(udpAddr(addr1)).AddrPort()}
		var first *attempt
		eventually(t, "the operational address's attempt", func() bool {
			c.mu.Lock()
			defer c.mu.Unlock()
			first = c.attempts[key]
			return first != nil
		})
		h.clock.waitTimer(t, 45*time.Second)
		h.clock.Advance(10 * time.Second)
		h.mdns.announce(t, srvRR(h.qname, h.hostQ, 5540, 120), aaaaRR(h.hostQ, "fd00::1", 120))
		eventually(t, "the proven host's address", func() bool { return len(p.Service().Addresses()) == 2 })
		h.noSigma1(t, routerLL)
		h.clock.Advance(35*time.Second - time.Millisecond)
		h.noSigma1(t, routerLL)
		c.mu.Lock()
		same, n := c.attempts[key] == first, len(c.attempts)
		c.mu.Unlock()
		if !same || n != 1 {
			t.Fatalf("attempts %d, the first kept %v: want the operational address's attempt alone", n, same)
		}
		h.clock.Advance(time.Millisecond)
		h.sigma1(t, routerLL)
		c.mu.Lock()
		same = c.attempts[key] == first
		c.mu.Unlock()
		if !same {
			t.Fatal("the operational address's attempt did not continue")
		}
	})
}

// TestProvenHostNewAddressFirst: a proven host advertised with a new
// address only is tried there first; the old operational address is not
// attempted — a genuine address change costs nothing (issue 488).
func TestProvenHostNewAddressFirst(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, nil)
		p, err := h.set.AddKnownPeer(Descriptor{Address: testAddr, OperationalAddress: udpAddr(addr9), OperationalHost: "node1234.local"})
		if err != nil {
			t.Fatal(err)
		}
		h.announceTwoHosts(t)
		eventually(t, "the addresses", func() bool { return len(p.Service().Addresses()) == 2 })
		began := h.clock.Now()
		connectAsync(p, ConnectOptions{ConnectionTimeout: -1})
		h.sigma1(t, addr1)
		if d := h.clock.Now().Sub(began); d != 0 {
			t.Fatalf("the new address began after %v", d)
		}
		// The router's address follows, outside or after the stagger
		// (the service may deliver it first, then the proven host's
		// preempts); the old operational address never.
		for range 6 {
			h.clock.Advance(30 * time.Second)
			h.noSigma1(t, addr9)
		}
	})
}
