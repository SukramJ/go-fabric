// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest_test

import (
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/controller/discovery"
	"github.com/SukramJ/go-fabric/fabrictest"
)

// TestControllerDiscover: the controller finds the uncommissioned device
// by its long discriminator, and nothing for another one once the timeout
// has passed on the site's clock.
func TestControllerDiscover(t *testing.T) {
	t.Parallel()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	ctx := testContext(t)
	p := site.UncommissionedPair(fabrictest.PairOptions{})
	found, err := p.Controller.Discover(ctx, discovery.ByLongDiscriminator(p.Device.Discriminator()), 90*time.Second)
	if err != nil || len(found) != 1 || found[0].D != p.Device.Discriminator() {
		t.Fatalf("discovered %+v, %v", found, err)
	}
	start := site.Clock().Now()
	none, err := p.Controller.Discover(ctx, discovery.ByLongDiscriminator(p.Device.Discriminator()^0x1), 5*time.Second)
	if err != nil || len(none) != 0 {
		t.Fatalf("discovered %+v, %v for another discriminator", none, err)
	}
	if elapsed := site.Clock().Now().Sub(start); elapsed < 5*time.Second {
		t.Fatalf("the search ended after %v, before its timeout", elapsed)
	}
}

// TestControllerCommissionNode: the registry's node of the discovered
// device is commissioned, started and subscribed.
func TestControllerCommissionNode(t *testing.T) {
	t.Parallel()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	ctx := testContext(t)
	p := site.UncommissionedPair(fabrictest.PairOptions{})
	n, err := p.Controller.CommissionNode(ctx, p.Device, client.CommissionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !n.IsCommissioned() || p.Controller.Registry().Size() != 1 {
		t.Fatalf("commissioned %v, registry size %d", n.IsCommissioned(), p.Controller.Registry().Size())
	}
	if err := site.Eventually(ctx, "the subscription", n.SubscriptionActive); err != nil {
		t.Fatal(err)
	}
}
