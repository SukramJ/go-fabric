// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package thermo

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"slices"

	"thingfabric.dev/go-fabric/internal/deepcopy"

	"thingfabric.dev/go-fabric/cluster/spec"
	tdef "thingfabric.dev/go-fabric/cluster/spec/thermostat"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/tlv"
)

// The write rules of Schedules (MSCH) — the atomic write of a controller,
// the host's SetSchedules and the configured schedules alike — as
// matter.js's ThermostatServer implements them behind its forward feature
// thermostat-msch (packages/node/src/behaviors/thermostat/ThermostatServer.ts
// at b25ed960: #validateScheduleWriteRequest :2693-2807,
// #validateScheduleTransitions :2825-3002, #normalizeSchedules :3028-3049,
// #validateScheduleCommit :3056-3086), on the atomic-write state of
// atomic.go.

// scheduleHandleLength is the length of a handle the server issues to a
// new schedule — matter.js entropy.randomBytes(16)
// (ThermostatServer.ts:3036-3040 at b25ed960) — and the schema's "max 16"
// (thermostat-cluster.element.ts:634).
const scheduleHandleLength = 16

// scheduleNameLength is the longest schedule name, the schema's "max 64"
// (thermostat-cluster.element.ts:636 at b25ed960).
const scheduleNameLength = 64

// validScheduleDays are the weekday bits a transition may carry: Sunday to
// Saturday, not Away (ThermostatServer.ts:2836-2845 at b25ed960).
const validScheduleDays = tdef.ScheduleDayOfWeekSunday | tdef.ScheduleDayOfWeekMonday | tdef.ScheduleDayOfWeekTuesday |
	tdef.ScheduleDayOfWeekWednesday | tdef.ScheduleDayOfWeekThursday | tdef.ScheduleDayOfWeekFriday |
	tdef.ScheduleDayOfWeekSaturday

// scheduleRules is what the schedule validation reads besides the
// schedules: the feature selection, the schedule types and counts, the
// setpoint limits and deadband, the presets a preset handle must name, and
// whose write it is.
type scheduleRules struct {
	features       uint32
	types          []ScheduleType
	maxSchedules   int
	maxTransitions int
	perDay         *uint8
	st             setpointState
	// presets are the presets a preset handle names: the writing peer's
	// own pending Presets, else the stored ones — matter.js's presets
	// getter (ThermostatServer.ts:3196-3222 at b25ed960), which consults
	// AtomicWriteHandler.pendingValueForAttributeAndPeer
	// (AtomicWriteHandler.ts:433-458: another peer's staged value is never
	// seen, :449-452). Without PRES no handle is present.
	presets []Preset
	// host marks the host's authoritative replace (SetSchedules, the
	// configured list): the content rules apply, matter.js's lifecycle
	// rules of a Matter write do not — a handle the device never issued, a
	// new built-in schedule, a changed BuiltIn (owner decision O-8,
	// BD-Thermostat-HostSchedules).
	host bool
}

// scheduleRules gathers the rules against presets; the caller holds s.mu.
func (s *ThermostatServer) scheduleRules(presets []Preset) scheduleRules {
	if s.features&ThermostatFeaturePRES == 0 {
		presets = nil
	}
	return scheduleRules{
		features:       s.features,
		types:          s.scheduleTypes,
		maxSchedules:   int(s.numberOfSchedules),
		maxTransitions: int(s.numberOfScheduleTransitions),
		perDay:         s.transitionsPerDay,
		st:             s.setpointState(),
		presets:        presets,
	}
}

// presetPresent reports whether a preset of the rules carries handle h.
func (r scheduleRules) presetPresent(h []byte) bool {
	return slices.ContainsFunc(r.presets, func(p Preset) bool {
		return !p.PresetHandle.Null && bytes.Equal(p.PresetHandle.Value, h)
	})
}

