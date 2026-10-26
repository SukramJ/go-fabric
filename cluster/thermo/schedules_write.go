// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package thermo

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"slices"

	"github.com/SukramJ/go-fabric/cluster/spec"
	tdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// The write of Schedules (MSCH) through AtomicRequest: chip's
// AppendPendingSchedule validation, PrecommitSchedules and the commit
// (ThermostatClusterSchedules.cpp at 645b267a), on the atomic-write state
// of atomic.go.

// scheduleHandleLength is the length of a handle the server issues to a
// new schedule, and the longest handle chip accepts
// (ScheduleStructWithOwnedMembers.h:39 kScheduleHandleSize = 16 at
// 645b267a; the schema's "max 16").
const scheduleHandleLength = 16

// scheduleNameLength is the longest schedule name (chip
// ScheduleStructWithOwnedMembers.h:41 kScheduleNameSize = 64; the schema's
// "max 64").
const scheduleNameLength = 64

// validScheduleDays are the weekday bits a transition may carry: Sunday to
// Saturday, not Away (ThermostatClusterSchedules.cpp:300-304).
const validScheduleDays = tdef.ScheduleDayOfWeekSunday | tdef.ScheduleDayOfWeekMonday | tdef.ScheduleDayOfWeekTuesday |
	tdef.ScheduleDayOfWeekWednesday | tdef.ScheduleDayOfWeekThursday | tdef.ScheduleDayOfWeekFriday |
	tdef.ScheduleDayOfWeekSaturday

// scheduleRules is what the pending-schedule validation reads besides the
// schedules: the feature selection, the schedule types and counts, the
// setpoint limits and deadband, and whether a preset handle names a
// preset.
type scheduleRules struct {
	features      uint32
	types         []ScheduleType
	maxSchedules  int
	maxTotal      int
	perDay        *uint8
	st            setpointState
	presetPresent func(handle []byte) bool
}

// scheduleRules gathers the rules; the caller holds s.mu. presetPresent
// answers for the presets a Presets atomic write staged, when one holds
// it, and for the stored presets otherwise — chip's
// IsPresetHandlePresentInPresetsOrPending (ThermostatClusterPresets.cpp:480-491
// at 645b267a); without PRES no handle is present
// (ThermostatClusterBase.h:99-103).
func (s *ThermostatServer) scheduleRules() scheduleRules {
	presets := s.presets
	if st := s.atomic.forAttribute(s.clk.Now(), tdef.AttrPresets); st != nil {
		if v, ok := st.staged(tdef.AttrPresets); ok {
			if staged, isList := v.([]Preset); isList {
				presets = staged
			}
		}
	}
	pres := s.features&ThermostatFeaturePRES != 0
	return scheduleRules{
		features:     s.features,
		types:        s.scheduleTypes,
		maxSchedules: int(s.numberOfSchedules),
		maxTotal:     int(s.numberOfScheduleTransitions),
		perDay:       s.transitionsPerDay,
		st:           s.setpointState(),
		presetPresent: func(h []byte) bool {
			return pres && slices.ContainsFunc(presets, func(p Preset) bool {
				return !p.PresetHandle.Null && bytes.Equal(p.PresetHandle.Value, h)
			})
		},
	}
}

// scheduleType is the ScheduleTypeStruct for mode, if any — chip
// MaximumScheduleTypeCount and GetScheduleTypeFeatures
// (ThermostatClusterSchedules.cpp:189-216, :258-282).
func (r scheduleRules) scheduleType(mode tdef.SystemModeEnum) (ScheduleType, bool) {
	for _, t := range r.types {
		if t.SystemMode == mode {
			return t, true
		}
	}
	return ScheduleType{}, false
}

// heatLimits and coolLimits are chip's Setpoints::GetLimits
// (Setpoints.cpp:41-59 at 645b267a): the user limits, with AUTO the
// heating maximum held a deadband below the cooling maximum and the
// cooling minimum a deadband above the heating minimum.
func (r scheduleRules) heatLimits() (lo, hi int) {
	lo, hi = r.st.minHeat, r.st.maxHeat
	if r.st.auto {
		hi = min(hi, r.st.maxCool-r.st.deadband)
	}
	return lo, hi
}

func (r scheduleRules) coolLimits() (lo, hi int) {
	lo, hi = r.st.minCool, r.st.maxCool
	if r.st.auto {
		lo = max(lo, r.st.minHeat+r.st.deadband)
	}
	return lo, hi
}

