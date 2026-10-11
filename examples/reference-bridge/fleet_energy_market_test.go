// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/energy"
	"github.com/SukramJ/go-fabric/cluster/spec"
	commtr "github.com/SukramJ/go-fabric/cluster/spec/commoditymetering"
	cp "github.com/SukramJ/go-fabric/cluster/spec/commodityprice"
	ct "github.com/SukramJ/go-fabric/cluster/spec/commoditytariff"
	egc "github.com/SukramJ/go-fabric/cluster/spec/electricalgridconditions"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/schema"
)

// TestEnergyMarketWireShapes fires the triggers TC-SETRF-2.2 (step 3) and
// TC-COMMTR-3.1 send through the daemon's whole TestEventTrigger dispatch
// and reads the mounted servers as the bridge does: every attribute is
// null, a scalar or a spec.Encodable — a plain slice goes on the wire as
// null (bridge/reply.go defaultAttributeValueWriter) — and the lists the
// cases iterate carry the trigger's entries (TC_SETRF_2_2.py step 4,
// TC_SETRF_3_1.py step 27, TC_COMMTR_3_1.py's MeteredQuantity report; CI
// run 38114710064 saw Null for each).
func TestEnergyMarketWireShapes(t *testing.T) {
	f, br := startFleetBridge(t)
	tariff := mountedEndpoint(t, br, "demo-tariff-1")
	meter := mountedEndpoint(t, br, "demo-electricalmeter-1")
	for _, trigger := range []uint64{
		triggerTariffDataUpdated | uint64(tariff.ID)<<32, triggerPriceForecastUpdate, triggerGridForecastConditions,
		triggerMeteringAttributesUpdate | uint64(meter.ID)<<32,
	} {
		if err := f.testEventTrigger(context.Background(), trigger); err != nil {
			t.Fatalf("trigger 0x%016X: %v", trigger, err)
		}
	}
	for _, ep := range []*endpoint.Endpoint{tariff, meter} {
		for _, srv := range endpoint.ClusterServers(ep) {
			switch srv.MatterClusterID() {
			case cp.ClusterID, egc.ClusterID, ct.ClusterID, commtr.ClusterID:
			default:
				continue
			}
			lister, ok := srv.(contract.ClusterAttributeLister)
			if !ok {
				t.Fatalf("cluster 0x%04X lists no attributes", srv.MatterClusterID())
			}
			for _, id := range lister.MatterAttributes() {
				if id >= 0xFFF8 {
					continue // the globals, written by the bridge itself
				}
				v, _ := srv.MatterRead(id)
				switch v.(type) {
				case nil, bool, uint8, uint16, uint32, uint64, int8, int16, int32, int64, string, spec.Encodable:
				default:
					t.Errorf("endpoint %d cluster 0x%04X attribute 0x%04X reads as %T, which the bridge writes as null", ep.ID, srv.MatterClusterID(), id, v)
				}
			}
		}
	}
	for _, c := range []struct {
		ep            *endpoint.Endpoint
		cluster, attr uint32
	}{
		{tariff, ct.ClusterID, ct.AttrTariffComponents},
		{tariff, ct.ClusterID, ct.AttrIndividualDays},
		{tariff, ct.ClusterID, ct.AttrDayEntries},
		{tariff, cp.ClusterID, cp.AttrPriceForecast},
		{tariff, egc.ClusterID, egc.AttrForecastConditions},
		{meter, commtr.ClusterID, commtr.AttrMeteredQuantity},
	} {
		v := readMounted(t, c.ep, c.cluster, c.attr)
		if l := reflect.ValueOf(v); v == nil || l.Kind() != reflect.Slice || l.Len() == 0 {
			t.Errorf("cluster 0x%04X attribute 0x%04X = %#v after the trigger", c.cluster, c.attr, v)
		}
	}
}

