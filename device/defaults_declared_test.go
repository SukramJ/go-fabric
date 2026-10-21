// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"context"

	"github.com/SukramJ/go-fabric/cluster/alarm"
	"github.com/SukramJ/go-fabric/cluster/alarmbase"
	"github.com/SukramJ/go-fabric/cluster/appliance"
	"github.com/SukramJ/go-fabric/cluster/closure"
	"github.com/SukramJ/go-fabric/cluster/energy"
	"github.com/SukramJ/go-fabric/cluster/measurement"
	"github.com/SukramJ/go-fabric/cluster/modebase"
	"github.com/SukramJ/go-fabric/cluster/modeselect"
	"github.com/SukramJ/go-fabric/cluster/pump"
	"github.com/SukramJ/go-fabric/cluster/servicearea"
	"github.com/SukramJ/go-fabric/cluster/spec"
	bsdef "github.com/SukramJ/go-fabric/cluster/spec/booleanstate"
	ccldef "github.com/SukramJ/go-fabric/cluster/spec/closurecontrol"
	cddef "github.com/SukramJ/go-fabric/cluster/spec/closuredimension"
	ctdef "github.com/SukramJ/go-fabric/cluster/spec/commoditytariff"
	demdef "github.com/SukramJ/go-fabric/cluster/spec/deviceenergymanagement"
	epdef "github.com/SukramJ/go-fabric/cluster/spec/energypreference"
	ldcdef "github.com/SukramJ/go-fabric/cluster/spec/laundrydryercontrols"
	mwocdef "github.com/SukramJ/go-fabric/cluster/spec/microwaveovencontrol"
	mwomdef "github.com/SukramJ/go-fabric/cluster/spec/microwaveovenmode"
	msdef "github.com/SukramJ/go-fabric/cluster/spec/modeselect"
	pumpdef "github.com/SukramJ/go-fabric/cluster/spec/pumpconfigurationandcontrol"
	sadef "github.com/SukramJ/go-fabric/cluster/spec/servicearea"
	smokedef "github.com/SukramJ/go-fabric/cluster/spec/smokecoalarm"
	tadef "github.com/SukramJ/go-fabric/cluster/spec/temperaturealarm"
	tcdef "github.com/SukramJ/go-fabric/cluster/spec/temperaturecontrol"
	valvedef "github.com/SukramJ/go-fabric/cluster/spec/valveconfigurationandcontrol"
	whmgdef "github.com/SukramJ/go-fabric/cluster/spec/waterheatermanagement"
	"github.com/SukramJ/go-fabric/cluster/thermo"
	"github.com/SukramJ/go-fabric/cluster/valve"
)

// The fake host ports of declaredCases, in the generated types.
type (
	fakePump   struct{}
	fakeValve  struct{}
	fakeModes2 struct{}
	fakeArea   struct{}
	fakeMover  struct{}
	fakePanel  struct{}
	fakeTemp   struct{}
	fakeOven   struct{}
	fakeBoost  struct{}
	fakeSmoke  struct{}
)

func (fakePump) PumpState() pumpdef.State                                          { return pumpdef.State{} }
func (fakePump) SetOperationMode(context.Context, pumpdef.OperationModeEnum) error { return nil }

func (fakeValve) CurrentState() (valvedef.ValveStateEnum, bool)         { return 0, true }
func (fakeValve) TargetState() (valvedef.ValveStateEnum, bool)          { return 0, true }
func (fakeValve) OpenDuration() (uint32, bool)                          { return 0, false }
func (fakeValve) RemainingDuration() (uint32, bool)                     { return 0, false }
func (fakeValve) DefaultOpenDuration() (uint32, bool)                   { return 0, false }
func (fakeValve) SetDefaultOpenDuration(context.Context, *uint32) error { return nil }
func (fakeValve) Open(context.Context, valvedef.OpenRequest) error      { return nil }
func (fakeValve) Close(context.Context) error                           { return nil }

func (fakeModes2) ModeDescription() string                   { return "Speed" }
func (fakeModes2) ModeNamespace() (uint8, bool)              { return 0, false }
func (fakeModes2) CurrentMode() uint8                        { return 0 }
func (fakeModes2) ChangeToMode(context.Context, uint8) error { return nil }
func (fakeModes2) SupportedModes() []msdef.ModeOptionStruct {
	return []msdef.ModeOptionStruct{{Label: "Low", Mode: 0}, {Label: "High", Mode: 1}}
}

