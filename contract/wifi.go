// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package contract

import "context"

// WiFiDriver is the host port of a node whose NetworkCommissioning
// cluster has the Wi-Fi feature: the radio operations the cluster
// cannot do itself — connectedhomeip's WiFiDriver ScanNetworks and
// ConnectNetwork (src/include/platform/NetworkCommissioning.h at the
// harness pin). The network list, its rules and the failsafe revert stay
// in the cluster (cluster/core.WiFiNetworkCommissioning).
//
// With [BLEPeripheral] it is one of the two radios a device-side host
// supplies: a node commissioned over BLE joins its Wi-Fi network through
// it. It is set as node.Network.WiFi's Driver; the module
// github.com/SukramJ/go-fabric/wifi/wpasupplicant implements it over
// wpa_supplicant's D-Bus interface on Linux, and the reference daemon's
// --wifi-sim implements it as a simulation.
type WiFiDriver interface {
	// ScanNetworks scans for the network named ssid, every network when
	// ssid is empty. status is a NetworkCommissioningStatusEnum value
	// (0 Success); debugText may be empty.
	ScanNetworks(ctx context.Context, ssid []byte) (status uint8, debugText string, results []WiFiScanResult)
	// ConnectNetwork joins the network ssid with credentials. status is
	// a NetworkCommissioningStatusEnum value; on a failure errorValue is
	// the interface's own connect error (the cluster's
	// LastConnectErrorValue).
	ConnectNetwork(ctx context.Context, ssid, credentials []byte) (status uint8, debugText string, errorValue int32)
}

// WiFiLinkReporter is an optional extension of a [WiFiDriver]: it reports
// which network the radio is on. After a join that did not succeed the
// cluster asks it, so the Networks attribute and the cluster's rejoin of
// its committed network agree with the driver — a driver that puts the
// radio back on the network it was on (github.com/SukramJ/go-fabric/
// wifi/wpasupplicant) reports that network, and a working link is
// neither read as disconnected nor joined again. Without it the cluster
// reads the radio as on no network after a failed join.
type WiFiLinkReporter interface {
	// CurrentNetwork is the SSID of the network the radio is on or
	// joining, nil when it is on none; known is false when the driver
	// cannot tell. The driver bounds its own wait; ctx may carry no
	// deadline.
	CurrentNetwork(ctx context.Context) (ssid []byte, known bool)
}

// WiFiScanResult is one network a [WiFiDriver] scan found — Matter
// WiFiInterfaceScanResultStruct.
type WiFiScanResult struct {
	// Security is the WiFiSecurityBitmap.
	Security uint8
	// SSID is the network's SSID, at most 32 bytes.
	SSID []byte
	// BSSID is the access point's BSSID, 6 bytes.
	BSSID []byte
	// Channel is the channel number.
	Channel uint16
	// Band is the WiFiBandEnum value; HasBand reports whether it is known.
	Band    uint8
	HasBand bool
	// RSSI is the signal strength in dBm; HasRSSI reports whether known.
	RSSI    int8
	HasRSSI bool
}
