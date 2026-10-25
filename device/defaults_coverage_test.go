// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/cover"
	"github.com/SukramJ/go-fabric/cluster/filter"
	"github.com/SukramJ/go-fabric/cluster/light"
	"github.com/SukramJ/go-fabric/cluster/lock"
	"github.com/SukramJ/go-fabric/cluster/spec"
	acfdef "github.com/SukramJ/go-fabric/cluster/spec/activatedcarbonfiltermonitoring"
	bscdef "github.com/SukramJ/go-fabric/cluster/spec/booleanstateconfiguration"
	ccdef "github.com/SukramJ/go-fabric/cluster/spec/colorcontrol"
	dwadef "github.com/SukramJ/go-fabric/cluster/spec/dishwasheralarm"
	doorlockdef "github.com/SukramJ/go-fabric/cluster/spec/doorlock"
	hepadef "github.com/SukramJ/go-fabric/cluster/spec/hepafiltermonitoring"
	washerdef "github.com/SukramJ/go-fabric/cluster/spec/laundrywashercontrols"
	rfadef "github.com/SukramJ/go-fabric/cluster/spec/refrigeratoralarm"
	tadef "github.com/SukramJ/go-fabric/cluster/spec/temperaturealarm"
	thermostatdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	tuidef "github.com/SukramJ/go-fabric/cluster/spec/thermostatuserinterfaceconfiguration"
	wcdef "github.com/SukramJ/go-fabric/cluster/spec/windowcovering"
	"github.com/SukramJ/go-fabric/cluster/thermo"
	"github.com/SukramJ/go-fabric/store"
)

// The host ports of the cases below are interfaces the servers keep and do
// not call while they are built; a nil interface embedded stands for each.
type (
	portColorTemperature struct{ ccdef.ColorTemperatureWriter }
	portColor            struct{ ccdef.ColorWriter }
	portMover            struct{ wcdef.Mover }
	portSink             struct{ thermostatdef.Sink }
	portPresets          struct{ thermostatdef.PresetStore }
	portResetter         struct{ hepadef.Resetter }
	portTimeWriter       struct{ hepadef.LastChangedTimeWriter }
	portAcfResetter      struct{ acfdef.Resetter }
	portAcfTimeWriter    struct{ acfdef.LastChangedTimeWriter }
	portAlarmDelegate    struct{ dwadef.Delegate }
	portFridgeDelegate   struct{ rfadef.Delegate }
	portTempDelegate     struct{ tadef.Delegate }
	portBoolDelegate     struct{ bscdef.Delegate }
	portLockState        struct{ doorlockdef.StateSource }
)

// portSettings is a setting store that keeps its values.
type portSettings map[string]string

func (p portSettings) GetSetting(_ context.Context, key string) (value string, ok bool, err error) {
	value, ok = p[key]
	return value, ok, nil
}

func (p portSettings) SetSetting(_ context.Context, key, value string) error {
	p[key] = value
	return nil
}

// portAuth is a credential store that keeps nothing.
type portAuth struct{ portSettings }

func (portAuth) LoadDoorLockAuth(context.Context, string) (store.DoorLockAuth, bool, error) {
	return store.DoorLockAuth{}, false, nil
}

func (portAuth) SaveDoorLockAuth(context.Context, string, store.DoorLockAuth) error { return nil }

// TestColorControlDefaultTakesTheDeclaration: a declared colour-temperature
// writer and colour writer reach the server, and an unknown feature bit is
// the generated server's.
func TestColorControlDefaultTakesTheDeclaration(t *testing.T) {
	t.Parallel()

	b := defaultBuild{
		def: ccdef.Definition, opts: spec.Options{Features: uint32(ccdef.FeatureColorTemperature | ccdef.FeatureXy)},
		sibling: noSiblings, initial: map[uint32]any{},
		ports: ccdef.Declare{ColorTemperatureWriter: portColorTemperature{}, ColorWriter: portColor{}},
	}
	srv, err := selfHeldDefaults[ccdef.ClusterID](b)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := srv.(*light.ColorControlServer); !ok {
		t.Errorf("ColorControl is %T, want the module's server", srv)
	}
	b.opts.Features = 1 << 9
	if _, err := selfHeldDefaults[ccdef.ClusterID](b); !errors.Is(err, ErrSelectionNotServed) {
		t.Errorf("an unknown feature bit: %v, want ErrSelectionNotServed", err)
	}
}

