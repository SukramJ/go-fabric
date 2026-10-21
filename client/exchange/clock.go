// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package exchange

import "github.com/SukramJ/go-fabric/clock"

// Clock is the time source of a [Manager]: the current time and timers.
// The default is the system clock; a test supplies its own
// ([github.com/SukramJ/go-fabric/clock/clocktest.Fake]) to step the
// retransmission and acknowledgement timers deterministically.
//
// Clock is the module's one time port, [clock.Clock]; the alias keeps
// every existing reference to exchange.Clock compiling.
type Clock = clock.Clock

// Timer is a timer [Clock.AfterFunc] started; it is [clock.Timer].
type Timer = clock.Timer

// SystemClock is the [Clock] of the time package; it is [clock.System].
type SystemClock = clock.System
