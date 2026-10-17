// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/node"
)

func TestWiFiSim(t *testing.T) {
	s := wifiSim{delay: time.Millisecond}
	if st, _, res := s.ScanNetworks(context.Background(), []byte("lab")); st != 0 || len(res) != 1 || string(res[0].SSID) != "lab" {
		t.Fatalf("directed scan %d %v", st, res)
	}
	if _, _, res := s.ScanNetworks(context.Background(), nil); len(res) != 0 {
		t.Fatalf("undirected scan %v", res)
	}
	if st, _, _ := s.ConnectNetwork(context.Background(), []byte("lab"), nil); st != 0 {
		t.Fatalf("connect %d", st)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if st, _, _ := (wifiSim{delay: time.Hour}).ConnectNetwork(ctx, []byte("lab"), nil); st != 12 {
		t.Fatalf("canceled connect %d", st)
	}
	var cfg node.Config
	applyTestWiFi(&cfg, flags{wifiSim: true}, nil)
	if cfg.Network.WiFiCommissioning.Driver == nil {
		t.Fatal("--wifi-sim mounts no driver")
	}
}
