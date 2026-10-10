// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/SukramJ/go-fabric/cluster/boolcfg"
	"github.com/SukramJ/go-fabric/cluster/measurement"
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

// --- device: an air quality sensor -------------------------------------------

// demoAirQualitySensor is an AirQualitySensor (0x002C) carrying AirQuality,
// the cluster the device type mandates, and all ten concentration clusters
// it lists as optional. Each reading is in the model's unit for its class
// ([contract.FloatMeasurementSource]); AirQuality grades the CO2 reading.
// The endpoint is assembled from a Source rather than a Measurement
// because a measurement endpoint carries one class, and this one carries
// ten.
type demoAirQualitySensor struct {
	name     string
	readings []*demoReading
}

var _ contract.EndpointSource = (*demoAirQualitySensor)(nil)

func newDemoAirQualitySensor(name string) *demoAirQualitySensor {
	return &demoAirQualitySensor{name: name, readings: []*demoReading{
		newDemoReading(name+" CO2", contract.MeasurementCO2, 650),
		newDemoReading(name+" CO", contract.MeasurementCO, 0.5),
		newDemoReading(name+" NO2", contract.MeasurementNO2, 0.01),
		newDemoReading(name+" Ozone", contract.MeasurementOzone, 0.02),
		newDemoReading(name+" PM2.5", contract.MeasurementPM25, 8),
		newDemoReading(name+" Formaldehyde", contract.MeasurementFormaldehyde, 0.01),
		newDemoReading(name+" PM1", contract.MeasurementPM1, 5),
		newDemoReading(name+" PM10", contract.MeasurementPM10, 14),
		newDemoReading(name+" TVOC", contract.MeasurementTVOC, 0.1),
		newDemoReading(name+" Radon", contract.MeasurementRadon, 40),
	}}
}

// MatterDeviceType implements [contract.EndpointSource].
func (a *demoAirQualitySensor) MatterDeviceType() uint16 {
	return contract.MeasurementClassDeviceType(contract.MeasurementCO2)
}

// MatterClusterServers implements [contract.EndpointSource]: AirQuality
// first, then each concentration cluster as its class materialises it
// (the class's materialiser returns AirQuality and the concentration
// cluster; the AirQuality copies are dropped).
func (a *demoAirQualitySensor) MatterClusterServers() []contract.ClusterServer {
	servers := []contract.ClusterServer{measurement.NewAirQualityServer(contract.MeasurementCO2, a.readings[0])}
	for _, r := range a.readings {
		for _, srv := range measurement.FromMeasurementClass(r.class, r, contract.MeasurementContext{}) {
			if srv.MatterClusterID() != measurement.ClusterAirQuality {
				servers = append(servers, srv)
			}
		}
	}
	return servers
}

// --- device: a contact sensor with alarm configuration -----------------------

// demoContactSensor is a window contact: BooleanState from the binary
// reading, plus BooleanStateConfiguration (optional for ContactSensor) with
// a three-step sensitivity level and a visual alarm a controller can
// enable, disable and suppress. Its endpoint is assembled from a Source so
// it can carry the second cluster; the binary reading stays the notifier a
// subscribed controller's StateValue reports come from.
type demoContactSensor struct {
	*demoBinary
	config *boolcfg.Server
}

var _ contract.EndpointSource = (*demoContactSensor)(nil)

func newDemoContactSensor(name string) *demoContactSensor {
	cfg, err := boolcfg.New(boolcfg.Config{
		Features:                   boolcfg.FeatureSensitivityLevel | boolcfg.FeatureVisual | boolcfg.FeatureAlarmSuppress,
		Optional:                   boolcfg.OptionalDefaultSensitivityLevel | boolcfg.OptionalAlarmsEnabled,
		SupportedSensitivityLevels: 3,
		DefaultSensitivityLevel:    1,
		AlarmsSupported:            boolcfg.AlarmVisual,
		AlarmsEnabled:              boolcfg.AlarmVisual,
		Delegate:                   contactConfigLog{name: name},
	})
	if err != nil {
		panic(fmt.Sprintf("contact sensor BooleanStateConfiguration: %v", err))
	}
	return &demoContactSensor{demoBinary: newDemoBinary(name, contract.MeasurementContact, true), config: cfg}
}

// MatterDeviceType implements [contract.EndpointSource].
func (c *demoContactSensor) MatterDeviceType() uint16 {
	return contract.MeasurementClassDeviceType(contract.MeasurementContact)
}

// MatterClusterServers implements [contract.EndpointSource].
func (c *demoContactSensor) MatterClusterServers() []contract.ClusterServer {
	return []contract.ClusterServer{measurement.NewBooleanStateServer(c.demoBinary), c.config}
}

// contactConfigLog is the contact sensor's side of a controller's
// configuration change: the daemon has no device to tell, so it logs it.
type contactConfigLog struct{ name string }

func (l contactConfigLog) CurrentSensitivityLevelChanged(_ context.Context, level uint8) error {
	slog.Info("contact.sensitivity", slog.String("device", l.name), slog.Int("level", int(level)))
	return nil
}

func (l contactConfigLog) AlarmsEnabledChanged(_ context.Context, enabled boolcfg.AlarmMode) error {
	slog.Info("contact.alarms_enabled", slog.String("device", l.name), slog.Int("alarms", int(enabled)))
	return nil
}

func (l contactConfigLog) AlarmsActiveChanged(_ context.Context, active boolcfg.AlarmMode) error {
	slog.Info("contact.alarms_active", slog.String("device", l.name), slog.Int("alarms", int(active)))
	return nil
}

func (l contactConfigLog) AlarmsSuppressedChanged(_ context.Context, suppressed boolcfg.AlarmMode) error {
	slog.Info("contact.alarms_suppressed", slog.String("device", l.name), slog.Int("alarms", int(suppressed)))
	return nil
}
