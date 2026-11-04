// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

import (
	"testing"

	"github.com/SukramJ/go-fabric/internal/inflight"
)

// TestGoWorkCountsAsRegistryReaction: a reaction the registry runs in the
// background is counted in the clock's in-flight group from its start
// until it returned, so a test site's clock waits for it.
func TestGoWorkCountsAsRegistryReaction(t *testing.T) {
	t.Parallel()
	g := inflight.New("controller")
	r := &Registry{inflight: g}
	started, release := make(chan struct{}), make(chan struct{})
	r.goWork(func() { close(started); <-release })
	<-started
	if snap := g.Snapshot(); snap[inflight.RegistryReactions] != 1 {
		t.Fatalf("counted %v while the reaction runs, want %s=1", snap, inflight.RegistryReactions)
	}
	close(release)
	r.work.Wait()
	if g.Busy() {
		t.Fatalf("still busy after the reaction returned: %v", g.Snapshot())
	}
	r.closed = true
	r.goWork(func() { t.Error("work after Close ran") })
	if g.Busy() {
		t.Fatal("work refused after Close was counted")
	}
}
