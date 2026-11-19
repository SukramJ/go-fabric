// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"errors"
	"net"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock"
)

// TestTrackOnExchangeRefusesWhileAMessageAwaitsItsAck (review II, hs 4b):
// a second message of the same exchange and role is refused while the
// first waits for its acknowledgement, and taken once it is acknowledged;
// a message of the other role that shares the id is another exchange and
// takes a slot of its own, the first keeping its retransmissions (review
// II, phase 7, R2-P7-req-03).
func TestTrackOnExchangeRefusesWhileAMessageAwaitsItsAck(t *testing.T) {
	t.Parallel()
	tr := newOutboundReliableTracker(nil, clock.System{})
	dest := &net.UDPAddr{IP: net.IPv6loopback, Port: 5540}
	now := time.Now()
	if err := tr.TrackOnExchange(1, 7, 0x10, false, []byte{1}, dest, now); err != nil {
		t.Fatalf("first message: %v", err)
	}
	if err := tr.TrackOnExchange(2, 7, 0x10, false, []byte{2}, dest, now); !errors.Is(err, errExchangeAwaitsAck) {
		t.Fatalf("second message while the first is unacknowledged: %v, want errExchangeAwaitsAck", err)
	}
	if tr.Pending() != 1 {
		t.Fatalf("pending = %d, want the first message alone", tr.Pending())
	}
	if !tr.Ack(7, 1) {
		t.Fatal("the first message's ack found no entry")
	}
	if err := tr.TrackOnExchange(2, 7, 0x10, false, []byte{2}, dest, now); err != nil {
		t.Fatalf("second message after the ack: %v", err)
	}
	if err := tr.TrackOnExchange(3, 7, 0x10, true, []byte{3}, dest, now); err != nil {
		t.Fatalf("a message of the other role on the shared id: %v, want it taken", err)
	}
	if tr.Pending() != 2 {
		t.Fatalf("pending = %d, want both roles' messages: the exchanges are two", tr.Pending())
	}
}