// TestColorConfigFromTheInitialState: the physical range, the coupling
// floor, the colour and the mode the declaration's Initial states are the
// server's configuration.
func TestColorConfigFromTheInitialState(t *testing.T) {
	t.Parallel()

	initial := map[uint32]any{
		ccdef.AttrColorTempPhysicalMinMireds:       uint16(100),
		ccdef.AttrColorTempPhysicalMaxMireds:       uint16(400),
		ccdef.AttrColorTemperatureMireds:           uint16(250),
		ccdef.AttrCoupleColorTempToLevelMinMireds:  uint16(120),
		ccdef.AttrCurrentX:                         uint16(300),
		ccdef.AttrCurrentY:                         uint16(310),
		ccdef.AttrEnhancedCurrentHue:               uint16(1000),
		ccdef.AttrCurrentHue:                       uint8(10),
		ccdef.AttrCurrentSaturation:                uint8(20),
		ccdef.AttrColorMode:                        uint8(light.ColorModeColorTemperature),
		0xFFFF:                                     uint8(1), // not a colour attribute: left alone
		ccdef.AttrRemainingTime:                    uint16(5),
		ccdef.AttrColorTempPhysicalMinMireds + 100: uint16(1),
	}
	var cfg light.ColorControlServerConfig
	colorConfigFrom(&cfg, initial)
	if cfg.MinMireds != 100 || cfg.MaxMireds != 400 || cfg.InitialMireds != 250 || cfg.CoupleColorTempToLevelMinMireds != 120 ||
		cfg.InitialX != 300 || cfg.InitialY != 310 || cfg.InitialEnhancedHue != 1000 || cfg.InitialHue != 10 || cfg.InitialSaturation != 20 {
		t.Errorf("configuration %+v", cfg)
	}
	if cfg.InitialColorMode == nil || *cfg.InitialColorMode != light.ColorModeColorTemperature {
		t.Errorf("InitialColorMode %v", cfg.InitialColorMode)
	}
	if _, ok := initial[ccdef.AttrCurrentX]; ok {
		t.Error("a consumed attribute is still in Initial")
	}
	if _, ok := initial[ccdef.AttrRemainingTime]; !ok {
		t.Error("an attribute that is no configuration was consumed")
	}

	// The enhanced mode wins over ColorMode; a value beyond the width is
	// left alone.
	both := map[uint32]any{
		ccdef.AttrEnhancedColorMode: uint8(light.ColorModeXY), ccdef.AttrColorMode: uint8(light.ColorModeColorTemperature),
		ccdef.AttrCurrentX: uint32(70000),
	}
	var cfg2 light.ColorControlServerConfig
	colorConfigFrom(&cfg2, both)
	if cfg2.InitialColorMode == nil || *cfg2.InitialColorMode != light.ColorModeXY || cfg2.InitialX != 0 {
		t.Errorf("mode %v, InitialX %d", cfg2.InitialColorMode, cfg2.InitialX)
	}
}

