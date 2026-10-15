// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package exchange

import "time"

// Clock is the time source of a [Manager]: the current time and one-shot
// timers. The default is the system clock; a test supplies its own to step
// the retransmission and acknowledgement timers deterministically.
type Clock interface {
	// Now is the current time.
	Now() time.Time
	// AfterFunc calls f in its own goroutine once d has elapsed.
	AfterFunc(d time.Duration, f func()) Timer
}

// Timer is a timer [Clock.AfterFunc] started.
type Timer interface {
	// Stop prevents the timer from firing. It reports whether it did.
	Stop() bool
}

// SystemClock is the [Clock] of the time package.
type SystemClock struct{}

// Now is time.Now.
func (SystemClock) Now() time.Time { return time.Now() }

// AfterFunc is time.AfterFunc.
func (SystemClock) AfterFunc(d time.Duration, f func()) Timer { return time.AfterFunc(d, f) }
