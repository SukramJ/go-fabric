// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/cluster/boolcfg"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/measurement"
	soildef "github.com/SukramJ/go-fabric/cluster/spec/soilmeasurement"
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

// demoButton is a momentary wall button that knows its position: idle (0)
// or pressed (1). Its endpoint is assembled from the MomentarySwitch
// measurement class, which mounts the Switch server (cluster/switch, the
// port of matter.js SwitchServer.ts) with MS, MSR, MSL and MSM; the button
// reports each position through the [contract.SwitchPositionSink] the
// bridge hands over at assembly (WireMatterSwitchPositions), and the server
// derives every event — InitialPress, ShortRelease, LongPress, LongRelease,
// MultiPressOngoing, MultiPressComplete — from that stream.
//
// A press here has no physical button behind it: the debug control hook
// (control.go) moves the button through positions on a test's behalf, the
// way matter.js's own test app does (support/chip-testing/src/simulators/
// SwitchSimulator.ts).
type demoButton struct {
	name string

	mu   sync.Mutex
	sink contract.SwitchPositionSink

	// running is held while one simulated action sequence runs; a second
	// one is refused, as SwitchSimulator.executeActions refuses one while
	// actions are pending (SwitchSimulator.ts:25-32).
	running sync.Mutex
}

var _ contract.MeasurementSource = (*demoButton)(nil)

func newDemoButton(name string) *demoButton { return &demoButton{name: name} }

// demoButtonMultiPressMax is the button's MultiPressMax: three presses
// counted, a fourth aborts the sequence (TC-SWTCH-2.5 step 6b runs with it).
const demoButtonMultiPressMax = 3

// MatterMeasurementClass implements [contract.MeasurementSource].
func (b *demoButton) MatterMeasurementClass() contract.MeasurementClass {
	return contract.MeasurementMomentarySwitch
}

// MatterSwitchPositions implements the GenericSwitch source port: idle and
// pressed.
func (b *demoButton) MatterSwitchPositions() uint8 { return 2 }

// MatterSwitchSupportsLongPress implements the GenericSwitch source port:
// the server tells a long press from a short one (MSL).
func (b *demoButton) MatterSwitchSupportsLongPress() bool { return true }

// MatterSwitchMultiPressMax implements the optional multi-press port: the
// server counts presses up to three (MSM).
func (b *demoButton) MatterSwitchMultiPressMax() uint8 { return demoButtonMultiPressMax }

// WireMatterSwitchPositions is how the bridge hands the button the Switch
// server's position input at every reassembly; the returned closure
// detaches it again.
func (b *demoButton) WireMatterSwitchPositions(sink contract.SwitchPositionSink) func() {
	b.mu.Lock()
	b.sink = sink
	b.mu.Unlock()
	return func() {
		b.mu.Lock()
		if b.sink == sink {
			b.sink = nil
		}
		b.mu.Unlock()
	}
}

// switchTuner is the part of the Switch server (cluster/switch.Server)
// beyond the position input that the simulation sets, as matter.js's
// SwitchSimulator sets the server's state before it acts.
type switchTuner interface {
	SetLongPressDelay(time.Duration)
	SetMultiPressDelay(time.Duration)
	ResetState()
}

// switchAction is one step of a simulated sequence: move to position, then
// wait delay (SwitchSimulator.ts:17).
type switchAction struct {
	position uint8
	delay    time.Duration
}

// errButtonNotMounted reports a button whose endpoint is not assembled yet.
var errButtonNotMounted = errors.New("the button has no position input yet (not mounted)")

// execute moves the button through actions, in order, each followed by its
// delay (SwitchSimulator.ts:25-65).
func (b *demoButton) execute(tune func(switchTuner), actions []switchAction) error {
	if !b.running.TryLock() {
		return errors.New("still unprocessed switch actions")
	}
	defer b.running.Unlock()
	b.mu.Lock()
	sink := b.sink
	b.mu.Unlock()
	if sink == nil {
		return errButtonNotMounted
	}
	if t, ok := sink.(switchTuner); ok && tune != nil {
		tune(t)
	}
	for _, a := range actions {
		if err := sink.SetCurrentPosition(a.position); err != nil {
			return err
		}
		if a.delay > 0 {
			time.Sleep(a.delay)
		}
	}
	return nil
}

