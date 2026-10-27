// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest_test

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/controller/ble/bletest"
	"github.com/SukramJ/go-fabric/controller/commissioner"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/secure/setup"
)

// joinAll is a Wi-Fi driver that joins every network.
type joinAll struct{}

func (joinAll) ScanNetworks(_ context.Context, ssid []byte) (status uint8, debugText string, results []contract.WiFiScanResult) {
	return 0, "", []contract.WiFiScanResult{{SSID: bytes.Clone(ssid), BSSID: make([]byte, 6), Channel: 6}}
}

func (joinAll) ConnectNetwork(context.Context, []byte, []byte) (status uint8, debugText string, errorValue int32) {
	return 0, "", 0
}

// TestDeviceBLE: the device's peripheral advertises its commissionable
// payload, and a controller commissions the device through it — PASE over
// BTP on the in-memory BLE model, the rest on the site's network.
func TestDeviceBLE(t *testing.T) {
	t.Parallel()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	dev := site.AddDevice(fabrictest.Topology{Configure: func(cfg *node.Config) {
		cfg.Network.WiFiCommissioning = node.WiFiCommissioning{Driver: joinAll{}}
	}})
	p := dev.BLE()
	if p.Address != fmt.Sprintf("C0:FF:EE:00:00:%02X", dev.Index()) || p.Discriminator != dev.Discriminator() || p.VendorID != 0xFFF1 || p.ProductID != 0x8001 {
		t.Fatalf("peripheral = %+v", p)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var c *controller.Controller
	if err := site.Run(ctx, func(ctx context.Context) (err error) {
		c, err = controller.New(ctx, site.ControllerConfig(fabrictest.ControllerOptions{}))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = site.Run(context.Background(), c.Close) })
	code, err := setup.ManualCode(dev.Discriminator(), dev.Passcode())
	if err != nil {
		t.Fatal(err)
	}
	if err := site.Run(ctx, func(ctx context.Context) error {
		_, err := c.Pair(ctx, code, controller.PairOptions{
			BLE: bletest.NewCentral(p), WiFi: &commissioner.WiFiNetwork{SSID: "lab", Credentials: "secret12"},
			OnAttestationFailure: attestation.Accept(),
		})
		return err
	}); err != nil {
		t.Fatalf("Pair over BLE: %v", err)
	}
	if len(c.Nodes()) != 1 {
		t.Fatalf("%d nodes", len(c.Nodes()))
	}
}
