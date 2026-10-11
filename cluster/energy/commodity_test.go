// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package energy_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/energy"
	"github.com/SukramJ/go-fabric/cluster/spec"
	commtr "github.com/SukramJ/go-fabric/cluster/spec/commoditymetering"
	cp "github.com/SukramJ/go-fabric/cluster/spec/commodityprice"
	ct "github.com/SukramJ/go-fabric/cluster/spec/commoditytariff"
	egc "github.com/SukramJ/go-fabric/cluster/spec/electricalgridconditions"
	"github.com/SukramJ/go-fabric/im"
)

func ptrTo[T any](v T) *T { return &v }

func read(t *testing.T, srv interface{ MatterRead(uint32) (any, bool) }, attr uint32) any {
	t.Helper()
	v, ok := srv.MatterRead(attr)
	if !ok {
		t.Fatalf("attribute 0x%04X not served", attr)
	}
	return v
}

// The PriceUpdate trigger's price (connectedhomeip examples/
// energy-gateway-app/commodity-price/src/CommodityPriceEventTriggers.cpp:
// 34-76): 15.916 p/kWh, level 3, "Medium", ExVAT and VAT components.
func triggerPrice(start uint32) energy.CommodityPrice {
	amount := int64(15916)
	return energy.CommodityPrice{
		PeriodStart: start, PeriodEnd: spec.ValueOf(start + 30*60),
		Price: &amount, PriceLevel: ptrTo(int16(3)), Description: ptrTo("Medium"),
		Components: &[]energy.CommodityPriceComponent{
			{Price: amount * 95 / 100, Source: cp.TariffPriceTypeStandard, Description: ptrTo("ExVAT")},
			{Price: amount * 5 / 100, Source: cp.TariffPriceTypeStandard, Description: ptrTo("VAT")},
		},
	}
}

