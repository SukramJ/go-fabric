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

	"github.com/SukramJ/go-fabric/internal/deepcopy"

	"github.com/SukramJ/go-fabric/cluster/spec"
	tdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// The MatterScheduleConfiguration feature (MSCH): the device's stored week
// programs, the active one, SetActiveScheduleRequest and the atomic write
// of Schedules. matter.js implements it behind its forward feature
// thermostat-msch, not released ("MatterScheduleConfiguration feature is
// not released yet", packages/node/src/behaviors/thermostat/ThermostatServer.ts:267-271
// at b25ed960); the server mirrors that implementation and is offered
// behind the extension BD-chip-Thermostat-Schedules (ahead.go, owner
// decision 291). The SchedulePort, the device's own storage of its
// programs, has no matter.js counterpart; it follows connectedhomeip's
// ThermostatSchedules::Delegate.

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
// validated and with a handle issued to every new schedule — before it is
// stored. An error refuses the change: the command or the commit answers
// with the error's status when it carries one (an [im.StatusCodeError]),
// FAILURE otherwise, and nothing is stored. Both run in the server's
// command queue with no lock the inbound setters take: the port may
// confirm through [ThermostatServer.SetSchedules] (the handles the commit
// issued count as the change's own) and
// [ThermostatServer.SetActiveScheduleHandle]. A nil port keeps the
// schedules in memory.
type SchedulePort = tdef.SchedulePort

// ErrSchedules reports a ThermostatConfig whose schedule configuration the
// server refuses: ScheduleTypes or a count outside its constraints, or
// configured schedules the write rules refuse.
var ErrSchedules = errors.New("thermo: schedule configuration refused")

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

