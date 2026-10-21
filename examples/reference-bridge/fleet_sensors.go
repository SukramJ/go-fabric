// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"sync"
	"time"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	aqdef "github.com/SukramJ/go-fabric/cluster/spec/airquality"
	bsdef "github.com/SukramJ/go-fabric/cluster/spec/booleanstate"
	bscdef "github.com/SukramJ/go-fabric/cluster/spec/booleanstateconfiguration"
	co2def "github.com/SukramJ/go-fabric/cluster/spec/carbondioxideconcentrationmeasurement"
	codef "github.com/SukramJ/go-fabric/cluster/spec/carbonmonoxideconcentrationmeasurement"
	ecodef "github.com/SukramJ/go-fabric/cluster/spec/ecosysteminformation"
	fmdef "github.com/SukramJ/go-fabric/cluster/spec/flowmeasurement"
	hchodef "github.com/SukramJ/go-fabric/cluster/spec/formaldehydeconcentrationmeasurement"
	no2def "github.com/SukramJ/go-fabric/cluster/spec/nitrogendioxideconcentrationmeasurement"
	osdef "github.com/SukramJ/go-fabric/cluster/spec/occupancysensing"
	o3def "github.com/SukramJ/go-fabric/cluster/spec/ozoneconcentrationmeasurement"
	pm10def "github.com/SukramJ/go-fabric/cluster/spec/pm10concentrationmeasurement"
	pm1def "github.com/SukramJ/go-fabric/cluster/spec/pm1concentrationmeasurement"
	pm25def "github.com/SukramJ/go-fabric/cluster/spec/pm25concentrationmeasurement"
	psdef "github.com/SukramJ/go-fabric/cluster/spec/powersource"
	radondef "github.com/SukramJ/go-fabric/cluster/spec/radonconcentrationmeasurement"
	rhdef "github.com/SukramJ/go-fabric/cluster/spec/relativehumiditymeasurement"
	soildef "github.com/SukramJ/go-fabric/cluster/spec/soilmeasurement"
	swdef "github.com/SukramJ/go-fabric/cluster/spec/switchcluster"
	tmdef "github.com/SukramJ/go-fabric/cluster/spec/temperaturemeasurement"
	tvocdef "github.com/SukramJ/go-fabric/cluster/spec/totalvolatileorganiccompoundsconcentrationmeasurement"
	"github.com/SukramJ/go-fabric/device"
)

// Sensors declare their measurement cluster like any other cluster: the
// value the sensor starts at is the declaration's Initial, and a new
// reading reaches the cluster through the device's typed Set, which
// reports it — a matter.js host setting the measurement behavior's state
// (packages/node/src/behavior/state/managed/Datasource.ts:261-264). The
// cluster's units are the host's to convert to; the helpers below convert
// as the module's measurement servers do for a host that hands over a
// live reading instead (cluster/measurement).

// demoReading is a scalar sensor reading in the model's own unit (% RH for
// humidity, m³/h for flow, …). publish sets it on the device's
// measurement cluster, in the cluster's unit; nil before the device is
// built.
type demoReading struct {
	name string

	mu      sync.RWMutex
	value   float64
	publish func(float64) error
}

func newDemoReading(name string, value float64) *demoReading {
	return &demoReading{name: name, value: value}
}

// reading is the current reading.
func (r *demoReading) reading() float64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.value
}

// bind makes publish where the reading goes.
func (r *demoReading) bind(publish func(float64) error) {
	r.mu.Lock()
	r.publish = publish
	r.mu.Unlock()
}

// reportFromDevice applies a new reading and pushes it to subscribers.
func (r *demoReading) reportFromDevice(value float64) {
	r.mu.Lock()
	r.value = value
	publish := r.publish
	r.mu.Unlock()
	slog.Info("sensor.reported", slog.String("device", r.name), slog.Float64("value", value))
	if publish != nil {
		if err := publish(value); err != nil {
			slog.Warn("sensor.publish", slog.String("device", r.name), slog.Any("err", err))
		}
	}
}

