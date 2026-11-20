// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"testing"
	"time"

	epmdef "github.com/SukramJ/go-fabric/cluster/spec/electricalpowermeasurement"
)

// reading is one EnergyReporting reading on one endpoint.
type reading struct{ power, voltage, current, imported int64 }

// The nine steps of the two loads: base − randomness + ⌊k × 2 ×
// randomness / 8⌋ for k = 0…8 (matter.js support/chip-testing/src/cluster/
// FakeElectricalReadingsHandler.ts:35-53 and :114-115 at b25ed960).
var (
	loadPowerSteps     = [9]int64{980_000, 985_000, 990_000, 995_000, 1_000_000, 1_005_000, 1_010_000, 1_015_000, 1_020_000}
	loadVoltageSteps   = [9]int64{229_000, 229_250, 229_500, 229_750, 230_000, 230_250, 230_500, 230_750, 231_000}
	loadCurrentSteps   = [9]int64{3_848, 3_973, 4_098, 4_223, 4_348, 4_473, 4_598, 4_723, 4_848}
	genPowerSteps      = [9]int64{-3_020_000, -3_015_000, -3_010_000, -3_005_000, -3_000_000, -2_995_000, -2_990_000, -2_985_000, -2_980_000}
	genCurrentSteps    = [9]int64{-13_543, -13_418, -13_293, -13_168, -13_043, -12_918, -12_793, -12_668, -12_543}
	fakeReadingsTicked = 11 // more than nine readings, so the steps wrap
)

// fakeReadingsRig is the market fleet's grid meter with its two
// measurement endpoints' timers captured: pending[i] is endpoint i's next
// reading, interval[i] its interval.
type fakeReadingsRig struct {
	f        *fleet
	r        []*fakeReadings
	pending  [2]func()
	interval [2]time.Duration
}

func newFakeReadingsRig(t *testing.T) *fakeReadingsRig {
	t.Helper()
	g := &fakeReadingsRig{f: marketFleet(t)}
	g.r = g.f.electricalMeter.readings
	if len(g.r) != 2 {
		t.Fatalf("fake readings: %d endpoints, want the meter and its sensor part", len(g.r))
	}
	for i, r := range g.r {
		r.schedule = func(d time.Duration, fn func()) func() {
			g.pending[i], g.interval[i] = fn, d
			return func() {}
		}
	}
	return g
}

func (g *fakeReadingsRig) read(t *testing.T, i int) reading {
	t.Helper()
	p, e := g.r[i].target.power.Get(), g.r[i].target.energy.Get()
	if p.ActivePower == nil || p.Voltage == nil || p.ActiveCurrent == nil || e.CumulativeEnergyImported == nil {
		t.Fatalf("endpoint %d: a reading is null: %+v %+v", i, p, e)
	}
	if e.CumulativeEnergyImported.EndTimestamp == nil {
		t.Errorf("endpoint %d: CumulativeEnergyImported has no end timestamp", i)
	}
	return reading{*p.ActivePower, *p.Voltage, *p.ActiveCurrent, e.CumulativeEnergyImported.Energy}
}

