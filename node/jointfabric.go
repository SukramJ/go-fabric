// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"

	"github.com/SukramJ/go-fabric/cluster/jointfabric"
	"github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/endpoint"
)

// nodeServicesParticipant is a cluster server of an endpoint that reads
// the node's fabric table and opens its commissioning window: the Joint
// Fabric administrator (cluster/jointfabric), which connectedhomeip hands
// the Server's FabricTable and CommissioningWindowManager
// (joint-fabric-administrator-server.cpp:192-195 at 6170af84). The node
// attaches each as its endpoint joins the topology, with the fail-safe
// participants (failsafe.go).
type nodeServicesParticipant interface {
	AttachNode(fabrics jointfabric.FabricTable, window wire.WindowController)
}

// fabricRemovedParticipant is a cluster server of an endpoint that drops
// what it holds of a removed fabric: the Joint Fabric servers, which
// connectedhomeip tells through the FabricTable delegate
// (JointFabricDatastore.h:323-338, JointFabricAdministrator.h:60-66).
type fabricRemovedParticipant interface {
	OnFabricRemoved(ctx context.Context, fabricIndex uint8)
}

// attachNodeServices hands the topology's participants the node's store
// and its commissioning window, and records the fabric-removal ones.
func (n *Node) attachNodeServices(t *endpoint.Topology) {
	var removed []fabricRemovedParticipant
	for _, ep := range t.Endpoints {
		if ep == nil {
			continue
		}
		for _, srv := range endpoint.ClusterServers(ep) {
			if p, ok := srv.(nodeServicesParticipant); ok {
				p.AttachNode(n.st, n.br.CommissioningWindow())
			}
			// The node's reach of an announcing commissioner, over its
			// client stack, where the declaration names none (brief 27-B §7).
			if p, ok := srv.(commissionerReachParticipant); ok && n.client != nil {
				p.AttachCommissionerReach(commissionerReach{n})
			}
			if p, ok := srv.(fabricRemovedParticipant); ok {
				removed = append(removed, p)
			}
		}
	}
	n.failSafe.mu.Lock()
	n.failSafe.removed = removed
	n.failSafe.mu.Unlock()
}

// fabricRemovedParticipants tells the topology's participants of a
// removed fabric.
func (n *Node) fabricRemovedParticipants(ctx context.Context, fabricIndex uint8) {
	n.failSafe.mu.Lock()
	ps := append([]fabricRemovedParticipant(nil), n.failSafe.removed...)
	n.failSafe.mu.Unlock()
	for _, p := range ps {
		p.OnFabricRemoved(ctx, fabricIndex)
	}
}
