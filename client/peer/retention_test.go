// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package peer

import (
	"context"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/miekg/dns"

	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// withoutRetention is the harness edit for matter.js's behaviour: no
// address is retained after discovery stopped advertising it
// (Timing.ProvenAddressRetention negative).
func withoutRetention(c *Context) { c.Timing = &Timing{ProvenAddressRetention: -1} }

// retainFor is the harness edit for a retention bound of d.
func retainFor(d time.Duration) func(*Context) {
	return func(c *Context) { c.Timing = &Timing{ProvenAddressRetention: d} }
}

// retentionLog records the retention lines a logger handled.
type retentionLog struct {
	mu      sync.Mutex
	records []map[string]string
}

func (l *retentionLog) Enabled(context.Context, slog.Level) bool { return true }

func (l *retentionLog) Handle(_ context.Context, r slog.Record) error {
	if !strings.HasPrefix(r.Message, "matter.controller.peer.address_ret") {
		return nil
	}
	m := map[string]string{"msg": r.Message, "level": r.Level.String()}
	r.Attrs(func(a slog.Attr) bool {
		m[a.Key] = a.Value.String()
		return true
	})
	l.mu.Lock()
	defer l.mu.Unlock()
	l.records = append(l.records, m)
	return nil
}

func (l *retentionLog) WithAttrs([]slog.Attr) slog.Handler { return l }
func (l *retentionLog) WithGroup(string) slog.Handler      { return l }

func (l *retentionLog) all() []map[string]string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]map[string]string(nil), l.records...)
}

// attemptOf is the process's attempt at addr, nil when none.
func attemptOf(p *Peer, addr string) *attempt {
	c := process(p)
	if c == nil {
		return nil
	}
	return attemptAt(c, addr)
}

// retainedOf is the addresses the process retains.
func retainedOf(c *connection) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, 0, len(c.retained))
	for ap := range c.retained {
		out = append(out, ap.String())
	}
	return out
}

const addrSecond = "[fd00::9]:5540" // the node host's second address

// TestIssue507_RetentionExpires: the proven host's address is retained when
// discovery flips to the router's target with the router's attempt in
// flight; at the bound it is deleted as discovery's deletion would have
// deleted it — its attempt aborted — and the router's attempt goes on.
// The retention and its end are logged at Info with address, host and
// reason.
func TestIssue507_RetentionExpires(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		log := &retentionLog{}
		const bound = 2 * time.Minute
		r := newRig507(t, retainFor(bound), func(c *Context) { c.Logger = slog.New(log) })
		p := r.peer(i507Host)
		r.nodeOnly()
		r.connect(p)
		r.advance(50 * time.Second)
		r.routerAlso()
		first := attemptOf(p, addr1)
		r.nodeGone()
		if first == nil || attemptOf(p, addr1) != first || !r.attempting(p, routerLL) {
			t.Fatalf("want the ULA's attempt retained beside the router's; %s", r.state(p))
		}
		r.advance(bound - i507Step)
		if attemptOf(p, addr1) != first || first.ctx.Err() != nil {
			t.Fatalf("the retained attempt ended before the bound; %s", r.state(p))
		}
		r.advance(i507Step)
		if attemptOf(p, addr1) != nil || first.ctx.Err() == nil {
			t.Fatalf("the retained attempt outlived the bound; %s", r.state(p))
		}
		if !r.attempting(p, routerLL) || len(retainedOf(process(p))) != 0 {
			t.Fatalf("want the router's attempt alone; %s", r.state(p))
		}
		got := log.all()
		if len(got) != 2 {
			t.Fatalf("retention lines %v, want two", got)
		}
		for i, msg := range []string{"matter.controller.peer.address_retained", "matter.controller.peer.address_retention_expired"} {
			g := got[i]
			if g["msg"] != msg || g["level"] != "INFO" || g["address"] != addr1 || g["host"] != i507Host || g["reason"] != "no longer advertised" {
				t.Fatalf("line %d = %v, want %s at INFO for %s of %s", i, g, msg, addr1, i507Host)
			}
		}
	})
}

