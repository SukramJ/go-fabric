// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/cluster/alarm"
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
	tcdef "github.com/SukramJ/go-fabric/cluster/spec/temperaturecontrol"
	valvedef "github.com/SukramJ/go-fabric/cluster/spec/valveconfigurationandcontrol"
	whmgdef "github.com/SukramJ/go-fabric/cluster/spec/waterheatermanagement"
	"github.com/SukramJ/go-fabric/cluster/thermo"
	"github.com/SukramJ/go-fabric/cluster/valve"
	"github.com/SukramJ/go-fabric/contract"
)

// The kit's servers of the appliance, closure, energy, pump and valve
// clusters on the ports their declarations carry (brief BU-b): a cluster
// whose server needs a port keeps the generated server without one; one
// that needs none (the dryer's controls, the tariff, the energy
// preference, MicrowaveOvenMode) is the kit's once the declaration's
// Initial holds what it needs. Each takes its configuration — the
// attributes its Config names — from the declaration's Initial
// ([configFrom]); a server whose state is its port's (the pump, the
// valve, ModeSelect, SmokeCoAlarm) takes no initial value beyond it.

func init() {
	for id, f := range map[uint32]serverFactory{
		pumpdef.ClusterID:  newPumpServer,
		valvedef.ClusterID: newValveServer,
		msdef.ClusterID:    newModeSelectServer,
		sadef.ClusterID:    newServiceAreaServer,
		ccldef.ClusterID:   newClosureControlServer,
		cddef.ClusterID:    newClosureDimensionServer,
		tcdef.ClusterID:    newTemperatureControlServer,
		mwocdef.ClusterID:  newMicrowaveControlServer,
		mwomdef.ClusterID:  newMicrowaveModeServer,
		whmgdef.ClusterID:  newWaterHeaterServer,
		demdef.ClusterID:   newDeviceEnergyManagementServer,
		ldcdef.ClusterID:   newDryerControlsServer,
		smokedef.ClusterID: newSmokeCoAlarmServer,
		ctdef.ClusterID:    newCommodityTariffServer,
		epdef.ClusterID:    newEnergyPreferenceServer,
		bsdef.ClusterID:    newBooleanStateServer,
	} {
		selfHeldDefaults[id] = f
	}
}

// tariffSyncInterval is the kit's CommodityTariff time sync: chip's
// energy-gateway app polls every 30 s
// (commodity-tariff/include/CommodityTariffInstance.h:27
// kTimerPollIntervalInSec).
const tariffSyncInterval = 30 * time.Second

// configFrom fills the fields of the configuration struct cfg points to
// that are named as an attribute of def (the name compared without case)
// from initial — the attribute's value converted to the field's type
// ([spec.Convert]) — and removes each attribute it took. A field no
// attribute names, and a value its field cannot hold, are left alone.
func configFrom(cfg any, def *spec.Cluster, initial map[uint32]any) {
	rv := reflect.ValueOf(cfg).Elem()
	for _, id := range slices.Sorted(maps.Keys(initial)) {
		a := def.Attribute(id)
		if a == nil {
			continue
		}
		f := rv.FieldByNameFunc(func(n string) bool { return strings.EqualFold(n, a.Name) })
		if !f.IsValid() || !f.CanSet() {
			continue
		}
		if c, ok := spec.Convert(initial[id], f.Type()); ok && c.IsValid() {
			f.Set(c)
			delete(initial, id)
		}
	}
}

// hostState drops the initial values of a server whose state is its
// port's: the port reports them.
func hostState(initial map[uint32]any) { clear(initial) }

// newPumpServer is the module's PumpConfigurationAndControl server
// ([pump.NewServer]) on the declared StateSource, with the limits from the
// declaration's Initial and the optional attributes and events of the
// selection.
func newPumpServer(b defaultBuild) (contract.ClusterServer, error) {
	d, _ := b.ports.(pumpdef.Declare)
	if d.StateSource == nil {
		return nil, notServed(fmt.Errorf("pump: %w", errNoPort))
	}
	cfg := pump.Config{
		Source: d.StateSource, Features: pump.Feature(b.opts.Features), Optional: pump.OptionalFor(b.opts.Attributes),
		Events: slices.Clone(b.opts.Events), DataVersion: b.cfg.DataVersion,
	}
	configFrom(&cfg.Limits, b.def, b.initial)
	hostState(b.initial)
	return pump.NewServer(cfg)
}