// appendPending validates sc for appending to pending — chip
// ThermostatSchedules::AppendPendingSchedule
// (ThermostatClusterSchedules.cpp:795-998 at 645b267a) — and returns it
// normalised (a null BuiltIn resolved). stored is the Schedules attribute.
func (r scheduleRules) appendPending(stored, pending []Schedule, sc Schedule) (Schedule, error) {
	constraint := func(format string, args ...any) error {
		return spec.Errorf(im.StatusConstraintError, "thermo: schedule: "+format, args...)
	}
	exhausted := func(format string, args ...any) error {
		return spec.Errorf(im.StatusResourceExhausted, "thermo: schedule: "+format, args...)
	}
	// Field sizes first (:798-810, :817-823).
	if !sc.ScheduleHandle.Null && len(sc.ScheduleHandle.Value) > scheduleHandleLength {
		return sc, constraint("handle longer than %d bytes", scheduleHandleLength)
	}
	if sc.Name != nil && len(*sc.Name) > scheduleNameLength {
		return sc, constraint("name longer than %d bytes", scheduleNameLength)
	}
	if sc.PresetHandle != nil && len(*sc.PresetHandle) > scheduleHandleLength {
		return sc, constraint("preset handle longer than %d bytes", scheduleHandleLength)
	}
	for _, t := range sc.Transitions {
		if t.PresetHandle != nil && len(*t.PresetHandle) > scheduleHandleLength {
			return sc, constraint("transition preset handle longer than %d bytes", scheduleHandleLength)
		}
	}
	// IsValidScheduleEntry: SystemMode Auto, Cool or Heat (:46-63, :835-838).
	switch sc.SystemMode {
	case tdef.SystemModeAuto, tdef.SystemModeCool, tdef.SystemModeHeat:
	default:
		return sc, constraint("SystemMode %d is not Auto, Cool or Heat", sc.SystemMode)
	}
	if sc.ScheduleHandle.Null {
		// A new schedule cannot claim to be built in (:840-847).
		if !sc.BuiltIn.Null && sc.BuiltIn.Value {
			return sc, constraint("a new schedule cannot be built in")
		}
		sc.BuiltIn = spec.ValueOf(false)
	} else {
		// (a) an existing schedule has the handle (:851-864) …
		match, ok := scheduleWithHandle(stored, sc.ScheduleHandle.Value)
		if !ok {
			return sc, spec.Errorf(im.StatusNotFound, "thermo: schedule: handle %s names no schedule", handleKey(sc.ScheduleHandle.Value))
		}
		// (b) … no pending schedule has it yet (:866-873) …
		if _, dup := scheduleWithHandle(pending, sc.ScheduleHandle.Value); dup {
			return sc, constraint("handle %s is already in the list", handleKey(sc.ScheduleHandle.Value))
		}
		// (c)/(d) … and BuiltIn matches the stored schedule's (:875-899).
		if match.BuiltIn.Null {
			return sc, spec.Errorf(im.StatusInvalidInState, "thermo: schedule: stored schedule %s has no BuiltIn", handleKey(sc.ScheduleHandle.Value))
		}
		if sc.BuiltIn.Null {
			sc.BuiltIn = match.BuiltIn
		} else if sc.BuiltIn.Value != match.BuiltIn.Value {
			return sc, constraint("BuiltIn differs from the stored schedule's")
		}
	}
	// The schedule type of the SystemMode (:902-913).
	typ, ok := r.scheduleType(sc.SystemMode)
	if !ok || typ.NumberOfSchedules == 0 {
		return sc, constraint("no schedule type for SystemMode %d", sc.SystemMode)
	}
	feats := typ.ScheduleTypeFeatures
	if sc.Name != nil && feats&tdef.ScheduleTypeFeaturesSupportsNames == 0 {
		return sc, constraint("the schedule type does not support names")
	}
	if sc.PresetHandle != nil {
		if feats&tdef.ScheduleTypeFeaturesSupportsPresets == 0 {
			return sc, constraint("the schedule type does not support presets")
		}
		if !r.presetPresent(*sc.PresetHandle) {
			return sc, constraint("preset handle %s names no preset", handleKey(*sc.PresetHandle))
		}
	}
	if err := r.validateTransitions(sc, feats); err != nil {
		return sc, err
	}
	if err := r.validateTransitionsPerDay(sc); err != nil {
		return sc, err
	}
	// The counts with sc appended (:945-993).
	count, typeCount, total := 1, 1, len(sc.Transitions)
	for _, o := range pending {
		count++
		total += len(o.Transitions)
		if o.SystemMode == sc.SystemMode {
			typeCount++
		}
	}
	switch {
	case count > r.maxSchedules:
		return sc, exhausted("%d schedules exceed NumberOfSchedules %d", count, r.maxSchedules)
	case typeCount > int(typ.NumberOfSchedules):
		return sc, exhausted("%d schedules of SystemMode %d exceed its type's %d", typeCount, sc.SystemMode, typ.NumberOfSchedules)
	case total > r.maxTotal:
		return sc, exhausted("%d transitions exceed NumberOfScheduleTransitions %d", total, r.maxTotal)
	}
	return sc, nil
}

