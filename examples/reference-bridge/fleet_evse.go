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
	dem "github.com/SukramJ/go-fabric/cluster/spec/deviceenergymanagement"
	evse "github.com/SukramJ/go-fabric/cluster/spec/energyevse"
	"github.com/SukramJ/go-fabric/cluster/spec/powersource"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/endpoint"
)

// --- device: an EV charger ---------------------------------------------------
//
// demoEvse is connectedhomeip's EVSE app (examples/evse-app/evse-common at
// the harness pin 6170af84), the device the EEVSE, EEVSEM, DEM and DEMM
// certification cases run against, transcribed onto this module's
// servers: the EnergyEvse state machine is the server's
// (cluster/energy/evse.go); what stays here is the app's configuration,
// its charging-schedule computation (EVSEManufacturerImpl.cpp) and its
// test event triggers (EnergyEvseEventTriggers.cpp; examples/
// energy-management/energy-management-triggers/DEMTestEventTriggers.cpp).
//
// chip's app serves everything on one endpoint (evse-app.zap: endpoint 1
// carries EnergyEvse, EnergyEvseMode, DeviceEnergyManagement(Mode),
// PowerSource, the electrical measurement clusters and PowerTopology).
// EnergyEvse (0x050C) lists PowerSource, DeviceEnergyManagement and
// ElectricalSensor as device types it requires (parity/schema.json), so
// here each is a part of the charger's endpoint.

// Device types (parity/schema.json).
const (
	deviceTypeEnergyEvse             uint16 = 0x050C
	deviceTypeDeviceEnergyManagement uint16 = 0x050D
	deviceTypeElectricalSensor       uint16 = 0x0510
)

// conditionControllableEsa is the DeviceEnergyManagement condition under
// which DeviceEnergyManagementMode is mandatory ("ControllableEsa, O").
const conditionControllableEsa = "ControllableEsa"

// EnergyEvseMode's modes (examples/evse-app/evse-common/include/
// energy-evse-modes.h:31-63).
const (
	evseModeManual                    uint8 = 0
	evseModeTimeOfUse                 uint8 = 1
	evseModeSolarCharging             uint8 = 2
	evseModeTimeOfUseAndSolarCharging uint8 = 3
)

// DeviceEnergyManagementMode's modes (examples/energy-management/
// device-energy-management/include/device-energy-management-modes.h:31-72).
const (
	demModeNoOptimization             uint8 = 0
	demModeDeviceOnlyOptimization     uint8 = 1
	demModeDeviceAndLocalOptimization uint8 = 2
	demModeDeviceAndGridOptimization  uint8 = 3
	demModeAllOptimization            uint8 = 4
)

// The EnergyEvse test event triggers (src/app/clusters/energy-evse-server/
// EnergyEvseTestEventTriggerHandler.h:44-82); the handler clears the
// endpoint bits first (:95), the mask the SmokeCoAlarm triggers use here.
const (
	triggerEvseBasicFunctionality      uint64 = 0x0099_0000_0000_0000
	triggerEvseBasicFunctionalityClear uint64 = 0x0099_0000_0000_0001
	triggerEvsePluggedIn               uint64 = 0x0099_0000_0000_0002
	triggerEvsePluggedInClear          uint64 = 0x0099_0000_0000_0003
	triggerEvseChargeDemand            uint64 = 0x0099_0000_0000_0004
	triggerEvseChargeDemandClear       uint64 = 0x0099_0000_0000_0005
	triggerEvseTimeOfUseMode           uint64 = 0x0099_0000_0000_0006
	triggerEvseGroundFault             uint64 = 0x0099_0000_0000_0010
	triggerEvseOverTemperatureFault    uint64 = 0x0099_0000_0000_0011
	triggerEvseFaultClear              uint64 = 0x0099_0000_0000_0012
	triggerEvseDiagnosticsComplete     uint64 = 0x0099_0000_0000_0020
	triggerEvseTimeOfUseModeClear      uint64 = 0x0099_0000_0000_0021
	triggerEvseSetSoCLow               uint64 = 0x0099_0000_0000_0030
	triggerEvseSetSoCHigh              uint64 = 0x0099_0000_0000_0031
	triggerEvseSetSoCClear             uint64 = 0x0099_0000_0000_0032
	triggerEvseSetVehicleID            uint64 = 0x0099_0000_0000_0040
	triggerEvseTriggerRFID             uint64 = 0x0099_0000_0000_0050
)

