// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behaviors/commodity-tariff/CommodityTariffServerTest.ts
// at 862ad75f.
//
// matter.js's CommodityTariffServer is the generated behavior
// (CommodityTariffServer.ts:17) on a MockServerNode whose every attribute
// starts null (NullState, :11-29); the cases write state with setStateOf
// and read it back, which exercises the managed state's struct defaults
// (matter.js #4386: a struct write fills in only a default the cluster
// supports). The Go server's write is UpdateTariff, chip's tariff update
// (BD-chip-Commodity-ServerChoices): it carries a whole tariff — TariffInfo,
// DayEntries, TariffComponents and TariffPeriods are required
// (ErrTariffMissing) — so the day entry the case writes arrives in the
// smallest tariff chip accepts, and the read is MatterRead of DayEntries.
// The two cases where chip's update validation refuses what matter.js's
// state fills in are missing (the register names why).
package energy_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/cluster/energy"
	"github.com/SukramJ/go-fabric/cluster/spec"
	ct "github.com/SukramJ/go-fabric/cluster/spec/commoditytariff"
)

// smallestTariff is the least tariff the Pricing server accepts around
// entries: one component with a price, one period over every entry, one
// individual day.
func smallestTariff(entries ...energy.TariffDayEntry) energy.Tariff {
	ids := make([]uint32, 0, len(entries))
	for _, e := range entries {
		ids = append(ids, e.DayEntryId)
	}
	price := int64(10)
	return energy.Tariff{
		TariffInfo: &energy.TariffInformation{
			TariffLabel: spec.ValueOf("Tariff"), ProviderName: spec.ValueOf("Provider"),
			Currency: ptrTo(spec.NullOf[ct.Currency]()), BlockMode: spec.ValueOf(ct.BlockModeNoBlock),
		},
		DayEntries: entries,
		TariffComponents: []energy.TariffComponent{{
			TariffComponentId: 1, Threshold: spec.NullOf[int64](),
			Price: ptrTo(spec.ValueOf(ct.TariffPriceStruct{PriceType: ct.TariffPriceTypeStandard, Price: &price})),
		}},
		TariffPeriods:  []energy.TariffPeriod{{Label: spec.ValueOf("Period"), DayEntryIds: ids, TariffComponentIds: []uint32{1}}},
		IndividualDays: []energy.TariffDay{{Date: 1, DayType: ct.DayTypeStandard, DayEntryIds: ids}},
	}
}

// TestCommodityTariffServerParityMatterJS ports the describe
// "CommodityTariffServer" (CommodityTariffServerTest.ts:35-87).
func TestCommodityTariffServerParityMatterJS(t *testing.T) {
	t.Parallel()

	// :36-48.
	t.Run("stores a day entry that omits the randomization fields", func(t *testing.T) {
		t.Parallel()
		srv, err := energy.NewCommodityTariff(energy.CommodityTariffConfig{Features: energy.TariffFeaturePricing})
		if err != nil {
			t.Fatal(err)
		}
		if err := srv.UpdateTariff(smallestTariff(energy.TariffDayEntry{DayEntryId: 1, StartTime: 0})); err != nil {
			t.Fatalf("the update: %v", err)
		}
		entries, ok := read(t, srv, ct.AttrDayEntries).(spec.List[energy.TariffDayEntry])
		if !ok || len(entries) != 1 {
			t.Fatalf("DayEntries %#v", read(t, srv, ct.AttrDayEntries))
		}
		if entries[0].DayEntryId != 1 {
			t.Errorf("DayEntryID %d, want 1", entries[0].DayEntryId)
		}
		if entries[0].RandomizationType != nil {
			t.Errorf("RandomizationType %v, want absent", *entries[0].RandomizationType)
		}
	})
}
