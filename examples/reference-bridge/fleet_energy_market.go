// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"fmt"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/energy"
	"github.com/SukramJ/go-fabric/cluster/measurement"
	"github.com/SukramJ/go-fabric/cluster/spec"
	commtr "github.com/SukramJ/go-fabric/cluster/spec/commoditymetering"
	cp "github.com/SukramJ/go-fabric/cluster/spec/commodityprice"
	ct "github.com/SukramJ/go-fabric/cluster/spec/commoditytariff"
	egc "github.com/SukramJ/go-fabric/cluster/spec/electricalgridconditions"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/node"
)

// The energy-market devices: an ElectricalEnergyTariff with CommodityPrice,
// ElectricalGridConditions and CommodityTariff, and an ElectricalMeter with
// CommodityMetering. They are connectedhomeip's energy-gateway app
// (examples/energy-gateway-app/ at the harness pin 6170af84), whose
// endpoint 1 carries all four clusters under the ElectricalEnergyTariff
// device type (energy-gateway-common/energy-gateway-app.matter:2159-2269);
// here the metering is on the ElectricalMeter, the device type that names
// it. The demo data and the test event triggers are the app's, each cited
// where it is used; file names below are under examples/energy-gateway-app/.

const (
	deviceTypeElectricalEnergyTariff uint16 = 0x0513
	deviceTypeElectricalMeter        uint16 = 0x0514
)

// The four clusters' test event triggers (connectedhomeip
// src/app/clusters/<cluster>-server/*TestEventTriggerHandler.h). Each
// handler clears the endpoint bits first (clearEndpointInEventTrigger),
// the mask control.go's smokeTriggerMask applies.
const (
	triggerPriceUpdate              uint64 = 0x0095_0000_0000_0000 // CommodityPriceTestEventTriggerHandler.h:48
	triggerPriceForecastUpdate      uint64 = 0x0095_0000_0000_0001 // :53
	triggerGridCurrentConditions    uint64 = 0x00A0_0000_0000_0000 // ElectricalGridConditionsTestEventTriggerHandler.h:49
	triggerGridForecastConditions   uint64 = 0x00A0_0000_0000_0001 // :55
	triggerTariffDataUpdated        uint64 = 0x0700_0000_0000_0000 // CommodityTariffTestEventTriggerHandler.h:46
	triggerTariffDataClear          uint64 = 0x0700_0000_0000_0001 // :48
	triggerTariffTimeShift24h       uint64 = 0x0700_0000_0000_0002 // :50
	triggerTariffTimeShift4h        uint64 = 0x0700_0000_0000_0003 // :52
	triggerTariffTimeShiftDisable   uint64 = 0x0700_0000_0000_0004 // :54
	triggerMeteringAttributesUpdate uint64 = 0x0b07_0000_0000_0000 // CommodityMeteringTestEventTriggerHandler.h:48
	triggerMeteringAttributesClear  uint64 = 0x0b07_0000_0000_0001 // :50
)

// tariffPollInterval is kTimerPollIntervalInSec
// (commodity-tariff/include/CommodityTariffInstance.h:27).
const tariffPollInterval = 30 * time.Second

// --- the tariff's test clock ---------------------------------------------------

// tariffTestClock is the app's TestTimeManager
// (commodity-tariff/src/CommodityTariffEventTriggers.cpp:42-195): while
// enabled the clock stands still at a set time and moves only when a
// trigger advances it, as chip's MockClock does. chip swaps the whole
// app's system clock; here it is the CommodityTariff server's clock alone.
type tariffTestClock struct {
	mu      sync.Mutex
	enabled bool
	at      time.Time
}

// now is the tariff's clock.
func (c *tariffTestClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.enabled {
		return c.at
	}
	return time.Now()
}

