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
// The module's own nodes are Ethernet-attached; the reference daemon
// implements this as a simulation (TESTING ONLY, --wifi-sim).
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
