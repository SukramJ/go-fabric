// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package topology

import (
	"net/netip"
	"slices"

	generaldiagdef "github.com/SukramJ/go-fabric/cluster/spec/generaldiagnostics"
	netcommdef "github.com/SukramJ/go-fabric/cluster/spec/networkcommissioning"
	threaddiagdef "github.com/SukramJ/go-fabric/cluster/spec/threadnetworkdiagnostics"
	wifidiagdef "github.com/SukramJ/go-fabric/cluster/spec/wifinetworkdiagnostics"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/device"
)

// Image is what one commissioned node contributes to the graph: its
// identity, reachability and bridge flag, and the four network clusters
// of its root endpoint as typed states. [ImageOf] reads one from a
// client.Node; a test or a host with its own cache fills one by hand.
type Image struct {
	// NodeID is the node's operational node id.
	NodeID uint64
	// Available: the controller is connected to the node.
	Available bool
	// IsBridge: an endpoint of the node lists the Aggregator device type.
	IsBridge bool
	// NetworkFeatures is the root NetworkCommissioning's FeatureMap, which
	// decides the node's [NetworkType].
	NetworkFeatures netcommdef.Feature
	// The root endpoint's network clusters; an attribute the node did not
	// report stays at its zero value.
	NetworkCommissioning     netcommdef.State
	GeneralDiagnostics       generaldiagdef.State
	ThreadNetworkDiagnostics threaddiagdef.State
	WiFiNetworkDiagnostics   wifidiagdef.State
	// Addresses are the node's known operational addresses.
	Addresses []netip.Addr
}

// ImageOf reads n's [Image] from its client image: the typed states of
// endpoint 0 through Endpoint.Cluster and the generated StateFrom (a
// cluster the image does not hold reads as absent), the node id from its
// peer address, Available from its connection state, IsBridge from the
// device types of every endpoint and the addresses from its record. ok is
// false for a node that is not commissioned.
//
// The network type follows matterjs-server at 0bd06f22, which decides it
// from the root endpoint's NetworkCommissioning FeatureMap alone
// (topology-utils.ts:89-108, attribute 0/49/65532). While that cluster is
// not in the image yet, the features fall back to the node's
// client.Properties — SupportsThread, SupportsWifi, SupportsEthernet, the
// same FeatureMap ORed over every endpoint — so a node whose root is not
// read yet is not shown as "unknown" when another endpoint already tells.
//
// IsBridge is matterjs-server's isBridgeNode at 0bd06f22
// (packages/ws-controller/src/util/bridgeDetection.ts:28-38): any
// endpoint's DeviceTypeList holds the Aggregator.
func ImageOf(n *client.Node) (Image, bool) {
	addr, ok := n.PeerAddress()
	if !ok {
		return Image{}, false
	}
	props := n.Properties()
	img := Image{
		NodeID:    addr.NodeID,
		Available: n.ConnectionState() == client.Connected,
		IsBridge:  slices.Contains(props.DeviceTypes, uint32(device.AggregatorID)),
	}
	if ep := n.Endpoint(0); ep != nil {
		nc := ep.Cluster(netcommdef.ClusterID)
		if nc != nil {
			img.NetworkFeatures = netcommdef.Feature(nc.FeatureMap())
		}
		img.NetworkCommissioning, _ = netcommdef.StateFrom(nc)
		img.GeneralDiagnostics, _ = generaldiagdef.StateFrom(ep.Cluster(generaldiagdef.ClusterID))
		img.ThreadNetworkDiagnostics, _ = threaddiagdef.StateFrom(ep.Cluster(threaddiagdef.ClusterID))
		img.WiFiNetworkDiagnostics, _ = wifidiagdef.StateFrom(ep.Cluster(wifidiagdef.ClusterID))
		if nc == nil {
			img.NetworkFeatures = featuresOf(props)
		}
	} else {
		img.NetworkFeatures = featuresOf(props)
	}
	for _, a := range n.Record().Addresses {
		if a.IP.IsValid() && !slices.Contains(img.Addresses, a.IP) {
			img.Addresses = append(img.Addresses, a.IP)
		}
	}
	slices.SortFunc(img.Addresses, netip.Addr.Compare)
	return img, true
}

// featuresOf is the NetworkCommissioning features client.Properties
// derived from every endpoint.
func featuresOf(p client.Properties) netcommdef.Feature {
	var f netcommdef.Feature
	if p.SupportsWifi {
		f |= netcommdef.FeatureWiFiNetworkInterface
	}
	if p.SupportsThread {
		f |= netcommdef.FeatureThreadNetworkInterface
	}
	if p.SupportsEthernet {
		f |= netcommdef.FeatureEthernetNetworkInterface
	}
	return f
}
