// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package thermo

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/SukramJ/go-fabric/cluster/spec"
	tdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	"github.com/SukramJ/go-fabric/contract"
)

// The typed server's optional interfaces ([spec.AttributeSetter],
// [spec.CommandRegistrar], [spec.EventEmitting], [spec.BeforeWriteSetter]);
// [ThermostatUIServer] takes them from its embedded [spec.Server].
var (
	_ spec.AttributeSetter   = (*ThermostatServer)(nil)
	_ spec.CommandRegistrar  = (*ThermostatServer)(nil)
	_ spec.EventEmitting     = (*ThermostatServer)(nil)
	_ spec.BeforeWriteSetter = (*ThermostatServer)(nil)
	_ contract.EventReceiver = (*ThermostatServer)(nil)

	_ spec.AttributeSetter   = (*ThermostatUIServer)(nil)
	_ spec.CommandRegistrar  = (*ThermostatUIServer)(nil)
	_ spec.EventEmitting     = (*ThermostatUIServer)(nil)
	_ spec.BeforeWriteSetter = (*ThermostatUIServer)(nil)
)

// setpointKeys names each setpoint and user limit as the reconcile's
// "changed" set does, and whether it is a heating one.
var setpointKeys = map[uint32]struct {
	key                     string
	heat, limit, unoccupied bool
}{
	thermoAttrOccupiedHeatingSetpoint:   {spOccupiedHeating, true, false, false},
	thermoAttrOccupiedCoolingSetpoint:   {spOccupiedCooling, false, false, false},
	thermoAttrUnoccupiedHeatingSetpoint: {spUnoccHeating, true, false, true},
	thermoAttrUnoccupiedCoolingSetpoint: {spUnoccCooling, false, false, true},
	thermoAttrMinHeatSetpointLimit:      {spMinHeatLimit, true, true, false},
	thermoAttrMaxHeatSetpointLimit:      {spMaxHeatLimit, true, true, false},
	thermoAttrMinCoolSetpointLimit:      {spMinCoolLimit, false, true, false},
	thermoAttrMaxCoolSetpointLimit:      {spMaxCoolLimit, false, true, false},
}

// SetAttributes implements [spec.AttributeSetter]: the host's own change,
// all or nothing, each value checked against its attribute's type,
// constraint and the selection's enum values, stored without the [Sink]
// and reported — no write veto. LocalTemperature (null included),
// LocalTemperatureCalibration, SystemMode and, with OCC, Occupancy are
// stored as given. A setpoint or user setpoint limit is held to its
// limits as a write is and runs the reconcile a write runs (matter.js #reconcileSetpoints on the attribute's $Changing),
// so a companion may move with it and is reported too; an absolute limit
// is held to the user limits as [ThermostatServer.SetAbsoluteSetpointLimit]
// holds it. A combination the reconcile cannot fit is refused and nothing
// is stored. A changed occupied (unoccupied) setpoint ends the active
// preset in effect, as a local direct change does (ThermostatServer.ts:744-768
// at 862ad75f).
//
// Presets and ActivePresetHandle, which the host owns too, go through
// [ThermostatServer.SetPresets] and [ThermostatServer.SetActivePresetHandle]
// — validated against the preset rules and persisted through the
// [PresetStore] under the command queue — before the other attributes, in
// that order: a refusal there leaves the other attributes unset (a
// refused handle after accepted presets keeps the presets).
// ControlSequenceOfOperation is derived from the features, and
// MinSetpointDeadBand, PresetTypes and NumberOfPresets are the
// configuration's: [spec.ErrNotHeld] for a value other than the one they
// read (any value for the two lists).
func (s *ThermostatServer) SetAttributes(values map[uint32]any) error {
	checked, presets, err := s.checkSet(values)
	if err != nil {
		return err
	}
	if err := s.setPresetAttributes(presets); err != nil {
		return err
	}
	if len(checked) == 0 {
		return nil
	}
	s.mu.Lock()
	st := s.setpointState()
	setpoints, err := st.applySet(checked)
	if err != nil {
		s.mu.Unlock()
		return err
	}

	var changed []uint32
	note := func(id uint32, moved bool) {
		if moved {
			changed = append(changed, id)
		}
	}
	for _, f := range []struct {
		dst *int16
		v   int
		id  uint32
	}{
		{&s.absMinHeat, st.absMinHeat, thermoAttrAbsMinHeatSetpointLimit},
		{&s.absMaxHeat, st.absMaxHeat, thermoAttrAbsMaxHeatSetpointLimit},
		{&s.absMinCool, st.absMinCool, thermoAttrAbsMinCoolSetpointLimit},
		{&s.absMaxCool, st.absMaxCool, thermoAttrAbsMaxCoolSetpointLimit},
	} {
		note(f.id, int(*f.dst) != f.v)
		*f.dst = int16(f.v) //nolint:gosec // an int16 given
	}
	moved := s.commitSetpoints(st)
	changed = append(changed, moved...)
	if v, ok := checked[thermoAttrLocalTemperature]; ok {
		var t *int16
		if n, isInt := v.(int16); isInt {
			t = &n
		}
		note(thermoAttrLocalTemperature, !equalTemperature(s.localTemp, t))
		s.localTemp = t
	}
	if n, ok := checked[thermoAttrLocalTemperatureCalibration].(int8); ok {
		note(thermoAttrLocalTemperatureCalibration, s.localTempCalib != int16(n))
		s.localTempCalib = int16(n)
	}
	if n, ok := checked[thermoAttrSystemMode].(uint8); ok {
		note(thermoAttrSystemMode, s.systemMode != n)
		s.systemMode = n
	}
	if n, ok := checked[thermoAttrOccupancy].(uint8); ok {
		occupied := n&1 != 0
		note(thermoAttrOccupancy, s.occupied != occupied)
		s.occupied = occupied
	}
	s.mu.Unlock()

	if len(changed) > 0 {
		slices.Sort(changed)
		s.changes.Notify(changed...)
	}
	if setpoints {
		for _, id := range moved {
			switch id {
			case thermoAttrOccupiedHeatingSetpoint, thermoAttrOccupiedCoolingSetpoint:
				s.clearActivePresetOnDirectWrite(context.Background(), false)
			case thermoAttrUnoccupiedHeatingSetpoint, thermoAttrUnoccupiedCoolingSetpoint:
				s.clearActivePresetOnDirectWrite(context.Background(), true)
			}
		}
	}
	return nil
}

