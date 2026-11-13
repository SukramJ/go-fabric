// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client_test

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/SukramJ/go-fabric/controller/client"
)

// withinItsContext runs call with a context of 50 ms and returns its
// error, which must be the context's: a call that still waits a second of
// bubble time later fails the test, after release let it return.
func withinItsContext(t *testing.T, what string, release func(), call func(ctx context.Context) error) {
	t.Helper()
	done := make(chan error, 1)
	ctx := shortContext(t)
	go func() { done <- call(ctx) }()
	time.Sleep(time.Second)
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("%s = %v, want context.DeadlineExceeded", what, err)
		}
	default:
		release()
		<-done
		t.Fatalf("%s still waited for the node's jobs a second after its 50 ms context ended", what)
	}
}

// TestStartReturnsWithItsContextWhileAHookBlocks (review II, the defect
// class of R2-P0-conc-C03 in Start): Start begins with the cache's
// adoption of the default subscription's fabric filter, which waits for
// the node's queued report jobs — and a hook that blocks held it without
// bound, here the hook a Stop had left behind. Start now waits within its
// context and returns the context's error with the node not started;
// once the hook returned it starts.
func TestStartReturnsWithItsContextWhileAHookBlocks(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := holdJobQueue(t)
		if err := h.n.Stop(shortContext(t)); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Stop = %v, want context.DeadlineExceeded", err)
		}
		// No default subscription: the Start below stays off the wire.
		if err := h.n.SetAutoSubscribe(context.Background(), false); err != nil {
			t.Fatal(err)
		}
		withinItsContext(t, "Start", h.release, h.n.Start)
		if client.Started(h.n) {
			t.Fatal("the node is started after a Start that returned its context's error")
		}
		if h.returned.Load() {
			t.Fatal("the hook returned: Start was not held by it")
		}

		h.release()
		if err := h.n.Start(context.Background()); err != nil {
			t.Fatal(err)
		}
		if !client.Started(h.n) {
			t.Fatal("the node is not started after the hook returned")
		}
		if !h.returned.Load() {
			t.Fatal("Start adopted the fabric filter before the queued hook returned")
		}
		if err := h.n.Stop(context.Background()); err != nil {
			t.Fatal(err)
		}
		client.WaitRegistry(h.sr.reg)
	})
}

// TestSetDefaultSubscriptionReturnsWithItsContextWhileAHookBlocks (review
// II, the defect class of R2-P0-conc-C03 in SetDefaultSubscription): the
// adoption of the new request's fabric filter waits for the node's queued
// report jobs. When the context ends first the call returns its error and
// the change is not made — the node keeps the default subscription it
// had and the cache its filter, so the two do not disagree — and the same
// call completes it once the hook returned.
func TestSetDefaultSubscriptionReturnsWithItsContextWhileAHookBlocks(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := holdJobQueue(t)
		if err := h.n.Stop(shortContext(t)); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Stop = %v, want context.DeadlineExceeded", err)
		}
		unfiltered := &client.SubscribeOptions{NoFabricFilter: true}
		withinItsContext(t, "SetDefaultSubscription", h.release, func(ctx context.Context) error {
			return h.n.SetDefaultSubscription(ctx, unfiltered)
		})
		if got := h.n.DefaultSubscription(); got != nil {
			t.Fatalf("the default subscription is %+v after the call failed, want the one before it (nil)", got)
		}
		if got := storedDefaultSubscription(t, h); got != nil {
			t.Fatalf("the stored default subscription is %+v after the call failed, want none", got)
		}
		if !client.CacheFabricFiltered(h.n) {
			t.Fatal("the cache left its fabric filter although the adoption did not begin")
		}

		h.release()
		if err := h.n.SetDefaultSubscription(context.Background(), unfiltered); err != nil {
			t.Fatal(err)
		}
		if got := h.n.DefaultSubscription(); got == nil || !got.NoFabricFilter {
			t.Fatalf("the default subscription is %+v, want the unfiltered one", got)
		}
		if got := storedDefaultSubscription(t, h); got == nil || !got.NoFabricFilter {
			t.Fatalf("the stored default subscription is %+v, want the unfiltered one", got)
		}
		if client.CacheFabricFiltered(h.n) {
			t.Fatal("the cache kept its fabric filter after the adoption")
		}
		if err := h.n.Stop(context.Background()); err != nil {
			t.Fatal(err)
		}
		client.WaitRegistry(h.sr.reg)
	})
}

// TestSetDefaultSubscriptionOnAStartedNodeWhileAHookBlocks: the same on a
// started node, whose default subscription the call ends before the
// adoption — matter.js's order, NetworkClient.#handleDefaultSubscriptionChange.
// The context's error leaves the node without that subscription, as an
// establishment that failed does, and with the request and the filter it
// had.
func TestSetDefaultSubscriptionOnAStartedNodeWhileAHookBlocks(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := holdJobQueue(t)
		unfiltered := &client.SubscribeOptions{NoFabricFilter: true}
		withinItsContext(t, "SetDefaultSubscription", h.release, func(ctx context.Context) error {
			return h.n.SetDefaultSubscription(ctx, unfiltered)
		})
		if got := h.n.DefaultSubscription(); got != nil {
			t.Fatalf("the default subscription is %+v after the call failed, want the one before it (nil)", got)
		}
		if !client.CacheFabricFiltered(h.n) {
			t.Fatal("the cache left its fabric filter although the adoption did not begin")
		}
		if h.n.SubscriptionActive() {
			t.Fatal("the default subscription the call ended is still reported active")
		}
		h.release()
		if err := h.n.Stop(context.Background()); err != nil {
			t.Fatal(err)
		}
		client.WaitRegistry(h.sr.reg)
	})
}

// storedDefaultSubscription is the default subscription the store holds
// for h's node, nil for none.
func storedDefaultSubscription(t *testing.T, h *heldQueue) *client.SubscribeOptions {
	t.Helper()
	o, err := client.StoredDefaultSubscription(h.sr.store, h.n.ID())
	if err != nil {
		t.Fatal(err)
	}
	return o
}
