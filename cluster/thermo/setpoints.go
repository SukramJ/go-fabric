// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package thermo

import (
	"fmt"
	"slices"
	"strings"
)

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
	// occ is the Occupancy feature: the Unoccupied setpoints take part
	// (matter.js #setpointViolation and #fixSetpoints skip them without
	// it, ThermostatServer.ts:956-958, :1069-1072 at 862ad75f).
	heat, cool, auto, occ bool
	// deadband is MinSetpointDeadBand in the setpoints' 0.01 °C unit
	// (matter.js setpointDeadBand: minSetpointDeadBand * 10; 0 without
	// AUTO).
	deadband int

	absMinHeat, absMaxHeat int
	absMinCool, absMaxCool int
	minHeat, maxHeat       int
	minCool, maxCool       int
	occupHeat, occupCool   int
	unoccHeat, unoccCool   int
}

// Setpoint and limit attribute names, as the reconcile's "changed" set
// carries them (matter.js uses the state property names).
const (
	spOccupiedHeating = "occupiedHeatingSetpoint"
	spOccupiedCooling = "occupiedCoolingSetpoint"
	spUnoccHeating    = "unoccupiedHeatingSetpoint"
	spUnoccCooling    = "unoccupiedCoolingSetpoint"
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
		occ:        s.features&ThermostatFeatureOCC != 0,
		absMinHeat: int(s.absMinHeat), absMaxHeat: int(s.absMaxHeat),
		absMinCool: int(s.absMinCool), absMaxCool: int(s.absMaxCool),
		minHeat: int(s.minHeat), maxHeat: int(s.maxHeat),
		minCool: int(s.minCool), maxCool: int(s.maxCool),
		occupHeat: int(s.occupHeat), occupCool: int(s.occupCool),
		unoccHeat: int(s.unoccHeat), unoccCool: int(s.unoccCool),
	}
	if st.auto {
		st.deadband = int(s.minSetpointDeadBand) * 10
	}
	return st
}

// assign sets the setpoint or user limit attrID to v.
func (st *setpointState) assign(attrID uint32, v int) {
	switch attrID {
	case thermoAttrOccupiedHeatingSetpoint:
		st.occupHeat = v
	case thermoAttrOccupiedCoolingSetpoint:
		st.occupCool = v
	case thermoAttrUnoccupiedHeatingSetpoint:
		st.unoccHeat = v
	case thermoAttrUnoccupiedCoolingSetpoint:
		st.unoccCool = v
	case thermoAttrMinHeatSetpointLimit:
		st.minHeat = v
	case thermoAttrMaxHeatSetpointLimit:
		st.maxHeat = v
	case thermoAttrMinCoolSetpointLimit:
		st.minCool = v
	case thermoAttrMaxCoolSetpointLimit:
		st.maxCool = v
	}
}

// value is the setpoint attrID (Occupied or Unoccupied, Heating or
// Cooling).
func (st *setpointState) value(attrID uint32) int {
	switch attrID {
	case thermoAttrOccupiedHeatingSetpoint:
		return st.occupHeat
	case thermoAttrUnoccupiedHeatingSetpoint:
		return st.unoccHeat
	case thermoAttrUnoccupiedCoolingSetpoint:
		return st.unoccCool
	}
	return st.occupCool
}

// setpoint is the heating or cooling setpoint of the occupied or the
// unoccupied pair.
func (st *setpointState) setpoint(heat, unoccupied bool) int {
	return st.value(setpointAttr(heat, unoccupied))
}

// setSetpoint sets the heating or cooling setpoint of the occupied or the
// unoccupied pair.
func (st *setpointState) setSetpoint(heat, unoccupied bool, v int) {
	st.assign(setpointAttr(heat, unoccupied), v)
}

// setpointAttr is the attribute of the heating or cooling setpoint of the
// occupied or the unoccupied pair.
func setpointAttr(heat, unoccupied bool) uint32 {
	switch {
	case heat && unoccupied:
		return thermoAttrUnoccupiedHeatingSetpoint
	case heat:
		return thermoAttrOccupiedHeatingSetpoint
	case unoccupied:
		return thermoAttrUnoccupiedCoolingSetpoint
	}
	return thermoAttrOccupiedCoolingSetpoint
}

// setpointKey is the reconcile key of the heating or cooling setpoint of
// the occupied or the unoccupied pair.
func setpointKey(heat, unoccupied bool) string {
	switch {
	case heat && unoccupied:
		return spUnoccHeating
	case heat:
		return spOccupiedHeating
	case unoccupied:
		return spUnoccCooling
	}
	return spOccupiedCooling
}

