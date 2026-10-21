// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest

import (
	"context"
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
