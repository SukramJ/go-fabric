// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"errors"
	"reflect"
	"slices"

	"github.com/SukramJ/go-fabric/cluster/alarmbase"
	"github.com/SukramJ/go-fabric/cluster/appliance"
	"github.com/SukramJ/go-fabric/cluster/boolcfg"
	"github.com/SukramJ/go-fabric/cluster/energy"
	"github.com/SukramJ/go-fabric/cluster/filter"
	"github.com/SukramJ/go-fabric/cluster/spec"
	acfdef "github.com/SukramJ/go-fabric/cluster/spec/activatedcarbonfiltermonitoring"
	bscdef "github.com/SukramJ/go-fabric/cluster/spec/booleanstateconfiguration"
	cpdef "github.com/SukramJ/go-fabric/cluster/spec/commodityprice"
	dwadef "github.com/SukramJ/go-fabric/cluster/spec/dishwasheralarm"
	evsedef "github.com/SukramJ/go-fabric/cluster/spec/energyevse"
	hepadef "github.com/SukramJ/go-fabric/cluster/spec/hepafiltermonitoring"
	rfadef "github.com/SukramJ/go-fabric/cluster/spec/refrigeratoralarm"
	tadef "github.com/SukramJ/go-fabric/cluster/spec/temperaturealarm"
	tuidef "github.com/SukramJ/go-fabric/cluster/spec/thermostatuserinterfaceconfiguration"
	"github.com/SukramJ/go-fabric/cluster/thermo"
	"github.com/SukramJ/go-fabric/contract"
)

// The appliance, sensor-configuration and energy servers of the
// self-held defaults ([selfHeldDefaults]): each runs on its own state, at
// the configuration the device type's selection names and empty host data
// (no alarm supported, no spin speed, no price) — the state matter.js's
// default servers start with — until the host sets it.

// alarmServer builds an AlarmBase derivation on the declared Delegate:
// Supported, Latch, Mask, State and a TemperatureAlarm's thresholds from
// the declaration's Initial, ModifyEnabledAlarms accepted where the
// selection mandates it. A TemperatureAlarm without the thresholds its
// features serve keeps the generated server.
func alarmServer(build func(alarmbase.Config) (*alarmbase.Server, error)) serverFactory {
	return func(b defaultBuild) (contract.ClusterServer, error) {
		cfg := alarmbase.Config{
			Features:            b.opts.Features,
			ModifyEnabledAlarms: slices.Contains(b.opts.Commands, dwadef.CmdModifyEnabledAlarms),
			Delegate:            alarmDelegateOf(b.ports),
			DataVersion:         b.cfg.DataVersion,
		}
		configFrom(&cfg, b.def, b.initial)
		for _, id := range []uint32{
			tadef.AttrCriticalOverTemperatureThreshold, tadef.AttrMajorOverTemperatureThreshold, tadef.AttrMinorOverTemperatureThreshold,
			tadef.AttrMinorUnderTemperatureThreshold, tadef.AttrMajorUnderTemperatureThreshold, tadef.AttrCriticalUnderTemperatureThreshold,
		} {
			if b.def.ID != tadef.ClusterID {
				break
			}
			if v, ok := b.initial[id].(int16); ok {
				if cfg.Thresholds == nil {
					cfg.Thresholds = map[uint32]int16{}
				}
				cfg.Thresholds[id] = v
				delete(b.initial, id)
			}
		}
		srv, err := build(cfg)
		if errors.Is(err, alarmbase.ErrThresholds) || errors.Is(err, alarmbase.ErrAdjustableThresholds) {
			return nil, notServed(err)
		}
		return srv, err
	}
}

