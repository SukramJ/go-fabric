// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package btp

import (
	"errors"
	"testing"
)

// TestSession_SendQueueIsBounded: the messages waiting for room in the
// peer's window are bounded ([maxQueuedMessages]); a message sent past the
// bound is refused with a [FlowError] and the ones queued before it stay.
// The peer controls how many wait: a central that acknowledges nothing of
// the peripheral's leaves every answer to its own messages in the queue
// once its window is full, and the ack-received timeout bounds that only
// in time — a central that acknowledges one segment now and then keeps
// the session.
func TestSession_SendQueueIsBounded(t *testing.T) {
	t.Parallel()
	const sent = 1000
	f := newMMH(t) // window 6
	f.write = func([]byte) error { return nil }
	refused := 0
	f.message = func([]byte) error {
		err := f.s.Send([]byte{0x09, 0x08, 0x07})
		var fe *FlowError
		if errors.As(err, &fe) {
			refused++
			return nil
		}
		return err
	}
	for i := range sent {
		// The central's own sequence numbers run on; it acknowledges
		// nothing.
		if err := f.s.HandleIncoming(singleSegment(t, nil, uint8(i%256), []byte{0x01})); err != nil { //nolint:gosec // i%256 fits
			t.Fatalf("message %d: %v", i, err)
		}
	}
	f.s.mu.Lock()
	queued, active := len(f.s.queue), f.s.active
	f.s.mu.Unlock()
	if !active {
		t.Fatal("the session closed")
	}
	if queued > maxQueuedMessages {
		t.Fatalf("%d messages queued after %d answers the peer did not acknowledge, want at most %d", queued, sent, maxQueuedMessages)
	}
	if queued != maxQueuedMessages || refused == 0 {
		t.Errorf("queued %d, refused %d: want the queue full at %d and the rest refused", queued, refused, maxQueuedMessages)
	}
}
