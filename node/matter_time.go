// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"slices"
	"sync"

	"github.com/SukramJ/go-fabric/clock"
	tsdef "github.com/SukramJ/go-fabric/cluster/spec/timesynchronization"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/endpoint"
)

// matterTimeParticipant is a cluster server of an endpoint that reads the
// node's Matter time: the Thermostat with ThermostatSuggestions (TSUGGEST,
// cluster/thermo) and the Valve with TimeSync (TS, cluster/valve) — no
// other server of the module reads it. The node hands each the root
// TimeSynchronization's UTC in epoch seconds
// (core.TimeSynchronization.MatterEpochSeconds) as its endpoint joins the
// topology, and tells each when that UTC moves — SetUTCTime, the trusted
// time source, the NTP fallback — so a controller reads one time from
// the node, as connectedhomeip's servers read the one system clock
// SetUTCTime sets (SystemClock.cpp:284-306, TimeSynchronizationCluster.cpp:
// 856-872 at 6170af84). A node without the root cluster
// ([TimeSynchronization].Off) has no UTC and hands none.
type matterTimeParticipant interface {
	// AttachMatterTime hands the server the node's Matter time.
	AttachMatterTime(matterTime clock.MatterTime)
	// MatterTimeChanged tells the server the time moved: the Valve sets
	// AutoCloseTime anew from RemainingDuration, the Thermostat
	// re-evaluates its suggestions.
	MatterTimeChanged()
}

// matterTimeSync is the topology's Matter time participants, the ones
// told of a change of the root's UTC.
type matterTimeSync struct {
	mu           sync.Mutex
	participants []matterTimeParticipant
}

// attachMatterTime hands the topology's Matter time participants the
// root TimeSynchronization's UTC and records them.
func (n *Node) attachMatterTime(t *endpoint.Topology) {
	ts := n.root.TimeSynchronization
	var ps []matterTimeParticipant
	for _, ep := range t.Endpoints {
		if ep == nil {
			continue
		}
		for _, srv := range endpoint.ClusterServers(ep) {
			p, ok := srv.(matterTimeParticipant)
			if !ok || ts == nil {
				continue
			}
			p.AttachMatterTime(ts.MatterEpochSeconds)
			ps = append(ps, p)
		}
	}
	n.matterTime.mu.Lock()
	n.matterTime.participants = ps
	n.matterTime.mu.Unlock()
}

// watchMatterTime tells the participants whenever the root's UTCTime
// moves — every path that moves it reports UTCTime
// (core.TimeSynchronization's adoption of a time), as connectedhomeip's
// SetUTCTime reports it and calls the delegate's
// UTCTimeAvailabilityChanged (TimeSynchronizationCluster.cpp:856-872, the
// call :865 at 6170af84), which the reference application hands to the
// valve (examples/all-clusters-app/linux/ValveControlDelegate.cpp:70-73).
func (n *Node) watchMatterTime() {
	ts := n.root.TimeSynchronization
	if ts == nil {
		return
	}
	ts.OnAttributeChange(func(c contract.Change) {
		if !slices.Contains(c.Attributes, tsdef.AttrUtcTime) {
			return
		}
		n.matterTime.mu.Lock()
		ps := slices.Clone(n.matterTime.participants)
		n.matterTime.mu.Unlock()
		for _, p := range ps {
			p.MatterTimeChanged()
		}
	})
}