// enable is EnableTestTime (:142-172): enabling an enabled clock changes
// nothing; enabling starts it at unixS, or at the real time when unixS
// is 0 (InitializeMockClock, :64-104); disabling returns to the real
// time.
func (c *tariffTestClock) enable(on bool, unixS int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if on == c.enabled {
		return
	}
	c.enabled = on
	if on {
		c.at = time.Now()
		if unixS != 0 {
			c.at = time.Unix(unixS, 0)
		}
	}
}

// advance is AdvanceTestTime (:174-195) up to the sync: the clock moves
// only while enabled.
func (c *tariffTestClock) advance(d time.Duration) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.enabled {
		return false
	}
	c.at = c.at.Add(d)
	return true
}

// --- the ElectricalEnergyTariff ----------------------------------------------

// demoEnergyTariff is the ElectricalEnergyTariff device: CommodityPrice with
// Forecasting and both detailed requests (CommodityPriceMain.cpp:60-62;
// CodegenIntegration.cpp:35-45 opts into both), ElectricalGridConditions
// with Forecasting (ElectricalGridConditionsMain.cpp:62), and
// CommodityTariff with all six features (CommodityTariffMain.cpp:73-76).
type demoEnergyTariff struct {
	name string

	once    sync.Once
	price   *energy.CommodityPriceServer
	grid    *energy.GridConditionsServer
	tariff  *energy.CommodityTariffServer
	version struct{ price, grid, tariff cluster.DataVersionTracker }
	clock   tariffTestClock

	mu          sync.Mutex
	presetIndex int
	poll        *time.Timer
}

func newDemoEnergyTariff(name string) *demoEnergyTariff { return &demoEnergyTariff{name: name} }

// servers are the cluster servers the device supplies.
func (d *demoEnergyTariff) servers() []contract.ClusterServer {
	d.build()
	return []contract.ClusterServer{d.price, d.grid, d.tariff}
}

func (d *demoEnergyTariff) build() {
	d.once.Do(func() {
		price, err := energy.NewCommodityPrice(energy.CommodityPriceConfig{
			Features: energy.CommodityPriceFeatureForecasting, DetailedPrice: true, DetailedForecast: true,
			DataVersion: &d.version.price,
		})
		if err != nil {
			panic(fmt.Sprintf("tariff CommodityPrice: %v", err))
		}
		// AppInit: GBP with 5 decimal points, kWh
		// (commodity-price/src/CommodityPriceDelegate.cpp:38, :56-60;
		// kCurrencyGBP = 826, src/app/clusters/commodity-price-server/
		// CodegenIntegration.h:43).
		if err := price.SetCurrency(&energy.PriceCurrency{Currency: 826, DecimalPoints: 5}); err != nil {
			panic(fmt.Sprintf("tariff Currency: %v", err))
		}
		grid, err := energy.NewElectricalGridConditions(energy.GridConditionsConfig{
			Features: energy.GridConditionsFeatureForecasting, DataVersion: &d.version.grid,
		})
		if err != nil {
			panic(fmt.Sprintf("tariff ElectricalGridConditions: %v", err))
		}
		// AppInit: local generation available
		// (electrical-grid-conditions/src/ElectricalGridConditionsDelegate.cpp:41-46).
		available := true
		if err := grid.SetLocalGenerationAvailable(&available); err != nil {
			panic(fmt.Sprintf("tariff LocalGenerationAvailable: %v", err))
		}
		tariff, err := energy.NewCommodityTariff(energy.CommodityTariffConfig{
			Features: energy.TariffFeaturePricing | energy.TariffFeatureFriendlyCredit | energy.TariffFeatureAuxiliaryLoad |
				energy.TariffFeaturePeakPeriod | energy.TariffFeaturePowerThreshold | energy.TariffFeatureRandomization,
			Now: d.clock.now, DataVersion: &d.version.tariff,
		})
		if err != nil {
			panic(fmt.Sprintf("tariff CommodityTariff: %v", err))
		}
		d.price, d.grid, d.tariff = price, grid, tariff
		d.poll = time.AfterFunc(tariffPollInterval, d.pollTariff)
	})
}