// scaled is v in a cluster's integer unit: v·factor rounded half away
// from zero, clamped to [lo, hi]; NaN is lo (cluster/measurement
// celsiusToInt16, humidityToUint16, flowToMatter).
func scaled(v, factor float64, lo, hi int64) int64 {
	if math.IsNaN(v) {
		return lo
	}
	return min(max(int64(math.Round(v*factor)), lo), hi)
}

// The measurement clusters' fixed ranges, as the module's measurement
// servers report them: TemperatureMeasurement −273.15 to 327.66 °C,
// RelativeHumidityMeasurement 0 to 100 %, FlowMeasurement 0 to 6553.4
// m³/h, each with Tolerance 0.
const (
	temperatureMin, temperatureMax int16  = -27315, 32766
	humidityMax                    uint16 = 10000
	flowMax                        uint16 = 65534
)

// temperatureDeclaration is a TemperatureMeasurement reading celsius.
func temperatureDeclaration(celsius float64) device.Declaration {
	v := int16(scaled(celsius, 100, int64(temperatureMin), int64(temperatureMax))) //nolint:gosec // clamped to int16
	lo, hi, tolerance := temperatureMin, temperatureMax, uint16(0)
	return tmdef.Declare{Initial: tmdef.Patch{MeasuredValue: &v, MinMeasuredValue: &lo, MaxMeasuredValue: &hi, Tolerance: &tolerance}}
}

// humidity is a RelativeHumidityMeasurement value of r (% RH × 100).
func humidity(r float64) uint16 { return uint16(scaled(r, 100, 0, int64(humidityMax))) } //nolint:gosec // clamped to uint16

// flow is a FlowMeasurement value of r (m³/h × 10).
func flow(r float64) uint16 { return uint16(scaled(r, 10, 0, int64(flowMax))) } //nolint:gosec // clamped to uint16

// humidityDeclaration is a RelativeHumidityMeasurement starting at r.
func (r *demoReading) humidityDeclaration() device.Declaration {
	v, lo, hi, tolerance := humidity(r.reading()), uint16(0), humidityMax, uint16(0)
	return rhdef.Declare{Initial: rhdef.Patch{MeasuredValue: &v, MinMeasuredValue: &lo, MaxMeasuredValue: &hi, Tolerance: &tolerance}}
}

// flowDeclaration is a FlowMeasurement starting at r.
func (r *demoReading) flowDeclaration() device.Declaration {
	v, lo, hi, tolerance := flow(r.reading()), uint16(0), flowMax, uint16(0)
	return fmdef.Declare{Initial: fmdef.Patch{MeasuredValue: &v, MinMeasuredValue: &lo, MaxMeasuredValue: &hi, Tolerance: &tolerance}}
}

// humiditySensor builds the HumiditySensor the reading drives.
func (r *demoReading) humiditySensor(opts device.Options) (*device.HumiditySensor, error) {
	d, err := device.NewHumiditySensor(opts, r.humidityDeclaration())
	if err != nil {
		return nil, err
	}
	srv := d.RelativeHumidityMeasurement()
	r.bind(func(v float64) error {
		h := humidity(v)
		return srv.Set(rhdef.Patch{MeasuredValue: &h})
	})
	return d, nil
}

// flowSensor builds the FlowSensor the reading drives.
func (r *demoReading) flowSensor(opts device.Options) (*device.FlowSensor, error) {
	d, err := device.NewFlowSensor(opts, r.flowDeclaration())
	if err != nil {
		return nil, err
	}
	srv := d.FlowMeasurement()
	r.bind(func(v float64) error {
		f := flow(v)
		return srv.Set(fmdef.Patch{MeasuredValue: &f})
	})
	return d, nil
}

