// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package clock

import "math"

// MatterTime is a node's Matter time: the current Matter epoch second
// (Matter §A.2, epoch-s) and whether the node has a time at all — false
// while its UTCTime is null. A node's own is its root
// TimeSynchronization's UTC
// (cluster/core.TimeSynchronization.MatterEpochSeconds), which it hands
// the servers that read Matter time (the Thermostat's suggestions, the
// Valve's AutoCloseTime) as they join its topology.
type MatterTime func() (epochS uint32, synced bool)

// matterEpochUnix is the Matter epoch, 2000-01-01T00:00:00Z (Matter §A.2),
// in Unix seconds.
const matterEpochUnix = 946684800

// MatterTimeOf is the Matter time of c (nil: [System]): its current time in
// Matter epoch seconds, false before the epoch or past what epoch-s holds.
// It is the time of a server outside a node whose host clock is its time
// source — the host attaches it (thermo.ThermostatServer.AttachMatterTime,
// valve.Server.AttachMatterTime); a node hands its own, which follows the
// UTC its TimeSynchronization adopted.
func MatterTimeOf(c Clock) MatterTime {
	return func() (uint32, bool) {
		s := Or(c).Now().Unix() - matterEpochUnix
		if s < 0 || s > math.MaxUint32 {
			return 0, false
		}
		return uint32(s), true
	}
}
