// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client_test

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"

	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
)

const abandonedTerminated = "matter.controller.interaction.abandoned_terminated"

// TestClientSubscribeCancelledParity ports ClientSubscribeTest's describe
// "cancelled while being established" (packages/node/test/node/ClientSubscribeTest.ts:1116-1330
// at 862ad75f): a subscription the caller gives up on while the peer
// primes it is abandoned (ClientInteraction.ts:1084-1097) and its priming
// report answered INVALID_SUBSCRIPTION (InteractionMessenger.ts:1116-1137),
// so the device keeps no subscription for it. matter.js reads the device's
// log for the refusal; here the controller logs the termination and the
// device's count of subscriptions stays. The two cases whose updated
// callback fails do not apply: a Go Updated returns no error.
func TestClientSubscribeCancelledParity(t *testing.T) {
	t.Parallel()

	setup := func(t *testing.T) (*clientPair, *msgLog) {
		t.Helper()
		log := &msgLog{}
		cp := newClientPair(t, clientPairOptions{controller: fabrictest.ControllerOptions{Logger: slog.New(log)}})
		return cp, log
	}
	request := func() interaction.SubscribeRequest {
		return interaction.SubscribeRequest{Attributes: []im.ConcreteAttributePath{{}}}
	}

	t.Run("answers the priming report that arrives after the cancel with INVALID_SUBSCRIPTION", func(t *testing.T) {
		t.Parallel()
		cp, log := setup(t)
		before := cp.pair.Device.Node().Subscriptions().Active()
		dev := cp.pair.Device.Addr().String()
		var hold atomic.Bool
		hold.Store(true)
		var held atomic.Int32
		cp.site.Net().Filter(func(d fabrictest.Datagram) bool {
			if hold.Load() && d.From.String() == dev {
				held.Add(1)
				return false
			}
			return true
		})
		ctx, cancel := context.WithCancel(parityCtx(t))
		errc := make(chan error, 1)
		go func() {
			_, err := cp.node.Subscribe(ctx, client.SubscribeRequest{SubscribeRequest: request()})
			errc <- err
		}()
		// The device primes the subscription; its replies are held.
		cp.eventually(t, "the device's held reply", func() bool { return held.Load() > 0 })
		cancel()
		if err := <-errc; !errors.Is(err, context.Canceled) {
			t.Fatalf("subscribe: %v, want the cancel", err)
		}
		hold.Store(false)
		cp.eventually(t, "the priming report answered INVALID_SUBSCRIPTION", func() bool { return log.has(abandonedTerminated) })
		cp.eventually(t, "the device's subscriptions", func() bool { return cp.pair.Device.Node().Subscriptions().Active() == before })
	})

	t.Run("answers the priming report it is processing during the cancel with INVALID_SUBSCRIPTION", func(t *testing.T) {
		t.Parallel()
		cp, log := setup(t)
		before := cp.pair.Device.Node().Subscriptions().Active()
		ctx, cancel := context.WithCancel(parityCtx(t))
		req := request()
		req.Updated = func(*interaction.Report) { cancel() }
		var err error
		cp.run(t, func(context.Context) error {
			_, err = cp.node.Subscribe(ctx, client.SubscribeRequest{SubscribeRequest: req})
			return nil
		})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("subscribe: %v, want the cancel", err)
		}
		cp.eventually(t, "the priming report answered INVALID_SUBSCRIPTION", func() bool { return log.has(abandonedTerminated) })
		cp.eventually(t, "the device's subscriptions", func() bool { return cp.pair.Device.Node().Subscriptions().Active() == before })
	})
}
