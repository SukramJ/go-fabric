// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/levelcontrol"
	"github.com/SukramJ/go-fabric/cluster/light"
	"github.com/SukramJ/go-fabric/cluster/onoff"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/device"
)

// extendedColorFeatures are the ColorControl features of the extended
// colour light: every feature matter.js's ColorControlServer implements,
// as matter.js's own CHIP test endpoint enables them
// (support/chip-testing/src/devices/ExtendedColorLightEndpoint.ts:
// Xy, ColorTemperature, HueSaturation, EnhancedHue, ColorLoop), so the
// certification cases of every ColorControl feature run against it.
const extendedColorFeatures = light.ColorFeatureXY | light.ColorFeatureColorTemperature |
	light.ColorFeatureHueSaturation | light.ColorFeatureEnhancedHue | light.ColorFeatureColorLoop

// lightingSurface is what turns a [dimmer] into a colour-temperature light:
// the LevelControl Lighting attributes and the colour temperature (the
// OnOff Lighting state is the device layer's default OnOff server). Its servers hold state of their own (the ColorControl
// server keeps the current mireds in-process, the LevelControl server
// StartUpCurrentLevel and the running transition), so the device builds
// them once and hands the same instances to every reassembly — a fresh
// server per call would reset the colour temperature to its initial value
// whenever the topology is rebuilt.
type lightingSurface struct {
	// extended makes the light an ExtendedColorLight: ColorControl with
	// every feature, starting in the XY mode.
	extended bool
	once     sync.Once
	servers  []contract.ClusterServer
}

// newDemoCeilingLight returns a colour-temperature light that is off at
// half brightness.
func newDemoCeilingLight(name string) *dimmer {
	return &dimmer{
		name:     name,
		kind:     "ceiling",
		minLevel: levelcontrol.LightingLevelMin,
		level:    127,
		lighting: &lightingSurface{},
	}
}

// newDemoColorLight returns an extended colour light that is off at half
// brightness, showing CIE x/y in the XY mode — the mode matter.js's CHIP
// test endpoint starts in (ExtendedColorLightEndpoint.ts).
func newDemoColorLight(name string) *dimmer {
	return &dimmer{
		name:     name,
		kind:     "colorlight",
		minLevel: levelcontrol.LightingLevelMin,
		level:    127,
		lighting: &lightingSurface{extended: true},
	}
}

// lightingServers builds the light's cluster surface once.
//
// The lamp cannot ramp, so both servers run the module's transition engine
// (cluster/transition, matter.js behavior/Transitions.ts): LevelControl
// with the Lighting feature steps the level through the dimmer's
// [dimmer.MoveToLevel] every 100 ms and couples it to the dimmer's On/Off
// state and to the colour temperature; ColorControl steps the colour
// temperature through [ceilingColorWriter]. RemainingTime is live, and the
// CHIP cases that read a level or a colour temperature part-way through a
// transition (TC-LVL-3.1 to 6.1, TC-CC-6.2, 6.3) and count its reports
// (TC-LVL-2.3, TC-CC-2.2) run against it. The speaker keeps the hand-off
// path, where the device is assumed to ramp natively.
//
// The extended colour light is the same surface with every ColorControl
// feature: hue and saturation, enhanced hue, the colour loop, x/y and the
// colour temperature all step on the engine, and [lampColorWriter] receives
// the whole colour in whichever mode — the CHIP ColorControl family runs
// against it (TC-CC-3.x to 9.x).
func (s *dimmer) lightingServers() []contract.ClusterServer {
	s.lighting.once.Do(func() {
		cfg := light.DefaultColorControlServerConfig()
		cfg.ManageTransitions = true
		cfg.OnOff = s
		if s.lighting.extended {
			cfg.Features = extendedColorFeatures
		}
		color := light.NewColorControlServer(cfg)
		color.SetWriter(ceilingColorWriter{name: s.name})
		if s.lighting.extended {
			color.SetColorWriter(lampColorWriter{name: s.name})
		}
		// ScenesManagement is the assembler's: the device type mandates it,
		// and the assembler mounts the stack's server there
		// (endpoint/materialize.go mountScenes).
		s.lighting.servers = []contract.ClusterServer{
			levelcontrol.NewServer(levelcontrol.Config{
				Source:      s,
				DataVersion: &s.version,
				Lighting:    true,
				Transitions: &levelcontrol.Transitions{OnOff: s, ColorTemperature: color},
			}),
			color,
		}
	})
	return s.lighting.servers
}

