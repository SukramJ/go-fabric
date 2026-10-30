// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"log/slog"
	"sync"
	"time"

	mtrid "github.com/SukramJ/go-fabric/cluster/spec/meteridentification"
	thermostatdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	whmgdef "github.com/SukramJ/go-fabric/cluster/spec/waterheatermanagement"
	whmdef "github.com/SukramJ/go-fabric/cluster/spec/waterheatermode"
	"github.com/SukramJ/go-fabric/device"
)

// --- device: a water heater ------------------------------------------------

// deviceTypeWaterHeater is WaterHeater (parity/schema.json, 0x050F rev 1):
// WaterHeaterManagement, WaterHeaterMode and Thermostat with HEAT are
// mandatory, Identify optional (the assembler mounts it on every bridged
// endpoint).
const deviceTypeWaterHeater uint16 = 0x050F

// Water-heater modes (WaterHeaterMode SupportedModes): connectedhomeip's
// water-heater app's three, values and tags (examples/water-heater-app/
// water-heater-common/include/water-heater-mode.h:29-48 at the harness
// pin), so the certification cases' trigger semantics hold.
const (
	waterHeaterOff    uint8 = 0
	waterHeaterManual uint8 = 1
	waterHeaterTimed  uint8 = 2
)

// Water temperatures, in 0.01 °C, and the tank, as the water-heater app's
// test event triggers set them (src/WaterHeaterManufacturer.cpp:47,
// :160-231).
const (
	waterColdTemperature   int16  = 2000
	waterTargetTemperature int16  = 6000
	waterTankVolume        uint16 = 100
)

// The WaterHeaterManagement test event triggers (connectedhomeip
// src/app/clusters/water-heater-management-server/
// WaterHeaterManagementTestEventTriggerHandler.h:46-70). The handler
// clears the endpoint bits first (:75, clearEndpointInEventTrigger), the
// same mask the SmokeCoAlarm triggers use here (control.go
// smokeTriggerMask).
const (
	triggerWaterHeaterInstallation      uint64 = 0x0094_0000_0000_0000
	triggerWaterHeaterInstallationClear uint64 = 0x0094_0000_0000_0001
	triggerWaterTemperature20C          uint64 = 0x0094_0000_0000_0002
	triggerWaterTemperature61C          uint64 = 0x0094_0000_0000_0003
	triggerWaterTemperature66C          uint64 = 0x0094_0000_0000_0004
	triggerWaterHeaterManual            uint64 = 0x0094_0000_0000_0005
	triggerWaterHeaterOff               uint64 = 0x0094_0000_0000_0006
	triggerDrawOffHotWater              uint64 = 0x0094_0000_0000_0007
)

// demoWaterHeater is a hot-water tank with two immersion elements. Its
// tank model is a transcription of connectedhomeip's water-heater app
// (examples/water-heater-app/water-heater-common/src/
// WaterHeaterDelegateImpl.cpp and WaterHeaterManufacturer.cpp at the
// harness pin), the device the EWATERHTR certification cases are written
// against: the tank's average temperature against a target decides the
// heat demand, the hot-water share is the tank percentage, and a boost
// heats regardless of the mode until its duration ends or — one-shot —
// its target is reached.
//
// One difference: HeaterTypes is fixed ("F") in the kit's server, so
// the tank declares both elements from the start; the app declares one and
// adds the second in its installation trigger (WaterHeaterManufacturer.cpp
// :46, :170-171), the one an emergency boost turns on (:101-107).
type demoWaterHeater struct {
	name string

	mu        sync.Mutex
	whm       *whmgdef.Server
	modes     *whmdef.Server
	thermo    *thermostatdef.Server
	target    int16 // mTargetWaterTemperature
	water     int16 // mWaterTemperature
	boosting  bool  // mBoostState == kActive
	boost     whmgdef.WaterHeaterBoostInfoStruct
	reached   bool // mBoostTargetTemperatureReached
	boostEnds *time.Timer
}

var (
	_ whmgdef.Booster    = (*demoWaterHeater)(nil)
	_ whmdef.ModeChanger = (*demoWaterHeater)(nil)
)

