// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package energy_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/cluster/energy"
	"github.com/SukramJ/go-fabric/cluster/spec"
	ct "github.com/SukramJ/go-fabric/cluster/spec/commoditytariff"
	dem "github.com/SukramJ/go-fabric/cluster/spec/deviceenergymanagement"
	evsedef "github.com/SukramJ/go-fabric/cluster/spec/energyevse"
	ep "github.com/SukramJ/go-fabric/cluster/spec/energypreference"
	whm "github.com/SukramJ/go-fabric/cluster/spec/waterheatermanagement"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// tariffValues is a Tariff as the attribute values SetAttributes takes.
func tariffValues(tb testing.TB, t energy.Tariff) map[uint32]any {
	tb.Helper()
	values := map[uint32]any{
		ct.AttrTariffUnit:                 uint8(*t.TariffUnit),
		ct.AttrStartDate:                  *t.StartDate,
		ct.AttrDefaultRandomizationOffset: *t.DefaultRandomizationOffset,
		ct.AttrDefaultRandomizationType:   uint8(*t.DefaultRandomizationType),
		ct.AttrTariffInfo:                 *t.TariffInfo,
		ct.AttrDayEntries:                 t.DayEntries,
		ct.AttrDayPatterns:                t.DayPatterns,
		ct.AttrTariffComponents:           t.TariffComponents,
		ct.AttrTariffPeriods:              t.TariffPeriods,
		ct.AttrCalendarPeriods:            t.CalendarPeriods,
	}
	return values
}

// TestTariffSetAttributes: the host's tariff is one update (all of it or
// none), every attribute null clears it, the derived attributes are the
// tariff's and the clock's ([spec.ErrNotHeld]), and a bad value is refused.
func TestTariffSetAttributes(t *testing.T) {
	t.Parallel()

	srv, _ := newTariff(t)
	if err := srv.SetAttributes(nil); err != nil {
		t.Fatalf("empty call = %v", err)
	}
	if err := srv.SetAttributes(tariffValues(t, sample2())); err != nil {
		t.Fatalf("full tariff = %v", err)
	}
	if v := read(t, srv, ct.AttrTariffUnit); v != uint8(ct.TariffUnitKWh) {
		t.Errorf("TariffUnit = %v after the update", v)
	}

	if err := srv.SetAttributes(map[uint32]any{ct.AttrCurrentDay: nil}); !errors.Is(err, spec.ErrNotHeld) {
		t.Errorf("CurrentDay = %v, want ErrNotHeld", err)
	}
	if err := srv.SetAttributes(map[uint32]any{ct.AttrTariffUnit: "kWh"}); !errors.Is(err, spec.ErrInvalidValue) {
		t.Errorf("TariffUnit of the wrong type = %v, want ErrInvalidValue", err)
	}
	if err := srv.SetAttributes(map[uint32]any{0x7F: uint8(1)}); !errors.Is(err, spec.ErrNotServed) {
		t.Errorf("unserved attribute = %v, want ErrNotServed", err)
	}
	// A partial update with a null list in it is one update, refused as a
	// whole by the cross validation; it must not clear the stored tariff.
	partial := map[uint32]any{ct.AttrTariffUnit: uint8(ct.TariffUnitKWh), ct.AttrDayEntries: nil}
	if err := srv.SetAttributes(partial); err == nil {
		t.Error("a partial tariff was accepted")
	}
	if v := read(t, srv, ct.AttrTariffUnit); v != uint8(ct.TariffUnitKWh) {
		t.Errorf("TariffUnit = %v after a refused update", v)
	}

	if err := srv.SetAttributes(map[uint32]any{ct.AttrTariffUnit: nil}); err != nil {
		t.Fatalf("all null = %v", err)
	}
	if v := read(t, srv, ct.AttrTariffUnit); v != nil {
		t.Errorf("TariffUnit = %v after the clear, want null", v)
	}
}

