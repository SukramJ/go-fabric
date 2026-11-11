// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/controller/ble/bletest"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/secure/setup"
)

// The node's BLE radio follows matter.js's uncommissioned mode
// (ServerNetworkRuntime.ts:255-275, :294-308 at db021da0): BLE while the
// node has no fabric, off once it has one — at start, and for any window
// an administrator or the host opens — and back with the own window when
// the last fabric is removed.

func bleCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)
	return ctx
}

// radioState waits, moving the site's clock, until r is on the radio
// (serving and advertising) or off it, as on says.
func radioState(t *testing.T, site *fabrictest.Site, r *bletest.Radio, on bool, what string) {
	t.Helper()
	if err := site.Eventually(bleCtx(t), what, func() bool {
		serving, _ := r.Serving()
		_, advertising := r.Advertising()
		return serving == on && advertising == on
	}); err != nil {
		serving, _ := r.Serving()
		_, advertising := r.Advertising()
		t.Fatalf("%v: serving %v, advertising %v, want %v", err, serving, advertising, on)
	}
}

func TestNodeBLEFollowsCommissioning(t *testing.T) {
	site := fabrictest.NewSite(t, fabrictest.Options{Logger: slog.New(slog.DiscardHandler)})
	pair := site.UncommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{BLE: true}})
	dev := pair.Device
	r := dev.BLE().Radio()

	// Uncommissioned: the own window is on BLE, the configured payload.
	radioState(t, site, r, true, "the uncommissioned node on BLE")
	ads, _ := r.Advertisements()
	if len(ads) == 0 {
		t.Fatal("no advertisement")
	}
	if got := dev.BLE().ServiceData(); len(got) != 8 || got[1] != byte(dev.Discriminator()) || got[2] != byte(dev.Discriminator()>>8) {
		t.Fatalf("service data %x, want discriminator %d", got, dev.Discriminator())
	}

	// CommissioningComplete (here over IP) takes the node off the radio.
	cn, err := pair.Controller.CommissionNode(bleCtx(t), dev, client.CommissionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	radioState(t, site, r, false, "the commissioned node off BLE")
	adsAfter, stops := r.Advertisements()

	// A commissioned node never touches the radio: not at start ...
	if err := dev.Restart(bleCtx(t)); err != nil {
		t.Fatal(err)
	}
	// ... not for an administrator's window ...
	if err := site.Eventually(bleCtx(t), "the default subscription", cn.SubscriptionActive); err != nil {
		t.Fatal(err)
	}
	if err := site.Run(bleCtx(t), func(ctx context.Context) error {
		_, err := cn.OpenCommissioningWindow(ctx, 3*time.Minute, client.WindowOptions{})
		return err
	}); err != nil {
		t.Fatalf("OpenCommissioningWindow: %v", err)
	}
	if !dev.Node().Window().Open {
		t.Fatal("the administrator's window is not open")
	}
	// ... nor for the own window the host opens.
	if err := dev.Act(func(n *node.Node) error {
		if err := n.CloseWindow(context.Background()); err != nil {
			return err
		}
		return n.OpenOwnWindow(context.Background(), 3*time.Minute)
	}); err != nil {
		t.Fatal(err)
	}
	// The window's transition hook runs inside OpenOwnWindow: nothing
	// that would start BLE is still pending.
	if serving, _ := r.Serving(); serving {
		t.Fatal("the commissioned node serves on the radio")
	}
	if ads, s := r.Advertisements(); len(ads) != len(adsAfter) || s != stops {
		t.Fatalf("the commissioned node advertised: %d advertisements, %d stops (was %d, %d)", len(ads), s, len(adsAfter), stops)
	}

	// Removing the last fabric brings the own window back on BLE.
	if err := dev.Act(func(n *node.Node) error {
		if err := n.CloseWindow(context.Background()); err != nil {
			return err
		}
		return n.RemoveFabric(context.Background(), 1)
	}); err != nil {
		t.Fatal(err)
	}
	radioState(t, site, r, true, "the decommissioned node on BLE")
}

// TestNodeSetupPayloadBLEBit: the QR code names BLE discovery beside the
// IP network only for a node with a radio (matter.js NetworkServer.ts:36-51,
// CommissioningServer.ts:430-443).
func TestNodeSetupPayloadBLEBit(t *testing.T) {
	site := fabrictest.NewSite(t, fabrictest.Options{Logger: slog.New(slog.DiscardHandler)})
	for _, tc := range []struct {
		ble  bool
		want setup.DiscoveryCaps
	}{
		{false, setup.DiscoveryOnIP},
		{true, setup.DiscoveryOnIP | setup.DiscoveryBLE},
	} {
		dev := site.AddDevice(fabrictest.Topology{BLE: tc.ble})
		sp, err := dev.Node().SetupPayload()
		if err != nil {
			t.Fatal(err)
		}
		qr, _, err := dev.Node().PairingCodesFor(node.WindowCredentials{Passcode: 20202021, Discriminator: 1234})
		if err != nil {
			t.Fatal(err)
		}
		for _, code := range []string{sp.QR, qr} {
			p, err := setup.DecodeQRCode(code)
			if err != nil {
				t.Fatal(err)
			}
			if p.DiscoveryCaps != tc.want {
				t.Fatalf("BLE %v: discovery capabilities %#x, want %#x", tc.ble, p.DiscoveryCaps, tc.want)
			}
		}
	}
}
