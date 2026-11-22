// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package interaction_test

import (
	"context"
	"sync/atomic"
	"testing"
	"testing/synctest"

	"thingfabric.dev/go-fabric/client/exchange"
	"thingfabric.dev/go-fabric/client/interaction"
	"thingfabric.dev/go-fabric/im"
)

type roleFactKey struct{}

// TestServeDispatchOfTheMoment: with ServerConfig.Dispatch the server asks
// for the dispatcher per request — none answers Failure, the one of the
// moment serves — and RequestContext adds the role's facts of the session
// to the request's context.
func TestServeDispatchOfTheMoment(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		s, reg, _, _ := handlerRig(t)
		var current atomic.Pointer[fakeDispatcher]
		var stamped atomic.Bool
		reg.Serve(interaction.ServerConfig{
			// No tree yet.
			Dispatch:  func() im.Dispatcher { return nil },
			Qualities: plainQualities{},
			RequestContext: func(ctx context.Context, sess *exchange.Session) context.Context {
				return context.WithValue(ctx, roleFactKey{}, sess.ID())
			},
		})
		s.push(t, 0x7401, im.OpcodeInvokeRequest, invokeRequest(t, false, false, 20))
		s.ackPushed(t, expectStatus(t, s, im.StatusFailure))

		d := &fakeDispatcher{sizes: map[uint32]int{20: 10}}
		current.Store(d)
		reg.Serve(interaction.ServerConfig{
			Dispatch:  func() im.Dispatcher { return current.Load() },
			Qualities: plainQualities{},
			RequestContext: func(ctx context.Context, sess *exchange.Session) context.Context {
				stamped.Store(sess != nil)
				return ctx
			},
		})
		s.push(t, 0x7402, im.OpcodeInvokeRequest, invokeRequest(t, false, false, 20))
		s.ackPushed(t, expectOpcode(t, s, im.OpcodeInvokeResponse))
		synctest.Wait()
		d.mu.Lock()
		n := len(d.timed)
		d.mu.Unlock()
		if n != 1 || !stamped.Load() {
			t.Fatalf("served %d invokes, context hook ran %v; want 1 and true", n, stamped.Load())
		}
	})
}