func TestCommodityPrice(t *testing.T) {
	t.Parallel()
	srv, err := energy.NewCommodityPrice(energy.CommodityPriceConfig{
		Features: energy.CommodityPriceFeatureForecasting, DetailedPrice: true, DetailedForecast: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	ev := &energyEvents{}
	srv.SetMatterEventEmitter(ev)
	srv.SetEndpoint(2)
	// Starting values (CommodityPriceCluster.h:174-181).
	if v := read(t, srv, cp.AttrTariffUnit); v != uint8(cp.TariffUnitKWh) {
		t.Errorf("TariffUnit %v", v)
	}
	if v := read(t, srv, cp.AttrCurrentPrice); v != nil {
		t.Errorf("CurrentPrice %v", v)
	}
	if err := srv.SetCurrency(&energy.PriceCurrency{Currency: 826, DecimalPoints: 5}); err != nil {
		t.Fatal(err)
	}
	wantStatus(t, srv.SetCurrency(&energy.PriceCurrency{Currency: 1000}), im.StatusConstraintError)
	if err := srv.SetCurrency(nil); err != nil || read(t, srv, cp.AttrCurrency) != nil {
		t.Errorf("SetCurrency(nil): %v", err)
	}
	wantStatus(t, srv.SetTariffUnit(5), im.StatusConstraintError)
	if err := srv.SetTariffUnit(cp.TariffUnitKVAh); err != nil {
		t.Fatal(err)
	}

	// CurrentPrice: the attribute and the event without description and
	// components, the full price for GetDetailedPriceRequest.
	price := triggerPrice(1000)
	if err := srv.SetCurrentPrice(&price); err != nil {
		t.Fatal(err)
	}
	stored := read(t, srv, cp.AttrCurrentPrice).(energy.CommodityPrice)
	if stored.Description != nil || stored.Components != nil || *stored.Price != 15916 {
		t.Errorf("CurrentPrice attribute %+v", stored)
	}
	events := ev.take()
	if len(events) != 1 || events[0].id != cp.EventPriceChange {
		t.Fatalf("events %+v", events)
	}
	if e := events[0].data.(cp.PriceChangeEvent); e.CurrentPrice.Null || e.CurrentPrice.Value.Description != nil {
		t.Errorf("PriceChange %+v", e)
	}
	resp, err := srv.MatterInvoke(context.Background(), cp.CmdGetDetailedPriceRequest, cp.GetDetailedPriceRequestRequest{Details: cp.CommodityPriceDetailDescription})
	if err != nil {
		t.Fatal(err)
	}
	if got := resp.(cp.GetDetailedPriceResponse).CurrentPrice.Value; *got.Description != "Medium" || got.Components != nil {
		t.Errorf("detailed price %+v", got)
	}
	_, err = srv.MatterInvoke(context.Background(), cp.CmdGetDetailedPriceRequest, &cp.GetDetailedPriceRequestRequest{Details: 4})
	wantStatus(t, err, im.StatusInvalidCommand)
	if srv.CurrentPrice() == nil || *srv.CurrentPrice().Description != "Medium" {
		t.Error("CurrentPrice() lost the description")
	}

	// ValidatePrice (CommodityPriceCluster.cpp:91-112).
	for name, mutate := range map[string]func(*energy.CommodityPrice){
		"end before start": func(p *energy.CommodityPrice) { p.PeriodEnd = spec.ValueOf(uint32(10)) },
		"no price":         func(p *energy.CommodityPrice) { p.Price, p.PriceLevel = nil, nil },
		"description":      func(p *energy.CommodityPrice) { p.Description = ptrTo(strings.Repeat("x", 33)) },
		"components": func(p *energy.CommodityPrice) {
			p.Components = ptrTo(make([]energy.CommodityPriceComponent, 11))
		},
		"component description": func(p *energy.CommodityPrice) {
			p.Components = &[]energy.CommodityPriceComponent{{Description: ptrTo(strings.Repeat("x", 33))}}
		},
	} {
		bad := triggerPrice(1000)
		mutate(&bad)
		if err := srv.SetCurrentPrice(&bad); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if err := srv.SetCurrentPrice(nil); err != nil || read(t, srv, cp.AttrCurrentPrice) != nil {
		t.Errorf("SetCurrentPrice(nil): %v", err)
	}
	resp, _ = srv.MatterInvoke(context.Background(), cp.CmdGetDetailedPriceRequest, cp.GetDetailedPriceRequestRequest{})
	if !resp.(cp.GetDetailedPriceResponse).CurrentPrice.Null {
		t.Error("detailed price of a null CurrentPrice")
	}

	// PriceForecast and GetDetailedForecastRequest.
	forecast := []energy.CommodityPrice{triggerPrice(0), triggerPrice(1800)}
	if err := srv.SetForecast(forecast); err != nil {
		t.Fatal(err)
	}
	if got := read(t, srv, cp.AttrPriceForecast).(spec.List[energy.CommodityPrice]); len(got) != 2 || got[1].Components != nil {
		t.Errorf("PriceForecast %+v", got)
	}
	resp, err = srv.MatterInvoke(context.Background(), cp.CmdGetDetailedForecastRequest,
		cp.GetDetailedForecastRequestRequest{Details: cp.CommodityPriceDetailDescription | cp.CommodityPriceDetailComponents})
	if err != nil {
		t.Fatal(err)
	}
	if got := resp.(cp.GetDetailedForecastResponse).PriceForecast; len(got) != 2 || len(*got[0].Components) != 2 {
		t.Errorf("detailed forecast %+v", got)
	}
	_, err = srv.MatterInvoke(context.Background(), cp.CmdGetDetailedForecastRequest, &cp.GetDetailedForecastRequestRequest{Details: 0x80})
	wantStatus(t, err, im.StatusInvalidCommand)
	_, err = srv.MatterInvoke(context.Background(), cp.CmdGetDetailedForecastRequest, "junk")
	wantStatus(t, err, im.StatusInvalidCommand)
	wantStatus(t, srv.SetForecast(make([]energy.CommodityPrice, 57)), im.StatusConstraintError)
	if err := srv.SetForecast([]energy.CommodityPrice{{}}); err == nil {
		t.Error("a forecast entry without price accepted")
	}

	plain, err := energy.NewCommodityPrice(energy.CommodityPriceConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if err := plain.SetForecast(nil); err == nil {
		t.Error("a forecast without FORE accepted")
	}
	_, err = plain.MatterInvoke(context.Background(), cp.CmdGetDetailedPriceRequest, cp.GetDetailedPriceRequestRequest{})
	wantStatus(t, err, im.StatusUnsupportedCommand)
	if plain.MatterDataVersion() == 0 {
		t.Log("data version starts at 0")
	}
	unsub := plain.OnMatterAttributesChanged(func([]uint32) {})
	unsub()
	wantStatus(t, plain.MatterWrite(context.Background(), cp.AttrTariffUnit, uint64(1)), im.StatusUnsupportedWrite)
}

func TestElectricalGridConditions(t *testing.T) {
	t.Parallel()
	srv, err := energy.NewElectricalGridConditions(energy.GridConditionsConfig{Features: energy.GridConditionsFeatureForecasting})
	if err != nil {
		t.Fatal(err)
	}
	ev := &energyEvents{}
	srv.SetMatterEventEmitter(ev)
	srv.SetEndpoint(4)
	if err := srv.SetLocalGenerationAvailable(ptrTo(true)); err != nil || read(t, srv, egc.AttrLocalGenerationAvailable) != true {
		t.Errorf("LocalGenerationAvailable: %v", err)
	}
	if err := srv.SetLocalGenerationAvailable(nil); err != nil || read(t, srv, egc.AttrLocalGenerationAvailable) != nil {
		t.Errorf("LocalGenerationAvailable null: %v", err)
	}
	if err := srv.MatterWrite(context.Background(), egc.AttrLocalGenerationAvailable, false); err != nil {
		t.Errorf("write LocalGenerationAvailable: %v", err)
	}
	// The CurrentConditionsUpdate trigger's conditions
	// (ElectricalGridConditionsEventTriggers.cpp:31-60).
	cond := energy.GridConditions{
		PeriodStart: 100, PeriodEnd: spec.ValueOf(uint32(1900)),
		GridCarbonIntensity: 230, GridCarbonLevel: egc.ThreeLevelMedium, LocalCarbonLevel: egc.ThreeLevelLow,
	}
	if err := srv.SetCurrentConditions(&cond); err != nil {
		t.Fatal(err)
	}
	if got := ev.take(); len(got) != 1 || got[0].id != egc.EventCurrentConditionsChanged {
		t.Errorf("events %+v", got)
	}
	for name, bad := range map[string]energy.GridConditions{
		"grid level":  {GridCarbonLevel: 3},
		"local level": {LocalCarbonLevel: 3},
		"period":      {PeriodStart: 10, PeriodEnd: spec.ValueOf(uint32(5))},
	} {
		wantStatus(t, srv.SetCurrentConditions(&bad), im.StatusConstraintError)
		_ = name
	}
	if err := srv.SetCurrentConditions(nil); err != nil || read(t, srv, egc.AttrCurrentConditions) != nil {
		t.Errorf("null conditions: %v", err)
	}
	if e := ev.take(); len(e) != 1 || !e[0].data.(egc.CurrentConditionsChangedEvent).CurrentConditions.Null {
		t.Errorf("null conditions event %+v", e)
	}
	if err := srv.SetForecastConditions([]energy.GridConditions{cond}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, srv, egc.AttrForecastConditions).(spec.List[energy.GridConditions]); len(got) != 1 {
		t.Errorf("ForecastConditions %v", got)
	}
	plain, err := energy.NewElectricalGridConditions(energy.GridConditionsConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if err := plain.SetForecastConditions(nil); err == nil {
		t.Error("ForecastConditions without FORE accepted")
	}
	_, err = plain.MatterInvoke(context.Background(), 0, nil)
	wantStatus(t, err, im.StatusUnsupportedCommand)
	plain.OnMatterAttributesChanged(func([]uint32) {})()
	_ = plain.MatterDataVersion()
}

func TestCommodityMetering(t *testing.T) {
	t.Parallel()
	srv, err := energy.NewCommodityMetering(energy.CommodityMeteringConfig{})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []uint32{commtr.AttrMeteredQuantity, commtr.AttrMeteredQuantityTimestamp, commtr.AttrTariffUnit, commtr.AttrMaximumMeteredQuantities} {
		if v := read(t, srv, id); v != nil {
			t.Errorf("0x%04X starts %v", id, v)
		}
	}
	quantities := []energy.MeteredQuantity{{TariffComponentIds: []uint32{1011, 1012}, Quantity: 2}}
	if err := srv.SetMeteredQuantity(quantities); !errors.Is(err, energy.ErrMeteringNoMaximum) {
		t.Errorf("without maximum: %v", err)
	}
	if err := srv.SetMaximumMeteredQuantities(ptrTo(uint16(0))); !errors.Is(err, energy.ErrMeteringMaximum) {
		t.Errorf("maximum 0: %v", err)
	}
	if err := srv.SetMaximumMeteredQuantities(ptrTo(uint16(1))); err != nil {
		t.Fatal(err)
	}
	if err := srv.SetMeteredQuantity(append(quantities, quantities...)); !errors.Is(err, energy.ErrMeteringListLength) {
		t.Errorf("too long: %v", err)
	}
	long := make([]uint32, 130)
	if err := srv.SetMeteredQuantity([]energy.MeteredQuantity{{TariffComponentIds: long, Quantity: 1}}); err != nil {
		t.Fatal(err)
	}
	if got := srv.MeteredQuantity(); len(got) != 1 || len(got[0].TariffComponentIds) != 128 {
		t.Errorf("IDs not cut to 128: %d", len(got[0].TariffComponentIds))
	}
	if err := srv.SetMeteredQuantity(quantities); err != nil {
		t.Fatal(err)
	}
	version := srv.MatterDataVersion()
	if err := srv.SetMeteredQuantity(quantities); err != nil || srv.MatterDataVersion() != version {
		t.Errorf("an equal list changed the data version: %v", err)
	}
	if err := srv.SetMeteredQuantity([]energy.MeteredQuantity{}); err != nil || len(srv.MeteredQuantity()) != 0 || srv.MeteredQuantity() == nil {
		t.Errorf("empty list: %v %v", err, srv.MeteredQuantity())
	}
	if err := srv.SetMeteredQuantity(nil); err != nil || srv.MeteredQuantity() != nil {
		t.Errorf("null: %v", err)
	}
	if err := srv.SetMeteredQuantityTimestamp(ptrTo(uint32(5))); err != nil || *srv.MeteredQuantityTimestamp() != 5 {
		t.Errorf("timestamp: %v", err)
	}
	if err := srv.SetMeteredQuantityTimestamp(nil); err != nil || srv.MeteredQuantityTimestamp() != nil {
		t.Errorf("null timestamp: %v", err)
	}
	wantStatus(t, srv.SetTariffUnit(ptrTo(commtr.TariffUnitEnum(2))), im.StatusConstraintError)
	if err := srv.SetTariffUnit(ptrTo(commtr.TariffUnitKVAh)); err != nil || *srv.TariffUnit() != commtr.TariffUnitKVAh {
		t.Errorf("TariffUnit: %v", err)
	}
	if err := srv.SetTariffUnit(nil); err != nil || srv.TariffUnit() != nil {
		t.Errorf("null TariffUnit: %v", err)
	}
	if *srv.MaximumMeteredQuantities() != 1 {
		t.Error("MaximumMeteredQuantities")
	}
	if err := srv.SetMaximumMeteredQuantities(nil); err != nil || srv.MaximumMeteredQuantities() != nil {
		t.Errorf("null maximum: %v", err)
	}
}

// tariffClock is a settable clock.
type tariffClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *tariffClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *tariffClock) advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

const allTariffFeatures = energy.TariffFeaturePricing | energy.TariffFeatureFriendlyCredit | energy.TariffFeatureAuxiliaryLoad |
	energy.TariffFeaturePeakPeriod | energy.TariffFeaturePowerThreshold | energy.TariffFeatureRandomization

// tariffComponent is a TariffComponentStruct as the energy-gateway app's
// samples write one (examples/energy-gateway-app/commodity-tariff/include/
// CommodityTariffSamples.h:279-329).
func tariffComponent(id uint32, price int64, level int16, threshold spec.Nullable[int64], label string) energy.TariffComponent {
	return energy.TariffComponent{
		TariffComponentId: id,
		Price:             ptrTo(spec.ValueOf(ct.TariffPriceStruct{PriceType: ct.TariffPriceTypeStandard, Price: &price, PriceLevel: &level})),
		FriendlyCredit:    ptrTo(false),
		AuxiliaryLoad:     &ct.AuxiliaryLoadSwitchSettingsStruct{Number: 1},
		PeakPeriod:        &ct.PeakPeriodStruct{Severity: ct.PeakPeriodSeverityLow, PeakPeriod: 1},
		PowerThreshold:    &ct.PowerThresholdStruct{PowerThreshold: ptrTo(int64(2400000)), ApparentPowerThreshold: ptrTo(int64(120)), PowerThresholdSource: spec.ValueOf(ct.PowerThresholdSourceContract)},
		Threshold:         threshold,
		Label:             ptrTo(spec.ValueOf(label)),
		Predicted:         ptrTo(false),
	}
}

// sample2 is the app's second preset (CommodityTariffSamples.h:211-352,
// :396-414): seven 4-hour day entries on every day, three components with
// thresholds under BlockMode Combined, three periods.
func sample2() energy.Tariff {
	entry := func(id uint32, start, duration uint16) energy.TariffDayEntry {
		return energy.TariffDayEntry{DayEntryId: id, StartTime: start, Duration: &duration, RandomizationOffset: ptrTo(int16(0)), RandomizationType: ptrTo(ct.DayEntryRandomizationTypeNone)}
	}
	period := func(label string, entries []uint32, components ...uint32) energy.TariffPeriod {
		return energy.TariffPeriod{Label: spec.ValueOf(label), DayEntryIds: entries, TariffComponentIds: components}
	}
	c3 := tariffComponent(30, 50, 0, spec.ValueOf(int64(320)), "Tariff Component 3")
	c3.FriendlyCredit, c3.Predicted = ptrTo(true), ptrTo(true)
	return energy.Tariff{
		TariffUnit: ptrTo(ct.TariffUnitKWh), StartDate: ptrTo(uint32(0)),
		DefaultRandomizationOffset: ptrTo(int16(0)), DefaultRandomizationType: ptrTo(ct.DayEntryRandomizationTypeNone),
		TariffInfo: &energy.TariffInformation{
			TariffLabel: spec.ValueOf("Full Tariff Two"), ProviderName: spec.ValueOf("Example Provider"),
			Currency: ptrTo(spec.ValueOf(ct.Currency{Currency: 200, DecimalPoints: 1})), BlockMode: spec.ValueOf(ct.BlockModeCombined),
		},
		DayEntries: []energy.TariffDayEntry{
			entry(10, 0, 240), entry(11, 240, 240), entry(12, 480, 240), entry(13, 720, 240),
			entry(14, 960, 240), entry(15, 1200, 240), entry(16, 1440, 60),
		},
		DayPatterns: []energy.TariffDayPattern{{DayPatternId: 0, DaysOfWeek: 0x7F, DayEntryIds: []uint32{10, 11, 12, 13, 14, 15, 16}}},
		TariffComponents: []energy.TariffComponent{
			tariffComponent(10, 15, 1, spec.ValueOf(int64(120)), "Tariff Component 1"),
			tariffComponent(20, 20, 0, spec.ValueOf(int64(240)), "Tariff Component 2"),
			c3,
		},
		TariffPeriods: []energy.TariffPeriod{
			period("Period 1", []uint32{10, 13, 16}, 10), period("Period 2", []uint32{11, 14}, 20), period("Period 3", []uint32{12, 15}, 30),
		},
		CalendarPeriods: []energy.TariffCalendarPeriod{{StartDate: spec.ValueOf(uint32(0)), DayPatternIds: []uint32{0}}},
	}
}

func newTariff(t *testing.T) (*energy.CommodityTariffServer, *tariffClock) {
	t.Helper()
	// The preset's TariffTestTimestamp, 01-Jun-2025 00:00 UTC
	// (CommodityTariffSamples.h:413).
	clock := &tariffClock{now: time.Unix(1748736000, 0)}
	srv, err := energy.NewCommodityTariff(energy.CommodityTariffConfig{Features: allTariffFeatures, Now: clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	return srv, clock
}

// TestCommodityTariffDerived runs the SETRF-2.3 sequence on the second
// preset: the current and next day and day entry after the update, after
// a 4-hour and after a 24-hour time shift (TC_SETRF_2_3.py at the pin),
// then GetTariffComponent, GetDayEntry and the clear.
func TestCommodityTariffDerived(t *testing.T) { //nolint:funlen // one sequence
	t.Parallel()
	srv, clock := newTariff(t)
	_, err := srv.MatterInvoke(context.Background(), ct.CmdGetDayEntry, ct.GetDayEntryRequest{DayEntryId: 10})
	wantStatus(t, err, im.StatusFailure)
	if err := srv.UpdateTariff(sample2()); err != nil {
		t.Fatal(err)
	}
	dayStart := uint32(1748736000 - 946684800)
	day := read(t, srv, ct.AttrCurrentDay).(energy.TariffDay)
	if day.Date != dayStart || day.DayType != ct.DayTypeStandard || len(day.DayEntryIds) != 7 {
		t.Errorf("CurrentDay %+v", day)
	}
	if next := read(t, srv, ct.AttrNextDay).(energy.TariffDay); next.Date != dayStart+86400 {
		t.Errorf("NextDay %+v", next)
	}
	if e := read(t, srv, ct.AttrCurrentDayEntry).(energy.TariffDayEntry); e.DayEntryId != 10 {
		t.Errorf("CurrentDayEntry %+v", e)
	}
	nextDate := read(t, srv, ct.AttrNextDayEntryDate)
	if read(t, srv, ct.AttrCurrentDayEntryDate) != dayStart || nextDate != dayStart+240*60 {
		t.Errorf("entry dates %v %v", read(t, srv, ct.AttrCurrentDayEntryDate), nextDate)
	}
	if c := read(t, srv, ct.AttrCurrentTariffComponents).(spec.List[energy.TariffComponent]); len(c) != 1 || c[0].TariffComponentId != 10 {
		t.Errorf("CurrentTariffComponents %+v", c)
	}
	if c := read(t, srv, ct.AttrNextTariffComponents).(spec.List[energy.TariffComponent]); len(c) != 1 || c[0].TariffComponentId != 20 {
		t.Errorf("NextTariffComponents %+v", c)
	}

	// Change Time: the day entry moves on, the day does not
	// (TC_SETRF_2_3.py steps 12-16).
	clock.advance(4 * time.Hour)
	if err := srv.SyncTime(); err != nil {
		t.Fatal(err)
	}
	if read(t, srv, ct.AttrCurrentDayEntryDate) != nextDate {
		t.Error("CurrentDayEntryDate is not the NextDayEntryDate before the shift")
	}
	if read(t, srv, ct.AttrNextDayEntryDate) == nextDate {
		t.Error("NextDayEntryDate did not move")
	}
	if !reflect.DeepEqual(read(t, srv, ct.AttrCurrentDay), day) {
		t.Error("CurrentDay moved on a 4-hour shift")
	}
	// Change Day (steps 17-21).
	nextDay := read(t, srv, ct.AttrNextDay)
	clock.advance(24 * time.Hour)
	if err := srv.SyncTime(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(read(t, srv, ct.AttrCurrentDay), nextDay) || reflect.DeepEqual(read(t, srv, ct.AttrNextDay), nextDay) {
		t.Error("the days did not move on a 24-hour shift")
	}

	// GetTariffComponent and GetDayEntry (Server.cpp:1099-1241).
	resp, err := srv.MatterInvoke(context.Background(), ct.CmdGetTariffComponent, ct.GetTariffComponentRequest{TariffComponentId: 10})
	if err != nil {
		t.Fatal(err)
	}
	got := resp.(ct.GetTariffComponentResponse)
	if got.Label.Value != "Period 1" || !reflect.DeepEqual(got.DayEntryIds, []uint32{10, 13, 16}) || got.TariffComponent.TariffComponentId != 10 {
		t.Errorf("GetTariffComponent %+v", got)
	}
	_, err = srv.MatterInvoke(context.Background(), ct.CmdGetTariffComponent, &ct.GetTariffComponentRequest{TariffComponentId: 99})
	wantStatus(t, err, im.StatusNotFound)
	resp, err = srv.MatterInvoke(context.Background(), ct.CmdGetDayEntry, &ct.GetDayEntryRequest{DayEntryId: 16})
	if err != nil || resp.(ct.GetDayEntryResponse).DayEntry.StartTime != 1440 {
		t.Errorf("GetDayEntry %v %v", resp, err)
	}
	_, err = srv.MatterInvoke(context.Background(), ct.CmdGetDayEntry, ct.GetDayEntryRequest{DayEntryId: 99})
	wantStatus(t, err, im.StatusNotFound)
	for _, cmd := range []uint32{ct.CmdGetDayEntry, ct.CmdGetTariffComponent} {
		_, err = srv.MatterInvoke(context.Background(), cmd, 7)
		wantStatus(t, err, im.StatusInvalidCommand)
	}

	// An equal update changes nothing; the clear nulls everything.
	version := srv.MatterDataVersion()
	if err := srv.UpdateTariff(sample2()); err != nil || srv.MatterDataVersion() != version {
		t.Errorf("an equal update: %v, version %d → %d", err, version, srv.MatterDataVersion())
	}
	if err := srv.ClearTariff(); err != nil {
		t.Fatal(err)
	}
	for _, id := range []uint32{ct.AttrTariffInfo, ct.AttrDayEntries, ct.AttrCurrentDay, ct.AttrCurrentTariffComponents, ct.AttrDefaultRandomizationType} {
		if v := read(t, srv, id); v != nil {
			t.Errorf("0x%04X after the clear: %v", id, v)
		}
	}
	if err := srv.ClearTariff(); err != nil {
		t.Fatal(err)
	}
	_, err = srv.MatterInvoke(context.Background(), ct.CmdGetTariffComponent, ct.GetTariffComponentRequest{TariffComponentId: 10})
	wantStatus(t, err, im.StatusFailure)
	if err := srv.SyncTime(); err != nil {
		t.Errorf("SyncTime without a tariff: %v", err)
	}
	srv.OnMatterAttributesChanged(func([]uint32) {})()
	wantStatus(t, srv.MatterWrite(context.Background(), ct.AttrTariffUnit, uint64(0)), im.StatusUnsupportedWrite)
}

// TestCommodityTariffDelayed holds an update whose StartDate lies ahead
// until SyncTime reaches it (Server.cpp:64-68, :316-328), and refuses a
// new one meanwhile.
func TestCommodityTariffDelayed(t *testing.T) {
	t.Parallel()
	srv, clock := newTariff(t)
	tariff := sample2()
	start := uint32(1748736000-946684800) + 3600
	tariff.StartDate = &start
	tariff.CalendarPeriods[0].StartDate = spec.ValueOf(start)
	if err := srv.UpdateTariff(tariff); err != nil {
		t.Fatal(err)
	}
	if read(t, srv, ct.AttrTariffInfo) != nil {
		t.Fatal("a delayed tariff is served early")
	}
	if err := srv.UpdateTariff(sample2()); !errors.Is(err, energy.ErrTariffUpdatePending) {
		t.Errorf("an update while pending: %v", err)
	}
	if err := srv.SyncTime(); err != nil || read(t, srv, ct.AttrTariffInfo) != nil {
		t.Errorf("activated before its StartDate: %v", err)
	}
	clock.advance(time.Hour)
	if err := srv.SyncTime(); err != nil {
		t.Logf("SyncTime: %v", err)
	}
	if read(t, srv, ct.AttrTariffInfo) == nil {
		t.Error("not activated at its StartDate")
	}
	// The first calendar period starts with the tariff, so the current
	// day is found only on the day it starts (FindDay, Server.cpp:704-709).
	if day, ok := read(t, srv, ct.AttrCurrentDay).(energy.TariffDay); !ok || day.DayType != ct.DayTypeStandard {
		t.Errorf("CurrentDay %v", read(t, srv, ct.AttrCurrentDay))
	}
	// A clear drops a pending update.
	srv2, _ := newTariff(t)
	if err := srv2.UpdateTariff(tariff); err != nil {
		t.Fatal(err)
	}
	if err := srv2.ClearTariff(); err != nil {
		t.Fatal(err)
	}
	if err := srv2.UpdateTariff(sample2()); err != nil {
		t.Errorf("an update after the clear: %v", err)
	}
}

// TestCommodityTariffRules runs each validation rule against one
// mutation of the second preset.
func TestCommodityTariffRules(t *testing.T) { //nolint:funlen // a table
	t.Parallel()
	cases := []struct {
		name   string
		mutate func(*energy.Tariff)
		want   error
	}{
		{"empty list", func(x *energy.Tariff) { x.DayEntries = []energy.TariffDayEntry{} }, energy.ErrTariffListLength},
		{"TariffUnit", func(x *energy.Tariff) { x.TariffUnit = ptrTo(ct.TariffUnitEnum(2)) }, energy.ErrTariffInvalid},
		{"StartDate in the past", func(x *energy.Tariff) { x.StartDate = ptrTo(uint32(5)) }, energy.ErrTariffInvalid},
		{"randomization type", func(x *energy.Tariff) { x.DefaultRandomizationType = ptrTo(ct.DayEntryRandomizationTypeEnum(5)) }, energy.ErrTariffInvalid},
		{"label", func(x *energy.Tariff) { x.TariffInfo.TariffLabel = spec.ValueOf(strings.Repeat("x", 129)) }, energy.ErrTariffInvalid},
		{"provider", func(x *energy.Tariff) { x.TariffInfo.ProviderName = spec.ValueOf(strings.Repeat("x", 129)) }, energy.ErrTariffInvalid},
		{"block mode", func(x *energy.Tariff) { x.TariffInfo.BlockMode = spec.ValueOf(ct.BlockModeEnum(3)) }, energy.ErrTariffInvalid},
		{"no currency", func(x *energy.Tariff) { x.TariffInfo.Currency = nil }, energy.ErrTariffInvalid},
		{"currency 999", func(x *energy.Tariff) { x.TariffInfo.Currency = ptrTo(spec.ValueOf(ct.Currency{Currency: 999})) }, energy.ErrTariffInvalid},
		{"duplicate day entry", func(x *energy.Tariff) { x.DayEntries[1].DayEntryId = 10 }, energy.ErrTariffDuplicateID},
		{"start time", func(x *energy.Tariff) { x.DayEntries[0].StartTime = 1500 }, energy.ErrTariffInvalid},
		{"randomization missing", func(x *energy.Tariff) { x.DayEntries[0].RandomizationType = nil }, energy.ErrTariffInvalid},
		{"randomization enum", func(x *energy.Tariff) { x.DayEntries[0].RandomizationType = ptrTo(ct.DayEntryRandomizationTypeEnum(9)) }, energy.ErrTariffInvalid},
		{"duplicate pattern", func(x *energy.Tariff) {
			x.DayPatterns = append(x.DayPatterns, energy.TariffDayPattern{DayPatternId: 0, DayEntryIds: []uint32{99}})
		}, energy.ErrTariffDuplicateID},
		{"pattern entries", func(x *energy.Tariff) { x.DayPatterns[0].DayEntryIds = nil }, energy.ErrTariffInvalid},
		{"entry in two patterns", func(x *energy.Tariff) {
			x.DayPatterns = append(x.DayPatterns, energy.TariffDayPattern{DayPatternId: 1, DayEntryIds: []uint32{10}})
		}, energy.ErrTariffDuplicateID},
		{"partial week", func(x *energy.Tariff) { x.DayPatterns[0].DaysOfWeek = 0x0F }, energy.ErrTariffInvalid},
		{"component id 0", func(x *energy.Tariff) { x.TariffComponents[0].TariffComponentId = 0 }, energy.ErrTariffInvalid},
		{"threshold without block", func(x *energy.Tariff) { x.TariffInfo.BlockMode = spec.ValueOf(ct.BlockModeNoBlock) }, energy.ErrTariffInvalid},
		{"component label", func(x *energy.Tariff) { x.TariffComponents[0].Label = ptrTo(spec.ValueOf(strings.Repeat("x", 129))) }, energy.ErrTariffInvalid},
		{"price type", func(x *energy.Tariff) {
			x.TariffComponents[0].Price = ptrTo(spec.ValueOf(ct.TariffPriceStruct{PriceType: 7}))
		}, energy.ErrTariffInvalid},
		{"required state", func(x *energy.Tariff) { x.TariffComponents[0].AuxiliaryLoad.RequiredState = 3 }, energy.ErrTariffInvalid},
		{"peak period", func(x *energy.Tariff) { x.TariffComponents[0].PeakPeriod.PeakPeriod = 0 }, energy.ErrTariffInvalid},
		{"power threshold", func(x *energy.Tariff) { x.TariffComponents[0].PowerThreshold.PowerThreshold = ptrTo(int64(0)) }, energy.ErrTariffInvalid},
		{"duplicate component", func(x *energy.Tariff) { x.TariffComponents[1].TariffComponentId = 10 }, energy.ErrTariffDuplicateID},
		{"period label", func(x *energy.Tariff) { x.TariffPeriods[0].Label = spec.ValueOf("") }, energy.ErrTariffInvalid},
		{"period entries", func(x *energy.Tariff) { x.TariffPeriods[0].DayEntryIds = nil }, energy.ErrTariffInvalid},
		{"period components", func(x *energy.Tariff) { x.TariffPeriods[0].TariffComponentIds = nil }, energy.ErrTariffInvalid},
		{"entry in two periods", func(x *energy.Tariff) { x.TariffPeriods[1].DayEntryIds = []uint32{10} }, energy.ErrTariffDuplicateID},
		{"component twice in a period", func(x *energy.Tariff) { x.TariffPeriods[0].TariffComponentIds = []uint32{10, 10} }, energy.ErrTariffDuplicateID},
		{"day date", func(x *energy.Tariff) {
			x.IndividualDays = []energy.TariffDay{{Date: 0, DayEntryIds: []uint32{10}}}
		}, energy.ErrTariffInvalid},
		{"day type", func(x *energy.Tariff) {
			x.IndividualDays = []energy.TariffDay{{Date: 5, DayType: 4, DayEntryIds: []uint32{10}}}
		}, energy.ErrTariffInvalid},
		{"day entries", func(x *energy.Tariff) { x.IndividualDays = []energy.TariffDay{{Date: 5}} }, energy.ErrTariffInvalid},
		{"day entry also in a pattern", func(x *energy.Tariff) {
			x.IndividualDays = []energy.TariffDay{{Date: 5, DayEntryIds: []uint32{10}}}
		}, energy.ErrTariffDuplicateID},
		{"calendar without StartDate", func(x *energy.Tariff) { x.StartDate = nil }, energy.ErrTariffInvalid},
		{"calendar patterns", func(x *energy.Tariff) { x.CalendarPeriods[0].DayPatternIds = nil }, energy.ErrTariffInvalid},
		{"calendar pattern twice", func(x *energy.Tariff) {
			x.CalendarPeriods = append(x.CalendarPeriods, energy.TariffCalendarPeriod{StartDate: spec.ValueOf(uint32(9)), DayPatternIds: []uint32{0}})
		}, energy.ErrTariffDuplicateID},
		{"first calendar start", func(x *energy.Tariff) { x.CalendarPeriods[0].StartDate = spec.ValueOf(uint32(7)) }, energy.ErrTariffInvalid},
		{"later calendar without start", func(x *energy.Tariff) {
			x.DayPatterns = append(x.DayPatterns, energy.TariffDayPattern{DayPatternId: 1, DaysOfWeek: 0x7F, DayEntryIds: []uint32{99}})
			x.CalendarPeriods = append(x.CalendarPeriods, energy.TariffCalendarPeriod{StartDate: spec.NullOf[uint32](), DayPatternIds: []uint32{1}})
		}, energy.ErrTariffInvalid},
		{"missing TariffPeriods", func(x *energy.Tariff) { x.TariffPeriods = nil }, energy.ErrTariffMissing},
		{"unknown period entry", func(x *energy.Tariff) { x.TariffPeriods[0].DayEntryIds = []uint32{77} }, energy.ErrTariffUnknownID},
		{"unknown period component", func(x *energy.Tariff) { x.TariffPeriods[0].TariffComponentIds = []uint32{77} }, energy.ErrTariffUnknownID},
		{"unknown pattern entry", func(x *energy.Tariff) { x.DayPatterns[0].DayEntryIds = []uint32{77} }, energy.ErrTariffUnknownID},
		{"unknown calendar pattern", func(x *energy.Tariff) { x.CalendarPeriods[0].DayPatternIds = []uint32{5} }, energy.ErrTariffUnknownID},
		{"no days", func(x *energy.Tariff) { x.CalendarPeriods = nil }, energy.ErrTariffNoDays},
		{"same slot twice in a period", func(x *energy.Tariff) { x.DayEntries[3].StartTime = 0 }, energy.ErrTariffDuplicateID},
		{"shared threshold", func(x *energy.Tariff) {
			x.TariffComponents[1].Threshold = spec.ValueOf(int64(120))
			x.TariffPeriods[0].TariffComponentIds = []uint32{10, 20}
		}, energy.ErrTariffDuplicateID},
	}
	for _, c := range cases {
		srv, _ := newTariff(t)
		tariff := sample2()
		c.mutate(&tariff)
		if err := srv.UpdateTariff(tariff); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", c.name, err, c.want)
		}
		if read(t, srv, ct.AttrTariffInfo) != nil {
			t.Errorf("%s: a refused update changed the tariff", c.name)
		}
	}
	// Without PRICE a Currency is refused; without the feature fields are.
	plain, err := energy.NewCommodityTariff(energy.CommodityTariffConfig{Features: energy.TariffFeatureFriendlyCredit})
	if err != nil {
		t.Fatal(err)
	}
	if err := plain.UpdateTariff(sample2()); !errors.Is(err, energy.ErrTariffInvalid) {
		t.Errorf("Currency without PRICE: %v", err)
	}
	tariff := sample2()
	tariff.TariffInfo.Currency = nil
	if err := plain.UpdateTariff(tariff); !errors.Is(err, energy.ErrTariffInvalid) {
		t.Errorf("Price without PRICE: %v", err)
	}
	for _, f := range []energy.TariffFeature{energy.TariffFeaturePricing | energy.TariffFeatureFriendlyCredit, energy.TariffFeaturePricing | energy.TariffFeatureAuxiliaryLoad} {
		srv, _ := energy.NewCommodityTariff(energy.CommodityTariffConfig{Features: f})
		if err := srv.UpdateTariff(sample2()); !errors.Is(err, energy.ErrTariffInvalid) {
			t.Errorf("features 0x%X: %v", f, err)
		}
	}
}
