// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package clock

// MatterTime is a node's Matter time: the current Matter epoch second
// (Matter §A.2, epoch-s) and whether the node has a time at all — false
// while its UTCTime is null. A node's own is its root
// TimeSynchronization's UTC
// (cluster/core.TimeSynchronization.MatterEpochSeconds), which it hands
// the servers that read Matter time (the Thermostat's suggestions, the
// Valve's AutoCloseTime) as they join its topology.
type MatterTime func() (epochS uint32, synced bool)
