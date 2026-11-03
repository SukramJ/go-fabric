// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/node"
)

// TestWiFiSim holds the simulated driver to the scan and connect rules
// TC-CNET-4.4 (scan_and_check, the random-SSID scan) and TC-CNET-4.23
// (wrong SSID, wrong passphrase) read.
func TestWiFiSim(t *testing.T) {
	s := wifiSim{delay: time.Millisecond, networks: parseWiFiSimSSIDs(" lab:secret12, open ,,zigbeehome:password123")}
	if len(s.networks) != 3 {
		t.Fatalf("parsed %d networks, want 3", len(s.networks))
	}
	if st, _, res := s.ScanNetworks(context.Background(), []byte("lab")); st != simStatusSuccess || len(res) != 1 || string(res[0].SSID) != "lab" {
		t.Fatalf("directed scan %d %v", st, res)
	}
	if st, _, res := s.ScanNetworks(context.Background(), nil); st != simStatusSuccess || len(res) != 3 || string(res[2].SSID) != "zigbeehome" || res[0].BSSID[5] == res[1].BSSID[5] {
		t.Fatalf("undirected scan %d %v", st, res)
	}
	if st, _, res := s.ScanNetworks(context.Background(), []byte("IncorrectSSID_12345")); st != simStatusNetworkNotFound || len(res) != 0 {
		t.Fatalf("scan for an unknown SSID %d %v", st, res)
	}
	if st, _, _ := (wifiSim{}).ScanNetworks(context.Background(), nil); st != simStatusNetworkNotFound {
		t.Fatalf("undirected scan without networks %d", st)
	}
	for _, c := range []struct {
		ssid, pass string
		want       uint8
	}{
		{"lab", "secret12", simStatusSuccess},
		{"lab", "IncorrectPassword123", simStatusAuthFailure},
		{"open", "anything", simStatusSuccess},
		{"IncorrectSSID_12345", "secret12", simStatusNetworkNotFound},
	} {
		if st, _, _ := s.ConnectNetwork(context.Background(), []byte(c.ssid), []byte(c.pass)); st != c.want {
			t.Errorf("connect %s/%s: %d, want %d", c.ssid, c.pass, st, c.want)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if st, _, _ := (wifiSim{delay: time.Hour, networks: s.networks}).ConnectNetwork(ctx, []byte("lab"), []byte("secret12")); st != simStatusUnknownError {
		t.Fatalf("canceled connect %d", st)
	}
	if d := parseWiFiSimSSIDs(defaultWiFiSimSSIDs); len(d) != 1 || string(d[0].ssid) != "zigbeehome" || string(d[0].passphrase) != "password123" {
		t.Fatalf("default networks %v", d)
	}
	n := testNodeWith(t, newFleet(), filepath.Join(t.TempDir(), "wifisim.db"), func(cfg *node.Config) {
		applyTestWiFi(cfg, flags{wifiSim: true, wifiSimSSIDs: defaultWiFiSimSSIDs}, nil)
	})
	if n.Root().WiFiNetworkCommissioning == nil {
		t.Fatal("--wifi-sim mounts no Wi-Fi NetworkCommissioning")
	}
}