func (fakeArea) SelectAreas(context.Context, []uint32) (sadef.SelectAreasStatus, string, error) {
	return 0, "", nil
}

func (fakeArea) SkipArea(context.Context, uint32) (sadef.SkipAreaStatus, string, error) {
	return 0, "", nil
}

func (fakeMover) MoveTo(context.Context, ccldef.TargetPositionEnum) error { return nil }
func (fakeMover) Stop(context.Context) error                              { return nil }

func (fakePanel) SetTarget(context.Context, *uint16, *bool, *cddef.ThreeLevelAutoEnum) error {
	return nil
}

func (fakePanel) Step(context.Context, cddef.StepDirectionEnum, uint16, *cddef.ThreeLevelAutoEnum) error {
	return nil
}

func (fakeTemp) SetTemperature(context.Context, *int16, *uint8) error { return nil }

func (fakeOven) OperationalState() uint8   { return 0 }
func (fakeOven) NormalMode() (uint8, bool) { return 0, true }

func (fakeOven) SupportedMode(uint8) bool { return true }

func (fakeOven) StartSupported() bool { return false }

func (fakeOven) SetCookingParameters(context.Context, uint8, uint32, bool, *uint8, *uint8) error {
	return nil
}

func (fakeOven) ModifyCookTime(context.Context, uint32) error { return nil }

func (fakeBoost) Boost(context.Context, whmgdef.WaterHeaterBoostInfoStruct) error { return nil }
func (fakeBoost) CancelBoost(context.Context) error                               { return nil }

func (fakeSmoke) SmokeCOState() smokedef.State { return smokedef.State{} }

