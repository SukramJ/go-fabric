// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"thingfabric.dev/go-fabric/clock"
)

// TestSettleBoundsItsGiveBacks (review II, R2-P6-conc-2210, decision 207,
// fable-6b-w2 G1 module rule): settle's give-backs run under one
// deadline, the cleanup budget, and settle returns only once the chain
// has ended — a give-back the camera does not answer is cut at the
// deadline, the next one starts on an ended context and sends nothing,
// and nothing of the chain runs after settle returned (that is, outside
// the endpoint lock its caller holds). Before, the chain ran on a context
// without a deadline and went on past the budget, after settle returned.
func TestSettleBoundsItsGiveBacks(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		m := newManager(&fakeIO{}, clock.System{}, slog.New(slog.DiscardHandler))
		s := &allocationScope{}
		var running atomic.Int32
		var hadDeadline, laterSawEnded atomic.Bool
		// settle runs newest first: the unanswered give-back, then later.
		s.returnOnFailure(func(ctx context.Context) error {
			laterSawEnded.Store(ctx.Err() != nil)
			return ctx.Err()
		})
		s.returnOnFailure(func(ctx context.Context) error {
			running.Add(1)
			defer running.Add(-1)
			_, ok := ctx.Deadline()
			hadDeadline.Store(ok)
			<-ctx.Done() // the camera never answers
			return ctx.Err()
		})
		returned := make(chan struct{})
		start := time.Now()
		go func() {
			m.settle(context.Background(), s, false)
			close(returned)
		}()
		<-returned
		if took := time.Since(start); took != deviceCleanupBudget {
			t.Errorf("settle returned after %v, want the budget %v", took, deviceCleanupBudget)
		}
		if running.Load() != 0 {
			t.Error("a give-back still runs after settle returned")
		}
		if !hadDeadline.Load() {
			t.Error("the give-back's context had no deadline")
		}
		if !laterSawEnded.Load() {
			t.Error("the give-back after the deadline started on a live context")
		}
	})
}

// cutWatchIO is a watch whose EndSession the camera never answers.
type cutWatchIO struct {
	watchIO
	hadDeadline atomic.Bool
}

func (c *cutWatchIO) endSession(ctx context.Context, _ uint64, _, _ uint16) error {
	_, ok := ctx.Deadline()
	c.hadDeadline.Store(ok)
	<-ctx.Done()
	return ctx.Err()
}

// TestSweepEndSessionHasTheBudget (review II, R2-P6-conc-2210): the
// leftover sweep's EndSession runs within the cleanup budget, so one the
// camera never answers ends at the deadline instead of holding the sweep
// for the service's life. Before, it ran on the service's life alone.
func TestSweepEndSessionHasTheBudget(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		f := newPSFixture()
		io := &cutWatchIO{watchIO: serviceWatch{f.svc}}
		w := newSessionWatch(context.Background(), io, clock.System{}, slog.New(slog.DiscardHandler))
		start := time.Now()
		if err := w.endLeftover(psCamera, psEndpoint, 9); err == nil {
			t.Error("an unanswered EndSession succeeded")
		}
		if took := time.Since(start); took != deviceCleanupBudget {
			t.Errorf("the EndSession ended after %v, want the budget %v", took, deviceCleanupBudget)
		}
		if !io.hadDeadline.Load() {
			t.Error("the sweep's EndSession had no deadline")
		}
	})
}
