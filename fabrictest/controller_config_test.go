// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest_test

import (
	"context"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"
)

// TestControllerConfigOnTheSite: a controller.New on Site.ControllerConfig
// discovers a device AddNode built from a daemon's configuration with its
// client stack enabled, on the site's clock.
func TestControllerConfigOnTheSite(t *testing.T) {
	t.Parallel()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	cfg := daemonConfig()
	cfg.Client = node.Client{Enabled: true, Listen: "[::]:5541"}
	dev := site.AddNode(cfg)
	ccfg := site.ControllerConfig(fabrictest.ControllerOptions{Label: "site", Attestation: attestation.TestStore()})
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	var c *controller.Controller
	if err := site.Run(ctx, func(ctx context.Context) (err error) {
		c, err = controller.New(ctx, ccfg)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = site.Run(ctx, c.Close) }()
	if got := c.Fabric().CurrentLabel(); got != "site" {
		t.Fatalf("label %q", got)
	}
	var found int
	if err := site.Run(ctx, func(ctx context.Context) error {
		devs, err := c.Discover(ctx, discovery.ByLongDiscriminator(dev.Discriminator()), 3*time.Second)
		found = len(devs)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if found != 1 {
		t.Fatalf("found %d devices", found)
	}
	if dev.Peers() == nil {
		t.Fatal("the daemon's client stack is not on the site")
	}
}
