// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

// The proof of brief BA item 4: the module's commissioner commissions the
// module's node with the simulated Wi-Fi interface — over the in-memory
// BTP pair and over IP — including the network steps (AddOrUpdateWiFiNetwork,
// ConnectNetwork), and the node is then reachable over CASE.

import (
	"bytes"
	"context"
	"net"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/spec"
	netcommdef "github.com/SukramJ/go-fabric/cluster/spec/networkcommissioning"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/controller/ble/bletest"
	"github.com/SukramJ/go-fabric/controller/commissioner"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/store"
)

// liveWiFi is a simulated Wi-Fi driver recording what it was asked.
type liveWiFi struct {
	mu       sync.Mutex
	scans    [][]byte
	connects [][2][]byte
	// fail answers every connect with AuthFailure.
	fail bool
}

func (w *liveWiFi) ScanNetworks(_ context.Context, ssid []byte) (status uint8, debugText string, results []contract.WiFiScanResult) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.scans = append(w.scans, bytes.Clone(ssid))
	return 0, "", []contract.WiFiScanResult{{SSID: bytes.Clone(ssid), BSSID: make([]byte, 6), Channel: 6}}
}

func (w *liveWiFi) ConnectNetwork(_ context.Context, ssid, credentials []byte) (status uint8, debugText string, errorValue int32) {
	time.Sleep(50 * time.Millisecond)
	w.mu.Lock()
	defer w.mu.Unlock()
	w.connects = append(w.connects, [2][]byte{bytes.Clone(ssid), bytes.Clone(credentials)})
	if w.fail {
		return 7, "refused", 15 // AuthFailure
	}
	return 0, "", 0
}

func wifiNode(t *testing.T, name string, drv *liveWiFi, radio *bletest.Radio) *node.Node {
	t.Helper()
	return liveNode(t, name, func(c *node.Config) {
		c.Network.WiFi = &node.WiFi{Driver: drv}
		if radio != nil {
			c.Network.BLE = radio
		}
	})
}

// checkWiFiCommissioned asserts the driver's calls and reads the node's
// Networks over CASE: the network connected.
func checkWiFiCommissioned(ctx context.Context, t *testing.T, f *ccFabric, res commissioner.Result, drv *liveWiFi) {
	t.Helper()
	drv.mu.Lock()
	scans, connects := len(drv.scans), drv.connects
	drv.mu.Unlock()
	if scans != 1 || len(connects) != 1 || string(connects[0][0]) != "lab" || string(connects[0][1]) != "secret12" {
		t.Fatalf("driver: %d scans, connects %q", scans, connects)
	}
	ia := f.com.Interaction(f.set.Get(res.Address))
	path := im.ConcreteAttributePath{Endpoint: 0, Cluster: netcommdef.ClusterID, Attribute: netcommdef.AttrNetworks, HasEndpoint: true, HasCluster: true, HasAttribute: true}
	report, err := ia.Read(ctx, path)
	if err != nil {
		t.Fatalf("Read over CASE: %v", err)
	}
	v, ok := report.Value(0, netcommdef.ClusterID, netcommdef.AttrNetworks)
	if !ok {
		t.Fatal("no Networks")
	}
	nets, err := spec.DecodeList(func(n spec.Node) (netcommdef.NetworkInfoStruct, error) {
		var s netcommdef.NetworkInfoStruct
		err := s.DecodeTLV(n)
		return s, err
	})(v)
	if err != nil || len(nets) != 1 || string(nets[0].NetworkId) != "lab" || !nets[0].Connected {
		t.Fatalf("Networks %+v, %v", nets, err)
	}
}

