// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package clockvar holds a [clock.Clock] that a component's SetClock may
// replace while its goroutines read it: an atomic pointer, so a reader
// never needs the component's own lock (some read it while holding that
// lock, which a mutex-guarded clock would deadlock on).
package clockvar

import (
	"sync/atomic"

	"github.com/SukramJ/go-fabric/clock"
)

// Var is a clock.Clock slot; the zero value reads [clock.System].
type Var struct{ p atomic.Pointer[box] }

type box struct{ c clock.Clock }

// Store sets the clock; nil is [clock.System].
func (v *Var) Store(c clock.Clock) { v.p.Store(&box{clock.Or(c)}) }

// Load is the clock last stored, or [clock.System].
func (v *Var) Load() clock.Clock {
	if b := v.p.Load(); b != nil {
		return b.c
	}
	return clock.System{}
}
