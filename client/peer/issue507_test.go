// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package peer

import (
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/miekg/dns"

	"github.com/SukramJ/go-fabric/transport/mrp"
)

// Issue 507: a Thread node's operational instance is answered, for a
// while, only with a border router's SRV target (router.local), whose one
// address is IPv6 link-local (routerLL) and never answers; the node itself
// answers at its ULA, the operational address addr1, under its own host
// (node1234.local). The cases measure on the fake clock when Sigma1 goes
// to the ULA and in which order the attempts start. The cases that assert
// the behaviour the issue asks for are skipped until it is fixed; each was
// run without its skip and failed as its skip message says.

// The TXT records of issue 507: the router's answer and the node's own.
var (
	i507RouterTXT = []string{"SII=9500", "SAI=2100", "SAT=5000"}
	i507NodeTXT   = []string{"SII=17000", "SAI=2500", "SAT=1000"}
)

// i507Host is the harness's node host (harness.hostQ).
const i507Host = "node1234.local"

// sigma1Sent is one Sigma1 transmission at its fake-clock offset from the
// rig's start.
type sigma1Sent struct {
	at      time.Duration
	to      string
	counter uint32
	first   bool // the first transmission of its message counter
}

// rig507 drives a harness on the fake clock and records every Sigma1.
type rig507 struct {
	*harness
	t     *testing.T
	start time.Time
	sent  []sigma1Sent
	seen  map[uint32]bool
	// respond, when set, is called for every fresh Sigma1.
	respond func(u unsecured, to string)
	// nodeIPs are the node host's addresses nodeOnly announces.
	nodeIPs []string
}

// newRig507 is a harness with the default network profiles: a peer
// without a medium is "unknown", the conservative profile with its 1.5 s
// MRP margin — the profile a Thread peer gets as well. edits change the
// set's context after that.
func newRig507(t *testing.T, edits ...func(*Context)) *rig507 {
	t.Helper()
	h := newHarness(t, func(c *Context) {
		c.Networks = NewNetworkProfiles(c.Clock, nil, slog.New(slog.DiscardHandler))
		for _, e := range edits {
			e(c)
		}
	})
	return &rig507{harness: h, t: t, start: h.clock.Now(), seen: map[uint32]bool{}, nodeIPs: []string{"fd00::1"}}
}

// peer is the known peer with the ULA as operational address and host as
// its proven host ("" never learned).
func (r *rig507) peer(host string) *Peer {
	r.t.Helper()
	p, err := r.set.AddKnownPeer(Descriptor{Address: testAddr, OperationalAddress: udpAddr(addr1), OperationalHost: host})
	if err != nil {
		r.t.Fatal(err)
	}
	return p
}

// connect starts the connection process and the rig's clock.
func (r *rig507) connect(p *Peer) {
	r.t.Helper()
	r.start = r.clock.Now()
	connectAsync(p, ConnectOptions{ConnectionTimeout: -1})
	eventually(r.t, "the process", func() bool { return process(p) != nil })
	r.collect()
}

// now is the fake-clock offset from the rig's start.
func (r *rig507) now() time.Duration { return r.clock.Now().Sub(r.start) }

// collect records the Sigma1 datagrams sent once the bubble settled.
func (r *rig507) collect() {
	r.t.Helper()
	synctest.Wait()
	for {
		select {
		case d := <-r.udp.ch:
			u := decodeUnsecured(r.t, d.bytes)
			if u.proto.Opcode != mrp.SCOpcodeSigma1 {
				continue
			}
			c := u.hdr.MessageCounter
			first := !r.seen[c]
			r.sent = append(r.sent, sigma1Sent{at: r.now(), to: d.to.String(), counter: c, first: first})
			r.seen[c] = true
			if first && r.respond != nil {
				r.respond(u, d.to.String())
				synctest.Wait()
			}
		default:
			return
		}
	}
}

// i507Step is the resolution of the rig's clock.
const i507Step = 250 * time.Millisecond

// advance moves the fake clock by d in steps of i507Step, collecting
// after each.
func (r *rig507) advance(d time.Duration) {
	r.t.Helper()
	for d > 0 {
		s := min(i507Step, d)
		r.clock.Advance(s)
		d -= s
		r.collect()
	}
}

// advanceUntil advances until cond holds, at most limit; it reports the
// time it took and whether cond held.
func (r *rig507) advanceUntil(limit time.Duration, cond func() bool) (time.Duration, bool) {
	r.t.Helper()
	began := r.now()
	for {
		if cond() {
			return r.now() - began, true
		}
		if r.now()-began >= limit {
			return r.now() - began, false
		}
		r.advance(i507Step)
	}
}

