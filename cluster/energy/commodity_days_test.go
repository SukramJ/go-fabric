// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package energy_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/energy"
	"github.com/SukramJ/go-fabric/cluster/spec"
	ct "github.com/SukramJ/go-fabric/cluster/spec/commoditytariff"
)

func undatedEntry(id uint32, start uint16) energy.TariffDayEntry {
	return energy.TariffDayEntry{DayEntryId: id, StartTime: start, RandomizationOffset: ptrTo(int16(0)), RandomizationType: ptrTo(ct.DayEntryRandomizationTypeNone)}
}

// TestCommodityTariffDays covers the day lookups the presets do not
// reach: an individual day on today's date, a calendar period starting
// today, day entries without a duration (up to the next entry, across
// midnight, to the end of the day), and GetTariffComponent joining the
// labels of several periods (Server.cpp:669-798, :1128-1181).
func TestCommodityTariffDays(t *testing.T) { //nolint:funlen // one sequence
	t.Parallel()
	srv, clock := newTariff(t)
	today := uint32(1748736000 - 946684800)
	tariff := sample2()
	// Day entries without Duration: 20 at 23:00 runs across midnight to
	// 21's start at 01:00; 21 runs to 22's start; 22 to the end of the day.
	tariff.DayEntries = append(tariff.DayEntries, undatedEntry(20, 1380), undatedEntry(21, 60), undatedEntry(22, 600))
	tariff.IndividualDays = []energy.TariffDay{{Date: today + 10, DayType: ct.DayTypeEvent, DayEntryIds: []uint32{20, 21, 22}}}
	tariff.TariffPeriods = append(tariff.TariffPeriods,
		energy.TariffPeriod{Label: spec.ValueOf(strings.Repeat("L", 100)), DayEntryIds: []uint32{20}, TariffComponentIds: []uint32{10}},
		energy.TariffPeriod{Label: spec.ValueOf(strings.Repeat("M", 100)), DayEntryIds: []uint32{21, 22}, TariffComponentIds: []uint32{10}},
	)
	if err := srv.UpdateTariff(tariff); err != nil {
		t.Fatal(err)
	}
	day := read(t, srv, ct.AttrCurrentDay).(energy.TariffDay)
	if day.DayType != ct.DayTypeEvent || day.Date != today+10 {
		t.Fatalf("CurrentDay %+v", day)
	}
	// 00:00: entry 20's window starts at 23:00 and 21's at 01:00, so no
	// entry covers 00:00; the walk ends on the last entry (22), no next.
	if e := read(t, srv, ct.AttrCurrentDayEntry).(energy.TariffDayEntry); e.DayEntryId != 22 {
		t.Errorf("CurrentDayEntry at 00:00 %+v", e)
	}
	if read(t, srv, ct.AttrNextDayEntry) != nil || read(t, srv, ct.AttrNextDayEntryDate) != nil {
		t.Error("a next day entry after the walk ran out")
	}
	clock.advance(2 * time.Hour) // 02:00: entry 21, 01:00 to 10:00
	if err := srv.SyncTime(); err != nil {
		t.Fatal(err)
	}
	if e := read(t, srv, ct.AttrCurrentDayEntry).(energy.TariffDayEntry); e.DayEntryId != 21 {
		t.Errorf("CurrentDayEntry at 02:00 %+v", e)
	}
	// NextDayEntryDate is CurrentDayEntryDate plus the minutes the current
	// entry has left from now (Server.cpp:1067): 01:00 + 8 h, not 10:00.
	if read(t, srv, ct.AttrNextDayEntryDate) != today+10+(60+480)*60 {
		t.Errorf("NextDayEntryDate %v", read(t, srv, ct.AttrNextDayEntryDate))
	}
	clock.advance(21*time.Hour + 30*time.Minute) // 23:30: entry 20, across midnight
	if err := srv.SyncTime(); err != nil {
		t.Fatal(err)
	}
	if e := read(t, srv, ct.AttrCurrentDayEntry).(energy.TariffDayEntry); e.DayEntryId != 20 {
		t.Errorf("CurrentDayEntry at 23:30 %+v", e)
	}
	// The labels of the periods holding component 10, joined and cut at
	// 128 characters; the day entry ids of the periods reached before the
	// label filled up, sorted (the third period's are not: :1161-1165).
	resp, err := srv.MatterInvoke(context.Background(), ct.CmdGetTariffComponent, ct.GetTariffComponentRequest{TariffComponentId: 10})
	if err != nil {
		t.Fatal(err)
	}
	got := resp.(ct.GetTariffComponentResponse)
	if len(got.Label.Value) != 128 || !strings.HasPrefix(got.Label.Value, "Period 1; "+strings.Repeat("L", 100)) {
		t.Errorf("label %q (%d)", got.Label.Value, len(got.Label.Value))
	}
	if !reflect.DeepEqual(got.DayEntryIds, []uint32{10, 13, 16, 20}) {
		t.Errorf("day entry ids %v", got.DayEntryIds)
	}

	// A calendar period whose StartDate is today is found on its day.
	srv2, _ := newTariff(t)
	tariff = sample2()
	tariff.StartDate = ptrTo(today)
	tariff.CalendarPeriods = []energy.TariffCalendarPeriod{{StartDate: spec.ValueOf(today), DayPatternIds: []uint32{0}}}
	if err := srv2.UpdateTariff(tariff); err != nil {
		t.Fatal(err)
	}
	if d, ok := read(t, srv2, ct.AttrCurrentDay).(energy.TariffDay); !ok || d.Date != today {
		t.Errorf("calendar day %v", read(t, srv2, ct.AttrCurrentDay))
	}
	// A day entry no tariff period references leaves the derived day
	// entry unset (UpdateTariffComponentAttrsDayEntryById, NOT_FOUND).
	srv3, _ := newTariff(t)
	tariff = sample2()
	extra := undatedEntry(5, 0)
	extra.Duration = ptrTo(uint16(10))
	tariff.DayEntries = append(tariff.DayEntries, extra)
	tariff.DayPatterns[0].DayEntryIds = append([]uint32{5}, tariff.DayPatterns[0].DayEntryIds...)
	if err := srv3.UpdateTariff(tariff); err != nil {
		t.Fatal(err)
	}
	if read(t, srv3, ct.AttrCurrentDayEntry) != nil {
		t.Error("a day entry without a period became current")
	}
	if err := srv3.SyncTime(); !errors.Is(err, energy.ErrTariffUnknownID) {
		t.Errorf("SyncTime: %v", err)
	}
	// No current day: the only calendar period starts another day.
	srv4, clock4 := newTariff(t)
	tariff = sample2()
	tariff.StartDate = ptrTo(today)
	tariff.CalendarPeriods = []energy.TariffCalendarPeriod{{StartDate: spec.ValueOf(today), DayPatternIds: []uint32{0}}}
	if err := srv4.UpdateTariff(tariff); err != nil {
		t.Fatal(err)
	}
	clock4.advance(48 * time.Hour)
	if err := srv4.SyncTime(); err == nil {
		t.Error("SyncTime found a day two days after the only calendar period")
	}
}

