// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"errors"
	"slices"

	"github.com/SukramJ/go-fabric/cluster/alarmbase"
	"github.com/SukramJ/go-fabric/cluster/appliance"
	"github.com/SukramJ/go-fabric/cluster/boolcfg"
	"github.com/SukramJ/go-fabric/cluster/energy"
	"github.com/SukramJ/go-fabric/cluster/filter"
	bscdef "github.com/SukramJ/go-fabric/cluster/spec/booleanstateconfiguration"
	cpdef "github.com/SukramJ/go-fabric/cluster/spec/commodityprice"
	dwadef "github.com/SukramJ/go-fabric/cluster/spec/dishwasheralarm"
	evsedef "github.com/SukramJ/go-fabric/cluster/spec/energyevse"
	hepadef "github.com/SukramJ/go-fabric/cluster/spec/hepafiltermonitoring"
	tuidef "github.com/SukramJ/go-fabric/cluster/spec/thermostatuserinterfaceconfiguration"
	"github.com/SukramJ/go-fabric/cluster/thermo"
	"github.com/SukramJ/go-fabric/contract"
)

// The appliance, sensor-configuration and energy servers of the
// self-held defaults ([selfHeldDefaults]): each runs on its own state, at
// the configuration the device type's selection names and empty host data
// (no alarm supported, no spin speed, no price) — the state matter.js's
// default servers start with — until the host sets it.

// alarmServer builds an AlarmBase derivation; ModifyEnabledAlarms is
// accepted where the selection mandates it.
func alarmServer(build func(alarmbase.Config) (*alarmbase.Server, error)) serverFactory {
	return func(b defaultBuild) (contract.ClusterServer, error) {
		srv, err := build(alarmbase.Config{
			Features:            b.opts.Features,
			ModifyEnabledAlarms: slices.Contains(b.opts.Commands, dwadef.CmdModifyEnabledAlarms),
			DataVersion:         b.cfg.DataVersion,
		})
		return srv, err
	}
}

// newBooleanStateConfigurationServer is the module's
// BooleanStateConfiguration server without a delegate ([boolcfg.New]),
// with the optional attributes the selection mandates.
func newBooleanStateConfigurationServer(b defaultBuild) (contract.ClusterServer, error) {
	var optional boolcfg.Optional
	for id, o := range map[uint32]boolcfg.Optional{
		bscdef.AttrDefaultSensitivityLevel: boolcfg.OptionalDefaultSensitivityLevel,
		bscdef.AttrAlarmsEnabled:           boolcfg.OptionalAlarmsEnabled,
		bscdef.AttrSensorFault:             boolcfg.OptionalSensorFault,
	} {
		if slices.Contains(b.opts.Attributes, id) {
			optional |= o
		}
	}
	srv, err := boolcfg.New(boolcfg.Config{Features: boolcfg.Feature(b.opts.Features), Optional: optional, DataVersion: b.cfg.DataVersion})
	if errors.Is(err, boolcfg.ErrInvalidConfig) {
		return nil, notServed(err)
	}
	return srv, err
}

// filterServer builds a resource-monitoring server with the optional
// attributes the selection mandates. ResetCondition needs the host's
// Resetter: a selection that mandates it keeps the generated server.
func filterServer(build func(filter.Config) (*filter.Server, error)) serverFactory {
	return func(b defaultBuild) (contract.ClusterServer, error) {
		if slices.Contains(b.opts.Commands, hepadef.CmdResetCondition) {
			return nil, notServed(filter.ErrNoResetter)
		}
		var optional filter.Optional
		if slices.Contains(b.opts.Attributes, hepadef.AttrInPlaceIndicator) {
			optional |= filter.OptionalInPlaceIndicator
		}
		if slices.Contains(b.opts.Attributes, hepadef.AttrLastChangedTime) {
			optional |= filter.OptionalLastChangedTime
		}
		return build(filter.Config{Features: filter.Feature(b.opts.Features), Optional: optional, DataVersion: b.cfg.DataVersion})
	}
}