// scheduleType is the ScheduleTypeStruct for mode, if any.
func (r scheduleRules) scheduleType(mode tdef.SystemModeEnum) (ScheduleType, bool) {
	for _, t := range r.types {
		if t.SystemMode == mode {
			return t, true
		}
	}
	return ScheduleType{}, false
}

// heatLimits and coolLimits are the bounds of a transition's setpoints —
// matter.js heatSetpointMinimum … coolSetpointMaximum
// (ThermostatServer.ts:1307-1321 at b25ed960): the user limit clipped to
// the absolute one, with no deadband term (the deadband between a
// transition's two setpoints is a check of its own, :2983-2991).
func (r scheduleRules) heatLimits() (lo, hi int) {
	return max(r.st.minHeat, r.st.absMinHeat), min(r.st.maxHeat, r.st.absMaxHeat)
}

func (r scheduleRules) coolLimits() (lo, hi int) {
	return max(r.st.minCool, r.st.absMinCool), min(r.st.maxCool, r.st.absMaxCool)
}

// validateSchedules validates list against the schedules it replaces and
// returns it normalised (a null BuiltIn resolved) — matter.js
// #validateScheduleWriteRequest (ThermostatServer.ts:2693-2807 at
// b25ed960): per schedule the handle rules, the schedule type, the
// transition count, the name, the preset and the transitions; the count
// limits only after every schedule passed its own checks (:2793-2806).
// A whole-list write and a controller's per-entry ADD come to the same
// result, an entry being validated against the entries before it only.
func (r scheduleRules) validateSchedules(stored, list []Schedule) ([]Schedule, error) {
	constraint := func(format string, args ...any) error {
		return spec.Errorf(im.StatusConstraintError, "thermo: schedule: "+format, args...)
	}
	exhausted := func(format string, args ...any) error {
		return spec.Errorf(im.StatusResourceExhausted, "thermo: schedule: "+format, args...)
	}
	pending := make([]Schedule, 0, len(list))
	var modes []tdef.SystemModeEnum
	counts := map[tdef.SystemModeEnum]int{}
	for _, sc := range deepcopy.Each(list) {
		// The schema's field sizes, which answer first on the wire
		// (thermostat-cluster.element.ts:634-657); kept for the host's
		// SetSchedules, which no schema check precedes with the same order.
		if err := checkScheduleSizes(sc); err != nil {
			return nil, err
		}
		builtIn, err := r.resolveBuiltIn(stored, pending, sc)
		if err != nil {
			return nil, err
		}
		sc.BuiltIn = builtIn
		// The schedule type of the SystemMode (:2750-2756): a mode no type
		// names — ScheduleTypes holds only Auto, Heat and Cool
		// (#assertScheduleTypes, :2666-2687) — is CONSTRAINT_ERROR; a type
		// of NumberOfSchedules 0 passes and is exhausted by the count below.
		typ, ok := r.scheduleType(sc.SystemMode)
		if !ok {
			return nil, constraint("no ScheduleType defined for SystemMode %d", sc.SystemMode)
		}
		// Transitions per schedule (:2758-2762).
		if len(sc.Transitions) > r.maxTransitions {
			return nil, exhausted("%d transitions exceed NumberOfScheduleTransitions %d", len(sc.Transitions), r.maxTransitions)
		}
		if counts[sc.SystemMode] == 0 {
			modes = append(modes, sc.SystemMode)
		}
		counts[sc.SystemMode]++
		feats := typ.ScheduleTypeFeatures
		if sc.Name != nil && feats&tdef.ScheduleTypeFeaturesSupportsNames == 0 {
			return nil, constraint("the schedule type does not support names")
		}
		if sc.PresetHandle != nil {
			if feats&tdef.ScheduleTypeFeaturesSupportsPresets == 0 {
				return nil, constraint("the schedule type does not support presets")
			}
			if !r.presetPresent(*sc.PresetHandle) {
				return nil, constraint("preset handle %s names no preset", handleKey(*sc.PresetHandle))
			}
		}
		if err := r.validateTransitions(sc, feats); err != nil {
			return nil, err
		}
		if err := r.validateTransitionsPerDay(sc); err != nil {
			return nil, err
		}
		pending = append(pending, sc)
	}
	// The count limits, after every schedule (:2793-2806).
	if len(pending) > r.maxSchedules {
		return nil, exhausted("%d schedules exceed NumberOfSchedules %d", len(pending), r.maxSchedules)
	}
	for _, mode := range modes {
		typ, _ := r.scheduleType(mode)
		if counts[mode] > int(typ.NumberOfSchedules) {
			return nil, exhausted("%d schedules of SystemMode %d exceed its type's %d", counts[mode], mode, typ.NumberOfSchedules)
		}
	}
	return pending, nil
}

