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
	"github.com/SukramJ/go-fabric/node"
)

// liveWiFi is a simulated Wi-Fi driver recording what it was asked.
type liveWiFi struct {
	mu       sync.Mutex
	scans    [][]byte
	connects [][2][]byte
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
	return 0, "", 0
}

func wifiNode(t *testing.T, name string, drv *liveWiFi) *node.Node {
	t.Helper()
	return liveNode(t, name, func(c *node.Config) {
		c.Network.WiFiCommissioning = node.WiFiCommissioning{Driver: drv}
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
	n := wifiNode(t, "node-ble-wifi", drv)
	if n.Root().WiFiNetworkCommissioning == nil || n.Root().NetworkCommissioning != nil {
		t.Fatal("the Wi-Fi NetworkCommissioning is not mounted")
	}
	central := bletest.NewCentral(blePeripheralOf(t, n))
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
	n := wifiNode(t, "node-ip-wifi", drv)
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
