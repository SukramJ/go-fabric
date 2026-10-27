// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"context"

	"github.com/SukramJ/go-fabric/cluster/netinfra"
	tdirdef "github.com/SukramJ/go-fabric/cluster/spec/threadnetworkdirectory"
	"github.com/SukramJ/go-fabric/contract"
)

// The network-infrastructure servers (cluster/netinfra) of a Network
// Infrastructure Manager and a Thread Border Router. matter.js serves both
// clusters as the generated stub; their rules follow connectedhomeip
// (cluster/netinfra's package doc), so the kit's server is the default
// with or without a declaration.

// newWiFiManagementServer is the module's WiFiNetworkManagement server
// ([netinfra.NewWiFiManagement]) on the device's clock, without
// credentials: the host sets them on it
// (netinfra.WiFiManagementServer.SetNetworkCredentials).
func newWiFiManagementServer(b defaultBuild) (contract.ClusterServer, error) {
	return netinfra.NewWiFiManagement(netinfra.WiFiManagementConfig{Clock: b.clock, DataVersion: b.cfg.DataVersion})
}

// newThreadDirectoryServer is the module's ThreadNetworkDirectory server
// ([netinfra.NewThreadDirectory]) over the declared ThreadNetworkStore
// (tdirdef.Declare), an in-memory store without one — of the Initial's
// ThreadNetworkTableSize when it sets one. The Initial's
// PreferredExtendedPanId is the preferred network it starts with when the
// store saved none (netinfra.ThreadDirectoryConfig); it must
// name a stored network.
func newThreadDirectoryServer(b defaultBuild) (contract.ClusterServer, error) {
	cfg := netinfra.ThreadDirectoryConfig{DataVersion: b.cfg.DataVersion, Logger: Logger}
	if d, ok := b.ports.(tdirdef.Declare); ok && d.ThreadNetworkStore != nil {
		cfg.Store = d.ThreadNetworkStore
	}
	if size, ok := b.initial[tdirdef.AttrThreadNetworkTableSize].(uint8); ok && cfg.Store == nil {
		cfg.Store = netinfra.NewMemoryThreadNetworkStore(size)
		delete(b.initial, tdirdef.AttrThreadNetworkTableSize)
	}
	if v, ok := b.initial[tdirdef.AttrPreferredExtendedPanId]; ok {
		if id, isBytes := v.([]byte); isBytes && len(id) > 0 {
			cfg.PreferredExtendedPanID = id
		}
		delete(b.initial, tdirdef.AttrPreferredExtendedPanId)
	}
	return netinfra.NewThreadDirectory(context.Background(), cfg)
}