// checkScheduleSizes holds a schedule to the schema's sizes: the handle,
// the name, the preset handle and each transition's preset handle
// (thermostat-cluster.element.ts:634-657 at b25ed960).
func checkScheduleSizes(sc Schedule) error {
	constraint := func(format string, args ...any) error {
		return spec.Errorf(im.StatusConstraintError, "thermo: schedule: "+format, args...)
	}
	if !sc.ScheduleHandle.Null && len(sc.ScheduleHandle.Value) > scheduleHandleLength {
		return constraint("handle longer than %d bytes", scheduleHandleLength)
	}
	if sc.Name != nil && len(*sc.Name) > scheduleNameLength {
		return constraint("name longer than %d bytes", scheduleNameLength)
	}
	if sc.PresetHandle != nil && len(*sc.PresetHandle) > scheduleHandleLength {
		return constraint("preset handle longer than %d bytes", scheduleHandleLength)
	}
	for _, t := range sc.Transitions {
		if t.PresetHandle != nil && len(*t.PresetHandle) > scheduleHandleLength {
			return constraint("transition preset handle longer than %d bytes", scheduleHandleLength)
		}
	}
	return nil
}

// resolveBuiltIn is the handle rules of #validateScheduleWriteRequest
// (ThermostatServer.ts:2718-2748 at b25ed960) and the BuiltIn
// #normalizeSchedules settles on (:3041-3047): a handle twice in the list
// is CONSTRAINT_ERROR; for a Matter write a handle the stored schedules
// lack is NOT_FOUND, and a new schedule claiming to be built in and a
// BuiltIn that differs from the stored schedule's are CONSTRAINT_ERROR —
// lifecycle rules the host's replace is not held to (r.host, decision
// O-8); a null BuiltIn takes the stored schedule's, false without one.
func (r scheduleRules) resolveBuiltIn(stored, pending []Schedule, sc Schedule) (spec.Nullable[bool], error) {
	constraint := func(format string, args ...any) error {
		return spec.Errorf(im.StatusConstraintError, "thermo: schedule: "+format, args...)
	}
	newBuiltIn := !sc.BuiltIn.Null && sc.BuiltIn.Value
	if sc.ScheduleHandle.Null {
		if newBuiltIn && !r.host {
			return sc.BuiltIn, constraint("can not add a new built-in schedule")
		}
		if !sc.BuiltIn.Null {
			return sc.BuiltIn, nil
		}
		return spec.ValueOf(false), nil
	}
	h := sc.ScheduleHandle.Value
	match, ok := scheduleWithHandle(stored, h)
	switch {
	case !ok && !r.host:
		return sc.BuiltIn, spec.Errorf(im.StatusNotFound, "thermo: schedule: handle %s does not exist in the stored schedules", handleKey(h))
	case scheduleHandleIn(pending, h):
		return sc.BuiltIn, constraint("handle %s is already in the list", handleKey(h))
	case ok && !r.host && !sc.BuiltIn.Null && (match.BuiltIn.Null || match.BuiltIn.Value != sc.BuiltIn.Value):
		return sc.BuiltIn, constraint("cannot change the built-in status of schedule %s", handleKey(h))
	}
	if !sc.BuiltIn.Null {
		return sc.BuiltIn, nil
	}
	if ok && !match.BuiltIn.Null {
		return match.BuiltIn, nil
	}
	return spec.ValueOf(false), nil
}

