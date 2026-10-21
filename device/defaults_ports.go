// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"

	"github.com/SukramJ/go-fabric/cluster/fan"
	"github.com/SukramJ/go-fabric/cluster/levelcontrol"
	"github.com/SukramJ/go-fabric/cluster/modebase"
	demmdef "github.com/SukramJ/go-fabric/cluster/spec/deviceenergymanagementmode"
	dwmdef "github.com/SukramJ/go-fabric/cluster/spec/dishwashermode"
	evsemdef "github.com/SukramJ/go-fabric/cluster/spec/energyevsemode"
	fandef "github.com/SukramJ/go-fabric/cluster/spec/fancontrol"
	lwmdef "github.com/SukramJ/go-fabric/cluster/spec/laundrywashermode"
	lvldef "github.com/SukramJ/go-fabric/cluster/spec/levelcontrol"
	ovenmdef "github.com/SukramJ/go-fabric/cluster/spec/ovenmode"
	rtccdef "github.com/SukramJ/go-fabric/cluster/spec/refrigeratorandtemperaturecontrolledcabinetmode"
	rvccdef "github.com/SukramJ/go-fabric/cluster/spec/rvccleanmode"
	rvcrdef "github.com/SukramJ/go-fabric/cluster/spec/rvcrunmode"
	whmdef "github.com/SukramJ/go-fabric/cluster/spec/waterheatermode"
	"github.com/SukramJ/go-fabric/contract"
)

// The kit's servers that run on a host port the cluster's declaration
// carries: without the port the cluster keeps the generated server, as
// before; with it the module's server is the default (brief BU, the ports
// brief BX1 listed). Their configuration attributes (a fan's
// FanModeSequence, a mode cluster's SupportedModes) come from the
// declaration's Initial.

// errNoPort: the declaration carries no port for a server that needs one.
var errNoPort = errors.New("device: the declaration carries no host port for the cluster's server")

func init() {
	for id, f := range map[uint32]serverFactory{
		lwmdef.ClusterID:   modeServer(modebase.NewLaundryWasherMode, func(p any) modePort { d, _ := p.(lwmdef.Declare); return d.ModeChanger }),
		dwmdef.ClusterID:   modeServer(modebase.NewDishwasherMode, func(p any) modePort { d, _ := p.(dwmdef.Declare); return d.ModeChanger }),
		rvcrdef.ClusterID:  modeServer(modebase.NewRvcRunMode, func(p any) modePort { d, _ := p.(rvcrdef.Declare); return d.ModeChanger }),
		rvccdef.ClusterID:  modeServer(modebase.NewRvcCleanMode, func(p any) modePort { d, _ := p.(rvccdef.Declare); return d.ModeChanger }),
		ovenmdef.ClusterID: modeServer(modebase.NewOvenMode, func(p any) modePort { d, _ := p.(ovenmdef.Declare); return d.ModeChanger }),
		rtccdef.ClusterID: modeServer(modebase.NewRefrigeratorAndTemperatureControlledCabinetMode,
			func(p any) modePort { d, _ := p.(rtccdef.Declare); return d.ModeChanger }),
		evsemdef.ClusterID: modeServer(modebase.NewEnergyEvseMode, func(p any) modePort { d, _ := p.(evsemdef.Declare); return d.ModeChanger }),
		whmdef.ClusterID:   modeServer(modebase.NewWaterHeaterMode, func(p any) modePort { d, _ := p.(whmdef.Declare); return d.ModeChanger }),
		demmdef.ClusterID:  modeServer(modebase.NewDeviceEnergyManagementMode, func(p any) modePort { d, _ := p.(demmdef.Declare); return d.ModeChanger }),
	} {
		selfHeldDefaults[id] = f
	}
}

// newFanControlServer is the module's FanControl server ([fan.NewServer])
// on the declared StateSource and Stepper (fancontroldef.Declare), with
// FanModeSequence, SpeedMax, RockSupport and WindSupport from the
// declaration's Initial — matter.js's FanControlServer, whose state the
// host's device drives.
func newFanControlServer(b defaultBuild) (contract.ClusterServer, error) {
	d, _ := b.ports.(fandef.Declare)
	if d.StateSource == nil {
		return nil, notServed(fmt.Errorf("fancontrol: %w", errNoPort))
	}
	cfg := fan.Config{
		Source: d.StateSource, Stepper: d.Stepper,
		Features: fan.Feature(b.opts.Features), DeviceType: b.deviceType, DataVersion: b.cfg.DataVersion,
	}
	if v, ok := consume8(b.initial, fandef.AttrFanModeSequence); ok {
		cfg.Sequence = fan.Sequence(v)
	}
	if v, ok := consume8(b.initial, fandef.AttrSpeedMax); ok {
		cfg.SpeedMax = v
	}
	if v, ok := consume8(b.initial, fandef.AttrRockSupport); ok {
		cfg.RockSupport = fan.RockBitmap(v)
	}
	if v, ok := consume8(b.initial, fandef.AttrWindSupport); ok {
		cfg.WindSupport = fan.WindBitmap(v)
	}
	return fan.NewServer(cfg)
}