// fresh is the offsets of the fresh Sigma1 transmissions to addr in
// [from, to).
func (r *rig507) fresh(addr string, from, to time.Duration) []time.Duration {
	var out []time.Duration
	for _, s := range r.sent {
		if s.first && s.to == addr && s.at >= from && s.at < to {
			out = append(out, s.at)
		}
	}
	return out
}

// order is the fresh Sigma1 transmissions, "offset addr" each.
func (r *rig507) order() string {
	var b strings.Builder
	for _, s := range r.sent {
		if s.first {
			fmt.Fprintf(&b, "%v %s; ", s.at, s.to)
		}
	}
	return b.String()
}

// attempting reports whether the process has an attempt at addr in
// flight.
func (r *rig507) attempting(p *Peer, addr string) bool {
	c := process(p)
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.attempts[attemptKey{ap: addressOf(udpAddr(addr)).AddrPort()}] != nil
}

// state is the process's attempts, pending addresses and fallback, for
// failure messages.
func (r *rig507) state(p *Peer) string {
	c := process(p)
	if c == nil {
		return "no process"
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	var ks, ps []string
	for k := range c.attempts {
		ks = append(ks, k.ap.String())
	}
	for _, x := range c.pending {
		ps = append(ps, x.addr.String())
	}
	fb := ""
	if c.fallback != nil {
		fb = c.fallback.String()
	}
	return fmt.Sprintf("attempts=%v pending=%v fallback=%q", ks, ps, fb)
}

// settle lets the cache's eviction delay pass on the bubble's clock, so a
// cache-flush record supersedes the records received before it.
func settle() { time.Sleep(2 * time.Second) }

// routerOnly makes the router's SRV target the instance's only one: its
// cache-flush SRV, TXT and link-local address flush the node's SRV
// (RFC 6762 §10.2) — matter.discovery.srv_target_changed to=router.local.
func (r *rig507) routerOnly() {
	r.t.Helper()
	settle()
	r.mdns.announce(r.t, srvRR(r.qname, routerHostQ, 5540, 120), txtRR(r.qname, 120, i507RouterTXT...), aaaaRR(routerHostQ, "fe80::1", 120))
	r.waitAddrs(1, routerHostQ)
}

// nodeOnly is routerOnly the other way round: the node's own SRV target
// with its addresses flushes the router's.
func (r *rig507) nodeOnly() {
	r.t.Helper()
	settle()
	rrs := make([]dns.RR, 0, 2+len(r.nodeIPs))
	rrs = append(rrs, srvRR(r.qname, r.hostQ, 5540, 120), txtRR(r.qname, 120, i507NodeTXT...))
	for _, ip := range r.nodeIPs {
		rrs = append(rrs, aaaaRR(r.hostQ, ip, 120))
	}
	r.mdns.announce(r.t, rrs...)
	r.waitAddrs(len(r.nodeIPs), r.hostQ)
}

// routerAlso adds the router's SRV target beside the node's: an SRV
// without the cache-flush bit, so both targets are discovered.
func (r *rig507) routerAlso() {
	r.t.Helper()
	srv := srvRR(r.qname, routerHostQ, 5540, 120)
	srv.Header().Class = dns.ClassINET
	r.mdns.announce(r.t, srv, aaaaRR(routerHostQ, "fe80::1", 120))
	r.waitAddrs(len(r.nodeIPs)+1, "")
}

// nodeGone sends the goodbye of the node's SRV target; the router's stays.
func (r *rig507) nodeGone() {
	r.t.Helper()
	r.mdns.announce(r.t, srvRR(r.qname, r.hostQ, 5540, 0))
	r.waitAddrs(1, routerHostQ)
}

// waitAddrs waits until n addresses are discovered, all of host unless
// host is "".
func (r *rig507) waitAddrs(n int, host string) {
	r.t.Helper()
	p, _ := r.set.For(testAddr)
	eventually(r.t, fmt.Sprintf("%d addresses of %q", n, host), func() bool {
		as := p.Service().Addresses()
		ok := len(as) == n
		for _, a := range as {
			ok = ok && (host == "" || a.Host == host)
		}
		if !ok {
			time.Sleep(100 * time.Millisecond)
		}
		return ok
	})
	r.collect()
}

// TestIssue507_RouterOnlyAtStart: the connection starts while discovery
// carries only the router's target. With the proven host known, the
// operational address is queued as fallback and ranked first; the
// router's link-local address follows DelayBeforeNextAddress later
// (BD-Matter-ProvenOperationalHost). Without a proven host the fallback is
// not queued — discovery has an address and no proven host is absent —
// and the ULA is never attempted: the attempt at the link-local address
// never ends on its own. The case asserts what the issue asks for: the
// ULA's first Sigma1 within DelayBeforeNextAddress of the start.
func TestIssue507_RouterOnlyAtStart(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ name, host string }{{"proven host", i507Host}, {"host never learned", ""}} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				r := newRig507(t)
				p := r.peer(c.host)
				r.routerOnly()
				r.connect(p)
				took, ok := r.advanceUntil(5*time.Minute, func() bool { return len(r.fresh(addr1, 0, time.Hour)) > 0 })
				if !ok || took > 45*time.Second {
					t.Fatalf("the ULA's first Sigma1 after %v (sent %v), want within DelayBeforeNextAddress; order: %s; %s", took, ok, r.order(), r.state(p))
				}
				if c.host != "" {
					r.advance(time.Minute)
					if got := r.fresh(routerLL, 0, time.Hour); len(got) == 0 || got[0] != 45*time.Second {
						t.Fatalf("the router's first Sigma1 at %v, want 45s; order: %s", got, r.order())
					}
				}
			})
		})
	}
}