// demoBinary is a boolean sensor: occupancy, contact, leak. Polarity is
// Matter's (true = occupied / contact closed). publish sets it on the
// device's cluster; nil before the device is built.
type demoBinary struct {
	name string

	mu      sync.RWMutex
	value   bool
	publish func(bool) error
}

func newDemoBinary(name string, value bool) *demoBinary {
	return &demoBinary{name: name, value: value}
}

// state is the current state.
func (b *demoBinary) state() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.value
}

// bind makes publish where the state goes.
func (b *demoBinary) bind(publish func(bool) error) {
	b.mu.Lock()
	b.publish = publish
	b.mu.Unlock()
}

// reportFromDevice applies a new state and pushes it to subscribers.
func (b *demoBinary) reportFromDevice(value bool) {
	b.mu.Lock()
	b.value = value
	publish := b.publish
	b.mu.Unlock()
	slog.Info("sensor.reported", slog.String("device", b.name), slog.Bool("value", value))
	if publish != nil {
		if err := publish(value); err != nil {
			slog.Warn("sensor.publish", slog.String("device", b.name), slog.Any("err", err))
		}
	}
}

// occupancyOf is the Occupancy bitmap of an occupied flag.
func occupancyOf(occupied bool) osdef.OccupancyBitmap {
	if occupied {
		return osdef.OccupancyOccupied
	}
	return 0
}

// occupancySensor builds the OccupancySensor the state drives: a PIR
// sensor (OccupancySensorType PIR, the bitmap's PIR bit).
func (b *demoBinary) occupancySensor(opts device.Options) (*device.OccupancySensor, error) {
	occupied := occupancyOf(b.state())
	kind, bitmap := osdef.OccupancySensorTypeEnumPir, osdef.OccupancySensorTypeBitmapPir
	d, err := device.NewOccupancySensor(opts, osdef.Declare{
		Features: osdef.FeaturePassiveInfrared,
		Initial:  osdef.Patch{Occupancy: &occupied, OccupancySensorType: &kind, OccupancySensorTypeBitmap: &bitmap},
	})
	if err != nil {
		return nil, err
	}
	srv := d.OccupancySensing()
	b.bind(func(v bool) error {
		o := occupancyOf(v)
		return srv.Set(osdef.Patch{Occupancy: &o})
	})
	return d, nil
}

// --- device: a wall button -------------------------------------------------

// demoButton is a momentary wall button that knows its position: idle (0)
// or pressed (1). Its GenericSwitch is the device kit's Switch server
// (cluster/switch, the port of matter.js SwitchServer.ts) with MS, MSR,
// MSL and MSM; the button reports each position to it, and the server
// derives every event — InitialPress, ShortRelease, LongPress, LongRelease,
// MultiPressOngoing, MultiPressComplete — from that stream.
//
// A press here has no physical button behind it: the debug control hook
// (control.go) moves the button through positions on a test's behalf, the
// way matter.js's own test app does (support/chip-testing/src/simulators/
// SwitchSimulator.ts).
type demoButton struct {
	name string

	mu  sync.Mutex
	srv switchServer

	// running is held while one simulated action sequence runs; a second
	// one is refused, as SwitchSimulator.executeActions refuses one while
	// actions are pending (SwitchSimulator.ts:25-32).
	running sync.Mutex
}

func newDemoButton(name string) *demoButton { return &demoButton{name: name} }

// demoButtonMultiPressMax is the button's MultiPressMax: three presses
// counted, a fourth aborts the sequence (TC-SWTCH-2.5 step 6b runs with it).
const demoButtonMultiPressMax = 3

// switchServer is the part of the kit's Switch server the simulation
// drives, as matter.js's SwitchSimulator sets the server's state before it
// acts: the position the button reports (a write of CurrentPosition the
// host debounced itself) and the timing of the press logic.
type switchServer interface {
	SetCurrentPosition(position uint8) error
	SetLongPressDelay(time.Duration)
	SetMultiPressDelay(time.Duration)
	ResetState()
}

