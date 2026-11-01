// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	gencommdef "github.com/SukramJ/go-fabric/cluster/spec/generalcommissioning"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/im"
)

// paseCloseGrace is how long the PASE sessions outlive the
// CommissioningComplete response: Apple sends the command over the
// fabric-adopted PASE session, and the peer's ack of the response
// arrives on it (the delayed teardown RemoveFabric uses too).
const paseCloseGrace = 100 * time.Millisecond

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
	// removed are the fabric-removal participants (jointfabric.go).
	removed []fabricRemovedParticipant
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
// (kFailSafeTimerExpired), then terminates every open PASE session —
// after the response when the expiry runs inside an interaction
// (ArmFailSafe with expiry 0). Mirrors matter.js
// packages/protocol/src/common/FailsafeContext.ts:277-279 (rollback step
// 1: closePaseSession with the current exchange).
func (n *Node) failSafeExpired(ctx context.Context, _ uint8) {
	for _, p := range n.failSafeParticipants() {
		p.OnFailSafeExpired(ctx)
	}
	n.closePASESessions(ctx, 0)
}

// closePASESessions clears every PASE session, including the ones
// AddNOC adopted onto a fabric, once the response of the interaction
// ctx belongs to has been sent (at once without one) and grace has
// elapsed after it.
func (n *Node) closePASESessions(ctx context.Context, grace time.Duration) {
	if n.sec == nil || n.sec.sessions == nil {
		return
	}
	sessions := n.sec.sessions
	closeNow := func() { sessions.ClosePASESessions() }
	run := closeNow
	if grace > 0 {
		clk := n.cfg.Clock
		if clk == nil {
			clk = clock.System{}
		}
		run = func() { clk.AfterFunc(grace, closeNow) }
	}
	if !im.DeferAfterResponse(ctx, run) {
		run()
	}
}

// failSafeCommitted commits every participant at CommissioningComplete
// (kCommissioningComplete).
func (n *Node) failSafeCommitted(ctx context.Context) {
	for _, p := range n.failSafeParticipants() {
		p.OnCommissioningComplete(ctx)
	}
}
