// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"
	"fmt"

	"github.com/SukramJ/go-fabric/client/handshake"
	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/client/peer"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/im"
)

// timePeerReader is the node's [mattercore.TimePeerReader]: it reads the
// trusted time source's node over the node's client stack — a CASE
// session to the node of the fabric ([Node.Peers]) and one read of the
// paths — as connectedhomeip's time client reads it over the session
// CASESessionManager finds or establishes
// (TimeSynchronizationCluster.cpp:401-428, :525-539 at 6170af84). The
// node attaches it to the root TimeSynchronization
// ([mattercore.TimeSynchronization.AttachTimePeerReader]) while
// [Config].Client is on, as it mounts its [commissionerReach].
type timePeerReader struct{ n *Node }

var _ mattercore.TimePeerReader = timePeerReader{}

// ReadPeerAttributes reads paths of the node nodeID of fabric fabricIndex
// in one Read; a path answered with a status is absent from the result.
func (r timePeerReader) ReadPeerAttributes(ctx context.Context, fabricIndex uint8, nodeID uint64, paths []im.ConcreteAttributePath) (map[im.ConcreteAttributePath]spec.Node, error) {
	ia, err := r.n.peerInteraction(ctx, fabricIndex, nodeID)
	if err != nil {
		return nil, err
	}
	rep, err := ia.ReadWith(ctx, interaction.ReadRequest{Attributes: paths})
	if err != nil {
		return nil, fmt.Errorf("node: read of %d:%#x: %w", fabricIndex, nodeID, err)
	}
	out := make(map[im.ConcreteAttributePath]spec.Node, len(paths))
	for _, p := range paths {
		if v, ok := rep.Value(p.Endpoint, p.Cluster, p.Attribute); ok {
			out[p] = v
		}
	}
	return out, nil
}

// peerInteraction opens the interaction with the node nodeID of fabric
// fabricIndex over the client stack, connecting first.
func (n *Node) peerInteraction(ctx context.Context, fabricIndex uint8, nodeID uint64) (*interaction.Interaction, error) {
	set := n.Peers()
	if set == nil {
		return nil, errNoClientStack
	}
	pr, err := set.For(handshake.PeerAddress{FabricIndex: fabricIndex, NodeID: nodeID}) //nolint:contextcheck // the peer runs on the peer set's context, not the caller's
	if err != nil {
		return nil, fmt.Errorf("node: peer %d:%#x: %w", fabricIndex, nodeID, err)
	}
	if _, err := pr.Connect(ctx, peer.ConnectOptions{}); err != nil {
		return nil, fmt.Errorf("node: peer %d:%#x: %w", fabricIndex, nodeID, err)
	}
	return interaction.ForPeer(pr, interaction.Config{Logger: n.logger, Clock: n.cfg.Clock}), nil
}
