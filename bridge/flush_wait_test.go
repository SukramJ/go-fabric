// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/im"
)

// TestFlushWaitContext: only a context marked by withFlushWait asks a
// report to wait for its answer.
func TestFlushWaitContext(t *testing.T) {
	t.Parallel()
	if flushWaitFromContext(context.Background()) {
		t.Fatal("a plain context asks for the wait")
	}
	if !flushWaitFromContext(withFlushWait(context.Background())) {
		t.Fatal("the marked context does not ask for the wait")
	}
}

// TestAwaitFlushedReport: the wait for a flushed report's answer ends
// with the peer's StatusResponse, with the context, or after the chunk
// StatusResponse bound — and the waiter is disarmed on every path.
func TestAwaitFlushedReport(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	b.chunkStatusResponseTimeoutOverride = 50 * time.Millisecond
	wait := func(ctx context.Context, exch uint16) <-chan struct{} {
		ch := b.armStatusResponseWait(0, exch, true)
		done := make(chan struct{})
		go func() {
			defer close(done)
			b.awaitFlushedReport(ctx, "subscribe.event_report", 0, exch, ch)
		}()
		return done
	}
	returned := func(what string, done <-chan struct{}) {
		t.Helper()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("the wait did not end %s", what)
		}
	}

	done := wait(context.Background(), 21)
	select {
	case <-done:
		t.Fatal("the wait ended before the answer")
	case <-time.After(10 * time.Millisecond):
	}
	if !b.signalStatusResponseRX(0, 21, true, im.StatusSuccess) {
		t.Fatal("the flushed report's exchange had no waiter")
	}
	returned("with the answer", done)
	if b.signalStatusResponseRX(0, 21, true, im.StatusSuccess) {
		t.Fatal("the waiter stayed armed after the answer")
	}

	ctx, cancel := context.WithCancel(context.Background())
	done = wait(ctx, 22)
	cancel()
	returned("with the context", done)
	if b.signalStatusResponseRX(0, 22, true, im.StatusSuccess) {
		t.Fatal("the waiter stayed armed after the context ended")
	}

	done = wait(context.Background(), 23)
	returned("after the bound", done)
	if b.signalStatusResponseRX(0, 23, true, im.StatusSuccess) {
		t.Fatal("the waiter stayed armed after the bound")
	}
}
