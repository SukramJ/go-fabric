// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"bytes"
	"context"
	"strings"
	"time"

	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/internal/nodeseam"
	"github.com/SukramJ/go-fabric/node"
)

// --wifi-sim (TESTING ONLY): the root's NetworkCommissioning gets the
// Wi-Fi feature instead of Ethernet, so the commissioner's network steps
// (AddOrUpdateWiFiNetwork, ConnectNetwork) can be run against a daemon
// without a radio: `reference-controller pair --passcode … --discriminator
// … --wifi-ssid zigbeehome --wifi-credentials password123`. The driver
// knows the --wifi-sim-ssid networks (default zigbeehome with
// password123, the CHIP harness's): a scan without an SSID finds them
// all, a scan for one of them finds that one, a scan for any other SSID
// is NetworkNotFound; a connect to a known network succeeds after a short
// delay with its passphrase and is AuthFailure with another, a connect to
// an unknown SSID is NetworkNotFound. The daemon stays on its Ethernet
// address, where the commissioner reaches it over CASE afterwards.
type wifiSim struct {
	// delay is how long a connect takes.
	delay time.Duration
	// networks are the access points in range, in --wifi-sim-ssid order.
	networks []simNetwork
}

// simNetwork is one simulated access point; an empty passphrase accepts
// any.
type simNetwork struct {
	ssid, passphrase []byte
}

var _ contract.WiFiDriver = wifiSim{}

// wifiSimConnectDelay is the simulated association time.
const wifiSimConnectDelay = 200 * time.Millisecond

// defaultWiFiSimSSIDs is --wifi-sim-ssid's default: the network the CHIP
// harness commissions the DUT onto (internal/chiptool wifiCaseArgs).
const defaultWiFiSimSSIDs = "zigbeehome:password123"

// NetworkCommissioningStatusEnum values the simulated driver answers.
const (
	simStatusSuccess         = 0
	simStatusNetworkNotFound = 5
	simStatusAuthFailure     = 7
	simStatusUnknownError    = 12
)

// parseWiFiSimSSIDs reads --wifi-sim-ssid: comma-separated ssid[:passphrase].
func parseWiFiSimSSIDs(v string) []simNetwork {
	var out []simNetwork
	for e := range strings.SplitSeq(v, ",") {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		ssid, pass, _ := strings.Cut(e, ":")
		out = append(out, simNetwork{ssid: []byte(ssid), passphrase: []byte(pass)})
	}
	return out
}

// applyTestWiFi wires the simulated Wi-Fi link (--wifi, wifi.go) and the
// simulated Wi-Fi commissioning (--wifi-sim), both TESTING ONLY.
func applyTestWiFi(cfg *node.Config, f flags, devices *fleet) {
	if f.wifi {
		cfg.Network.WiFi = devices.wifi.nodeConfig()
	}
	if f.wifiSim {
		nodeseam.UseWiFiCommissioning(cfg, wifiSim{delay: wifiSimConnectDelay, networks: parseWiFiSimSSIDs(f.wifiSimSSIDs)})
	}
}

// find is the network with that SSID, or nil.
func (s wifiSim) find(ssid []byte) *simNetwork {
	for i := range s.networks {
		if bytes.Equal(s.networks[i].ssid, ssid) {
			return &s.networks[i]
		}
	}
	return nil
}

// ScanNetworks implements contract.WiFiDriver: every known network for an
// undirected scan, the one named, or NetworkNotFound.
func (s wifiSim) ScanNetworks(_ context.Context, ssid []byte) (status uint8, debugText string, results []contract.WiFiScanResult) {
	for i, n := range s.networks {
		if len(ssid) > 0 && !bytes.Equal(n.ssid, ssid) {
			continue
		}
		results = append(results, contract.WiFiScanResult{
			Security: 0x08, // WPA2-Personal
			SSID:     bytes.Clone(n.ssid),
			BSSID:    []byte{0x02, 0x00, 0x00, 0x00, 0x00, byte(i + 1)},
			Channel:  6, Band: 0, HasBand: true, RSSI: -40, HasRSSI: true,
		})
	}
	if len(results) == 0 {
		// No DebugText: chip's Linux driver gives none, and TC-CNET-4.4's
		// scan_and_check reads a non-empty one through a misspelt field
		// (TC_CNET_4_4.py:88-89, scan_results.debug_text).
		return simStatusNetworkNotFound, "", nil
	}
	return simStatusSuccess, "", results
}

// ConnectNetwork implements contract.WiFiDriver: a known network with its
// passphrase joins after the delay.
func (s wifiSim) ConnectNetwork(ctx context.Context, ssid, credentials []byte) (status uint8, debugText string, errorValue int32) {
	n := s.find(ssid)
	if n == nil {
		return simStatusNetworkNotFound, "no such network in range", 0
	}
	t := time.NewTimer(s.delay)
	defer t.Stop()
	select {
	case <-t.C:
	case <-ctx.Done():
		return simStatusUnknownError, "connect canceled", 0
	}
	if len(n.passphrase) > 0 && !bytes.Equal(n.passphrase, credentials) {
		return simStatusAuthFailure, "wrong passphrase", 15 // IEEE 802.11 reason 15: 4-way handshake timeout
	}
	return simStatusSuccess, "", 0
}
