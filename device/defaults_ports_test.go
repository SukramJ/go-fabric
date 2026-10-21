// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/fan"
	"github.com/SukramJ/go-fabric/cluster/modebase"
	"github.com/SukramJ/go-fabric/cluster/spec"
	demdef "github.com/SukramJ/go-fabric/cluster/spec/deviceenergymanagement"
	demmdef "github.com/SukramJ/go-fabric/cluster/spec/deviceenergymanagementmode"
	dwmdef "github.com/SukramJ/go-fabric/cluster/spec/dishwashermode"
	evsemdef "github.com/SukramJ/go-fabric/cluster/spec/energyevsemode"
	fandef "github.com/SukramJ/go-fabric/cluster/spec/fancontrol"
	lwmdef "github.com/SukramJ/go-fabric/cluster/spec/laundrywashermode"
	ovenmdef "github.com/SukramJ/go-fabric/cluster/spec/ovenmode"
	rtccdef "github.com/SukramJ/go-fabric/cluster/spec/refrigeratorandtemperaturecontrolledcabinetmode"
	rvccdef "github.com/SukramJ/go-fabric/cluster/spec/rvccleanmode"
	rvcrdef "github.com/SukramJ/go-fabric/cluster/spec/rvcrunmode"
	whmdef "github.com/SukramJ/go-fabric/cluster/spec/waterheatermode"
)

// fakeFan is a fan host port in the generated types.
type fakeFan struct{ applied []fandef.Patch }

func (f *fakeFan) FanState() fandef.State { return fandef.State{} }

func (f *fakeFan) ApplyFanSettings(_ context.Context, p fandef.Patch) error {
	f.applied = append(f.applied, p)
	return nil
}

// fakeModes is a mode changer of every ModeBase derivation.
type fakeModes struct{ changed []uint8 }

func (f *fakeModes) ChangeToMode(_ context.Context, mode uint8) (uint8, string, error) {
	f.changed = append(f.changed, mode)
	return 0, "", nil
}

// modeOf builds one SupportedModes entry of a derivation with one tag.
type modeOf[O any] func(label string, mode uint8, tag uint16) O

func modes[O any](mk modeOf[O], tags ...uint16) *[]O {
	out := make([]O, 0, len(tags))
	for i, tag := range tags {
		out = append(out, mk(string(rune('A'+i)), uint8(i), tag))
	}
	return &out
}