// TestEnergyReportingFakeReadings holds the grid meter's fake readings to
// matter.js's FakeElectricalReadingsHandler (support/chip-testing/src/
// cluster/FakeElectricalReadingsHandler.ts at b25ed960; matter.js's own
// cases, all-devices-test-instance.test.ts:226-330, are ported as
// TestFakeElectricalReadingsHandler below): the readings step through the nine values, the step runs on
// across a stop and a start, the cumulative energy adds each interval's,
// and the endpoint bits select the endpoint. The values stay inside
// TC_EPM_2_2.py's ranges and change with every reading (steps 4a-4c,
// 5a-5c; connectedhomeip at 6170af84).
func TestEnergyReportingFakeReadings(t *testing.T) { //nolint:gocognit,cyclop,funlen // one sequence per subtest, checked per endpoint
	t.Run("the 1 kW load steps through nine values every 2 s", func(t *testing.T) {
		g := newFakeReadingsRig(t)
		fireMarket(t, g.f, triggerFakeReadingsLoadStart)
		var imported [2]int64
		for tick := range fakeReadingsTicked {
			k := tick % fakeLoadSteps
			for i := range 2 {
				if g.interval[i] != 2*time.Second {
					t.Fatalf("endpoint %d: interval %v, want 2s", i, g.interval[i])
				}
				imported[i] += loadPowerSteps[k] * 2 / 3600
				want := reading{loadPowerSteps[k], loadVoltageSteps[k], loadCurrentSteps[k], imported[i]}
				if got := g.read(t, i); got != want {
					t.Errorf("tick %d endpoint %d: %+v, want %+v", tick, i, got, want)
				}
			}
			g.pending[0]()
			g.pending[1]()
		}
		fireMarket(t, g.f, triggerFakeReadingsStop)
	})

	t.Run("the stop keeps the last readings and the step runs on into the generator", func(t *testing.T) {
		g := newFakeReadingsRig(t)
		fireMarket(t, g.f, triggerFakeReadingsLoadStart) // step 0
		g.pending[0]()                                   // step 1
		g.pending[1]()
		stale := g.pending[0]
		fireMarket(t, g.f, triggerFakeReadingsStop)
		stopped := g.read(t, 0)
		g.pending[0] = nil
		stale() // the timer the stop cancelled, delivered anyway
		if g.pending[0] != nil || g.read(t, 0) != stopped {
			t.Errorf("a reading after the stop: %+v after %+v", g.read(t, 0), stopped)
		}

		fireMarket(t, g.f, triggerFakeReadingsGenStart) // step 2: #start does not reset the step
		for tick := range fakeReadingsTicked {
			k := (2 + tick) % fakeLoadSteps
			for i := range 2 {
				if g.interval[i] != 5*time.Second {
					t.Fatalf("endpoint %d: generator interval %v, want 5s", i, g.interval[i])
				}
				want := reading{genPowerSteps[k], loadVoltageSteps[k], genCurrentSteps[k], 0}
				if got := g.read(t, i); got != want {
					t.Errorf("tick %d endpoint %d: %+v, want %+v (a generator exports; the imported total restarts at 0)", tick, i, got, want)
				}
				if exported := genPowerSteps[k] * -5 / 3600; tick == 0 && g.r[i].exported != exported {
					t.Errorf("endpoint %d: exported total %d, want %d", i, g.r[i].exported, exported)
				}
			}
			g.pending[0]()
			g.pending[1]()
		}
		fireMarket(t, g.f, triggerFakeReadingsStop)
	})

	t.Run("the endpoint bits select the endpoint", func(t *testing.T) {
		g := newFakeReadingsRig(t)
		numbers := [2]uint16{7, 8}
		for i, r := range g.r {
			r.target.endpoint = func() (uint16, bool) { return numbers[i], true }
		}
		fireMarket(t, g.f, triggerFakeReadingsLoadStart|8<<32)
		if g.pending[0] != nil || g.pending[1] == nil {
			t.Fatalf("endpoint 8's trigger started meter %v, sensor %v; want the sensor only", g.pending[0] != nil, g.pending[1] != nil)
		}
		if e := g.r[0].target.energy.Get().CumulativeEnergyImported; e != nil {
			t.Errorf("the meter's endpoint read %+v on endpoint 8's trigger", e)
		}
		if got := g.read(t, 1); got.power != loadPowerSteps[0] {
			t.Errorf("endpoint 8: ActivePower %d, want %d", got.power, loadPowerSteps[0])
		}
		for _, other := range []uint64{triggerFakeReadingsLoadStart | 9<<32, triggerFakeReadingsStop | 0xffff<<32, triggerFakeReadingsGenStart | 1<<32} {
			if g.f.electricalMeter.testEventTrigger(other) {
				t.Errorf("trigger 0x%016X for another endpoint was consumed", other)
			}
		}
		fireMarket(t, g.f, triggerFakeReadingsStop) // endpoint bits 0: every endpoint
		fireMarket(t, g.f, triggerFakeReadingsLoadStart|7<<32)
		if g.pending[0] == nil {
			t.Error("endpoint 7's trigger did not start the meter's readings")
		}
		fireMarket(t, g.f, triggerFakeReadingsStop)
	})
}

// mountedReadings is a started fleet node with the grid meter's sensor
// part — the ElectricalSensor endpoint, as matter.js's test reads its
// electrical-sensor endpoint — its endpoint number, and its timer
// captured: pending is the next reading, interval its interval.
type mountedReadings struct {
	f        *fleet
	sensor   *fakeReadings
	number   uint16
	pending  func()
	interval time.Duration
}