// TestIssue507_ProvenHostReturnsWithAnotherSet: both of the node host's
// addresses are retained across the flip to the router; the host comes
// back with one of them only. Its fresh set replaces the retained ones:
// the address it still carries keeps its attempt, the one it no longer
// carries is deleted — its attempt aborted, the other remaining.
func TestIssue507_ProvenHostReturnsWithAnotherSet(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		r := newRig507(t)
		r.nodeIPs = []string{"fd00::1", "fd00::9"}
		p := r.peer(i507Host)
		r.nodeOnly()
		r.connect(p)
		r.advance(50 * time.Second)
		gone, kept := attemptOf(p, addr1), attemptOf(p, addrSecond)
		if gone == nil || kept == nil {
			t.Fatalf("want both node addresses attempted; %s; order: %s", r.state(p), r.order())
		}
		r.routerOnly()
		if n := len(retainedOf(process(p))); n != 2 || attemptOf(p, addr1) != gone || attemptOf(p, addrSecond) != kept {
			t.Fatalf("retained %d, want both attempts kept; %s", n, r.state(p))
		}
		r.nodeIPs = []string{"fd00::9"}
		r.nodeOnly()
		if gone.ctx.Err() == nil || attemptOf(p, addr1) != nil {
			t.Fatalf("the address the host no longer carries is still attempted; %s", r.state(p))
		}
		if kept.ctx.Err() != nil || attemptOf(p, addrSecond) != kept {
			t.Fatalf("the address the host still carries lost its attempt; %s", r.state(p))
		}
		if got := retainedOf(process(p)); len(got) != 0 {
			t.Fatalf("still retained: %v", got)
		}
	})
}

// TestIssue507_SessionEndsRetention: a session ends the process and with
// it every retention: the retained address and the timer of its bound go.
func TestIssue507_SessionEndsRetention(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		const bound = 7 * time.Minute
		r := newRig507(t, retainFor(bound))
		p := r.peer(i507Host)
		r.nodeOnly()
		r.connect(p)
		r.advance(50 * time.Second)
		r.routerAlso()
		r.nodeGone()
		c := process(p)
		if got := retainedOf(c); len(got) != 1 || !r.clock.hasTimer(bound) {
			t.Fatalf("retained %v, the bound's timer %v", got, r.clock.hasTimer(bound))
		}
		r.set.Adopt(r.secureSession(t, 0x51, udpAddr(addr1)))
		eventually(t, "the process to end", func() bool { return process(p) == nil })
		<-c.done
		if got := retainedOf(c); len(got) != 0 || r.clock.hasTimer(bound) {
			t.Fatalf("after the session: retained %v, the bound's timer %v", got, r.clock.hasTimer(bound))
		}
	})
}

// TestIssue507_StaleOperationalAddress is issue 507's live log: the stored
// operational address is dead (a stale prefix), the node answers at
// another address of its host; discovery flips to the router's target
// before that address was attempted. The address stays a retained
// candidate, ranked ahead of the router's, and is attempted when the
// stagger passed — without the node's host being advertised again — where
// the node answers: the session comes up there.
func TestIssue507_StaleOperationalAddress(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		r := newRig507(t)
		r.nodeIPs = []string{"fd00::1", "fd00::9"}
		answered := false
		r.respond = func(_ unsecured, to string) {
			if to == addrSecond && !answered {
				answered = true
				// The node answers here: its session, made from this
				// address, ends the process.
				r.set.Adopt(r.secureSession(r.t, 0x52, udpAddr(addrSecond)))
			}
		}
		p := r.peer(i507Host)
		r.nodeOnly()
		r.connect(p)
		r.advance(20 * time.Second)
		r.routerOnly()
		took, ok := r.advanceUntil(5*time.Minute, func() bool { return answered })
		if !ok {
			t.Fatalf("the second address was not attempted; order: %s; %s", r.order(), r.state(p))
		}
		if got := r.fresh(addrSecond, 0, time.Hour); len(got) != 1 || got[0] != 45*time.Second {
			t.Fatalf("the second address's Sigma1 at %v (%v after the flip), want 45s; order: %s", got, took, r.order())
		}
		if got := r.fresh(routerLL, 0, time.Hour); len(got) != 0 {
			t.Fatalf("the router's Sigma1 at %v, before the retained address answered; order: %s", got, r.order())
		}
		for _, a := range p.Service().Addresses() {
			if a.Host == i507Host {
				t.Fatalf("the node's host was advertised again: %v", p.Service().Addresses())
			}
		}
		eventually(t, "the process to end", func() bool { return process(p) == nil })
		if got := p.Descriptor().OperationalAddress.String(); got != addrSecond {
			t.Fatalf("the operational address %s, want the second %s", got, addrSecond)
		}
	})
}

