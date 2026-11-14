// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package btp

import (
	"context"
	"testing"
	"time"
)

// TestSession_RepeatedAckIsRefused: an acknowledgement is valid only for a
// packet sent and not yet acknowledged, so an ack number already received
// is a protocol error that closes the session and drops the connection —
// it is no progress and does not restart the ack-received timer.
// connectedhomeip refuses it the same way (src/ble/BtpEngine.cpp:142-162
// IsValidAck, :169 BLE_ERROR_INVALID_ACK; the error closes the end point,
// BLEEndPoint.cpp:1293-1297 at 6170af84); matter.js accepts it
// (packages/protocol/src/ble/BtpSessionHandler.ts:287-304 at db021da0)
// (review II, R2-P3-ble-03).
func TestSession_RepeatedAckIsRefused(t *testing.T) {
	t.Parallel()
	clk := newFakeClock()
	f, o := acceptFake(t, clk)
	f.write(handshakeRequest(100))
	f.subscribe(100)
	conn := o.conn(t)

	// The central acknowledges the handshake response (sequence 0).
	f.write(mustEncode(t, Packet{
		Header:  Header{HasAckNumber: true, IsBeginningSegment: true, IsEndingSegment: true},
		Payload: Payload{AckNumber: u8(0), SequenceNumber: 0, MessageLength: u16(1), SegmentPayload: []byte{1}},
	}))
	// The peripheral's sequence 1 is outstanding from here on.
	if err := conn.Send([]byte{0xAA}); err != nil {
		t.Fatal(err)
	}
	clk.Advance(AckTimeout - time.Second)
	if f.isGone() {
		t.Fatal("the connection went before the ack-received timeout")
	}
	// A stand-alone ack that repeats the ack number 0.
	f.write(mustEncode(t, Packet{
		Header:  Header{HasAckNumber: true},
		Payload: Payload{AckNumber: u8(0), SequenceNumber: 1},
	}))
	s := conn.Session()
	s.mu.Lock()
	active := s.active
	s.mu.Unlock()
	if active {
		t.Fatal("the session is still active after a repeated ack number")
	}
	// The server drops the connection on its own goroutine.
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	select {
	case <-f.Disconnected():
	case <-ctx.Done():
		t.Fatal("the connection was kept after a repeated ack number")
	}
}