// The DeviceEnergyManagement test event triggers (src/app/clusters/
// device-energy-management-server/
// DeviceEnergyManagementTestEventTriggerHandler.h:43-83).
const (
	triggerDemPowerAdjustment                uint64 = 0x0098_0000_0000_0000
	triggerDemPowerAdjustmentClear           uint64 = 0x0098_0000_0000_0001
	triggerDemUserOptOutLocalOptimization    uint64 = 0x0098_0000_0000_0002
	triggerDemUserOptOutGridOptimization     uint64 = 0x0098_0000_0000_0003
	triggerDemUserOptOutClearAll             uint64 = 0x0098_0000_0000_0004
	triggerDemStartTimeAdjustment            uint64 = 0x0098_0000_0000_0005
	triggerDemStartTimeAdjustmentClear       uint64 = 0x0098_0000_0000_0006
	triggerDemPausable                       uint64 = 0x0098_0000_0000_0007
	triggerDemPausableNextSlot               uint64 = 0x0098_0000_0000_0008
	triggerDemPausableClear                  uint64 = 0x0098_0000_0000_0009
	triggerDemForecastAdjustment             uint64 = 0x0098_0000_0000_000A
	triggerDemForecastAdjustmentNextSlot     uint64 = 0x0098_0000_0000_000B
	triggerDemForecastAdjustmentClear        uint64 = 0x0098_0000_0000_000C
	triggerDemConstraintBasedAdjustment      uint64 = 0x0098_0000_0000_000D
	triggerDemConstraintBasedAdjustmentClear uint64 = 0x0098_0000_0000_000E
	triggerDemForecast                       uint64 = 0x0098_0000_0000_000F
	triggerDemForecastClear                  uint64 = 0x0098_0000_0000_0010
	triggerDemPowerRangeAdjustment           uint64 = 0x0098_0000_0000_0011
	triggerDemPowerRangeAdjustmentClear      uint64 = 0x0098_0000_0000_0012
)

// kMaxRequiredEnergy_mWh (EVSEManufacturerImpl.cpp:49): 1000 MWh.
const evseMaxRequiredEnergy int64 = 1000000000000

// evseTriggerSave is EVSETestEventSaveData (EnergyEvseEventTriggers.cpp:
// 29-39).
type evseTriggerSave struct {
	maxHwCharge, maxHwDischarge, circuit, userMax, cable int64
	hwBasic, hwPluggedIn, hwDemand                       energy.EvseState
}

// demoEvse is the charger.
type demoEvse struct {
	name string
	now  func() time.Time

	once     sync.Once
	evse     *energy.EvseServer
	modes    *modebase.Server
	dem      *demoDem
	power    *wiredPowerSource
	meter    *evseMeter
	version  struct{ evse, modes cluster.DataVersionTracker }
	saved    evseTriggerSave
	triggers sync.Mutex
}

var (
	_ contract.EndpointSource = (*demoEvse)(nil)
	_ energy.EvseHost         = (*demoEvse)(nil)
	_ modebase.ModeChanger    = (*demoEvse)(nil)
)

func newDemoEvse(name string) *demoEvse { return &demoEvse{name: name, now: time.Now} }

// MatterDeviceType implements [contract.EndpointSource].
func (e *demoEvse) MatterDeviceType() uint16 { return deviceTypeEnergyEvse }

// MatterClusterServers implements [contract.EndpointSource].
func (e *demoEvse) MatterClusterServers() []contract.ClusterServer {
	e.build()
	return []contract.ClusterServer{e.evse, e.modes}
}

func (e *demoEvse) build() {
	e.once.Do(func() {
		// The app's EnergyEvse: PREF, RFID, SOC, PNC and V2X, the three
		// optional attributes and StartDiagnostics (EnergyEvseMain.cpp:
		// 104-112).
		srv, err := energy.NewEnergyEvse(energy.EvseConfig{
			Features: energy.EvseFeatureChargingPreferences | energy.EvseFeatureRfid | energy.EvseFeatureSoCReporting |
				energy.EvseFeaturePlugAndCharge | energy.EvseFeatureV2X,
			UserMaximumChargeCurrent: true, RandomizationDelayWindow: true, ApproximateEvEfficiency: true,
			StartDiagnostics: true,
			Host:             e,
			Now:              e.now,
			DataVersion:      &e.version.evse,
		})
		if err != nil {
			panic(fmt.Sprintf("evse EnergyEvse: %v", err))
		}
		// The app's four modes; the zap file sets no CurrentMode, the
		// first is taken.
		modes, err := modebase.NewEnergyEvseMode(modebase.Config{
			Changer: e,
			SupportedModes: []modebase.ModeOption{
				{Label: "Manual", Mode: evseModeManual, Tags: []modebase.ModeTag{{Value: modebase.EvseTagManual}}},
				{Label: "Auto-scheduled", Mode: evseModeTimeOfUse, Tags: []modebase.ModeTag{{Value: modebase.EvseTagTimeOfUse}}},
				{Label: "Solar", Mode: evseModeSolarCharging, Tags: []modebase.ModeTag{{Value: modebase.EvseTagSolarCharging}}},
				{Label: "Auto-scheduled with Solar charging", Mode: evseModeTimeOfUseAndSolarCharging, Tags: []modebase.ModeTag{
					{Value: modebase.EvseTagTimeOfUse}, {Value: modebase.EvseTagSolarCharging},
				}},
			},
			CurrentMode: evseModeManual,
			DataVersion: &e.version.modes,
		})
		if err != nil {
			panic(fmt.Sprintf("evse EnergyEvseMode: %v", err))
		}
		e.evse, e.modes = srv, modes
		e.dem = newDemoDem(e.name, e.now)
		e.power = newWiredPowerSource()
		e.meter = &evseMeter{}
	})
}

