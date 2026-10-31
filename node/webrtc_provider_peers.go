// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"

	"github.com/SukramJ/go-fabric/bridge"
	"github.com/SukramJ/go-fabric/cluster/camera"
	"github.com/SukramJ/go-fabric/endpoint"
)

// bridgePeerConnector is the camera.PeerConnector of the node's bridge:
// a CASE session this node initiates to the requestor of a WebRTC session
// ([bridge.Bridge.ConnectPeer]), as connectedhomeip's camera app reaches it
// through CASESessionManager::FindOrEstablishSession
// (webrtc-provider-manager.cpp:860-942 at 6170af84).
type bridgePeerConnector struct{ br *bridge.Bridge }

func (c bridgePeerConnector) ConnectPeer(ctx context.Context, fabricIndex uint8, peerNodeID uint64) (camera.PeerSession, error) {
	s, err := c.br.ConnectPeer(ctx, fabricIndex, peerNodeID)
	if err != nil {
		return nil, err
	}
	return s, nil
}

// camerasOnline is what Start does for the camera surface: the requestor's
// ACL entry (webrtc_requestor_acl.go) and the providers' route to their
// requestors.
func (n *Node) camerasOnline(ctx context.Context) {
	n.ensureRequestorACL(ctx)
	n.attachCameraPeers()
}

// attachCameraPeers gives every WebRtcTransportProvider the kit assembled
// ([camera.Provider]) the bridge as its PeerConnector, so the camera's
// Offer, Answer, IceCandidates and End reach the requestor over this
// node's own CASE initiator. Called at Start and after each assembly: a
// camera added to a running node gets its connector with the assembly
// that mounts it, before any controller can offer it a session.
func (n *Node) attachCameraPeers() {
	topo := n.br.Topology()
	if topo == nil {
		return
	}
	connector := bridgePeerConnector{br: n.br}
	for _, ep := range topo.Endpoints {
		for _, srv := range endpoint.ClusterServers(ep) {
			if p, ok := srv.(*camera.Provider); ok {
				p.AttachPeerConnector(connector)
			}
		}
	}
}