// switchFeatures are the button's: momentary, with release, long press
// and multi-press.
const switchFeatures = swdef.FeatureMomentarySwitch | swdef.FeatureMomentarySwitchRelease |
	swdef.FeatureMomentarySwitchLongPress | swdef.FeatureMomentarySwitchMultiPress

// device builds the GenericSwitch: two positions, idle and pressed.
func (b *demoButton) device(opts device.Options) (*device.GenericSwitch, error) {
	positions, multi := uint8(2), uint8(demoButtonMultiPressMax)
	d, err := device.NewGenericSwitch(opts, swdef.Declare{
		Features: switchFeatures,
		Initial:  swdef.Patch{NumberOfPositions: &positions, MultiPressMax: &multi},
	})
	if err != nil {
		return nil, err
	}
	srv, ok := d.Server(swdef.ClusterID).(switchServer)
	if !ok {
		return nil, errors.New("the button's Switch server takes no positions")
	}
	b.mu.Lock()
	b.srv = srv
	b.mu.Unlock()
	return d, nil
}

// switchAction is one step of a simulated sequence: move to position, then
// wait delay (SwitchSimulator.ts:17).
type switchAction struct {
	position uint8
	delay    time.Duration
}

// errButtonNotMounted reports a button whose device is not built yet.
var errButtonNotMounted = errors.New("the button has no Switch server yet (not built)")