// commit stores st and returns the attributes whose value it changed.
func (s *ThermostatServer) commitSetpoints(st setpointState) []uint32 {
	var changed []uint32
	set := func(dst *int16, v int, id uint32) {
		if int(*dst) != v {
			s.queueSetpointEventLocked(id, *dst, int16(v)) //nolint:gosec // reconciled values stay within the int16 absolute limits
			*dst = int16(v)                                //nolint:gosec // reconciled values stay within the int16 absolute limits
			changed = append(changed, id)
		}
	}
	set(&s.minHeat, st.minHeat, thermoAttrMinHeatSetpointLimit)
	set(&s.maxHeat, st.maxHeat, thermoAttrMaxHeatSetpointLimit)
	set(&s.minCool, st.minCool, thermoAttrMinCoolSetpointLimit)
	set(&s.maxCool, st.maxCool, thermoAttrMaxCoolSetpointLimit)
	set(&s.occupHeat, st.occupHeat, thermoAttrOccupiedHeatingSetpoint)
	set(&s.occupCool, st.occupCool, thermoAttrOccupiedCoolingSetpoint)
	set(&s.unoccHeat, st.unoccHeat, thermoAttrUnoccupiedHeatingSetpoint)
	set(&s.unoccCool, st.unoccCool, thermoAttrUnoccupiedCoolingSetpoint)
	return changed
}