// TestTariffTimeSyncRunsOnTheClock: with a SyncInterval the server syncs
// itself on the clock — a delayed update activates when its StartDate
// comes — until MatterQuiesce, and the next update starts it again.
func TestTariffTimeSyncRunsOnTheClock(t *testing.T) {
	t.Parallel()

	const interval = 30 * time.Second
	clock := clocktest.New(time.Unix(1748736000, 0))
	srv, err := energy.NewCommodityTariff(energy.CommodityTariffConfig{
		Features: allTariffFeatures, Clock: clock, SyncInterval: interval,
	})
	if err != nil {
		t.Fatal(err)
	}
	if clock.Pending() != 1 {
		t.Fatalf("armed timers = %d at construction, want the sync", clock.Pending())
	}

	delayed := sample2()
	start := uint32(1748736000-946684800) + 45
	delayed.StartDate = &start
	delayed.CalendarPeriods = []energy.TariffCalendarPeriod{{StartDate: spec.ValueOf(start), DayPatternIds: []uint32{0}}}
	if err := srv.UpdateTariff(delayed); err != nil {
		t.Fatal(err)
	}
	if clock.Pending() != 1 {
		t.Errorf("armed timers = %d after an update, want the one sync", clock.Pending())
	}
	if v := read(t, srv, ct.AttrStartDate); v != nil {
		t.Fatalf("StartDate = %v before it came, want the update pending", v)
	}

	clock.Advance(interval) // 30 s: not yet
	if v := read(t, srv, ct.AttrStartDate); v != nil {
		t.Errorf("StartDate = %v after 30 s, want still pending", v)
	}
	if clock.Pending() != 1 {
		t.Errorf("armed timers = %d after a tick, want the sync re-armed", clock.Pending())
	}
	clock.Advance(interval) // 60 s: the StartDate has come
	if v := read(t, srv, ct.AttrStartDate); v != start {
		t.Errorf("StartDate = %v after 60 s, want %d", v, start)
	}

	srv.MatterQuiesce()
	if clock.Pending() != 0 {
		t.Errorf("armed timers = %d after MatterQuiesce, want none", clock.Pending())
	}
	srv.MatterQuiesce() // a second one has nothing to stop
	clock.Advance(10 * interval)

	if err := srv.UpdateTariff(sample2()); err != nil {
		t.Fatal(err)
	}
	if clock.Pending() != 1 {
		t.Errorf("armed timers = %d after the next update, want the sync again", clock.Pending())
	}
}

// TestDemSetAttributes: the device's own EsaState, power limits, null
// capability and forecast, and OptOutState go through their setters;
// EsaType is the configuration's, a bad value is refused.
func TestDemSetAttributes(t *testing.T) {
	t.Parallel()

	srv, _ := newDEM(t, demPFRSet, &demHost{})
	if err := srv.SetAttributes(nil); err != nil {
		t.Fatalf("empty call = %v", err)
	}
	err := srv.SetAttributes(map[uint32]any{
		dem.AttrEsaState:                  uint8(energy.EsaStateOffline),
		dem.AttrAbsMinPower:               int64(1000000),
		dem.AttrAbsMaxPower:               int64(8000000),
		dem.AttrPowerAdjustmentCapability: nil,
		dem.AttrForecast:                  nil,
		dem.AttrOptOutState:               uint8(energy.OptOutLocal),
	})
	if err != nil {
		t.Fatalf("SetAttributes = %v", err)
	}
	if got := srv.EsaState(); got != energy.EsaStateOffline {
		t.Errorf("EsaState = %v, want Offline", got)
	}
	if got := srv.OptOutState(); got != energy.OptOutLocal {
		t.Errorf("OptOutState = %v, want LocalOptOut", got)
	}
	if v := read(t, srv, dem.AttrAbsMaxPower); v != int64(8000000) {
		t.Errorf("AbsMaxPower = %v", v)
	}

	if err := srv.SetAttributes(map[uint32]any{dem.AttrEsaType: uint8(0)}); !errors.Is(err, spec.ErrNotHeld) {
		t.Errorf("EsaType = %v, want ErrNotHeld", err)
	}
	if err := srv.SetAttributes(map[uint32]any{dem.AttrEsaState: "online"}); !errors.Is(err, spec.ErrInvalidValue) {
		t.Errorf("EsaState of the wrong type = %v, want ErrInvalidValue", err)
	}
	if err := srv.SetAttributes(map[uint32]any{0x7F: uint8(1)}); !errors.Is(err, spec.ErrNotServed) {
		t.Errorf("unserved attribute = %v, want ErrNotServed", err)
	}
}

