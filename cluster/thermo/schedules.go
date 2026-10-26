// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package thermo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"github.com/SukramJ/go-fabric/cluster/spec"
	tdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// The MatterScheduleConfiguration feature (MSCH): the device's stored week
// programs, the active one, SetActiveScheduleRequest and the atomic write
// of Schedules. matter.js does not implement it ("MatterScheduleConfiguration:
// This feature is not implemented yet", packages/node/src/behaviors/
// thermostat/ThermostatServer.ts:82 at 862ad75f); the behaviour follows
// connectedhomeip's ThermostatSchedules
// (src/app/clusters/thermostat-server/ThermostatClusterSchedules.cpp at
// 645b267a — the harness image's CHIP commit 6170af84 has no schedules
// server). Every rule below cites its chip line.

// Schedule is the ScheduleStruct of the generated definition.
type Schedule = tdef.ScheduleStruct

// ScheduleTransition is the ScheduleTransitionStruct of the generated
// definition.
type ScheduleTransition = tdef.ScheduleTransitionStruct

// ScheduleType is the ScheduleTypeStruct of the generated definition.
type ScheduleType = tdef.ScheduleTypeStruct

// SchedulePort is the device's week program behind MSCH, in the spirit of
// chip's ThermostatSchedules::Delegate (ThermostatClusterSchedules.h:40-153
// at 645b267a): SetActiveSchedule is called after a SetActiveScheduleRequest
// passed the server's checks and before the handle is stored;
// CommitSchedules is called with the list an atomic write committed —
// validated, precommitted and with a handle issued to every new schedule —
// before it is stored. An error refuses the change: the command or the
// commit answers with the error's status when it carries one (an
// [im.StatusCodeError]), FAILURE otherwise, and nothing is stored. Both run
// in the server's command queue with no lock the inbound setters take:
// the port may confirm through [ThermostatServer.SetSchedules] and
// [ThermostatServer.SetActiveScheduleHandle]. A nil port keeps the
// schedules in memory.
type SchedulePort = tdef.SchedulePort

// ErrSchedules reports a ThermostatConfig whose schedule configuration the
// server refuses: ScheduleTypes or a count outside its constraints, or
// configured schedules without a unique handle.
var ErrSchedules = errors.New("thermo: schedule configuration refused")

// cloneSchedules deep-copies schedules.
func cloneSchedules(in []Schedule) []Schedule {
	if in == nil {
		return nil
	}
	out := make([]Schedule, len(in))
	for i, sc := range in {
		out[i] = sc
		if !sc.ScheduleHandle.Null {
			out[i].ScheduleHandle.Value = bytes.Clone(sc.ScheduleHandle.Value)
		}
		if sc.Name != nil {
			n := *sc.Name
			out[i].Name = &n
		}
		if sc.PresetHandle != nil {
			h := bytes.Clone(*sc.PresetHandle)
			out[i].PresetHandle = &h
		}
		out[i].Transitions = make([]ScheduleTransition, len(sc.Transitions))
		for j, t := range sc.Transitions {
			out[i].Transitions[j] = t
			if t.PresetHandle != nil {
				h := bytes.Clone(*t.PresetHandle)
				out[i].Transitions[j].PresetHandle = &h
			}
			if t.SystemMode != nil {
				m := *t.SystemMode
				out[i].Transitions[j].SystemMode = &m
			}
			if t.CoolingSetpoint != nil {
				c := *t.CoolingSetpoint
				out[i].Transitions[j].CoolingSetpoint = &c
			}
			if t.HeatingSetpoint != nil {
				h := *t.HeatingSetpoint
				out[i].Transitions[j].HeatingSetpoint = &h
			}
		}
	}
	return out
}

// schedulesValue is a Schedules value as the server handles it: the
// bridge decodes a write as spec.List, a host may pass a plain slice.
func schedulesValue(v any) ([]Schedule, error) {
	switch x := v.(type) {
	case spec.List[Schedule]:
		return []Schedule(x), nil
	case []Schedule:
		return x, nil
	}
	return nil, spec.Errorf(im.StatusConstraintError, "thermo: Schedules value %T is not a list of ScheduleStruct", v)
}

// scheduleWithHandle is the schedule of list whose handle is h, if any.
func scheduleWithHandle(list []Schedule, h []byte) (Schedule, bool) {
	for _, sc := range list {
		if !sc.ScheduleHandle.Null && bytes.Equal(sc.ScheduleHandle.Value, h) {
			return sc, true
		}
	}
	return Schedule{}, false
}

