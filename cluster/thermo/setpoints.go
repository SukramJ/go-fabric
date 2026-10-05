// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package thermo

import "fmt"

// setpointState is the part of the Thermostat state the setpoint and
// setpoint-limit writes reconcile: a value copy, so a write is tried on a
// candidate and committed only when the result is valid.
//
// Mirrors matter.js packages/node/src/behaviors/thermostat/
// ThermostatServer.ts — #reconcileSetpoints and its helpers
// (#fixUserLimits, #fixLimitDeadband, #fixSetpointRange,
// #setpointsValid), themselves ports of chip's FixUserLimits /
// FixUserLimitDeadband / FixRange.
type setpointState struct {
	heat, cool, auto bool
	// deadband is MinSetpointDeadBand in the setpoints' 0.01 °C unit
	// (matter.js setpointDeadBand: minSetpointDeadBand * 10; 0 without
	// AUTO).
	deadband int

	absMinHeat, absMaxHeat int
	absMinCool, absMaxCool int
	minHeat, maxHeat       int
	minCool, maxCool       int
	occupHeat, occupCool   int
}

// Setpoint and limit attribute names, as the reconcile's "changed" set
// carries them (matter.js uses the state property names).
const (
	spOccupiedHeating = "occupiedHeatingSetpoint"
	spOccupiedCooling = "occupiedCoolingSetpoint"
	spMinHeatLimit    = "minHeatSetpointLimit"
	spMaxHeatLimit    = "maxHeatSetpointLimit"
	spMinCoolLimit    = "minCoolSetpointLimit"
	spMaxCoolLimit    = "maxCoolSetpointLimit"
)

func (s *ThermostatServer) setpointState() setpointState {
	st := setpointState{
		heat:       s.features&ThermostatFeatureHEAT != 0,
		cool:       s.features&ThermostatFeatureCOOL != 0,
		auto:       s.features&ThermostatFeatureAUTO != 0,
		absMinHeat: int(s.absMinHeat), absMaxHeat: int(s.absMaxHeat),
		absMinCool: int(s.absMinCool), absMaxCool: int(s.absMaxCool),
		minHeat: int(s.minHeat), maxHeat: int(s.maxHeat),
		minCool: int(s.minCool), maxCool: int(s.maxCool),
		occupHeat: int(s.occupHeat), occupCool: int(s.occupCool),
	}
	if st.auto {
		st.deadband = int(s.minSetpointDeadBand) * 10
	}
	return st
}

// commit stores st and returns the attributes whose value it changed.
func (s *ThermostatServer) commitSetpoints(st setpointState) []uint32 {
	var changed []uint32
	set := func(dst *int16, v int, id uint32) {
		if int(*dst) != v {
			*dst = int16(v) //nolint:gosec // reconciled values stay within the int16 absolute limits
			changed = append(changed, id)
		}
	}
	set(&s.minHeat, st.minHeat, thermoAttrMinHeatSetpointLimit)
	set(&s.maxHeat, st.maxHeat, thermoAttrMaxHeatSetpointLimit)
	set(&s.minCool, st.minCool, thermoAttrMinCoolSetpointLimit)
	set(&s.maxCool, st.maxCool, thermoAttrMaxCoolSetpointLimit)
	set(&s.occupHeat, st.occupHeat, thermoAttrOccupiedHeatingSetpoint)
	set(&s.occupCool, st.occupCool, thermoAttrOccupiedCoolingSetpoint)
	return changed
}

func (st *setpointState) userLimits(heat bool) (minV, maxV int) {
	if heat {
		return st.minHeat, st.maxHeat
	}
	return st.minCool, st.maxCool
}

// effectiveLimits is matter.js heatSetpointMinimum / …Maximum: the user
// limit, bounded by the absolute one.
func (st *setpointState) effectiveLimits(heat bool) (minV, maxV int) {
	if heat {
		return max(st.minHeat, st.absMinHeat), min(st.maxHeat, st.absMaxHeat)
	}
	return max(st.minCool, st.absMinCool), min(st.maxCool, st.absMaxCool)
}

// assertLimitWithinAbs is matter.js #assertLimitWithinAbs: a written user
// limit outside its own mode's absolute range is a ConstraintError (chip
// ChangeLimits).
func (st *setpointState) assertLimitWithinAbs(heat bool, v int) error {
	absMin, absMax := st.absMinCool, st.absMaxCool
	scope := "Cool"
	if heat {
		absMin, absMax, scope = st.absMinHeat, st.absMaxHeat, "Heat"
	}
	if v < absMin || v > absMax {
		return thermoConstraintErr{fmt.Sprintf("thermostat: %sSetpointLimit (%d) must be within absolute limits [%d, %d]", scope, v, absMin, absMax)}
	}
	return nil
}

// assertSetpointWithinLimits is matter.js #assertSetpointWithinLimits.
func (st *setpointState) assertSetpointWithinLimits(heat bool, v int) error {
	lo, hi := st.effectiveLimits(heat)
	scope := "Cooling"
	if heat {
		scope = "Heating"
	}
	if v < lo {
		return thermoConstraintErr{fmt.Sprintf("thermostat: Occupied%sSetpoint %d below its minimum limit %d", scope, v, lo)}
	}
	if v > hi {
		return thermoConstraintErr{fmt.Sprintf("thermostat: Occupied%sSetpoint %d above its maximum limit %d", scope, v, hi)}
	}
	return nil
}