// i507BusyWait is the Busy delay of the node's 65535 ms answer with the
// harness's jitter (DelayAfterNetworkError 15 s × 0.5).
const i507BusyWait = 65535*time.Millisecond + 7500*time.Millisecond

// TestIssue507_BusyDeadline: the node's Busy holds every Sigma1 to the peer
// until the deadline — across an abort of the attempt that received it
// and the preemption that restarts its address, and at a second address —
// and a session clears it.
func TestIssue507_BusyDeadline(t *testing.T) {
	t.Parallel()
	busyAt := func(r *rig507, addr string) {
		r.respond = func(u unsecured, to string) {
			if to == addr {
				r.status(u, addr, generalStatusBusy, secureChannelStatusBusy, busyData(65535))
			}
		}
	}
	t.Run("abort and preemption", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			// Without the retention the flip aborts the attempt waiting out
			// Busy; the host's return preempts the stagger and restarts it.
			r := newRig507(t, withoutRetention)
			busyAt(r, addr1)
			p := r.peer(i507Host)
			r.nodeOnly()
			r.connect(p)
			first := attemptOf(p, addr1)
			r.advance(50 * time.Second)
			r.routerAlso()
			r.nodeGone()
			if first == nil || first.ctx.Err() == nil {
				t.Fatalf("the flip did not abort the attempt; %s", r.state(p))
			}
			r.advance(10 * time.Second)
			r.nodeOnly()
			second := attemptOf(p, addr1)
			if second == nil || second == first {
				t.Fatalf("the host's return did not restart the address; %s", r.state(p))
			}
			r.advance(20 * time.Second)
			if got := r.fresh(addr1, i507Step, i507BusyWait); len(got) > 0 {
				t.Fatalf("fresh Sigma1 to the ULA at %v, inside the Busy wait of %v; order: %s", got, i507BusyWait, r.order())
			}
			if got := r.fresh(routerLL, 0, i507BusyWait); len(got) > 0 {
				t.Fatalf("Sigma1 to the router at %v, inside the Busy wait; order: %s", got, r.order())
			}
			if got := r.fresh(addr1, i507BusyWait, time.Hour); len(got) == 0 || got[0] > i507BusyWait+i507Step {
				t.Fatalf("the restarted attempt's Sigma1 at %v, want at the deadline %v; order: %s", got, i507BusyWait, r.order())
			}
		})
	})
	t.Run("second address", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			r := newRig507(t)
			r.nodeIPs = []string{"fd00::1", "fd00::9"}
			busyAt(r, addr1)
			p := r.peer(i507Host)
			r.nodeOnly()
			r.connect(p)
			r.advance(90 * time.Second)
			if attemptOf(p, addrSecond) == nil {
				t.Fatalf("the second address was not attempted; %s", r.state(p))
			}
			got := r.fresh(addrSecond, 0, time.Hour)
			if len(got) == 0 || got[0] < i507BusyWait || got[0] > i507BusyWait+i507Step {
				t.Fatalf("the second address's first Sigma1 at %v, want at the Busy deadline %v; order: %s", got, i507BusyWait, r.order())
			}
		})
	})
	t.Run("a session clears it", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			r := newRig507(t)
			busyAt(r, addr1)
			p := r.peer(i507Host)
			r.nodeOnly()
			r.connect(p)
			if d := p.busyRemaining(); d != i507BusyWait {
				t.Fatalf("the Busy deadline in %v, want %v", d, i507BusyWait)
			}
			s := r.secureSession(t, 0x53, udpAddr(addr1))
			r.set.Adopt(s)
			eventually(t, "the process to end", func() bool { return process(p) == nil })
			if d := p.busyRemaining(); d > 0 {
				t.Fatalf("the Busy deadline holds %v after the session", d)
			}
			r.respond = nil
			s.Close()
			eventually(t, "the session to go", func() bool { return !p.HasSession() })
			r.connect(p)
			if got := r.fresh(addr1, 0, time.Hour); len(got) == 0 {
				t.Fatalf("no Sigma1 at once after the session cleared the deadline; order: %s; %s", r.order(), r.state(p))
			}
		})
	})
}