// presetValues are the Presets and ActivePresetHandle a
// [ThermostatServer.SetAttributes] sets, each when given.
type presetValues struct {
	presets    []Preset
	hasPresets bool
	handle     []byte
	hasHandle  bool
}

// setPresetAttributes applies p through the preset setters, Presets first.
func (s *ThermostatServer) setPresetAttributes(p presetValues) error {
	ctx := context.Background()
	if p.hasPresets {
		if err := s.SetPresets(ctx, p.presets); err != nil {
			return err
		}
	}
	if p.hasHandle {
		return s.SetActivePresetHandle(ctx, p.handle)
	}
	return nil
}

// checkSet checks every value of a [ThermostatServer.SetAttributes] and
// returns the held ones in their stored form, the preset attributes apart.
func (s *ThermostatServer) checkSet(values map[uint32]any) (map[uint32]any, presetValues, error) {
	var presets presetValues
	checked := make(map[uint32]any, len(values))
	for _, id := range slices.Sorted(maps.Keys(values)) {
		v, err := s.inst.CheckSet(id, values[id], s.MatterRead)
		if err != nil {
			return nil, presets, err
		}
		switch id {
		case tdef.AttrPresets:
			list, err := presetsValue(v)
			if err != nil {
				return nil, presets, err
			}
			presets.presets, presets.hasPresets = list, true
			continue
		case tdef.AttrActivePresetHandle:
			handle, _ := v.([]byte) // octstr per CheckSet; nil is null
			presets.handle, presets.hasHandle = handle, true
			continue
		case tdef.AttrPresetTypes, tdef.AttrNumberOfPresets:
			return nil, presets, fmt.Errorf("%w: thermostat: attribute 0x%04X is the configuration's", spec.ErrNotHeld, id)
		case thermoAttrControlSequenceOfOperation, thermoAttrMinSetpointDeadBand:
			if cur, _ := s.MatterRead(id); v != cur {
				return nil, presets, fmt.Errorf("%w: thermostat: attribute 0x%04X is fixed at %v", spec.ErrNotHeld, id, cur)
			}
			continue
		}
		checked[id] = v
	}
	return checked, presets, nil
}

