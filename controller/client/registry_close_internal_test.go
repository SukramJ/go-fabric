// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

import (
	"context"
	"testing"
	"time"
)

// TestRegistryCloseEndsAReactionWaitingOnItsContext (review II,
// R2-P6-err-2309): a leave reaction holds the fabric-operation lock while
// it deletes its node on the registry's context (onLeave →
// deleteNode(r.ctx, …)); a node whose stop is held waits there until that
// context ends. Close cancels the context before it drains the lock, so
// it returns. Before, it drained first and waited for the reaction, which
// waited for a context only Close's next step would cancel.
func TestRegistryCloseEndsAReactionWaitingOnItsContext(t *testing.T) {
	t.Parallel()
	h := newRegHarness(t)
	r := h.registry(t, nil)
	holding := make(chan struct{})
	go func() {
		r.op.Lock()
		defer r.op.Unlock()
		close(holding)
		<-r.ctx.Done() // the reaction's n.Close(r.ctx) of a node whose stop is held
	}()
	<-holding
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- r.Close(ctx) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second): // a drain that never ends: this bound only names the failure
		t.Fatal("Registry.Close waits for a reaction that waits for the registry's context")
	}
}