// pollTariff is TariffTimeUpdCb (commodity-tariff/src/
// CommodityTariffInstance.cpp:66-77): activate a delayed tariff, derive the
// current attributes, schedule the next poll.
func (d *demoEnergyTariff) pollTariff() {
	d.syncTariff()
	d.mu.Lock()
	d.poll.Reset(tariffPollInterval)
	d.mu.Unlock()
}

func (d *demoEnergyTariff) syncTariff() {
	if err := d.tariff.SyncTime(); err != nil {
		slog.Debug("tariff.sync", slog.String("device", d.name), slog.Any("err", err))
	}
}

func matterEpochOf(t time.Time) uint32 { return uint32(t.Unix() - 946684800) } //nolint:gosec // a clock after 2000

// setPriceUpdate is SetTestEventTrigger_PriceUpdate
// (commodity-price/src/CommodityPriceEventTriggers.cpp:34-76): 15.916 p/kWh
// for 30 minutes from now, level 3, "Medium", ExVAT 95 % and VAT 5 %.
func (d *demoEnergyTariff) setPriceUpdate() {
	start := matterEpochOf(time.Now())
	amount := int64(15916)
	p := energy.CommodityPrice{
		PeriodStart: start, PeriodEnd: spec.ValueOf(start + 30*60),
		Price: &amount, PriceLevel: ptr(int16(3)), Description: ptr(priceDescription(amount)),
		Components: ptr(priceComponents(amount)),
	}
	if err := d.price.SetCurrentPrice(&p); err != nil {
		slog.Warn("tariff.price", slog.String("device", d.name), slog.Any("err", err))
	}
}

// priceDescription is the triggers' description (:55, :119).
func priceDescription(amount int64) string {
	switch {
	case amount < 10000:
		return "Low"
	case amount < 24000:
		return "Medium"
	}
	return "High"
}

// priceComponents are the triggers' ExVAT and VAT components (:59-67,
// :123-131).
func priceComponents(amount int64) []energy.CommodityPriceComponent {
	return []energy.CommodityPriceComponent{
		{Price: amount * 95 / 100, Source: cp.TariffPriceTypeStandard, Description: ptr("ExVAT")},
		{Price: amount * 5 / 100, Source: cp.TariffPriceTypeStandard, Description: ptr("VAT")},
	}
}

// setForecastUpdate is SetTestEventTrigger_ForecastUpdate (:78-142): 56
// half-hour entries from now, each a random 4 p to 32 p.
func (d *demoEnergyTariff) setForecastUpdate() {
	start := matterEpochOf(time.Now())
	forecast := make([]energy.CommodityPrice, 56)
	for i := range forecast {
		amount := int64(4000 + rand.IntN(32000-4000+1)) //nolint:gosec // demo data, as the app's rand()
		level := int16(3)
		switch {
		case amount < 10000:
			level = 1
		case amount < 24000:
			level = 2
		}
		forecast[i] = energy.CommodityPrice{
			PeriodStart: start, PeriodEnd: spec.ValueOf(start + 30*60 - 1),
			Price: &amount, PriceLevel: &level, Description: ptr(priceDescription(amount)),
			Components: ptr(priceComponents(amount)),
		}
		start += 30 * 60
	}
	if err := d.price.SetForecast(forecast); err != nil {
		slog.Warn("tariff.forecast", slog.String("device", d.name), slog.Any("err", err))
	}
}

// carbonLevel is the triggers' level of an intensity
// (electrical-grid-conditions/src/ElectricalGridConditionsEventTriggers.cpp:95-102).
func carbonLevel(level int16) energy.ThreeLevel {
	switch {
	case level < 100:
		return egc.ThreeLevelLow
	case level < 300:
		return egc.ThreeLevelMedium
	}
	return egc.ThreeLevelHigh
}

