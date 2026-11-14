// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"bytes"
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	netcommdef "github.com/SukramJ/go-fabric/cluster/spec/networkcommissioning"
	"github.com/SukramJ/go-fabric/contract"
)

// crowdedScanDriver answers a scan with n access points; the i-th has
// the RSSI rssi(i), and none when rssi is nil.
type crowdedScanDriver struct {
	n    int
	rssi func(i int) (int8, bool)
}

func (d crowdedScanDriver) ScanNetworks(_ context.Context, ssid []byte) (status uint8, debugText string, results []contract.WiFiScanResult) {
	for i := range d.n {
		name := ssid
		if len(name) == 0 {
			name = []byte("neighbour-network-with-a-long-ss")
		}
		r := contract.WiFiScanResult{Security: 4, SSID: bytes.Clone(name), BSSID: []byte{0x02, 0, 0, 0, byte(i >> 8), byte(i)}, Channel: 6, HasBand: true}
		if d.rssi != nil {
			r.RSSI, r.HasRSSI = d.rssi(i)
		}
		results = append(results, r)
	}
	return 0, "", results
}

func (crowdedScanDriver) ConnectNetwork(context.Context, []byte, []byte) (status uint8, debugText string, errorValue int32) {
	return 0, "", 0
}

// TestScanNetworksResponseIsBounded: a ScanNetworksResponse carries at
// most connectedhomeip's kMaxNetworksInScanResponse results
// (src/app/clusters/network-commissioning/constants.h:26-28 at 6170af84,
// "each item will cost ~60 bytes in TLV, thus 15 is a safe upper bound";
// WifiScanResponse.cpp:65-69 stops encoding there), for an undirected and
// a directed scan alike, and keeps the strongest signals, those without a
// known RSSI last (review II, R2-P3-net-04).
func TestScanNetworksResponseIsBounded(t *testing.T) {
	t.Parallel()
	const chipMax = 15
	// Every third access point has no RSSI; the others grow stronger
	// with the index, so the strongest are at the end of the driver's
	// list.
	rssi := func(i int) (int8, bool) {
		if i%3 == 0 {
			return 0, false
		}
		return int8(-90 + i), true
	}
	for _, directed := range []bool{false, true} {
		w, err := NewWiFiNetworkCommissioning(WiFiNetworkCommissioningConfig{Driver: crowdedScanDriver{n: 40, rssi: rssi}})
		if err != nil {
			t.Fatal(err)
		}
		req := netcommdef.ScanNetworksRequest{}
		if directed {
			req.Ssid = &spec.Nullable[[]byte]{Value: []byte("home")}
		}
		r, err := w.MatterInvoke(t.Context(), netcommdef.CmdScanNetworks, req)
		if err != nil {
			t.Fatal(err)
		}
		resp := r.(netcommdef.ScanNetworksResponse)
		if resp.WiFiScanResults == nil {
			t.Fatal("no results")
		}
		list := *resp.WiFiScanResults
		if len(list) != chipMax {
			t.Fatalf("directed=%v: ScanNetworksResponse carries %d results of the 40 access points in range, want %d", directed, len(list), chipMax)
		}
		for k, e := range list {
			// The 26 access points with an RSSI are i%3 != 0; the 15
			// strongest of them are the last 15, strongest first.
			if e.Rssi == nil {
				t.Fatalf("directed=%v: result %d has no RSSI while %d results with one were found", directed, k, 26)
			}
			if k > 0 && *list[k-1].Rssi < *e.Rssi {
				t.Errorf("directed=%v: result %d (RSSI %d) is stronger than result %d (RSSI %d)", directed, k, *e.Rssi, k-1, *list[k-1].Rssi)
			}
		}
		if got := *list[chipMax-1].Rssi; got != -90+17 {
			t.Errorf("directed=%v: the weakest result kept has RSSI %d, want %d (the 15th strongest)", directed, got, -90+17)
		}
	}
}
