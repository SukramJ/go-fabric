// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"testing"
	"time"
)

// TestDropFabricWaitsForAnExtensionWrite: a fabric's removal takes the
// write mutex a write of the Extension holds across its comparison, its
// setting and its entry in memory, so the removal is one step against the
// write — it does not clear the entry between the write's setting and the
// write's store in memory, which would leave the removed fabric's entry
// behind (review II, phase 4).
func TestDropFabricWaitsForAnExtensionWrite(t *testing.T) {
	t.Parallel()
	a := &AccessControl{extensions: map[uint8][]AccessControlExtensionEntry{1: {{FabricIndex: 1}}}}
	a.writeMu.Lock()
	done := make(chan error, 1)
	go func() { done <- a.MatterDropFabric(context.Background(), 1) }()
	select {
	case <-done:
		t.Fatal("the removal ran while a write held the write mutex")
	case <-time.After(50 * time.Millisecond):
	}
	a.mu.RLock()
	_, held := a.extensions[1]
	a.mu.RUnlock()
	if !held {
		t.Fatal("the entry left memory while a write held the write mutex")
	}
	a.writeMu.Unlock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("MatterDropFabric: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the removal did not run once the write mutex was free")
	}
	a.mu.RLock()
	_, held = a.extensions[1]
	a.mu.RUnlock()
	if held {
		t.Fatal("the entry stayed in memory after the removal")
	}
}