// parts are the charger's PowerSource, DeviceEnergyManagement (stating
// ControllableEsa, so DeviceEnergyManagementMode is mounted) and
// ElectricalSensor endpoints.
func (e *demoEvse) parts() []endpoint.Spec {
	e.build()
	return []endpoint.Spec{
		{StableKey: endpoint.StringKey("demo:evse:power"), DeviceType: deviceTypePowerSource, Source: e.power},
		{
			StableKey: endpoint.StringKey("demo:evse:dem"), DeviceType: deviceTypeDeviceEnergyManagement, Source: e.dem,
			DeviceConditions: []string{conditionControllableEsa},
		},
		{StableKey: endpoint.StringKey("demo:evse:meter"), DeviceType: deviceTypeElectricalSensor, Measurement: e.meter},
	}
}

// ChangeToMode implements [modebase.ModeChanger]: every supported mode is
// taken (energy-evse-mode.cpp:37-40).
func (e *demoEvse) ChangeToMode(_ context.Context, newMode uint8) (modebase.Status, string, error) {
	slog.Info("evse.mode", slog.String("device", e.name), slog.Int("mode", int(newMode)))
	return modebase.StatusSuccess, "", nil
}

// EvseEnergyMeter implements [energy.EvseHost]: the app's meter readings
// stay 0 (EVSEManufacturerImpl.cpp:505-515; EVSEManufacturerImpl.h:
// 208-209 never move).
func (e *demoEvse) EvseEnergyMeter(bool) int64 { return 0 }

// EvseChanged implements [energy.EvseHost]: the app recomputes the
// charging schedule on a state, current-limit or preferences change
// (EVSEManufacturerImpl.cpp:485-525).
func (e *demoEvse) EvseChanged(change energy.EvseChange) {
	switch change {
	case energy.EvseStateChanged, energy.EvseChargeCurrentChanged, energy.EvseChargingPreferencesChanged:
		e.computeChargingSchedule()
	case energy.EvseDischargeCurrentChanged:
	}
}

// computeChargingSchedule is ComputeChargingSchedule
// (EVSEManufacturerImpl.cpp:335-423): with the EV plugged in and charging
// enabled, the next target — today's after now, else tomorrow's earliest —
// sets NextChargeTargetTime, and the energy it needs NextChargeStartTime.
func (e *demoEvse) computeChargingSchedule() {
	now := e.now()
	local := now.Local()
	dayMap := uint8(1) << uint(local.Weekday())            // GetLocalDayOfWeekNow (EnergyTimeUtils.cpp:51-66)
	minutesNow := uint16(local.Hour()*60 + local.Minute()) //nolint:gosec // < 1440; GetMinutesPastMidnight (:94-113)
	nowEpoch := uint32(now.Unix() - 946684800)             //nolint:gosec // the Matter epoch, a clock after 2000
	var sched energy.ChargeSchedule
	if e.evse.IsPluggedIn() && e.evse.SupplyState() == energy.SupplyChargingEnabled { // :375
		var (
			minutes uint16
			soc     *uint8
			added   *int64
			found   bool
		)
		searchDay := uint32(0)
		for searchDay < 2 { // :378-398
			minutes, soc, added, found = e.findNextTarget(dayMap, minutesNow, searchDay != 0)
			if found {
				break
			}
			searchDay++
			dayMap = (dayMap << 1) & 0x7F
			if dayMap == 0 {
				dayMap = uint8(evse.TargetDayOfWeekSunday)
			}
		}
		if found { // :400-413
			target := ((nowEpoch / 60) + uint32(minutes) + searchDay*1440 - uint32(minutesNow)) * 60
			sched.TargetTime = &target
			sched.StartTime = e.startTime(target, nowEpoch, e.requiredEnergy(&soc, &added))
		}
		sched.TargetSoC, sched.RequiredEnergy = soc, added // :417-420
	}
	if err := e.evse.SetChargeSchedule(sched); err != nil {
		slog.Warn("evse.schedule", slog.String("device", e.name), slog.Any("err", err))
	}
}

