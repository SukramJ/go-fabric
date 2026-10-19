// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest

import (
	"slices"

	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/node"
)

// Endpoint is a bridged endpoint for [Topology].Bridged that serves the
// given cluster servers under one device type: matter.js's
// MutableEndpoint({ deviceType, … }).with(…Server) that a test adds to a
// MockServerNode (packages/node/src/testing/mock-server-node.ts), for a
// server the test built itself — a feature selection or an initial state
// no device-layer constructor offers. key is the endpoint's StableKey
// ([Device.EndpointOf] finds its number by it), its device address and its
// NodeLabel. The servers are the test's: it changes them locally between
// interactions ([Device.Act]).
func Endpoint(key string, deviceType uint16, servers ...contract.ClusterServer) node.Endpoint {
	return &bridgedEndpoint{key: key, deviceType: deviceType, servers: slices.Clone(servers)}
}

// bridgedEndpoint is the [contract.EndpointSource] of [Endpoint].
type bridgedEndpoint struct {
	key        string
	deviceType uint16
	servers    []contract.ClusterServer
}

func (e *bridgedEndpoint) Spec() endpoint.Spec {
	return endpoint.Spec{
		StableKey: endpoint.StringKey(e.key), DeviceAddress: e.key, ChannelAddress: e.key + ":0",
		DeviceType: e.deviceType, FriendlyName: e.key, Source: e,
	}
}

func (e *bridgedEndpoint) MatterDeviceType() uint16 { return e.deviceType }

func (e *bridgedEndpoint) MatterClusterServers() []contract.ClusterServer {
	return slices.Clone(e.servers)
}