// TestCommodityTariffNoRandomization serves the RNDM attributes only
// with the feature, ignores the randomization fields without it, and
// runs the list-length and date-order rules.
func TestCommodityTariffNoRandomization(t *testing.T) {
	t.Parallel()
	srv, err := energy.NewCommodityTariff(energy.CommodityTariffConfig{
		Features: allTariffFeatures &^ energy.TariffFeatureRandomization,
		Now:      func() time.Time { return time.Unix(1748736000, 0) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := srv.MatterRead(ct.AttrDefaultRandomizationType); ok {
		t.Error("DefaultRandomizationType served without RNDM")
	}
	tariff := sample2()
	tariff.DayEntries[0].RandomizationType = nil
	if err := srv.UpdateTariff(tariff); err != nil {
		t.Errorf("an entry without randomization under no RNDM: %v", err)
	}
	if err := srv.ClearTariff(); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		mutate func(*energy.Tariff)
		want   error
	}{
		"dates not increasing": {func(x *energy.Tariff) {
			x.IndividualDays = []energy.TariffDay{{Date: 5, DayEntryIds: []uint32{99}}, {Date: 5, DayEntryIds: []uint32{98}}}
		}, energy.ErrTariffInvalid},
		"51 days":     {func(x *energy.Tariff) { x.IndividualDays = make([]energy.TariffDay, 51) }, energy.ErrTariffListLength},
		"29 patterns": {func(x *energy.Tariff) { x.DayPatterns = make([]energy.TariffDayPattern, 29) }, energy.ErrTariffListLength},
		"673 entries": {func(x *energy.Tariff) { x.DayEntries = make([]energy.TariffDayEntry, 673) }, energy.ErrTariffListLength},
		"673 components": {func(x *energy.Tariff) {
			x.TariffComponents = make([]energy.TariffComponent, 673)
		}, energy.ErrTariffListLength},
		"673 periods": {func(x *energy.Tariff) { x.TariffPeriods = make([]energy.TariffPeriod, 673) }, energy.ErrTariffListLength},
		"later calendar period at 0": {func(x *energy.Tariff) {
			x.DayPatterns = append(x.DayPatterns, energy.TariffDayPattern{DayPatternId: 1, DaysOfWeek: 0x7F, DayEntryIds: []uint32{99}})
			x.CalendarPeriods = append(x.CalendarPeriods, energy.TariffCalendarPeriod{StartDate: spec.ValueOf(uint32(5)), DayPatternIds: []uint32{1}},
				energy.TariffCalendarPeriod{StartDate: spec.ValueOf(uint32(5)), DayPatternIds: []uint32{2}})
		}, energy.ErrTariffInvalid},
		"first calendar period undated under a set StartDate": {func(x *energy.Tariff) {
			x.StartDate = ptrTo(uint32(1748736000 - 946684800 + 60))
		}, energy.ErrTariffInvalid},
	} {
		tariff := sample2()
		c.mutate(&tariff)
		if err := srv.UpdateTariff(tariff); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", name, err, c.want)
		}
	}
}