// scheduleHandleIn reports whether a schedule of list carries handle h.
func scheduleHandleIn(list []Schedule, h []byte) bool {
	_, ok := scheduleWithHandle(list, h)
	return ok
}

// validateTransitions is matter.js #validateScheduleTransitions
// (ThermostatServer.ts:2825-3002 at b25ed960), every refusal
// CONSTRAINT_ERROR. Where two rules hit one transition the order is
// ours (decision C-7, D11: no wire difference, every one answers
// CONSTRAINT_ERROR). The feature a transition's SystemMode needs is
// matter.js's SystemModeEnum value conformance (:2919-2923), here a rule
// of its own with the same status; the empty list and a TransitionTime
// beyond 1439 are the schema's (thermostat-cluster.element.ts:639-645,
// :653), held here as well for the host's SetSchedules.
func (r scheduleRules) validateTransitions(sc Schedule, feats tdef.ScheduleTypeFeaturesBitmap) error { //nolint:gocognit,gocyclo,cyclop,funlen // one rule per matter.js check, in its order
	constraint := func(i int, format string, args ...any) error {
		return spec.Errorf(im.StatusConstraintError, "thermo: schedule transition %d: "+format, append([]any{i}, args...)...)
	}
	if len(sc.Transitions) == 0 {
		return spec.Errorf(im.StatusConstraintError, "thermo: schedule has no transitions")
	}
	heatLo, heatHi := r.heatLimits()
	coolLo, coolHi := r.coolLimits()
	for i, t := range sc.Transitions {
		// No Away bit, at least one day (:2836-2845).
		if t.DayOfWeek == 0 || t.DayOfWeek&^validScheduleDays != 0 {
			return constraint(i, "DayOfWeek 0x%02X has no weekday or a day outside Sunday to Saturday", t.DayOfWeek)
		}
		if t.TransitionTime > 1439 {
			return constraint(i, "TransitionTime %d is beyond 1439", t.TransitionTime)
		}
		// No two transitions at one time on a shared day (:2847-2858).
		for j := range i {
			if p := sc.Transitions[j]; p.TransitionTime == t.TransitionTime && p.DayOfWeek&t.DayOfWeek != 0 {
				return constraint(i, "repeats transition %d's time on a shared day", j)
			}
		}
		setpointFields := t.SystemMode != nil || t.CoolingSetpoint != nil || t.HeatingSetpoint != nil
		// A preset handle: supported, naming a preset, alone (:2866-2890).
		if t.PresetHandle != nil {
			if feats&tdef.ScheduleTypeFeaturesSupportsPresets == 0 {
				return constraint(i, "the schedule type does not support presets")
			}
			if !r.presetPresent(*t.PresetHandle) {
				return constraint(i, "preset handle %s names no preset", handleKey(*t.PresetHandle))
			}
			if setpointFields {
				return constraint(i, "a preset handle excludes SystemMode and the setpoints")
			}
		}
		// Setpoint fields need SupportsSetpoints (:2893-2902).
		if setpointFields && feats&tdef.ScheduleTypeFeaturesSupportsSetpoints == 0 {
			return constraint(i, "the schedule type does not support setpoints")
		}
		if t.SystemMode != nil {
			// Omitted when it equals the schedule's (:2905-2909).
			if *t.SystemMode == sc.SystemMode {
				return constraint(i, "SystemMode %d must be omitted when it equals the schedule's SystemMode", *t.SystemMode)
			}
			var need uint32
			switch *t.SystemMode {
			case tdef.SystemModeOff:
				if feats&tdef.ScheduleTypeFeaturesSupportsOff == 0 {
					return constraint(i, "the schedule type does not support Off")
				}
			case tdef.SystemModeHeat:
				need = ThermostatFeatureHEAT
			case tdef.SystemModeCool:
				need = ThermostatFeatureCOOL
			case tdef.SystemModeAuto:
				need = ThermostatFeatureAUTO
			default:
				return constraint(i, "SystemMode %d is not Off, Heat, Cool or Auto", *t.SystemMode)
			}
			if need != 0 && r.features&need == 0 {
				return constraint(i, "SystemMode %d needs a feature the thermostat lacks", *t.SystemMode)
			}
		}
		// The setpoints of the effective mode (:2931-2991): present unless
		// the transition or the schedule names a preset, allowed only where
		// the mode heats or cools, within the limits, and a deadband apart.
		mode := sc.SystemMode
		if t.SystemMode != nil {
			mode = *t.SystemMode
		}
		heats := mode == tdef.SystemModeHeat || mode == tdef.SystemModeAuto
		cools := mode == tdef.SystemModeCool || mode == tdef.SystemModeAuto
		if t.PresetHandle == nil && sc.PresetHandle == nil {
			if heats && t.HeatingSetpoint == nil {
				return constraint(i, "SystemMode %d needs a HeatingSetpoint", mode)
			}
			if cools && t.CoolingSetpoint == nil {
				return constraint(i, "SystemMode %d needs a CoolingSetpoint", mode)
			}
		}
		if t.CoolingSetpoint != nil && (!cools || int(*t.CoolingSetpoint) < coolLo || int(*t.CoolingSetpoint) > coolHi) {
			return constraint(i, "CoolingSetpoint %d is outside SystemMode %d or the cooling limits %d to %d", *t.CoolingSetpoint, mode, coolLo, coolHi)
		}
		if t.HeatingSetpoint != nil && (!heats || int(*t.HeatingSetpoint) < heatLo || int(*t.HeatingSetpoint) > heatHi) {
			return constraint(i, "HeatingSetpoint %d is outside SystemMode %d or the heating limits %d to %d", *t.HeatingSetpoint, mode, heatLo, heatHi)
		}
		if t.HeatingSetpoint != nil && t.CoolingSetpoint != nil && int(*t.CoolingSetpoint)-int(*t.HeatingSetpoint) < r.st.deadband {
			return constraint(i, "the setpoints are closer than the deadband %d", r.st.deadband)
		}
	}
	return nil
}