// setpointDiff lists what next changes against cur, in commitSetpoints'
// order.
func setpointDiff(cur, next setpointState) []SetpointChange {
	var out []SetpointChange
	add := func(was, is int, id uint32) {
		if was != is {
			out = append(out, SetpointChange{AttrID: id, Value: int16(is)}) //nolint:gosec // reconciled values stay within the int16 absolute limits
		}
	}
	add(cur.minHeat, next.minHeat, thermoAttrMinHeatSetpointLimit)
	add(cur.maxHeat, next.maxHeat, thermoAttrMaxHeatSetpointLimit)
	add(cur.minCool, next.minCool, thermoAttrMinCoolSetpointLimit)
	add(cur.maxCool, next.maxCool, thermoAttrMaxCoolSetpointLimit)
	add(cur.occupHeat, next.occupHeat, thermoAttrOccupiedHeatingSetpoint)
	add(cur.occupCool, next.occupCool, thermoAttrOccupiedCoolingSetpoint)
	add(cur.unoccHeat, next.unoccHeat, thermoAttrUnoccupiedHeatingSetpoint)
	add(cur.unoccCool, next.unoccCool, thermoAttrUnoccupiedCoolingSetpoint)
	return out
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

// assertLimitWithinAbs is matter.js #assertLimitWithinAbs
// (ThermostatServer.ts:909-916 at 862ad75f): a written user setpoint limit
// outside its own mode's absolute range is a hard CONSTRAINT_ERROR (chip
// ChangeLimits). The text is matter.js's.
func (st *setpointState) assertLimitWithinAbs(heat bool, v int) error {
	absMin, absMax := st.absMinCool, st.absMaxCool
	scope := "Cool"
	if heat {
		absMin, absMax, scope = st.absMinHeat, st.absMaxHeat, "Heat"
	}
	if v < absMin || v > absMax {
		return thermoConstraintErr{fmt.Sprintf("%sSetpointLimit (%d) must be within absolute limits [%d, %d]", scope, v, absMin, absMax)}
	}
	return nil
}

// assertSetpointWithinLimits is matter.js #assertSetpointWithinLimits
// (ThermostatServer.ts:890-903 at 862ad75f): a directly written setpoint
// within the effective limits. The text is matter.js's.
func (st *setpointState) assertSetpointWithinLimits(heat, unoccupied bool, v int) error {
	lo, hi := st.effectiveLimits(heat)
	scope, kind := "Cool", "Occupied"
	if heat {
		scope = "Heat"
	}
	if unoccupied {
		kind = "Unoccupied"
	}
	if v < lo {
		return thermoConstraintErr{fmt.Sprintf("%s%sSetpoint (%d) must be greater than or equal to min%sSetpointLimit (%d)", scope, kind, v, scope, lo)}
	}
	if v > hi {
		return thermoConstraintErr{fmt.Sprintf("%s%sSetpoint (%d) must be less than or equal to max%sSetpointLimit (%d)", scope, kind, v, scope, hi)}
	}
	return nil
}

// assertUserSetpointLimits is matter.js #assertUserSetpointLimits
// (ThermostatServer.ts:830-853 at 862ad75f): an absolute limit a device
// changes keeps absMin <= min <= max <= absMax. The texts are matter.js's.
func (st *setpointState) assertUserSetpointLimits(heat bool) error {
	scope, absMin, absMax := "Cool", st.absMinCool, st.absMaxCool
	if heat {
		scope, absMin, absMax = "Heat", st.absMinHeat, st.absMaxHeat
	}
	minV, maxV := st.userLimits(heat)
	switch {
	case absMin > minV:
		return thermoConstraintErr{fmt.Sprintf("absMin%sSetpointLimit (%d) must be less than or equal to min%sSetpointLimit (%d)", scope, absMin, scope, minV)}
	case minV > maxV:
		return thermoConstraintErr{fmt.Sprintf("min%sSetpointLimit (%d) must be less than or equal to max%sSetpointLimit (%d)", scope, minV, scope, maxV)}
	case maxV > absMax:
		return thermoConstraintErr{fmt.Sprintf("max%sSetpointLimit (%d) must be less than or equal to absMax%sSetpointLimit (%d)", scope, maxV, scope, absMax)}
	}
	return nil
}

// changedKeys is the reconcile's "changed" set: the state keys whose
// value the reconcile preserves, moving the others (matter.js passes a
// Set<string> of state property names).
type changedKeys []string

func (c changedKeys) has(keys ...string) bool {
	for _, k := range keys {
		if slices.Contains(c, k) {
			return true
		}
	}
	return false
}

// violation describes the first violated setpoint invariant, "" when all
// hold. Mirrors matter.js packages/node/src/behaviors/thermostat/
// ThermostatServer.ts:941-986 at 862ad75f (#setpointViolation, chip
// Setpoints::Valid()): per served mode, ordered absolute limits, user
// limits ordered and nested in them, the occupied — and with OCC the
// unoccupied — setpoint within the user limits; with AUTO, the deadband
// between the max limits, the min limits and each setpoint pair. The texts
// are matter.js's.
func (st *setpointState) violation() string {
	for _, heat := range []bool{true, false} {
		if (heat && !st.heat) || (!heat && !st.cool) {
			continue
		}
		scope, absMin, absMax := "Cool", st.absMinCool, st.absMaxCool
		if heat {
			scope, absMin, absMax = "Heat", st.absMinHeat, st.absMaxHeat
		}
		if absMin > absMax {
			return fmt.Sprintf("absMin%sSetpointLimit (%d) is above absMax%sSetpointLimit (%d)", scope, absMin, scope, absMax)
		}
		minV, maxV := st.userLimits(heat)
		if minV > maxV || minV < absMin || maxV > absMax {
			return fmt.Sprintf("%s setpoint limits [%d, %d] are not ordered within absolute limits [%d, %d]", scope, minV, maxV, absMin, absMax)
		}
		for _, unocc := range st.pairs() {
			if v := st.setpoint(heat, unocc); v < minV || v > maxV {
				return fmt.Sprintf("%s (%d) is outside %s setpoint limits [%d, %d]", setpointKey(heat, unocc), v, scope, minV, maxV)
			}
		}
	}
	if st.auto {
		if st.maxCool-st.maxHeat < st.deadband {
			return fmt.Sprintf("maxCoolSetpointLimit (%d) - maxHeatSetpointLimit (%d) is below the deadband (%d)", st.maxCool, st.maxHeat, st.deadband)
		}
		if st.minCool-st.minHeat < st.deadband {
			return fmt.Sprintf("minCoolSetpointLimit (%d) - minHeatSetpointLimit (%d) is below the deadband (%d)", st.minCool, st.minHeat, st.deadband)
		}
		for _, unocc := range st.pairs() {
			heat, cool := st.setpoint(true, unocc), st.setpoint(false, unocc)
			if cool-heat < st.deadband {
				return fmt.Sprintf("%s (%d) - %s (%d) is below the deadband (%d)", setpointKey(false, unocc), cool, setpointKey(true, unocc), heat, st.deadband)
			}
		}
	}
	return ""
}

// pairs are the setpoint pairs that take part: the occupied one, and the
// unoccupied one with OCC (false for occupied, true for unoccupied).
func (st *setpointState) pairs() []bool {
	if st.occ {
		return []bool{false, true}
	}
	return []bool{false}
}

// reconcile is matter.js #reconcileSetpoints: repair what a write left
// inconsistent by moving the values the write did not touch, and refuse
// with ConstraintError when no repair fits.
func (st *setpointState) reconcile(changed string) error {
	violation := st.violation()
	if violation == "" {
		return nil
	}
	st.fixSetpoints(changedKeys{changed})
	if remaining := st.violation(); remaining != "" {
		return thermoConstraintErr{fmt.Sprintf("thermo: setpoints could not be reconciled within the configured limits: %s; after adjusting, %s", violation, remaining)}
	}
	return nil
}

// reconcileInitial repairs a configuration that violates the setpoint
// invariants, preferring the heating side (the cooling side without HEAT):
// the kept side's user limits and occupied setpoint and
// MinCoolSetpointLimit are the "changed" set, so the other side moves
// first. It returns the violation found ("" when the configuration was
// valid) and the adjustments as "key before -> after"; an error when a
// violation remains. Mirrors matter.js 0ceb202d packages/node/src/
// behaviors/thermostat/ThermostatServer.ts:1015-1042
// (#reconcileInitialSetpoints; the unoccupied setpoint of its set is not
// served here).
func (st *setpointState) reconcileInitial() (violation string, adjusted []string, err error) {
	violation = st.violation()
	if violation == "" {
		return "", nil, nil
	}
	before := *st
	kept := "Heat"
	occupied, unoccupied := spOccupiedHeating, spUnoccHeating
	if !st.heat {
		kept, occupied, unoccupied = "Cool", spOccupiedCooling, spUnoccCooling
	}
	st.fixSetpoints(changedKeys{"min" + kept + "SetpointLimit", "max" + kept + "SetpointLimit", occupied, unoccupied, spMinCoolLimit})
	if remaining := st.violation(); remaining != "" {
		return violation, nil, fmt.Errorf("%w: %s; after adjusting, %s", ErrSetpoints, violation, remaining)
	}
	for _, c := range []struct {
		key     string
		was, is int
	}{
		{spMinHeatLimit, before.minHeat, st.minHeat},
		{spMaxHeatLimit, before.maxHeat, st.maxHeat},
		{spMinCoolLimit, before.minCool, st.minCool},
		{spMaxCoolLimit, before.maxCool, st.maxCool},
		{spOccupiedHeating, before.occupHeat, st.occupHeat},
		{spOccupiedCooling, before.occupCool, st.occupCool},
		{spUnoccHeating, before.unoccHeat, st.unoccHeat},
		{spUnoccCooling, before.unoccCool, st.unoccCool},
	} {
		if c.was != c.is {
			adjusted = append(adjusted, fmt.Sprintf("%s %d -> %d", c.key, c.was, c.is))
		}
	}
	return violation, adjusted, nil
}

// adjustedText joins reconcileInitial's adjustments as matter.js's warning
// does.
func adjustedText(adjusted []string) string { return strings.Join(adjusted, ", ") }

// fixSetpoints is matter.js #fixSetpoints (ThermostatServer.ts:1058-1073
// at 862ad75f): user limits, then the AUTO limit deadband (max, then min),
// then the occupied setpoint range and, with OCC, the unoccupied one.
func (st *setpointState) fixSetpoints(changed changedKeys) {
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
	st.fixSetpointRange(false, changed)
	if st.occ {
		st.fixSetpointRange(true, changed)
	}
}

// fixUserLimits is matter.js #fixUserLimits (chip FixUserLimits).
func (st *setpointState) fixUserLimits(heat bool, changed changedKeys) {
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
		switch {
		case changed.has(minKey):
			*maxP = *minP
		case changed.has(maxKey):
			*minP = *maxP
		}
	}
}

