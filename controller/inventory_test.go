// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller_test

import (
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/secure/setup"
)

// TestInventoryKeepsThePairedCode: with Config.KeepOnboardingCodes the
// code Pair was called with is the node's inventory entry, as paired; a
// code of another device is refused and leaves it; the device's QR code
// replaces it with both forms; a cleared code is gone.
func TestInventoryKeepsThePairedCode(t *testing.T) {
	t.Parallel()
	_, dev, c, n := sitePair(t, fabrictest.Topology{}, func(cfg *controller.Config) { cfg.KeepOnboardingCodes = true })
	manual := manualCode(t, dev)
	inv := c.Inventory()
	if len(inv) != 1 {
		t.Fatalf("%d entries", len(inv))
	}
	e := inv[0]
	if e.Node != n || !e.HasCode || e.ManualCode != manual || e.QRCode != "" || e.Source != client.OnboardingPaired || e.StoredAt.IsZero() {
		t.Fatalf("paired entry: has %v manual %q qr %q source %d at %v", e.HasCode, e.ManualCode, e.QRCode, e.Source, e.StoredAt)
	}
	if addr, _ := n.PeerAddress(); e.NodeID != addr.NodeID || e.VendorID == 0 || e.VendorName == "" || e.ProductName == "" {
		t.Fatalf("identity: node %d vendor 0x%04X %q product %q", e.NodeID, e.VendorID, e.VendorName, e.ProductName)
	}

	other, err := setup.QRCode(setup.Payload{
		VendorID: e.VendorID + 1, ProductID: e.ProductID, Discriminator: dev.Discriminator(), Passcode: dev.Passcode(), DiscoveryCaps: setup.DiscoveryOnIP,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := n.SetOnboardingCode(other); !errors.Is(err, client.ErrOnboardingMismatch) {
		t.Fatalf("another vendor's code: %v", err)
	}
	if got := c.Inventory()[0]; got.ManualCode != manual || got.Source != client.OnboardingPaired {
		t.Fatalf("a refused code changed the entry: %q source %d", got.ManualCode, got.Source)
	}

	qr, err := setup.QRCode(setup.Payload{
		VendorID: e.VendorID, ProductID: e.ProductID, Discriminator: dev.Discriminator(), Passcode: dev.Passcode(), DiscoveryCaps: setup.DiscoveryOnIP,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := n.SetOnboardingCode(qr); err != nil {
		t.Fatal(err)
	}
	got := c.Inventory()[0]
	if got.QRCode != qr || got.ManualCode != manual || got.Source != client.OnboardingAttached || got.Checks&client.OnboardingCheckedVendorProduct == 0 {
		t.Fatalf("attached entry: qr %q manual %q source %d checks %b", got.QRCode, got.ManualCode, got.Source, got.Checks)
	}

	if err := n.ClearOnboardingCode(); err != nil {
		t.Fatal(err)
	}
	if got := c.Inventory()[0]; got.HasCode || got.QRCode != "" || got.ManualCode != "" {
		t.Fatalf("cleared entry still has a code")
	}
}

// TestInventoryWithoutKeeping: by default Pair keeps no code; the node is
// listed without one.
func TestInventoryWithoutKeeping(t *testing.T) {
	t.Parallel()
	_, _, c, n := sitePair(t, fabrictest.Topology{}, nil)
	inv := c.Inventory()
	if len(inv) != 1 || inv[0].Node != n || inv[0].HasCode || inv[0].ManualCode != "" {
		t.Fatalf("inventory without keeping: %+v", inv)
	}
	if _, ok := n.OnboardingCode(); ok {
		t.Fatal("a code was kept without Config.KeepOnboardingCodes")
	}
}