func newMountedReadings(t *testing.T) *mountedReadings {
	t.Helper()
	f, _ := startFleetBridge(t)
	m := &mountedReadings{f: f}
	f.electricalMeter.mu.Lock()
	readings := f.electricalMeter.readings
	f.electricalMeter.mu.Unlock()
	if len(readings) != 2 {
		t.Fatalf("fake readings: %d endpoints, want the meter and its sensor part", len(readings))
	}
	m.sensor = readings[1]
	n, ok := m.sensor.target.endpoint()
	if !ok || n == 0 {
		t.Fatalf("the sensor part has no endpoint number (%d, %v)", n, ok)
	}
	m.number = n
	for _, r := range readings {
		r.mu.Lock()
		r.schedule = func(d time.Duration, fn func()) func() {
			if r == m.sensor {
				m.pending, m.interval = fn, d
			}
			return func() {}
		}
		r.mu.Unlock()
	}
	t.Cleanup(func() { _ = f.testEventTrigger(context.Background(), triggerFakeReadingsStop) })
	return m
}

// trigger sends eventTrigger through the daemon's TestEventTrigger
// dispatch, as matter.js's test sends it through GeneralDiagnostics.
func (m *mountedReadings) trigger(eventTrigger uint64) error {
	return m.f.testEventTrigger(context.Background(), eventTrigger)
}

// at is eventTrigger with the sensor's endpoint in its endpoint bits.
func (m *mountedReadings) at(eventTrigger uint64) uint64 {
	return eventTrigger | uint64(m.number)<<32
}

func (m *mountedReadings) power(t *testing.T) int64 {
	t.Helper()
	p := m.sensor.target.power.Get().ActivePower
	if p == nil {
		t.Fatal("ActivePower is null")
	}
	return *p
}

func (m *mountedReadings) imported(t *testing.T) int64 {
	t.Helper()
	e := m.sensor.target.energy.Get().CumulativeEnergyImported
	if e == nil {
		t.Fatal("CumulativeEnergyImported is null")
	}
	return e.Energy
}

func (m *mountedReadings) exported() int64 {
	m.sensor.mu.Lock()
	defer m.sensor.mu.Unlock()
	return m.sensor.exported
}

