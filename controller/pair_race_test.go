// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller_test

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/controller/ble/bletest"
	"github.com/SukramJ/go-fabric/controller/commissioner"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"
)

// TestPairRaceOverBLEAndIP: with PairOptions.BLE a manual code — which
// names no transport — is searched for over DNS-SD and BLE at once, the
// device answers on both, and the first transport to establish PASE wins
// (matter.js CommissioningDiscovery over ParallelPaseDiscovery,
// packages/node/src/behavior/system/controller/discovery/ at 862ad75f).
// Each transport reports its sub-step of Discovery 2.1: started, then
// succeeded for the winner and skipped for the other; PASE 2.2 is reported
// once, and the node is commissioned once.
func TestPairRaceOverBLEAndIP(t *testing.T) {
	t.Parallel()
	drv := &wifiDriver{}
	site, dev, c := siteController(t, fabrictest.Topology{Configure: func(cfg *node.Config) {
		cfg.Network.WiFiCommissioning = node.WiFiCommissioning{Driver: drv}
	}}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	l := &progressLog{}
	if err := site.Run(ctx, func(ctx context.Context) error {
		_, err := c.Pair(ctx, manualCode(t, dev), controller.PairOptions{
			BLE: bletest.NewCentral(dev.BLE()), WiFi: &commissioner.WiFiNetwork{SSID: "lab", Credentials: "secret12"},
			OnAttestationFailure: attestation.Accept(), Progress: l.add,
		})
		return err
	}); err != nil {
		t.Fatalf("Pair: %v", err)
	}
	got := l.lines()
	count := func(line string) int {
		n := 0
		for _, g := range got {
			if g == line {
				n++
			}
		}
		return n
	}
	for _, line := range []string{
		"2.1 Discovery started", "2.1 Discovery succeeded",
		"2.1 Discovery.IP started", "2.1 Discovery.BLE started",
		"2.2 PASE started", "2.2 PASE succeeded", "21.1 Subscribe succeeded",
	} {
		if n := count(line); n != 1 {
			t.Fatalf("%q reported %d times in %q", line, n, got)
		}
	}
	won, skipped := 0, 0
	for _, g := range got {
		if strings.HasPrefix(g, "2.1 Discovery.") {
			switch {
			case strings.HasSuffix(g, " succeeded"):
				won++
			case strings.HasSuffix(g, " skipped"):
				skipped++
			}
		}
	}
	if won != 1 || skipped != 1 {
		t.Fatalf("transport outcomes %q: want one succeeded, one skipped", got)
	}
	if i, j := slices.Index(got, "2.2 PASE succeeded"), slices.Index(got, "0.1 GetInitialData started"); i < 0 || j < i {
		t.Fatalf("PASE succeeded must precede the flow: %q", got)
	}
	if len(c.Nodes()) == 0 {
		t.Fatal("no node")
	}
}
