// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"testing"
	"testing/synctest"

	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/transport/message"
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
//
// The receiver, its handlers and the delivery run in a synctest bubble:
// synctest.Wait returns once each of them is parked or has returned, so
// "the delivery waits" and "the delivery went on" are read off the
// goroutines, not off a clock.
func TestConnReceiverBoundsTheMessagesInFlight(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
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
		synctest.Wait() // the handlers behind held, the delivery at the bound
		select {
		case <-next:
			t.Errorf("message %d was taken while %d are in flight", limit+1, limit)
		default:
		}

		close(held)     // the handlers run and return
		synctest.Wait() // and the delivery with them
		select {
		case <-next:
		default:
			t.Error("the delivery did not go on once the handlers in flight returned")
			r.close() // ends the delivery, so the bubble ends too
		}
		r.wait()
	})
}

// TestConnReceiverCloseEndsAWaitingDelivery: a delivery waiting at the
// bound returns when the connection closes, and takes nothing.
func TestConnReceiverCloseEndsAWaitingDelivery(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
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
		synctest.Wait() // the delivery waits at the bound
		select {
		case <-next:
			t.Errorf("message %d was taken while %d are in flight: the test checks nothing", limit+1, limit)
		default:
		}
		r.close()
		synctest.Wait()
		select {
		case <-next:
		default:
			t.Error("a delivery waiting at the bound outlived the connection's close")
		}
		close(held)
		r.wait()
	})
}

// panickyConn is a BTP-like connection whose name panics: a group message
// delivered on it makes the receive path panic while it logs the drop.
type panickyConn struct{ fakeConn }

func (*panickyConn) String() string { panic("test: a handler panic over BLE") }

// TestConnReceiverRecoversAHandlerPanic (review II, R2-P1-dr-req-08): a
// handler of a message delivered over BLE that panics is recovered, logged
// and counted, as the UDP listener's safeDispatch and the TCP connection's
// safeHandle do — it does not end the process — and the receiver goes on:
// its turn and its place in flight are given back, so the next message is
// handled and wait returns.
func TestConnReceiverRecoversAHandlerPanic(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		b := newUnstartedBridge(t)
		m := &recordingMetrics{}
		b.AttachMetrics(m)
		r := b.newConnReceiver(&panickyConn{})
		group := []byte{0x00, 0x00, 0x00, byte(message.SessionGroup)}
		r.deliver(group)
		r.deliver(group)
		synctest.Wait()
		r.wait()
		if got := m.counter(contract.MetricPanicsRecovered); got != 2 {
			t.Errorf("panics.recovered = %d after two panicking handlers, want 2", got)
		}
		if n := len(r.inFlight); n != 0 {
			t.Errorf("%d places in flight still taken after the handlers panicked", n)
		}
	})
}
