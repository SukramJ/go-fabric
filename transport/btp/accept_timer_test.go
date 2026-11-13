// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package btp

import (
	"testing"
	"time"
)

// TestAcceptSilentCentralIsDisconnected: a central that connects and
// starts no handshake is disconnected [ConnRspTimeout] after the accept
// (review II, R2-P1-req-07, owner decision 31). matter.js starts its
// timer only at the handshake request, so a silent central held the
// connection, and with it the radio, for as long as it liked.
func TestAcceptSilentCentralIsDisconnected(t *testing.T) {
	clk := newFakeClock()
	f, o := acceptFake(t, clk)
	clk.Advance(ConnRspTimeout - time.Millisecond)
	if f.isGone() {
		t.Fatal("disconnected before the accept timer ended")
	}
	clk.Advance(time.Millisecond)
	if !f.isGone() {
		t.Fatalf("a silent central is still connected %v after the accept", ConnRspTimeout)
	}
	if o.count() != 0 {
		t.Fatal("a session was opened")
	}
}

// TestAcceptHandshakeInTimeIsUntouched: a central that completes the
// handshake before the timer the accept started ends keeps its connection
// past it — the timer stops with the session and cannot fire into it.
func TestAcceptHandshakeInTimeIsUntouched(t *testing.T) {
	clk := newFakeClock()
	f, o := acceptFake(t, clk)
	clk.Advance(5 * time.Second)
	f.write(handshakeRequest(100))
	f.subscribe(100)
	if o.count() != 1 {
		t.Fatalf("%d sessions after handshake and subscription, want 1", o.count())
	}
	// One Matter message acknowledging the handshake response (sequence
	// 0), so the session's own acknowledgement timer stays out of the way.
	f.write(mustEncode(t, Packet{
		Header:  Header{HasAckNumber: true, IsBeginningSegment: true, IsEndingSegment: true},
		Payload: Payload{AckNumber: u8(0), SequenceNumber: 0, MessageLength: u16(1), SegmentPayload: []byte{1}},
	}))
	clk.Advance(ConnRspTimeout - 5*time.Second)
	if f.isGone() {
		t.Fatal("the timer the accept started dropped an established connection")
	}
}