// validateTransitionsPerDay is the per-day count of
// #validateScheduleTransitions (ThermostatServer.ts:2994-3002 at
// b25ed960), after every transition passed: with a
// NumberOfScheduleTransitionPerDay, no day carries more transitions; null
// is no limit.
func (r scheduleRules) validateTransitionsPerDay(sc Schedule) error {
	if r.perDay == nil {
		return nil
	}
	for bit := range 7 {
		day := tdef.ScheduleDayOfWeekBitmap(1 << bit)
		n := 0
		for _, t := range sc.Transitions {
			if t.DayOfWeek&day != 0 {
				n++
			}
		}
		if n > int(*r.perDay) {
			return spec.Errorf(im.StatusResourceExhausted, "thermo: schedule: %d transitions on day 0x%02X exceed NumberOfScheduleTransitionPerDay %d", n, day, *r.perDay)
		}
	}
	return nil
}

// validateScheduleCommit is matter.js #validateScheduleCommit
// (ThermostatServer.ts:3056-3086 at b25ed960): a built-in schedule the new
// list drops is CONSTRAINT_ERROR, the active schedule it drops
// INVALID_IN_STATE — only if the active handle names a stored schedule
// (:3073-3085, the clause :3077-3080). A handle that names no stored
// schedule is not kept here — the stores clear one their list drops
// ([ThermostatServer.commitSchedules], [ThermostatServer.SetSchedules],
// R4-fsm-4253) —, so the clause only keeps such a handle from blocking a
// commit.
func validateScheduleCommit(stored, next []Schedule, active []byte) error {
	for _, sc := range stored {
		if sc.BuiltIn.Null || !sc.BuiltIn.Value || sc.ScheduleHandle.Null {
			continue
		}
		if !scheduleHandleIn(next, sc.ScheduleHandle.Value) {
			return spec.Errorf(im.StatusConstraintError, "thermo: cannot remove built-in schedule %s", handleKey(sc.ScheduleHandle.Value))
		}
	}
	if active != nil && !scheduleHandleIn(next, active) && scheduleHandleIn(stored, active) {
		return spec.Errorf(im.StatusInvalidInState, "thermo: cannot remove schedule %s while it is the active schedule", handleKey(active))
	}
	return nil
}

