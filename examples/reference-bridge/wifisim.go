// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"bytes"
	"context"
	"time"

	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/node"
)

// --wifi-sim (TESTING ONLY): the root's NetworkCommissioning gets the
// Wi-Fi feature instead of Ethernet, so the commissioner's network steps
// (AddOrUpdateWiFiNetwork, ConnectNetwork) can be run against a daemon
// without a radio: `reference-controller pair --passcode … --discriminator
// … --wifi-ssid lab --wifi-credentials secret12`. The driver "connects" to
// any SSID after a short delay and the daemon stays on its Ethernet
// address, where the commissioner reaches it over CASE afterwards. A
// scan for an SSID finds that one network; an undirected scan finds the
// SSIDs added so far, or nothing.
type wifiSim struct {
	// delay is how long a connect takes.
	delay time.Duration
}

var _ contract.WiFiDriver = wifiSim{}

// wifiSimConnectDelay is the simulated association time.
const wifiSimConnectDelay = 200 * time.Millisecond

// applyTestWiFi wires the simulated Wi-Fi link (--wifi, wifi.go) and the
// simulated Wi-Fi commissioning (--wifi-sim), both TESTING ONLY.
func applyTestWiFi(cfg *node.Config, f flags, devices *fleet) {
	if f.wifi {
		cfg.Network.WiFi = devices.wifi.nodeConfig()
	}
	if f.wifiSim {
		cfg.Network.WiFiCommissioning = node.WiFiCommissioning{Driver: wifiSim{delay: wifiSimConnectDelay}}
	}
}

// ScanNetworks implements contract.WiFiDriver.
func (s wifiSim) ScanNetworks(_ context.Context, ssid []byte) (status uint8, debugText string, results []contract.WiFiScanResult) {
	if len(ssid) == 0 {
		return 0, "", nil
	}
	return 0, "", []contract.WiFiScanResult{{
		Security: 0x08, // WPA2-Personal
		SSID:     bytes.Clone(ssid),
		BSSID:    []byte{0x02, 0x00, 0x00, 0x00, 0x00, 0x01},
		Channel:  6, Band: 0, HasBand: true, RSSI: -40, HasRSSI: true,
	}}
}

// ConnectNetwork implements contract.WiFiDriver: success after the delay.
func (s wifiSim) ConnectNetwork(ctx context.Context, _, _ []byte) (status uint8, debugText string, errorValue int32) {
	t := time.NewTimer(s.delay)
	defer t.Stop()
	select {
	case <-t.C:
		return 0, "", 0
	case <-ctx.Done():
		return 12, "connect canceled", 0 // UnknownError
	}
}
