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
// With [GATTPeripheral] it is one of the two radios a device-side host
// supplies: a node commissioned over BLE joins its Wi-Fi network through
// it. It is set as node.Network.WiFi's Driver; the module
// thingfabric.dev/go-fabric/wifi/wpasupplicant implements it over
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
// radio back on the network it was on (thingfabric.dev/go-fabric/
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

// WiFiInterfaceSwitcher is an optional extension of a [WiFiDriver]: it
// switches the radio's network interface on and off — connectedhomeip's
// optional driver feature SetEnabled
// (src/include/platform/NetworkCommissioning.h:225-228 at the harness
// pin), which the cluster calls for a write of InterfaceEnabled. With it a
// write that changes InterfaceEnabled switches the interface, and the
// attribute takes the value once the driver switched it; while it is
// false the cluster joins no network of its own accord. Without it the
// interface is not switchable: a write of false answers INVALID_ACTION
// (NetworkCommissioningCluster.cpp:1120-1132) and InterfaceEnabled stays
// true.
type WiFiInterfaceSwitcher interface {
	// SetInterfaceEnabled switches the interface on (true) or off (false)
	// and returns once it did. An error leaves the interface as it was:
	// one that wraps errors.ErrUnsupported says the driver cannot switch
	// it so (the write answers INVALID_ACTION), a [StatusError] names the
	// write's status, any other answers FAILURE. It may be called while a
	// ScanNetworks or ConnectNetwork runs. The driver bounds its own wait;
	// ctx may carry no deadline.
	SetInterfaceEnabled(ctx context.Context, enabled bool) error
}

// WiFiLinkWatcher is an optional extension of a [WiFiDriver]: it pushes
// every change of the radio's link — a link lost, and one the radio
// regained by itself (its own rejoin after a loss of connectivity) — so
// the cluster's Networks connected flags and its LastNetworkingStatus,
// LastNetworkID and LastConnectErrorValue follow the radio between the
// joins the cluster asks for. connectedhomeip's driver contract has the
// same push: a NetworkStatusChangeCallback handed to the driver at Init,
// whose OnNetworkingStatusChange reports "autonomous connection after
// loss of connectivity" (src/include/platform/NetworkCommissioning.h:
// 188-202, :207 at 6170af84). The cluster starts no rejoin of its own on a
// reported loss: the driver's radio rejoins by itself. Without it the
// cluster learns of the link only from the joins it asks for.
type WiFiLinkWatcher interface {
	// WatchLink calls report for every change of the link until ctx ends,
	// then returns. report is called one change at a time, never
	// concurrently, and returns at once.
	WatchLink(ctx context.Context, report func(WiFiLinkChange))
}

// WiFiLinkChange is one change a [WiFiLinkWatcher] reports.
type WiFiLinkChange struct {
	// SSID is the network the change is about: the one the radio is on
	// when Connected, the one it lost otherwise (nil when unknown).
	SSID []byte
	// Connected is whether the radio is on SSID now.
	Connected bool
	// Status is the NetworkCommissioningStatusEnum value the cluster
	// serves as LastNetworkingStatus (0 Success for a link that came up).
	Status uint8
	// ErrorValue is the interface's own error of a link that went down,
	// served as LastConnectErrorValue; nil for none.
	ErrorValue *int32
}

// WiFiDisconnector is an optional extension of a [WiFiDriver]: it takes the
// radio off the network it is on. When the fail-safe expires, the cluster
// reverts its network list; it calls DisconnectNetwork when no committed
// network is left, and when the radio is on a network the revert removed
// and the rejoin of the committed network failed — so the radio does not
// stay on a network the node was never committed to. connectedhomeip's
// driver has RevertConfiguration for the fail-safe's expiry and a
// DisconnectFromNetwork that is itself conditional
// (src/include/platform/NetworkCommissioning.h:255-259, :291-296 at
// 6170af84), which is why the capability is optional here. Without it the
// radio stays where the last join left it.
type WiFiDisconnector interface {
	// DisconnectNetwork leaves the network the radio is on; on none it
	// does nothing. The driver bounds its own wait; ctx may carry no
	// deadline.
	DisconnectNetwork(ctx context.Context) error
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
