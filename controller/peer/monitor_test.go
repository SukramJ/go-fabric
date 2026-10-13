// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package peer

import (
	"context"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/controller/handshake"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/transport/message"
)

// monitored is a peer with a live session at addr1 whose node side the
// test plays.
func monitored(t *testing.T) (*harness, *Peer, nodeSide) {
	t.Helper()
	return monitoredWith(t, nil)
}

func monitoredWith(t *testing.T, edit func(*Context)) (*harness, *Peer, nodeSide) {
	t.Helper()
	h := newHarness(t, edit)
	s, ch := h.securePair(t, 0x50, udpAddr(addr1))
	p := h.set.Adopt(s)
	return h, p, nodeSide{h: h, s: s, ch: ch}
}

// answer replies to the probe datagram d with a final report.
func (n nodeSide) answer(t *testing.T, d datagram) {
	t.Helper()
	hdr, proto, _ := n.read(t, d)
	if proto.Opcode != im.OpcodeReadRequest {
		t.Fatalf("expected a probe, got opcode 0x%02X", proto.Opcode)
	}
	n.send(t, message.ProtocolHeader{HasAck: true, AckCounter: hdr.MessageCounter, Opcode: im.OpcodeReportData, ExchangeID: proto.ExchangeID, ProtocolID: im.InteractionModelProtocolID}, reportData(false, true))
}

type verifyResult struct {
	ok  bool
	err error
}

func verifyAsync(ctx context.Context, p *Peer, reason ReachabilityReason) <-chan verifyResult {
	out := make(chan verifyResult, 1)
	go func() {
		ok, err := p.VerifyReachability(ctx, reason)
		out <- verifyResult{ok, err}
	}()
	return out
}

func waitVerify(t *testing.T, ch <-chan verifyResult) verifyResult {
	t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(10 * time.Second):
		t.Fatal("VerifyReachability did not return")
		return verifyResult{}
	}
}

// failProbes advances the clock until the verification ch returns,
// leaving every probe unanswered; it returns the probes' destinations.
func failProbes(t *testing.T, h *harness, ch <-chan verifyResult) (r verifyResult, to []string) {
	t.Helper()
	for range 400 {
		for drained := false; !drained; {
			select {
			case d := <-h.udp.ch:
				to = append(to, d.to.String())
			default:
				drained = true
			}
		}
		select {
		case r = <-ch:
			return r, to
		default:
		}
		h.clock.Advance(time.Second)
		time.Sleep(time.Millisecond)
	}
	t.Fatal("the verification never ended")
	return verifyResult{}, nil
}

// TestMonitorStillDiscoveredAndCooldown: no probe while the session's
// address is discovered or nothing is (PeerAddressMonitor.ts:182-188), nor
// within the cooldown of the last receipt (:189-192); after it a probe
// that answers advances the backoff (:213-216).
func TestMonitorStillDiscoveredAndCooldown(t *testing.T) {
	t.Parallel()
	h, p, n := monitored(t)
	if r := waitVerify(t, verifyAsync(context.Background(), p, ReasonAddressChange)); !r.ok {
		t.Fatal("nothing discovered: want true without a probe")
	}
	h.announceAddrs(t, "fd00::1", "fd00::2")
	eventually(t, "the addresses", func() bool { return len(p.Service().Addresses()) == 2 })
	h.udp.drain()
	if r := waitVerify(t, verifyAsync(context.Background(), p, ReasonAddressChange)); !r.ok {
		t.Fatal("still discovered: want true")
	}
	h.expire(t, "fd00::1")
	eventually(t, "the expiry", func() bool { return len(p.Service().Addresses()) == 1 })
	if r := waitVerify(t, verifyAsync(context.Background(), p, ReasonAddressChange)); !r.ok {
		t.Fatal("within the cooldown of the session's creation: want true")
	}
	h.udp.none(t)
	h.clock.Advance(10 * time.Minute)
	res := verifyAsync(context.Background(), p, ReasonAddressChange)
	n.answer(t, h.udp.next(t))
	if r := waitVerify(t, res); !r.ok {
		t.Fatal("an answered probe: want true")
	}
	m := p.addressMonitor()
	m.mu.Lock()
	cur := m.fibCurr
	m.mu.Unlock()
	if cur != 10*time.Minute {
		t.Fatalf("cooldown after the first probe = %v", cur)
	}
	m.stop()
}

