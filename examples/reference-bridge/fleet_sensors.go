// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package main

import (
	"log/slog"
	"sync"

	"github.com/SukramJ/go-fabric/contract"
)

// Sensors need no cluster server of their own: an endpoint whose Spec
// carries a Measurement gets its cluster and its device type from the
// declared [contract.MeasurementClass], as [demoThermometer] does. The two
// types below are that pattern made reusable — one for a scalar reading,
// one for a boolean — plus the push half ([notifier]) a subscribed
// controller depends on.

// demoReading is a scalar sensor reading in the model's own unit (% RH for
// humidity, m³/h for flow, … — see [contract.FloatMeasurementSource]).
type demoReading struct {
	name  string
	class contract.MeasurementClass
	notifier

	mu    sync.RWMutex
	value float64
}

var (
	_ contract.FloatMeasurementSource = (*demoReading)(nil)
	_ contract.ChangeNotifier         = (*demoReading)(nil)
)

func newDemoReading(name string, class contract.MeasurementClass, value float64) *demoReading {
	return &demoReading{name: name, class: class, value: value}
}

// MatterMeasurementClass implements [contract.MeasurementSource].
func (r *demoReading) MatterMeasurementClass() contract.MeasurementClass { return r.class }

// MatterFloatValue implements [contract.FloatMeasurementSource].
func (r *demoReading) MatterFloatValue() (float64, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.value, true
}

// reportFromDevice applies a new reading and pushes it to subscribers.
func (r *demoReading) reportFromDevice(value float64) {
	r.mu.Lock()
	r.value = value
	r.mu.Unlock()
	slog.Info("sensor.reported", slog.String("device", r.name), slog.Float64("value", value))
	r.notify()
}

// demoBinary is a boolean sensor: occupancy, contact, leak. Polarity is
// Matter's (true = occupied / contact closed), see
// [contract.BoolMeasurementSource].
type demoBinary struct {
	name  string
	class contract.MeasurementClass
	notifier

	mu    sync.RWMutex
	value bool
}

var (
	_ contract.BoolMeasurementSource = (*demoBinary)(nil)
	_ contract.ChangeNotifier        = (*demoBinary)(nil)
)

func newDemoBinary(name string, class contract.MeasurementClass, value bool) *demoBinary {
	return &demoBinary{name: name, class: class, value: value}
}

// MatterMeasurementClass implements [contract.MeasurementSource].
func (b *demoBinary) MatterMeasurementClass() contract.MeasurementClass { return b.class }

// MatterBoolValue implements [contract.BoolMeasurementSource].
func (b *demoBinary) MatterBoolValue() (value, observed bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.value, true
}

// reportFromDevice applies a new state and pushes it to subscribers.
func (b *demoBinary) reportFromDevice(value bool) {
	b.mu.Lock()
	b.value = value
	b.mu.Unlock()
	slog.Info("sensor.reported", slog.String("device", b.name), slog.Bool("value", value))
	b.notify()
}

// --- device: a wall button -------------------------------------------------

// demoButton is a momentary wall button. Its endpoint is assembled from the
// MomentarySwitch measurement class, which mounts the GenericSwitch (Switch,
// 0x003B) cluster; presses reach it as the Matter gesture sequence through
// the [contract.SwitchEventEmitter] the bridge hands over at assembly
// (WireMatterSwitchHandler).
//
// A press here has no physical button behind it: the debug control hook
// (control.go) narrates one on a test's behalf, exactly as a host narrates a
// press its southbound bus reported.
type demoButton struct {
	name string

	mu       sync.Mutex
	emitter  contract.SwitchEventEmitter
	position uint8
}

var _ contract.MeasurementSource = (*demoButton)(nil)

func newDemoButton(name string) *demoButton { return &demoButton{name: name} }

// MatterMeasurementClass implements [contract.MeasurementSource].
func (b *demoButton) MatterMeasurementClass() contract.MeasurementClass {
	return contract.MeasurementMomentarySwitch
}

// MatterSwitchPositions implements the GenericSwitch source port: idle and
// pressed.
func (b *demoButton) MatterSwitchPositions() uint8 { return 2 }

// MatterSwitchSupportsLongPress implements the GenericSwitch source port.
// The button tells a long press from a short one, so the cluster advertises
// MSL and forwards LongPress / LongRelease.
func (b *demoButton) MatterSwitchSupportsLongPress() bool { return true }

// MatterSwitchCurrentPosition implements the optional live-position port: 1
// while a press is held.
func (b *demoButton) MatterSwitchCurrentPosition() uint8 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.position
}

// WireMatterSwitchHandler is how the bridge hands the button its emitter at
// every reassembly; the returned closure detaches it again.
func (b *demoButton) WireMatterSwitchHandler(e contract.SwitchEventEmitter) func() {
	b.mu.Lock()
	b.emitter = e
	b.mu.Unlock()
	return func() {
		b.mu.Lock()
		if b.emitter == e {
			b.emitter = nil
		}
		b.mu.Unlock()
	}
}

// press narrates one press as Matter gestures: InitialPress, then
// ShortRelease — or InitialPress, LongPress, LongRelease for a long one.
// Mirrors matter.js SwitchServer.ts, which derives the same sequence from
// one currentPosition stream.
func (b *demoButton) press(long bool) bool {
	b.mu.Lock()
	e := b.emitter
	b.position = 1
	b.mu.Unlock()
	if e == nil {
		return false
	}
	e.FireInitialPress(1)
	if long {
		e.FireLongPress(1)
	}
	b.mu.Lock()
	b.position = 0
	b.mu.Unlock()
	if long {
		e.FireLongRelease(1)
	} else {
		e.FireShortRelease(1)
	}
	slog.Info("button.press", slog.String("device", b.name), slog.Bool("long", long))
	return true
}
