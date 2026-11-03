// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package topology_test

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"time"

	netcommdef "github.com/SukramJ/go-fabric/cluster/spec/networkcommissioning"
	wifidiagdef "github.com/SukramJ/go-fabric/cluster/spec/wifinetworkdiagnostics"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/controller/topology"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/secure/setup"
)

var siteBSSID = []byte{0x02, 0x00, 0x5E, 0x10, 0x00, 0x01}

// link is a node.WiFiReporter of one association at -72 dBm.
type link struct{}

func (link) BSSID() ([]byte, bool) { return bytes.Clone(siteBSSID), true }
func (link) SecurityType() (wifidiagdef.SecurityTypeEnum, bool) {
	return wifidiagdef.SecurityTypeWpa2, true
}

func (link) WiFiVersion() (wifidiagdef.WiFiVersionEnum, bool) {
	return wifidiagdef.WiFiVersionAx, true
}
func (link) ChannelNumber() (uint16, bool)          { return 6, true }
func (link) RSSI() (int8, bool)                     { return -72, true }
func (link) BeaconLostCount() (uint32, bool)        { return 0, true }
func (link) BeaconRxCount() (uint32, bool)          { return 0, true }
func (link) PacketMulticastRxCount() (uint32, bool) { return 0, true }
func (link) PacketMulticastTxCount() (uint32, bool) { return 0, true }
func (link) PacketUnicastRxCount() (uint32, bool)   { return 0, true }
func (link) PacketUnicastTxCount() (uint32, bool)   { return 0, true }
func (link) CurrentMaxRate() (uint64, bool)         { return 0, false }
func (link) OverrunCount() (uint64, bool)           { return 0, true }
func (link) ResetCounts()                           {}

// joinAll is a Wi-Fi driver that joins every network.
type joinAll struct{}

func (joinAll) ScanNetworks(context.Context, []byte) (status uint8, debugText string, results []contract.WiFiScanResult) {
	return 0, "", nil
}

func (joinAll) ConnectNetwork(context.Context, []byte, []byte) (status uint8, debugText string, errorValue int32) {
	return 0, "", 0
}

// TestServiceOnASite: a controller built with controller.New on a
// fabrictest site, the topology's Changes installed on its configuration,
// pairs a Wi-Fi device (WiFiNetworkDiagnostics and a Wi-Fi
// NetworkCommissioning on its root) and an Ethernet one; a Refresh reads
// the Wi-Fi station's diagnostics and the graph holds the station, its
// access point and the link at the reported RSSI, and the Ethernet node
// without one. The observer hears of it; the node's later change after a
// read reaches the service through the installed hooks. A Thread node
// cannot stand on a site — the module's nodes have no Thread radio — so
// the Thread rules are held by the pure Build tests.
func TestServiceOnASite(t *testing.T) {
	t.Parallel()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	wifiDev := site.AddDevice(fabrictest.Topology{Configure: func(cfg *node.Config) {
		cfg.Network.WiFi = &node.WiFi{Reporter: link{}}
		fabrictest.UseWiFiCommissioning(cfg, joinAll{})
	}})
	ethDev := site.AddDevice(fabrictest.Topology{})

	var changes topology.Changes
	cfg := site.ControllerConfig(fabrictest.ControllerOptions{})
	changes.Install(&cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	var c *controller.Controller
	if err := site.Run(ctx, func(ctx context.Context) (err error) {
		c, err = controller.New(ctx, cfg)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = site.Run(context.Background(), c.Close) })
	pair := func(dev *fabrictest.Device) *client.Node {
		t.Helper()
		code, err := setup.ManualCode(dev.Discriminator(), dev.Passcode())
		if err != nil {
			t.Fatal(err)
		}
		var n *client.Node
		if err := site.Run(ctx, func(ctx context.Context) (err error) {
			n, err = c.Pair(ctx, code, controller.PairOptions{OnAttestationFailure: attestation.Accept()})
			return err
		}); err != nil {
			t.Fatalf("Pair: %v", err)
		}
		return n
	}
	// The site's devices share a pairing code, so which Pair commissions
	// which device is discovery's choice: tell them apart by their root.
	wifiNode, ethNode := pair(wifiDev), pair(ethDev)
	if img, _ := topology.ImageOf(wifiNode); img.NetworkFeatures&netcommdef.FeatureWiFiNetworkInterface == 0 {
		wifiNode, ethNode = ethNode, wifiNode
	}
	if err := site.Eventually(ctx, "both nodes connected", func() bool {
		return wifiNode.ConnectionState() == client.Connected && ethNode.ConnectionState() == client.Connected
	}); err != nil {
		t.Fatal(err)
	}

	svc := topology.New(c, topology.Config{Changes: &changes, Clock: cfg.Clock})
	t.Cleanup(svc.Close)
	var mu sync.Mutex
	var seen []topology.Topology
	svc.OnChanged(func(t topology.Topology) {
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, t)
	})

	var topo topology.Topology
	if err := site.Run(ctx, func(ctx context.Context) (err error) {
		topo, err = svc.Refresh(ctx)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	wifiImg, ok := topology.ImageOf(wifiNode)
	if !ok {
		t.Fatal("ImageOf: the paired node is not commissioned")
	}
	ethImg, _ := topology.ImageOf(ethNode)
	wifiID, ethID := id(wifiImg.NodeID), id(ethImg.NodeID)
	nodes := map[string]topology.Node{}
	for _, n := range topo.Nodes {
		nodes[n.ID] = n
	}
	station := nodes[wifiID]
	if station.Kind != topology.KindMatter || station.NetworkType != topology.NetworkWiFi || station.Role != topology.RoleStation ||
		station.BSSID != "02:00:5E:10:00:01" || !station.Available || len(station.Addresses) == 0 {
		t.Errorf("station = %+v", station)
	}
	if ap := nodes["ap_02005E100001"]; ap.Kind != topology.KindWiFiAP || ap.BSSID != station.BSSID || ap.SSID != station.SSID {
		t.Errorf("access point = %+v", ap)
	}
	if eth := nodes[ethID]; eth.NetworkType != topology.NetworkEthernet || eth.Role != "" {
		t.Errorf("ethernet node = %+v", eth)
	}
	if len(topo.Connections) != 1 {
		t.Fatalf("connections = %+v", topo.Connections)
	}
	if l := topo.Connections[0]; l.Source != wifiID || l.Target != "ap_02005E100001" || l.Strength != topology.StrengthMedium ||
		l.SourceToTarget == nil || l.SourceToTarget.RSSI == nil || *l.SourceToTarget.RSSI != -72 {
		t.Errorf("link = %+v", l)
	}
	mu.Lock()
	n := len(seen)
	mu.Unlock()
	if n != 1 {
		t.Fatalf("observer told %d times, want 1", n)
	}

	// The ethernet node goes away: its decommission reaches the service
	// through the installed hooks and, after the debounce, the observer.
	if err := site.Run(ctx, func(ctx context.Context) error { return c.Registry().Decommission(ctx, ethNode) }); err != nil {
		t.Fatalf("Decommission: %v", err)
	}
	site.Fake().Advance(topology.DefaultDebounce)
	if err := site.Eventually(ctx, "the decommission rebuilt", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(seen) == 2 && len(seen[1].Nodes) == 2
	}); err != nil {
		t.Fatalf("after the decommission: %v", err)
	}
}

func id(n uint64) string {
	const digits = "0123456789"
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{digits[n%10]}, b...)
	}
	return string(b)
}
