// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package peer

import (
	"context"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/client/handshake"
)

// TestContextPeerAdded: the set calls PeerAdded once for every peer that
// joins it — not again for a peer it knows — outside its lock (the hook
// reads the set), and before the peer's first session, so an observer
// registered there sees it.
func TestContextPeerAdded(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var added []handshake.PeerAddress
	sessions := map[handshake.PeerAddress]int{}
	var h *harness
	h = newHarness(t, func(c *Context) {
		c.PeerAdded = func(p *Peer) {
			if !h.set.Has(p.Address()) { // takes the set's lock: deadlocks under it
				t.Errorf("peer %v is not in the set yet", p.Address())
			}
			mu.Lock()
			added = append(added, p.Address())
			mu.Unlock()
			p.Observe(Observer{SessionAdded: func(*exchange.Session) {
				mu.Lock()
				sessions[p.Address()]++
				mu.Unlock()
			}})
		}
	})
	s := h.secureSession(t, 0x60, udpAddr(addr1))
	p := h.set.Adopt(s)
	if p == nil {
		t.Fatal("Adopt returned no peer")
	}
	if again := h.set.Adopt(h.secureSession(t, 0x61, udpAddr(addr1))); again != p {
		t.Fatal("a second session made another peer")
	}
	if _, err := h.set.For(handshake.PeerAddress{FabricIndex: 1, NodeID: testNode + 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.set.For(p.Address()); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(added) != 2 || added[0] != p.Address() || added[1].NodeID != testNode+1 {
		t.Fatalf("PeerAdded for %v, want the two peers once each", added)
	}
	if sessions[p.Address()] != 2 {
		t.Fatalf("the observer registered in PeerAdded saw %d sessions, want 2", sessions[p.Address()])
	}
}

// verdict is one ReachabilityVerified call.
type verdict struct {
	reason  ReachabilityReason
	ok      bool
	address string
}

// observeVerdicts registers a ReachabilityVerified observer on p.
func observeVerdicts(p *Peer) func() []verdict {
	var mu sync.Mutex
	var got []verdict
	p.Observe(Observer{ReachabilityVerified: func(reason ReachabilityReason, ok bool, address string) {
		mu.Lock()
		got = append(got, verdict{reason, ok, address})
		mu.Unlock()
	}})
	return func() []verdict {
		mu.Lock()
		defer mu.Unlock()
		return append([]verdict(nil), got...)
	}
}

// TestReachabilityVerifiedOncePerRun: a run that answers on the current
// address, one that migrates and one whose probes all fail each report
// one verdict, with the session's address after the run ("" when it was
// given up), for both reasons; a run that needs no probe reports none.
func TestReachabilityVerifiedOncePerRun(t *testing.T) {
	t.Parallel()
	t.Run("no probe", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			_, p, _ := monitored(t)
			got := observeVerdicts(p)
			if r := waitVerify(t, verifyAsync(context.Background(), p, ReasonAddressChange)); !r.ok {
				t.Fatal("nothing discovered: want true without a probe")
			}
			if v := got(); len(v) != 0 {
				t.Fatalf("verdicts %+v, want none", v)
			}
		})
	})
	t.Run("suspect answered", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			h, p, n := monitored(t)
			got := observeVerdicts(p)
			res := verifyAsync(context.Background(), p, ReasonSessionSuspect)
			n.answer(t, h.udp.next(t))
			if r := waitVerify(t, res); !r.ok {
				t.Fatal("an answered probe: want true")
			}
			if v := got(); len(v) != 1 || v[0] != (verdict{ReasonSessionSuspect, true, addr1}) {
				t.Fatalf("verdicts %+v, want one success at %s", v, addr1)
			}
		})
	})
	t.Run("address change migrates", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			h, p, n := monitored(t)
			got := observeVerdicts(p)
			h.clock.Advance(time.Hour)
			h.announceAddrs(t, "fd00::2")
			eventually(t, "the address", func() bool { return len(p.Service().Addresses()) == 1 })
			h.udp.drain()
			res := verifyAsync(context.Background(), p, ReasonAddressChange)
			for answered := false; !answered; {
				select {
				case d := <-h.udp.ch:
					if d.to.String() == addr2 {
						n.answer(t, d)
						answered = true
					}
				case <-time.After(5 * time.Millisecond):
					h.clock.Advance(time.Second)
				}
			}
			if r := waitVerify(t, res); !r.ok {
				t.Fatal("migration: want true")
			}
			if v := got(); len(v) != 1 || v[0] != (verdict{ReasonAddressChange, true, addr2}) {
				t.Fatalf("verdicts %+v, want one success at %s", v, addr2)
			}
		})
	})
	for _, reason := range []ReachabilityReason{ReasonAddressChange, ReasonSessionSuspect} {
		t.Run(reason.String()+" all fail", func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				h, p, _ := monitored(t)
				got := observeVerdicts(p)
				h.clock.Advance(time.Hour)
				h.announceAddrs(t, "fd00::2")
				eventually(t, "the address", func() bool { return len(p.Service().Addresses()) == 1 })
				h.udp.drain()
				if r, _ := failProbes(t, h, verifyAsync(context.Background(), p, reason)); r.ok {
					t.Fatal("unanswered probes: want false")
				}
				if v := got(); len(v) != 1 || v[0] != (verdict{reason, false, ""}) {
					t.Fatalf("verdicts %+v, want one failure", v)
				}
			})
		})
	}
}

// TestContextPeerRemoved: a closed peer leaves the set and PeerRemoved
// runs once, outside the set's lock (the hook reads the set); a second
// Close removes nothing more.
func TestContextPeerRemoved(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var removed []handshake.PeerAddress
	var h *harness
	h = newHarness(t, func(c *Context) {
		c.PeerRemoved = func(p *Peer) {
			if h.set.Has(p.Address()) { // takes the set's lock: deadlocks under it
				t.Errorf("peer %v is still in the set", p.Address())
			}
			mu.Lock()
			removed = append(removed, p.Address())
			mu.Unlock()
		}
	})
	p := h.set.Adopt(h.secureSession(t, 0x62, udpAddr(addr1)))
	p.Close(context.Background())
	p.Close(context.Background())
	mu.Lock()
	defer mu.Unlock()
	if len(removed) != 1 || removed[0] != p.Address() {
		t.Fatalf("PeerRemoved for %v, want the peer once", removed)
	}
}