// setGridCurrentConditions is SetTestEventTrigger_CurrentConditionsUpdate
// (ElectricalGridConditionsEventTriggers.cpp:31-60): 230 g/kWh Medium on
// the grid, 0 g/kWh Low locally, for 30 minutes from now.
func (d *demoEnergyTariff) setGridCurrentConditions() {
	start := matterEpochOf(time.Now())
	c := energy.GridConditions{
		PeriodStart: start, PeriodEnd: spec.ValueOf(start + 30*60),
		GridCarbonIntensity: 230, GridCarbonLevel: egc.ThreeLevelMedium,
		LocalCarbonIntensity: 0, LocalCarbonLevel: egc.ThreeLevelLow,
	}
	if err := d.grid.SetCurrentConditions(&c); err != nil {
		slog.Warn("tariff.grid", slog.String("device", d.name), slog.Any("err", err))
	}
}

// setGridForecastConditions is SetTestEventTrigger_ForecastConditionsUpdate
// (:62-114): 48 half-hour entries from now, each a random 0 to 600 g/kWh.
func (d *demoEnergyTariff) setGridForecastConditions() {
	start := matterEpochOf(time.Now())
	forecast := make([]energy.GridConditions, 48)
	for i := range forecast {
		level := int16(rand.IntN(601)) //nolint:gosec // demo data, as the app's rand()
		forecast[i] = energy.GridConditions{
			PeriodStart: start, PeriodEnd: spec.ValueOf(start + 30*60 - 1),
			GridCarbonIntensity: level, GridCarbonLevel: carbonLevel(level),
			LocalCarbonIntensity: level, LocalCarbonLevel: carbonLevel(level),
		}
		start += 30 * 60
	}
	if err := d.grid.SetForecastConditions(forecast); err != nil {
		slog.Warn("tariff.gridforecast", slog.String("device", d.name), slog.Any("err", err))
	}
}

// setTariffDataUpdated is SetTestEventTrigger_TariffDataUpdated
// (commodity-tariff/src/CommodityTariffEventTriggers.cpp:207-284): the next
// of the two presets, alternating, with the test clock enabled at the
// preset's TariffTestTimestamp (a no-op while it already runs, :142-156).
func (d *demoEnergyTariff) setTariffDataUpdated() {
	d.mu.Lock()
	preset := tariffPresets[d.presetIndex]
	d.presetIndex = (d.presetIndex + 1) % len(tariffPresets) // GetNextPreset, :200-205
	d.mu.Unlock()
	d.clock.enable(true, preset.testTimestamp)
	if err := d.tariff.UpdateTariff(preset.tariff()); err != nil {
		slog.Warn("tariff.update", slog.String("device", d.name), slog.Any("err", err))
	}
}

// shiftTariffTime is SetTestEventTrigger_TimeShift24h and _TimeShift4h
// (:297-311): enable the test clock (at the real time, unless it runs),
// advance it, and derive the current attributes again.
func (d *demoEnergyTariff) shiftTariffTime(by time.Duration) {
	d.clock.enable(true, 0)
	if d.clock.advance(by) {
		d.syncTariff()
	}
}