// newValveServer is the module's ValveConfigurationAndControl server
// ([valve.NewServer]) on the declared StateSource.
func newValveServer(b defaultBuild) (contract.ClusterServer, error) {
	d, _ := b.ports.(valvedef.Declare)
	if d.StateSource == nil {
		return nil, notServed(fmt.Errorf("valve: %w", errNoPort))
	}
	if b.opts.Features != 0 {
		return nil, notServed(errors.New("valve: the server serves no feature"))
	}
	hostState(b.initial)
	return valve.NewServer(valve.Config{Source: d.StateSource, Events: slices.Clone(b.opts.Events), DataVersion: b.cfg.DataVersion}), nil
}

// newModeSelectServer is the module's ModeSelect server
// ([modeselect.NewServer]) on the declared ModeSource.
func newModeSelectServer(b defaultBuild) (contract.ClusterServer, error) {
	d, _ := b.ports.(msdef.Declare)
	if d.ModeSource == nil {
		return nil, notServed(fmt.Errorf("modeselect: %w", errNoPort))
	}
	hostState(b.initial)
	return modeselect.NewServer(modeselect.Config{Source: d.ModeSource, DataVersion: b.cfg.DataVersion}), nil
}

// newServiceAreaServer is the module's ServiceArea server
// ([servicearea.New]) on the declared Selector (and Skipper), with the
// areas, maps, selection and progress from the declaration's Initial.
func newServiceAreaServer(b defaultBuild) (contract.ClusterServer, error) {
	d, _ := b.ports.(sadef.Declare)
	if d.Selector == nil {
		return nil, notServed(fmt.Errorf("servicearea: %w", errNoPort))
	}
	cfg := servicearea.Config{
		Features: servicearea.Feature(b.opts.Features), Selector: d.Selector, Skipper: d.Skipper,
		CurrentArea:      slices.Contains(b.opts.Attributes, sadef.AttrCurrentArea),
		EstimatedEndTime: slices.Contains(b.opts.Attributes, sadef.AttrEstimatedEndTime),
		DataVersion:      b.cfg.DataVersion, Clock: b.clock,
	}
	if v, ok := b.initial[sadef.AttrCurrentArea]; ok {
		delete(b.initial, sadef.AttrCurrentArea)
		if id, ok := v.(uint32); ok {
			cfg.InitialCurrentArea = &id
		}
	}
	configFrom(&cfg, b.def, b.initial)
	return servicearea.New(cfg)
}

// newClosureControlServer is the module's ClosureControl server
// ([closure.New]) on the declared Mover.
func newClosureControlServer(b defaultBuild) (contract.ClusterServer, error) {
	d, _ := b.ports.(ccldef.Declare)
	if d.Mover == nil {
		return nil, notServed(fmt.Errorf("closurecontrol: %w", errNoPort))
	}
	srv, err := closure.New(closure.Config{FeatureMap: b.opts.Features, Mover: d.Mover})
	if errors.Is(err, closure.ErrFeatureMap) {
		return nil, notServed(err)
	}
	return srv, err
}

// newClosureDimensionServer is the module's ClosureDimension server
// ([closure.NewDimension]) on the declared DimensionHandler, its
// configuration and initial states from the declaration's Initial.
func newClosureDimensionServer(b defaultBuild) (contract.ClusterServer, error) {
	d, _ := b.ports.(cddef.Declare)
	if d.DimensionHandler == nil {
		return nil, notServed(fmt.Errorf("closuredimension: %w", errNoPort))
	}
	cfg := closure.DimensionConfig{
		Features: closure.DimensionFeature(b.opts.Features), Handler: d.DimensionHandler,
		DataVersion: b.cfg.DataVersion, Clock: b.clock,
	}
	configFrom(&cfg, b.def, b.initial)
	return closure.NewDimension(cfg)
}

