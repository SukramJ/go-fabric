// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package energy

import (
	"sync/atomic"

	"github.com/SukramJ/go-fabric/clock"
)

// nodeTime is the Matter time an energy server reads its epoch-s times
// from: the node's, once the node attaches it as the server joins its
// topology, else its own clock's (clock.MatterTimeOf). connectedhomeip's
// energy servers read the one system clock SetUTCTime sets, through
// System::Clock::GetClock_MatterEpochS — DeviceEnergyManagement's
// forecast and pause times (DeviceEnergyManagementCluster.cpp:449, :698 at
// 6170af84), CommodityTariff's current day and period
// (commodity-tariff-server.cpp:928), EnergyEVSE's targets and session
// times (src/system/SystemClock.cpp:284-306) — so after SetUTCTime a
// controller reads one time from the node (issue 445). Each reads the time
// when a command or its own timer asks for it; none re-evaluates on a
// change of the time, so MatterTimeChanged does nothing, and none refuses
// to start without one; each does what its chip line does when
// GetClock_MatterEpochS fails while the node's time is null.
type nodeTime struct {
	t atomic.Pointer[clock.MatterTime]
}

// AttachMatterTime hands the server the node's Matter time; nil leaves it
// unchanged.
func (n *nodeTime) AttachMatterTime(matterTime clock.MatterTime) {
	if matterTime != nil {
		n.t.Store(&matterTime)
	}
}

// MatterTimeChanged does nothing: the server reads the time when it needs
// it (connectedhomeip's servers ask GetClock_MatterEpochS per command and
// per timer, DeviceEnergyManagementCluster.cpp:449, :698 at 6170af84).
func (n *nodeTime) MatterTimeChanged() {}

// RequiresMatterTime is false: connectedhomeip's energy servers start
// without a time and fail the one command that needs it.
func (n *nodeTime) RequiresMatterTime() bool { return false }

// epochS is the node's Matter time, false while it is null — a node's
// server does not fall back to another clock, as chip's callers test
// GetClock_MatterEpochS's error. A server outside a node, with no time
// attached, reads c (nil: [clock.System]).
func (n *nodeTime) epochS(c clock.Clock) (uint32, bool) {
	if p := n.t.Load(); p != nil {
		return (*p)()
	}
	return clock.MatterTimeOf(c)()
}