// simulateLongPress holds the button at position for duration, after
// setting the long-press delay to delay (0 keeps it). Mirrors matter.js
// SwitchSimulator.simulateLongPress (SwitchSimulator.ts:81-92).
func (b *demoButton) simulateLongPress(position uint8, delay, duration time.Duration) error {
	err := b.execute(func(t switchTuner) {
		if delay > 0 {
			t.SetLongPressDelay(delay)
		}
	}, []switchAction{{position: position, delay: duration}, {position: 0}})
	if err == nil {
		slog.Info("button.press", slog.String("device", b.name), slog.Bool("long", true))
	}
	return err
}

// simulateMultiPress presses the button n times, pressed for pressed and
// released for released between the presses, after setting the
// multi-press delay to released + 500 ms. Mirrors matter.js
// SwitchSimulator.simulateMultiPress (SwitchSimulator.ts:111-139).
func (b *demoButton) simulateMultiPress(position uint8, n int, pressed, released time.Duration) error {
	actions := make([]switchAction, 0, 2*n)
	for i := range n {
		actions = append(actions, switchAction{position: position, delay: pressed})
		if i < n-1 {
			actions = append(actions, switchAction{position: 0, delay: released})
		}
	}
	actions = append(actions, switchAction{position: 0})
	err := b.execute(func(t switchTuner) { t.SetMultiPressDelay(released + 500*time.Millisecond) }, actions)
	if err == nil {
		slog.Info("button.press", slog.String("device", b.name), slog.Int("presses", n))
	}
	return err
}

