// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"bytes"
	"net"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
	"github.com/SukramJ/go-fabric/transport/udp"
)

// kickLookup is a session lookup stating SLOW_PEER's schedule for its
// session (matter.js MessageExchangeTest.ts:219 at b25ed960): both
// intervals 7 s, the active threshold its own.
type kickLookup struct {
	sid       uint16
	threshold time.Duration
}

func (l kickLookup) Lookup(id uint16) (*channel.Session, bool) { return nil, id == l.sid }

func (l kickLookup) PeerMRPSchedule(id uint16) (idle, active, threshold time.Duration, peerActive, ok bool) {
	return 7 * time.Second, 7 * time.Second, l.threshold, true, id == l.sid
}

func (l kickLookup) RetransmitBaseInterval(id uint16, _ time.Time) (time.Duration, bool) {
	return 7 * time.Second, id == l.sid
}

// kickRig is a bridge with one BDX message of an exchange this node
// answers on outstanding: it acknowledges the peer's counter 10, its
// first transmission went out at the fake's start.
type kickRig struct {
	b       *Bridge
	clk     *clocktest.Fake
	peer    *net.UDPConn
	entry   *outboundEntry
	ended   bool
	sent    []byte
	sessID  uint16
	xchgID  uint16
	counter uint32
}

func newKickRig(t *testing.T, threshold time.Duration, protocolBDX bool) *kickRig {
	t.Helper()
	listener, err := udp.New(udp.Config{LocalAddr: "127.0.0.1:0", PreferIPv4: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	peer, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = peer.Close() })
	r := &kickRig{clk: clocktest.New(time.UnixMilli(1_700_000_000_000)), peer: peer, sessID: 0x0E30, xchgID: 0x0101, counter: 500}
	r.b = &Bridge{}
	r.b.clock = r.clk
	r.b.listener = listener
	r.b.sessions = kickLookup{sid: r.sessID, threshold: threshold}
	r.b.outboundReliable = newOutboundReliableTracker(r.b.outboundBaseInterval, r.clk)
	r.sent = []byte{0xB0, 0x0D, 0x0A}
	kick := &exchangeKick{bdx: protocolBDX, hasAck: true, ackCounter: 10, ended: func() bool { return r.ended }}
	now := r.clk.Now()
	if err := r.b.outboundReliable.trackKickable(r.counter, r.sessID, r.xchgID, false, r.sent, peer.LocalAddr(), now, 0, kick); err != nil {
		t.Fatal(err)
	}
	r.b.outboundReliable.markSent(r.sessID, r.counter, now)
	r.entry = r.b.outboundReliable.pending[outboundKey{r.sessID, r.counter}]
	return r
}

// duplicate is the peer's counter dup arriving again gap after the send;
// it reports whether the outstanding message went out again, and checks
// that what went out is that message, the acknowledgement of the
// duplicate in it.
func (r *kickRig) duplicate(t *testing.T, gap time.Duration, dup uint32) bool {
	t.Helper()
	r.clk.Advance(gap)
	hdr := &message.Header{SessionID: r.sessID, MessageCounter: dup}
	proto := message.ProtocolHeader{Initiator: true, ExchangeID: r.xchgID, ProtocolID: 2, NeedsAck: true}
	if !r.b.kickForLiveness(hdr, proto) {
		return false
	}
	buf := make([]byte, 64)
	if err := r.peer.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	n, _, err := r.peer.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("the retransmission did not arrive: %v", err)
	}
	if !bytes.Equal(buf[:n], r.sent) {
		t.Fatalf("retransmitted %x, want the outstanding message %x", buf[:n], r.sent)
	}
	return true
}