// TestIssue507_FallbackRetiredWithoutRetention: matter.js's retirement,
// deterministic. Without the retention and without a proven host, the
// ULA's attempt is kept as fallback at the flip (the only attempt), the
// router's address supersedes it, and AddressChangeStabilizationDelay later
// the fallback is retired and the router's address starts at once —
// whichever of the retirement worker and the process's loop sees the flip
// first (refall).
func TestIssue507_FallbackRetiredWithoutRetention(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		r := newRig507(t, withoutRetention)
		p := r.peer("")
		r.nodeOnly()
		r.connect(p)
		first := attemptOf(p, addr1)
		r.advance(20 * time.Second)
		r.routerOnly()
		flip := r.now()
		r.advance(10*time.Second - i507Step)
		if attemptOf(p, addr1) != first || first.ctx.Err() != nil {
			t.Fatalf("the fallback was retired before the stabilization delay; %s", r.state(p))
		}
		r.advance(i507Step)
		if first.ctx.Err() == nil || attemptOf(p, addr1) != nil {
			t.Fatalf("the fallback was not retired; %s", r.state(p))
		}
		if got := r.fresh(routerLL, 0, time.Hour); len(got) != 1 || got[0] != flip+10*time.Second {
			t.Fatalf("the router's Sigma1 at %v, want at the retirement %v; order: %s", got, flip+10*time.Second, r.order())
		}
	})
}

// TestRetainedFallbackNotRetired: without a proven host, the operational
// address — attempted as matter.js's fallback while nothing was
// discovered — is retained once discovery names another address: the
// retirement passes it by, the discovered address starts beside it at
// once (anchorBesideLocked), and at the bound it is deleted, its attempt aborted
// while the other goes on (TestFallbackRetiredAfterStabilization is
// matter.js's behaviour).
func TestRetainedFallbackNotRetired(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		const bound = 2 * time.Minute
		h := newHarness(t, retainFor(bound))
		p, _ := h.set.AddKnownPeer(Descriptor{Address: testAddr, OperationalAddress: udpAddr(addr9)})
		connectAsync(p, ConnectOptions{ConnectionTimeout: -1})
		h.sigma1(t, addr9)
		c := process(p)
		fb := attemptAt(c, addr9)
		h.announceAddrs(t, "fd00::1")
		eventually(t, "the retention", func() bool { return len(retainedOf(c)) == 1 })
		h.sigma1(t, addr1)
		h.clock.Advance(45 * time.Second)
		h.clock.Yield()
		if attemptAt(c, addr9) != fb || fb.ctx.Err() != nil {
			t.Fatal("the retained fallback was retired")
		}
		h.clock.Advance(bound - 45*time.Second - time.Millisecond)
		h.clock.Yield()
		if fb.ctx.Err() != nil {
			t.Fatal("the retained fallback ended before the bound")
		}
		h.clock.Advance(time.Millisecond)
		eventually(t, "the bound", func() bool { return fb.ctx.Err() != nil && attemptAt(c, addr9) == nil })
		if attemptAt(c, addr1) == nil {
			t.Fatal("the discovered address's attempt ended with the retention")
		}
	})
}

