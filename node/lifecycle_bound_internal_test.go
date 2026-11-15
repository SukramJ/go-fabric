// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestStartAndFactoryResetBoundedByContext (R2-P5-apinode-04, N-life-5):
// while a Stop holds the lifecycle slot, Start and FactoryReset wait for it
// only until their ctx ends and then return its error — before, both took
// the slot with a bare send and blocked whatever ctx said.
func TestStartAndFactoryResetBoundedByContext(t *testing.T) {
	t.Parallel()
	n := newMDNSTestNode(t, mdnsTestConfig(t))
	n.lifecycle <- struct{}{} // a Stop in progress holds the slot
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	for name, call := range map[string]func(context.Context) error{
		"Start":        n.Start,
		"FactoryReset": n.FactoryReset,
	} {
		done := make(chan error, 1)
		go func() { done <- call(ctx) }()
		select {
		case err := <-done:
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("%s while the slot is held: %v, want the context's error", name, err)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("%s blocked on the held lifecycle slot past its context", name)
		}
	}
	n.leaveLifecycle()
	if n.started.Load() {
		t.Error("the refused Start started the node")
	}
}
