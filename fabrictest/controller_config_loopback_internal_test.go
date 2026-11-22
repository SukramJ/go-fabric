// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest

import (
	"context"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/controller"
)

// TestControllerConfigOnLoopbackLeavesThePortToNew: on a loopback site
// ControllerConfig hands controller.New an auto-assigned listen address,
// not a bound socket, so a controller with incoming TCP binds the UDP and
// TCP pair itself and binds again while the UDP socket's port is taken
// for TCP — matter.js's port assignment, which New applies only to a port
// it chose (controller/tcp.go bindTransports). A socket the site bound
// left the TCP twin of its ephemeral port to chance: a TCP socket
// elsewhere in the machine holding the same number failed New with
// EADDRINUSE (controller/camera TestServiceOnTheFabric,
// service_fabrictest_test.go:77, in CI). The site still answers for the
// controller's operational record at the port New bound.
func TestControllerConfigOnLoopbackLeavesThePortToNew(t *testing.T) {
	t.Parallel()
	site := NewSite(t, Options{Loopback: true})
	cfg := site.ControllerConfig(ControllerOptions{})
	if cfg.Listener != nil || cfg.Listen != "[::1]:0" {
		t.Fatalf("Listener %v, Listen %q: want no socket and [::1]:0", cfg.Listener, cfg.Listen)
	}
	cfg.Camera = &controller.CameraConfig{} // a served tree: the operational record
	cfg.TCP = controller.TCPConfig{Incoming: true, Outgoing: true}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	var c *controller.Controller
	if err := site.Run(ctx, func(ctx context.Context) (err error) {
		c, err = controller.New(ctx, cfg)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = site.Run(ctx, c.Close) }()
	f := c.Fabric()
	addrs, err := siteResolver{site}.ResolveOperational(ctx, f.Identity.CompressedFabricID, f.RootNodeID)
	if err != nil || len(addrs) != 1 {
		t.Fatalf("the site resolves the controller to %v, %v", addrs, err)
	}
	if got, want := addrs[0], c.Addr(); !got.IP.Equal(want.IP) || got.Port != want.Port {
		t.Fatalf("the site answers the controller at %v, it serves on %v", got, want)
	}
}