// TestEvseSetAttributes: the host's own values are checked and stored, the
// vehicle id empty is null, and what the state machine computes is refused.
func TestEvseSetAttributes(t *testing.T) {
	t.Parallel()

	srv, _, _ := newEvse(t, evsedef.FeatureChargingPreferences|evsedef.FeatureSoCReporting|evsedef.FeaturePlugAndCharge)
	basicFunctionality(t, srv)

	if err := srv.SetAttributes(map[uint32]any{
		evsedef.AttrCircuitCapacity:          int64(20000),
		evsedef.AttrUserMaximumChargeCurrent: int64(10000),
		evsedef.AttrVehicleId:                "VIN-1",
	}); err != nil {
		t.Fatalf("SetAttributes = %v", err)
	}
	if v := read(t, srv, evsedef.AttrVehicleId); v != "VIN-1" {
		t.Errorf("VehicleId = %v", v)
	}
	if v := read(t, srv, evsedef.AttrUserMaximumChargeCurrent); v != int64(10000) {
		t.Errorf("UserMaximumChargeCurrent = %v", v)
	}
	if err := srv.SetAttributes(map[uint32]any{evsedef.AttrVehicleId: ""}); err != nil {
		t.Fatalf("empty VehicleId = %v", err)
	}
	if v := read(t, srv, evsedef.AttrVehicleId); v != nil {
		t.Errorf("VehicleId = %v for an empty id, want null", v)
	}

	tests := []struct {
		name   string
		values map[uint32]any
		is     error
	}{
		{"derived attribute", map[uint32]any{evsedef.AttrMaximumChargeCurrent: int64(5000)}, spec.ErrNotHeld},
		{"negative circuit capacity", map[uint32]any{evsedef.AttrCircuitCapacity: int64(-1)}, spec.ErrInvalidValue},
		{"negative user maximum", map[uint32]any{evsedef.AttrUserMaximumChargeCurrent: int64(-1)}, spec.ErrInvalidValue},
		{"vehicle id too long", map[uint32]any{evsedef.AttrVehicleId: strings.Repeat("x", 33)}, spec.ErrInvalidValue},
		{"unserved attribute", map[uint32]any{0x7F: uint8(1)}, spec.ErrNotServed},
	}
	for _, tt := range tests {
		err := srv.SetAttributes(tt.values)
		if err == nil {
			t.Errorf("%s: accepted", tt.name)
			continue
		}
		if !errors.Is(err, tt.is) && statusOf(err) == im.StatusSuccess {
			t.Errorf("%s: %v, want %v or an IM status", tt.name, err, tt.is)
		}
	}
}