// TestBDXLivenessKickOnDuplicate ports matter.js
// packages/protocol/test/protocol/MessageExchangeTest.ts:207-355 at
// b25ed960 (describe "liveness kick on duplicate") onto the bridge's
// reliable tracker: the duplicate's verdict, the retransmission of the
// outstanding message through the listener, its schedule restarted. As in
// matter.js, the 7 s base puts the backoff above the BDX cap, so the armed
// wait is exactly 7.4 s (capBDXWait).
func TestBDXLivenessKickOnDuplicate(t *testing.T) {
	t.Parallel()
	const gap = 7100 * time.Millisecond
	t.Run("retransmits a BDX message when the peer re-asks for what it answers", func(t *testing.T) {
		t.Parallel()
		r := newKickRig(t, 200*time.Millisecond, true)
		if !r.duplicate(t, gap, 10) {
			t.Fatal("no kick")
		}
		if r.entry.retries != 1 || r.entry.inFlight || !r.entry.sentAt.Equal(r.clk.Now()) || !r.entry.nextSendAt.Equal(r.clk.Now().Add(r.entry.interval)) {
			t.Fatalf("after the kick: retries %d, in flight %v, sent at %v, next %v (interval %v)",
				r.entry.retries, r.entry.inFlight, r.entry.sentAt, r.entry.nextSendAt, r.entry.interval)
		}
	})
	t.Run("lets the retransmission carry the ack instead of sending a standalone one", func(t *testing.T) {
		t.Parallel()
		// The retransmitted datagram is the outstanding message, which
		// acknowledges counter 10 (duplicate checks the bytes); the
		// receive path then discharges the owed acknowledgement instead
		// of expediting it (receive.go, the duplicate branch).
		r := newKickRig(t, 200*time.Millisecond, true)
		if !r.duplicate(t, gap, 10) {
			t.Fatal("no kick: the standalone ack would go out")
		}
	})
	t.Run("does not retransmit before the schedule's own cadence has elapsed", func(t *testing.T) {
		t.Parallel()
		if newKickRig(t, 200*time.Millisecond, true).duplicate(t, 6500*time.Millisecond, 10) {
			t.Fatal("kicked")
		}
	})
	t.Run("does not retransmit when the peer can stay awake for the remaining wait", func(t *testing.T) {
		t.Parallel()
		r := newKickRig(t, 500*time.Millisecond, true)
		if r.entry.interval != bdxMaxRetransmissionInterval {
			t.Fatalf("armed interval %v, want the BDX cap %v", r.entry.interval, bdxMaxRetransmissionInterval)
		}
		if r.duplicate(t, gap, 10) {
			t.Fatal("kicked")
		}
	})
	t.Run("acks rather than retransmitting once the retransmission budget is down to its last attempt", func(t *testing.T) {
		t.Parallel()
		r := newKickRig(t, 200*time.Millisecond, true)
		r.entry.retries = mrp.MaxRetransmissions
		if r.duplicate(t, gap, 10) {
			t.Fatal("kicked")
		}
	})
	t.Run("does not retransmit for a non-BDX protocol", func(t *testing.T) {
		t.Parallel()
		if newKickRig(t, 200*time.Millisecond, false).duplicate(t, gap, 10) {
			t.Fatal("kicked")
		}
	})
	t.Run("does not retransmit for a duplicate of an unrelated message", func(t *testing.T) {
		t.Parallel()
		if newKickRig(t, 200*time.Millisecond, true).duplicate(t, gap, 99) {
			t.Fatal("kicked")
		}
	})
	t.Run("does not retransmit on an ended exchange", func(t *testing.T) {
		t.Parallel()
		r := newKickRig(t, 200*time.Millisecond, true)
		r.ended = true
		if r.duplicate(t, gap, 10) {
			t.Fatal("kicked")
		}
	})
	t.Run("does not retransmit before the first transmission went out", func(t *testing.T) {
		t.Parallel()
		r := newKickRig(t, 200*time.Millisecond, true)
		r.entry.sentAt, r.entry.inFlight = time.Time{}, true
		if r.duplicate(t, gap, 10) {
			t.Fatal("kicked")
		}
	})
}
