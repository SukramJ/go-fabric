// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package clock

import (
	"context"
	"errors"
	"time"
)

// WithTimeout is context.WithTimeout measured on c: the returned context
// ends with [context.DeadlineExceeded] once d has passed on c, or when
// parent ends, or when cancel is called, whichever comes first. A nil c
// and [System] are context.WithTimeout itself, so a host on the wall
// clock sees no change.
//
// On any other clock the deadline is a timer of c: a
// [github.com/SukramJ/go-fabric/clock/clocktest.Fake] ends the context
// when it is advanced past d, never on the machine's time, so a deadline
// in code a test paces on a fake clock cannot expire while the test's
// clock stands. Such a context reports no Deadline of its own (a
// stdlib context derived from it would measure an instant of c on the
// wall clock); its Err is [context.DeadlineExceeded] once the timer fired,
// as context.Cause of it and of every context derived from it is.
//
// This is the deadline form of matter.js's Time port: every timer and
// timeout of the stack goes through the injectable Time
// (packages/general/src/time/Time.ts:73-118 at 862ad75f, getTimer and
// sleep), so MockTime decides when it fires.
func WithTimeout(parent context.Context, c Clock, d time.Duration) (context.Context, context.CancelFunc) {
	if c == nil {
		return context.WithTimeout(parent, d)
	}
	if _, ok := c.(System); ok {
		return context.WithTimeout(parent, d)
	}
	ctx, cancelCause := context.WithCancelCause(parent)
	dc := &deadlineContext{Context: ctx}
	if d <= 0 {
		cancelCause(context.DeadlineExceeded)
		return dc, func() { cancelCause(context.Canceled) }
	}
	t := c.AfterFunc(d, func() { cancelCause(context.DeadlineExceeded) })
	// The parent's end stops the timer too, so a fake clock does not keep
	// the deadline of a finished operation as its next one.
	stop := context.AfterFunc(ctx, func() { t.Stop() })
	return dc, func() {
		stop()
		t.Stop()
		cancelCause(context.Canceled)
	}
}

// WithDeadline is context.WithDeadline measured on c: [WithTimeout] for
// the time from c's now until at. A nil c and [System] are
// context.WithDeadline itself.
func WithDeadline(parent context.Context, c Clock, at time.Time) (context.Context, context.CancelFunc) {
	if c == nil {
		return context.WithDeadline(parent, at)
	}
	if _, ok := c.(System); ok {
		return context.WithDeadline(parent, at)
	}
	return WithTimeout(parent, c, Until(c, at))
}

// deadlineContext is a cancel-cause context whose Err is
// [context.DeadlineExceeded] when its cause is: what a caller that tells a
// timeout from a cancellation by ctx.Err() reads from context.WithTimeout.
type deadlineContext struct {
	context.Context
}

func (c *deadlineContext) Err() error {
	err := c.Context.Err()
	if err != nil && errors.Is(context.Cause(c.Context), context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return err
}