// portCases are TestSelfHeldDefaults' cases of the servers that run on a
// declared host port: the declaration with the port mounts the module's
// server.
func portCases() (cases []struct {
	cluster uint32
	build   func() (*Device, error)
	want    any
},
) {
	ch := &fakeModes{}
	return []struct {
		cluster uint32
		build   func() (*Device, error)
		want    any
	}{
		{fandef.ClusterID, func() (*Device, error) {
			return New(&FanType, testOptions("fan"), fandef.Declare{StateSource: &fakeFan{}})
		}, (*fan.Server)(nil)},
		{lwmdef.ClusterID, func() (*Device, error) {
			mk := func(l string, m uint8, tag uint16) lwmdef.ModeOptionStruct {
				return lwmdef.ModeOptionStruct{Label: l, Mode: m, ModeTags: []lwmdef.ModeTagStruct{{Value: lwmdef.ModeTag(tag)}}}
			}
			return New(&LaundryWasherType, testOptions("lwm"), lwmdef.Declare{
				ModeChanger: ch,
				Initial:     lwmdef.Patch{SupportedModes: modes(mk, modebase.LaundryTagNormal, modebase.LaundryTagHeavy)},
			})
		}, (*modebase.Server)(nil)},
		{dwmdef.ClusterID, func() (*Device, error) {
			mk := func(l string, m uint8, tag uint16) dwmdef.ModeOptionStruct {
				return dwmdef.ModeOptionStruct{Label: l, Mode: m, ModeTags: []dwmdef.ModeTagStruct{{Value: dwmdef.ModeTag(tag)}}}
			}
			return New(&DishwasherType, testOptions("dwm"), dwmdef.Declare{
				ModeChanger: ch,
				Initial:     dwmdef.Patch{SupportedModes: modes(mk, modebase.DishwasherTagNormal, modebase.DishwasherTagHeavy)},
			})
		}, (*modebase.Server)(nil)},
		{rvcrdef.ClusterID, func() (*Device, error) {
			mk := func(l string, m uint8, tag uint16) rvcrdef.ModeOptionStruct {
				return rvcrdef.ModeOptionStruct{Label: l, Mode: m, ModeTags: []rvcrdef.ModeTagStruct{{Value: rvcrdef.ModeTag(tag)}}}
			}
			return New(&RoboticVacuumCleanerType, testOptions("rvcr"), rvcrdef.Declare{
				ModeChanger: ch,
				Initial:     rvcrdef.Patch{SupportedModes: modes(mk, modebase.RvcRunTagIdle, modebase.RvcRunTagCleaning)},
			})
		}, (*modebase.Server)(nil)},
		{rvccdef.ClusterID, func() (*Device, error) {
			mk := func(l string, m uint8, tag uint16) rvccdef.ModeOptionStruct {
				return rvccdef.ModeOptionStruct{Label: l, Mode: m, ModeTags: []rvccdef.ModeTagStruct{{Value: rvccdef.ModeTag(tag)}}}
			}
			return New(&RoboticVacuumCleanerType, testOptions("rvcc"), rvccdef.Declare{
				ModeChanger: ch,
				Initial:     rvccdef.Patch{SupportedModes: modes(mk, modebase.RvcCleanTagVacuum, modebase.RvcCleanTagMop)},
			})
		}, (*modebase.Server)(nil)},
		{ovenmdef.ClusterID, func() (*Device, error) {
			mk := func(l string, m uint8, tag uint16) ovenmdef.ModeOptionStruct {
				return ovenmdef.ModeOptionStruct{Label: l, Mode: m, ModeTags: []ovenmdef.ModeTagStruct{{Value: ovenmdef.ModeTag(tag)}}}
			}
			opts := testOptions("ovenm")
			opts.Conditions = []string{"Heater"}
			return New(&TemperatureControlledCabinetType, opts, ovenmdef.Declare{
				ModeChanger: ch,
				Initial:     ovenmdef.Patch{SupportedModes: modes(mk, modebase.OvenTagBake, modebase.OvenTagGrill)},
			})
		}, (*modebase.Server)(nil)},
		{rtccdef.ClusterID, func() (*Device, error) {
			mk := func(l string, m uint8, tag uint16) rtccdef.ModeOptionStruct {
				return rtccdef.ModeOptionStruct{Label: l, Mode: m, ModeTags: []rtccdef.ModeTagStruct{{Value: rtccdef.ModeTag(tag)}}}
			}
			return New(&RefrigeratorType, testOptions("rtcc"), rtccdef.Declare{
				ModeChanger: ch,
				Initial:     rtccdef.Patch{SupportedModes: modes(mk, uint16(rtccdef.ModeTagAuto), uint16(rtccdef.ModeTagQuick))},
			})
		}, (*modebase.Server)(nil)},
		{evsemdef.ClusterID, func() (*Device, error) {
			mk := func(l string, m uint8, tag uint16) evsemdef.ModeOptionStruct {
				return evsemdef.ModeOptionStruct{Label: l, Mode: m, ModeTags: []evsemdef.ModeTagStruct{{Value: evsemdef.ModeTag(tag)}}}
			}
			return New(&EnergyEvseType, testOptions("evsem"), evsemdef.Declare{
				ModeChanger: ch,
				Initial:     evsemdef.Patch{SupportedModes: modes(mk, modebase.EvseTagManual, modebase.EvseTagTimeOfUse)},
			})
		}, (*modebase.Server)(nil)},
		{whmdef.ClusterID, func() (*Device, error) {
			mk := func(l string, m uint8, tag uint16) whmdef.ModeOptionStruct {
				return whmdef.ModeOptionStruct{Label: l, Mode: m, ModeTags: []whmdef.ModeTagStruct{{Value: whmdef.ModeTag(tag)}}}
			}
			return New(&WaterHeaterType, testOptions("whm"), withFeatures(0x0201, 1), whmdef.Declare{
				ModeChanger: ch,
				Initial:     whmdef.Patch{SupportedModes: modes(mk, modebase.WaterHeaterTagManual, modebase.WaterHeaterTagOff)},
			})
		}, (*modebase.Server)(nil)},
		{demmdef.ClusterID, func() (*Device, error) {
			mk := func(l string, m uint8, tag uint16) demmdef.ModeOptionStruct {
				return demmdef.ModeOptionStruct{Label: l, Mode: m, ModeTags: []demmdef.ModeTagStruct{{Value: demmdef.ModeTag(tag)}}}
			}
			return New(&DeviceEnergyManagementType, testOptions("demm"),
				demdef.Declare{Features: demdef.FeaturePowerForecastReporting}, demmdef.Declare{
					ModeChanger: ch,
					Initial:     demmdef.Patch{SupportedModes: modes(mk, modebase.DemTagNoOptimization, modebase.DemTagLocalOptimization, modebase.DemTagGridOptimization)},
				})
		}, (*modebase.Server)(nil)},
	}
}

// TestPortServersNeedTheirPort: without the declared port each of these
// clusters keeps the generated server, as before the declaration.
func TestPortServersNeedTheirPort(t *testing.T) {
	t.Parallel()
	d, err := New(&FanType, testOptions("fan"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := d.Server(fandef.ClusterID).(*spec.Server); !ok {
		t.Errorf("Fan without a StateSource mounts %T, want the generated server", d.Server(fandef.ClusterID))
	}
	_, err = selfHeldDefaults[lwmdef.ClusterID](defaultBuild{def: lwmdef.Definition, sibling: noSiblings})
	if !errors.Is(err, errSelectionNotServed) || !errors.Is(err, errNoPort) {
		t.Errorf("LaundryWasherMode without a ModeChanger: %v, want errNoPort", err)
	}
}