// alarmDelegateOf is the Delegate an AlarmBase derivation's declaration
// carries, nil without one.
func alarmDelegateOf(ports any) alarmbase.Delegate {
	switch d := ports.(type) {
	case dwadef.Declare:
		if d.Delegate != nil {
			return d.Delegate
		}
	case rfadef.Declare:
		if d.Delegate != nil {
			return d.Delegate
		}
	case tadef.Declare:
		if d.Delegate != nil {
			return d.Delegate
		}
	}
	return nil
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
	cfg := boolcfg.Config{Features: boolcfg.Feature(b.opts.Features), Optional: optional, DataVersion: b.cfg.DataVersion}
	if d, ok := b.ports.(bscdef.Declare); ok && d.Delegate != nil {
		cfg.Delegate = d.Delegate
	}
	// The sensitivity levels and the alarms the sensor supports are its
	// configuration.
	configFrom(&cfg, b.def, b.initial)
	srv, err := boolcfg.New(cfg)
	if errors.Is(err, boolcfg.ErrInvalidConfig) {
		return nil, notServed(err)
	}
	return srv, err
}

// filterServer builds a resource-monitoring server with the optional
// attributes the selection mandates, DegradationDirection and
// ReplacementProducts from the declaration's Initial, and the declared
// Resetter and LastChangedTimeWriter. ResetCondition needs the Resetter:
// a selection that mandates it without one keeps the generated server.
func filterServer(build func(filter.Config) (*filter.Server, error)) serverFactory {
	return func(b defaultBuild) (contract.ClusterServer, error) {
		resetter, writer := filterPortsOf(b.ports)
		reset := slices.Contains(b.opts.Commands, hepadef.CmdResetCondition)
		if reset && resetter == nil {
			return nil, notServed(filter.ErrNoResetter)
		}
		var optional filter.Optional
		if reset {
			optional |= filter.OptionalResetCondition
		}
		if slices.Contains(b.opts.Attributes, hepadef.AttrInPlaceIndicator) {
			optional |= filter.OptionalInPlaceIndicator
		}
		if slices.Contains(b.opts.Attributes, hepadef.AttrLastChangedTime) {
			optional |= filter.OptionalLastChangedTime
		}
		cfg := filter.Config{
			Features: filter.Feature(b.opts.Features), Optional: optional, Resetter: resetter, LastChangedTimeWriter: writer,
			DataVersion: b.cfg.DataVersion,
		}
		configFrom(&cfg, b.def, b.initial)
		// ReplacementProductList is Config.ReplacementProducts.
		if v, ok := b.initial[hepadef.AttrReplacementProductList]; ok {
			if c, ok := spec.Convert(v, reflect.TypeFor[[]filter.ReplacementProduct]()); ok && c.IsValid() {
				cfg.ReplacementProducts, _ = c.Interface().([]filter.ReplacementProduct)
				delete(b.initial, hepadef.AttrReplacementProductList)
			}
		}
		return build(cfg)
	}
}

// filterPortsOf are the ports a resource-monitoring declaration carries.
func filterPortsOf(ports any) (filter.Resetter, filter.LastChangedTimeWriter) {
	var (
		r filter.Resetter
		w filter.LastChangedTimeWriter
	)
	switch d := ports.(type) {
	case hepadef.Declare:
		if d.Resetter != nil {
			r = d.Resetter
		}
		if d.LastChangedTimeWriter != nil {
			w = d.LastChangedTimeWriter
		}
	case acfdef.Declare:
		if d.Resetter != nil {
			r = d.Resetter
		}
		if d.LastChangedTimeWriter != nil {
			w = d.LastChangedTimeWriter
		}
	}
	return r, w
}

// newLaundryWasherControlsServer is the module's LaundryWasherControls
// server, its spin speeds and rinses from the declaration's Initial
// (SpinSpeeds, SpinSpeedCurrent, SupportedRinses, NumberOfRinses). The
// rinses a RINSE selection serves are host data (an empty list leaves no
// valid NumberOfRinses): without them that selection keeps the generated
// server.
func newLaundryWasherControlsServer(b defaultBuild) (contract.ClusterServer, error) {
	cfg := appliance.WasherConfig{Features: b.opts.Features, DataVersion: b.cfg.DataVersion}
	configFrom(&cfg, b.def, b.initial)
	srv, err := appliance.NewLaundryWasherControls(cfg)
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
	if d, ok := b.ports.(tuidef.Declare); ok && d.SettingStore != nil {
		cfg.Settings = d.SettingStore
		if b.key != "" {
			cfg.SettingKey = b.key + ".keypad_lockout"
		}
	}
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