// TestEnergyMarketDevices: the ElectricalEnergyTariff (0x0513) serves
// CommodityPrice (FORE), ElectricalGridConditions (FORE) and
// CommodityTariff (all six features, FeatureMap 63 as the app's zap
// file: energy-gateway-app.matter:2240); the ElectricalMeter (0x0514)
// ElectricalPowerMeasurement, ElectricalEnergyMeasurement and
// CommodityMetering, with an ElectricalSensor part. The device-type
// validator finds one thing on the two: the ElectricalEnergyTariff's
// Descriptor lacks TAGLIST (electrical-energy-tariff.element.ts:21), which
// this module's Descriptor does not serve — the owner's to classify
// (notes/briefs/reports/K.md); anything else fails.
func TestEnergyMarketDevices(t *testing.T) {
	for id, want := range map[uint16]string{deviceTypeElectricalEnergyTariff: "ElectricalEnergyTariff", deviceTypeElectricalMeter: "ElectricalMeter"} {
		if name, _ := schema.DeviceTypeName(uint32(id)); name != want {
			t.Errorf("device type 0x%04X is %q in the snapshot, want %q", id, name, want)
		}
	}
	_, br := startFleetBridge(t)
	tariff := mountedEndpoint(t, br, "demo-tariff-1")
	if tariff.DeviceType != deviceTypeElectricalEnergyTariff {
		t.Fatalf("tariff endpoint %+v", tariff)
	}
	for cl, want := range map[uint32]uint32{cp.ClusterID: 1, egc.ClusterID: 1, ct.ClusterID: 63} {
		if fm := readMounted(t, tariff, cl, cluster.AttrGlobalFeatureMap); fm != want {
			t.Errorf("cluster 0x%04X FeatureMap = %v, want %d", cl, fm, want)
		}
	}
	if v := readMounted(t, tariff, cp.ClusterID, cp.AttrCurrency); v != (energy.PriceCurrency{Currency: 826, DecimalPoints: 5}) {
		t.Errorf("Currency %v", v)
	}
	if v := readMounted(t, tariff, egc.ClusterID, egc.AttrLocalGenerationAvailable); v != true {
		t.Errorf("LocalGenerationAvailable %v", v)
	}
	meter := mountedEndpoint(t, br, "demo-electricalmeter-1")
	if meter.DeviceType != deviceTypeElectricalMeter {
		t.Fatalf("meter endpoint %+v", meter)
	}
	readMounted(t, meter, commtr.ClusterID, commtr.AttrMeteredQuantity)
	for _, cl := range []uint32{0x0090, 0x0091} {
		readMounted(t, meter, cl, cluster.AttrGlobalFeatureMap)
	}
	if len(meter.PartIDs) != 1 || br.Topology().FindByID(meter.PartIDs[0]).DeviceType != deviceTypeElectricalSensor {
		t.Errorf("meter parts %v", meter.PartIDs)
	}
	for _, v := range endpoint.ValidateDeviceTypes(br.Topology()) {
		if v.Endpoint != tariff.ID && v.Endpoint != meter.ID && (len(meter.PartIDs) == 0 || v.Endpoint != meter.PartIDs[0]) {
			continue
		}
		if v.Endpoint == tariff.ID && strings.Contains(v.String(), "Descriptor.TAGLIST") {
			t.Logf("device type finding (open, report K): %s", v)
			continue
		}
		t.Errorf("device type violation: %s", v)
	}
}

func fireMarket(t *testing.T, f *fleet, trigger uint64) {
	t.Helper()
	if !f.energyMarketTestEventTrigger(trigger) {
		t.Fatalf("trigger 0x%016X not handled", trigger)
	}
}

func marketFleet() *fleet {
	return &fleet{energyTariff: newDemoEnergyTariff("test tariff"), electricalMeter: newDemoElectricalMeter("test meter")}
}

