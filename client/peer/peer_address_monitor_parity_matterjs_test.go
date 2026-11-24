// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package peer

// A port of matter.js packages/protocol/test/peer/PeerAddressMonitorTest.ts at
// 862ad75f: verifyReachability's address-change and session-suspect runs,
// their coalescing, close, a joiner's abort, and the probe backoff.
//
// matter.js drives the monitor against a scripted fake peer; Go drives it
// against a live session whose node side the test plays (monitoredWith):
// a probe "answers" when the test sends the report, and "fails" when the
// test lets its retransmissions run out. The stabilization delay is 99 h
// unless a case needs the timer, so the debounce a DNS-SD announcement
// arms never fires into a case.

import (
	"context"
	"net"
	"net/netip"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// monitorFixture is a peer with a live session at addr1, an hour past its
// creation, whose service discovered the given addresses.
func monitorFixture(t *testing.T, stabilization, cooldownMin, cooldownMax time.Duration, discovered ...string) (*harness, *Peer, nodeSide) {
	t.Helper()
	h, p, n := monitoredWith(t, func(c *Context) {
		c.Timing = &Timing{
			AddressChangeStabilizationDelay: stabilization,
			AddressChangeProbeCooldown:      ProbeCooldown{Min: cooldownMin, Max: cooldownMax},
		}
	})
	h.clock.Advance(time.Hour)
	if len(discovered) > 0 {
		h.announceAddrs(t, discovered...)
		eventually(t, "the discovered addresses", func() bool { return len(p.Service().Addresses()) == len(discovered) })
	}
	h.udp.drain()
	return h, p, n
}

// verifyFixture is monitorFixture with matter.js makeFixture's cooldown
// (1 s to 60 s) and a stabilization delay that never fires.
func verifyFixture(t *testing.T, discovered ...string) (*harness, *Peer, nodeSide) {
	t.Helper()
	return monitorFixture(t, 99*time.Hour, time.Second, time.Minute, discovered...)
}

// failRuns advances the clock, leaving every probe unanswered, until each
// verification returns; it returns the results and the probes'
// destinations.
func failRuns(t *testing.T, h *harness, chs ...<-chan verifyResult) (rs []verifyResult, to []string) {
	t.Helper()
	rs = make([]verifyResult, len(chs))
	got := make([]bool, len(chs))
	for range 400 {
		for drained := false; !drained; {
			select {
			case d := <-h.udp.ch:
				to = append(to, d.to.String())
			default:
				drained = true
			}
		}
		left := 0
		for i, ch := range chs {
			if got[i] {
				continue
			}
			select {
			case rs[i] = <-ch:
				got[i] = true
			default:
				left++
			}
		}
		if left == 0 {
			return rs, to
		}
		h.clock.Advance(time.Second)
		h.clock.Yield()
	}
	t.Fatal("the verification never ended")
	return nil, nil
}

// waitVerifyCallers waits until n goroutines are blocked in a monitor's
// verify. The cases that count run as non-parallel top-level tests, so
// no other test's monitor is in flight (Go pauses parallel tests until
// the sequential ones are done).
func waitVerifyCallers(t *testing.T, n int) {
	t.Helper()
	eventually(t, "the joiners", func() bool {
		buf := make([]byte, 1<<20)
		for {
			k := runtime.Stack(buf, true)
			if k < len(buf) {
				return strings.Count(string(buf[:k]), "peer.(*monitor).verify(") >= n
			}
			buf = make([]byte, 2*len(buf))
		}
	})
}

// TestPeerAddressMonitorVerifyReachability is the describe
// "PeerAddressMonitor.verifyReachability" (the other cases are
// TestMonitorSessionSuspect, TestMonitorMigratesToAnAlternate,
// TestMonitorStillDiscoveredAndCooldown and TestMonitorAllProbesFail).
// "session-suspect alone: current fails → no alternate walk" is not
// ported, and neither is the describe "coalescing"'s "session-suspect
// stays current-only even when an address-change joins the run": a
// suspect run walks the alternates here
// (BD-Matter-SuspectProbeWalksAlternates; TestMonitorSessionSuspect and
// TestMonitorSuspectMigratesToAnAlternate hold this module's behaviour).
func TestPeerAddressMonitorVerifyReachability(t *testing.T) {
	t.Parallel()
	t.Run("address-change: current matches a channel-agnostic discovered address → skip, no probe", func(t *testing.T) {
		t.Parallel()
		// Discovered addresses carry no transport; the session's UDP address
		// matches by IP and port.
		h, p, _ := verifyFixture(t, "fd00::1")
		if r := waitVerify(t, verifyAsync(context.Background(), p, ReasonAddressChange)); !r.ok {
			t.Fatal("want true")
		}
		h.udp.none(t)
	})

	t.Run("close() stops the timer and awaits the in-flight run", func(t *testing.T) {
		t.Parallel()
		h, p, n := verifyFixture(t, "fd00::1")
		m := p.addressMonitor()
		h.clock.waitTimer(t, 99*time.Hour) // the debounce the announcement armed
		run := verifyAsync(context.Background(), p, ReasonSessionSuspect)
		probe := h.udp.next(t) // the run hangs on the unanswered probe
		closed := make(chan bool, 1)
		go func() {
			m.close()
			m.mu.Lock()
			defer m.mu.Unlock()
			closed <- m.running == nil
		}()
		// close stops the debounce first, on its own goroutine: wait for
		// that rather than read the timers after one Yield, which may come
		// before close ran (the -race flake of make ci on 2026-11-19).
		eventually(t, "close stopping the debounce", func() bool { return !h.clock.hasTimer(99 * time.Hour) })
		h.clock.Yield()
		select {
		case <-closed:
			t.Fatal("close returned while the run was in flight")
		default:
		}
		n.answer(t, probe)
		select {
		case finished := <-closed:
			if !finished {
				t.Fatal("close returned before the run finished")
			}
		case <-time.After(10 * time.Second):
			t.Fatal("close did not return")
		}
		if r := waitVerify(t, run); !r.ok {
			t.Fatal("the run: want true")
		}
	})

	t.Run("a caller's own aborted signal yields false without preventing the peer-owned run", func(t *testing.T) {
		t.Parallel()
		h, p, n := verifyFixture(t, "fd00::1")
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if r := waitVerify(t, verifyAsync(ctx, p, ReasonSessionSuspect)); r.ok || r.err == nil {
			t.Fatalf("an aborted caller = %+v, want false", r)
		}
		// The run is the peer's: it still probes.
		probe := h.udp.next(t)
		if probe.to.String() != addr1 {
			t.Fatalf("probe to %v", probe.to)
		}
		n.answer(t, probe)
		m := p.addressMonitor()
		eventually(t, "the run to end", func() bool {
			m.mu.Lock()
			defer m.mu.Unlock()
			return m.running == nil
		})
	})
}

// TestPeerAddressMonitorCoalescing holds the cases that count the
// goroutines in verify; it is not parallel (waitVerifyCallers).
func TestPeerAddressMonitorCoalescing(t *testing.T) {
	t.Run("coalesces concurrent triggers into one run", func(t *testing.T) {
		h, p, n := verifyFixture(t, "fd00::1")
		a := verifyAsync(context.Background(), p, ReasonSessionSuspect)
		probe := h.udp.next(t)
		b := verifyAsync(context.Background(), p, ReasonSessionSuspect)
		waitVerifyCallers(t, 2)
		n.answer(t, probe)
		if ra, rb := waitVerify(t, a), waitVerify(t, b); !ra.ok || !rb.ok {
			t.Fatalf("results %v %v, want true true", ra.ok, rb.ok)
		}
		h.udp.none(t) // a single shared run, a single probe
	})
}

// TestPeerAddressMonitorBackoff is the describe "PeerAddressMonitor
// backoff": the discovered set never holds the current address.
func TestPeerAddressMonitorBackoff(t *testing.T) {
	t.Parallel()
	backoff := func(t *testing.T) (*harness, *Peer, nodeSide) {
		t.Helper()
		return monitorFixture(t, 99*time.Hour, 2*time.Second, 24*time.Hour, "fd00::9") // :308-312
	}
	// answered runs a verification whose probe the node answers.
	answered := func(t *testing.T, h *harness, p *Peer, n nodeSide, reason ReachabilityReason) {
		t.Helper()
		res := verifyAsync(context.Background(), p, reason)
		n.answer(t, h.udp.next(t))
		if r := waitVerify(t, res); !r.ok {
			t.Fatal("an answered probe: want true")
		}
	}
	// skipped runs a verification that must return true without a probe
	// (a probe would leave it waiting for the answer).
	skipped := func(t *testing.T, h *harness, p *Peer) {
		t.Helper()
		if r := waitVerify(t, verifyAsync(context.Background(), p, ReasonAddressChange)); !r.ok {
			t.Fatal("a skipped check: want true")
		}
		h.udp.none(t)
	}

	t.Run("an address change does not reset the growing cooldown", func(t *testing.T) {
		t.Parallel()
		h, p, n := backoff(t)
		answered(t, h, p, n, ReasonAddressChange) // cooldown 2 s
		h.clock.Advance(2001 * time.Millisecond)
		answered(t, h, p, n, ReasonAddressChange) // cooldown 4 s
		n.s.SetAddr(udpAddr(addr2))               // the current address churns
		h.clock.Advance(3 * time.Second)          // past the old 2 s floor, under 4 s
		skipped(t, h, p)
	})

	t.Run("session-suspect resets the backoff so the next address-change probes immediately", func(t *testing.T) {
		t.Parallel()
		h, p, n := backoff(t)
		answered(t, h, p, n, ReasonAddressChange)
		h.clock.Advance(2001 * time.Millisecond)
		answered(t, h, p, n, ReasonAddressChange) // cooldown 4 s
		skipped(t, h, p)                          // within the cooldown
		answered(t, h, p, n, ReasonSessionSuspect)
		// The suspect probe's answer refreshes the session's activeTimestamp,
		// which matter.js's fixture pins at 0, so "immediately" is 2.5 s
		// here: past the reset 2 s floor, under the 4 s the reset dropped.
		h.clock.Advance(2500 * time.Millisecond)
		answered(t, h, p, n, ReasonAddressChange)
	})

	t.Run("a session-suspect run does not arm the address-change debounce", func(t *testing.T) {
		t.Parallel()
		// No announcement, so nothing else arms the 1 s debounce.
		h, p, n := monitorFixture(t, time.Second, 2*time.Second, 24*time.Hour)
		m := p.addressMonitor()
		armed := func() bool {
			m.mu.Lock()
			defer m.mu.Unlock()
			return m.timer != nil
		}
		answered(t, h, p, n, ReasonSessionSuspect)
		if armed() {
			t.Fatal("the session-suspect run armed the debounce")
		}
		h.clock.Advance(2 * time.Second)
		h.udp.none(t)
	})
}

// TestPeerAddressMonitorSleepingLITICD ports the four sleeping-LIT-ICD
// cases of PeerAddressMonitorTest.ts (:280-344 at b25ed960): the peer
// awaits an ICD check-in (SetICDAwait), no probe answers, and the monitor
// neither probes nor closes the session; it adopts a discovered address
// on trust, in the session's IP family first, and keeps one still
// discovered (review II, R2-C2-doc-2156). The family case of the source
// puts an IPv4 candidate first for an IPv6 session; the Go service lists
// the IPv6 address first (asserted below), so the port inverts the case —
// an IPv4 session, the IPv6 candidate first — to keep the first candidate
// in the other family.
func TestPeerAddressMonitorSleepingLITICD(t *testing.T) {
	t.Parallel()
	lit := func() bool { return true }
	sleeping := func(t *testing.T, at string, rrs ...string) (*harness, *Peer, nodeSide) {
		t.Helper()
		h := newHarness(t, func(c *Context) {
			c.Timing = &Timing{AddressChangeStabilizationDelay: 99 * time.Hour}
		})
		s, ch := h.securePair(t, 0x50, udpAddr(at))
		p := h.set.Adopt(s)
		n := nodeSide{h: h, s: s, ch: ch}
		h.clock.Advance(time.Hour)
		p.SetICDAwait(lit)
		records := []dns.RR{srvRR(h.qname, h.hostQ, 5540, 120), txtRR(h.qname, 120, "SII=800", "SAI=300")}
		for _, ip := range rrs {
			if netip.MustParseAddr(ip).Is4() {
				records = append(records, &dns.A{Hdr: rrHdr(h.hostQ, dns.TypeA, 120), A: net.ParseIP(ip).To4()})
			} else {
				records = append(records, aaaaRR(h.hostQ, ip, 120))
			}
		}
		h.mdns.announce(t, records...)
		eventually(t, "the discovered addresses", func() bool { return len(p.Service().Addresses()) == len(rrs) })
		h.udp.drain()
		return h, p, n
	}
	check := func(t *testing.T, h *harness, p *Peer, n nodeSide, reason ReachabilityReason, want string) {
		t.Helper()
		if r := waitVerify(t, verifyAsync(context.Background(), p, reason)); !r.ok {
			t.Fatal("verification failed")
		}
		h.udp.none(t) // never probe a sleeping ICD
		if n.s.IsPeerLost() || !p.HasSession() {
			t.Fatal("the healthy session was given up")
		}
		if got := n.s.Addr().String(); got != want {
			t.Fatalf("session at %v, want %v", got, want)
		}
	}
	t.Run("sleeping LIT ICD whose address left mDNS: adopts a discovered address on trust, no probe", func(t *testing.T) {
		t.Parallel()
		h, p, n := sleeping(t, addr1, "fd00::2")
		check(t, h, p, n, ReasonAddressChange, addr2)
	})
	t.Run("sleeping LIT ICD prefers a discovered address in the session's IP family", func(t *testing.T) {
		t.Parallel()
		h, p, n := sleeping(t, "10.0.0.1:5540", "fd00::2", "10.0.0.5")
		if first := p.Service().Addresses()[0]; !first.IP.Is6() {
			t.Fatalf("first candidate %v, want the IPv6 one", first)
		}
		check(t, h, p, n, ReasonAddressChange, "10.0.0.5:5540")
	})
	t.Run("sleeping LIT ICD still in mDNS: keeps its address untouched, no probe", func(t *testing.T) {
		t.Parallel()
		h, p, n := sleeping(t, addr1, "fd00::1")
		check(t, h, p, n, ReasonAddressChange, addr1)
	})
	t.Run("session-suspect on a sleeping LIT ICD never probes or closes the session", func(t *testing.T) {
		t.Parallel()
		h, p, n := sleeping(t, addr1, "fd00::1")
		check(t, h, p, n, ReasonSessionSuspect, addr1)
	})
}
