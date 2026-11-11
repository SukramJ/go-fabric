// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/mdns"
)

// countingAdvertiser counts the node's on-demand re-announcements.
type countingAdvertiser struct {
	*mdns.Noop
	triggers atomic.Int32
}

func (a *countingAdvertiser) TriggerReannounce(context.Context) { a.triggers.Add(1) }

// fixedWiFi answers every connect with one status.
type fixedWiFi struct{ status uint8 }

func (fixedWiFi) ScanNetworks(context.Context, []byte) (status uint8, debugText string, results []contract.WiFiScanResult) {
	return 0, "", nil
}

func (w fixedWiFi) ConnectNetwork(context.Context, []byte, []byte) (status uint8, debugText string, errorValue int32) {
	if w.status != 0 {
		return w.status, "refused", 7
	}
	return 0, "", 0
}

// TestReannouncingDriver: the driver the node hands the Wi-Fi
// NetworkCommissioning server re-announces after a successful join and
// only then, and passes the host driver's answer through unchanged.
func TestReannouncingDriver(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		status uint8
		want   int32
	}{
		{"success", 0, 1},
		{"OutOfRange", 1, 0},
		{"AuthFailure", 7, 0},
		{"UnknownError", 12, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			adv := &countingAdvertiser{Noop: mdns.NewNoop()}
			n := &Node{advertiser: adv, cfg: Config{Network: Network{WiFi: &WiFi{Driver: fixedWiFi{status: tc.status}}}}}
			drv := n.wifiDriver()
			if drv == nil {
				t.Fatal("no driver for a WiFi with a Driver")
			}
			status, _, errValue := drv.ConnectNetwork(t.Context(), []byte("lab"), []byte("secret12"))
			if status != tc.status || (tc.status != 0) != (errValue == 7) {
				t.Errorf("ConnectNetwork = %d, %d; want the host driver's answer", status, errValue)
			}
			if got := adv.triggers.Load(); got != tc.want {
				t.Errorf("re-announced %d times, want %d", got, tc.want)
			}
		})
	}
	n := &Node{cfg: Config{Network: Network{WiFi: &WiFi{Reporter: nil}}}}
	if n.wifiDriver() != nil {
		t.Error("a driver without Network.WiFi.Driver")
	}
}