func newDemoWaterHeater(name string) *demoWaterHeater { return &demoWaterHeater{name: name} }

// device builds the water heater: WaterHeaterManagement with EM and TP, as
// the app serves them (WaterHeaterMain.cpp:105-106), both immersion
// elements; the three modes; a heating Thermostat from 30 °C to 80 °C at
// the target. The tank starts as the app's does: empty readings, nothing
// reported until a trigger or the host moves the water.
func (w *demoWaterHeater) device(opts device.Options) (*device.WaterHeater, error) {
	heaters := whmgdef.WaterHeaterHeatSourceImmersionElement1 | whmgdef.WaterHeaterHeatSourceImmersionElement2
	mode := func(label string, m uint8, tag whmdef.ModeTag) whmdef.ModeOptionStruct {
		return whmdef.ModeOptionStruct{Label: label, Mode: m, ModeTags: []whmdef.ModeTagStruct{{Value: tag}}}
	}
	modes := []whmdef.ModeOptionStruct{
		mode("Off", waterHeaterOff, whmdef.ModeTagOff), mode("Manual", waterHeaterManual, whmdef.ModeTagManual),
		mode("Timed", waterHeaterTimed, whmdef.ModeTagTimed),
	}
	off, minHeat, maxHeat, setpoint := waterHeaterOff, int16(3000), int16(8000), waterTargetTemperature
	d, err := device.NewWaterHeater(opts,
		whmgdef.Declare{
			Features: whmgdef.FeatureEnergyManagement | whmgdef.FeatureTankPercent,
			Booster:  w,
			Initial:  whmgdef.Patch{HeaterTypes: &heaters},
		},
		whmdef.Declare{ModeChanger: w, Initial: whmdef.Patch{SupportedModes: &modes, CurrentMode: &off}},
		thermostatdef.Declare{
			Features: thermostatdef.FeatureHeating,
			Initial: thermostatdef.Patch{
				AbsMinHeatSetpointLimit: &minHeat, AbsMaxHeatSetpointLimit: &maxHeat, OccupiedHeatingSetpoint: &setpoint,
			},
		})
	if err != nil {
		return nil, err
	}
	w.mu.Lock()
	w.whm, w.modes, w.thermo = d.WaterHeaterManagement(), d.WaterHeaterMode(), d.Thermostat()
	w.mu.Unlock()
	return d, nil
}

// ChangeToMode implements [whmdef.ModeChanger]: every supported mode is
// taken, as the app's mode delegate takes it (water-heater-mode.cpp:39-42).
func (w *demoWaterHeater) ChangeToMode(_ context.Context, newMode uint8) (status uint8, text string, err error) {
	slog.Info("waterheater.mode", slog.String("device", w.name), slog.Int("mode", int(newMode)))
	return uint8(whmdef.ModeChangeStatusSuccess), "", nil
}

// Boost implements [whmgdef.Booster]: the app's HandleBoost
// (WaterHeaterDelegateImpl.cpp:158-222) — keep the parameters, restart the
// duration timer, then turn the heat on or off as the boost requires. The
// server sets BoostState and emits BoostStarted when this returns.
func (w *demoWaterHeater) Boost(_ context.Context, info whmgdef.WaterHeaterBoostInfoStruct) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.boost, w.reached, w.boosting = info, false, true
	w.stopTimerLocked()
	w.boostEnds = time.AfterFunc(time.Duration(info.Duration)*time.Second, w.boostExpired)
	slog.Info("waterheater.boost", slog.String("device", w.name), slog.Int("seconds", int(info.Duration)))
	w.changeHeatingLocked()
	return nil
}

// CancelBoost implements [whmgdef.Booster]: the app's HandleCancelBoost
// (WaterHeaterDelegateImpl.cpp:270-285) for a running boost — the server
// does not call it for none.
func (w *demoWaterHeater) CancelBoost(context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.boosting = false
	w.boost.EmergencyBoost = nil
	w.stopTimerLocked()
	w.changeHeatingLocked()
	return nil
}

// boostExpired is the app's HandleBoostTimerExpiry
// (WaterHeaterDelegateImpl.cpp:234-259).
func (w *demoWaterHeater) boostExpired() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.boosting {
		return
	}
	w.boosting = false
	w.boostEnds = nil
	w.changeHeatingLocked()
	w.endBoostLocked()
}