// newLaundryWasherControlsServer is the module's LaundryWasherControls
// server without spin speeds. The rinses a RINSE selection serves are host
// data (an empty list leaves no valid NumberOfRinses): that selection
// keeps the generated server.
func newLaundryWasherControlsServer(b defaultBuild) (contract.ClusterServer, error) {
	srv, err := appliance.NewLaundryWasherControls(appliance.WasherConfig{Features: b.opts.Features, DataVersion: b.cfg.DataVersion})
	if errors.Is(err, appliance.ErrLaundryValue) {
		return nil, notServed(err)
	}
	return srv, err
}

// newThermostatUIServer is the module's
// ThermostatUserInterfaceConfiguration server ([thermo.NewThermostatUserInterfaceConfiguration]):
// Celsius, no lockout, ScheduleProgrammingVisibility where the selection
// mandates it.
func newThermostatUIServer(b defaultBuild) (contract.ClusterServer, error) {
	cfg := thermo.ThermostatUIConfig{DataVersion: b.cfg.DataVersion}
	if slices.Contains(b.opts.Attributes, tuidef.AttrScheduleProgrammingVisibility) {
		v := tuidef.ScheduleProgrammingVisibilityScheduleProgrammingPermitted
		cfg.ScheduleProgrammingVisibility = &v
	}
	return thermo.NewThermostatUserInterfaceConfiguration(cfg)
}

// newCommodityPriceServer is the module's CommodityPrice server
// ([energy.NewCommodityPrice]), without a price until the host sets one.
func newCommodityPriceServer(b defaultBuild) (contract.ClusterServer, error) {
	return energy.NewCommodityPrice(energy.CommodityPriceConfig{
		Features:         energy.CommodityPriceFeature(b.opts.Features),
		DetailedPrice:    slices.Contains(b.opts.Commands, cpdef.CmdGetDetailedPriceRequest),
		DetailedForecast: slices.Contains(b.opts.Commands, cpdef.CmdGetDetailedForecastRequest),
		DataVersion:      b.cfg.DataVersion,
	})
}

// newGridConditionsServer is the module's ElectricalGridConditions server
// ([energy.NewElectricalGridConditions]).
func newGridConditionsServer(b defaultBuild) (contract.ClusterServer, error) {
	return energy.NewElectricalGridConditions(energy.GridConditionsConfig{
		Features: energy.GridConditionsFeature(b.opts.Features), DataVersion: b.cfg.DataVersion,
	})
}

// newEnergyEvseServer is the module's EnergyEvse state machine without a
// host ([energy.NewEnergyEvse]), on the device's clock, with the optional
// attributes and commands the selection mandates.
func newEnergyEvseServer(b defaultBuild) (contract.ClusterServer, error) {
	return energy.NewEnergyEvse(energy.EvseConfig{
		Features:                 energy.EvseFeature(b.opts.Features),
		UserMaximumChargeCurrent: slices.Contains(b.opts.Attributes, evsedef.AttrUserMaximumChargeCurrent),
		RandomizationDelayWindow: slices.Contains(b.opts.Attributes, evsedef.AttrRandomizationDelayWindow),
		ApproximateEvEfficiency:  slices.Contains(b.opts.Attributes, evsedef.AttrApproximateEvEfficiency),
		StartDiagnostics:         slices.Contains(b.opts.Commands, evsedef.CmdStartDiagnostics),
		Clock:                    b.clock,
		DataVersion:              b.cfg.DataVersion,
	})
}

// newCommodityMeteringServer is the module's CommodityMetering server
// ([energy.NewCommodityMetering]), every attribute null until the host
// sets it.
func newCommodityMeteringServer(b defaultBuild) (contract.ClusterServer, error) {
	return energy.NewCommodityMetering(energy.CommodityMeteringConfig{DataVersion: b.cfg.DataVersion})
}
