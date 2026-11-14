// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest_test

import (
	"context"
	"testing"

	netcomm "github.com/SukramJ/go-fabric/cluster/spec/networkcommissioning"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
)

// crowdedWiFi is a Wi-Fi driver whose scan finds n access points with
// 32-byte SSIDs, as wifi/wpasupplicant returns every BSS wpa_supplicant
// lists.
type crowdedWiFi struct{ n int }

func (d crowdedWiFi) ScanNetworks(_ context.Context, _ []byte) (status uint8, debugText string, results []contract.WiFiScanResult) {
	for i := range d.n {
		results = append(results, contract.WiFiScanResult{
			Security: 4, SSID: []byte("neighbour-network-with-a-long-ss"), BSSID: []byte{0x02, 0, 0, 0, 0, byte(i)},
			Channel: 6, HasBand: true, RSSI: -60, HasRSSI: true,
		})
	}
	return 0, "", results
}

func (crowdedWiFi) ConnectNetwork(context.Context, []byte, []byte) (status uint8, debugText string, errorValue int32) {
	return 0, "", 0
}

// TestScanNetworksAnswersInACrowdedBand: ScanNetworks is answered with a
// ScanNetworksResponse whatever the number of access points in range —
// the response carries a bounded number of results, so it fits its
// message (review II, R2-P3-net-04).
func TestScanNetworksAnswersInACrowdedBand(t *testing.T) {
	site := fabrictest.NewSite(t, fabrictest.Options{})
	p := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Configure: func(cfg *node.Config) {
		cfg.Network.WiFi = &node.WiFi{Driver: crowdedWiFi{n: 40}}
	}}})
	ctx := testContext(t)
	res, err := fabrictest.Invoke(ctx, p, im.ConcreteCommandPath{Endpoint: 0, Cluster: netcomm.ClusterID, Command: netcomm.CmdScanNetworks}, netcomm.ScanNetworksRequest{})
	if err != nil {
		t.Fatalf("ScanNetworks with 40 access points in range: %v, want a ScanNetworksResponse", err)
	}
	if res.Status != im.StatusSuccess {
		t.Fatalf("ScanNetworks with 40 access points in range: status %v, want a ScanNetworksResponse", res.Status)
	}
}
