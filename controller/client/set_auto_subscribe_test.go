// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client_test

import (
	"context"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/fabrictest"
)

// TestSetAutoSubscribe: switching a started node's default subscription
// off closes it, on again establishes it, and the choice survives a
// controller restart in the node's stored settings.
func TestSetAutoSubscribe(t *testing.T) {
	t.Parallel()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	pair := site.UncommissionedPair(fabrictest.PairOptions{})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	n, err := pair.Controller.CommissionNode(ctx, pair.Device, client.CommissionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := site.Eventually(ctx, "the default subscription", n.SubscriptionActive); err != nil {
		t.Fatal(err)
	}
	if err := site.Run(ctx, func(ctx context.Context) error { return n.SetAutoSubscribe(ctx, false) }); err != nil {
		t.Fatal(err)
	}
	if n.AutoSubscribe() || n.SubscriptionActive() {
		t.Fatalf("after off: AutoSubscribe %v, SubscriptionActive %v", n.AutoSubscribe(), n.SubscriptionActive())
	}
	if err := site.Run(ctx, func(ctx context.Context) error { return n.SetAutoSubscribe(ctx, true) }); err != nil {
		t.Fatal(err)
	}
	if err := site.Eventually(ctx, "the default subscription again", n.SubscriptionActive); err != nil {
		t.Fatal(err)
	}
	if err := site.Run(ctx, func(ctx context.Context) error { return n.SetAutoSubscribe(ctx, false) }); err != nil {
		t.Fatal(err)
	}
	id := n.ID()
	if err := pair.Controller.Restart(ctx); err != nil {
		t.Fatal(err)
	}
	restored := pair.Controller.Registry().Get(id)
	if restored == nil || restored.AutoSubscribe() {
		t.Fatalf("restored node %v: the stored choice off was not kept", restored)
	}
}
