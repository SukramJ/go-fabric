// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"

	netcommdef "github.com/SukramJ/go-fabric/cluster/spec/networkcommissioning"
	"github.com/SukramJ/go-fabric/contract"
)

// reannouncingDriver is the host's Wi-Fi driver as the node hands it to
// the Wi-Fi NetworkCommissioning server: a successful ConnectNetwork
// command re-announces the node's DNS-SD records at once instead of
// waiting for the responder's interface check. The rejoin at [Node.Start]
// goes through it too; it runs before the start sequence publishes the
// fabrics' operational records (startLocked), so there the trigger finds
// nothing published yet and the records go out right after it. Mirrors matter.js
// examples/device-onoff-advanced/src/cluster/DummyWifiNetworkCommissioningServer.ts:132-133
// at db021da0 ("Announce operational in IP network",
// `this.env.get(DeviceAdvertiser).restartAdvertisement()` after its connect). The
// cluster's own handling of the result does not change.
type reannouncingDriver struct {
	contract.WiFiDriver
	n *Node
}

// ConnectNetwork joins through the host's driver and re-announces on
// Success.
func (d reannouncingDriver) ConnectNetwork(ctx context.Context, ssid, credentials []byte) (status uint8, debugText string, errorValue int32) {
	status, debugText, errorValue = d.WiFiDriver.ConnectNetwork(ctx, ssid, credentials)
	if netcommdef.NetworkCommissioningStatusEnum(status) == netcommdef.NetworkCommissioningStatusSuccess {
		d.n.TriggerReannounce(ctx)
	}
	return status, debugText, errorValue
}

// wifiDriver is [Network].WiFi's Driver wrapped for the cluster, nil
// without one.
func (n *Node) wifiDriver() contract.WiFiDriver {
	if w := n.cfg.Network.WiFi; w != nil && w.Driver != nil {
		return reannouncingDriver{WiFiDriver: w.Driver, n: n}
	}
	return nil
}