// ceilingColorWriter is where a real host would push the colour
// temperature to the lamp. Here it only logs, which is what the chip-tool
// suite reads back as proof the command reached the device. A transition
// pushes every step; those are logged at debug level, so the log shows one
// line per command.
type ceilingColorWriter struct{ name string }

// SetColorTemperatureMireds implements [light.ColorTemperatureWriter].
func (w ceilingColorWriter) SetColorTemperatureMireds(_ context.Context, mireds uint16) error {
	slog.Debug("ceiling.color", slog.String("device", w.name), slog.Int("mireds", int(mireds)))
	return nil
}

// lampColorWriter is where a real host would push the colour to a lamp
// that takes x/y or hue and saturation. Here it only logs, at debug level
// like [ceilingColorWriter], since a transition pushes every step.
type lampColorWriter struct{ name string }

// SetColor implements [light.ColorWriter].
func (w lampColorWriter) SetColor(_ context.Context, c light.Color) error {
	slog.Debug("colorlight.color", slog.String("device", w.name), slog.Int("mode", int(c.Mode)),
		slog.Int("x", int(c.X)), slog.Int("y", int(c.Y)), slog.Int("hue", int(c.Hue)),
		slog.Int("enhanced_hue", int(c.EnhancedHue)), slog.Int("saturation", int(c.Saturation)),
		slog.Int("mireds", int(c.ColorTemperatureMireds)))
	return nil
}

// OnOff implements [levelcontrol.OnOff] and [light.OnOffState]: the On/Off
// state the light's LevelControl and ColorControl gate and couple on.
func (s *dimmer) OnOff() bool { return s.isOn() }

// SetOnOff implements [levelcontrol.OnOff]: a "with On/Off" level command
// switches the light as its On/Off cluster does, OnLevel included
// ([dimmer.setOn]), and the OnOff server stores and reports the change
// ([dimmer.publishOnOff]).
func (s *dimmer) SetOnOff(_ context.Context, on bool) error {
	s.setOn(on)
	slog.Info(s.kind+".set", slog.String("device", s.name), slog.Bool("on", on))
	s.publishOnOff(on)
	return nil
}

// nativeDimmer is a dimmable light whose lamp ramps by itself — a dimmer
// actuator with a ramp time is the case this stands for — so its
// LevelControl takes the hand-off path through
// [levelcontrol.NewNativeSource]: the source keeps Options and OnLevel,
// applies the ExecuteIfOff gate, couples the "with On/Off" commands to the
// light's On/Off state, and the lamp receives one target level and one
// duration per command ([nativeDimmer.Ramp]). The ramp is simulated: the
// level moves in [nativeRampStep] steps over the duration and notifies at
// each. RemainingTime reads 0, as on every hand-off path.
//
// It covers the hand-off path of a light; it is not mounted in the fleet,
// whose two lights run the transition engine the LVL and CC certification
// families are read against.
type nativeDimmer struct {
	name string
	notifier
	version cluster.DataVersionTracker
	// src is the LevelControl source over the lamp.
	src *levelcontrol.NativeSource

	mu    sync.Mutex
	on    bool
	level uint8
	// ramp counts the ramps; a step of a ramp a later one or a Stop
	// replaced does nothing.
	ramp uint64
	// onOff is the light's OnOff server; nil before [nativeDimmer.device].
	onOff *onoff.Server
}

// nativeRampStep is the step of the simulated ramp.
const nativeRampStep = 100 * time.Millisecond

// Compile-time assertions.
var (
	_ levelcontrol.NativeRamp = (*nativeDimmer)(nil)
	_ levelcontrol.OnOff      = (*nativeDimmer)(nil)
)