// testEventTrigger handles the CommodityPrice, ElectricalGridConditions
// and CommodityTariff triggers as the energy-gateway app's handlers do
// (CommodityPriceEventTriggers.cpp:144-164,
// ElectricalGridConditionsEventTriggers.cpp:116-136,
// CommodityTariffEventTriggers.cpp:318-349). False for any other.
func (d *demoEnergyTariff) testEventTrigger(trigger uint64) bool {
	d.build()
	switch trigger & smokeTriggerMask {
	case triggerPriceUpdate:
		d.setPriceUpdate()
	case triggerPriceForecastUpdate:
		d.setForecastUpdate()
	case triggerGridCurrentConditions:
		d.setGridCurrentConditions()
	case triggerGridForecastConditions:
		d.setGridForecastConditions()
	case triggerTariffDataUpdated:
		d.setTariffDataUpdated()
	case triggerTariffDataClear: // :286-291
		if err := d.tariff.ClearTariff(); err != nil {
			slog.Warn("tariff.clear", slog.String("device", d.name), slog.Any("err", err))
		}
	case triggerTariffTimeShift24h:
		d.shiftTariffTime(24 * time.Hour)
	case triggerTariffTimeShift4h:
		d.shiftTariffTime(4 * time.Hour)
	case triggerTariffTimeShiftDisable: // :313-316
		d.clock.enable(false, 0)
	default:
		return false
	}
	return true
}

// --- the tariff presets ------------------------------------------------------

// tariffPreset is a TariffDataSet
// (commodity-tariff/include/CommodityTariffSamples.h:354-368).
type tariffPreset struct {
	tariff        func() energy.Tariff
	testTimestamp int64
}

// tariffPresets are kTariffPresets (CommodityTariffSamples.h:373-415):
// Sample 1 at 01-Dec-2024, Sample 2 at 01-Jun-2025 (Unix seconds, :393,
// :413).
var tariffPresets = [...]tariffPreset{
	{tariff: tariffSample1, testTimestamp: 1733011200},
	{tariff: tariffSample2, testTimestamp: 1748736000},
}

// tariffDayEntry is one sample day entry: a duration, randomization 0 and
// None (CommodityTariffSamples.h:47-123, :227-263).
func tariffDayEntry(id uint32, start, duration uint16) energy.TariffDayEntry {
	return energy.TariffDayEntry{
		DayEntryId: id, StartTime: start, Duration: ptr(duration),
		RandomizationOffset: ptr(int16(0)), RandomizationType: ptr(ct.DayEntryRandomizationTypeNone),
	}
}

// tariffSampleComponent is one sample tariff component: a Standard price,
// no friendly credit, auxiliary load 1 Off, a Low peak of 1, a Contract
// power threshold (CommodityTariffSamples.h:143-177, :278-329).
func tariffSampleComponent(id uint32, price int64, level int16, power, apparent int64, threshold spec.Nullable[int64], label string) energy.TariffComponent {
	return energy.TariffComponent{
		TariffComponentId: id,
		Price:             ptr(spec.ValueOf(ct.TariffPriceStruct{PriceType: ct.TariffPriceTypeStandard, Price: &price, PriceLevel: &level})),
		FriendlyCredit:    ptr(false),
		AuxiliaryLoad:     &ct.AuxiliaryLoadSwitchSettingsStruct{Number: 1, RequiredState: ct.AuxiliaryLoadSettingOff},
		PeakPeriod:        &ct.PeakPeriodStruct{Severity: ct.PeakPeriodSeverityLow, PeakPeriod: 1},
		PowerThreshold: &ct.PowerThresholdStruct{
			PowerThreshold: &power, ApparentPowerThreshold: &apparent,
			PowerThresholdSource: spec.ValueOf(ct.PowerThresholdSourceContract),
		},
		Threshold: threshold,
		Label:     ptr(spec.ValueOf(label)),
		Predicted: ptr(false),
	}
}

func tariffSamplePeriod(label string, dayEntries []uint32, components ...uint32) energy.TariffPeriod {
	return energy.TariffPeriod{Label: spec.ValueOf(label), DayEntryIds: dayEntries, TariffComponentIds: components}
}

// tariffSampleHead is what both presets share: kWh, StartDate 0,
// DefaultRandomizationOffset 0 and Type None (CommodityTariffSamples.h:
// 376-379, :397-400).
func tariffSampleHead() energy.Tariff {
	return energy.Tariff{
		TariffUnit: ptr(ct.TariffUnitKWh), StartDate: ptr(uint32(0)),
		DefaultRandomizationOffset: ptr(int16(0)), DefaultRandomizationType: ptr(ct.DayEntryRandomizationTypeNone),
	}
}