// TestWaterHeaterSetAttributes: the device's report goes through the
// setters, a boost ends with BoostState Inactive and starts only with the
// Boost command, HeaterTypes is the configuration's.
func TestWaterHeaterSetAttributes(t *testing.T) {
	t.Parallel()

	host := &booster{}
	srv := newWaterHeater(t, energy.WaterHeaterFeatureEnergyManagement|energy.WaterHeaterFeatureTankPercent, host)
	em := &emitter{}
	srv.SetMatterEventEmitter(em)

	before := srv.MatterDataVersion()
	if err := srv.SetAttributes(map[uint32]any{
		whm.AttrHeatDemand:     uint8(energy.HeatSourceImmersionElement1),
		whm.AttrTankPercentage: uint8(55),
		whm.AttrTankVolume:     uint16(120),
	}); err != nil {
		t.Fatalf("SetAttributes = %v", err)
	}
	if st := srv.State(); st.HeatDemand != energy.HeatSourceImmersionElement1 || st.TankPercentage != 55 || st.TankVolume != 120 {
		t.Errorf("State = %+v", st)
	}
	if srv.MatterDataVersion() == before {
		t.Error("DataVersion did not move")
	}

	// A boost ends with Inactive; one that does not run is no error.
	if err := srv.SetAttributes(map[uint32]any{whm.AttrBoostState: uint8(energy.BoostStateInactive)}); err != nil {
		t.Errorf("Inactive without a boost = %v", err)
	}
	if _, err := srv.MatterInvoke(context.Background(), whm.CmdBoost, whm.BoostRequest{BoostInfo: energy.BoostInfo{Duration: 60}}); err != nil {
		t.Fatal(err)
	}
	em.take()
	if err := srv.SetAttributes(map[uint32]any{
		whm.AttrBoostState: uint8(energy.BoostStateInactive), whm.AttrTankPercentage: uint8(60),
	}); err != nil {
		t.Fatalf("ending the boost = %v", err)
	}
	if srv.BoostState() != energy.BoostStateInactive {
		t.Errorf("BoostState = %v, want Inactive", srv.BoostState())
	}
	if evs := em.take(); len(evs) != 1 || evs[0].id != whm.EventBoostEnded {
		t.Errorf("events = %+v, want BoostEnded", evs)
	}

	tests := []struct {
		name   string
		values map[uint32]any
		is     error
	}{
		{"BoostState Active", map[uint32]any{whm.AttrBoostState: uint8(energy.BoostStateActive)}, spec.ErrNotHeld},
		{"HeaterTypes", map[uint32]any{whm.AttrHeaterTypes: uint8(energy.HeatSourceBoiler)}, spec.ErrNotHeld},
		{"BoostState of the wrong type", map[uint32]any{whm.AttrBoostState: "off"}, spec.ErrInvalidValue},
		{"TankPercentage of the wrong type", map[uint32]any{whm.AttrTankPercentage: "full"}, spec.ErrInvalidValue},
		{"HeatDemand outside HeaterTypes", map[uint32]any{whm.AttrHeatDemand: uint8(energy.HeatSourceBoiler)}, energy.ErrInvalidValue},
	}
	for _, tt := range tests {
		if err := srv.SetAttributes(tt.values); !errors.Is(err, tt.is) {
			t.Errorf("%s: %v, want %v", tt.name, err, tt.is)
		}
	}
}

// TestWaterHeaterBoostErrorMessage: the INVALID_COMMAND of a Boost without
// fields carries its message.
func TestWaterHeaterBoostErrorMessage(t *testing.T) {
	t.Parallel()

	srv := newWaterHeater(t, 0, &booster{})
	_, err := srv.MatterInvoke(context.Background(), whm.CmdBoost, (*whm.BoostRequest)(nil))
	if err == nil || !strings.Contains(err.Error(), "Boost without fields") {
		t.Errorf("err = %v, want the Boost message", err)
	}
	wantStatus(t, err, im.StatusInvalidCommand)
}

// TestPreferenceDataVersionAndPriorityEncoding: the DataVersion follows
// the stored values, and EnergyPriorities encodes as an array of enum8.
func TestPreferenceDataVersionAndPriorityEncoding(t *testing.T) {
	t.Parallel()

	cfg := preferenceConfig(energy.PreferenceFeatureEnergyBalance)
	cfg.Changer = &changer{}
	srv, err := energy.NewEnergyPreference(cfg)
	if err != nil {
		t.Fatal(err)
	}
	before := srv.MatterDataVersion()
	if err := srv.MatterWrite(context.Background(), ep.AttrCurrentEnergyBalance, uint8(2)); err != nil {
		t.Fatal(err)
	}
	if srv.MatterDataVersion() == before {
		t.Error("DataVersion did not move on a stored write")
	}

	v, ok := srv.MatterRead(ep.AttrEnergyPriorities)
	if !ok {
		t.Fatal("EnergyPriorities not served")
	}
	enc, ok := v.(spec.Encodable)
	if !ok {
		t.Fatalf("EnergyPriorities is %T, not Encodable", v)
	}
	e := tlv.NewEncoder()
	enc.EncodeTLV(e, tlv.AnonymousTag())
	b, err := e.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	d := tlv.NewDecoder(b)
	el, err := d.Next()
	if err != nil || el.Type != tlv.TypeArray {
		t.Fatalf("encodes as %v (%v), want an array", el.Type, err)
	}
	var got []uint64
	for {
		el, err := d.Next()
		if err != nil {
			t.Fatal(err)
		}
		if el.Type == tlv.TypeEndContainer {
			break
		}
		got = append(got, el.Uint)
	}
	if len(got) != 2 || got[0] != uint64(energy.PriorityEfficiency) || got[1] != uint64(energy.PriorityComfort) {
		t.Errorf("priorities = %v, want [Efficiency Comfort]", got)
	}
}