// findNextTarget is FindNextTarget (EVSEManufacturerImpl.cpp:129-194):
// the earliest target of the first schedule holding a day of dayMap,
// skipping targets before now unless allowPast.
func (e *demoEvse) findNextTarget(dayMap uint8, minutesNow uint16, allowPast bool) (minutes uint16, soc *uint8, added *int64, found bool) {
	best := uint16(24 * 60)
	for _, sch := range e.evse.Targets() {
		if uint8(sch.DayOfWeekForSequence)&dayMap != 0 {
			for _, t := range sch.ChargingTargets {
				if t.TargetTimeMinutesPastMidnight < minutesNow && !allowPast {
					continue
				}
				if t.TargetTimeMinutesPastMidnight < best {
					found, best = true, t.TargetTimeMinutesPastMidnight
					minutes, soc, added = t.TargetTimeMinutesPastMidnight, t.TargetSoC, t.AddedEnergy
				}
			}
		}
		if found {
			break
		}
	}
	return minutes, soc, added, found
}

// requiredEnergy is DetermineRequiredEnergy (EVSEManufacturerImpl.cpp:
// 203-271), which always succeeds; it clears the target field the
// computation did not use.
func (e *demoEvse) requiredEnergy(soc **uint8, added **int64) int64 {
	if *soc != nil {
		if e.evse.HasFeature("SOC") {
			vehicle, capacity := e.evse.StateOfCharge(), e.evse.BatteryCapacity()
			if vehicle != nil && capacity != nil { // :217-230
				*added = nil
				return max(0, (int64(**soc)-int64(*vehicle))*(*capacity)/100)
			}
		} else { // :234-248
			return evseMaxRequiredEnergy
		}
	}
	if *added == nil { // :253-261
		return evseMaxRequiredEnergy
	}
	*soc = nil // :264-268
	return **added
}

// startTime is ComputeStartTime (EVSEManufacturerImpl.cpp:277-329): the
// charging time at the present power, plus 15 minutes, before the target;
// not before now.
func (e *demoEvse) startTime(target, now uint32, required int64) *uint32 {
	if required == 0 {
		return nil
	}
	powerW := uint32((e.evse.NominalMainsVoltage() * e.evse.MaximumChargeCurrent()) / 1000000) //nolint:gosec // as chip's static_cast
	if powerW == 0 {
		return nil
	}
	duration := uint32((uint64(required) * 36) / (uint64(powerW) * 10)) //nolint:gosec // as chip's static_cast
	duration += 15 * 60
	start := target - duration
	if start < now {
		start = now
	}
	return &start
}