// seedSchedules takes the MSCH configuration: the schedule types and
// counts (each checked against its attribute), the device's schedules and
// the active handle ([ThermostatServer.SetSchedules]'s rules; an active
// handle that names no schedule is cleared with a warning), and the port.
func (s *ThermostatServer) seedSchedules(cfg ThermostatConfig) error {
	if _, err := s.inst.CheckValue(tdef.AttrScheduleTypes, spec.List[ScheduleType](cfg.ScheduleTypes), nil); err != nil {
		return fmt.Errorf("%w: ScheduleTypes: %w", ErrSchedules, err)
	}
	s.scheduleTypes = slices.Clone(cfg.ScheduleTypes)
	s.numberOfSchedules = cfg.NumberOfSchedules
	s.numberOfScheduleTransitions = cfg.NumberOfScheduleTransitions
	if p := cfg.NumberOfScheduleTransitionPerDay; p != nil {
		v := *p
		s.transitionsPerDay = &v
	}
	for _, id := range []uint32{tdef.AttrNumberOfSchedules, tdef.AttrNumberOfScheduleTransitions, tdef.AttrNumberOfScheduleTransitionPerDay} {
		v, _ := s.MatterRead(id)
		if _, err := s.inst.CheckValue(id, v, s.MatterRead); err != nil {
			return fmt.Errorf("%w: attribute 0x%04X: %w", ErrSchedules, id, err)
		}
	}
	s.schedulePort = cfg.SchedulePort
	schedules, err := s.hostSchedules(cfg.Schedules)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrSchedules, err)
	}
	s.schedules = schedules
	active := bytes.Clone(cfg.ActiveScheduleHandle)
	if active != nil {
		if _, ok := scheduleWithHandle(schedules, active); !ok {
			s.logger().Warn("matter.thermostat.active_schedule_cleared",
				slog.String("schedule_handle", handleKey(active)), slog.String("reason", "matches no schedule"))
			active = nil
		}
	}
	s.activeScheduleHandle = active
	return nil
}

// hostSchedules checks and normalises the device's own schedules: the
// list held to the Schedules attribute's type and constraints, every
// schedule with a handle of its own (a SetActiveScheduleRequest names it),
// a null BuiltIn taken as built in — a schedule the device supplies is
// one a controller cannot remove. chip takes the delegate's list as it is
// (ThermostatClusterSchedules.h:83-91); the handle rule is what its
// SetActiveSchedule and PrecommitSchedules need of it.
func (s *ThermostatServer) hostSchedules(list []Schedule) ([]Schedule, error) {
	out := cloneSchedules(list)
	if out == nil {
		out = []Schedule{}
	}
	if _, err := s.inst.CheckValue(tdef.AttrSchedules, spec.List[Schedule](out), s.MatterRead); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for i := range out {
		sc := &out[i]
		if sc.ScheduleHandle.Null || len(sc.ScheduleHandle.Value) == 0 {
			return nil, fmt.Errorf("%w: schedule %d has no handle", spec.ErrInvalidValue, i)
		}
		k := handleKey(sc.ScheduleHandle.Value)
		if seen[k] {
			return nil, fmt.Errorf("%w: schedule handle %s appears twice", spec.ErrInvalidValue, k)
		}
		seen[k] = true
		if sc.BuiltIn.Null {
			sc.BuiltIn = spec.ValueOf(true)
		}
	}
	return out, nil
}

// SetSchedules records the device's own schedules — a program edited on
// the device, or the list a [SchedulePort] stored: held to the Schedules
// attribute's type and constraints, each schedule with a unique handle, a
// null BuiltIn taken as built in; stored and reported through the change
// notifier, without the port. An active schedule the list drops is
// cleared and reported. The error is the refusal ([spec.ErrInvalidValue],
// or UNSUPPORTED_ATTRIBUTE without MSCH); schedules is not modified.
func (s *ThermostatServer) SetSchedules(schedules []Schedule) error {
	if s.features&ThermostatFeatureMSCH == 0 {
		return spec.Errorf(im.StatusUnsupportedAttribute, "thermo: Schedules needs the MatterScheduleConfiguration feature")
	}
	list, err := s.hostSchedules(schedules)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.schedules = list
	changed := []uint32{tdef.AttrSchedules}
	if s.activeScheduleHandle != nil {
		if _, ok := scheduleWithHandle(list, s.activeScheduleHandle); !ok {
			s.activeScheduleHandle = nil
			changed = append(changed, tdef.AttrActiveScheduleHandle)
		}
	}
	s.mu.Unlock()
	s.changes.NotifyFrom(contract.OriginHostSet, changed...)
	return nil
}

