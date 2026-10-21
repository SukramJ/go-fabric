// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"net"
	"testing"
	"time"
)

// TestOutboundReliable_TickSendsWithoutTheLock pins G1-16 (ADR 0026 P6):
// Tick sends with the tracker's lock released — an Ack that arrives while
// the socket writes (here: from inside send) is taken at once — and an
// entry acknowledged after the sweep picked it is not retransmitted.
func TestOutboundReliable_TickSendsWithoutTheLock(t *testing.T) {
	t.Parallel()
	tr := newOutboundReliableTracker(nil, nil)
	dest := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 2), Port: 5151}
	now := time.Unix(0, 0)
	tr.Track(7, 0, 0, []byte{0xAA}, dest, now)
	tr.Track(8, 0, 0, []byte{0xBB}, dest, now)

	var sent []byte
	send := func(_ net.Addr, payload []byte) error {
		sent = append(sent, payload[0])
		// The Ack of both messages lands while the first resend is on
		// the socket: under the lock this would deadlock.
		tr.Ack(0, 7)
		tr.Ack(0, 8)
		return nil
	}
	done := make(chan struct{})
	go func() {
		tr.Tick(now.Add(time.Second), send)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second): // a held lock never releases: this bound only names the failure
		t.Fatal("Tick holds the tracker's lock while it sends")
	}
	if len(sent) != 1 {
		t.Fatalf("resent %x, want only the first: the second was acknowledged before its turn", sent)
	}
	if tr.Pending() != 0 {
		t.Fatalf("%d entries pending after both Acks", tr.Pending())
	}
}