// tariffSample1 is Sample1 (CommodityTariffSamples.h:29-209): two day
// patterns, Sunday-Tuesday-Thursday-Saturday and Monday-Wednesday-Friday,
// one Holiday, two components without thresholds, five periods.
func tariffSample1() energy.Tariff {
	t := tariffSampleHead()
	t.TariffInfo = &energy.TariffInformation{ // :33-40
		TariffLabel: spec.ValueOf("Full Tariff One"), ProviderName: spec.ValueOf("Default Provider"),
		Currency:  ptr(spec.ValueOf(ct.Currency{Currency: 120, DecimalPoints: 0})),
		BlockMode: spec.ValueOf(ct.BlockModeNoBlock),
	}
	t.DayEntries = []energy.TariffDayEntry{ // :47-123
		tariffDayEntry(10, 0, 240), tariffDayEntry(11, 240, 240), tariffDayEntry(12, 480, 240), tariffDayEntry(13, 720, 240),
		tariffDayEntry(14, 960, 240), tariffDayEntry(15, 1200, 240), tariffDayEntry(16, 1440, 60),
		tariffDayEntry(20, 0, 240), tariffDayEntry(21, 240, 240), tariffDayEntry(22, 480, 240), tariffDayEntry(23, 720, 240),
		tariffDayEntry(24, 960, 240), tariffDayEntry(25, 1200, 240), tariffDayEntry(26, 1440, 60),
		tariffDayEntry(30, 0, 1500),
	}
	t.DayPatterns = []energy.TariffDayPattern{ // :126-131
		{DayPatternId: 0, DaysOfWeek: 0x55, DayEntryIds: []uint32{10, 11, 12, 13, 14, 15, 16}},
		{DayPatternId: 1, DaysOfWeek: 0x2A, DayEntryIds: []uint32{20, 21, 22, 23, 24, 25, 26}},
	}
	t.CalendarPeriods = []energy.TariffCalendarPeriod{{StartDate: spec.ValueOf(uint32(0)), DayPatternIds: []uint32{0, 1}}} // :136-137
	t.IndividualDays = []energy.TariffDay{{Date: 1745427104, DayType: ct.DayTypeHoliday, DayEntryIds: []uint32{30}}}       // :139-141
	t.TariffComponents = []energy.TariffComponent{                                                                         // :143-178
		tariffSampleComponent(10, 15, 1, 2400000, 120, spec.NullOf[int64](), "Tariff Component 1"),
		tariffSampleComponent(20, 20, 0, 4800000, 240, spec.NullOf[int64](), "Tariff Component 2"),
	}
	t.TariffPeriods = []energy.TariffPeriod{ // :180-207
		tariffSamplePeriod("Period 1", []uint32{10, 14, 21, 25}, 10),
		tariffSamplePeriod("Period 2", []uint32{11, 15, 22, 26}, 20),
		tariffSamplePeriod("Period 3", []uint32{12, 16, 23}, 10),
		tariffSamplePeriod("Period 4", []uint32{13, 20, 24}, 20),
		tariffSamplePeriod("Period 5", []uint32{30}, 10),
	}
	return t
}

