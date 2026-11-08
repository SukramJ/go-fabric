// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package energy

import (
	"errors"
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
// clock's until a node attaches its Matter time, then the node's — after
// SetUTCTime the root's UTC, an hour from the host clock here — and none
// while the node's is null: a node's server does not fall back to another
// clock, as chip's callers test GetClock_MatterEpochS's error.
func TestNodeTime(t *testing.T) {
	t.Parallel()
	host := clocktest.New(time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC))
	hostS, _ := clock.MatterTimeOf(host)()
	var n nodeTime
	if got, ok := n.epochS(host); !ok || got != hostS {
		t.Fatalf("before a node's time: %d %v, want the clock's %d", got, ok, hostS)
	}
	synced := true
	n.AttachMatterTime(func() (uint32, bool) { return hostS + 3600, synced })
	if got, ok := n.epochS(host); !ok || got != hostS+3600 {
		t.Fatalf("with the node's time: %d %v, want %d", got, ok, hostS+3600)
	}
	synced = false
	if _, ok := n.epochS(host); ok {
		t.Fatal("a time while the node's is null")
	}
	if n.RequiresMatterTime() {
		t.Fatal("an energy server requires no time")
	}
}

// TestEnergyServersWithoutTheNodesTime: with the node's time null a
// tariff sync reports ErrNoTime, as UpdateCurrentAttrs fails without a
// time (commodity-tariff-server.cpp:928-934 at 6170af84).
func TestEnergyServersWithoutTheNodesTime(t *testing.T) {
	t.Parallel()
	null := func() (uint32, bool) { return 0, false }
	tariff, err := NewCommodityTariff(CommodityTariffConfig{Features: TariffFeaturePricing})
	if err != nil {
		t.Fatal(err)
	}
	tariff.AttachMatterTime(null)
	if err := tariff.SyncTime(); !errors.Is(err, ErrNoTime) {
		t.Fatalf("SyncTime without a time: %v, want ErrNoTime", err)
	}
}
