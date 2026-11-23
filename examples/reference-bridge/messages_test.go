// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"log/slog"
	"sync"
	"testing"

	"thingfabric.dev/go-fabric/cluster/spec"
	msgdef "thingfabric.dev/go-fabric/cluster/spec/messages"
)

// TestCancelledMessageIsNotCompleted holds R2-C2-conc-2603 (b): a
// presentation that fired before CancelMessage stopped it, or that the
// cancel overtakes while it waits for the server, schedules no completion,
// and a completion that fired before the cancel completes nothing.
func TestCancelledMessageIsNotCompleted(t *testing.T) {
	var held heldTimers
	var mu sync.Mutex
	var completed int
	p := &tvMessages{
		logger:    slog.New(slog.DiscardHandler),
		timers:    map[string]*latestTimer{},
		after:     held.after,
		presented: func([]byte) bool { return true },
		complete:  func([]byte) { mu.Lock(); completed++; mu.Unlock() },
	}
	ctx := context.Background()
	msg := msgdef.MessageStruct{MessageId: []byte("0123456789abcdef"), StartTime: spec.NullOf[uint32](), Duration: spec.Nullable[uint64]{Value: 1000}}
	if err := p.PresentMessage(ctx, msg); err != nil {
		t.Fatal(err)
	}
	if err := p.CancelMessage(ctx, msg.MessageId); err != nil {
		t.Fatal(err)
	}
	held.fire(t, 0) // the presentation, fired before the cancel
	if len(held.fns) > 1 {
		held.fire(t, 1)
	}
	if completed != 0 || len(p.timers) != 0 {
		t.Fatalf("the cancelled message: %d completions, %d timers kept", completed, len(p.timers))
	}

	// The presentation runs; the completion fires, then the cancel lands.
	if err := p.PresentMessage(ctx, msg); err != nil {
		t.Fatal(err)
	}
	n := len(held.fns)
	held.fire(t, n-1)
	if len(held.fns) != n+1 {
		t.Fatalf("the presentation armed %d completions, want 1", len(held.fns)-n)
	}
	if err := p.CancelMessage(ctx, msg.MessageId); err != nil {
		t.Fatal(err)
	}
	held.fire(t, n)
	if completed != 0 {
		t.Fatalf("the cancelled message completed %d times", completed)
	}

	// The cancel lands while the presentation waits for the server.
	p.presented = func(id []byte) bool {
		if err := p.CancelMessage(ctx, id); err != nil {
			t.Error(err)
		}
		return true
	}
	if err := p.PresentMessage(ctx, msg); err != nil {
		t.Fatal(err)
	}
	n = len(held.fns)
	held.fire(t, n-1)
	if len(held.fns) != n || len(p.timers) != 0 {
		t.Fatalf("a message cancelled during its presentation armed %d completions, kept %d timers", len(held.fns)-n, len(p.timers))
	}
}
