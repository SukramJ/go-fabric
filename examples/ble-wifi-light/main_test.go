// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/controller/ble/bletest"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/secure/setup"
)

// joinAll is a Wi-Fi driver that finds and joins every network.
type joinAll struct{}

func (joinAll) ScanNetworks(context.Context, []byte) (status uint8, debugText string, results []contract.WiFiScanResult) {
	return 0, "", nil
}

func (joinAll) ConnectNetwork(context.Context, []byte, []byte) (status uint8, debugText string, errorValue int32) {
	return 0, "", 0
}

// TestNode builds the program's node with in-process radios — the
// in-memory BLE peripheral and a Wi-Fi driver that joins every network —
// on loopback: it mounts the Wi-Fi NetworkCommissioning, advertises its
// own window on the radio, and its QR code names BLE. The binary on real
// radios is driven by the ble-virtual job (script/ble-virtual-test.sh).
func TestNode(t *testing.T) {
	ctx := context.Background()
	o, err := parseFlags([]string{"--storage", filepath.Join(t.TempDir(), "light.db"), "--discriminator", "3001", "--passcode", "20202024"})
	if err != nil {
		t.Fatal(err)
	}
	p := &bletest.Peripheral{Address: "C0:FF:EE:00:00:01", Discriminator: 3001, VendorID: 0xFFF1, ProductID: 0x8001}
	radio := p.Radio()
	loopback := node.Network{Listen: "127.0.0.1:0", PreferIPv4: true, MDNS: node.MDNS{Off: true}}
	n, err := newNode(ctx, o, loopback, radio, joinAll{})
	if err != nil {
		t.Fatalf("newNode: %v", err)
	}
	if err := n.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = n.Stop(context.Background()) })
	if n.Root().WiFiNetworkCommissioning == nil || n.Root().NetworkCommissioning != nil {
		t.Error("the Wi-Fi NetworkCommissioning is not mounted")
	}
	// The own window goes on the radio from Start, asynchronously.
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for advertising := false; !advertising; {
		select {
		case <-deadline.C:
			t.Fatal("the own window is not advertised over BLE")
		case <-tick.C:
			_, advertising = radio.Advertising()
		}
	}
	sp, err := n.SetupPayload()
	if err != nil {
		t.Fatal(err)
	}
	qr, err := setup.DecodeQRCode(sp.QR)
	if err != nil {
		t.Fatal(err)
	}
	if qr.DiscoveryCaps&setup.DiscoveryBLE == 0 || qr.Discriminator != 3001 || qr.Passcode != 20202024 {
		t.Errorf("QR payload %+v", qr)
	}
}

// TestFlags: the passcode and the discriminator are bounded.
func TestFlags(t *testing.T) {
	for _, args := range [][]string{{"--passcode", "100000000"}, {"--discriminator", "4096"}, {"--log-level", "loud"}} {
		if _, err := parseFlags(args); err == nil {
			t.Errorf("parseFlags(%v) accepted", args)
		}
	}
	// The refusal names the bound, not the value (review II,
	// R2-P0-sec-E04): the text reaches a log.
	if _, err := parseFlags([]string{"--passcode", "202020211"}); err == nil || strings.Contains(err.Error(), "202020211") || !strings.Contains(err.Error(), "--passcode") {
		t.Errorf("parseFlags(--passcode 202020211) = %v, want a refusal that names the flag and not the value", err)
	}
	o, err := parseFlags(nil)
	if err != nil || o.adapter != "hci0" || o.wifiInterface != "wlan0" || o.passcode != 20202021 || o.discriminator != 3840 || o.logLevel != slog.LevelInfo {
		t.Errorf("defaults %+v, %v", o, err)
	}
}

// TestNodeWithoutWiFi: --no-wifi builds the node without a Wi-Fi driver —
// the Ethernet NetworkCommissioning in its place — still on the BLE radio,
// the variant the ble-virtual job runs where the runner has no Wi-Fi radio.
func TestNodeWithoutWiFi(t *testing.T) {
	ctx := context.Background()
	o, err := parseFlags([]string{"--storage", filepath.Join(t.TempDir(), "light.db"), "--no-wifi"})
	if err != nil || !o.noWiFi {
		t.Fatalf("parseFlags: %+v, %v", o, err)
	}
	p := &bletest.Peripheral{Address: "C0:FF:EE:00:00:02", Discriminator: 3840, VendorID: 0xFFF1, ProductID: 0x8001}
	loopback := node.Network{Listen: "127.0.0.1:0", PreferIPv4: true, MDNS: node.MDNS{Off: true}}
	n, err := newNode(ctx, o, loopback, p.Radio(), nil)
	if err != nil {
		t.Fatalf("newNode: %v", err)
	}
	if err := n.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = n.Stop(context.Background()) })
	if n.Root().WiFiNetworkCommissioning != nil || n.Root().NetworkCommissioning == nil {
		t.Error("without a Wi-Fi driver the Ethernet NetworkCommissioning is not mounted in place of the Wi-Fi one")
	}
}