// seedSchedules takes the MSCH configuration as matter.js's initialize
// does (ThermostatServer.ts:321-366 at b25ed960): the schedule types
// (#assertScheduleTypes) and counts (each checked against its attribute),
// the configured schedules held to the write rules against themselves
// (:322-334) and normalised — a handle issued where null, a null BuiltIn
// false — the active handle (one that names no schedule is cleared with a
// warning, :353-366), and the port.
func (s *ThermostatServer) seedSchedules(cfg ThermostatConfig) error {
	if _, err := s.inst.CheckValue(tdef.AttrScheduleTypes, spec.List[ScheduleType](cfg.ScheduleTypes), nil); err != nil {
		return fmt.Errorf("%w: ScheduleTypes: %w", ErrSchedules, err)
	}
	if err := assertScheduleTypes(cfg.ScheduleTypes); err != nil {
		return fmt.Errorf("%w: %w", ErrSchedules, err)
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
	configured := cfg.Schedules
	if configured == nil {
		configured = []Schedule{}
	}
	schedules, err := s.validateHostSchedules(configured, configured)
	if err != nil {
		return fmt.Errorf("%w: configured schedules are invalid: %w", ErrSchedules, err)
	}
	if err := issueScheduleHandles(schedules, nil); err != nil {
		return fmt.Errorf("%w: %w", ErrSchedules, err)
	}
	s.schedules = schedules
	active := bytes.Clone(cfg.ActiveScheduleHandle)
	if active != nil && !scheduleHandleIn(schedules, active) {
		s.logger().Warn("matter.thermostat.active_schedule_cleared",
			slog.String("schedule_handle", handleKey(active)), slog.String("reason", "matches no schedule"))
		active = nil
	}
	s.activeScheduleHandle = active
	return nil
}

// assertScheduleTypes is matter.js #assertScheduleTypes
// (ThermostatServer.ts:2666-2687 at b25ed960): every schedule type names
// Auto, Heat or Cool, no mode twice, and supports presets or setpoints.
func assertScheduleTypes(types []ScheduleType) error {
	seen := map[tdef.SystemModeEnum]bool{}
	for _, t := range types {
		switch t.SystemMode {
		case tdef.SystemModeAuto, tdef.SystemModeHeat, tdef.SystemModeCool:
		default:
			return fmt.Errorf("thermo: ScheduleTypes may only name Auto, Heat or Cool, not SystemMode %d", t.SystemMode)
		}
		if seen[t.SystemMode] {
			return fmt.Errorf("thermo: ScheduleTypes names SystemMode %d more than once", t.SystemMode)
		}
		seen[t.SystemMode] = true
		if t.ScheduleTypeFeatures&(tdef.ScheduleTypeFeaturesSupportsPresets|tdef.ScheduleTypeFeaturesSupportsSetpoints) == 0 {
			return fmt.Errorf("thermo: ScheduleTypes entry for SystemMode %d supports neither presets nor setpoints", t.SystemMode)
		}
	}
	return nil
}

// validateHostSchedules holds the device's schedules to the Schedules
// attribute's schema — its literal bounds; "max numberOfSchedules" is the
// write rules' count, which answers RESOURCE_EXHAUSTED after every
// schedule's own rules, as on the wire (MatterWriteChanged) — and to the
// write rules against stored, with the stored presets, and returns them
// normalised. The caller holds s.mu or owns s.
func (s *ThermostatServer) validateHostSchedules(stored, list []Schedule) ([]Schedule, error) {
	if _, err := s.inst.CheckValue(tdef.AttrSchedules, spec.List[Schedule](list), nil); err != nil {
		return nil, spec.Errorf(im.StatusConstraintError, "thermo: Schedules: %w", err)
	}
	return s.scheduleRules(s.presets).validateSchedules(stored, list)
}

// SetSchedules is a local write of the Schedules attribute — matter.js
// `state.schedules = …` with a local actor, which stores at once
// (ThermostatServer.ts:3272-3287 at b25ed960) through
// persistedSchedules$Changing (:2603-2630): the schedules pass the rules a
// controller's do against the stored ones (a handle the device has not
// issued is NOT_FOUND, the type and counts, names, presets and
// transitions), new schedules get a handle, a null BuiltIn takes the
// stored schedule's or false, and a built-in or the active schedule
// cannot be removed; the list is stored and reported through the change
// notifier, without the port. A [SchedulePort] confirming a commit may
// pass the handles that commit issued. The error is the refusal, with its
// IM status (UNSUPPORTED_ATTRIBUTE without MSCH); schedules is not
// modified.
func (s *ThermostatServer) SetSchedules(schedules []Schedule) error {
	if s.features&ThermostatFeatureMSCH == 0 {
		return spec.Errorf(im.StatusUnsupportedAttribute, "thermo: Schedules needs the MatterScheduleConfiguration feature")
	}
	if schedules == nil {
		schedules = []Schedule{}
	}
	s.mu.Lock()
	list, err := s.validateHostSchedules(s.schedules, schedules)
	if err == nil {
		err = validateScheduleCommit(s.schedules, list, s.activeScheduleHandle)
	}
	if err == nil {
		err = issueScheduleHandles(list, nil)
	}
	if err != nil {
		s.mu.Unlock()
		return err
	}
	changed := !schedulesTLVEqual(s.schedules, list)
	s.schedules = list
	s.mu.Unlock()
	if changed {
		s.changes.NotifyFrom(contract.OriginHostSet, tdef.AttrSchedules)
	}
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
	return deepcopy.Each(s.schedules)
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

// handleSetActiveScheduleRequest is matter.js handleSetActiveScheduleRequest
// (ThermostatServer.ts:582-591 at b25ed960): a handle that names no stored
// schedule is INVALID_COMMAND, else the handle is stored. Before it is
// stored the [SchedulePort] switches the device (its refusal answers the
// command and stores nothing; chip's delegate, matter.js has no port); the
// handle is reported, and with the Events feature the
// ActiveScheduleChange event carries the previous and the new handle
// (ThermostatClusterEvents.cpp:189-213 at 6170af84: emitted only with
// TEVT; without it the event is outside the EventList and
// [spec.Events.Emit] drops it).
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
	// SetSchedules takes no op: a list the host stored while the port ran
	// may have dropped the handle, and a dangling one is never stored
	// (R2-P6-conc-1510).
	if _, ok := scheduleWithHandle(s.schedules, handle); !ok {
		s.mu.Unlock()
		return spec.Errorf(im.StatusInvalidCommand, "thermo: schedule handle %s was removed while the device switched to it", handleKey(handle))
	}
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