// declaredCases are TestSelfHeldDefaults' cases of the servers brief BU-b
// put on the declaration: the declaration with its port (or, for a server
// that needs none, its Initial) mounts the module's server.
func declaredCases() []struct {
	cluster uint32
	build   func() (*Device, error)
	want    any
} {
	maxCook := uint32(3600)
	levels := []ldcdef.DrynessLevelEnum{ldcdef.DrynessLevelLow, ldcdef.DrynessLevelExtra}
	normal := []mwomdef.ModeOptionStruct{
		{Label: "Normal", Mode: 0, ModeTags: []mwomdef.ModeTagStruct{{Value: mwomdef.ModeTagNormal}}},
		{Label: "Defrost", Mode: 1, ModeTags: []mwomdef.ModeTagStruct{{Value: mwomdef.ModeTagDefrost}}},
	}
	power := uint8(100)
	critical := int16(1000)
	balances := []epdef.BalanceStruct{{Step: 0}, {Step: 100}}
	priorities := []epdef.EnergyPriorityEnum{epdef.EnergyPriorityComfort, epdef.EnergyPriorityEfficiency}
	minT, maxT, setT := int16(0), int16(10000), int16(5000)
	heater := whmgdef.WaterHeaterHeatSourceImmersionElement1
	return []struct {
		cluster uint32
		build   func() (*Device, error)
		want    any
	}{
		{pumpdef.ClusterID, func() (*Device, error) {
			return New(&PumpType, testOptions("pump"), pumpdef.Declare{Features: pumpdef.FeatureConstantPressure, StateSource: fakePump{}})
		}, (*pump.Server)(nil)},
		{valvedef.ClusterID, func() (*Device, error) {
			return New(&WaterValveType, testOptions("valve"), valvedef.Declare{StateSource: fakeValve{}})
		}, (*valve.Server)(nil)},
		{msdef.ClusterID, func() (*Device, error) {
			return New(&ModeSelectType, testOptions("ms"), msdef.Declare{ModeSource: fakeModes2{}})
		}, (*modeselect.Server)(nil)},
		{sadef.ClusterID, func() (*Device, error) {
			return New(&RoboticVacuumCleanerType, testOptions("sa"), sadef.Declare{Selector: fakeArea{}})
		}, (*servicearea.Server)(nil)},
		{ccldef.ClusterID, func() (*Device, error) {
			return New(&ClosureType, testOptions("cc"), ccldef.Declare{
				Features: ccldef.FeaturePositioning | ccldef.FeatureVentilation, Mover: fakeMover{},
			})
		}, (*closure.ControlServer)(nil)},
		{cddef.ClusterID, func() (*Device, error) {
			res, step := uint16(100), uint16(1000)
			return New(&ClosurePanelType, testOptions("cd"), cddef.Declare{
				Features: cddef.FeaturePositioning | cddef.FeatureTranslation, DimensionHandler: fakePanel{},
				Initial: cddef.Patch{Resolution: &res, StepValue: &step},
			})
		}, (*closure.DimensionServer)(nil)},
		{tcdef.ClusterID, func() (*Device, error) {
			return New(&TemperatureControlledCabinetType, testOptions("tc"), tcdef.Declare{
				Features: tcdef.FeatureTemperatureNumber, TemperatureSetter: fakeTemp{},
				Initial: tcdef.Patch{MinTemperature: &minT, MaxTemperature: &maxT, TemperatureSetpoint: &setT},
			})
		}, (*thermo.TemperatureControlServer)(nil)},
		{mwocdef.ClusterID, func() (*Device, error) {
			return New(&MicrowaveOvenType, testOptions("mwoc"), mwocdef.Declare{
				Features: mwocdef.FeaturePowerAsNumber, Oven: fakeOven{}, Initial: mwocdef.Patch{MaxCookTime: &maxCook, PowerSetting: &power},
			})
		}, (*appliance.MicrowaveServer)(nil)},
		{mwomdef.ClusterID, func() (*Device, error) {
			return New(&MicrowaveOvenType, testOptions("mwom"), mwocdef.Declare{Features: mwocdef.FeaturePowerAsNumber},
				mwomdef.Declare{Initial: mwomdef.Patch{SupportedModes: &normal}})
		}, (*modebase.Server)(nil)},
		{whmgdef.ClusterID, func() (*Device, error) {
			return New(&WaterHeaterType, testOptions("whmg"), withFeatures(0x0201, 1),
				whmgdef.Declare{Booster: fakeBoost{}, Initial: whmgdef.Patch{HeaterTypes: &heater}})
		}, (*energy.WaterHeaterManagementServer)(nil)},
		{demdef.ClusterID, func() (*Device, error) {
			return New(&DeviceEnergyManagementType, testOptions("dem"),
				demdef.Declare{Features: demdef.FeaturePowerForecastReporting, DemManager: energy.NopDemManager{}})
		}, (*energy.DeviceEnergyManagementServer)(nil)},
		{ldcdef.ClusterID, func() (*Device, error) {
			return New(&LaundryDryerType, testOptions("ldc"), ldcdef.Declare{Initial: ldcdef.Patch{SupportedDrynessLevels: &levels}})
		}, (*appliance.DryerServer)(nil)},
		{smokedef.ClusterID, func() (*Device, error) {
			return New(&SmokeCoAlarmType, testOptions("smoke"), smokedef.Declare{Features: smokedef.FeatureSmokeAlarm, StateSource: fakeSmoke{}})
		}, (*alarm.Server)(nil)},
		{tadef.ClusterID, func() (*Device, error) {
			return New(&TemperatureControlledCabinetType, testOptions("ta"), tadef.Declare{
				Features: tadef.FeatureOverTemperature, Initial: tadef.Patch{CriticalOverTemperatureThreshold: &critical},
			})
		}, (*alarmbase.Server)(nil)},
		{ctdef.ClusterID, func() (*Device, error) {
			return New(&ElectricalEnergyTariffType, testOptions("ct"), ctdef.Declare{Features: ctdef.FeaturePricing})
		}, (*energy.CommodityTariffServer)(nil)},
		{bsdef.ClusterID, func() (*Device, error) {
			return New(&ContactSensorType, testOptions("bs"), bsdef.Declare{Features: bsdef.FeatureChangeEvent})
		}, (*measurement.HeldBooleanState)(nil)},
		{epdef.ClusterID, func() (*Device, error) {
			zero := uint8(0)
			return New(&ThermostatType, testOptions("ep"), withFeatures(0x0201, thermo.ThermostatFeatureHEAT), epdef.Declare{
				Features: epdef.FeatureEnergyBalance,
				Initial:  epdef.Patch{EnergyBalances: &balances, EnergyPriorities: &priorities, CurrentEnergyBalance: &zero},
			})
		}, (*energy.PreferenceServer)(nil)},
	}
}

var _ = spec.ErrNotHeld