// TestIssue507_ProvenHostWithdrawnWhileAttempting: the ULA's attempt is in
// flight when discovery stops carrying the node's host and carries the
// router's. deleteAddressLocked keeps the operational address's attempt as
// fallback only while it is the only attempt; with another attempt in
// flight — the node's second address, or the router's link-local address
// started before the node's host went — the ULA's attempt is aborted
// ("aborting attempt because address is expired"), and
// maybeAttemptFallbackLocked does not queue it again while the router's
// address is pending or attempted. The ULA is next tried when the node's
// host is advertised again. The case asserts what the issue asks for: an
// attempt at the ULA in flight within DelayBeforeNextAddress of the flip,
// with the proven host known or not.
func TestIssue507_ProvenHostWithdrawnWhileAttempting(t *testing.T) {
	t.Parallel()
	type variant struct {
		name, host string
		second     bool // the node's host carries a second address
		flip       func(r *rig507)
	}
	// The node's host carries the ULA and a second address; both are
	// attempted (0 s, 45 s) when discovery flips to the router at 50 s.
	secondAddress := func(r *rig507) {
		r.advance(50 * time.Second)
		r.routerOnly()
	}
	// The router's target joins the node's at 50 s and its link-local
	// attempt starts at once (the stagger has passed); then the node's
	// SRV says goodbye.
	routerFirst := func(r *rig507) {
		r.advance(50 * time.Second)
		r.routerAlso()
		r.nodeGone()
	}
	variants := make([]variant, 0, 4)
	for _, h := range []struct{ name, host string }{{"proven host", i507Host}, {"host never learned", ""}} {
		variants = append(variants,
			variant{"second node address in flight, " + h.name, h.host, true, secondAddress},
			variant{"router attempt in flight, " + h.name, h.host, false, routerFirst})
	}
	for _, v := range variants {
		t.Run(v.name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				r := newRig507(t)
				if v.second {
					r.nodeIPs = []string{"fd00::1", "fd00::9"}
				}
				p := r.peer(v.host)
				r.nodeOnly()
				r.connect(p)
				v.flip(r)
				flip := r.now()
				took, ok := r.advanceUntil(5*time.Minute, func() bool { return r.attempting(p, addr1) })
				if !ok || took > 45*time.Second {
					t.Fatalf("an attempt at the ULA %v after the flip at %v (in flight %v), want within DelayBeforeNextAddress; order: %s; %s", took, flip, ok, r.order(), r.state(p))
				}
			})
		})
	}
}

// TestIssue507_OnlyAttemptSurvivesTheFlip: with the proven host known,
// the ULA's attempt is the only one when discovery flips to the router's
// target before DelayBeforeNextAddress passed: it is kept as fallback and
// goes on (fallbackSuperseded is false while the proven host is absent);
// the router's link-local address starts beside it when the stagger has
// passed. Without a proven host the operational address is retained the
// same way (retainLocked), so the fallback retirement passes it by, and the
// router's address — the first discovered beside the anchor — starts at
// the flip, without the stagger (anchorBesideLocked). Before
// the fix whether retireStaleFallback aborted it there depended on the
// order its goroutine and the process's loop saw the change in;
// TestIssue507_FallbackRetiredWithoutRetention pins matter.js's retirement.
func TestIssue507_OnlyAttemptSurvivesTheFlip(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ name, host string }{{"proven host", i507Host}, {"host never learned", ""}} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				r := newRig507(t)
				p := r.peer(c.host)
				r.nodeOnly()
				r.connect(p)
				pc := process(p)
				key := attemptKey{ap: addressOf(udpAddr(addr1)).AddrPort()}
				pc.mu.Lock()
				first := pc.attempts[key]
				pc.mu.Unlock()
				if first == nil {
					t.Fatalf("no attempt at the ULA; %s", r.state(p))
				}
				r.advance(20 * time.Second)
				r.routerOnly()
				flip := r.now()
				r.advance(30 * time.Second)
				pc.mu.Lock()
				same := pc.attempts[key] == first
				pc.mu.Unlock()
				if !same {
					t.Fatalf("the ULA's attempt did not go on; %s", r.state(p))
				}
				want := 45 * time.Second
				if c.host == "" {
					want = flip
				}
				if got := r.fresh(routerLL, 0, time.Hour); len(got) != 1 || got[0] != want {
					t.Fatalf("the router's Sigma1 at %v, want %v; order: %s", got, want, r.order())
				}
			})
		})
	}
}