// fixLimitDeadband is matter.js #fixLimitDeadband (chip
// FixUserLimitDeadband) for the max (upper=true) or min limit pair.
func (st *setpointState) fixLimitDeadband(upper bool, changed changedKeys) {
	heatP, coolP := &st.minHeat, &st.minCool
	if upper {
		heatP, coolP = &st.maxHeat, &st.maxCool
	}
	dead := st.deadband
	if *coolP-*heatP >= dead {
		return
	}
	switch {
	case changed.has(spMinHeatLimit, spMaxHeatLimit):
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
	case changed.has(spMinCoolLimit, spMaxCoolLimit):
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

// fixSetpointRange is matter.js #fixSetpointRange (chip FixRange,
// ThermostatServer.ts:1142-1183 at 862ad75f) for the occupied or the
// unoccupied setpoints.
func (st *setpointState) fixSetpointRange(unoccupied bool, changed changedKeys) {
	heatKey, coolKey := setpointKey(true, unoccupied), setpointKey(false, unoccupied)
	heat, cool := st.setpoint(true, unoccupied), st.setpoint(false, unoccupied)
	if st.heat {
		heat = min(max(heat, st.minHeat), st.maxHeat)
	}
	if st.cool {
		cool = min(max(cool, st.minCool), st.maxCool)
	}
	defer func() {
		st.setSetpoint(true, unoccupied, heat)
		st.setSetpoint(false, unoccupied, cool)
	}()
	if !st.auto || cool-heat >= st.deadband {
		return
	}
	dead := st.deadband
	switch {
	case changed.has(heatKey, spMinHeatLimit, spMaxHeatLimit):
		newCool := heat + dead
		if newCool >= st.minCool && newCool <= st.maxCool {
			cool = newCool
		} else {
			cool = st.maxCool
			heat = st.maxCool - dead
		}
	case changed.has(coolKey, spMinCoolLimit, spMaxCoolLimit):
		newHeat := cool - dead
		if newHeat >= st.minHeat && newHeat <= st.maxHeat {
			heat = newHeat
		} else {
			heat = st.minHeat
			cool = st.minHeat + dead
		}
	}
}