func (w *demoWaterHeater) stopTimerLocked() {
	if w.boostEnds != nil {
		w.boostEnds.Stop()
		w.boostEnds = nil
	}
}

func (w *demoWaterHeater) endBoostLocked() {
	inactive := whmgdef.BoostStateInactive
	if err := w.whm.Set(whmgdef.Patch{BoostState: &inactive}); err != nil {
		slog.Warn("waterheater.boost_end", slog.String("device", w.name), slog.Any("err", err))
	}
}

// activeTargetLocked is the app's GetActiveTargetWaterTemperature
// (WaterHeaterDelegateImpl.cpp:305-318).
func (w *demoWaterHeater) activeTargetLocked() int16 {
	if w.boosting && w.boost.TemporarySetpoint != nil {
		return *w.boost.TemporarySetpoint
	}
	return w.target
}

// tankPercentageLocked is the app's CalculateTankPercentage
// (WaterHeaterDelegateImpl.cpp:320-335).
func (w *demoWaterHeater) tankPercentageLocked() uint8 {
	percent := int32(100)
	if divisor := int32(w.activeTargetLocked()) - int32(waterColdTemperature); divisor > 0 {
		percent = 100 * (int32(w.water) - int32(waterColdTemperature)) / divisor
	}
	return uint8(min(max(percent, 0), 100)) //nolint:gosec // clamped to 0..100
}

// reachedTargetLocked is the app's HasWaterTemperatureReachedTarget
// (WaterHeaterDelegateImpl.cpp:380-419).
func (w *demoWaterHeater) reachedTargetLocked() bool {
	if !w.boosting {
		return w.water >= w.activeTargetLocked()
	}
	tank := w.whm.Get().TankPercentage
	switch {
	case w.reached && w.boost.TargetReheat != nil:
		return tank >= *w.boost.TargetReheat
	case w.boost.TargetPercentage != nil:
		return tank >= *w.boost.TargetPercentage
	default:
		return w.water >= w.activeTargetLocked()
	}
}

// changeHeatingLocked is the app's ChangeHeatingIfNecessary and
// DetermineIfChangingHeatingState (WaterHeaterDelegateImpl.cpp:421-509),
// with TurnHeatingOn / TurnHeatingOff (WaterHeaterManufacturer.cpp:93-127).
func (w *demoWaterHeater) changeHeatingLocked() {
	demand := w.whm.Get().HeatDemand
	mode := w.modes.Get().CurrentMode
	on, off := false, false
	switch {
	case !w.reachedTargetLocked():
		if demand == 0 {
			if w.boosting {
				w.reached = false
			}
			on = w.boosting || mode == waterHeaterManual
		} else if !w.boosting && mode == waterHeaterOff {
			off = true
		}
	case demand != 0:
		off = true
		w.reached = w.boosting
	}
	switch {
	case on:
		next := whmgdef.WaterHeaterHeatSourceImmersionElement1
		if w.boost.EmergencyBoost != nil && *w.boost.EmergencyBoost {
			next |= whmgdef.WaterHeaterHeatSourceImmersionElement2
		}
		w.setDemandLocked(next)
	case off:
		// A one-shot boost whose heating stops has reached its target:
		// the boost is complete (:437-455).
		if w.boosting && w.boost.OneShot != nil && *w.boost.OneShot {
			w.boosting = false
			w.stopTimerLocked()
			w.boost.EmergencyBoost = nil
			w.endBoostLocked()
		}
		w.setDemandLocked(0)
	}
}

func (w *demoWaterHeater) setDemandLocked(demand whmgdef.WaterHeaterHeatSourceBitmap) {
	if err := w.whm.Set(whmgdef.Patch{HeatDemand: &demand}); err != nil {
		slog.Warn("waterheater.demand", slog.String("device", w.name), slog.Any("err", err))
	}
}

// setWaterLocked is the app's SetWaterTemperature
// (WaterHeaterDelegateImpl.cpp:342-354).
func (w *demoWaterHeater) setWaterLocked(centi int16) {
	w.water = centi
	w.reportLocked()
	w.changeHeatingLocked()
}

