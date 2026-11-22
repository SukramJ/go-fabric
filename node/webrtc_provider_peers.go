// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"

	"thingfabric.dev/go-fabric/cluster/camera"
	"thingfabric.dev/go-fabric/endpoint"
)

// camerasOnline is what Start does for the camera surface: the requestor's
// ACL entry (webrtc_requestor_acl.go) and the providers' route to their
// requestors.
func (n *Node) camerasOnline(ctx context.Context) {
	n.ensureRequestorACL(ctx)
	n.attachCameraPeers()
}

// attachCameraPeers gives every WebRtcTransportProvider the kit assembled
// ([camera.Provider]) the node's peer path ([peerPath]) as its
// PeerConnector, so the camera's Offer, Answer, IceCandidates and End —
// Large Message commands — reach the requestor over a TCP session, the
// requestor's own or one the client stack establishes. Called at Start and
// after each assembly: a
// camera added to a running node gets its connector with the assembly
// that mounts it, before any controller can offer it a session.
func (n *Node) attachCameraPeers() {
	topo := n.br.Topology()
	if topo == nil {
		return
	}
	connector := peerPath{n}
	for _, ep := range topo.Endpoints {
		for _, srv := range endpoint.ClusterServers(ep) {
			if p, ok := srv.(*camera.Provider); ok {
				p.AttachPeerConnector(connector)
			}
		}
	}
}
