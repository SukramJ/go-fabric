// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"testing"
	"time"
)

// reading is one update's EnergyReporting values on one endpoint.
type reading struct{ power, voltage, current, imported int64 }

// TestEnergyReportingFakeReadings replays TC_EPM_2_2.py and TC_EEM_2_2.py
// (connectedhomeip at 6170af84) on the grid meter without waiting: the
// 1 kW load trigger puts ActivePower, Voltage and ActiveCurrent in the
// cases' ranges on the meter and its sensor part, every update changes
// each of them (steps 5a-5c), CumulativeEnergyImported grows with every
// update (TC_EEM_2_2.py step 5a), the stop trigger ends the updates and
// keeps the last readings, and the generator trigger resets the totals.
func TestEnergyReportingFakeReadings(t *testing.T) { //nolint:gocognit,cyclop // one sequence, checked per endpoint
	f := marketFleet(t)
	r := &f.electricalMeter.readings
	var pending func()
	var interval time.Duration
	r.schedule = func(d time.Duration, fn func()) func() {
		pending, interval = fn, d
		return func() {}
	}
	if len(r.targets) != 2 {
		t.Fatalf("targets: %d, want the meter and its sensor part", len(r.targets))
	}
	read := func(i int) reading {
		t.Helper()
		p, e := r.targets[i].power.Get(), r.targets[i].energy.Get()
		if p.ActivePower == nil || p.Voltage == nil || p.ActiveCurrent == nil || e.CumulativeEnergyImported == nil {
			t.Fatalf("endpoint %d: a reading is null: %+v %+v", i, p, e)
		}
		return reading{*p.ActivePower, *p.Voltage, *p.ActiveCurrent, e.CumulativeEnergyImported.Energy}
	}
	inRange := func(name string, v, lo, hi int64) {
		t.Helper()
		if v < lo || v > hi {
			t.Errorf("%s %d outside %d..%d", name, v, lo, hi)
		}
	}

	fireMarket(t, f, triggerFakeReadingsLoadStart)
	if interval != 2*time.Second {
		t.Fatalf("load interval %v, want 2s", interval)
	}
	var prev [2]reading
	for tick := range 6 {
		for i := range 2 {
			now := read(i)
			// TC_EPM_2_2.py steps 4a-4c and 5a-5c.
			inRange("ActivePower", now.power, 980_000, 1_020_000)
			inRange("ActiveCurrent", now.current, 3_848, 4_848)
			inRange("Voltage", now.voltage, 229_000, 231_000)
			if now.imported <= 0 {
				t.Errorf("tick %d endpoint %d: CumulativeEnergyImported %d after an update", tick, i, now.imported)
			}
			if tick > 0 {
				if now.power == prev[i].power || now.voltage == prev[i].voltage || now.current == prev[i].current {
					t.Errorf("tick %d endpoint %d: a reading did not change: %+v after %+v", tick, i, now, prev[i])
				}
				if now.imported <= prev[i].imported {
					t.Errorf("tick %d endpoint %d: CumulativeEnergyImported %d after %d", tick, i, now.imported, prev[i].imported)
				}
			}
			prev[i] = now
		}
		pending()
	}

	stale := pending
	fireMarket(t, f, triggerFakeReadingsStop)
	stopped := read(0)
	pending = nil
	stale() // the timer the stop cancelled, delivered anyway
	if pending != nil || read(0) != stopped {
		t.Errorf("an update after the stop: %+v after %+v", read(0), stopped)
	}

	fireMarket(t, f, triggerFakeReadingsGenStart|1<<32) // endpoint bits are cleared
	if interval != 5*time.Second {
		t.Errorf("generator interval %v, want 5s", interval)
	}
	gen := read(0)
	inRange("ActivePower", gen.power, -3_020_000, -2_980_000)
	inRange("ActiveCurrent", gen.current, -13_543, -12_543)
	if gen.imported != 0 {
		t.Errorf("the generator start kept CumulativeEnergyImported %d", gen.imported)
	}
	fireMarket(t, f, triggerFakeReadingsStop)
	if f.electricalMeter.testEventTrigger(0x0091_0000_0000_0003) {
		t.Error("an unknown EnergyReporting trigger was handled")
	}
}