// TestTariffTriggers replays TC_SETRF_2_2.py and TC_SETRF_2_3.py's
// trigger sequence (connectedhomeip at the harness pin) on both presets:
// attributes value set, GetTariffComponent and GetDayEntry, the 4-hour
// and the 24-hour time shift, clear.
func TestTariffTriggers(t *testing.T) { //nolint:funlen // two runs of one sequence
	f := marketFleet()
	f.energyTariff.build()
	defer f.energyTariff.poll.Stop()
	srv := f.energyTariff.tariff
	read := func(attr uint32) any {
		v, _ := srv.MatterRead(attr)
		return v
	}
	for run := range 2 {
		fireMarket(t, f, triggerTariffDataUpdated)
		info, ok := read(ct.AttrTariffInfo).(energy.TariffInformation)
		if !ok {
			t.Fatalf("run %d: no TariffInfo", run)
		}
		if want := []string{"Full Tariff One", "Full Tariff Two"}[run]; info.TariffLabel.Value != want {
			t.Errorf("run %d: tariff %q, want %q", run, info.TariffLabel.Value, want)
		}
		components := read(ct.AttrTariffComponents).(spec.List[energy.TariffComponent])
		resp, err := srv.MatterInvoke(context.Background(), ct.CmdGetTariffComponent, ct.GetTariffComponentRequest{TariffComponentId: components[0].TariffComponentId})
		if err != nil || resp.(ct.GetTariffComponentResponse).Label.Null {
			t.Errorf("run %d: GetTariffComponent %v %v", run, resp, err)
		}
		entries := read(ct.AttrDayEntries).(spec.List[energy.TariffDayEntry])
		if _, err := srv.MatterInvoke(context.Background(), ct.CmdGetDayEntry, ct.GetDayEntryRequest{DayEntryId: entries[0].DayEntryId}); err != nil {
			t.Errorf("run %d: GetDayEntry %v", run, err)
		}
		// TC_SETRF_2_3.py steps 12-16: the day entry moves on, the day stays.
		currentDay, nextDay := read(ct.AttrCurrentDay), read(ct.AttrNextDay)
		nextDate := read(ct.AttrNextDayEntryDate)
		if currentDay == nil || nextDate == nil {
			t.Fatalf("run %d: CurrentDay %v NextDayEntryDate %v", run, currentDay, nextDate)
		}
		fireMarket(t, f, triggerTariffTimeShift4h)
		if read(ct.AttrCurrentDayEntryDate) != nextDate || read(ct.AttrNextDayEntryDate) == nextDate {
			t.Errorf("run %d: the 4-hour shift: current %v next %v, was next %v", run, read(ct.AttrCurrentDayEntryDate), read(ct.AttrNextDayEntryDate), nextDate)
		}
		if !reflect.DeepEqual(read(ct.AttrCurrentDay), currentDay) || !reflect.DeepEqual(read(ct.AttrNextDay), nextDay) {
			t.Errorf("run %d: the days moved on a 4-hour shift", run)
		}
		// Steps 17-21: the day moves on.
		fireMarket(t, f, triggerTariffTimeShift24h)
		if !reflect.DeepEqual(read(ct.AttrCurrentDay), nextDay) || reflect.DeepEqual(read(ct.AttrNextDay), nextDay) {
			t.Errorf("run %d: the 24-hour shift: current %v next %v, was next %v", run, read(ct.AttrCurrentDay), read(ct.AttrNextDay), nextDay)
		}
		if read(ct.AttrCurrentDayEntryDate) == read(ct.AttrNextDayEntryDate) {
			t.Errorf("run %d: CurrentDayEntryDate equals NextDayEntryDate after the day shift", run)
		}
		fireMarket(t, f, triggerTariffDataClear)
		if read(ct.AttrTariffInfo) != nil || read(ct.AttrCurrentDay) != nil {
			t.Errorf("run %d: the clear left a tariff", run)
		}
		fireMarket(t, f, triggerTariffTimeShiftDisable)
	}
	// A time shift with the test clock off enables it at the real time.
	fireMarket(t, f, triggerTariffTimeShift4h)
	if !f.energyTariff.clock.enabled {
		t.Error("the 4-hour shift left the test clock off")
	}
	f.energyTariff.pollTariff()
}