// validateTransitions is chip ValidateScheduleTransitions
// (ThermostatClusterSchedules.cpp:292-445 at 645b267a).
func (r scheduleRules) validateTransitions(sc Schedule, feats tdef.ScheduleTypeFeaturesBitmap) error { //nolint:gocognit,gocyclo,cyclop,funlen // one rule per chip check, in chip's order
	constraint := func(i int, format string, args ...any) error {
		return spec.Errorf(im.StatusConstraintError, "thermo: schedule transition %d: "+format, append([]any{i}, args...)...)
	}
	if len(sc.Transitions) == 0 {
		return spec.Errorf(im.StatusConstraintError, "thermo: schedule has no transitions")
	}
	heatLo, heatHi := r.heatLimits()
	coolLo, coolHi := r.coolLimits()
	for i, t := range sc.Transitions {
		if t.DayOfWeek == 0 || t.DayOfWeek&^validScheduleDays != 0 {
			return constraint(i, "DayOfWeek 0x%02X has no weekday or a day outside Sunday to Saturday", t.DayOfWeek)
		}
		if t.TransitionTime > 1439 {
			return constraint(i, "TransitionTime %d is beyond 1439", t.TransitionTime)
		}
		for j := range i {
			if p := sc.Transitions[j]; p.TransitionTime == t.TransitionTime && p.DayOfWeek&t.DayOfWeek != 0 {
				return constraint(i, "repeats transition %d's time on a shared day", j)
			}
		}
		setpointFields := t.SystemMode != nil || t.CoolingSetpoint != nil || t.HeatingSetpoint != nil
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
		if setpointFields && feats&tdef.ScheduleTypeFeaturesSupportsSetpoints == 0 {
			return constraint(i, "the schedule type does not support setpoints")
		}
		if t.SystemMode != nil {
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
		if t.HeatingSetpoint != nil && (!heats || int(*t.HeatingSetpoint) < heatLo || int(*t.HeatingSetpoint) > heatHi) {
			return constraint(i, "HeatingSetpoint %d is outside SystemMode %d or the heating limits %d to %d", *t.HeatingSetpoint, mode, heatLo, heatHi)
		}
		if t.CoolingSetpoint != nil && (!cools || int(*t.CoolingSetpoint) < coolLo || int(*t.CoolingSetpoint) > coolHi) {
			return constraint(i, "CoolingSetpoint %d is outside SystemMode %d or the cooling limits %d to %d", *t.CoolingSetpoint, mode, coolLo, coolHi)
		}
		if t.HeatingSetpoint != nil && t.CoolingSetpoint != nil && int(*t.CoolingSetpoint)-int(*t.HeatingSetpoint) < r.st.deadband {
			return constraint(i, "the setpoints are closer than the deadband")
		}
	}
	return nil
}

// validateTransitionsPerDay is chip ValidateTransitionsPerDay
// (ThermostatClusterSchedules.cpp:457-488 at 645b267a): with a
// NumberOfScheduleTransitionPerDay, no day — Away included — carries more
// transitions; null is no limit.
func (r scheduleRules) validateTransitionsPerDay(sc Schedule) error {
	if r.perDay == nil {
		return nil
	}
	for bit := range 8 {
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

// pendingSchedules validates list as the pending schedules, entry by entry
// in its order — chip WriteAttribute's ReplaceAll, which clears the
// pending list and appends every entry through AppendPendingSchedule
// (ThermostatClusterSchedules.cpp:568-586). The dispatcher hands an
// AppendItem over as the staged list with the item after it, whose
// re-validation from the start comes to the same result (an entry is
// validated against the entries before it only). It returns the
// normalised list.
func (r scheduleRules) pendingSchedules(stored, list []Schedule) ([]Schedule, error) {
	pending := make([]Schedule, 0, len(list))
	for _, sc := range cloneSchedules(list) {
		n, err := r.appendPending(stored, pending, sc)
		if err != nil {
			return nil, err
		}
		pending = append(pending, n)
	}
	return pending, nil
}

// precommitSchedules is chip ThermostatSchedules::PrecommitSchedules
// (ThermostatClusterSchedules.cpp:1000-1072 at 645b267a): a built-in
// schedule the pending list dropped is CONSTRAINT_ERROR, and an active
// schedule it dropped INVALID_IN_STATE.
func precommitSchedules(stored, pending []Schedule, active []byte) error {
	for _, sc := range stored {
		if sc.BuiltIn.Null || !sc.BuiltIn.Value || sc.ScheduleHandle.Null {
			continue
		}
		if _, ok := scheduleWithHandle(pending, sc.ScheduleHandle.Value); !ok {
			return spec.Errorf(im.StatusConstraintError, "thermo: cannot remove built-in schedule %s", handleKey(sc.ScheduleHandle.Value))
		}
	}
	if active != nil {
		if _, ok := scheduleWithHandle(pending, active); !ok {
			return spec.Errorf(im.StatusInvalidInState, "thermo: cannot remove schedule %s while it is the active schedule", handleKey(active))
		}
	}
	return nil
}

// issueScheduleHandles gives every schedule without a handle a new one:
// chip leaves it to the delegate's CommitPendingSchedules
// (ThermostatClusterSchedules.h:136-147 at 645b267a: "it is the
// responsibility of this API to allocate unique schedule handles"); here
// the server issues 16 random bytes, as matter.js does for a preset
// (ThermostatServer.ts:1526 at 862ad75f).
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

// writeSchedules is a controller's write of Schedules: only the peer
// holding an atomic write of it may write, any other request is
// INVALID_IN_STATE — chip WriteAttribute (ThermostatClusterSchedules.cpp:555-566
// at 645b267a), which answers another peer INVALID_IN_STATE too, where the
// Presets write answers it BUSY. The list is validated as the pending
// schedules ([scheduleRules.pendingSchedules]) and staged.
func (s *ThermostatServer) writeSchedules(ctx context.Context, v any) error {
	list, err := schedulesValue(v)
	if err != nil {
		return err
	}
	return s.stageAtomicAttribute(ctx, tdef.AttrSchedules, im.StatusInvalidInState, func() (any, error) {
		s.mu.RLock()
		stored, rules := s.schedules, s.scheduleRules()
		s.mu.RUnlock()
		return rules.pendingSchedules(stored, list)
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
// [precommitSchedules] passed: every new schedule gets a handle, the
// [SchedulePort] stores the list — chip OnAtomicWriteCommit,
// CommitPendingSchedules (ThermostatClusterSchedules.cpp:639-651 at
// 645b267a), whose error fails the commit — and the list is stored and
// reported. The caller holds s.op.
func (s *ThermostatServer) commitSchedules(ctx context.Context, staged []Schedule) error {
	list := cloneSchedules(staged)
	if err := issueScheduleHandles(list); err != nil {
		return err
	}
	if s.schedulePort != nil {
		if err := s.schedulePort.CommitSchedules(ctx, cloneSchedules(list)); err != nil {
			return err
		}
	}
	s.mu.Lock()
	changed := !schedulesTLVEqual(s.schedules, list)
	s.schedules = list
	s.mu.Unlock()
	if changed {
		s.changes.NotifyFrom(contract.OriginFrom(ctx), tdef.AttrSchedules)
	}
	return nil
}

// presetsInUseBySchedules is chip's PrecommitPresets check
// (ThermostatClusterPresets.cpp:708-724 at 645b267a, with
// ThermostatSchedules::IsPresetHandleInUse, ThermostatClusterSchedules.cpp:706-739):
// with MSCH, a preset the new list drops that a schedule or a schedule
// transition names is INVALID_IN_STATE. The schedules are the ones a
// Schedules atomic write staged, when one holds it, and the stored ones
// otherwise. The caller holds s.mu (read).
func (s *ThermostatServer) presetsInUseBySchedules(old, next []Preset) error {
	if s.features&ThermostatFeatureMSCH == 0 {
		return nil
	}
	schedules := s.schedules
	if st := s.atomic.forAttribute(s.clk.Now(), tdef.AttrSchedules); st != nil {
		if v, ok := st.staged(tdef.AttrSchedules); ok {
			if staged, isList := v.([]Schedule); isList {
				schedules = staged
			}
		}
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
