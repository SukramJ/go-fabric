// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package energy_test

import (
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/cluster/energy"
	ct "github.com/SukramJ/go-fabric/cluster/spec/commoditytariff"
)

// TestCommodityTariffDeclaredClock (issue 491): with ClockDeclared the
// tariff's current day entry follows its clock — the device's declared
// TimeSource — although the node attached its Matter time, also on a
// SyncTime after the clock advanced; without it the node's time is read
// (issue 445).
func TestCommodityTariffDeclaredClock(t *testing.T) {
	t.Parallel()
	day := time.Date(2024, 12, 1, 0, 0, 0, 0, time.UTC)
	dayS, _ := clock.MatterTimeOf(clocktest.New(day))()
	tariff := smallestTariff(
		energy.TariffDayEntry{DayEntryId: 1, StartTime: 0}, energy.TariffDayEntry{DayEntryId: 2, StartTime: 240},
		energy.TariffDayEntry{DayEntryId: 3, StartTime: 0}, energy.TariffDayEntry{DayEntryId: 4, StartTime: 240})
	tariff.TariffUnit, tariff.StartDate = ptrTo(ct.TariffUnitKWh), ptrTo(uint32(0))
	tariff.TariffPeriods = []energy.TariffPeriod{
		{Label: tariff.TariffPeriods[0].Label, DayEntryIds: []uint32{1, 2}, TariffComponentIds: []uint32{1}},
		{Label: tariff.TariffPeriods[0].Label, DayEntryIds: []uint32{3, 4}, TariffComponentIds: []uint32{1}},
	}
	tariff.IndividualDays = []energy.TariffDay{
		{Date: dayS - 86400, DayType: ct.DayTypeStandard, DayEntryIds: []uint32{1, 2}},
		{Date: dayS, DayType: ct.DayTypeStandard, DayEntryIds: []uint32{3, 4}},
	}
	// The node's time: the day before, in the second entry.
	nodeS := dayS - 86400 + 5*3600
	nodeTime := func() (uint32, bool) { return nodeS, true }

	for _, declared := range []bool{true, false} {
		fake := clocktest.New(day.Add(time.Hour))
		srv, err := energy.NewCommodityTariff(energy.CommodityTariffConfig{
			Features: energy.TariffFeaturePricing, Clock: fake, ClockDeclared: declared,
		})
		if err != nil {
			t.Fatal(err)
		}
		srv.AttachMatterTime(nodeTime)
		if err := srv.UpdateTariff(tariff); err != nil {
			t.Fatalf("declared %v: the update: %v", declared, err)
		}
		wantDate := map[bool]uint32{true: dayS, false: nodeS - 3600}[declared]
		if got := read(t, srv, ct.AttrCurrentDayEntryDate); got != wantDate {
			t.Errorf("declared %v: CurrentDayEntryDate %v, want %d", declared, got, wantDate)
		}
		fake.Advance(4 * time.Hour)
		if err := srv.SyncTime(); err != nil {
			t.Fatal(err)
		}
		wantDate = map[bool]uint32{true: dayS + 240*60, false: nodeS - 3600}[declared]
		if got := read(t, srv, ct.AttrCurrentDayEntryDate); got != wantDate {
			t.Errorf("declared %v: after the advance CurrentDayEntryDate %v, want %d", declared, got, wantDate)
		}
		if got, ok := read(t, srv, ct.AttrCurrentDay).(energy.TariffDay); !ok || got.Date != map[bool]uint32{true: dayS, false: dayS - 86400}[declared] {
			t.Errorf("declared %v: CurrentDay %v", declared, got)
		}
	}
}