// TestMeteringTriggers replays TC_COMMTR_3_1.py's sequence: after the
// first AttributesValueUpdate every attribute differs from its starting
// null; Clear nulls them all; the next update changes them again.
func TestMeteringTriggers(t *testing.T) {
	f := marketFleet()
	f.electricalMeter.build()
	s := f.electricalMeter.metering
	fireMarket(t, f, triggerMeteringAttributesUpdate)
	if s.MaximumMeteredQuantities() == nil || *s.MaximumMeteredQuantities() != 2 || len(s.MeteredQuantity()) != 2 ||
		s.MeteredQuantityTimestamp() == nil || s.TariffUnit() == nil || *s.TariffUnit() != commtr.TariffUnitKVAh {
		t.Fatalf("first update: max %v quantities %v unit %v", s.MaximumMeteredQuantities(), s.MeteredQuantity(), s.TariffUnit())
	}
	fireMarket(t, f, triggerMeteringAttributesUpdate)
	if *s.TariffUnit() != commtr.TariffUnitKWh || len(s.MeteredQuantity()) != 3 || !reflect.DeepEqual(s.MeteredQuantity()[2].TariffComponentIds, []uint32{2031, 2032}) {
		t.Errorf("second update: unit %v quantities %v", s.TariffUnit(), s.MeteredQuantity())
	}
	fireMarket(t, f, triggerMeteringAttributesClear)
	if s.MaximumMeteredQuantities() != nil || s.MeteredQuantity() != nil || s.MeteredQuantityTimestamp() != nil || s.TariffUnit() != nil {
		t.Error("the clear left a value")
	}
	fireMarket(t, f, triggerMeteringAttributesUpdate)
	if s.TariffUnit() == nil || *s.TariffUnit() != commtr.TariffUnitKVAh {
		t.Errorf("update after the clear: unit %v", s.TariffUnit())
	}
	if f.energyMarketTestEventTrigger(0x0b07_0000_0000_0009) {
		t.Error("an unknown metering trigger was handled")
	}
}

// TestPriceAndGridTriggers fires the CommodityPrice and
// ElectricalGridConditions triggers.
func TestPriceAndGridTriggers(t *testing.T) {
	f := marketFleet()
	f.energyTariff.build()
	defer f.energyTariff.poll.Stop()
	d := f.energyTariff
	fireMarket(t, f, triggerPriceUpdate)
	if p := d.price.CurrentPrice(); p == nil || *p.Price != 15916 || len(*p.Components) != 2 {
		t.Errorf("CurrentPrice %+v", p)
	}
	fireMarket(t, f, triggerPriceForecastUpdate|0x0000_0001_0000_0000) // endpoint bits are cleared
	if v, _ := d.price.MatterRead(cp.AttrPriceForecast); len(v.(spec.List[energy.CommodityPrice])) != 56 {
		t.Errorf("PriceForecast %d entries", len(v.(spec.List[energy.CommodityPrice])))
	}
	fireMarket(t, f, triggerGridCurrentConditions)
	if v, _ := d.grid.MatterRead(egc.AttrCurrentConditions); v.(energy.GridConditions).GridCarbonIntensity != 230 {
		t.Errorf("CurrentConditions %v", v)
	}
	fireMarket(t, f, triggerGridForecastConditions)
	if v, _ := d.grid.MatterRead(egc.AttrForecastConditions); len(v.(spec.List[energy.GridConditions])) != 48 {
		t.Errorf("ForecastConditions %v", v)
	}
	for amount, want := range map[int64]string{5000: "Low", 15000: "Medium", 30000: "High"} {
		if got := priceDescription(amount); got != want {
			t.Errorf("priceDescription(%d) = %q", amount, got)
		}
	}
	for level, want := range map[int16]energy.ThreeLevel{50: egc.ThreeLevelLow, 200: egc.ThreeLevelMedium, 500: egc.ThreeLevelHigh} {
		if got := carbonLevel(level); got != want {
			t.Errorf("carbonLevel(%d) = %d", level, got)
		}
	}
	if f.energyMarketTestEventTrigger(0x0001_0000_0000_0000) {
		t.Error("a foreign trigger was handled")
	}
}
