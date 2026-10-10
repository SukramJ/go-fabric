// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/energy"
	"github.com/SukramJ/go-fabric/cluster/modebase"
	"github.com/SukramJ/go-fabric/cluster/spec"
	mtrid "github.com/SukramJ/go-fabric/cluster/spec/meteridentification"
	"github.com/SukramJ/go-fabric/cluster/thermo"
	"github.com/SukramJ/go-fabric/contract"
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
// One difference: HeaterTypes is fixed ("F") in the module's server, so
// the tank declares both elements from the start; the app declares one and
// adds the second in its installation trigger (WaterHeaterManufacturer.cpp
// :46, :170-171), the one an emergency boost turns on (:101-107).
type demoWaterHeater struct {
	name string

	once    sync.Once
	whm     *energy.WaterHeaterManagementServer
	modes   *modebase.Server
	thermo  *thermo.ThermostatServer
	version struct{ whm, modes cluster.DataVersionTracker }

	mu        sync.Mutex
	target    int16 // mTargetWaterTemperature
	water     int16 // mWaterTemperature
	boosting  bool  // mBoostState == kActive
	boost     energy.BoostInfo
	reached   bool // mBoostTargetTemperatureReached
	boostEnds *time.Timer
}

var (
	_ contract.EndpointSource = (*demoWaterHeater)(nil)
	_ energy.Booster          = (*demoWaterHeater)(nil)
	_ modebase.ModeChanger    = (*demoWaterHeater)(nil)
)

func newDemoWaterHeater(name string) *demoWaterHeater { return &demoWaterHeater{name: name} }

// MatterDeviceType implements [contract.EndpointSource].
func (w *demoWaterHeater) MatterDeviceType() uint16 { return deviceTypeWaterHeater }

// MatterClusterServers implements [contract.EndpointSource].
func (w *demoWaterHeater) MatterClusterServers() []contract.ClusterServer {
	w.build()
	return []contract.ClusterServer{w.whm, w.modes, w.thermo}
}

func (w *demoWaterHeater) build() {
	w.once.Do(func() {
		// EM and TP, as the app serves them (WaterHeaterMain.cpp:105-106).
		srv, err := energy.NewWaterHeaterManagement(energy.WaterHeaterConfig{
			Features:    energy.WaterHeaterFeatureEnergyManagement | energy.WaterHeaterFeatureTankPercent,
			HeaterTypes: energy.HeatSourceImmersionElement1 | energy.HeatSourceImmersionElement2,
			Booster:     w,
			DataVersion: &w.version.whm,
		})
		if err != nil {
			panic(fmt.Sprintf("water heater WaterHeaterManagement: %v", err))
		}
		modes, err := modebase.NewWaterHeaterMode(modebase.Config{
			Changer: w,
			SupportedModes: []modebase.ModeOption{
				{Label: "Off", Mode: waterHeaterOff, Tags: []modebase.ModeTag{{Value: modebase.WaterHeaterTagOff}}},
				{Label: "Manual", Mode: waterHeaterManual, Tags: []modebase.ModeTag{{Value: modebase.WaterHeaterTagManual}}},
				{Label: "Timed", Mode: waterHeaterTimed, Tags: []modebase.ModeTag{{Value: modebase.WaterHeaterTagTimed}}},
			},
			CurrentMode: waterHeaterOff,
			DataVersion: &w.version.modes,
		})
		if err != nil {
			panic(fmt.Sprintf("water heater WaterHeaterMode: %v", err))
		}
		cfg := thermo.ThermostatConfig{
			Features:                thermo.ThermostatFeatureHEAT,
			AbsMinHeatSetpointLimit: 3000,
			AbsMaxHeatSetpointLimit: 8000,
			InitialHeatingSetpoint:  waterTargetTemperature,
		}
		// The tank starts as the app's does: empty readings, nothing
		// reported until a trigger or the host moves the water.
		w.whm, w.modes, w.thermo = srv, modes, thermo.NewThermostatServer(cfg)
	})
}

// ChangeToMode implements [modebase.ModeChanger]: every supported mode is
// taken, as the app's mode delegate takes it (water-heater-mode.cpp:39-42).
func (w *demoWaterHeater) ChangeToMode(_ context.Context, newMode uint8) (modebase.Status, string, error) {
	slog.Info("waterheater.mode", slog.String("device", w.name), slog.Int("mode", int(newMode)))
	return modebase.StatusSuccess, "", nil
}

// Boost implements [energy.Booster]: the app's HandleBoost
// (WaterHeaterDelegateImpl.cpp:158-222) — keep the parameters, restart the
// duration timer, then turn the heat on or off as the boost requires. The
// server sets BoostState and emits BoostStarted when this returns.
func (w *demoWaterHeater) Boost(_ context.Context, info energy.BoostInfo) error {
	w.build()
	w.mu.Lock()
	defer w.mu.Unlock()
	w.boost, w.reached, w.boosting = info, false, true
	w.stopTimerLocked()
	w.boostEnds = time.AfterFunc(time.Duration(info.Duration)*time.Second, w.boostExpired)
	slog.Info("waterheater.boost", slog.String("device", w.name), slog.Int("seconds", int(info.Duration)))
	w.changeHeatingLocked()
	return nil
}

