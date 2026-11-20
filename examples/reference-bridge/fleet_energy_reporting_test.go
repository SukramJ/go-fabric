// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
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
// cluster/FakeElectricalReadingsHandler.ts at b25ed960; matter.js has no
// test of it): the readings step through the nine values, the step runs on
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

	t.Run("another code is not handled", func(t *testing.T) {
		g := newFakeReadingsRig(t)
		if g.f.electricalMeter.testEventTrigger(0x0091_0000_0000_0003) {
			t.Error("an unknown EnergyReporting trigger was handled")
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
