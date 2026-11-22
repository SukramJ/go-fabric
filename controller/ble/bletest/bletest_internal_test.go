// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bletest

import (
	"sync/atomic"
	"testing"

	"thingfabric.dev/go-fabric/transport/btp"
)

// TestADroppedConnectionStartsNoSession: a connection dropped after
// SubscribeC2 passed its check but before the session started — the
// handshake request reaching maybeStart on the pipe's goroutine — starts
// no session: Serve is not called, so no session is left without its
// closed hook (review II, R2-P6-conc-2053).
func TestADroppedConnectionStartsNoSession(t *testing.T) {
	t.Parallel()
	var serves, closes atomic.Int32
	p := &Peripheral{Address: "AA:BB:CC:00:20:53", Serve: func(*btp.Conn) (func([]byte), func()) {
		serves.Add(1)
		return func([]byte) {}, func() { closes.Add(1) }
	}}
	c := p.accept()
	req := btp.EncodeHandshakeRequest(btp.HandshakeRequest{Versions: []int{4}, ATTMTU: 247, ClientWindowSize: 4})
	c.mu.Lock()
	c.request = req
	c.onC2 = func([]byte) {} // SubscribeC2 passed its gone check
	c.mu.Unlock()
	c.drop()
	c.maybeStart() // the request's maybeStart, after the drop
	c.mu.Lock()
	session, messages := c.session, c.messages
	c.mu.Unlock()
	if session != nil || messages != nil || serves.Load() != closes.Load() {
		t.Fatalf("after the drop: session %v, messages pipe %v, Serve %d times, closed %d times", session != nil, messages != nil, serves.Load(), closes.Load())
	}
}
