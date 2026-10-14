// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package node_test

// Network.WiFi (brief AM §3): a Wi-Fi node serves WiFiNetworkDiagnostics on
// the root and states the RootNode condition WiFi.

import (
	"bytes"
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	wifidiagdef "github.com/SukramJ/go-fabric/cluster/spec/wifinetworkdiagnostics"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/node"
)

const clusterWiFiNetworkDiagnostics uint32 = 0x0036

var testBSSID = []byte{0x02, 0x00, 0x5E, 0x10, 0x00, 0x01}

// wifiLink is a mattercore.WiFiReporter with fixed values.
type wifiLink struct{}

func (wifiLink) BSSID() ([]byte, bool) { return testBSSID, true }
func (wifiLink) SecurityType() (wifidiagdef.SecurityTypeEnum, bool) {
	return wifidiagdef.SecurityTypeWpa2, true
}

func (wifiLink) WiFiVersion() (wifidiagdef.WiFiVersionEnum, bool) {
	return wifidiagdef.WiFiVersionAx, true
}
func (wifiLink) ChannelNumber() (uint16, bool)          { return 36, true }
func (wifiLink) RSSI() (int8, bool)                     { return -55, true }
func (wifiLink) BeaconLostCount() (uint32, bool)        { return 0, true }
func (wifiLink) BeaconRxCount() (uint32, bool)          { return 0, true }
func (wifiLink) PacketMulticastRxCount() (uint32, bool) { return 0, true }
func (wifiLink) PacketMulticastTxCount() (uint32, bool) { return 0, true }
func (wifiLink) PacketUnicastRxCount() (uint32, bool)   { return 0, true }
func (wifiLink) PacketUnicastTxCount() (uint32, bool)   { return 0, true }
func (wifiLink) CurrentMaxRate() (uint64, bool)         { return 0, false }
func (wifiLink) OverrunCount() (uint64, bool)           { return 0, true }
func (wifiLink) ResetCounts()                           {}

// TestWiFiDiagnostics: with Network.WiFi the root serves 0x0036 next to
// EthernetNetworkDiagnostics, states the condition WiFi, and the device-type
// judgement finds nothing to say about either; without it nothing of the
// sort is there.
func TestWiFiDiagnostics(t *testing.T) {
	t.Parallel()
	for name, wifi := range map[string]bool{"wifi": true, "ethernet only": false} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cfg := minimalConfig(openDB(t, filepath.Join(t.TempDir(), "node.db")))
			if wifi {
				cfg.Network.WiFi = node.WiFi{Reporter: wifiLink{}, Features: wifidiagdef.FeaturePacketCounts | wifidiagdef.FeatureErrorCounts}
			}
			n := newNode(t, cfg)
			if (n.Root().WiFiNetworkDiagnostics != nil) != wifi {
				t.Fatalf("Root().WiFiNetworkDiagnostics = %v with Network.WiFi %v", n.Root().WiFiNetworkDiagnostics, wifi)
			}
			s := paseSession(t, n)
			ctx := context.Background()
			report, err := s.Read(ctx, attr(0, clusterDescriptor, attrServerList))
			if err != nil {
				t.Fatalf("read ServerList: %v", err)
			}
			v, ok := report.Value(0, clusterDescriptor, attrServerList)
			if !ok {
				t.Fatalf("no ServerList: %+v", report.Statuses)
			}
			servers := uintList(v)
			if listed := slices.Contains(servers, uint64(clusterWiFiNetworkDiagnostics)); listed != wifi {
				t.Errorf("ServerList %v lists WiFiNetworkDiagnostics: %v, want %v", servers, listed, wifi)
			}
			if !slices.Contains(servers, 0x0037) {
				t.Errorf("ServerList %v lost EthernetNetworkDiagnostics", servers)
			}

			root := n.Bridge().Topology().FindByID(0)
			if stated := slices.Contains(root.DeviceConditions, "WiFi"); stated != wifi {
				t.Errorf("root conditions %v state WiFi: %v, want %v", root.DeviceConditions, stated, wifi)
			}
			for _, v := range endpoint.ValidateDeviceTypes(n.Bridge().Topology()) {
				if v.Endpoint == 0 && (strings.Contains(v.Requirement, "WiFi") || strings.Contains(v.Requirement, "Ethernet")) {
					t.Errorf("root violation %+v", v)
				}
			}
			if !wifi {
				return
			}
			report, err = s.Read(ctx, attr(0, clusterWiFiNetworkDiagnostics, wifidiagdef.AttrBssid))
			if err != nil {
				t.Fatal(err)
			}
			if b, ok := report.Value(0, clusterWiFiNetworkDiagnostics, wifidiagdef.AttrBssid); !ok || !bytes.Equal(b.Octets, testBSSID) {
				t.Errorf("Bssid %+v (%v)", b, ok)
			}
			report, err = s.Read(ctx, attr(0, clusterWiFiNetworkDiagnostics, wifidiagdef.AttrCurrentMaxRate))
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := report.Value(0, clusterWiFiNetworkDiagnostics, wifidiagdef.AttrCurrentMaxRate); ok {
				t.Error("CurrentMaxRate served without opting in")
			}
		})
	}
}