// execute moves the button through actions, in order, each followed by its
// delay (SwitchSimulator.ts:25-65).
func (b *demoButton) execute(tune func(switchServer), actions []switchAction) error {
	if !b.running.TryLock() {
		return errors.New("still unprocessed switch actions")
	}
	defer b.running.Unlock()
	b.mu.Lock()
	srv := b.srv
	b.mu.Unlock()
	if srv == nil {
		return errButtonNotMounted
	}
	if tune != nil {
		tune(srv)
	}
	for _, a := range actions {
		if err := srv.SetCurrentPosition(a.position); err != nil {
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
	err := b.execute(func(t switchServer) {
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
	err := b.execute(func(t switchServer) { t.SetMultiPressDelay(released + 500*time.Millisecond) }, actions)
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
	srv := b.srv
	b.mu.Unlock()
	srv.ResetState()
	return nil
}

// --- device: an air quality sensor -------------------------------------------

// demoAirQualitySensor is an AirQualitySensor (0x002C) carrying AirQuality,
// the cluster the device type mandates, and all ten concentration clusters
// it lists as optional, each with a fixed reading in its cluster's unit;
// AirQuality grades the CO2 reading (Good up to 1000 ppm, as the module's
// AirQuality server grades it, cluster/measurement airQualityGoodBelow).
type demoAirQualitySensor struct{ name string }

func newDemoAirQualitySensor(name string) *demoAirQualitySensor {
	return &demoAirQualitySensor{name: name}
}

// The air quality sensor's readings. Each concentration cluster serves
// NumericMeasurement (MEA) over 0 to 100000 in the air.
const (
	co2PPM, coPPM, no2PPM, ozonePPM     float32 = 650, 0.5, 0.01, 0.02
	pm25UG, pm1UG, pm10UG               float32 = 8, 5, 14
	formaldehydePPM, tvocPPM, radonBQM3 float32 = 0.01, 0.1, 40
	concentrationMin, concentrationMax  float32 = 0, 100000
	co2GoodBelow                        float32 = 1000
)

// declarations declare the sensor's clusters: AirQuality, the ten
// concentrations, and EcosystemInformation (BridgedNode: optional) with
// the demo directory that names this sensor (fleet_node.go).
func (a *demoAirQualitySensor) declarations() []device.Option {
	quality := aqdef.AirQualityPoor
	if co2PPM <= co2GoodBelow {
		quality = aqdef.AirQualityGood
	}
	v := func(f float32) *float32 { return &f }
	lo, hi := v(concentrationMin), v(concentrationMax)
	return []device.Option{
		aqdef.Declare{Initial: aqdef.Patch{AirQuality: &quality}},
		co2def.Declare{Features: co2def.FeatureNumericMeasurement, Initial: co2def.Patch{
			MeasuredValue: v(co2PPM), MinMeasuredValue: lo, MaxMeasuredValue: hi,
			MeasurementUnit: new(co2def.MeasurementUnitPpm), MeasurementMedium: new(co2def.MeasurementMediumAir),
		}},
		codef.Declare{Features: codef.FeatureNumericMeasurement, Initial: codef.Patch{
			MeasuredValue: v(coPPM), MinMeasuredValue: lo, MaxMeasuredValue: hi,
			MeasurementUnit: new(codef.MeasurementUnitPpm), MeasurementMedium: new(codef.MeasurementMediumAir),
		}},
		no2def.Declare{Features: no2def.FeatureNumericMeasurement, Initial: no2def.Patch{
			MeasuredValue: v(no2PPM), MinMeasuredValue: lo, MaxMeasuredValue: hi,
			MeasurementUnit: new(no2def.MeasurementUnitPpm), MeasurementMedium: new(no2def.MeasurementMediumAir),
		}},
		o3def.Declare{Features: o3def.FeatureNumericMeasurement, Initial: o3def.Patch{
			MeasuredValue: v(ozonePPM), MinMeasuredValue: lo, MaxMeasuredValue: hi,
			MeasurementUnit: new(o3def.MeasurementUnitPpm), MeasurementMedium: new(o3def.MeasurementMediumAir),
		}},
		pm25def.Declare{Features: pm25def.FeatureNumericMeasurement, Initial: pm25def.Patch{
			MeasuredValue: v(pm25UG), MinMeasuredValue: lo, MaxMeasuredValue: hi,
			MeasurementUnit: new(pm25def.MeasurementUnitUgm3), MeasurementMedium: new(pm25def.MeasurementMediumAir),
		}},
		hchodef.Declare{Features: hchodef.FeatureNumericMeasurement, Initial: hchodef.Patch{
			MeasuredValue: v(formaldehydePPM), MinMeasuredValue: lo, MaxMeasuredValue: hi,
			MeasurementUnit: new(hchodef.MeasurementUnitPpm), MeasurementMedium: new(hchodef.MeasurementMediumAir),
		}},
		pm1def.Declare{Features: pm1def.FeatureNumericMeasurement, Initial: pm1def.Patch{
			MeasuredValue: v(pm1UG), MinMeasuredValue: lo, MaxMeasuredValue: hi,
			MeasurementUnit: new(pm1def.MeasurementUnitUgm3), MeasurementMedium: new(pm1def.MeasurementMediumAir),
		}},
		pm10def.Declare{Features: pm10def.FeatureNumericMeasurement, Initial: pm10def.Patch{
			MeasuredValue: v(pm10UG), MinMeasuredValue: lo, MaxMeasuredValue: hi,
			MeasurementUnit: new(pm10def.MeasurementUnitUgm3), MeasurementMedium: new(pm10def.MeasurementMediumAir),
		}},
		tvocdef.Declare{Features: tvocdef.FeatureNumericMeasurement, Initial: tvocdef.Patch{
			MeasuredValue: v(tvocPPM), MinMeasuredValue: lo, MaxMeasuredValue: hi,
			MeasurementUnit: new(tvocdef.MeasurementUnitPpm), MeasurementMedium: new(tvocdef.MeasurementMediumAir),
		}},
		radondef.Declare{Features: radondef.FeatureNumericMeasurement, Initial: radondef.Patch{
			MeasuredValue: v(radonBQM3), MinMeasuredValue: lo, MaxMeasuredValue: hi,
			MeasurementUnit: new(radondef.MeasurementUnitBqm3), MeasurementMedium: new(radondef.MeasurementMediumAir),
		}},
		ecodef.Declare{HostServer: mattercore.NewEcosystemInformation(demoEcosystem{name: a.name})},
	}
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

// demoSoilSensor is a SoilSensor: a soil moisture reading in percent,
// nullable, within fixed limits. It starts at the minimum, as the
// all-clusters app sets it (soil-measurement-stub.cpp:67-68); the app
// pipe's SetSimulatedSoilMoisture moves it (control_actions.go).
type demoSoilSensor struct {
	name string

	mu    sync.RWMutex
	value *uint8
	srv   *soildef.Server
}

func newDemoSoilSensor(name string) *demoSoilSensor {
	initial := uint8(0) // demoSoilLimits' MinMeasuredValue
	return &demoSoilSensor{name: name, value: &initial}
}

// device builds the SoilSensor.
func (s *demoSoilSensor) device(opts device.Options) (*device.SoilSensor, error) {
	limits := demoSoilLimits()
	s.mu.RLock()
	value := s.value
	s.mu.RUnlock()
	d, err := device.NewSoilSensor(opts, soildef.Declare{
		Initial: soildef.Patch{SoilMoistureMeasurementLimits: &limits, SoilMoistureMeasuredValue: value},
	})
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.srv = d.SoilMeasurement()
	s.mu.Unlock()
	return d, nil
}

// reportFromDevice applies a new reading (nil for null) and pushes it to
// subscribers. A reading outside the limits is ignored, as the module's
// soil server ignores it (cluster/measurement soil.go measured): the
// sensor keeps the reading it had.
func (s *demoSoilSensor) reportFromDevice(value *uint8) {
	limits := demoSoilLimits()
	if value != nil && (int64(*value) < limits.MinMeasuredValue || int64(*value) > limits.MaxMeasuredValue) {
		slog.Info("sensor.ignored", slog.String("device", s.name), slog.Int("value", int(*value)))
		return
	}
	s.mu.Lock()
	s.value = value
	srv := s.srv
	s.mu.Unlock()
	patch := soildef.Patch{SoilMoistureMeasuredValue: value}
	if value == nil {
		patch.Null = []uint32{soildef.AttrSoilMoistureMeasuredValue}
		slog.Info("sensor.reported", slog.String("device", s.name), slog.String("value", "null"))
	} else {
		slog.Info("sensor.reported", slog.String("device", s.name), slog.Int("value", int(*value)))
	}
	if srv != nil {
		if err := srv.Set(patch); err != nil {
			slog.Warn("sensor.publish", slog.String("device", s.name), slog.Any("err", err))
		}
	}
}

// --- device: a contact sensor with alarm configuration -----------------------

// demoContactSensor is a window contact: BooleanState, which the device
// kit's server emits StateChange for on every change (matter.js
// BooleanStateServer), plus BooleanStateConfiguration (optional for
// ContactSensor) with a three-step sensitivity level and a visual alarm a
// controller can enable, disable and suppress.
type demoContactSensor struct {
	*demoBinary
	config *bscdef.Server
}

func newDemoContactSensor(name string) *demoContactSensor {
	return &demoContactSensor{demoBinary: newDemoBinary(name, true)}
}

// device builds the ContactSensor.
func (c *demoContactSensor) device(opts device.Options) (*device.ContactSensor, error) {
	closed := c.state()
	levels, level := uint8(3), uint8(1)
	visual := bscdef.AlarmModeVisual
	d, err := device.NewContactSensor(opts,
		bsdef.Declare{Features: bsdef.FeatureChangeEvent, Initial: bsdef.Patch{StateValue: &closed}},
		bscdef.Declare{
			Features: bscdef.FeatureSensitivityLevel | bscdef.FeatureVisual | bscdef.FeatureAlarmSuppress,
			Delegate: contactConfigLog{name: c.name},
			Initial: bscdef.Patch{
				SupportedSensitivityLevels: &levels, DefaultSensitivityLevel: &level,
				AlarmsSupported: &visual, AlarmsEnabled: &visual,
			},
		})
	if err != nil {
		return nil, err
	}
	state := d.BooleanState()
	c.bind(func(v bool) error { return state.Set(bsdef.Patch{StateValue: &v}) })
	c.mu.Lock()
	c.config = bscdef.WrapOn(d.Server(bscdef.ClusterID), nil)
	c.mu.Unlock()
	return d, nil
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
// suppressed alarms (ClearAllAlarms, :40), each through the typed Set. The
// app ignores the result of either call (TEMPORARY_RETURN_IGNORED) and
// reports the trigger handled; so does this. False for a trigger that is
// not one of them.
func (c *demoContactSensor) testEventTrigger(trigger uint64) bool {
	c.mu.RLock()
	config := c.config
	c.mu.RUnlock()
	var err error
	switch trigger & smokeTriggerMask {
	case triggerBoolCfgSensorTrigger:
		enabled := config.Get().AlarmsEnabled
		err = config.Set(bscdef.Patch{AlarmsActive: &enabled})
	case triggerBoolCfgSensorUntrigger:
		var none bscdef.AlarmModeBitmap
		err = config.Set(bscdef.Patch{AlarmsActive: &none, AlarmsSuppressed: &none})
	default:
		return false
	}
	if err != nil {
		slog.Warn("contact.trigger", slog.String("device", c.name), slog.Any("err", err))
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

func (l contactConfigLog) AlarmsEnabledChanged(_ context.Context, enabled bscdef.AlarmModeBitmap) error {
	slog.Info("contact.alarms_enabled", slog.String("device", l.name), slog.Int("alarms", int(enabled)))
	return nil
}

func (l contactConfigLog) AlarmsActiveChanged(_ context.Context, active bscdef.AlarmModeBitmap) error {
	slog.Info("contact.alarms_active", slog.String("device", l.name), slog.Int("alarms", int(active)))
	return nil
}

func (l contactConfigLog) AlarmsSuppressedChanged(_ context.Context, suppressed bscdef.AlarmModeBitmap) error {
	slog.Info("contact.alarms_suppressed", slog.String("device", l.name), slog.Int("alarms", int(suppressed)))
	return nil
}

// --- a battery ------------------------------------------------------------------

// battery is a PowerSource part with a replaceable battery at a fixed
// charge: the module's battery server's shape (cluster/measurement
// PowerSourceServer from a percentage) — Active, order 1, "Battery", the
// remaining charge in half percent, charge level OK, user replaceable.
// EndpointList names the part's own endpoint once the node has numbered
// it ([powerSourceEndpointList]).
func batteryDeclaration(percent float64) device.Declaration {
	status, order, description := psdef.PowerSourceStatusActive, uint8(1), "Battery"
	halves := uint8(scaled(percent, 2, 0, 200)) //nolint:gosec // clamped to uint8
	level, needed, replace := psdef.BatChargeLevelOk, false, psdef.BatReplaceabilityUserReplaceable
	return psdef.Declare{Features: psdef.FeatureBattery, Initial: psdef.Patch{
		Status: &status, Order: &order, Description: &description, BatPercentRemaining: &halves,
		BatChargeLevel: &level, BatReplacementNeeded: &needed, BatReplaceability: &replace, EndpointList: &[]uint16{},
	}}
}

// powerSourceEndpointList sets a PowerSource's EndpointList to the
// endpoint the node serves it under, once the node has numbered it.
func powerSourceEndpointList(d *device.PowerSource) error {
	n, ok := d.EndpointNumber()
	if !ok {
		return errors.New("the power source has no endpoint number yet")
	}
	return d.PowerSource().Set(psdef.Patch{EndpointList: &[]uint16{n}})
}
