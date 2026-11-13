// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package peer

import (
	"context"
	"net/netip"
	"testing"
	"testing/synctest"
	"time"
)

// TestHintAddressesWithoutProcess: without a running connection process a
// hint queues nothing.
func TestHintAddressesWithoutProcess(t *testing.T) {
	t.Parallel()
	h := newHarness(t, nil)
	p, _ := h.set.For(testAddr)
	if n := p.HintAddresses(netip.MustParseAddrPort(addr1)); n != 0 {
		t.Fatalf("HintAddresses without a process queued %d", n)
	}
}

// TestHintAddressesAttempted: with nothing discovered the process waits
// for discovery; a hinted address is attempted at once, a repeated hint
// and an invalid one queue nothing, and a second hinted address follows
// the stagger.
func TestHintAddressesAttempted(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, nil)
		p, _ := h.set.For(testAddr)
		connectAsync(p, ConnectOptions{ConnectionTimeout: -1})
		eventually(t, "the process", func() bool { return process(p) != nil && p.status.IsResolving() })
		if n := p.HintAddresses(netip.MustParseAddrPort(addr1), netip.AddrPort{}, netip.MustParseAddrPort("[fd00::7]:0")); n != 1 {
			t.Fatalf("queued %d, want 1", n)
		}
		h.sigma1(t, addr1)
		if n := p.HintAddresses(netip.MustParseAddrPort(addr1)); n != 0 {
			t.Fatalf("a repeated hint queued %d", n)
		}
		if n := p.HintAddresses(netip.MustParseAddrPort(addr2)); n != 1 {
			t.Fatalf("queued %d, want 1", n)
		}
		h.clock.waitTimer(t, 45*time.Second)
		h.clock.Advance(45*time.Second - time.Millisecond)
		h.noSigma1(t, addr2)
		h.clock.Advance(time.Millisecond)
		h.sigma1(t, addr2)
		p.Close(context.Background())
	})
}

// TestHintAddressesDiscoveryTakesOver: an address discovery delivers while
// only a hinted attempt is in flight starts without the stagger, the
// hinted attempt going on beside it.
func TestHintAddressesDiscoveryTakesOver(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, nil)
		p, _ := h.set.For(testAddr)
		connectAsync(p, ConnectOptions{ConnectionTimeout: -1})
		eventually(t, "the process", func() bool { return process(p) != nil && p.status.IsResolving() })
		p.HintAddresses(netip.MustParseAddrPort(addr9))
		h.sigma1(t, addr9)
		h.announceAddrs(t, "fd00::1")
		h.sigma1(t, addr1)
		if attemptOf(p, addr9) == nil {
			t.Fatal("the hinted attempt ended when discovery answered")
		}
		c := process(p)
		c.mu.Lock()
		hinted := c.hinted[netip.MustParseAddrPort(addr1)]
		c.mu.Unlock()
		if hinted {
			t.Fatal("the discovered address counts as hinted")
		}
		p.Close(context.Background())
	})
}