// tariffSample2 is Sample2 (CommodityTariffSamples.h:211-352): one pattern
// for every day, three components with thresholds under BlockMode
// Combined, three periods, no individual days.
func tariffSample2() energy.Tariff {
	t := tariffSampleHead()
	t.TariffInfo = &energy.TariffInformation{ // :215-222
		TariffLabel: spec.ValueOf("Full Tariff Two"), ProviderName: spec.ValueOf("Example Provider"),
		Currency:  ptr(spec.ValueOf(ct.Currency{Currency: 200, DecimalPoints: 1})),
		BlockMode: spec.ValueOf(ct.BlockModeCombined),
	}
	t.DayEntries = []energy.TariffDayEntry{ // :227-263
		tariffDayEntry(10, 0, 240), tariffDayEntry(11, 240, 240), tariffDayEntry(12, 480, 240), tariffDayEntry(13, 720, 240),
		tariffDayEntry(14, 960, 240), tariffDayEntry(15, 1200, 240), tariffDayEntry(16, 1440, 60),
	}
	t.DayPatterns = []energy.TariffDayPattern{{DayPatternId: 0, DaysOfWeek: 0x7F, DayEntryIds: []uint32{10, 11, 12, 13, 14, 15, 16}}} // :266-268
	t.CalendarPeriods = []energy.TariffCalendarPeriod{{StartDate: spec.ValueOf(uint32(0)), DayPatternIds: []uint32{0}}}               // :273-275
	third := tariffSampleComponent(30, 50, 0, 3200000, 320, spec.ValueOf(int64(320)), "Tariff Component 3")                           // :313-329
	third.FriendlyCredit, third.Predicted = ptr(true), ptr(true)
	t.TariffComponents = []energy.TariffComponent{ // :278-330
		tariffSampleComponent(10, 15, 1, 2400000, 120, spec.ValueOf(int64(120)), "Tariff Component 1"),
		tariffSampleComponent(20, 20, 0, 4800000, 240, spec.ValueOf(int64(240)), "Tariff Component 2"),
		third,
	}
	t.TariffPeriods = []energy.TariffPeriod{ // :332-350
		tariffSamplePeriod("Period 1", []uint32{10, 13, 16}, 10),
		tariffSamplePeriod("Period 2", []uint32{11, 14}, 20),
		tariffSamplePeriod("Period 3", []uint32{12, 15}, 30),
	}
	return t
}

// --- the ElectricalMeter -----------------------------------------------------

// demoElectricalMeter is the ElectricalMeter device: ElectricalPowerMeasurement
// and ElectricalEnergyMeasurement — the charger's unobserved readings
// (fleet_evse.go evseMeter), mounted through the same constructors
// ElectricalSensor uses (cluster/measurement electricalSensorServers) —
// and CommodityMetering with the energy-gateway app's test data. Every
// CommodityMetering attribute starts null (commodity-metering-server.cpp:
// 150-158), so the first AttributesValueUpdate changes all four
// (TC_COMMTR_3_1.py steps 9-12).
type demoElectricalMeter struct {
	name string

	once     sync.Once
	power    contract.ClusterServer
	energy   contract.ClusterServer
	metering *energy.CommodityMeteringServer
	version  cluster.DataVersionTracker

	// The trigger file's TestDataManager (commodity-metering/src/
	// CommodityMeteringEventTriggers.cpp:62-256): whether it has read the
	// instance (mInstance), and its TariffUnit, which UpdAttrs toggles.
	mu    sync.Mutex
	saved bool
	unit  *energy.MeteringTariffUnit
}

func newDemoElectricalMeter(name string) *demoElectricalMeter {
	return &demoElectricalMeter{name: name}
}

// servers are the cluster servers the device supplies.
func (m *demoElectricalMeter) servers() []contract.ClusterServer {
	m.build()
	return []contract.ClusterServer{m.power, m.energy, m.metering}
}

func (m *demoElectricalMeter) build() {
	m.once.Do(func() {
		readings := evseMeter{}
		m.power = measurement.NewElectricalPowerServerFromReadings(readings)
		m.energy = measurement.NewElectricalEnergyServer(readings)
		metering, err := energy.NewCommodityMetering(energy.CommodityMeteringConfig{DataVersion: &m.version})
		if err != nil {
			panic(fmt.Sprintf("meter CommodityMetering: %v", err))
		}
		m.metering = metering
	})
}