// newLevelControlPortServer is the module's LevelControl server on the
// declared LevelSource ([levelcontrol.NewServer] with Config.Source), with
// the Lighting feature of the selection and the declaration's Transitions.
func newLevelControlPortServer(b defaultBuild, d lvldef.Declare) (contract.ClusterServer, error) {
	cfg := levelcontrol.Config{
		Source: d.LevelSource, DataVersion: b.cfg.DataVersion,
		Lighting: b.opts.Features&uint32(lvldef.FeatureLighting) != 0,
	}
	if d.Transitions {
		cfg.Transitions = &levelcontrol.Transitions{Clock: b.clock}
	}
	return levelcontrol.NewServer(cfg), nil
}

// modePort is a ModeBase derivation's declared ModeChanger.
type modePort interface {
	ChangeToMode(ctx context.Context, newMode uint8) (status uint8, text string, err error)
}

// modeChanger is a modePort as the [modebase.ModeChanger] the server
// calls.
type modeChanger struct{ p modePort }

// ChangeToMode implements [modebase.ModeChanger].
func (c modeChanger) ChangeToMode(ctx context.Context, newMode uint8) (modebase.Status, string, error) {
	status, text, err := c.p.ChangeToMode(ctx, newMode)
	return modebase.Status(status), text, err
}

// modeServer is the factory of a ModeBase derivation's server on the
// declared ModeChanger, with SupportedModes and CurrentMode from the
// declaration's Initial — matter.js's ModeBase servers
// (packages/node/src/behaviors/<derivation>-mode/*ModeServer.ts), whose modes
// the device type's application states.
func modeServer(newServer func(modebase.Config) (*modebase.Server, error), port func(any) modePort) serverFactory {
	return func(b defaultBuild) (contract.ClusterServer, error) {
		p := port(b.ports)
		if p == nil {
			return nil, notServed(fmt.Errorf("%s: %w", b.def.Name, errNoPort))
		}
		cfg := modebase.Config{Changer: modeChanger{p}, Features: modebase.Feature(b.opts.Features), DataVersion: b.cfg.DataVersion}
		if v, ok := b.initial[modeAttrSupportedModes]; ok {
			delete(b.initial, modeAttrSupportedModes)
			cfg.SupportedModes = modeOptionsOf(v)
		}
		if v, ok := consume8(b.initial, modeAttrCurrentMode); ok {
			cfg.CurrentMode = v
		}
		return newServer(cfg)
	}
}

// The ModeBase attributes every derivation shares.
const (
	modeAttrSupportedModes = lwmdef.AttrSupportedModes
	modeAttrCurrentMode    = lwmdef.AttrCurrentMode
)

// modeOptionsOf converts a derivation's generated SupportedModes list —
// each derivation's own ModeOptionStruct, the same fields in every one —
// to the module server's.
func modeOptionsOf(v any) []modebase.ModeOption {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Slice {
		return nil
	}
	out := make([]modebase.ModeOption, 0, rv.Len())
	for i := range rv.Len() {
		o := rv.Index(i)
		opt := modebase.ModeOption{Label: o.FieldByName("Label").String(), Mode: uint8(o.FieldByName("Mode").Uint())} //nolint:gosec // the generated field is a uint8
		tags := o.FieldByName("ModeTags")
		for j := range tags.Len() {
			t := tags.Index(j)
			tag := modebase.ModeTag{Value: uint16(t.FieldByName("Value").Uint())} //nolint:gosec // the generated ModeTag is a uint16
			if mfg, ok := t.FieldByName("MfgCode").Interface().(*uint16); ok {
				tag.MfgCode = mfg
			}
			opt.Tags = append(opt.Tags, tag)
		}
		out = append(out, opt)
	}
	return out
}

// consume8 is [consume] for an attribute of one octet: a value above 255
// is none (the definition's check refuses it later).
func consume8(initial map[uint32]any, attrID uint32) (uint8, bool) {
	v, ok := consume(initial, attrID)
	if !ok || v > math.MaxUint8 {
		return 0, false
	}
	return uint8(v), true
}

// consume removes attrID from initial and returns its value as an
// unsigned integer: a typed value of the declaration's Patch, or the
// uint64 of a device-type default.
func consume(initial map[uint32]any, attrID uint32) (uint64, bool) {
	v, ok := initial[attrID]
	if !ok {
		return 0, false
	}
	delete(initial, attrID)
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return rv.Uint(), true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if rv.Int() >= 0 {
			return uint64(rv.Int()), true //nolint:gosec // not negative
		}
	default:
	}
	return 0, false
}