// TestWindowCoveringDefaultTakesTheDeclaration: a declared mover, the
// transitions and the position the covering starts at are the server's
// configuration.
func TestWindowCoveringDefaultTakesTheDeclaration(t *testing.T) {
	t.Parallel()

	b := defaultBuild{
		def: wcdef.Definition, sibling: noSiblings,
		opts:  spec.Options{Features: uint32(wcdef.FeatureLift | wcdef.FeatureTilt | wcdef.FeaturePositionAwareLift | wcdef.FeaturePositionAwareTilt)},
		ports: wcdef.Declare{Mover: portMover{}},
		initial: map[uint32]any{
			wcdef.AttrType: uint8(wcdef.TypeTiltBlindLift), wcdef.AttrEndProductType: uint8(wcdef.EndProductTypeInteriorBlind),
			wcdef.AttrCurrentPositionLiftPercent100ths: uint16(2500), wcdef.AttrTargetPositionLiftPercent100ths: uint16(9),
			wcdef.AttrCurrentPositionTiltPercent100ths: uint16(5000), wcdef.AttrTargetPositionTiltPercent100ths: uint16(9),
		},
	}
	srv, err := selfHeldDefaults[wcdef.ClusterID](b)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := srv.(*cover.WindowCoveringServer); !ok {
		t.Errorf("WindowCovering is %T, want the module's server", srv)
	}
	if v, _ := srv.MatterRead(wcdef.AttrCurrentPositionLiftPercent100ths); v != uint16(2500) {
		t.Errorf("lift %v, want 2500", v)
	}
	if v, _ := srv.MatterRead(wcdef.AttrCurrentPositionTiltPercent100ths); v != uint16(5000) {
		t.Errorf("tilt %v, want 5000", v)
	}
	for _, id := range []uint32{wcdef.AttrTargetPositionLiftPercent100ths, wcdef.AttrTargetPositionTiltPercent100ths} {
		if _, ok := b.initial[id]; ok {
			t.Errorf("the target 0x%04X is still in Initial: the position is both", id)
		}
	}
	// Without a mover the declaration's Transitions travel the covering.
	moving := defaultBuild{
		def: wcdef.Definition, sibling: noSiblings, transitions: true,
		initial: map[uint32]any{wcdef.AttrType: uint8(wcdef.TypeTiltBlindLift), wcdef.AttrEndProductType: uint8(wcdef.EndProductTypeInteriorBlind)},
		opts:    spec.Options{Features: uint32(wcdef.FeatureLift | wcdef.FeatureTilt | wcdef.FeaturePositionAwareLift | wcdef.FeaturePositionAwareTilt)},
	}
	if _, err := selfHeldDefaults[wcdef.ClusterID](moving); err != nil {
		t.Fatalf("with transitions: %v", err)
	}
	// Lift alone is served, as matter.js serves it (WindowCoveringServer.ts:123-141
	// at 862ad75f); AbsolutePosition is not.
	b.opts.Features = 0x1
	if _, err := selfHeldDefaults[wcdef.ClusterID](b); err != nil {
		t.Errorf("Lift alone: %v", err)
	}
	b.opts.Features = 0x1 | 0x4 | 0x8
	if _, err := selfHeldDefaults[wcdef.ClusterID](b); !errors.Is(err, ErrSelectionNotServed) {
		t.Errorf("AbsolutePosition: %v, want ErrSelectionNotServed", err)
	}
}

// TestThermostatDefaultTakesTheDeclaration: a declared sink and preset
// store, and the setpoints the declaration's Initial states.
func TestThermostatDefaultTakesTheDeclaration(t *testing.T) {
	t.Parallel()

	b := defaultBuild{
		def: thermostatdef.Definition, sibling: noSiblings,
		opts: spec.Options{Features: thermo.ThermostatFeatureHEAT | thermo.ThermostatFeatureCOOL | thermo.ThermostatFeatureOCC},
		ports: thermostatdef.Declare{
			Sink: portSink{}, PresetStore: portPresets{},
		},
		initial: map[uint32]any{
			thermostatdef.AttrOccupiedHeatingSetpoint:   int16(2100),
			thermostatdef.AttrOccupiedCoolingSetpoint:   int16(2700),
			thermostatdef.AttrUnoccupiedHeatingSetpoint: int16(1600),
			thermostatdef.AttrUnoccupiedCoolingSetpoint: int16(2900),
		},
	}
	srv, err := selfHeldDefaults[thermostatdef.ClusterID](b)
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[uint32]int16{
		thermostatdef.AttrOccupiedHeatingSetpoint: 2100, thermostatdef.AttrOccupiedCoolingSetpoint: 2700,
		thermostatdef.AttrUnoccupiedHeatingSetpoint: 1600, thermostatdef.AttrUnoccupiedCoolingSetpoint: 2900,
	} {
		if v, _ := srv.MatterRead(id); v != want {
			t.Errorf("attribute 0x%04X = %v, want %d", id, v, want)
		}
		if _, ok := b.initial[id]; ok {
			t.Errorf("attribute 0x%04X is still in Initial", id)
		}
	}
}

