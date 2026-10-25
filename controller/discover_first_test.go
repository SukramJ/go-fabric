// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller_test

import (
	"context"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/fabrictest"
)

// TestDiscoverFirst: two site devices advertise the same discriminator; a
// full Discover waits out its timeout and reports both, DiscoverWith
// First returns the first alone, before the timeout on the site's clock.
func TestDiscoverFirst(t *testing.T) {
	t.Parallel()
	site, dev, c := siteController(t, fabrictest.Topology{}, nil)
	site.AddDevice(fabrictest.Topology{})
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	id := discovery.ByLongDiscriminator(dev.Discriminator())
	const timeout = 30 * time.Second
	var all, first []client.Device
	var took time.Duration
	if err := site.Run(ctx, func(ctx context.Context) (err error) {
		if all, err = c.Discover(ctx, id, timeout); err != nil {
			return err
		}
		start := site.Clock().Now()
		first, err = c.DiscoverWith(ctx, id, timeout, controller.DiscoverOptions{First: true})
		took = site.Clock().Now().Sub(start)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("Discover found %d devices, want both", len(all))
	}
	if len(first) != 1 || len(first[0].Addresses) == 0 || first[0].D != dev.Discriminator() {
		t.Fatalf("DiscoverWith First found %+v, want one device with an address", first)
	}
	if took >= timeout {
		t.Fatalf("DiscoverWith First took %v, the whole timeout", took)
	}
	// Nothing matches: an empty result after the timeout, no error.
	var none []client.Device
	if err := site.Run(ctx, func(ctx context.Context) (err error) {
		none, err = c.DiscoverWith(ctx, discovery.ByLongDiscriminator(dev.Discriminator()+1), time.Second, controller.DiscoverOptions{First: true})
		return err
	}); err != nil || none == nil || len(none) != 0 {
		t.Fatalf("DiscoverWith First of no device: %v %v", none, err)
	}
}
