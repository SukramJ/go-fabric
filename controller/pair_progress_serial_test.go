// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/controller/ble/bletest"
	"github.com/SukramJ/go-fabric/controller/commissioner"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"
)

// TestPairRaceProgressRunsOneCallAtATime (review II, R2-P6-doc-2313): in a
// discovery race over DNS-SD and BLE the Progress hook is called from each
// transport's search and each attempt, one call at a time. Before, the
// race called it from those goroutines unserialised, and two reports
// overlapped.
func TestPairRaceProgressRunsOneCallAtATime(t *testing.T) {
	t.Parallel()
	drv := &wifiDriver{}
	site, dev, c := siteController(t, fabrictest.Topology{BLE: true, Configure: func(cfg *node.Config) {
		cfg.Network.WiFi = &node.WiFi{Driver: drv}
	}}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var inside, overlapped atomic.Int32
	progress := func(controller.PairingStep) {
		if inside.Add(1) > 1 {
			overlapped.Add(1)
		}
		// Hold the call a little, so a concurrent one would meet it.
		for start := time.Now(); time.Since(start) < 2*time.Millisecond; {
		}
		inside.Add(-1)
	}
	if err := site.Run(ctx, func(ctx context.Context) error {
		_, err := c.Pair(ctx, manualCode(t, dev), controller.PairOptions{
			BLE: bletest.NewCentral(dev.BLE()), WiFi: &commissioner.WiFiNetwork{SSID: "lab", Credentials: "secret12"},
			OnAttestationFailure: attestation.Accept(), Progress: progress,
		})
		return err
	}); err != nil {
		t.Fatalf("Pair: %v", err)
	}
	if n := overlapped.Load(); n != 0 {
		t.Fatalf("%d Progress calls overlapped another", n)
	}
}