// newDemoNativeDimmer returns a dimmable light that is off at half
// brightness.
func newDemoNativeDimmer(name string) *nativeDimmer {
	d := &nativeDimmer{name: name, level: 127}
	d.src = levelcontrol.NewNativeSource(d, d, levelcontrol.NativeConfig{Lighting: true})
	return d
}

// servers is the light's LevelControl, Lighting feature on: DimmableLight
// requires LT on LevelControl (dimmable-light.element.ts:19-33).
func (d *nativeDimmer) servers() []contract.ClusterServer {
	return []contract.ClusterServer{levelcontrol.NewServer(levelcontrol.Config{
		Source: d.src, DataVersion: &d.version, Lighting: true,
	})}
}

// device builds the DimmableLight, OnOff the device layer's default
// server, followed so a switch-on applies OnLevel as a dimmer's does.
func (d *nativeDimmer) device(opts device.Options) (*device.Device, error) {
	l, err := device.NewDimmableLight(opts, serve(d.servers(), device.WithChangeNotifier(d))...)
	if err != nil {
		return nil, err
	}
	srv := l.OnOff()
	if srv == nil {
		return nil, fmt.Errorf("%s: no default OnOff server", d.name)
	}
	d.mu.Lock()
	d.onOff = srv
	d.mu.Unlock()
	followOnOff(l.Device, srv, func(on bool) {
		if d.OnOff() != on {
			d.setOn(on)
		}
	})
	return l.Device, nil
}

// Level implements [levelcontrol.NativeRamp].
func (d *nativeDimmer) Level() (uint8, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.level, true
}

// Ramp implements [levelcontrol.NativeRamp]: the simulated lamp moves to
// level over dur in [nativeRampStep] steps, at once for a shorter dur.
func (d *nativeDimmer) Ramp(_ context.Context, level uint8, dur time.Duration, withOnOff bool) error {
	slog.Info("dimmer.ramp", slog.String("device", d.name), slog.Int("level", int(level)),
		slog.Duration("over", dur), slog.Bool("with_on_off", withOnOff))
	d.mu.Lock()
	d.ramp++
	ramp, from := d.ramp, d.level
	d.mu.Unlock()
	d.rampStep(ramp, from, level, 1, max(int(dur/nativeRampStep), 1))
	return nil
}

// rampStep applies step k of n of a ramp from → to and schedules the next.
func (d *nativeDimmer) rampStep(ramp uint64, from, to uint8, k, n int) {
	d.mu.Lock()
	if d.ramp != ramp {
		d.mu.Unlock()
		return
	}
	d.level = uint8(int(from) + (int(to)-int(from))*k/n) //nolint:gosec // between from and to
	d.mu.Unlock()
	d.notify()
	if k < n {
		time.AfterFunc(nativeRampStep, func() { d.rampStep(ramp, from, to, k+1, n) })
	}
}

// Stop implements [levelcontrol.NativeRamp]: the lamp stays where it is.
func (d *nativeDimmer) Stop(context.Context) error {
	d.mu.Lock()
	d.ramp++
	d.mu.Unlock()
	slog.Info("dimmer.stop", slog.String("device", d.name))
	return nil
}

// OnOff implements [levelcontrol.OnOff].
func (d *nativeDimmer) OnOff() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.on
}

// SetOnOff implements [levelcontrol.OnOff]: a "with On/Off" command
// switches the light as its OnOff cluster does, and the OnOff server
// stores and reports the change.
func (d *nativeDimmer) SetOnOff(_ context.Context, on bool) error {
	d.setOn(on)
	d.mu.Lock()
	srv := d.onOff
	d.mu.Unlock()
	if srv != nil && srv.OnOff() != on {
		srv.Set(on)
	}
	return nil
}

// setOn switches the lamp; switching on applies OnLevel when one is set
// (matter.js LevelControlServer.ts:658-670 handleOnOffChange).
func (d *nativeDimmer) setOn(on bool) {
	onLevel, has := d.src.OnLevel()
	d.mu.Lock()
	d.on = on
	if on && has {
		d.ramp++
		d.level = onLevel
	}
	d.mu.Unlock()
	slog.Info("dimmer.set", slog.String("device", d.name), slog.Bool("on", on))
	d.notify()
}