// meteredQuantitySamples are MeteredQuantitySamples (CommodityMeteringEventTriggers.cpp:
// 29-58): each entry's Quantity is its number of component ids. Sample2's
// third entry takes TariffComponents3 with the length of TariffComponents2
// (:54), so its ids are 2031 and 2032.
var meteredQuantitySamples = [2][]energy.MeteredQuantity{
	{
		{TariffComponentIds: []uint32{1011, 1012}, Quantity: 2},
		{TariffComponentIds: []uint32{1021, 1022, 1023}, Quantity: 3},
	},
	{
		{TariffComponentIds: []uint32{2011}, Quantity: 1},
		{TariffComponentIds: []uint32{2021, 2023}, Quantity: 2},
		{TariffComponentIds: []uint32{2031, 2032}, Quantity: 3},
	},
}

// update is TestDataManager::Update (:241-249): save the attributes on
// the first update (SaveAttributes, :163-171), then UpdAttrs (:192-235) —
// the timestamp is now, the tariff unit toggles kWh/kVAh (starting kVAh
// from null), the sample is the first for kVAh and the second for kWh, and
// MaximumMeteredQuantities is its length. The app ignores the setters'
// results.
func (m *demoElectricalMeter) update() {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.metering
	if !m.saved {
		m.saved, m.unit = true, s.TariffUnit()
	}
	ts := matterEpochOf(time.Now())
	unit := commtr.TariffUnitKVAh
	if m.unit != nil && *m.unit == commtr.TariffUnitKVAh {
		unit = commtr.TariffUnitKWh
	}
	m.unit = &unit
	sample := meteredQuantitySamples[0]
	if unit == commtr.TariffUnitKWh {
		sample = meteredQuantitySamples[1]
	}
	maximum := uint16(len(sample)) //nolint:gosec // two or three
	m.log(s.SetMaximumMeteredQuantities(&maximum))
	m.log(s.SetMeteredQuantity(sample))
	m.log(s.SetMeteredQuantityTimestamp(&ts))
	m.log(s.SetTariffUnit(&unit))
}

// clear is TestDataManager::Clear (:251-255): ClearAttributes nulls the
// saved values before RestoreAttributes writes them, so every attribute
// becomes null; the saved state stays (mInstance is not reset).
func (m *demoElectricalMeter) clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.metering
	m.unit = nil
	m.log(s.SetMaximumMeteredQuantities(nil))
	m.log(s.SetMeteredQuantity(nil))
	m.log(s.SetMeteredQuantityTimestamp(nil))
	m.log(s.SetTariffUnit(nil))
}

func (m *demoElectricalMeter) log(err error) {
	if err != nil {
		slog.Warn("meter.metering", slog.String("device", m.name), slog.Any("err", err))
	}
}

// testEventTrigger handles the CommodityMetering triggers as
// HandleCommodityMeteringTestEventTrigger does (:261-280).
func (m *demoElectricalMeter) testEventTrigger(trigger uint64) bool {
	m.build()
	switch trigger & smokeTriggerMask {
	case triggerMeteringAttributesUpdate:
		m.update()
	case triggerMeteringAttributesClear:
		m.clear()
	default:
		return false
	}
	return true
}

// device builds the grid meter. ElectricalMeter requires an
// ElectricalSensor device type, min 1 (matter.js
// packages/model/src/standard/elements/electrical-meter.element.ts:21):
// the meter's sensor is a part, as the charger's is (fleet_evse.go), served
// from its reading.
func (m *demoElectricalMeter) device(opts device.Options) (node.Endpoint, error) {
	meter, err := device.NewElectricalMeter(opts, serve(m.servers())...)
	if err != nil {
		return nil, err
	}
	sensor, err := device.NewElectricalSensor(part("demo:electricalmeter:sensor"), measured(evseMeter{}))
	if err != nil {
		return nil, err
	}
	if err := meter.AddPart(sensor.Device); err != nil {
		return nil, err
	}
	return meter, nil
}