// valid is matter.js #setpointsValid.
func (st *setpointState) valid() bool {
	for _, heat := range []bool{true, false} {
		if (heat && !st.heat) || (!heat && !st.cool) {
			continue
		}
		absMin, absMax := st.absMinCool, st.absMaxCool
		occ := st.occupCool
		if heat {
			absMin, absMax, occ = st.absMinHeat, st.absMaxHeat, st.occupHeat
		}
		if absMin > absMax {
			return false
		}
		minV, maxV := st.userLimits(heat)
		if minV > maxV || minV < absMin || maxV > absMax {
			return false
		}
		if occ < minV || occ > maxV {
			return false
		}
	}
	if st.auto {
		if st.maxCool-st.maxHeat < st.deadband || st.minCool-st.minHeat < st.deadband {
			return false
		}
		if st.occupCool-st.occupHeat < st.deadband {
			return false
		}
	}
	return true
}

// reconcile is matter.js #reconcileSetpoints: repair what a write left
// inconsistent by moving the values the write did not touch, and refuse
// with ConstraintError when no repair fits.
func (st *setpointState) reconcile(changed string) error {
	if st.valid() {
		return nil
	}
	if st.heat {
		st.fixUserLimits(true, changed)
	}
	if st.cool {
		st.fixUserLimits(false, changed)
	}
	if st.auto {
		st.fixLimitDeadband(true, changed)
		st.fixLimitDeadband(false, changed)
	}
	st.fixSetpointRange(changed)
	if !st.valid() {
		return thermoConstraintErr{"thermostat: setpoints could not be reconciled within the configured limits"}
	}
	return nil
}

// fixUserLimits is matter.js #fixUserLimits (chip FixUserLimits).
func (st *setpointState) fixUserLimits(heat bool, changed string) {
	absMin, absMax := st.absMinCool, st.absMaxCool
	minP, maxP := &st.minCool, &st.maxCool
	minKey, maxKey := spMinCoolLimit, spMaxCoolLimit
	if heat {
		absMin, absMax = st.absMinHeat, st.absMaxHeat
		minP, maxP = &st.minHeat, &st.maxHeat
		minKey, maxKey = spMinHeatLimit, spMaxHeatLimit
	}
	if absMin > absMax {
		return
	}
	if *minP < absMin || *minP > absMax {
		*minP = absMin
	}
	if *maxP < absMin || *maxP > absMax {
		*maxP = absMax
	}
	if *minP > *maxP {
		switch changed {
		case minKey:
			*maxP = *minP
		case maxKey:
			*minP = *maxP
		}
	}
}

// fixLimitDeadband is matter.js #fixLimitDeadband (chip
// FixUserLimitDeadband) for the max (upper=true) or min limit pair.
func (st *setpointState) fixLimitDeadband(upper bool, changed string) {
	heatP, coolP := &st.minHeat, &st.minCool
	if upper {
		heatP, coolP = &st.maxHeat, &st.maxCool
	}
	dead := st.deadband
	if *coolP-*heatP >= dead {
		return
	}
	switch changed {
	case spMinHeatLimit, spMaxHeatLimit:
		newCool := *heatP + dead
		if newCool >= st.absMinCool && newCool <= st.absMaxCool {
			*coolP = newCool
		} else {
			pinned := st.absMinCool
			if upper {
				pinned = st.absMaxCool
			}
			*coolP = pinned
			*heatP = pinned - dead
		}
	case spMinCoolLimit, spMaxCoolLimit:
		newHeat := *coolP - dead
		if newHeat >= st.absMinHeat && newHeat <= st.absMaxHeat {
			*heatP = newHeat
		} else {
			pinned := st.absMinHeat
			if upper {
				pinned = st.absMaxHeat
			}
			*heatP = pinned
			*coolP = pinned + dead
		}
	}
}

// fixSetpointRange is matter.js #fixSetpointRange (chip FixRange) for the
// occupied setpoints.
func (st *setpointState) fixSetpointRange(changed string) {
	if st.heat {
		st.occupHeat = min(max(st.occupHeat, st.minHeat), st.maxHeat)
	}
	if st.cool {
		st.occupCool = min(max(st.occupCool, st.minCool), st.maxCool)
	}
	if !st.auto || st.occupCool-st.occupHeat >= st.deadband {
		return
	}
	dead := st.deadband
	switch changed {
	case spOccupiedHeating, spMinHeatLimit, spMaxHeatLimit:
		newCool := st.occupHeat + dead
		if newCool >= st.minCool && newCool <= st.maxCool {
			st.occupCool = newCool
		} else {
			st.occupCool = st.maxCool
			st.occupHeat = st.maxCool - dead
		}
	case spOccupiedCooling, spMinCoolLimit, spMaxCoolLimit:
		newHeat := st.occupCool - dead
		if newHeat >= st.minHeat && newHeat <= st.maxHeat {
			st.occupHeat = newHeat
		} else {
			st.occupHeat = st.minHeat
			st.occupCool = st.minHeat + dead
		}
	}
}
