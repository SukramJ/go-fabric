// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/cluster/levelcontrol"
	levelcontroldef "github.com/SukramJ/go-fabric/cluster/spec/levelcontrol"
	onoffdef "github.com/SukramJ/go-fabric/cluster/spec/onoff"
	"github.com/SukramJ/go-fabric/device"
)

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
// families are read against — so it lives with its test.
type nativeDimmer struct {
	name string
	notifier
	// src is the LevelControl source over the lamp.
	src *levelcontrol.NativeSource

	mu    sync.Mutex
	on    bool
	level uint8
	// ramp counts the ramps; a step of a ramp a later one or a Stop
	// replaced does nothing.
	ramp uint64
	// onOff is the light's OnOff server; nil before [nativeDimmer.device].
	onOff *onoffdef.Server
}

// nativeRampStep is the step of the simulated ramp.
const nativeRampStep = 100 * time.Millisecond

// Compile-time assertions.
var (
	_ levelcontroldef.NativeRamp = (*nativeDimmer)(nil)
	_ levelcontrol.OnOff         = (*nativeDimmer)(nil)
)

// newDemoNativeDimmer returns a dimmable light that is off at half
// brightness.
func newDemoNativeDimmer(name string) *nativeDimmer {
	d := &nativeDimmer{name: name, level: 127}
	d.src = levelcontrol.NewNativeSource(d, d, levelcontrol.NativeConfig{Lighting: true})
	return d
}

// device builds the DimmableLight with the light's LevelControl, Lighting
// feature on (DimmableLight requires LT on LevelControl,
// dimmable-light.element.ts:19-33), as the host's server; OnOff is the
// device layer's default server, followed so a switch-on applies OnLevel
// as a dimmer's does.
func (d *nativeDimmer) device(opts device.Options) (*device.Device, error) {
	lvl := levelcontrol.NewServer(levelcontrol.Config{Source: d.src, Lighting: true})
	l, err := device.NewDimmableLight(opts, levelcontroldef.Declare{HostServer: lvl}, device.WithChangeNotifier(d))
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

// Level implements [levelcontroldef.NativeRamp].
func (d *nativeDimmer) Level() (uint8, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.level, true
}

// Ramp implements [levelcontroldef.NativeRamp]: the simulated lamp moves to
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

// Stop implements [levelcontroldef.NativeRamp]: the lamp stays where it is.
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
	if srv != nil && srv.Get().OnOff != on {
		_ = srv.Set(onoffdef.Patch{OnOff: new(on)})
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
