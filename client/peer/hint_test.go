// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package peer

import (
	"context"
	"log/slog"
	"net/netip"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

// hintLog counts the address_hinted and address_hint_limit lines a logger
// handled.
type hintLog struct {
	mu     sync.Mutex
	n      int
	limits int
}

func (l *hintLog) Enabled(context.Context, slog.Level) bool { return true }

func (l *hintLog) Handle(_ context.Context, r slog.Record) error {
	if r.Message == "matter.controller.peer.address_hinted" {
		l.mu.Lock()
		l.n++
		l.mu.Unlock()
	}
	if r.Message == "matter.controller.peer.address_hint_limit" {
		l.mu.Lock()
		l.limits++
		l.mu.Unlock()
	}
	return nil
}

func (l *hintLog) WithAttrs([]slog.Attr) slog.Handler { return l }
func (l *hintLog) WithGroup(string) slog.Handler      { return l }

func (l *hintLog) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.n
}

// attemptsAt counts the process's attempts at addr.
func attemptsAt(c *connection, addr string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	ap := addressOf(udpAddr(addr)).AddrPort()
	n := 0
	for k := range c.attempts {
		if k.ap == ap {
			n++
		}
	}
	for _, x := range c.pending {
		if x.addr.AddrPort() == ap {
			n++
		}
	}
	return n
}

// hintedCount is how many addresses the process holds as hinted.
func hintedCount(c *connection) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.hinted)
}

// TestHintAddressesBounded (review II, R2-P0-bound-E01): one connection
// process takes at most MaxHintedAddresses hinted addresses — in one call
// or over many, as a commissionee that reports other addresses on every
// probe would have it — and says once that it passed some over. The
// addresses it took stay queued; a known one is still a repeat, not an
// overflow.
func TestHintAddressesBounded(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		log := &hintLog{}
		h := newHarness(t, func(c *Context) { c.Logger = slog.New(log) })
		p, _ := h.set.For(testAddr)
		connectAsync(p, ConnectOptions{ConnectionTimeout: -1})
		eventually(t, "the process", func() bool { return process(p) != nil && p.status.IsResolving() })
		hint := func(i int) netip.AddrPort {
			return netip.AddrPortFrom(netip.AddrFrom16([16]byte{0: 0xfd, 1: 0x77, 14: byte(i >> 8), 15: byte(i)}), 5540)
		}
		first := make([]netip.AddrPort, 0, MaxHintedAddresses+10)
		for i := range MaxHintedAddresses + 10 {
			first = append(first, hint(i+1))
		}
		if n := p.HintAddresses(first...); n != MaxHintedAddresses {
			t.Fatalf("one call with %d addresses queued %d, want MaxHintedAddresses (%d)", len(first), n, MaxHintedAddresses)
		}
		c := process(p)
		for round := range 3 {
			later := make([]netip.AddrPort, 0, 50)
			for i := range 50 {
				later = append(later, hint(1000+round*50+i))
			}
			if n := p.HintAddresses(later...); n != 0 {
				t.Fatalf("a later probe's %d new addresses queued %d, want 0 past the bound", len(later), n)
			}
		}
		if n := p.HintAddresses(first[0]); n != 0 {
			t.Fatalf("a repeated hint queued %d", n)
		}
		if got := hintedCount(c); got != MaxHintedAddresses {
			t.Fatalf("the process holds %d hinted addresses, want %d", got, MaxHintedAddresses)
		}
		c.mu.Lock()
		queued := len(c.pending) + len(c.attempts)
		c.mu.Unlock()
		if queued > MaxHintedAddresses {
			t.Fatalf("%d targets pending or attempted, want at most %d", queued, MaxHintedAddresses)
		}
		log.mu.Lock()
		lines, limits := log.n, log.limits
		log.mu.Unlock()
		if lines != MaxHintedAddresses || limits != 1 {
			t.Fatalf("%d address_hinted lines and %d address_hint_limit lines, want %d and 1", lines, limits, MaxHintedAddresses)
		}
		p.Close(context.Background())
	})
}

// TestHintAddressesUnicastOnly (review II, R2-P0-bound-E01): an address
// that names no single host — unspecified, multicast, the IPv4 limited
// broadcast and the reserved range it lies in, IPv4 "this network" — is
// not queued, in either family and IPv4-mapped; a unicast address beside
// them is.
func TestHintAddressesUnicastOnly(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, nil)
		p, _ := h.set.For(testAddr)
		connectAsync(p, ConnectOptions{ConnectionTimeout: -1})
		eventually(t, "the process", func() bool { return process(p) != nil && p.status.IsResolving() })
		for _, a := range []string{
			"[::]:5540", "[::%eth0]:5540", "[ff02::1]:5540", "[ff05::fb]:5540",
			"0.0.0.0:5540", "0.1.2.3:5540", "224.0.0.251:5540", "239.1.1.1:5540", "240.0.0.1:5540", "255.255.255.255:5540",
			"[::ffff:255.255.255.255]:5540", "[::ffff:224.0.0.251]:5540",
		} {
			if n := p.HintAddresses(netip.MustParseAddrPort(a)); n != 0 {
				t.Errorf("%s was queued", a)
			}
		}
		if got := hintedCount(process(p)); got != 0 {
			t.Fatalf("%d addresses hinted, want none", got)
		}
		if n := p.HintAddresses(netip.MustParseAddrPort("192.0.2.7:5540"), netip.MustParseAddrPort(addr1)); n != 2 {
			t.Fatalf("two unicast addresses queued %d", n)
		}
		p.Close(context.Background())
	})
}

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
		hinted := c.hintedLocked(addressOf(udpAddr(addr1)))
		c.mu.Unlock()
		if hinted {
			t.Fatal("the discovered address counts as hinted")
		}
		p.Close(context.Background())
	})
}

