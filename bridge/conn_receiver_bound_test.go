// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"testing"
	"time"
)

// TestConnReceiverBoundsTheMessagesInFlight: a connection of a transport
// without a listener of the bridge's own — a BTP session — holds at most
// connReceiverMaxInFlight messages that are delivered but not finished;
// the next delivery waits until one of them returns, or the connection
// closes. It is the bound transport/tcp puts on the same queue
// (maxInFlightPerConn, 64), by the same means: the deliverer stops taking
// messages. Before, each message a central sent while a handler held the
// connection's turn became one more goroutine and one more copy, without
// a limit.
func TestConnReceiverBoundsTheMessagesInFlight(t *testing.T) {
	t.Parallel()
	const limit = connReceiverMaxInFlight
	b := newUnstartedBridge(t)
	r := b.newConnReceiver(&fakeConn{})
	// A handler that holds the connection's turn: every later message
	// queues behind it.
	held := make(chan struct{})
	r.mu.Lock()
	r.prev = held
	r.mu.Unlock()

	for range limit {
		r.deliver([]byte{0x00})
	}
	next := make(chan struct{})
	go func() {
		r.deliver([]byte{0x00})
		close(next)
	}()
	select {
	case <-next:
		t.Fatalf("message %d was taken while %d are in flight", limit+1, limit)
	case <-time.After(150 * time.Millisecond):
	}

	close(held) // the handlers run and return
	select {
	case <-next:
	case <-time.After(5 * time.Second):
		t.Fatal("the delivery did not go on once the handlers in flight returned")
	}
	r.wait()
}

// TestConnReceiverCloseEndsAWaitingDelivery: a delivery waiting at the
// bound returns when the connection closes, and takes nothing.
func TestConnReceiverCloseEndsAWaitingDelivery(t *testing.T) {
	t.Parallel()
	const limit = connReceiverMaxInFlight
	b := newUnstartedBridge(t)
	r := b.newConnReceiver(&fakeConn{})
	held := make(chan struct{})
	r.mu.Lock()
	r.prev = held
	r.mu.Unlock()
	for range limit {
		r.deliver([]byte{0x00})
	}
	next := make(chan struct{})
	go func() {
		r.deliver([]byte{0x00})
		close(next)
	}()
	time.Sleep(50 * time.Millisecond)
	r.close()
	select {
	case <-next:
	case <-time.After(5 * time.Second):
		t.Fatal("a delivery waiting at the bound outlived the connection's close")
	}
	close(held)
	r.wait()
}
