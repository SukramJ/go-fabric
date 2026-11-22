// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"

	"thingfabric.dev/go-fabric/client/exchange"
	"thingfabric.dev/go-fabric/client/handshake"
	"thingfabric.dev/go-fabric/cluster/spec"
)

// PeerPathClientBuilt reports whether the node built a client stack for its
// peer path (or runs the configured one).
func PeerPathClientBuilt(n *Node) bool { return n.existingPeerStack() != nil }

// PeerPathClientSessions is the live sessions of the peer path's client
// stack with the node nodeID of the fabric fabricIndex.
func PeerPathClientSessions(n *Node, fabricIndex uint8, nodeID uint64) []*exchange.Session {
	cs := n.existingPeerStack()
	if cs == nil {
		return nil
	}
	p := cs.peers.Get(handshake.PeerAddress{FabricIndex: fabricIndex, NodeID: nodeID})
	if p == nil {
		return nil
	}
	return p.Sessions()
}

// PeerPathInvoke invokes one command on the node nodeID of the fabric
// fabricIndex over n's peer path.
func PeerPathInvoke(ctx context.Context, n *Node, fabricIndex uint8, nodeID uint64, endpoint uint16, clusterID, commandID uint32, fields spec.Encodable) (*spec.Node, error) {
	return peerPath{n}.to(fabricIndex, nodeID).Invoke(ctx, endpoint, clusterID, commandID, fields)
}