// TestIssue507_BusyNodeAcrossFlips: the node answers the ULA's Sigma1
// with Busy and the largest delay (65535 ms); the attempt waits it plus
// the jitter of DelayAfterNetworkError (the harness's rand is 0.5:
// 65.535 s + 7.5 s). A flip to the router's target, its link-local attempt
// already started, aborts the attempt and cancels its wait; when the
// node's host is advertised again the ULA is attempted at once — a fresh
// Sigma1 before the Busy delay passed. The case asserts what the issue
// asks for: no fresh Sigma1 to the node before its Busy delay passed.
func TestIssue507_BusyNodeAcrossFlips(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		r := newRig507(t)
		r.respond = func(u unsecured, to string) {
			if to == addr1 {
				r.status(u, addr1, generalStatusBusy, secureChannelStatusBusy, busyData(65535))
			}
		}
		p := r.peer(i507Host)
		r.nodeOnly()
		r.connect(p)
		r.advance(50 * time.Second)
		r.routerAlso()
		r.nodeGone()
		r.advance(10 * time.Second)
		r.nodeOnly()
		r.advance(20 * time.Second)
		const busyWait = 65535*time.Millisecond + 7500*time.Millisecond
		if got := r.fresh(addr1, i507Step, busyWait); len(got) > 0 {
			t.Fatalf("fresh Sigma1 to the ULA at %v, inside the Busy wait of %v; order: %s; %s", got, busyWait, r.order(), r.state(p))
		}
	})
}

// TestIssue507_LinkLocalAttemptOutlivesTwoMinutes: the attempt at the
// router's link-local address — the router's SII 9500 ms, the
// conservative profile's 1.5 s margin, the harness's rand 0.5 — sends its
// first Sigma1 five times (0, 13.6, 27.2, 49.0, 83.9 s) and moves to a
// fresh one only after the budget of the identical phase, at least
// MaxDelayBetweenInitialContactRetries (2 min) after the first, plus the
// final wait: 151.5 s. Then the attempt goes on at the same address — it
// never ends by itself, so its end never queues the fallback. Without a
// proven host the operational address is anchored (anchorLocked): the ULA
// is attempted at once, the link-local address — the first discovered
// beside the anchor — at once too (anchorBesideLocked), and both stay in
// flight.
func TestIssue507_LinkLocalAttemptOutlivesTwoMinutes(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		r := newRig507(t)
		p := r.peer("")
		r.routerOnly()
		r.connect(p)
		r.advance(4 * time.Minute)
		fresh := r.fresh(routerLL, 0, time.Hour)
		if len(fresh) == 0 || fresh[0] != 0 {
			t.Fatalf("the router's first Sigma1 at %v, want 0s; order: %s", fresh, r.order())
		}
		if got := r.fresh(addr1, 0, time.Hour); len(got) == 0 || got[0] != 0 {
			t.Fatalf("the ULA's first Sigma1 at %v, want 0s; order: %s", got, r.order())
		}
		var all []time.Duration
		var first uint32
		for _, s := range r.sent {
			if s.to == routerLL && s.first && first == 0 {
				first = s.counter
			}
			if s.to == routerLL && s.counter == first {
				all = append(all, s.at-fresh[0])
			}
		}
		want := []time.Duration{0, 13750 * time.Millisecond, 27250 * time.Millisecond, 49250 * time.Millisecond, 84 * time.Second}
		if fmt.Sprint(all) != fmt.Sprint(want) {
			t.Fatalf("the first Sigma1's transmissions at %v after its start, want %v (rounded up to %v)", all, want, i507Step)
		}
		if len(fresh) < 2 || fresh[1]-fresh[0] != 151750*time.Millisecond {
			t.Fatalf("fresh Sigma1 at %v, want the second 151.75s after the first", fresh)
		}
		if !r.attempting(p, routerLL) || !r.attempting(p, addr1) {
			t.Fatalf("want the link-local and the ULA's attempts in flight; %s; order: %s", r.state(p), r.order())
		}
	})
}