// TestMonitorMigratesToAnAlternate: the current address fails, a
// discovered alternate answers: the session moves there in place and the
// operational address follows, and the set's SessionMigrated hook closes
// the peer's subscriptions (PeerAddressMonitor.ts:219-252).
func TestMonitorMigratesToAnAlternate(t *testing.T) {
	t.Parallel()
	migrated := make(chan handshake.PeerAddress, 1)
	h, p, n := monitoredWith(t, func(c *Context) { c.SessionMigrated = func(a handshake.PeerAddress) { migrated <- a } })
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
	if n.s.Addr().String() != addr2 {
		t.Fatalf("session at %v", n.s.Addr())
	}
	eventually(t, "the operational address", func() bool { return p.Descriptor().OperationalAddress.String() == addr2 })
	if a := <-migrated; a != p.Address() {
		t.Fatalf("SessionMigrated(%+v)", a)
	}
}

// TestMonitorAllProbesFail: no address answers: the session is lost with
// PeerUnresponsiveError (PeerAddressMonitor.ts:254-266).
func TestMonitorAllProbesFail(t *testing.T) {
	t.Parallel()
	h, p, n := monitored(t)
	h.clock.Advance(time.Hour)
	h.announceAddrs(t, "fd00::2")
	eventually(t, "the address", func() bool { return len(p.Service().Addresses()) == 1 })
	h.udp.drain()
	r, to := failProbes(t, h, verifyAsync(context.Background(), p, ReasonAddressChange))
	if r.ok || !n.s.IsPeerLost() {
		t.Fatalf("ok %v lost %v", r.ok, n.s.IsPeerLost())
	}
	seen := map[string]bool{}
	for _, a := range to {
		seen[a] = true
	}
	if !seen[addr1] || !seen[addr2] {
		t.Fatalf("probed %v, want both addresses", to)
	}
	if ok, err := p.VerifyReachability(context.Background(), ReasonAddressChange); ok || err != nil {
		t.Fatal("a peer without a session verified")
	}
}

// TestMonitorSessionSuspect: a session-suspect run probes the current
// address only, cooldown or not (PeerAddressMonitor.ts:177-180, :219);
// a joiner whose context ends returns without cancelling the run (:121-126).
func TestMonitorSessionSuspect(t *testing.T) {
	t.Parallel()
	h, p, n := monitored(t)
	h.announceAddrs(t, "fd00::2")
	eventually(t, "the address", func() bool { return len(p.Service().Addresses()) == 1 })
	h.udp.drain()
	res := verifyAsync(context.Background(), p, ReasonSessionSuspect)
	first := h.udp.next(t) // the suspect run is in flight
	if first.to.String() != addr1 {
		t.Fatalf("suspect probe to %v", first.to)
	}
	ctx, cancel := context.WithCancel(context.Background())
	joiner := verifyAsync(ctx, p, ReasonAddressChange)
	cancel()
	if r := waitVerify(t, joiner); r.ok || r.err == nil {
		t.Fatalf("a cancelled joiner = %+v", r)
	}
	r, to := failProbes(t, h, res)
	if r.ok || !n.s.IsPeerLost() {
		t.Fatal("a failed suspect probe kept the session")
	}
	for _, a := range to {
		if a != addr1 {
			t.Fatalf("a session-suspect run probed %s", a)
		}
	}
}

// TestMonitorScheduledByAServiceChange: a service change with a live
// session arms the stabilisation timer; when it fires, the address check
// runs (Peer.ts:137-138, PeerAddressMonitor.ts:69-84).
func TestMonitorScheduledByAServiceChange(t *testing.T) {
	t.Parallel()
	h, p, n := monitored(t)
	h.clock.Advance(time.Hour)
	h.announceAddrs(t, "fd00::2")
	h.clock.waitTimer(t, 10*time.Second)
	h.udp.drain()
	h.clock.Advance(10 * time.Second)
	n.answer(t, h.udp.nextTo(t, addr1))
	eventually(t, "the backoff to advance", func() bool {
		m := p.addressMonitor()
		m.mu.Lock()
		defer m.mu.Unlock()
		return m.fibCurr == 10*time.Minute
	})
}
