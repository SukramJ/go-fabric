// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"
	"sync"

	gencommdef "github.com/SukramJ/go-fabric/cluster/spec/generalcommissioning"
	"github.com/SukramJ/go-fabric/endpoint"
)

// failSafeParticipant is a cluster server of an endpoint whose commands
// run under the node's fail-safe: the Thread Border Router Management
// server (cluster/threadbr) and the Thread NetworkCommissioning server
// (cluster/core) — connectedhomeip hands them the FailSafeContext and the
// BreadCrumbTracker and the kFailSafeTimerExpired and
// kCommissioningComplete platform events (ThreadBorderRouterManagementCluster.cpp
// :117-136, :297-323 at 6170af84). The node wires each as its endpoint
// joins the topology and calls it after the root's own fail-safe hooks.
type failSafeParticipant interface {
	AttachFailSafe(armed func() bool, setBreadcrumb func(uint64))
	OnFailSafeExpired(ctx context.Context)
	OnCommissioningComplete(ctx context.Context)
}

// failSafeSync is the topology's participants.
type failSafeSync struct {
	mu           sync.Mutex
	participants []failSafeParticipant
}

// attachFailSafe wires the topology's fail-safe participants to the
// root's GeneralCommissioning: whether the fail-safe is armed, and the
// Breadcrumb a completed command writes.
func (n *Node) attachFailSafe(ctx context.Context, t *endpoint.Topology) {
	gc := n.root.GeneralCommissioning
	if gc == nil {
		return
	}
	var participants []failSafeParticipant
	for _, ep := range t.Endpoints {
		if ep == nil {
			continue
		}
		for _, srv := range endpoint.ClusterServers(ep) {
			p, ok := srv.(failSafeParticipant)
			if !ok {
				continue
			}
			p.AttachFailSafe(gc.FailSafeArmed, func(v uint64) {
				_ = gc.MatterWrite(context.WithoutCancel(ctx), gencommdef.AttrBreadcrumb, v)
			})
			participants = append(participants, p)
		}
	}
	n.failSafe.mu.Lock()
	n.failSafe.participants = participants
	n.failSafe.mu.Unlock()
}

func (n *Node) failSafeParticipants() []failSafeParticipant {
	n.failSafe.mu.Lock()
	defer n.failSafe.mu.Unlock()
	return append([]failSafeParticipant(nil), n.failSafe.participants...)
}

// failSafeExpired reverts every participant after the root's rollback
// (kFailSafeTimerExpired).
func (n *Node) failSafeExpired(ctx context.Context, _ uint8) {
	for _, p := range n.failSafeParticipants() {
		p.OnFailSafeExpired(ctx)
	}
}

// failSafeCommitted commits every participant at CommissioningComplete
// (kCommissioningComplete).
func (n *Node) failSafeCommitted(ctx context.Context) {
	for _, p := range n.failSafeParticipants() {
		p.OnCommissioningComplete(ctx)
	}
}
