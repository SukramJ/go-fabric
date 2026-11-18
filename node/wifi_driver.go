// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"

	"github.com/SukramJ/go-fabric/contract"
)

// wifiConnected re-announces the node's DNS-SD records after a successful
// join of the Wi-Fi NetworkCommissioning server's driver
// (mattercore.WiFiNetworkCommissioningConfig.OnConnected) instead of
// waiting for the responder's interface check. The rejoin at [Node.Start]
// runs it too; that rejoin runs before the start sequence publishes the
// fabrics' operational records (startLocked), so there the trigger finds
// nothing published yet and the records go out right after it. Mirrors
// matter.js
// examples/device-onoff-advanced/src/cluster/DummyWifiNetworkCommissioningServer.ts:132-133
// at db021da0 ("Announce operational in IP network",
// `this.env.get(DeviceAdvertiser).restartAdvertisement()` after its
// connect).
func (n *Node) wifiConnected(ctx context.Context) {
	n.TriggerReannounce(ctx)
}

// wifiDriver is [Network].WiFi's Driver as the node hands it to the Wi-Fi
// NetworkCommissioning server: the host's own, unwrapped, so the server
// finds every optional port the driver has — the link watch and the
// disconnect a wrapper hid before (review II, R2-P6-api-2450); nil
// without one.
func (n *Node) wifiDriver() contract.WiFiDriver {
	if w := n.cfg.Network.WiFi; w != nil && w.Driver != nil {
		return w.Driver
	}
	return nil
}