// simulateIdle returns the button to its idle position, then resets the
// server's press state. Mirrors matter.js support/chip-testing/src/
// AllClustersTestInstance.ts:179-185 (simulateSwitchIdle).
func (b *demoButton) simulateIdle() error {
	if err := b.execute(nil, []switchAction{{position: 0}}); err != nil {
		return err
	}
	b.mu.Lock()
	sink := b.sink
	b.mu.Unlock()
	if t, ok := sink.(switchTuner); ok {
		t.ResetState()
	}
	return nil
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

// servers are the cluster servers the device supplies: AirQuality
// first, then each concentration cluster as its class materialises it
// (the class's materialiser returns AirQuality and the concentration
// cluster; the AirQuality copies are dropped), then EcosystemInformation
// (BridgedNode: optional) with the demo directory that names this sensor
// (fleet_node.go).
func (a *demoAirQualitySensor) servers() []contract.ClusterServer {
	servers := []contract.ClusterServer{measurement.NewAirQualityServer(contract.MeasurementCO2, a.readings[0])}
	for _, r := range a.readings {
		for _, srv := range measurement.FromMeasurementClass(r.class, r, contract.MeasurementContext{}) {
			if srv.MatterClusterID() != measurement.ClusterAirQuality {
				servers = append(servers, srv)
			}
		}
	}
	return append(servers, mattercore.NewEcosystemInformation(demoEcosystem{name: a.name}))
}

// --- device: a soil moisture sensor -------------------------------------------

// demoSoilLimits are the limits connectedhomeip's all-clusters app gives
// its soil sensor: 0 to 100 %, one accuracy range over the whole span
// with PercentMax 10 (examples/all-clusters-app/all-clusters-common/src/
// soil-measurement-stub.cpp:30-41 at the harness pin 6170af84), the shape
// TC_SOIL_2_1.py checks (steps 3-10).
func demoSoilLimits() soildef.MeasurementAccuracyStruct {
	percentMax := uint16(10)
	return soildef.MeasurementAccuracyStruct{
		MeasurementType:  soildef.MeasurementTypeSoilMoisture,
		Measured:         true,
		MinMeasuredValue: 0,
		MaxMeasuredValue: 100,
		AccuracyRanges:   []soildef.MeasurementAccuracyRangeStruct{{RangeMin: 0, RangeMax: 100, PercentMax: &percentMax}},
	}
}

// demoSoilSensor is a SoilSensor (0x0045): a soil moisture reading in
// percent, nullable, with fixed limits ([measurement.SoilMoistureSource]).
// It starts at the minimum, as the all-clusters app sets it
// (soil-measurement-stub.cpp:67-68); the app pipe's
// SetSimulatedSoilMoisture moves it (control_actions.go).
type demoSoilSensor struct {
	name string
	notifier

	mu    sync.RWMutex
	value *uint8
}

var (
	_ measurement.SoilMoistureSource = (*demoSoilSensor)(nil)
	_ contract.ChangeNotifier        = (*demoSoilSensor)(nil)
)

func newDemoSoilSensor(name string) *demoSoilSensor {
	initial := uint8(0) // demoSoilLimits' MinMeasuredValue
	return &demoSoilSensor{name: name, value: &initial}
}

// MatterMeasurementClass implements [contract.MeasurementSource].
func (*demoSoilSensor) MatterMeasurementClass() contract.MeasurementClass {
	return contract.MeasurementSoilMoisture
}

// MatterFloatValue implements [contract.FloatMeasurementSource]; a null
// reading is unobserved.
func (s *demoSoilSensor) MatterFloatValue() (float64, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.value == nil {
		return 0, false
	}
	return float64(*s.value), true
}

// MatterSoilMoistureLimits implements [measurement.SoilMoistureSource].
func (*demoSoilSensor) MatterSoilMoistureLimits() soildef.MeasurementAccuracyStruct {
	return demoSoilLimits()
}

// reportFromDevice applies a new reading (nil for null) and pushes it to
// subscribers.
func (s *demoSoilSensor) reportFromDevice(value *uint8) {
	s.mu.Lock()
	s.value = value
	s.mu.Unlock()
	if value == nil {
		slog.Info("sensor.reported", slog.String("device", s.name), slog.String("value", "null"))
	} else {
		slog.Info("sensor.reported", slog.String("device", s.name), slog.Int("value", int(*value)))
	}
	s.notify()
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

// servers are the cluster servers the device supplies.
func (c *demoContactSensor) servers() []contract.ClusterServer {
	return []contract.ClusterServer{measurement.NewBooleanStateServer(c.demoBinary), c.config}
}

// The BooleanStateConfiguration test event triggers (connectedhomeip
// src/app/clusters/boolean-state-configuration-server/
// BooleanStateConfigurationTestEventTriggerHandler.h:25-29). The handler
// clears the endpoint bits first (.cpp:26, clearEndpointInEventTrigger),
// the same mask the SmokeCoAlarm triggers use here (control.go
// smokeTriggerMask).
const (
	triggerBoolCfgSensorTrigger   uint64 = 0x0080_0000_0000_0000
	triggerBoolCfgSensorUntrigger uint64 = 0x0080_0000_0000_0001
)

// testEventTrigger handles the BooleanStateConfiguration triggers as the
// all-clusters app's HandleBooleanStateConfigurationTestEventTrigger does
// (examples/all-clusters-app/all-clusters-common/src/boolcfg-stub.cpp:
// 27-49): SensorTrigger raises every enabled alarm
// (SetAllEnabledAlarmsActive, :35), SensorUntrigger clears the active and
// suppressed alarms (ClearAllAlarms, :40). The app ignores the result of
// either call (TEMPORARY_RETURN_IGNORED) and reports the trigger handled;
// so does this. False for a trigger that is not one of them.
func (c *demoContactSensor) testEventTrigger(trigger uint64) bool {
	switch trigger & smokeTriggerMask {
	case triggerBoolCfgSensorTrigger:
		if err := c.config.SetAllEnabledAlarmsActive(); err != nil {
			slog.Warn("contact.trigger", slog.String("device", c.name), slog.Any("err", err))
		}
	case triggerBoolCfgSensorUntrigger:
		c.config.ClearAllAlarms()
	default:
		return false
	}
	return true
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
