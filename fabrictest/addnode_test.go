// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest_test

import (
	"context"
	"testing"
	"time"

	basicinfo "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"
)

// daemonConfig is a node configuration as a daemon composes it: its own
// passcode, discriminator, product and labels, an mDNS listen address and
// TCP the site replaces.
func daemonConfig() node.Config {
	return node.Config{
		Network: node.Network{Listen: "[::]:5540"},
		Commissioning: node.Commissioning{
			Passcode: 34567890, Discriminator: 1234, PBKDFIterations: 1000, Salt: []byte("SPAKE2P Key Salt daemon"),
		},
		ProductDescription: node.ProductDescription{VendorID: 0xFFF2, ProductID: 0x8002},
		BasicInformation: node.BasicInformation{
			VendorName: "daemon vendor", ProductName: "daemon bridge", NodeLabel: "the daemon", SerialNumber: "D-1",
			HardwareVersion: 1, HardwareVersionString: "1", SoftwareVersion: 2, SoftwareVersionString: "2",
		},
	}
}

// TestAddNode: a daemon's own node.Config runs on the site — commissioned
// by its passcode and discriminator, reading its NodeLabel, serving a
// device added through its Aggregator, and again after a restart.
func TestAddNode(t *testing.T) {
	t.Parallel()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	dev := site.AddNode(daemonConfig())
	if dev.Passcode() != 34567890 || dev.Discriminator() != 1234 {
		t.Fatalf("passcode %d, discriminator %d", dev.Passcode(), dev.Discriminator())
	}
	plug, err := device.NewOnOffPlugInUnit(device.Options{StableKey: device.StringKey("plug"), Name: "plug"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := dev.Act(func(n *node.Node) error { return n.Aggregator().Add(ctx, plug) }); err != nil {
		t.Fatal(err)
	}
	c := site.AddController(fabrictest.ControllerOptions{})
	n, err := c.CommissionNode(ctx, dev, client.CommissionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	read := func() string {
		var rep *client.Report
		if err := site.Run(ctx, func(ctx context.Context) (err error) {
			var req client.ReadRequest
			req.Attributes = []client.AttributePath{client.AttributeAt(0, basicinfo.ClusterID, basicinfo.AttrNodeLabel)}
			rep, err = n.Read(ctx, req)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		v, _ := rep.Value(0, basicinfo.ClusterID, basicinfo.AttrNodeLabel)
		return v.El.String
	}
	if got := read(); got != "the daemon" {
		t.Fatalf("NodeLabel %q", got)
	}
	if _, ok := plug.EndpointNumber(); !ok {
		t.Fatal("the plug was not assembled")
	}
	if err := dev.Restart(ctx); err != nil {
		t.Fatal(err)
	}
	if got := read(); got != "the daemon" {
		t.Fatalf("NodeLabel after a restart %q", got)
	}
}
