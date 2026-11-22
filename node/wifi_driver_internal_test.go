// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"
	"sync/atomic"
	"testing"

	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/mdns"
)

// countingAdvertiser counts the node's on-demand re-announcements.
type countingAdvertiser struct {
	*mdns.Noop
	triggers atomic.Int32
}

func (a *countingAdvertiser) TriggerReannounce(context.Context) { a.triggers.Add(1) }

// fullWiFi implements contract.WiFiDriver and every optional port.
type fullWiFi struct{}

func (fullWiFi) ScanNetworks(context.Context, []byte) (status uint8, debugText string, results []contract.WiFiScanResult) {
	return 0, "", nil
}

func (fullWiFi) ConnectNetwork(context.Context, []byte, []byte) (status uint8, debugText string, errorValue int32) {
	return 0, "", 0
}
func (fullWiFi) CurrentNetwork(context.Context) ([]byte, bool)            { return nil, false }
func (fullWiFi) SetInterfaceEnabled(context.Context, bool) error          { return nil }
func (fullWiFi) WatchLink(context.Context, func(contract.WiFiLinkChange)) {}
func (fullWiFi) DisconnectNetwork(context.Context) error                  { return nil }

// TestWiFiDriverHandedOverUnwrapped (review II, R2-P6-api-2450): the node
// hands the Wi-Fi NetworkCommissioning server the host's driver itself,
// so the server finds every optional port the driver implements — the
// link watch and the disconnect among them, which the wrapper before hid.
func TestWiFiDriverHandedOverUnwrapped(t *testing.T) {
	t.Parallel()
	var host contract.WiFiDriver = fullWiFi{}
	n := &Node{cfg: Config{Network: Network{WiFi: &WiFi{Driver: host}}}}
	drv := n.wifiDriver()
	if drv != host {
		t.Fatalf("the node hands the server %T, want the host's driver", drv)
	}
	_, watcher := drv.(contract.WiFiLinkWatcher)
	_, disconnector := drv.(contract.WiFiDisconnector)
	_, reporter := drv.(contract.WiFiLinkReporter)
	_, switcher := drv.(contract.WiFiInterfaceSwitcher)
	if !watcher || !disconnector || !reporter || !switcher {
		t.Errorf("as handed to the server: watcher %v disconnector %v reporter %v switcher %v; want all", watcher, disconnector, reporter, switcher)
	}
	if (&Node{cfg: Config{Network: Network{WiFi: &WiFi{}}}}).wifiDriver() != nil {
		t.Error("a driver without Network.WiFi.Driver")
	}
}

// TestWiFiConnectedReannounces: the node's connect hook re-announces the
// node's DNS-SD records.
func TestWiFiConnectedReannounces(t *testing.T) {
	t.Parallel()
	adv := &countingAdvertiser{Noop: mdns.NewNoop()}
	n := &Node{advertiser: adv}
	n.wifiConnected(t.Context())
	if got := adv.triggers.Load(); got != 1 {
		t.Errorf("re-announced %d times, want 1", got)
	}
}