// TestIssue507_SessionEndsAnchor: without a proven host the anchored
// operational address is retained while the router's target is the only
// one; a session from it, or the process stopped, ends the process and no
// retention is created afterwards — not even when the attempts' goroutines
// look at the fallback once more on their way out.
func TestIssue507_SessionEndsAnchor(t *testing.T) {
	t.Parallel()
	const bound = 7 * time.Minute
	for _, end := range []struct {
		name string
		do   func(r *rig507, p *Peer)
	}{
		{"session", func(r *rig507, p *Peer) { r.set.Adopt(r.secureSession(r.t, 0x54, udpAddr(addr1))) }},
		{"stopConnecting", func(_ *rig507, p *Peer) { p.stopConnecting(context.Background()) }},
	} {
		t.Run(end.name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				r := newRig507(t, retainFor(bound))
				p := r.peer("")
				r.routerOnly()
				r.connect(p)
				r.advance(50 * time.Second)
				c := process(p)
				if got := retainedOf(c); len(got) != 1 {
					t.Fatalf("retained %v, want the anchor", got)
				}
				end.do(r, p)
				<-c.done
				synctest.Wait()
				// After stopConnecting the waiting Connect starts a new
				// process, whose own anchor is due in the full bound.
				fresh := process(p) == nil && r.clock.hasTimer(bound)
				if got := retainedOf(c); len(got) != 0 || fresh || r.clock.hasTimer(bound-50*time.Second) {
					t.Fatalf("after the end: retained %v, timers %v", got, r.clock.pending())
				}
			})
		})
	}
}

// TestIssue507_BusyHoldsTheHandlersDelay: the error handler's replacement
// of the Busy delay is the delay the peer's deadline holds.
func TestIssue507_BusyHoldsTheHandlersDelay(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		r := newRig507(t, func(c *Context) {
			c.HandleError = func(error) (time.Duration, bool, error) { return 2 * time.Second, true, nil }
		})
		r.respond = func(u unsecured, to string) {
			r.status(u, to, generalStatusBusy, secureChannelStatusBusy, busyData(65535))
		}
		p := r.peer(i507Host)
		r.nodeOnly()
		r.connect(p)
		if d := p.busyRemaining(); d != 2*time.Second {
			t.Fatalf("the Busy deadline in %v, want the handler's 2s", d)
		}
	})
}

// TestIssue507_BusyAfterSessionHoldsNothing: a Busy handled while the peer
// has a session — a parallel attempt's, after another established the
// session — sets no deadline.
func TestIssue507_BusyAfterSessionHoldsNothing(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, nil)
		p, _ := h.set.For(testAddr)
		h.set.Adopt(h.secureSession(t, 0x55, udpAddr(addr1)))
		eventually(t, "the session", p.HasSession)
		p.holdBusy(time.Minute)
		if d := p.busyRemaining(); d > 0 {
			t.Fatalf("the Busy deadline holds %v with a session", d)
		}
	})
}

// TestIssue507_DeletionAfterTheFlipBack: discovery's deletion of the
// node's address is handled after the address was advertised again (the
// stream runs behind the cache): the attempt in flight goes on — the
// addition follows and finds it running.
func TestIssue507_DeletionAfterTheFlipBack(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		r := newRig507(t)
		p := r.peer(i507Host)
		r.nodeOnly()
		r.connect(p)
		first := attemptOf(p, addr1)
		c := process(p)
		c.mu.Lock()
		c.withdrawnLocked(addressOf(udpAddr(addr1)))
		c.mu.Unlock()
		if first == nil || first.ctx.Err() != nil || attemptOf(p, addr1) != first || len(retainedOf(c)) != 0 {
			t.Fatalf("the attempt at the advertised address was aborted or retained; %s", r.state(p))
		}
	})
}

// announceOn injects one response received on the interface ifIndex.
func (f *fakeMDNS) announceOn(t *testing.T, ifIndex int, rrs ...dns.RR) {
	t.Helper()
	m := new(dns.Msg)
	m.Response = true
	m.Answer = rrs
	b, err := m.Pack()
	if err != nil {
		t.Fatal(err)
	}
	f.out <- mdns.QueryResponse{Msg: b, IfIndex: ifIndex}
}

