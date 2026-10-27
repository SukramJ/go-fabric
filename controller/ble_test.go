// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller_test

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/controller/ble/bletest"
	"github.com/SukramJ/go-fabric/controller/commissioner"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"
)

// wifiDriver is a Wi-Fi driver that joins every network it is given.
type wifiDriver struct {
	mu       sync.Mutex
	connects []string
}

func (w *wifiDriver) ScanNetworks(_ context.Context, ssid []byte) (status uint8, debugText string, results []contract.WiFiScanResult) {
	return 0, "", []contract.WiFiScanResult{{SSID: bytes.Clone(ssid), BSSID: make([]byte, 6), Channel: 6}}
}

func (w *wifiDriver) ConnectNetwork(_ context.Context, ssid, credentials []byte) (status uint8, debugText string, errorValue int32) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.connects = append(w.connects, string(ssid)+"/"+string(credentials))
	return 0, "", 0
}

// TestPairOverBLE: PairOptions.BLE finds the site's Wi-Fi device by its
// BLE advertisement (an in-memory central, bletest), runs PASE over BTP,
// provisions PairOptions.WiFi through the network steps, and completes over
// CASE on the site's network.
func TestPairOverBLE(t *testing.T) {
	t.Parallel()
	drv := &wifiDriver{}
	site, dev, c := siteController(t, fabrictest.Topology{Configure: func(cfg *node.Config) {
		cfg.Network.WiFiCommissioning = node.WiFiCommissioning{Driver: drv}
	}}, nil)
	p := dev.BLE()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if _, err := c.Pair(ctx, manualCode(t, dev), controller.PairOptions{BLE: bletest.NewCentral(p)}); !errors.Is(err, controller.ErrConfiguration) {
		t.Fatalf("BLE without a network: %v, want ErrConfiguration", err)
	}
	if err := site.Run(ctx, func(ctx context.Context) error {
		_, err := c.Pair(ctx, manualCode(t, dev), controller.PairOptions{
			BLE: bletest.NewCentral(p), WiFi: &commissioner.WiFiNetwork{SSID: "lab", Credentials: "secret12"},
			OnAttestationFailure: attestation.Accept(),
		})
		return err
	}); err != nil {
		t.Fatalf("Pair over BLE: %v", err)
	}
	drv.mu.Lock()
	defer drv.mu.Unlock()
	if len(drv.connects) != 1 || drv.connects[0] != "lab/secret12" {
		t.Fatalf("driver connects %q", drv.connects)
	}
	if len(c.Nodes()) != 1 {
		t.Fatalf("%d nodes", len(c.Nodes()))
	}
}