// TestDoorLockDefaultTakesTheDeclaration: a declared actuator and
// credential store reach the server, and an unserved feature bit is the
// generated server's.
func TestDoorLockDefaultTakesTheDeclaration(t *testing.T) {
	t.Parallel()

	b := defaultBuild{
		def: doorlockdef.Definition, sibling: noSiblings, key: "test:lock", initial: map[uint32]any{},
		opts:  spec.Options{Features: 0},
		ports: doorlockdef.Declare{StateSource: portLockState{}, AuthStore: portAuth{portSettings{}}},
	}
	srv, err := selfHeldDefaults[doorlockdef.ClusterID](b)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := srv.(*lock.DoorLockServer); !ok {
		t.Errorf("DoorLock is %T, want the module's server", srv)
	}
}

// TestAlarmDelegateOfTheDeclaration: the delegate of each AlarmBase
// derivation's declaration, nil without one.
func TestAlarmDelegateOfTheDeclaration(t *testing.T) {
	t.Parallel()

	for name, ports := range map[string]any{
		"DishwasherAlarm":    dwadef.Declare{Delegate: portAlarmDelegate{}},
		"RefrigeratorAlarm":  rfadef.Declare{Delegate: portFridgeDelegate{}},
		"TemperatureAlarm":   tadef.Declare{Delegate: portTempDelegate{}},
		"another declaraton": bscdef.Declare{},
	} {
		got := alarmDelegateOf(ports)
		if (got != nil) != (name != "another declaraton") {
			t.Errorf("%s: delegate %v", name, got)
		}
	}
	for name, ports := range map[string]any{
		"DishwasherAlarm": dwadef.Declare{}, "RefrigeratorAlarm": rfadef.Declare{}, "TemperatureAlarm": tadef.Declare{}, "none": nil,
	} {
		if got := alarmDelegateOf(ports); got != nil {
			t.Errorf("%s without a delegate: %v", name, got)
		}
	}
}

// TestTemperatureAlarmWithoutItsThresholds: the features serve thresholds
// the declaration does not state, so the cluster keeps the generated
// server.
func TestTemperatureAlarmWithoutItsThresholds(t *testing.T) {
	t.Parallel()

	_, err := selfHeldDefaults[tadef.ClusterID](defaultBuild{
		def: tadef.Definition, sibling: noSiblings, initial: map[uint32]any{},
		opts:  spec.Options{Features: uint32(tadef.FeatureOverTemperature)},
		ports: tadef.Declare{Delegate: portTempDelegate{}},
	})
	if !errors.Is(err, ErrSelectionNotServed) {
		t.Errorf("TemperatureAlarm without thresholds: %v, want ErrSelectionNotServed", err)
	}
}

// TestBooleanStateConfigurationDefault: a declared delegate reaches the
// server, a selection it does not serve is the generated server's.
func TestBooleanStateConfigurationDefault(t *testing.T) {
	t.Parallel()

	b := defaultBuild{
		def: bscdef.Definition, sibling: noSiblings, initial: map[uint32]any{},
		opts:  spec.Options{Features: uint32(bscdef.FeatureVisual)},
		ports: bscdef.Declare{Delegate: portBoolDelegate{}},
	}
	if _, err := selfHeldDefaults[bscdef.ClusterID](b); err != nil {
		t.Fatalf("with a delegate: %v", err)
	}
	b.opts.Features = uint32(bscdef.FeatureAlarmSuppress)
	if _, err := selfHeldDefaults[bscdef.ClusterID](b); !errors.Is(err, ErrSelectionNotServed) {
		t.Errorf("SPRS without VIS or AUD: %v, want ErrSelectionNotServed", err)
	}
}