// CancelBoost implements [energy.Booster]: the app's HandleCancelBoost
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
	if err := w.whm.EndBoost(); err != nil {
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
	tank := w.whm.State().TankPercentage
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
	demand := w.whm.State().HeatDemand
	mode, _ := w.modes.MatterRead(modebase.AttrCurrentMode)
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
		next := energy.HeatSourceImmersionElement1
		if w.boost.EmergencyBoost != nil && *w.boost.EmergencyBoost {
			next |= energy.HeatSourceImmersionElement2
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

func (w *demoWaterHeater) setDemandLocked(demand energy.HeatSource) {
	if err := w.whm.SetHeatDemand(demand); err != nil {
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
	if err := w.whm.SetTankPercentage(w.tankPercentageLocked()); err != nil {
		slog.Warn("waterheater.tank", slog.String("device", w.name), slog.Any("err", err))
	}
	water := w.water
	w.thermo.SetLocalTemperature(&water)
}

// setModeLocked is the app's SetWaterHeaterMode (WaterHeaterDelegateImpl.cpp:
// 511-529).
func (w *demoWaterHeater) setModeLocked(mode uint8) {
	if err := w.modes.SetCurrentMode(mode); err != nil {
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
	w.build()
	w.mu.Lock()
	defer w.mu.Unlock()
	switch trigger & smokeTriggerMask {
	case triggerWaterHeaterInstallation:
		// A 100 l tank of water at 20 °C, target 60 °C, mode unchanged.
		if err := w.whm.SetTankVolume(waterTankVolume); err != nil {
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

// deviceTypeElectricalUtilityMeter is ElectricalUtilityMeter
// (parity/schema.json, 0x0511 rev 1): MeterIdentification mandatory,
// Identify mandatory (mounted by the assembler), and the root's
// TimeSyncCond, which the daemon's root TimeSynchronization meets
// (wiring.go).
const deviceTypeElectricalUtilityMeter uint16 = 0x0511

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

	once    sync.Once
	srv     *spec.Server
	version cluster.DataVersionTracker

	mu sync.Mutex
	// saved holds the identification before the first test event; nil
	// when none is pending (the app's mInstance, EventTriggers.cpp:32).
	saved      map[uint32]any
	presetsIdx int
}

var _ contract.EndpointSource = (*demoMeter)(nil)

func newDemoMeter(name string) *demoMeter { return &demoMeter{name: name} }

// MatterDeviceType implements [contract.EndpointSource].
func (m *demoMeter) MatterDeviceType() uint16 { return deviceTypeElectricalUtilityMeter }

// MatterClusterServers implements [contract.EndpointSource].
func (m *demoMeter) MatterClusterServers() []contract.ClusterServer {
	m.build()
	return []contract.ClusterServer{m.srv}
}

func (m *demoMeter) build() {
	m.once.Do(func() {
		serial := "DEMO-0001"
		srv, err := energy.NewMeterIdentification(energy.MeterConfig{
			MeterSerialNumber: &serial,
			DataVersion:       &m.version,
		})
		if err != nil {
			panic(fmt.Sprintf("meter MeterIdentification: %v", err))
		}
		m.srv = srv
	})
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
var meterPresets = [2]map[uint32]any{
	{
		mtrid.AttrMeterType:         energy.MeterTypeUtility,
		mtrid.AttrPointOfDelivery:   "Test delivery point",
		mtrid.AttrMeterSerialNumber: "TST-123456789",
	},
	{
		mtrid.AttrMeterType:         energy.MeterTypePrivate,
		mtrid.AttrPointOfDelivery:   "New delivery point",
		mtrid.AttrMeterSerialNumber: "NEW-987654321",
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
	m.build()
	m.mu.Lock()
	defer m.mu.Unlock()
	switch trigger & smokeTriggerMask {
	case triggerMeterAttributesValueUpdate:
		if m.saved == nil {
			m.saved = make(map[uint32]any, len(meterPresets[0]))
			for id := range meterPresets[0] {
				v, _ := m.srv.MatterRead(id)
				m.saved[id] = v
			}
		}
		if err := m.srv.SetAttributes(meterPresets[m.presetsIdx]); err != nil {
			slog.Warn("meter.trigger", slog.String("device", m.name), slog.Any("err", err))
		}
		m.presetsIdx = 1 - m.presetsIdx
	case triggerMeterAttributesValueUpdateClear:
		if m.saved != nil {
			if err := m.srv.SetAttributes(m.saved); err != nil {
				slog.Warn("meter.trigger", slog.String("device", m.name), slog.Any("err", err))
			}
		}
		m.saved = nil
	default:
		return false
	}
	return true
}