// drawOffLocked is the app's DrawOffHotWater
// (WaterHeaterDelegateImpl.cpp:364-378): percent of the tank replaced by
// water at centi.
func (w *demoWaterHeater) drawOffLocked(percent int32, centi int16) {
	w.water = int16((int32(w.water)*(100-percent) + int32(centi)*percent) / 100) //nolint:gosec // a weighted mean of two int16
	w.reportLocked()
	w.changeHeatingLocked()
}

// reportLocked reports the tank percentage and the water temperature, the
// latter as the thermostat's LocalTemperature.
func (w *demoWaterHeater) reportLocked() {
	tank := w.tankPercentageLocked()
	if err := w.whm.Set(whmgdef.Patch{TankPercentage: &tank}); err != nil {
		slog.Warn("waterheater.tank", slog.String("device", w.name), slog.Any("err", err))
	}
	water := w.water
	if err := w.thermo.Set(thermostatdef.Patch{LocalTemperature: &water}); err != nil {
		slog.Warn("waterheater.temperature", slog.String("device", w.name), slog.Any("err", err))
	}
}

// setModeLocked is the app's SetWaterHeaterMode (WaterHeaterDelegateImpl.cpp:
// 511-529).
func (w *demoWaterHeater) setModeLocked(mode uint8) {
	if err := w.modes.Set(whmdef.Patch{CurrentMode: &mode}); err != nil {
		slog.Warn("waterheater.mode", slog.String("device", w.name), slog.Any("err", err))
		return
	}
	w.changeHeatingLocked()
}

// testEventTrigger handles the WaterHeaterManagement triggers as the
// app's HandleWaterHeaterManagementTestEventTrigger does
// (WaterHeaterManufacturer.cpp:160-231, :240-292); false for a trigger
// that is not one of them.
func (w *demoWaterHeater) testEventTrigger(trigger uint64) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	switch trigger & smokeTriggerMask {
	case triggerWaterHeaterInstallation:
		// A 100 l tank of water at 20 °C, target 60 °C, mode unchanged.
		volume := waterTankVolume
		if err := w.whm.Set(whmgdef.Patch{TankVolume: &volume}); err != nil {
			slog.Warn("waterheater.volume", slog.String("device", w.name), slog.Any("err", err))
		}
		w.target = waterTargetTemperature
		w.changeHeatingLocked()
		w.drawOffLocked(100, waterColdTemperature)
	case triggerWaterHeaterInstallationClear:
	case triggerWaterTemperature20C:
		w.setWaterLocked(2000)
	case triggerWaterTemperature61C:
		w.setWaterLocked(6100)
	case triggerWaterTemperature66C:
		w.setWaterLocked(6600)
	case triggerWaterHeaterManual:
		w.setModeLocked(waterHeaterManual)
	case triggerWaterHeaterOff:
		w.setModeLocked(waterHeaterOff)
	case triggerDrawOffHotWater:
		w.drawOffLocked(25, waterColdTemperature)
	default:
		return false
	}
	return true
}

// --- device: an electricity meter --------------------------------------------

// demoMeter is the utility's electricity meter, identified and nothing
// more: the energy readings it may carry are ElectricalMeter parts
// ("[ElectricalEnergy].a+", optional) it does not have.
//
// MeterType starts null, as CHIP's server starts every attribute
// (meter-identification-server.cpp:59-65, Instance::Init): TC-MTRID-3.1
// requires the first test event to change MeterType, PointOfDelivery and
// MeterSerialNumber (TC_MTRIDTestBase.py:113-120, :152-158), and the
// first preset's MeterType is Utility (MeterIdentificationEventTriggers.cpp:56).
type demoMeter struct {
	name string

	mu  sync.Mutex
	srv *mtrid.Server
	// saved holds the identification before the first test event; nil
	// when none is pending (the app's mInstance, EventTriggers.cpp:32).
	saved      *mtrid.State
	presetsIdx int
}

func newDemoMeter(name string) *demoMeter { return &demoMeter{name: name} }