// TestFilterPortsOfTheDeclarations: the resetter and the writer of both
// resource-monitoring declarations, none for another.
func TestFilterPortsOfTheDeclarations(t *testing.T) {
	t.Parallel()

	for name, ports := range map[string]any{
		"HepaFilterMonitoring":            hepadef.Declare{Resetter: portResetter{}, LastChangedTimeWriter: portTimeWriter{}},
		"ActivatedCarbonFilterMonitoring": acfdef.Declare{Resetter: portAcfResetter{}, LastChangedTimeWriter: portAcfTimeWriter{}},
	} {
		if r, w := filterPortsOf(ports); r == nil || w == nil {
			t.Errorf("%s: resetter %v, writer %v", name, r, w)
		}
	}
	for name, ports := range map[string]any{"Hepa": hepadef.Declare{}, "ActivatedCarbon": acfdef.Declare{}, "none": nil} {
		if r, w := filterPortsOf(ports); r != nil || w != nil {
			t.Errorf("%s without ports: resetter %v, writer %v", name, r, w)
		}
	}
}

// TestFilterDefaultResetsAndReplacementProducts: ResetCondition with its
// resetter, and the ReplacementProductList the declaration's Initial
// states.
func TestFilterDefaultResetsAndReplacementProducts(t *testing.T) {
	t.Parallel()

	products := []hepadef.ReplacementProductStruct{{ProductIdentifierType: hepadef.ProductIdentifierTypeUpc, ProductIdentifierValue: "12345"}}
	b := defaultBuild{
		def: hepadef.Definition, sibling: noSiblings,
		opts: spec.Options{
			Features:   uint32(hepadef.FeatureCondition | hepadef.FeatureReplacementProductList),
			Commands:   []uint32{hepadef.CmdResetCondition},
			Attributes: []uint32{hepadef.AttrLastChangedTime},
		},
		ports:   hepadef.Declare{Resetter: portResetter{}, LastChangedTimeWriter: portTimeWriter{}},
		initial: map[uint32]any{hepadef.AttrReplacementProductList: products},
	}
	srv, err := selfHeldDefaults[hepadef.ClusterID](b)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := srv.(*filter.Server); !ok {
		t.Errorf("HepaFilterMonitoring is %T, want the module's server", srv)
	}
	if v, ok := srv.MatterRead(hepadef.AttrReplacementProductList); !ok || reflect.ValueOf(v).Len() != 1 {
		t.Errorf("ReplacementProductList %v, %t", v, ok)
	}
	if _, ok := b.initial[hepadef.AttrReplacementProductList]; ok {
		t.Error("the list is still in Initial")
	}
}

// TestLaundryWasherControlsWithoutItsRinses: a RINSE selection without the
// rinses the host states keeps the generated server.
func TestLaundryWasherControlsWithoutItsRinses(t *testing.T) {
	t.Parallel()

	_, err := selfHeldDefaults[washerdef.ClusterID](defaultBuild{
		def: washerdef.Definition, sibling: noSiblings, initial: map[uint32]any{},
		opts: spec.Options{Features: uint32(washerdef.FeatureRinse)},
	})
	if !errors.Is(err, ErrSelectionNotServed) {
		t.Errorf("RINSE without rinses: %v, want ErrSelectionNotServed", err)
	}
}

// TestThermostatUIDefaultTakesItsSettingStore: a declared store keeps the
// keypad lockout under the device's key.
func TestThermostatUIDefaultTakesItsSettingStore(t *testing.T) {
	t.Parallel()

	for _, key := range []string{"test:ui", ""} {
		if _, err := selfHeldDefaults[tuidef.ClusterID](defaultBuild{
			def: tuidef.Definition, sibling: noSiblings, key: key, initial: map[uint32]any{},
			ports: tuidef.Declare{SettingStore: portSettings{}},
		}); err != nil {
			t.Errorf("key %q: %v", key, err)
		}
	}
}
