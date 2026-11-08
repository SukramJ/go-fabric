// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package energy

import (
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/clock/clocktest"
)

// matterTimeParticipant is the node's participant interface
// (node/matter_time.go), which the three energy servers implement.
type matterTimeParticipant interface {
	AttachMatterTime(clock.MatterTime)
	MatterTimeChanged()
	RequiresMatterTime() bool
}

var (
	_ matterTimeParticipant = (*DeviceEnergyManagementServer)(nil)
	_ matterTimeParticipant = (*CommodityTariffServer)(nil)
	_ matterTimeParticipant = (*EvseServer)(nil)
)

// TestNodeTime (issue 445): an energy server's epoch-s times are its
// clock's until a node attaches its Matter time, then the node's while it
// has one — after SetUTCTime the root's UTC, an hour from the host clock
// here — and the clock's again while the node's is null.
func TestNodeTime(t *testing.T) {
	t.Parallel()
	host := clocktest.New(time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC))
	hostS, _ := clock.MatterTimeOf(host)()
	var n nodeTime
	if got := n.epochS(host); got != hostS {
		t.Fatalf("before a node's time: %d, want the clock's %d", got, hostS)
	}
	synced := true
	n.AttachMatterTime(func() (uint32, bool) { return hostS + 3600, synced })
	if got := n.epochS(host); got != hostS+3600 {
		t.Fatalf("with the node's time: %d, want %d", got, hostS+3600)
	}
	synced = false
	if got := n.epochS(host); got != hostS {
		t.Fatalf("while the node's time is null: %d, want the clock's %d", got, hostS)
	}
	if n.RequiresMatterTime() {
		t.Fatal("an energy server requires no time")
	}
}