// issueScheduleHandles gives every schedule without a handle a new one,
// 16 random bytes — matter.js #normalizeSchedules
// (ThermostatServer.ts:3028-3040 at b25ed960).
func issueScheduleHandles(list []Schedule) error {
	for i := range list {
		if !list[i].ScheduleHandle.Null {
			continue
		}
		h := make([]byte, scheduleHandleLength)
		if _, err := rand.Read(h); err != nil {
			return fmt.Errorf("thermo: issue a schedule handle: %w", err)
		}
		list[i].ScheduleHandle = spec.ValueOf(h)
	}
	return nil
}

// resolveScheduleBuiltIns gives every schedule of list with a null BuiltIn
// the BuiltIn of the stored schedule with its handle, false without one.
// Mirrors matter.js
// packages/node/src/behaviors/thermostat/ThermostatServer.ts:3041-3047 at
// b25ed960
// (#normalizeSchedules), after the handles are issued.
func resolveScheduleBuiltIns(stored, list []Schedule) {
	for i := range list {
		if !list[i].BuiltIn.Null {
			continue
		}
		list[i].BuiltIn = spec.ValueOf(false)
		if list[i].ScheduleHandle.Null {
			continue
		}
		if match, ok := scheduleWithHandle(stored, list[i].ScheduleHandle.Value); ok && !match.BuiltIn.Null {
			list[i].BuiltIn = match.BuiltIn
		}
	}
}

// writeSchedules is a controller's write of Schedules: only the peer
// holding an atomic write of it may write, another peer's write is
// INVALID_IN_STATE where Presets answers BUSY (AtomicWriteHandler.ts:54-56,
// :474-479 at b25ed960); no atomic write is INVALID_IN_STATE (:467-469).
// The list is validated against the stored schedules and the peer's own
// pending Presets — schedules$AtomicChanging (ThermostatServer.ts:2581-2591)
// — and staged as written, a null BuiltIn kept null: matter.js keeps the
// written value pending unchanged (AtomicWriteHandler.ts:272 at b25ed960),
// checked, not normalized (ThermostatServer.ts:3004-3007), and resolves
// it when it stores the value ([ThermostatServer.commitSchedules]).
func (s *ThermostatServer) writeSchedules(ctx context.Context, v any) error {
	list, err := schedulesValue(v)
	if err != nil {
		return err
	}
	return s.stageAtomicAttribute(ctx, tdef.AttrSchedules, im.StatusInvalidInState, func() (any, error) {
		s.mu.RLock()
		presets := s.presets
		if v, ok := s.pendingAtomicValue(ctx, tdef.AttrPresets); ok {
			if pending, isList := v.([]Preset); isList {
				presets = pending
			}
		}
		stored, rules := s.schedules, s.scheduleRules(presets)
		s.mu.RUnlock()
		if _, err := rules.validateSchedules(stored, list); err != nil {
			return nil, err
		}
		return deepcopy.Each(list), nil
	})
}