func TestLiveCommissioningOverBLEWithWiFi(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	f := newCCFabric(t, bleFabricID+1, bleAdmin, "ble wifi")
	drv := &liveWiFi{}
	p := nodePeripheral()
	n := wifiNode(t, "node-ble-wifi", drv, p.Radio())
	if n.Root().WiFiNetworkCommissioning == nil || n.Root().NetworkCommissioning != nil {
		t.Fatal("the Wi-Fi NetworkCommissioning is not mounted")
	}
	central := bletest.NewCentral(p)
	ch := dialBLE(ctx, t, central)
	f.announceOperational(t, udpAddrOf(t, n), bleNodeID, "node-ble-wifi")
	res, err := f.com.Commission(ctx, commissioner.Options{
		Fabric: f.fab, Passcode: otaLivePasscode, NodeID: bleNodeID, Channel: ch,
		WiFiNetwork:          &commissioner.WiFiNetwork{SSID: "lab", Credentials: "secret12"},
		OnAttestationFailure: attestation.Accept(), Timeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("Commission over BLE with Wi-Fi: %v", err)
	}
	checkWiFiCommissioned(ctx, t, f, res, drv)
}

func TestLiveCommissioningOverIPWithWiFi(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	f := newCCFabric(t, bleFabricID+2, bleAdmin, "ip wifi")
	drv := &liveWiFi{}
	n := wifiNode(t, "node-ip-wifi", drv, nil)
	addr := udpAddrOf(t, n)
	f.announceOperational(t, addr, bleNodeID, "node-ip-wifi")
	res, err := f.com.Commission(ctx, commissioner.Options{
		Fabric: f.fab, Passcode: otaLivePasscode, NodeID: bleNodeID, Addresses: []*net.UDPAddr{addr},
		WiFiNetwork:          &commissioner.WiFiNetwork{SSID: "lab", Credentials: "secret12"},
		OnAttestationFailure: attestation.Accept(), Timeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("Commission over IP with Wi-Fi: %v", err)
	}
	checkWiFiCommissioned(ctx, t, f, res, drv)
}

// joinReannouncer counts the re-announcements the node triggers from the
// Wi-Fi driver's return (the node's driver wrapper on its call stack) apart
// from every other: the bridge also re-announces on session events during
// commissioning (triggerSessionReannounce), which a plain count would
// include.
type joinReannouncer struct {
	*mdns.Noop
	mu       sync.Mutex
	fromJoin int
}

func (a *joinReannouncer) Enabled() bool { return true }

func (a *joinReannouncer) TriggerReannounce(context.Context) {
	if !strings.Contains(string(debug.Stack()), "reannouncingDriver.ConnectNetwork") {
		return
	}
	a.mu.Lock()
	a.fromJoin++
	a.mu.Unlock()
}

func (a *joinReannouncer) joins() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.fromJoin
}

// TestLiveWiFiJoinReannounces: a successful join announces the node's
// DNS-SD records — the ConnectNetwork command's (commissioning over IP)
// once, and the rejoin of the committed network at the next Start once —
// and a failed join does not (matter.js
// DummyWifiNetworkCommissioningServer.ts:132-133 at db021da0).
func TestLiveWiFiJoinReannounces(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "rejoin.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	start := func(name string, drv *liveWiFi) (*node.Node, *joinReannouncer) {
		adv := &joinReannouncer{Noop: mdns.NewNoop()}
		return liveNode(t, name, func(c *node.Config) {
			c.Store = node.Store{DB: db}
			c.Advertiser = adv
			c.Network.WiFi = &node.WiFi{Driver: drv}
		}), adv
	}
	triggers := (*joinReannouncer).joins

	f := newCCFabric(t, bleFabricID+3, bleAdmin, "rejoin")
	n, adv := start("node-rejoin", &liveWiFi{})
	addr := udpAddrOf(t, n)
	f.announceOperational(t, addr, bleNodeID, "node-rejoin")
	if _, err := f.com.Commission(ctx, commissioner.Options{
		Fabric: f.fab, Passcode: otaLivePasscode, NodeID: bleNodeID, Addresses: []*net.UDPAddr{addr},
		WiFiNetwork:          &commissioner.WiFiNetwork{SSID: "lab", Credentials: "secret12"},
		OnAttestationFailure: attestation.Accept(), Timeout: 30 * time.Second,
	}); err != nil {
		t.Fatalf("Commission over IP with Wi-Fi: %v", err)
	}
	if got := triggers(adv); got != 1 {
		t.Errorf("ConnectNetwork re-announced %d times, want 1", got)
	}
	if err := n.Stop(ctx); err != nil {
		t.Fatal(err)
	}

	// The rejoin at Start: liveNode starts the node.
	n, adv = start("node-rejoin", &liveWiFi{})
	if got := triggers(adv); got != 1 {
		t.Errorf("the rejoin re-announced %d times, want 1", got)
	}
	if err := n.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	drv := &liveWiFi{fail: true}
	_, adv = start("node-rejoin", drv)
	drv.mu.Lock()
	connects := len(drv.connects)
	drv.mu.Unlock()
	if connects != 1 {
		t.Fatalf("the failing driver saw %d connects at Start, want 1", connects)
	}
	if got := triggers(adv); got != 0 {
		t.Errorf("a failed rejoin re-announced %d times, want 0", got)
	}
}

// TestLiveWiFiFailedConnectDoesNotReannounce: a ConnectNetwork command the
// driver fails announces nothing.
func TestLiveWiFiFailedConnectDoesNotReannounce(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	f := newCCFabric(t, bleFabricID+4, bleAdmin, "connect fails")
	adv := &joinReannouncer{Noop: mdns.NewNoop()}
	drv := &liveWiFi{fail: true}
	n := liveNode(t, "node-connect-fails", func(c *node.Config) {
		c.Advertiser = adv
		c.Network.WiFi = &node.WiFi{Driver: drv}
	})
	addr := udpAddrOf(t, n)
	if _, err := f.com.Commission(ctx, commissioner.Options{
		Fabric: f.fab, Passcode: otaLivePasscode, NodeID: bleNodeID, Addresses: []*net.UDPAddr{addr},
		WiFiNetwork:          &commissioner.WiFiNetwork{SSID: "lab", Credentials: "secret12"},
		OnAttestationFailure: attestation.Accept(), Timeout: 30 * time.Second,
	}); err == nil {
		t.Fatal("Commission succeeded with a failing ConnectNetwork")
	}
	drv.mu.Lock()
	connects := len(drv.connects)
	drv.mu.Unlock()
	triggers := adv.joins()
	if connects != 1 || triggers != 0 {
		t.Errorf("%d connects, %d re-announcements; want 1 and 0", connects, triggers)
	}
}