// newTemperatureControlServer is the module's TemperatureControl server
// ([thermo.NewTemperatureControl]) on the declared TemperatureSetter, its
// limits, levels and initial setpoint from the declaration's Initial.
func newTemperatureControlServer(b defaultBuild) (contract.ClusterServer, error) {
	d, _ := b.ports.(tcdef.Declare)
	if d.TemperatureSetter == nil {
		return nil, notServed(fmt.Errorf("temperaturecontrol: %w", errNoPort))
	}
	cfg := thermo.TemperatureControlConfig{
		Features: thermo.TemperatureControlFeature(b.opts.Features), Setter: d.TemperatureSetter, DataVersion: b.cfg.DataVersion,
	}
	configFrom(&cfg, b.def, b.initial)
	return thermo.NewTemperatureControl(cfg)
}

// newMicrowaveControlServer is the module's MicrowaveOvenControl server
// ([appliance.NewMicrowaveOvenControl]) on the declared Oven, its cook
// time and power configuration from the declaration's Initial.
func newMicrowaveControlServer(b defaultBuild) (contract.ClusterServer, error) {
	d, _ := b.ports.(mwocdef.Declare)
	if d.Oven == nil {
		return nil, notServed(fmt.Errorf("microwaveovencontrol: %w", errNoPort))
	}
	cfg := appliance.MicrowaveConfig{
		Features: b.opts.Features, Oven: d.Oven, DataVersion: b.cfg.DataVersion,
		AddMoreTime: slices.Contains(b.opts.Commands, mwocdef.CmdAddMoreTime),
	}
	configFrom(&cfg, b.def, b.initial)
	return appliance.NewMicrowaveOvenControl(cfg)
}

// newMicrowaveModeServer is the module's MicrowaveOvenMode server
// ([modebase.NewMicrowaveOvenMode]): no ChangeToMode, the modes from the
// declaration's Initial; without them the cluster keeps the generated
// server.
func newMicrowaveModeServer(b defaultBuild) (contract.ClusterServer, error) {
	v, ok := b.initial[modeAttrSupportedModes]
	if !ok {
		return nil, notServed(errors.New("microwaveovenmode: the declaration's Initial carries no SupportedModes"))
	}
	delete(b.initial, modeAttrSupportedModes)
	cfg := modebase.Config{SupportedModes: modeOptionsOf(v), Features: modebase.Feature(b.opts.Features), DataVersion: b.cfg.DataVersion}
	if m, ok := consume8(b.initial, modeAttrCurrentMode); ok {
		cfg.CurrentMode = m
	}
	return modebase.NewMicrowaveOvenMode(cfg)
}

// newWaterHeaterServer is the module's WaterHeaterManagement server
// ([energy.NewWaterHeaterManagement]) on the declared Booster, HeaterTypes
// from the declaration's Initial.
func newWaterHeaterServer(b defaultBuild) (contract.ClusterServer, error) {
	d, _ := b.ports.(whmgdef.Declare)
	if d.Booster == nil {
		return nil, notServed(fmt.Errorf("waterheatermanagement: %w", errNoPort))
	}
	cfg := energy.WaterHeaterConfig{Features: energy.WaterHeaterFeature(b.opts.Features), Booster: d.Booster, DataVersion: b.cfg.DataVersion}
	configFrom(&cfg, b.def, b.initial)
	return energy.NewWaterHeaterManagement(cfg)
}

// newDeviceEnergyManagementServer is the module's DeviceEnergyManagement
// server ([energy.NewDeviceEnergyManagement]) on the declared DemManager,
// the ESA's description from the declaration's Initial.
func newDeviceEnergyManagementServer(b defaultBuild) (contract.ClusterServer, error) {
	d, _ := b.ports.(demdef.Declare)
	if d.DemManager == nil {
		return nil, notServed(fmt.Errorf("deviceenergymanagement: %w", errNoPort))
	}
	cfg := energy.DeviceEnergyConfig{
		Features: energy.DemFeature(b.opts.Features), Manager: d.DemManager, Clock: b.clock, DataVersion: b.cfg.DataVersion,
	}
	configFrom(&cfg, b.def, b.initial)
	return energy.NewDeviceEnergyManagement(cfg)
}