// testEventTrigger handles the EVSE and DEM triggers as the app does
// (EnergyEvseEventTriggers.cpp:195-274; DEMTestEventTriggers.cpp:366-463);
// false for a trigger that is not one of them.
func (e *demoEvse) testEventTrigger(trigger uint64) bool { //nolint:funlen // a flat dispatch table: one case per trigger
	e.build()
	e.triggers.Lock()
	defer e.triggers.Unlock()
	trigger &= smokeTriggerMask
	if trigger>>48 == 0x0098 {
		return e.dem.testEventTrigger(trigger)
	}
	if trigger>>48 != 0x0099 {
		return false
	}
	s := e.evse
	logged := func(err error) {
		if err != nil {
			slog.Warn("evse.trigger", slog.String("device", e.name), slog.Any("err", err))
		}
	}
	switch trigger {
	case triggerEvseBasicFunctionality: // :53-70
		e.saved.maxHwCharge, e.saved.maxHwDischarge = s.MaxHardwareChargeCurrent(), s.MaxHardwareDischargeCurrent()
		e.saved.circuit, e.saved.userMax, e.saved.hwBasic = s.CircuitCapacity(), s.UserMaximumChargeCurrent(), s.HardwareState()
		logged(s.SetMaxHardwareChargeCurrent(32000))
		logged(s.SetMaxHardwareDischargeCurrent(32000))
		logged(s.SetCircuitCapacity(32000))
		logged(s.SetUserMaximumChargeCurrent(32000))
		logged(s.SetHardwareState(energy.EvseNotPluggedIn))
	case triggerEvseBasicFunctionalityClear: // :71-82
		logged(s.SetMaxHardwareChargeCurrent(e.saved.maxHwCharge))
		logged(s.SetMaxHardwareDischargeCurrent(e.saved.maxHwDischarge))
		logged(s.SetCircuitCapacity(e.saved.circuit))
		logged(s.SetUserMaximumChargeCurrent(e.saved.userMax))
		logged(s.SetHardwareState(e.saved.hwBasic))
	case triggerEvsePluggedIn: // :83-92
		e.saved.cable, e.saved.hwPluggedIn = s.CableAssemblyLimit(), s.HardwareState()
		logged(s.SetCableAssemblyLimit(63000))
		logged(s.SetHardwareState(energy.EvsePluggedInNoDemand))
	case triggerEvsePluggedInClear: // :93-98
		logged(s.SetCableAssemblyLimit(e.saved.cable))
		logged(s.SetHardwareState(e.saved.hwPluggedIn))
	case triggerEvseChargeDemand: // :100-106
		e.saved.hwDemand = s.HardwareState()
		logged(s.SetHardwareState(energy.EvsePluggedInDemand))
	case triggerEvseChargeDemandClear: // :107-112
		logged(s.SetHardwareState(e.saved.hwDemand))
	case triggerEvseTimeOfUseMode, triggerEvseTimeOfUseModeClear: // :113-120, a TODO in chip
	case triggerEvseGroundFault: // :121-126
		logged(s.SetFault(evse.FaultStateGroundFault))
	case triggerEvseOverTemperatureFault: // :128-133
		logged(s.SetFault(evse.FaultStateOverTemperature))
	case triggerEvseFaultClear: // :135-140
		logged(s.SetFault(energy.EvseFaultNone))
	case triggerEvseDiagnosticsComplete: // :142-147
		logged(s.DiagnosticsComplete())
	case triggerEvseSetSoCLow, triggerEvseSetSoCHigh: // :149-168: 20 % or 95 % of 70 kWh
		soc, capacity := uint8(20), int64(70000000)
		if trigger == triggerEvseSetSoCHigh {
			soc = 95
		}
		logged(s.SetStateOfCharge(&soc))
		logged(s.SetBatteryCapacity(&capacity))
	case triggerEvseSetSoCClear: // :169-178
		logged(s.SetStateOfCharge(nil))
		logged(s.SetBatteryCapacity(nil))
	case triggerEvseSetVehicleID: // :179-185
		logged(s.SetVehicleID("Test-Vehicle-ID-012345789-ABCDEF"))
	case triggerEvseTriggerRFID: // :186-193
		logged(s.ReportRFID([]byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99}))
	default:
		return false
	}
	return true
}

// --- the charger's DeviceEnergyManagement part ---------------------------------

// demoDem is the charger's DeviceEnergyManagement endpoint: the cluster
// with the app's default features and its mode cluster. The app's
// manufacturer hooks do nothing (EVSEManufacturerImpl.cpp:533-588) and
// report 300 mWh used (:528-531).
type demoDem struct {
	energy.NopDemManager

	name    string
	srv     *energy.DeviceEnergyManagementServer
	modes   *modebase.Server
	version struct{ dem, modes cluster.DataVersionTracker }

	// The trigger file's statics (DEMTestEventTriggers.cpp:30-49).
	slots    [10]energy.ForecastSlot
	forecast energy.Forecast
	savedMin int64
	savedMax int64
	saved    bool
}

var (
	_ contract.EndpointSource = (*demoDem)(nil)
	_ energy.DemManager       = (*demoDem)(nil)
	_ modebase.ModeChanger    = (*demoDem)(nil)
)

