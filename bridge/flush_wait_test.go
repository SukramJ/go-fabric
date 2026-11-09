// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/mdns"
)

// TestAwaitFlushedReport: the wait for a flushed report's answer ends
// with the peer's StatusResponse, with the context, or after 500 ms on
// the bridge's clock — matter.js's bound for a DataReport of a closed
// subscription (InteractionMessenger.ts:844 at db021da0), not the chunk
// StatusResponse bound — and the waiter is disarmed on every path
// (issue 473).
func TestAwaitFlushedReport(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		fake := clocktest.New(time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC))
		b, err := New(wbEmptySnapshotter, mdns.NewNoop(), Config{Listen: ":0", Clock: fake}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if bound := b.chunkStatusResponseTimeout(0); bound <= flushStatusResponseTimeout {
			t.Fatalf("the chunk bound %v does not exceed the flush bound %v; the test cannot tell them apart", bound, flushStatusResponseTimeout)
		}
		wait := func(ctx context.Context, exch uint16) <-chan struct{} {
			ch := b.armStatusResponseWait(0, exch, true)
			done := make(chan struct{})
			go func() {
				defer close(done)
				b.awaitFlushedReport(ctx, "subscribe.flush", 0, exch, ch)
			}()
			synctest.Wait()
			return done
		}
		ended := func(done <-chan struct{}) bool {
			synctest.Wait()
			select {
			case <-done:
				return true
			default:
				return false
			}
		}

		done := wait(context.Background(), 21)
		if ended(done) {
			t.Fatal("the wait ended before the answer")
		}
		if !b.signalStatusResponseRX(0, 21, true, im.StatusSuccess) {
			t.Fatal("the flushed report's exchange had no waiter")
		}
		if !ended(done) {
			t.Fatal("the wait did not end with the answer")
		}
		if b.signalStatusResponseRX(0, 21, true, im.StatusSuccess) {
			t.Fatal("the waiter stayed armed after the answer")
		}

		ctx, cancel := context.WithCancel(context.Background())
		done = wait(ctx, 22)
		cancel()
		if !ended(done) {
			t.Fatal("the wait did not end with the context")
		}
		if b.signalStatusResponseRX(0, 22, true, im.StatusSuccess) {
			t.Fatal("the waiter stayed armed after the context ended")
		}

		done = wait(context.Background(), 23)
		if armed := fake.Armed(); !slices.Contains(armed, flushStatusResponseTimeout) {
			t.Fatalf("the wait armed %v, want the %v flush bound", armed, flushStatusResponseTimeout)
		}
		fake.Advance(flushStatusResponseTimeout - time.Millisecond)
		if ended(done) {
			t.Fatal("the wait ended before the bound")
		}
		fake.Advance(time.Millisecond)
		if !ended(done) {
			t.Fatalf("the wait did not end after %v", flushStatusResponseTimeout)
		}
		if b.signalStatusResponseRX(0, 23, true, im.StatusSuccess) {
			t.Fatal("the waiter stayed armed after the bound")
		}
	})
}