// device builds the meter: MeterSerialNumber set, MeterType and
// PointOfDelivery null.
func (m *demoMeter) device(opts device.Options) (*device.ElectricalUtilityMeter, error) {
	serial := "DEMO-0001"
	d, err := device.NewElectricalUtilityMeter(opts, mtrid.Declare{Initial: mtrid.Patch{
		MeterSerialNumber: &serial, Null: []uint32{mtrid.AttrMeterType, mtrid.AttrPointOfDelivery},
	}})
	if err != nil {
		return withheldOr(d, err)
	}
	m.mu.Lock()
	m.srv = d.MeterIdentification()
	m.mu.Unlock()
	return d, nil
}

// identification is the Patch that sets the three identifying attributes
// to st's (nil to null).
func identification(st mtrid.State) mtrid.Patch {
	p := mtrid.Patch{MeterType: st.MeterType, PointOfDelivery: st.PointOfDelivery, MeterSerialNumber: st.MeterSerialNumber}
	for id, v := range map[uint32]bool{
		mtrid.AttrMeterType: st.MeterType == nil, mtrid.AttrPointOfDelivery: st.PointOfDelivery == nil,
		mtrid.AttrMeterSerialNumber: st.MeterSerialNumber == nil,
	} {
		if v {
			p.Null = append(p.Null, id)
		}
	}
	return p
}

// The MeterIdentification test event triggers (connectedhomeip
// src/app/clusters/meter-identification-server/
// MeterIdentificationTestEventTriggerHandler.h:44-51). The handler clears
// the endpoint bits first (:64, clearEndpointInEventTrigger), the same
// mask the SmokeCoAlarm triggers use here (control.go smokeTriggerMask).
const (
	triggerMeterAttributesValueUpdate      uint64 = 0x0b06_0000_0000_0000
	triggerMeterAttributesValueUpdateClear uint64 = 0x0b06_0000_0000_0001
)

// meterPresets are the energy-gateway app's TestsDataPresets
// (examples/energy-gateway-app/meter-identification/src/
// MeterIdentificationEventTriggers.cpp:55-68), for the attributes this
// meter serves: ProtocolVersion and PowerThreshold (the PTH feature) are
// not served, so their preset values have nowhere to go.
var meterPresets = [2]mtrid.State{
	{
		MeterType:         new(mtrid.MeterTypeUtility),
		PointOfDelivery:   new("Test delivery point"),
		MeterSerialNumber: new("TST-123456789"),
	},
	{
		MeterType:         new(mtrid.MeterTypePrivate),
		PointOfDelivery:   new("New delivery point"),
		MeterSerialNumber: new("NEW-987654321"),
	},
}

// testEventTrigger handles the MeterIdentification triggers as the
// energy-gateway app's HandleMeterIdentificationTestEventTrigger does
// (MeterIdentificationEventTriggers.cpp:219-238). AttributesValueUpdate
// saves the identification on the first update after a clear (Update,
// :199-207; SaveAttributes, :151-160), then applies the next preset,
// alternating between the two (UpdAttrsByPresetIdx, :184-193).
// AttributesValueUpdateClear restores what was saved, if anything, and
// forgets it (Clear, :209-213; RestoreAttributes, :172-182); the preset
// index is not reset. The app ignores the setters' results
// (TEMPORARY_RETURN_IGNORED) and reports the trigger handled; so does
// this. False for a trigger that is not one of them.
func (m *demoMeter) testEventTrigger(trigger uint64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	switch trigger & smokeTriggerMask {
	case triggerMeterAttributesValueUpdate:
		if m.saved == nil {
			st := m.srv.Get()
			m.saved = &st
		}
		if err := m.srv.Set(identification(meterPresets[m.presetsIdx])); err != nil {
			slog.Warn("meter.trigger", slog.String("device", m.name), slog.Any("err", err))
		}
		m.presetsIdx = 1 - m.presetsIdx
	case triggerMeterAttributesValueUpdateClear:
		if m.saved != nil {
			if err := m.srv.Set(identification(*m.saved)); err != nil {
				slog.Warn("meter.trigger", slog.String("device", m.name), slog.Any("err", err))
			}
		}
		m.saved = nil
	default:
		return false
	}
	return true
}