// TestHintAddressesOncePerProcess: the same hint on three probes in a row
// is queued once — one attempt, one log line — also once its address was
// deleted from the process; a link-local address without a zone is not
// queued.
func TestHintAddressesOncePerProcess(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		log := &hintLog{}
		h := newHarness(t, func(c *Context) { c.Logger = slog.New(log) })
		p, _ := h.set.For(testAddr)
		connectAsync(p, ConnectOptions{ConnectionTimeout: -1})
		eventually(t, "the process", func() bool { return process(p) != nil && p.status.IsResolving() })
		hint := netip.MustParseAddrPort(addr1)
		got := []int{p.HintAddresses(hint), p.HintAddresses(hint), p.HintAddresses(hint)}
		h.sigma1(t, addr1)
		if got[0] != 1 || got[1] != 0 || got[2] != 0 || attemptsAt(process(p), addr1) != 1 || log.count() != 1 {
			t.Fatalf("queued %v, %d attempts, %d log lines; want one each", got, attemptsAt(process(p), addr1), log.count())
		}
		c := process(p)
		c.mu.Lock()
		c.deleteAddressLocked(addressOf(udpAddr(addr1)), "test", true)
		c.mu.Unlock()
		if n := p.HintAddresses(hint); n != 0 || attemptsAt(c, addr1) != 0 {
			t.Fatalf("a deleted hint was queued again (%d)", n)
		}
		if n := p.HintAddresses(netip.MustParseAddrPort("[fe80::1]:5540")); n != 0 {
			t.Fatal("a link-local address without a zone was queued")
		}
		if n := p.HintAddresses(netip.MustParseAddrPort("[fe80::1%eth0]:5540")); n != 1 {
			t.Fatal("a link-local address with a zone was not queued")
		}
		p.Close(context.Background())
	})
}

// TestHintAddressesDiscoveredFirst: with a hinted and a discovered address
// both pending behind an attempt in flight, the discovered one is
// attempted first after the stagger, the hinted one a stagger later.
func TestHintAddressesDiscoveredFirst(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, nil)
		p, _ := h.set.For(testAddr)
		h.announceAddrs(t, "fd00::9")
		eventually(t, "the address", func() bool { return len(p.Service().Addresses()) == 1 })
		connectAsync(p, ConnectOptions{ConnectionTimeout: -1})
		h.sigma1(t, addr9)
		p.HintAddresses(netip.MustParseAddrPort(addr1))
		h.announceAddrs(t, "fd00::9", "fd00::2")
		eventually(t, "both pending", func() bool { return attemptsAt(process(p), addr1) == 1 && attemptsAt(process(p), addr2) == 1 })
		h.clock.waitTimer(t, 45*time.Second)
		h.clock.Advance(45 * time.Second)
		h.sigma1(t, addr2)
		h.noSigma1(t, addr1)
		h.clock.waitTimer(t, 45*time.Second)
		h.clock.Advance(45 * time.Second)
		h.sigma1(t, addr1)
		p.Close(context.Background())
	})
}

// TestHintAddressesBesideOnce: the stagger exemption behind a hinted
// attempt is one-shot. A hinted attempt H is in flight; the first
// discovered address D1 starts beside it at once; D1 is withdrawn (its
// attempt deleted, H alone in flight again); the next discovered address
// D2 waits out DelayBeforeNextAddress from D1's start.
func TestHintAddressesBesideOnce(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, withoutRetention)
		p, _ := h.set.For(testAddr)
		connectAsync(p, ConnectOptions{ConnectionTimeout: -1})
		eventually(t, "the process", func() bool { return process(p) != nil && p.status.IsResolving() })
		p.HintAddresses(netip.MustParseAddrPort(addr9))
		h.sigma1(t, addr9)
		h.announceAddrs(t, "fd00::1")
		h.sigma1(t, addr1)
		h.expire(t, "fd00::1")
		eventually(t, "D1 deleted", func() bool { return attemptOf(p, addr1) == nil })
		h.clock.Advance(10 * time.Second)
		h.announceAddrs(t, "fd00::2")
		eventually(t, "D2 pending", func() bool { return attemptsAt(process(p), addr2) == 1 })
		h.noSigma1(t, addr2)
		h.clock.waitTimer(t, 35*time.Second)
		h.clock.Advance(35*time.Second - time.Millisecond)
		h.noSigma1(t, addr2)
		h.clock.Advance(time.Millisecond)
		h.sigma1(t, addr2)
		if attemptOf(p, addr9) == nil {
			t.Fatal("the hinted attempt ended")
		}
		p.Close(context.Background())
	})
}