// schedulesTLVEqual reports whether a and b encode alike.
func schedulesTLVEqual(a, b []Schedule) bool {
	enc := func(l []Schedule) []byte {
		e := tlv.NewEncoder()
		spec.List[Schedule](l).EncodeTLV(e, tlv.AnonymousTag())
		out, _ := e.Bytes()
		return out
	}
	return bytes.Equal(enc(a), enc(b))
}

// commitSchedules stores the schedules an atomic write staged, after
// precommitAtomic passed: every new schedule gets a handle
// ([issueScheduleHandles]) and a null BuiltIn is resolved against the
// stored schedules ([resolveScheduleBuiltIns]) — matter.js
// #normalizeSchedules at pre-commit (ThermostatServer.ts:3028-3049 at
// b25ed960) —, the [SchedulePort] stores the list — its error fails the
// commit — and the list is stored and reported. The port may confirm the
// list through [ThermostatServer.SetSchedules], the host's replace. An
// active handle the stored list no longer holds is cleared at the store,
// as [ThermostatServer.SetSchedules] clears one: the commit's check ran
// before the port, and a host's [ThermostatServer.SetActiveScheduleHandle]
// takes no s.op, so it may name a schedule the commit drops
// (R4-fsm-4253). The caller holds s.op.
func (s *ThermostatServer) commitSchedules(ctx context.Context, staged []Schedule) error {
	list := deepcopy.Each(staged)
	if err := issueScheduleHandles(list); err != nil {
		return err
	}
	s.mu.RLock()
	resolveScheduleBuiltIns(s.schedules, list)
	s.mu.RUnlock()
	if s.schedulePort != nil {
		if err := s.schedulePort.CommitSchedules(ctx, deepcopy.Each(list)); err != nil {
			return err
		}
	}
	s.mu.Lock()
	var changed []uint32
	if !schedulesTLVEqual(s.schedules, list) {
		changed = append(changed, tdef.AttrSchedules)
	}
	s.schedules = list
	var dropped []byte
	if s.activeScheduleHandle != nil && !scheduleHandleIn(list, s.activeScheduleHandle) {
		dropped = s.activeScheduleHandle
		s.activeScheduleHandle = nil
		changed = append(changed, tdef.AttrActiveScheduleHandle)
	}
	s.mu.Unlock()
	if len(changed) > 0 {
		s.changes.NotifyFrom(contract.OriginFrom(ctx), changed...)
	}
	s.emitActiveScheduleChange(ctx, dropped, nil)
	return nil
}

// presetsInUseBySchedules is the schedules half of matter.js
// #validatePresetCommit (ThermostatServer.ts:2032-2080; its schedules half :2048-2065 at b25ed960): with
// MSCH, a preset the new list drops that a schedule or a schedule
// transition of schedules names is INVALID_IN_STATE. schedules are the
// ones of the change's own transaction: the Schedules the same commit
// staged when they passed, the stored ones otherwise (:2051 reads
// persistedSchedules).
func (s *ThermostatServer) presetsInUseBySchedules(old, next []Preset, schedules []Schedule) error {
	if s.features&ThermostatFeatureMSCH == 0 {
		return nil
	}
	for _, p := range old {
		if p.PresetHandle.Null || slices.ContainsFunc(next, func(n Preset) bool {
			return !n.PresetHandle.Null && bytes.Equal(n.PresetHandle.Value, p.PresetHandle.Value)
		}) {
			continue
		}
		h := p.PresetHandle.Value
		for _, sc := range schedules {
			used := sc.PresetHandle != nil && bytes.Equal(*sc.PresetHandle, h)
			for _, t := range sc.Transitions {
				used = used || (t.PresetHandle != nil && bytes.Equal(*t.PresetHandle, h))
			}
			if used {
				return spec.Errorf(im.StatusInvalidInState, "thermo: cannot remove preset %s while a schedule uses it", handleKey(h))
			}
		}
	}
	return nil
}
