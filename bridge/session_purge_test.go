// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"net"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// sessionStateCounts counts what the bridge still holds for sessionID:
// outbound reliable entries, timed deadlines and exchange reply routes.
func sessionStateCounts(b *Bridge, sessionID uint16) (outbound, timed, routes int) {
	b.outboundReliable.mu.Lock()
	for key := range b.outboundReliable.pending {
		if key.sessionID == sessionID {
			outbound++
		}
	}
	b.outboundReliable.mu.Unlock()
	b.routing.timedDeadlines.Range(func(k, _ any) bool {
		if key, ok := k.(timedKey); ok && key.sessionID == sessionID {
			timed++
		}
		return true
	})
	b.routing.exchangeSrcs.Range(func(k, _ any) bool {
		if key, ok := k.(mrp.ExchangeKey); ok && key.SessionID == sessionID {
			routes++
		}
		return true
	})
	return outbound, timed, routes
}

// TestSessionClosedPurgesTheSessionsState: the close of a session — every
// close path reaches SessionClosed — drops all the bridge holds for it:
// its outbound reliable entries (nothing it sent is retransmitted any
// more, a waiter on one is woken), its timed deadlines, its exchange reply
// routes and the ack obligations they route. Another session's state
// stays. Review II, R2-P2-sess-06 and R2-P2-sess-10 (S-21).
func TestSessionClosedPurgesTheSessionsState(t *testing.T) {
	t.Parallel()
	const closing, other uint16 = 7, 8
	const exchangeID uint16 = 0x0042
	b := newUnstartedBridge(t)
	tracker := mrp.NewAckTracker(time.Hour) // obligations only; no pump
	b.AttachAckTracker(tracker)
	peer := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 5540}
	now := b.clk().Now()

	for _, sid := range []uint16{closing, other} {
		b.outboundReliable.Track(100, sid, exchangeID, []byte{0x01}, peer, now)
		b.routing.timedDeadlines.Store(timedKey{sessionID: sid, exchangeID: exchangeID}, now.Add(time.Minute))
		b.owedInboundAck(peer,
			&message.Header{SessionID: sid, MessageCounter: 700},
			message.ProtocolHeader{ProtocolID: mrp.SecureChannelProtocolID, ExchangeID: exchangeID, Initiator: true, NeedsAck: true})
	}
	// A WaitForAck caller blocked on the closing session's entry, as
	// WaitForAck registers one.
	waiter := make(chan error, 1)
	b.outboundReliable.mu.Lock()
	b.outboundReliable.waiters[outboundKey{closing, 100}] = waiter
	b.outboundReliable.mu.Unlock()

	b.SessionClosed(closing)

	if o, td, r := sessionStateCounts(b, closing); o != 0 || td != 0 || r != 0 {
		t.Fatalf("closed session still holds %d outbound entries, %d timed deadlines, %d reply routes", o, td, r)
	}
	if tracker.Owes(closing, exchangeID, false) {
		t.Fatal("closed session still owes its ack obligation")
	}
	if o, td, r := sessionStateCounts(b, other); o != 1 || td != 1 || r != 1 {
		t.Fatalf("other session holds %d outbound entries, %d timed deadlines, %d reply routes; want 1 of each", o, td, r)
	}
	if !tracker.Owes(other, exchangeID, false) {
		t.Fatal("the other session's ack obligation went with the closed one")
	}
	select {
	case err, ok := <-waiter:
		if !ok || err == nil {
			t.Fatal("the waiter on the closed session's entry was told of an acknowledgement")
		}
	default:
		t.Fatal("the waiter on the closed session's entry was not woken")
	}
}
