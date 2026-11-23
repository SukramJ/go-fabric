// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"thingfabric.dev/go-fabric/controller/softwareupdate"
)

// TestOTAHandoffReleasedOnlyWhenItsNodeDeletedReturned holds
// R2-C2-conc-2301: a hand-off is released only when its own NodeDeleted
// returned — a host OnUpdateFailed for p, reported from q's NodeDeleted
// run, releases nothing (the manager calls the host's callback itself) —
// so Close waits for p's hand-off while p's NodeDeleted still runs.
func TestOTAHandoffReleasedOnlyWhenItsNodeDeletedReturned(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		c := &Controller{}
		p := softwareupdate.Peer{FabricIndex: 1, NodeID: 1}
		q := softwareupdate.Peer{FabricIndex: 1, NodeID: 2}
		op := c.otaHandoffs.start(p)
		oq := c.otaHandoffs.start(q)
		// q's run reported p failed to the host, then returned.
		c.otaHandoffs.done(q, oq)

		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		closed := make(chan struct{})
		go func() {
			c.otaHandoffs.close(ctx)
			close(closed)
		}()
		<-closed // the bubble's clock ends ctx once close waits; a close that ignores it deadlocks the bubble
		if ctx.Err() == nil {
			t.Fatal("close returned while p's NodeDeleted was still running")
		}
		c.otaHandoffs.done(p, op)
	})
}