func newDemoDem(name string, now func() time.Time) *demoDem {
	d := &demoDem{name: name}
	// PA, PFR, STA, PAU, FA and CON: the app's default feature map
	// (examples/evse-app/linux/main.cpp:60-62). ESAState Online, ESAType
	// EVSE, 1.2 kW to 7.6 kW (EVSEManufacturerImpl.cpp:73-78).
	srv, err := energy.NewDeviceEnergyManagement(energy.DeviceEnergyConfig{
		Features: energy.DemFeaturePowerAdjustment | energy.DemFeaturePowerForecastReporting | energy.DemFeatureStartTimeAdjustment |
			energy.DemFeaturePausable | energy.DemFeatureForecastAdjustment | energy.DemFeatureConstraintBasedAdjustment,
		EsaType: dem.ESATypeEvse, EsaState: energy.EsaStateOnline,
		AbsMinPower: 1200000, AbsMaxPower: 7600000,
		Manager: d, Now: now, DataVersion: &d.version.dem,
	})
	if err != nil {
		panic(fmt.Sprintf("evse DeviceEnergyManagement: %v", err))
	}
	modes, err := modebase.NewDeviceEnergyManagementMode(modebase.Config{
		Changer: d,
		SupportedModes: []modebase.ModeOption{
			{Label: "No energy management (forecast only)", Mode: demModeNoOptimization, Tags: []modebase.ModeTag{{Value: modebase.DemTagNoOptimization}}},
			{Label: "Device optimizes (no local or grid control)", Mode: demModeDeviceOnlyOptimization, Tags: []modebase.ModeTag{{Value: modebase.DemTagDeviceOptimization}}},
			{Label: "Optimized within building", Mode: demModeDeviceAndLocalOptimization, Tags: []modebase.ModeTag{
				{Value: modebase.DemTagLocalOptimization}, {Value: modebase.DemTagDeviceOptimization},
			}},
			{Label: "Optimized for grid", Mode: demModeDeviceAndGridOptimization, Tags: []modebase.ModeTag{
				{Value: modebase.DemTagDeviceOptimization}, {Value: modebase.DemTagGridOptimization},
			}},
			{Label: "Optimized for grid and building", Mode: demModeAllOptimization, Tags: []modebase.ModeTag{
				{Value: modebase.DemTagLocalOptimization}, {Value: modebase.DemTagDeviceOptimization}, {Value: modebase.DemTagGridOptimization},
			}},
		},
		CurrentMode: demModeNoOptimization,
		DataVersion: &d.version.modes,
	})
	if err != nil {
		panic(fmt.Sprintf("evse DeviceEnergyManagementMode: %v", err))
	}
	d.srv, d.modes = srv, modes
	return d
}

// MatterDeviceType implements [contract.EndpointSource].
func (d *demoDem) MatterDeviceType() uint16 { return deviceTypeDeviceEnergyManagement }

// MatterClusterServers implements [contract.EndpointSource].
func (d *demoDem) MatterClusterServers() []contract.ClusterServer {
	return []contract.ClusterServer{d.srv, d.modes}
}

// ApproxEnergyDuringSession implements [energy.DemManager]
// (EVSEManufacturerImpl.cpp:528-531).
func (d *demoDem) ApproxEnergyDuringSession() int64 { return 300 }

// ChangeToMode implements [modebase.ModeChanger]: every supported mode is
// taken (device-energy-management-mode.cpp:46-50).
func (d *demoDem) ChangeToMode(_ context.Context, newMode uint8) (modebase.Status, string, error) {
	slog.Info("evse.dem.mode", slog.String("device", d.name), slog.Int("mode", int(newMode)))
	return modebase.StatusSuccess, "", nil
}

func matterEpochNow() uint32 { return uint32(time.Now().Unix() - 946684800) } //nolint:gosec // a clock after 2000

// configureForecast is ConfigureForecast (DEMTestEventTriggers.cpp:51-135).
// The slots persist between calls as chip's static array does, and slot
// n's MinPauseDuration is twice slot n-1's SlotIsPausable (:114), as
// written there.
func (d *demoDem) configureForecast(numSlots int) {
	now := matterEpochNow()
	earliest, latest := spec.ValueOf(now), now*3
	f := &d.forecast
	f.StartTime, f.EarliestStartTime, f.EndTime, f.LatestEndTime = now+60, &earliest, now*3, &latest
	f.IsPausable, f.ActiveSlotNumber = true, spec.ValueOf(uint16(0))
	pfr, sfr := d.srv.HasFeature("PFR"), d.srv.HasFeature("SFR")
	s0 := &d.slots[0]
	s0.MinDuration, s0.MaxDuration, s0.DefaultDuration, s0.ElapsedSlotTime, s0.RemainingSlotTime = 10, 20, 15, 0, 0
	s0.SlotIsPausable, s0.MinPauseDuration, s0.MaxPauseDuration = ptr(true), ptr(uint32(10)), ptr(uint32(60))
	if pfr {
		s0.NominalPower, s0.MinPower, s0.MaxPower = ptr(int64(2500000)), ptr(int64(1200000)), ptr(int64(7600000))
	}
	s0.NominalEnergy = ptr(int64(2000))
	if sfr {
		s0.ManufacturerEsaState = ptr(uint16(23))
	}
	for n := 1; n < numSlots; n++ {
		p, s := &d.slots[n-1], &d.slots[n]
		s.MinDuration, s.MaxDuration, s.DefaultDuration = 2*p.MinDuration, 2*p.MaxDuration, 2*p.DefaultDuration
		s.ElapsedSlotTime, s.RemainingSlotTime = 2*p.ElapsedSlotTime, 2*p.RemainingSlotTime
		s.SlotIsPausable = ptr(n&1 == 0)
		prevPausable := uint32(0)
		if p.SlotIsPausable != nil && *p.SlotIsPausable {
			prevPausable = 1
		}
		s.MinPauseDuration, s.MaxPauseDuration = ptr(2*prevPausable), ptr(2**p.MaxPauseDuration)
		if pfr {
			s.NominalPower, s.MinPower, s.MaxPower = ptr(*p.NominalPower), ptr(*p.MinPower), ptr(*p.MaxPower)
			s.NominalEnergy = ptr(2 * *p.NominalEnergy)
		}
		if sfr {
			s.ManufacturerEsaState = ptr(*p.ManufacturerEsaState + 1)
		}
	}
	f.Slots = append([]energy.ForecastSlot(nil), d.slots[:numSlots]...)
	d.setForecast(f)
}

