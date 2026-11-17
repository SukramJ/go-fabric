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

// TestAcceptRefusedHandshakeIsDisconnected (review II, R2-P1-dr-req-03): a
// subscription whose kept handshake request opens no session — nine bytes
// that pass for a request but name no version — drops the connection, and
// the accept timer is stopped only once a session exists. Before, the timer
// was stopped first and the refusal did not disconnect, so the central held
// the one connection with no timer left.
func TestAcceptRefusedHandshakeIsDisconnected(t *testing.T) {
	clk := newFakeClock()
	f, o := acceptFake(t, clk)
	clk.Advance(5 * time.Second)
	f.write([]byte{0x65, 0x6c, 0x00, 0x00, 0x00, 0x00, 0x64, 0x00, 0x05})
	f.subscribe(100)
	if o.count() != 0 {
		t.Fatalf("%d sessions opened on a request with no version, want 0", o.count())
	}
	if !f.isGone() {
		clk.Advance(ConnRspTimeout)
		t.Fatalf("the refused handshake left the central connected (after the accept timer too: gone=%v)", f.isGone())
	}
}

// TestAcceptUnconfirmedResponseIsDisconnected holds R2-P6-fsm-0204: a
// central that subscribes C2 and never confirms the handshake response's
// indication is disconnected when the accept timer ends. Before, the
// subscription held the server's lock across the indication and the
// timer's callback waited for it, so the bound did not hold.
func TestAcceptUnconfirmedResponseIsDisconnected(t *testing.T) {
	clk := newFakeClock()
	f, o := acceptFake(t, clk)
	f.mu.Lock()
	f.hold = make(chan struct{}) // never closed: the central never confirms
	f.mu.Unlock()
	f.write(handshakeRequest(100))
	subscribed := make(chan struct{})
	go func() {
		defer close(subscribed)
		f.subscribe(100)
	}()
	waitFor(t, func() bool { return len(f.indicated()) == 1 })
	advanced := make(chan struct{})
	go func() {
		defer close(advanced)
		clk.Advance(ConnRspTimeout)
	}()
	select {
	case <-advanced:
	case <-deadline(t).Done():
		f.goneOnce.Do(func() { close(f.gone) })
		t.Fatal("the handshake timer's callback is blocked behind the indication")
	}
	if !f.isGone() {
		t.Fatalf("a central that never confirmed the response is still connected %v after the accept", ConnRspTimeout)
	}
	select {
	case <-subscribed:
	case <-deadline(t).Done():
		t.Fatal("the subscription kept waiting after the connection dropped")
	}
	if o.count() != 0 {
		t.Fatal("an unconfirmed response opened a session")
	}
}

// TestAcceptRepeatedRequestDoesNotExtendTheBound holds R2-P6-fsm-0205: a
// central that repeats its handshake request and never subscribes is
// disconnected ConnRspTimeout after the accept — a request does not start
// the timer again. Only one timer is ever armed.
func TestAcceptRepeatedRequestDoesNotExtendTheBound(t *testing.T) {
	clk := &heldClock{fakeClock: newFakeClock()}
	f := newFakeGATT()
	if err := Accept(f, (&opened{}).open, Options{TimeSource: clk}); err != nil {
		t.Fatal(err)
	}
	for range 5 {
		f.write(handshakeRequest(100))
		clk.Advance(ConnRspTimeout / 5)
	}
	clk.mu.Lock()
	armed := len(clk.fns)
	clk.mu.Unlock()
	if armed != 1 {
		t.Fatalf("%d handshake timers armed for one accept and five requests, want 1", armed)
	}
	clk.fn(0)()
	if !f.isGone() {
		t.Fatal("the accept timer did not drop a central that only repeated its request")
	}
}

// TestAcceptRequestOnOpenSessionKeepsTheConnection holds R2-P6-fsm-0206: a
// handshake request written on an established session arms no timer, so
// it does not close the session's connection ConnRspTimeout later.
func TestAcceptRequestOnOpenSessionKeepsTheConnection(t *testing.T) {
	clk := newFakeClock()
	f, o := acceptFake(t, clk)
	f.write(handshakeRequest(100))
	f.subscribe(100)
	if o.count() != 1 {
		t.Fatal("no session")
	}
	f.write(mustEncode(t, Packet{
		Header:  Header{HasAckNumber: true, IsBeginningSegment: true, IsEndingSegment: true},
		Payload: Payload{AckNumber: u8(0), SequenceNumber: 0, MessageLength: u16(1), SegmentPayload: []byte{1}},
	}))
	f.write(handshakeRequest(100))
	clk.Advance(ConnRspTimeout + time.Second)
	if f.isGone() {
		t.Fatal("a stray handshake request on an open session dropped its connection")
	}
}