// TestIssue507_ZoneSpelledTwoWays: the stored operational address carries
// its zone as the interface index (the session's spelling), discovery
// spells it with the interface's name. The anchor treats them as one
// address: it is not anchored beside its own discovered spelling, and its
// retention ends when the other spelling is advertised.
func TestIssue507_ZoneSpelledTwoWays(t *testing.T) {
	t.Parallel()
	ifi, err := net.InterfaceByIndex(1)
	if err != nil {
		t.Skip("no interface with index 1")
	}
	op := "[fe80::1%" + strconv.Itoa(ifi.Index) + "]:5540"
	t.Run("not anchored beside its discovered spelling", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			h := newHarness(t, nil)
			p, _ := h.set.AddKnownPeer(Descriptor{Address: testAddr, OperationalAddress: udpAddr(op)})
			h.mdns.announceOn(t, ifi.Index, srvRR(h.qname, h.hostQ, 5540, 120), aaaaRR(h.hostQ, "fe80::1", 120))
			eventually(t, "the discovered address", func() bool {
				as := p.Service().Addresses()
				return len(as) == 1 && as[0].IP.Zone() == ifi.Name
			})
			connectAsync(p, ConnectOptions{ConnectionTimeout: -1})
			eventually(t, "the process", func() bool { return process(p) != nil })
			synctest.Wait()
			c := process(p)
			if got := retainedOf(c); len(got) != 0 {
				t.Fatalf("anchored beside its discovered spelling: %v", got)
			}
			c.mu.Lock()
			defer c.mu.Unlock()
			if n := len(c.attempts) + len(c.pending); n != 1 {
				t.Fatalf("%d attempts and pending addresses, want the discovered one alone", n)
			}
		})
	})
	t.Run("the other spelling ends the retention", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			h := newHarness(t, nil)
			p, _ := h.set.AddKnownPeer(Descriptor{Address: testAddr, OperationalAddress: udpAddr(op)})
			h.announceAddrs(t, "fd00::5")
			eventually(t, "the other address", func() bool { return len(p.Service().Addresses()) == 1 })
			connectAsync(p, ConnectOptions{ConnectionTimeout: -1})
			eventually(t, "the process", func() bool { return process(p) != nil })
			c := process(p)
			eventually(t, "the anchor", func() bool { return len(retainedOf(c)) == 1 })
			h.mdns.announceOn(t, ifi.Index, aaaaRR(h.hostQ, "fe80::1", 120))
			eventually(t, "the retention to end", func() bool { return len(retainedOf(c)) == 0 })
		})
	})
}

// TestIssue507_NewAddressBesideTheAnchor: no proven host, the stored
// address is stale and discovery offers a new one: the anchor's Sigma1
// and the new address's go out at once; a second discovered address
// follows the stagger.
func TestIssue507_NewAddressBesideTheAnchor(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, nil)
		p, _ := h.set.AddKnownPeer(Descriptor{Address: testAddr, OperationalAddress: udpAddr(addr9)})
		h.announceAddrs(t, "fd00::1")
		eventually(t, "the new address", func() bool { return len(p.Service().Addresses()) == 1 })
		began := h.clock.Now()
		connectAsync(p, ConnectOptions{ConnectionTimeout: -1})
		seen := map[string]bool{}
		for !seen[addr9] || !seen[addr1] {
			d := h.udp.next(t)
			if decodeUnsecured(t, d.bytes).proto.Opcode == mrp.SCOpcodeSigma1 {
				seen[d.to.String()] = true
			}
		}
		if d := h.clock.Now().Sub(began); d != 0 {
			t.Fatalf("the new address began after %v", d)
		}
		h.clock.Advance(time.Second)
		h.announceAddrs(t, "fd00::1", "fd00::2")
		eventually(t, "the second address", func() bool { return len(p.Service().Addresses()) == 2 })
		h.clock.waitTimer(t, 44*time.Second)
		h.clock.Advance(44*time.Second - time.Millisecond)
		h.noSigma1(t, addr2)
		h.clock.Advance(time.Millisecond)
		h.sigma1(t, addr2)
	})
}
