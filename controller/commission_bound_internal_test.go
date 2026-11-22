// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller

import (
	"context"
	"errors"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/clock/clocktest"
)

// TestCommissionBoundNamesOnlyItsOwnEnd (go-fabric#554, Fable's read): an
// error of a commissioning is DeadlineExceeded, naming the bound, only
// when the bound ended it; a caller's own end — a deadline whose cause
// reaches the bounded context too — keeps its error, and without the
// option nothing is named.
func TestCommissionBoundNamesOnlyItsOwnEnd(t *testing.T) {
	t.Parallel()
	clk := clocktest.New(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	opts := PairOptions{CommissionTimeout: time.Minute}

	ctx, cancel, ended := opts.commissionContext(context.Background(), clk)
	defer cancel()
	clk.Advance(time.Minute)
	<-ctx.Done()
	if err := ended(context.Canceled); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("the bound's end: %v, want context.DeadlineExceeded", err)
	}

	parent, end := context.WithCancelCause(context.Background())
	_, cancel2, ended2 := opts.commissionContext(parent, clk)
	defer cancel2()
	end(context.DeadlineExceeded)                                 // the caller's deadline
	if err := ended2(context.Canceled); err != context.Canceled { //nolint:errorlint // unchanged, not wrapped
		t.Fatalf("the caller's end: %v, want its error unchanged", err)
	}

	parent3, end3 := context.WithCancelCause(context.Background())
	_, cancel3, ended3 := PairOptions{}.commissionContext(parent3, clk)
	defer cancel3()
	end3(context.DeadlineExceeded)
	if err := ended3(context.Canceled); err != context.Canceled { //nolint:errorlint // unchanged, not wrapped
		t.Fatalf("no bound: %v, want the error unchanged", err)
	}
}
