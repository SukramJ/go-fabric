// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package peer

// A port of matter.js packages/protocol/test/peer/PeerConnectionFallbackRetirementTest.ts at
// 862ad75f: retiring the speculative fallback address once discovery
// settled without it, the supersession decision, the keep-alive guard of
// deleteAddress and the scheduler stagger.
//
// matter.js drives mirrors of the closures; Go drives the closures
// themselves: the retirement worker and the stagger through the running
// connection process, fallbackSuperseded and deleteAddressLocked on a
// connection of a real peer.

import (
	"context"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/discovery"
)

const addrAlt = "[fd00::5]:5540" // the discovered alternative, :116

// fallbackPeer is a peer whose operational address addr9 is the fallback,
// with the given addresses discovered.
func fallbackPeer(t *testing.T, discovered ...string) (*harness, *Peer) {
	t.Helper()
	h := newHarness(t, nil)
	p, _ := h.set.AddKnownPeer(Descriptor{Address: testAddr, OperationalAddress: udpAddr(addr9)})
	if len(discovered) > 0 {
		h.announceAddrs(t, discovered...)
		eventually(t, "the discovered addresses", func() bool { return len(p.Service().Addresses()) == len(discovered) })
	}
	return h, p
}

// attemptAt is the process's attempt at addr.
func attemptAt(c *connection, addr string) *attempt {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.attempts[attemptKey{ap: addressOf(udpAddr(addr)).AddrPort()}]
}

// TestPeerConnectionFallbackRetirementAfterStabilization is the describe
// "retire after stabilization" (the retiring case is
// TestFallbackRetiredAfterStabilization).
func TestPeerConnectionFallbackRetirementAfterStabilization(t *testing.T) {
	t.Parallel()
	t.Run("does not retire a fallback that discovery re-advertises during the debounce", func(t *testing.T) {
		t.Parallel()
		h, p := fallbackPeer(t)
		connectAsync(p, ConnectOptions{ConnectionTimeout: -1})
		h.sigma1(t, addr9)
		c := process(p)
		fb := attemptAt(c, addr9)
		h.announceAddrs(t, "fd00::5")
		h.clock.waitTimer(t, 10*time.Second) // the debounce
		h.clock.Advance(5 * time.Second)     // :145
		h.announceAddrs(t, "fd00::5", "fd00::9")
		eventually(t, "the re-advertised fallback", func() bool { return len(p.Service().Addresses()) == 2 })
		h.clock.Advance(6 * time.Second) // :150
		h.clock.Yield()
		// Discovery naming the fallback promotes it to a real address
		// (PeerConnection.ts:346-352), so Go's flag clears where the mirror's
		// stays set; the attempt itself is the one that ran before.
		if got := attemptAt(c, addr9); got != fb || fb.ctx.Err() != nil {
			t.Fatal("the re-advertised fallback was retired")
		}
	})

	t.Run("does not retire while no discovered alternative exists", func(t *testing.T) {
		t.Parallel()
		h, p := fallbackPeer(t)
		connectAsync(p, ConnectOptions{ConnectionTimeout: -1})
		h.sigma1(t, addr9)
		c := process(p)
		fb := attemptAt(c, addr9)
		for range 60 { // :172, the Sigma1 retransmissions drained
			h.clock.Advance(time.Second)
			h.udp.drain()
		}
		h.clock.Yield()
		c.mu.Lock()
		marked := c.fallback != nil && c.fallback.AddrPort() == addressOf(udpAddr(addr9)).AddrPort()
		c.mu.Unlock()
		if got := attemptAt(c, addr9); !marked || got != fb || fb.ctx.Err() != nil {
			t.Fatal("the fallback was retired without a discovered alternative")
		}
	})
}

