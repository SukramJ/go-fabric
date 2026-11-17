// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"net"
	"testing"
	"time"
)

// statedForever is the base interval a session resolves from the largest
// SessionIdleInterval a peer may state (a uint32 of milliseconds): no entry
// of such a session ends by its retransmissions within a day.
func statedForever(uint16, time.Time) time.Duration {
	return time.Duration(0xFFFFFFFF) * time.Millisecond
}

// TestSecureRetransmitTableIsBoundedPerSession: a secure session's messages
// waiting for their acknowledgement belong to at most secureExchangeLimit
// exchanges; the exchange tracked least recently loses its message's
// retransmissions to a newer one, the hook hears of
// it, and another session's entries stay (review II, R2-P6-bound-0303).
func TestSecureRetransmitTableIsBoundedPerSession(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_700_000_000, 0)
	tr := newOutboundReliableTracker(statedForever, nil)
	type eviction struct {
		sessionID, exchangeID uint16
		superseded            bool
	}
	var evictions []eviction
	tr.secureEvicted = func(sessionID, exchangeID uint16, _ uint32, superseded bool) {
		evictions = append(evictions, eviction{sessionID, exchangeID, superseded})
	}
	dst := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 5540}
	tr.Track(1, 0x0099, 0x0001, []byte{0x01}, dst, now) // another session
	tr.Track(1, 0x0042, 0x1000, []byte{0x01}, dst, now)
	const n = 1000
	for i := range n {
		tr.Track(uint32(i+2), 0x0042, uint16(0x2000+i), []byte{0x01}, dst, now)
	}
	if got := tr.Pending(); got != secureExchangeLimit+1 {
		t.Fatalf("pending = %d after %d replies on one session, want %d of it and 1 of the other", got, n+1, secureExchangeLimit)
	}
	if !tr.isPending(0x0099, 1) {
		t.Fatal("another session's entry lost its place")
	}
	for i := n - secureExchangeLimit; i < n; i++ {
		if !tr.isPending(0x0042, uint32(i+2)) {
			t.Fatalf("the entry of exchange %#x, among the %d tracked last, is gone", 0x2000+i, secureExchangeLimit)
		}
	}
	if len(evictions) != n+1-secureExchangeLimit || evictions[0] != (eviction{0x0042, 0x1000, false}) {
		t.Fatalf("the hook heard %d evictions, first %+v; want %d, first of exchange 0x1000", len(evictions), evictions[0], n+1-secureExchangeLimit)
	}
	if res := tr.Tick(now.Add(24*time.Hour), func(net.Addr, []byte) error { return nil }); tr.Pending() != secureExchangeLimit+1 || len(res) > secureExchangeLimit+1 {
		t.Fatalf("a day later: pending %d, %d results", tr.Pending(), len(res))
	}
}

// TestSecureRetransmitTableHoldsOneMessagePerExchange: a message tracked on
// an exchange whose earlier message still waits for its acknowledgement
// takes that one's place — an exchange has one message in flight — so
// requests on one exchange that are never acknowledged do not grow the
// table either (review II, R2-P6-bound-0303).
func TestSecureRetransmitTableHoldsOneMessagePerExchange(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_700_000_000, 0)
	tr := newOutboundReliableTracker(statedForever, nil)
	superseded := 0
	tr.secureEvicted = func(_, _ uint16, _ uint32, s bool) {
		if s {
			superseded++
		}
	}
	dst := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 5540}
	for i := range 100 {
		tr.Track(uint32(i+1), 0x0042, 0x3000, []byte{0x01}, dst, now)
	}
	if got := tr.Pending(); got != 1 || !tr.isPending(0x0042, 100) {
		t.Fatalf("pending = %d (newest held=%v), want the newest message alone", got, tr.isPending(0x0042, 100))
	}
	if superseded != 99 {
		t.Fatalf("the hook heard %d superseded messages, want 99", superseded)
	}
	if !tr.Ack(0x0042, 100) || tr.Pending() != 0 {
		t.Fatal("the newest message's acknowledgement did not clear the exchange")
	}
	// The exchange's place is free again: 30 other exchanges fit.
	for i := range secureExchangeLimit {
		tr.Track(uint32(200+i), 0x0042, uint16(0x4000+i), []byte{0x01}, dst, now)
	}
	if got := tr.Pending(); got != secureExchangeLimit {
		t.Fatalf("pending = %d, want %d", got, secureExchangeLimit)
	}
}

func (t *outboundReliableTracker) isPending(sessionID uint16, counter uint32) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	_, ok := t.pending[outboundKey{sessionID, counter}]
	return ok
}