func ptr[T any](v T) *T { return &v }

func (d *demoDem) setForecast(f *energy.Forecast) {
	if err := d.srv.SetForecast(f); err != nil {
		slog.Warn("evse.dem.forecast", slog.String("device", d.name), slog.Any("err", err))
	}
}

// current is GetDEMDelegate()->GetForecast().Value(), copied into the
// trigger file's static; a null forecast leaves the static as it was.
func (d *demoDem) current() *energy.Forecast {
	if f := d.srv.Forecast(); f != nil {
		d.forecast = *f
	}
	return &d.forecast
}

// setCapability is SetTestEventTrigger_PowerAdjustment and _ClearForecast
// (DEMTestEventTriggers.cpp:137-177).
func (d *demoDem) setCapability(minPower, maxPower int64, minDuration, maxDuration uint32) {
	c := &energy.PowerAdjustCapability{
		PowerAdjustCapability: spec.ValueOf([]energy.PowerAdjustRange{{MinPower: minPower, MaxPower: maxPower, MinDuration: minDuration, MaxDuration: maxDuration}}),
		Cause:                 dem.PowerAdjustReasonNoAdjustment,
	}
	if err := d.srv.SetPowerAdjustmentCapability(c); err != nil {
		slog.Warn("evse.dem.capability", slog.String("device", d.name), slog.Any("err", err))
	}
}

func (d *demoDem) optOut(state energy.OptOutState) {
	if err := d.srv.SetOptOutState(state); err != nil {
		slog.Warn("evse.dem.optout", slog.String("device", d.name), slog.Any("err", err))
	}
}

// testEventTrigger is HandleDeviceEnergyManagementTestEventTrigger
// (DEMTestEventTriggers.cpp:366-463).
func (d *demoDem) testEventTrigger(trigger uint64) bool { //nolint:funlen // a flat dispatch table: one case per trigger
	switch trigger {
	case triggerDemPowerAdjustment: // :137-155: 5 kW to 30 kW, 10 s to 60 s
		d.setCapability(5000*1000, 30000*1000, 10, 60)
	case triggerDemPowerAdjustmentClear, triggerDemPausableClear, triggerDemForecastAdjustmentClear,
		triggerDemConstraintBasedAdjustmentClear: // :157-177
		d.setCapability(0, 0, 0, 0)
	case triggerDemUserOptOutLocalOptimization: // :382-385
		d.optOut(energy.OptOutLocal)
	case triggerDemUserOptOutGridOptimization: // :386-389
		d.optOut(energy.OptOutGrid)
	case triggerDemUserOptOutClearAll: // :390-393
		d.optOut(energy.OptOutNone)
	case triggerDemStartTimeAdjustment: // :179-208
		d.configureForecast(2)
		f := d.current()
		now := matterEpochNow()
		earliest, latest := spec.ValueOf(now-60), now*3+60
		f.StartTime, f.EarliestStartTime, f.EndTime, f.LatestEndTime = now, &earliest, now*3, &latest
		d.setForecast(f)
	case triggerDemStartTimeAdjustmentClear: // :210-222
		f := d.current()
		f.StartTime, f.EndTime, f.EarliestStartTime, f.LatestEndTime = 0, 0, nil, nil
		d.setForecast(f)
	case triggerDemPausable, triggerDemForecast: // :229-232, :243-246
		d.configureForecast(2)
	case triggerDemPausableNextSlot: // :234-241
		f := d.current()
		f.ActiveSlotNumber = spec.ValueOf(uint16(1))
		d.setForecast(f)
	case triggerDemForecastClear: // :248-259: the static, not the current forecast
		f := &d.forecast
		f.StartTime, f.EndTime, f.EarliestStartTime, f.LatestEndTime = 0, 0, nil, nil
		f.IsPausable, f.ActiveSlotNumber, f.Slots = false, spec.NullOf[uint16](), nil
		d.setForecast(f)
	case triggerDemForecastAdjustment: // :261-275
		d.configureForecast(2)
		f := d.current()
		s0 := &d.slots[0]
		s0.MinPowerAdjustment, s0.MaxPowerAdjustment = ptr(int64(20)), ptr(int64(2000))
		s0.MinDurationAdjustment, s0.MaxDurationAdjustment = ptr(uint32(120)), ptr(uint32(240))
		f.Slots = append([]energy.ForecastSlot(nil), d.slots[:2]...)
		d.setForecast(f)
	case triggerDemForecastAdjustmentNextSlot: // :277-283
		f := d.current()
		if !f.ActiveSlotNumber.Null {
			f.ActiveSlotNumber = spec.ValueOf(f.ActiveSlotNumber.Value + 1)
		}
		d.setForecast(f)
	case triggerDemConstraintBasedAdjustment: // :285-288
		d.configureForecast(4)
	case triggerDemPowerRangeAdjustment: // :290-329: AbsMinPower 1 kW, AbsMaxPower 7.6 kW
		if !d.saved {
			d.savedMin, d.savedMax, d.saved = d.srv.AbsMinPower(), d.srv.AbsMaxPower(), true
		}
		_ = d.srv.SetAbsMinPower(1000000)
		_ = d.srv.SetAbsMaxPower(7600000)
	case triggerDemPowerRangeAdjustmentClear: // :331-364
		if d.saved {
			_ = d.srv.SetAbsMinPower(d.savedMin)
			_ = d.srv.SetAbsMaxPower(d.savedMax)
			d.saved = false
		}
	default:
		return false
	}
	return true
}