// SetActiveScheduleHandle records the schedule the device follows now
// (nil for none), without the port: stored and reported through the change
// notifier. A handle that names no stored schedule is refused
// ([spec.ErrInvalidValue]).
func (s *ThermostatServer) SetActiveScheduleHandle(handle []byte) error {
	if s.features&ThermostatFeatureMSCH == 0 {
		return spec.Errorf(im.StatusUnsupportedAttribute, "thermo: ActiveScheduleHandle needs the MatterScheduleConfiguration feature")
	}
	s.mu.Lock()
	if handle != nil {
		if _, ok := scheduleWithHandle(s.schedules, handle); !ok {
			s.mu.Unlock()
			return fmt.Errorf("%w: schedule handle %s names no schedule", spec.ErrInvalidValue, handleKey(handle))
		}
	}
	changed := !bytes.Equal(s.activeScheduleHandle, handle) || (s.activeScheduleHandle == nil) != (handle == nil)
	s.activeScheduleHandle = bytes.Clone(handle)
	s.mu.Unlock()
	if changed {
		s.changes.NotifyFrom(contract.OriginHostSet, tdef.AttrActiveScheduleHandle)
	}
	return nil
}

// Schedules returns a copy of the stored schedules.
func (s *ThermostatServer) Schedules() []Schedule {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneSchedules(s.schedules)
}

// ActiveScheduleHandle returns the ActiveScheduleHandle, nil for null.
func (s *ThermostatServer) ActiveScheduleHandle() []byte {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return bytes.Clone(s.activeScheduleHandle)
}

// scheduleRequest normalises the SetActiveScheduleRequest payload: the
// bridge decodes it through the generated definition.
func scheduleRequest(fields any) (tdef.SetActiveScheduleRequestRequest, error) {
	switch v := fields.(type) {
	case tdef.SetActiveScheduleRequestRequest:
		return v, nil
	case *tdef.SetActiveScheduleRequestRequest:
		if v != nil {
			return *v, nil
		}
	}
	return tdef.SetActiveScheduleRequestRequest{}, spec.Errorf(im.StatusInvalidCommand, "thermo: SetActiveScheduleRequest carried %T", fields)
}

// handleSetActiveScheduleRequest is chip ThermostatSchedules::SetActiveSchedule
// (ThermostatClusterSchedules.cpp:741-793 at 645b267a): a handle that names
// no stored schedule is INVALID_COMMAND; then the [SchedulePort] switches
// the device (its refusal answers the command and stores nothing), the
// handle is stored and reported, and with the Events feature the
// ActiveScheduleChange event carries the previous and the new handle
// (ThermostatClusterEvents.cpp:189-213: emitted only with TEVT, a feature
// this server does not serve, so the event stays out of its EventList).
func (s *ThermostatServer) handleSetActiveScheduleRequest(ctx context.Context, fields any) error {
	req, err := scheduleRequest(fields)
	if err != nil {
		return err
	}
	s.op.Lock()
	defer s.op.Unlock()
	s.mu.RLock()
	_, known := scheduleWithHandle(s.schedules, req.ScheduleHandle)
	s.mu.RUnlock()
	if !known {
		return spec.Errorf(im.StatusInvalidCommand, "thermo: schedule handle %s names no schedule", handleKey(req.ScheduleHandle))
	}
	handle := bytes.Clone(req.ScheduleHandle)
	if s.schedulePort != nil {
		if err := s.schedulePort.SetActiveSchedule(ctx, bytes.Clone(handle)); err != nil {
			return sinkError("SetActiveScheduleRequest", err)
		}
	}
	s.mu.Lock()
	previous := s.activeScheduleHandle
	s.activeScheduleHandle = handle
	s.mu.Unlock()
	s.logger().Info("matter.thermostat.active_schedule", slog.String("schedule_handle", handleKey(handle)))
	s.changes.NotifyFrom(contract.OriginFrom(ctx), tdef.AttrActiveScheduleHandle)
	prev := spec.Nullable[[]byte]{Null: previous == nil, Value: previous}
	if path, ok := im.CommandPathFromContext(ctx); ok {
		event := tdef.ActiveScheduleChangeEvent{PreviousScheduleHandle: &prev, CurrentScheduleHandle: spec.ValueOf(bytes.Clone(handle))}
		if err := s.events.Emit(s.inst, path.Endpoint, tdef.EventActiveScheduleChange, event); err != nil && !errors.Is(err, spec.ErrNotEmitted) {
			s.logger().Warn("matter.thermostat.event_failed", slog.String("reason", err.Error()))
		}
	}
	return nil
}
