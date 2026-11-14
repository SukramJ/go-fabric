// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"
	"log/slog"
	"reflect"

	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/endpoint"
)

// fabricStateServers returns every server of the node that holds state
// keyed by fabric index ([contract.FabricScopedState]): the root's, the
// Aggregator's and those of every endpoint of each of ts, each server once.
// A nil topology has no endpoints beyond the root's and the Aggregator's
// servers.
func (n *Node) fabricStateServers(ts ...*endpoint.Topology) []contract.FabricScopedState {
	var out []contract.FabricScopedState
	seen := map[any]bool{}
	add := func(servers []contract.ClusterServer) {
		for _, srv := range servers {
			s, ok := srv.(contract.FabricScopedState)
			if !ok {
				continue
			}
			if reflect.TypeOf(s).Comparable() {
				if seen[s] {
					continue
				}
				seen[s] = true
			}
			out = append(out, s)
		}
	}
	if n.root != nil {
		add(n.root.Servers())
	}
	add(n.aggServers)
	for _, t := range ts {
		if t == nil {
			continue
		}
		for _, ep := range t.Endpoints {
			if ep != nil {
				add(endpoint.ClusterServers(ep))
			}
		}
	}
	return out
}

// dropFabricState is the removal contract's fan-out: every server of the
// root, the Aggregator and every endpoint of the served topology drops
// what it holds for fabricIndex (matter.js limitNodeDataToAllowedFabrics
// on fabrics.events.deleting, node/server/ServerEnvironment.ts:188-196 at
// db021da0). An endpoint absent now is reconciled when it joins
// ([Node.reconcileFabricState]).
//
// It walks the topology the node observed last and the one the bridge
// serves: they differ while a snapshot the bridge goes on to refuse has
// been observed, and the bridge then keeps serving the topology it had
// (review II, R2-P2-dr-node-03). The binding manager reads the observed
// one only.
func (n *Node) dropFabricState(ctx context.Context, fabricIndex uint8) {
	var served *endpoint.Topology
	if n.br != nil {
		served = n.br.Topology()
	}
	for _, s := range n.fabricStateServers(nodeTopology{n}.topology(), served) {
		if err := s.MatterDropFabric(ctx, fabricIndex); err != nil {
			n.logger.Warn("fabric.state.drop_failed", slog.Int("fabric_index", int(fabricIndex)), slog.Any("err", err))
		}
	}
}

// reconcileFabricState drops, on every server of the root, the Aggregator
// and every endpoint of t, the state held for a fabric index the fabric
// table does not hold: what a run that stopped between the store's
// removal and the fan-out left, what an endpoint absent at a removal kept,
// and what the start-up rollback of an uncommitted fabric left. It runs
// before the bridge serves t, at start and whenever the topology is
// assembled again. Mirrors matter.js
// limitEndpointAttributeDataToAllowedFabrics when an endpoint initialises
// (node/server/ServerEndpointInitializer.ts:129-136 at db021da0), which
// skips a node without fabrics; this module also reconciles that case, as
// it does not erase the node when its last fabric goes
// (BD-Matter-LastFabricReopenWithoutErase).
//
// The held indices are read before the table: state a server holds was
// written for a fabric the table already held, so an index the table no
// longer holds afterwards is a removed fabric's, and a fabric added
// meanwhile is not dropped.
func (n *Node) reconcileFabricState(ctx context.Context, t *endpoint.Topology) {
	servers := n.fabricStateServers(t)
	held := make([][]uint8, len(servers))
	holds := false
	for i, s := range servers {
		held[i] = s.MatterHeldFabrics()
		holds = holds || len(held[i]) > 0
	}
	if !holds {
		return
	}
	fabrics, err := n.st.ListFabrics(ctx)
	if err != nil {
		n.logger.Warn("fabric.state.reconcile_failed", slog.Any("err", err))
		return
	}
	live := make(map[uint8]bool, len(fabrics))
	for i := range fabrics {
		live[fabrics[i].FabricIndex] = true
	}
	for i, s := range servers {
		for _, f := range held[i] {
			if live[f] {
				continue
			}
			if err := s.MatterDropFabric(ctx, f); err != nil {
				n.logger.Warn("fabric.state.drop_failed", slog.Int("fabric_index", int(f)), slog.Any("err", err))
			}
		}
	}
}