// TestPeerConnectionFallbackRetirementSuperseded is the describe
// "fallbackSuperseded decision".
func TestPeerConnectionFallbackRetirementSuperseded(t *testing.T) {
	t.Parallel()
	fallback := addressOf(udpAddr(addr9))
	cases := []struct {
		name       string
		fallback   *discovery.Address
		discovered []string
		want       bool
	}{
		{"is superseded with a discovered alternative that is not the fallback", &fallback, []string{"fd00::5"}, true},
		{"is not superseded once promoted (fallback flag cleared)", nil, []string{"fd00::5"}, false},
		{"is not superseded when no discovered address exists", &fallback, nil, false},
		{"is not superseded when the fallback is itself re-advertised (matched ignoring transport)", &fallback, []string{"fd00::9"}, false},
		{"is not superseded when the fallback is re-advertised alongside other addresses", &fallback, []string{"fd00::9", "fd00::5"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, p := fallbackPeer(t, tc.discovered...)
			c := &connection{p: p, fallback: tc.fallback}
			if got := c.fallbackSuperseded(); got != tc.want {
				t.Fatalf("superseded = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestPeerConnectionFallbackRetirementKeepAlive is the describe
// "deleteAddress keep-alive guard".
func TestPeerConnectionFallbackRetirementKeepAlive(t *testing.T) {
	t.Parallel()
	op := addressOf(udpAddr(addr9))
	other := addressOf(udpAddr(addrAlt))
	cases := []struct {
		name    string
		force   bool
		others  int // the non-operational attempts that remain
		deleted bool
	}{
		{"keeps the operational address alive when it is the last attempt (unforced)", false, 0, false},
		{"deletes when a non-operational attempt still remains", false, 1, true},
		{"force bypasses the keep-alive so a superseded fallback is actually dropped", true, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, p := fallbackPeer(t)
			c := &connection{p: p, attempts: map[attemptKey]*attempt{}}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			opAttempt := &attempt{addr: op, ctx: ctx, cancel: cancel}
			c.attempts[attemptKey{ap: op.AddrPort()}] = opAttempt
			if tc.others > 0 {
				c.attempts[attemptKey{ap: other.AddrPort()}] = &attempt{addr: other, ctx: context.Background(), cancel: func() {}}
			}
			c.mu.Lock()
			c.deleteAddressLocked(op, "test", tc.force)
			c.mu.Unlock()
			_, kept := c.attempts[attemptKey{ap: op.AddrPort()}]
			if tc.deleted == kept || tc.deleted != (ctx.Err() != nil) {
				t.Fatalf("operational attempt kept %v, cancelled %v; want deleted %v", kept, ctx.Err() != nil, tc.deleted)
			}
			if !tc.deleted && (c.fallback == nil || c.fallback.AddrPort() != op.AddrPort()) {
				t.Fatal("the kept operational address is not marked as fallback")
			}
		})
	}
}

// TestPeerConnectionFallbackRetirementStagger is the describe "scheduler
// stagger".
func TestPeerConnectionFallbackRetirementStagger(t *testing.T) {
	t.Parallel()
	t.Run("staggers a second real address behind the first", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, nil)
		p, _ := h.set.For(testAddr)
		h.announceAddrs(t, "fd00::1", "fd00::2")
		eventually(t, "the addresses", func() bool { return len(p.Service().Addresses()) == 2 })
		connectAsync(p, ConnectOptions{ConnectionTimeout: -1})
		h.sigma1(t, addr1)
		stagger := DefaultTiming().DelayBeforeNextAddress
		h.clock.waitTimer(t, stagger)
		h.clock.Advance(stagger - time.Millisecond)
		if !h.clock.hasTimer(time.Millisecond) || attemptAt(process(p), addr2) != nil {
			t.Fatal("the second address did not wait the full stagger")
		}
		h.clock.Advance(time.Millisecond)
		h.sigma1(t, addr2)
	})

	t.Run("starts immediately once retirement resets lastAttemptAt", func(t *testing.T) {
		t.Parallel()
		h, p := fallbackPeer(t)
		connectAsync(p, ConnectOptions{ConnectionTimeout: -1})
		h.sigma1(t, addr9)
		start := h.clock.Now()
		h.announceAddrs(t, "fd00::5")
		h.clock.waitTimer(t, 10*time.Second)
		h.clock.Advance(10 * time.Second)
		h.sigma1(t, addrAlt)
		if waited := h.clock.Now().Sub(start); waited >= DefaultTiming().DelayBeforeNextAddress {
			t.Fatalf("the discovered address waited %v, the stagger", waited)
		}
	})
}
