// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock"
)

// countingWall is the system clock, counting its readings.
type countingWall struct {
	clock.System
	reads atomic.Int64
}

func (w *countingWall) Now() time.Time {
	w.reads.Add(1)
	return w.System.Now()
}

// TestSettleMeasuresOnTheInjectedWallClock pins G1-13 (ADR 0026 P4): the
// quiet check reads the wall clock the site was given (Options.Wall), not
// one it builds itself.
func TestSettleMeasuresOnTheInjectedWallClock(t *testing.T) {
	t.Parallel()
	wall := &countingWall{}
	site := NewSite(t, Options{Logger: slog.New(slog.DiscardHandler), Wall: wall})
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := site.Settle(ctx); err != nil {
		t.Fatal(err)
	}
	if wall.reads.Load() == 0 {
		t.Fatal("Settle never read the injected wall clock")
	}
}

// racingWall is the system clock running a second ahead at every reading.
type racingWall struct {
	clock.System
	ahead atomic.Int64
}

func (w *racingWall) Now() time.Time {
	return w.System.Now().Add(time.Duration(w.ahead.Add(1)) * time.Second)
}

// TestEventuallyWithoutDeadlineEnds pins G2-11: Eventually on a context
// without a deadline gives up once the site's wait (DefaultWait on the
// wall clock) has passed, with ErrWaited, instead of hanging the test.
func TestEventuallyWithoutDeadlineEnds(t *testing.T) {
	t.Parallel()
	wall := &racingWall{}
	site := NewSite(t, Options{Logger: slog.New(slog.DiscardHandler), Wall: wall})
	if site.opts.Wait != DefaultWait {
		t.Fatalf("the default wait is %v, want %v", site.opts.Wait, DefaultWait)
	}
	start := wall.Now()
	err := site.Eventually(context.Background(), "a condition that never holds", func() bool { return false })
	if !errors.Is(err, ErrWaited) {
		t.Fatalf("Eventually: %v, want ErrWaited", err)
	}
	if waited := wall.Now().Sub(start); waited < DefaultWait {
		t.Errorf("gave up after %v on the wall clock, before the default wait %v", waited, DefaultWait)
	}
	short := NewSite(t, Options{Logger: slog.New(slog.DiscardHandler), Wait: time.Millisecond})
	if err := short.Eventually(context.Background(), "never", func() bool { return false }); !errors.Is(err, ErrWaited) {
		t.Fatalf("Eventually with Options.Wait: %v, want ErrWaited", err)
	}
}
