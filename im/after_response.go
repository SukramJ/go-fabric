// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

import (
	"context"
	"sync"
)

// AfterResponse collects work a command defers until its interaction's
// response has been sent — matter.js NodeSession.initiateClose, which
// defers a session close "until exchanges end" (deferredClose), so a
// RevokeCommissioning received over the PASE session it closes still
// answers on it. The transport creates one per interaction, attaches it
// with [WithAfterResponse] and calls [AfterResponse.Run] once the
// response is on the wire.
type AfterResponse struct {
	// mu guards fns and ran.
	mu  sync.Mutex
	fns []func()
	// ran is set once the response is out: nothing can be deferred until
	// after it any more.
	ran bool
}

type afterResponseKey struct{}

// WithAfterResponse attaches a to ctx.
func WithAfterResponse(ctx context.Context, a *AfterResponse) context.Context {
	return context.WithValue(ctx, afterResponseKey{}, a)
}

// DeferAfterResponse registers fn to run once the response of the
// interaction ctx belongs to has been sent. It reports false — and does
// not register fn — when ctx carries no [AfterResponse], or one whose
// response is out already ([AfterResponse.Run] ran); the caller then does
// the work at once. A context outlives its interaction wherever a command
// starts something that fires later with it — the fail-safe timer an
// ArmFailSafe starts hands its expiry hooks that command's context — and
// work registered then would never run (review II, R2-P0-sec-A03: the PASE
// close on the fail-safe's timer).
func DeferAfterResponse(ctx context.Context, fn func()) bool {
	a, ok := ctx.Value(afterResponseKey{}).(*AfterResponse)
	if !ok || a == nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.ran {
		return false
	}
	a.fns = append(a.fns, fn)
	return true
}

// Run runs the deferred work in registration order, once; later calls
// run nothing, and [DeferAfterResponse] registers nothing from then on.
func (a *AfterResponse) Run() {
	a.mu.Lock()
	fns := a.fns
	a.fns = nil
	a.ran = true
	a.mu.Unlock()
	for _, fn := range fns {
		fn()
	}
}