// applySet puts the absolute limits, user limits and setpoints checked
// holds onto st: each setpoint or user limit held to its limits as a
// write is (#assertSetpointWithinLimits, #assertLimitWithinAbs) and
// reconciled, the absolute limits held to the user limits
// (#assertUserSetpointLimits). setpoints reports whether a setpoint or
// user limit was given.
func (st *setpointState) applySet(checked map[uint32]any) (setpoints bool, err error) {
	absHeat, absCool := false, false
	for _, id := range slices.Sorted(maps.Keys(checked)) {
		n, isInt := checked[id].(int16)
		switch id {
		case thermoAttrAbsMinHeatSetpointLimit:
			st.absMinHeat, absHeat = int(n), true
		case thermoAttrAbsMaxHeatSetpointLimit:
			st.absMaxHeat, absHeat = int(n), true
		case thermoAttrAbsMinCoolSetpointLimit:
			st.absMinCool, absCool = int(n), true
		case thermoAttrAbsMaxCoolSetpointLimit:
			st.absMaxCool, absCool = int(n), true
		default:
			k, ok := setpointKeys[id]
			if !ok || !isInt {
				continue
			}
			assert := st.assertSetpointWithinLimits(k.heat, k.unoccupied, int(n))
			if k.limit {
				assert = st.assertLimitWithinAbs(k.heat, int(n))
			}
			if assert != nil {
				return false, fmt.Errorf("%w: %w", spec.ErrInvalidValue, assert)
			}
			st.assign(id, int(n))
			if err := st.reconcile(k.key); err != nil {
				return false, err
			}
			setpoints = true
		}
	}
	if absHeat {
		if err := st.assertUserSetpointLimits(true); err != nil {
			return false, err
		}
	}
	if absCool {
		if err := st.assertUserSetpointLimits(false); err != nil {
			return false, err
		}
	}
	return setpoints, nil
}

func equalTemperature(a, b *int16) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// Handle implements [spec.CommandRegistrar]: fn replaces the server's own
// handling of cmdID — the request's checks, the setpoint reconcile, the
// presets and the [Sink] — as a matter.js host overrides the
// ThermostatServer command method; fn runs outside the server's queue.
// nil hands the command back. A command the selection does not accept
// (SetActivePresetRequest and AtomicRequest without PRES) is refused with
// [spec.ErrNotAccepted].
func (s *ThermostatServer) Handle(cmdID uint32, fn spec.CommandHandler) error {
	return s.host.Register(s.inst, cmdID, fn)
}

// Emit implements [spec.EventEmitting]: an event of the selection's
// EventList ([spec.ErrNotEmitted] otherwise — the server serves none of
// the event feature TEVT's events).
func (s *ThermostatServer) Emit(endpoint uint16, eventID uint32, data any) error {
	return s.events.Emit(s.inst, endpoint, eventID, data)
}

// SetMatterEventEmitter implements [contract.EventReceiver].
func (s *ThermostatServer) SetMatterEventEmitter(emitter contract.EventEmitter) {
	s.events.SetEmitter(emitter)
}

// SetBeforeWrite implements [spec.BeforeWriteSetter]: fn vetoes a
// controller's write after the definition's checks and before the
// server's rules, the [Sink] and the store, with the value in its stored
// form (an int16 temperature, an int8 calibration or deadband, a uint8
// enum, the presets as a []Preset).
func (s *ThermostatServer) SetBeforeWrite(fn func(ctx context.Context, attrID uint32, value any) error) {
	s.veto.Set(fn)
}

// storedWrite is a value [spec.Instance.ValidateWrite] normalised (uint64,
// int64) in the Go type of its attribute's width, the form the write veto
// sees.
func storedWrite(attrID uint32, v any) any {
	a := tdef.Definition.Attribute(attrID)
	if a == nil {
		return v
	}
	switch n := v.(type) {
	case int64:
		if a.Type.Bits > 0 && a.Type.Bits <= 8 {
			return int8(n) //nolint:gosec // within the width per ValidateWrite
		}
		return int16(n) //nolint:gosec // within the width per ValidateWrite
	case uint64:
		if a.Type.Bits > 0 && a.Type.Bits <= 8 {
			return uint8(n) //nolint:gosec // within the width per ValidateWrite
		}
		return uint16(n) //nolint:gosec // within the width per ValidateWrite
	}
	return v
}