// TestFakeElectricalReadingsHandler ports matter.js's "AllDevicesTestInstance
// electrical-sensor" cases (support/chip-testing/test/cert-framework/
// all-devices-test-instance.test.ts:226-330 at b25ed960) by meaning, through
// the daemon's TestEventTrigger dispatch on a started fleet. The daemon's
// meter declares ImportedEnergy and CumulativeEnergy only (decision 303),
// so the periodic and the exported measurements matter.js's cases read are
// checked on the handler's totals where it keeps one and left out where it
// keeps none (the periodic energy).
func TestFakeElectricalReadingsHandler(t *testing.T) { //nolint:gocognit,cyclop // five ported cases
	t.Run("reads a fake 1 kW load every 2 s until the stop trigger", func(t *testing.T) {
		m := newMountedReadings(t)
		if err := m.trigger(m.at(triggerFakeReadingsLoadStart)); err != nil {
			t.Fatal(err)
		}
		st := m.sensor.target.power.Get()
		if *st.ActivePower != 980_000 || *st.Voltage != 229_000 || *st.ActiveCurrent != 3_848 {
			t.Errorf("first reading %d mW, %d mV, %d mA; want 980000, 229000, 3848", *st.ActivePower, *st.Voltage, *st.ActiveCurrent)
		}
		if got := m.imported(t); got != 544 {
			t.Errorf("cumulative imported %d, want 544", got)
		}
		if m.interval != 2*time.Second {
			t.Fatalf("interval %v, want 2s", m.interval)
		}
		m.pending()
		if got := m.power(t); got != 985_000 {
			t.Errorf("second reading %d, want 985000", got)
		}
		if got := m.imported(t); got != 544+547 {
			t.Errorf("cumulative imported %d, want %d", got, 544+547)
		}
		stale := m.pending
		if err := m.trigger(m.at(triggerFakeReadingsStop)); err != nil {
			t.Fatal(err)
		}
		stale() // the timer the stop cancelled, delivered anyway
		if got := m.power(t); got != 985_000 {
			t.Errorf("a reading after the stop: %d", got)
		}
	})

	t.Run("reads a fake 3 kW generator on a trigger for all endpoints", func(t *testing.T) {
		m := newMountedReadings(t)
		if err := m.trigger(triggerFakeReadingsGenStart); err != nil {
			t.Fatal(err)
		}
		if got := m.power(t); got != -3_020_000 {
			t.Errorf("first reading %d, want -3020000", got)
		}
		if got, imp := m.exported(), m.imported(t); got != 4194 || imp != 0 {
			t.Errorf("exported %d, imported %d; want 4194, 0", got, imp)
		}
		if m.interval != 5*time.Second {
			t.Fatalf("interval %v, want 5s", m.interval)
		}
		m.pending()
		if got := m.power(t); got != -3_015_000 {
			t.Errorf("second reading %d, want -3015000", got)
		}
		if got := m.exported(); got != 4194+4187 {
			t.Errorf("exported %d, want %d", got, 4194+4187)
		}
		if err := m.trigger(triggerFakeReadingsStop); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("restarts the readings with the new load", func(t *testing.T) {
		m := newMountedReadings(t)
		if err := m.trigger(triggerFakeReadingsLoadStart); err != nil {
			t.Fatal(err)
		}
		loadTimer := m.pending
		if err := m.trigger(triggerFakeReadingsGenStart); err != nil {
			t.Fatal(err)
		}
		if got := m.power(t); got != -3_015_000 {
			t.Errorf("reading after the restart %d, want -3015000", got)
		}
		if got, imp := m.exported(), m.imported(t); got != 4187 || imp != 0 {
			t.Errorf("exported %d, imported %d; want 4187, 0", got, imp)
		}
		loadTimer() // the load's 2 s timer, cancelled by the restart
		if got := m.power(t); got != -3_015_000 {
			t.Errorf("the load's timer read %d after the restart", got)
		}
		if m.interval != 5*time.Second {
			t.Fatalf("interval %v, want the generator's 5s", m.interval)
		}
		m.pending()
		if got := m.power(t); got != -3_010_000 {
			t.Errorf("next reading %d, want -3010000", got)
		}
	})

	t.Run("refuses an unknown energy reporting trigger", func(t *testing.T) {
		m := newMountedReadings(t)
		if err := m.trigger(0x0091_0000_0000_0003); err == nil {
			t.Error("trigger 0x0091000000000003 was accepted")
		}
	})

	t.Run("refuses a trigger for another endpoint", func(t *testing.T) {
		m := newMountedReadings(t)
		meter, _ := m.f.electricalMeter.readings[0].target.endpoint()
		other := uint16(0xfffe)
		if other == meter || other == m.number {
			t.Fatalf("endpoint 0x%04X is the meter's", other)
		}
		if err := m.trigger(triggerFakeReadingsLoadStart | uint64(other)<<32); err == nil {
			t.Error("a trigger for another endpoint was accepted")
		}
		if m.pending != nil {
			t.Error("a trigger for another endpoint started the readings")
		}
	})
}

// TestElectricalAccuracyCoversServedTypes holds R2-P6-par-2601: after the
// fake readings ran, every ElectricalPowerMeasurement reading that is not
// null on either endpoint has an Accuracy entry of its type, and
// NumberOfMeasurementTypes is the list's length (the rule of
// cluster/measurement's elPwrAccuracy, R2-P6-par-1466). A reading the
// fake readings start setting later fails here until it gets its entry.
func TestElectricalAccuracyCoversServedTypes(t *testing.T) {
	f := marketFleet(t)
	for _, r := range f.electricalMeter.readings {
		r.schedule = func(time.Duration, func()) func() { return func() {} }
	}
	fireMarket(t, f, triggerFakeReadingsLoadStart)
	defer fireMarket(t, f, triggerFakeReadingsStop)
	for i, r := range f.electricalMeter.readings {
		st := r.target.power.Get()
		listed := map[epmdef.MeasurementTypeEnum]bool{}
		for _, a := range st.Accuracy {
			listed[a.MeasurementType] = true
		}
		if int(st.NumberOfMeasurementTypes) != len(st.Accuracy) {
			t.Errorf("endpoint %d: NumberOfMeasurementTypes %d, Accuracy has %d entries", i, st.NumberOfMeasurementTypes, len(st.Accuracy))
		}
		served := map[epmdef.MeasurementTypeEnum]*int64{
			epmdef.MeasurementTypeVoltage:       st.Voltage,
			epmdef.MeasurementTypeActiveCurrent: st.ActiveCurrent,
			epmdef.MeasurementTypeActivePower:   st.ActivePower,
			epmdef.MeasurementTypeFrequency:     st.Frequency,
		}
		for typ, v := range served {
			if v != nil && !listed[typ] {
				t.Errorf("endpoint %d: measurement type %d is set but has no Accuracy entry (%v)", i, typ, st.Accuracy)
			}
		}
		if !listed[epmdef.MeasurementTypeActivePower] {
			t.Errorf("endpoint %d: no ActivePower entry", i)
		}
	}
}