// --- the charger's PowerSource and ElectricalSensor parts ---------------------

// wiredPowerSource is the charger's mains supply, as the app sets it
// (EVSEManufacturerImpl.cpp:444-468): WIRED, Active, "Primary Mains
// Power", AC at 230 V and 32 A. Order is the zap file's empty default, 0.
// EndpointList names the endpoint the server is mounted on, as this
// module's battery PowerSource does (cluster/measurement PowerSourceServer).
type wiredPowerSource struct {
	*spec.Server
	version cluster.DataVersionTracker
}

var _ contract.EndpointSource = (*wiredPowerSource)(nil)

func newWiredPowerSource() *wiredPowerSource {
	p := &wiredPowerSource{}
	srv, err := spec.NewServer(powersource.Definition, spec.Options{
		Features:   uint32(powersource.FeatureWired),
		Attributes: []uint32{powersource.AttrWiredNominalVoltage, powersource.AttrWiredMaximumCurrent},
	}, spec.ServerConfig{DataVersion: &p.version, Initial: map[uint32]any{
		powersource.AttrStatus:              powersource.PowerSourceStatusActive,
		powersource.AttrOrder:               uint8(0),
		powersource.AttrDescription:         "Primary Mains Power",
		powersource.AttrWiredCurrentType:    powersource.WiredCurrentTypeAc,
		powersource.AttrWiredNominalVoltage: uint32(230000),
		powersource.AttrWiredMaximumCurrent: uint32(32000),
		powersource.AttrEndpointList:        []uint16{},
	}})
	if err != nil {
		panic(fmt.Sprintf("evse PowerSource: %v", err))
	}
	p.Server = srv
	return p
}

// MatterDeviceType implements [contract.EndpointSource].
func (p *wiredPowerSource) MatterDeviceType() uint16 { return deviceTypePowerSource }

// MatterClusterServers implements [contract.EndpointSource].
func (p *wiredPowerSource) MatterClusterServers() []contract.ClusterServer {
	return []contract.ClusterServer{p}
}

// SetEndpoint stamps EndpointList; the bridge calls it at reassembly.
func (p *wiredPowerSource) SetEndpoint(ep uint16) {
	if err := p.Set(powersource.AttrEndpointList, []uint16{ep}); err != nil {
		slog.Warn("evse.powersource", slog.Any("err", err))
	}
}

// evseMeter is the charger's ElectricalSensor: the app's readings come
// from its energy-reporting triggers (FakeReadings), which this daemon
// does not answer, so every reading is unobserved — null — and the energy
// counter is declared, which mounts ElectricalEnergyMeasurement as
// EnergyEvse requires.
type evseMeter struct{}

var _ contract.ElectricalReadings = evseMeter{}

func (evseMeter) MatterMeasurementClass() contract.MeasurementClass {
	return contract.MeasurementElectrical
}
func (evseMeter) MatterFloatValue() (float64, bool) { return 0, false }
func (evseMeter) ActivePower() (float64, bool)      { return 0, false }
func (evseMeter) Voltage() (float64, bool)          { return 0, false }
func (evseMeter) Current() (float64, bool)          { return 0, false }
func (evseMeter) Frequency() (float64, bool)        { return 0, false }
func (evseMeter) Energy() (float64, bool)           { return 0, false }
func (evseMeter) HasEnergy() bool                   { return true }