// newDryerControlsServer is the module's LaundryDryerControls server
// ([appliance.NewLaundryDryerControls]), its dryness levels from the
// declaration's Initial; without them the cluster keeps the generated
// server.
func newDryerControlsServer(b defaultBuild) (contract.ClusterServer, error) {
	if _, ok := b.initial[ldcdef.AttrSupportedDrynessLevels]; !ok {
		return nil, notServed(errors.New("laundrydryercontrols: the declaration's Initial carries no SupportedDrynessLevels"))
	}
	cfg := appliance.DryerConfig{DataVersion: b.cfg.DataVersion}
	configFrom(&cfg, b.def, b.initial)
	return appliance.NewLaundryDryerControls(cfg)
}

// newSmokeCoAlarmServer is the module's SmokeCoAlarm server
// ([alarm.NewServer]) on the declared StateSource, with the optional
// attributes of the selection.
func newSmokeCoAlarmServer(b defaultBuild) (contract.ClusterServer, error) {
	d, _ := b.ports.(smokedef.Declare)
	if d.StateSource == nil {
		return nil, notServed(fmt.Errorf("smokecoalarm: %w", errNoPort))
	}
	hostState(b.initial)
	return alarm.NewServer(alarm.Config{
		Source: d.StateSource, Features: alarm.Feature(b.opts.Features), Optional: alarm.OptionalFor(b.opts.Attributes),
		DataVersion: b.cfg.DataVersion,
	})
}

// newCommodityTariffServer is the module's CommodityTariff server
// ([energy.NewCommodityTariff]) on the device's clock, syncing itself
// every [tariffSyncInterval]; the host sets the tariff through the typed
// Set.
func newCommodityTariffServer(b defaultBuild) (contract.ClusterServer, error) {
	clk := b.clock
	d, _ := b.ports.(ctdef.Declare)
	if d.TimeSource != nil {
		clk = sourceClock{Clock: b.clock, now: d.TimeSource.Now}
	}
	srv, err := energy.NewCommodityTariff(energy.CommodityTariffConfig{
		Features: energy.TariffFeature(b.opts.Features), Clock: clk, SyncInterval: tariffSyncInterval, DataVersion: b.cfg.DataVersion,
	})
	if err != nil {
		return nil, notServed(err)
	}
	if n, ok := d.TimeSource.(contract.ChangeNotifier); ok {
		n.OnMatterValueChanged(func() { _ = srv.SyncTime() }) // the device's lifetime: the source is the device's
	}
	return srv, nil
}

// sourceClock is a clock whose Now is a declared TimeSource's; its timers
// run on the device's clock.
type sourceClock struct {
	clock.Clock
	now func() time.Time
}

// Now implements [clock.Clock].
func (c sourceClock) Now() time.Time { return c.now() }

// newBooleanStateServer is the module's BooleanState server that holds
// StateValue ([measurement.NewHeldBooleanState]) — matter.js's
// BooleanStateServer, which emits StateChange on every change with the
// ChangeEvent feature; the host sets StateValue through the typed Set.
func newBooleanStateServer(b defaultBuild) (contract.ClusterServer, error) {
	return measurement.NewHeldBooleanState(measurement.HeldBooleanStateConfig{
		Features: bsdef.Feature(b.opts.Features), DataVersion: b.cfg.DataVersion,
	})
}

// newEnergyPreferenceServer is the module's EnergyPreference server
// ([energy.NewEnergyPreference]), its lists and indexes from the
// declaration's Initial; a selection whose lists it lacks keeps the
// generated server.
func newEnergyPreferenceServer(b defaultBuild) (contract.ClusterServer, error) {
	cfg := energy.PreferenceConfig{Features: energy.PreferenceFeature(b.opts.Features), DataVersion: b.cfg.DataVersion}
	configFrom(&cfg, b.def, b.initial)
	srv, err := energy.NewEnergyPreference(cfg)
	if err != nil {
		return nil, notServed(err)
	}
	return srv, nil
}
