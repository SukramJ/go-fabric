// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package thermo

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sync/atomic"

	"github.com/SukramJ/go-fabric/cluster/spec"
	tdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// The Events feature (TEVT) and its events, served after connectedhomeip:
// matter.js's ThermostatServer emits none of the cluster's eight events
// (its only emit is the calibratedTemperature$Changed observable,
// ThermostatServer.ts:525 at 862ad75f), so chip's
// src/app/clusters/thermostat-server at 6170af84 is the authority. Every
// Generate…Event there returns unless the selection has TEVT
// (ThermostatClusterEvents.cpp:33-36, :59-61, :83-86, :109-112, :137-140,
// :163-166, :189-192, :215-218); here the selection's EventList is that
// gate ([spec.Events.Emit] answers [spec.ErrNotEmitted] for an event
// outside it). chip emits:
//
//   - SystemModeChange when SystemMode changed (ThermostatClusterBase.cpp:
//     158-162), with the previous mode;
//   - LocalTemperatureChange when LocalTemperature changed (:179-183);
//   - RunningModeChange when ThermostatRunningMode changed (:246-250);
//   - RunningStateChange when ThermostatRunningState changed (:283-287);
//   - SetpointChange for each of the four occupied and unoccupied
//     setpoints a change moved, Heat or Cool with Occupied or no
//     occupancy (ThermostatClusterSetpointsBase.cpp:63-85, called from
//     ThermostatClusterHeatingSetpoints.cpp:196-241 and
//     ThermostatClusterCoolingSetpoints.cpp:247-292, where a changed user
//     limit reaches the switch without a case);
//   - ActivePresetChange whenever SetActivePreset runs
//     (ThermostatClusterPresets.cpp:461-500), the command's and the one a
//     write of a setpoint in effect makes (ThermostatCluster.h:215-224).
//
// chip never calls GenerateOccupancyChangeEvent, and at 6170af84 has no
// schedule server; ActiveScheduleChange is emitted by SetActiveScheduleRequest
// after the earlier pin the schedules follow (schedules.go).

// ThermostatFeatureTEVT is the Events feature (bit 9).
const ThermostatFeatureTEVT = uint32(tdef.FeatureEvents)

// servedEvents are the TEVT events the selection emits. Each is
// provisional ("P, TEVT…", thermostat-cluster.element.ts:287-333 at
// 862ad75f), which the definition reads as optional, so the server names
// them: every event whose feature condition the selection meets.
func servedEvents(features uint32) []uint32 {
	if features&ThermostatFeatureTEVT == 0 {
		return nil
	}
	out := []uint32{tdef.EventSystemModeChange}
	if features&ThermostatFeatureLTNE == 0 {
		out = append(out, tdef.EventLocalTemperatureChange)
	}
	if features&ThermostatFeatureOCC != 0 {
		out = append(out, tdef.EventOccupancyChange)
	}
	out = append(out, tdef.EventSetpointChange, tdef.EventRunningStateChange)
	if features&ThermostatFeatureAUTO != 0 {
		out = append(out, tdef.EventRunningModeChange)
	}
	if features&ThermostatFeatureMSCH != 0 {
		out = append(out, tdef.EventActiveScheduleChange)
	}
	if features&ThermostatFeaturePRES != 0 {
		out = append(out, tdef.EventActivePresetChange)
	}
	return out
}

// eventState is what the TEVT events report a change of.
type eventState struct {
	systemMode   uint8
	localTemp    *int16
	runningMode  uint8
	runningState uint16
}

// eventStateLocked reads the evented state; the caller holds s.mu.
func (s *ThermostatServer) eventStateLocked() eventState {
	return eventState{
		systemMode: s.systemMode, localTemp: s.localTemp, runningMode: s.runningMode, runningState: s.runningState,
	}
}

// emitChanges emits the events of what moved from before to after, in
// chip's attribute order, then the SetpointChange events commitSetpoints
// queued; the caller does not hold s.mu.
func (s *ThermostatServer) emitChanges(ctx context.Context, before, after eventState) {
	defer s.flushSetpointEvents(ctx)
	if s.features&ThermostatFeatureTEVT == 0 {
		return
	}
	if before.systemMode != after.systemMode {
		prev := tdef.SystemModeEnum(before.systemMode)
		s.emit(ctx, tdef.EventSystemModeChange, tdef.SystemModeChangeEvent{PreviousSystemMode: &prev, CurrentSystemMode: tdef.SystemModeEnum(after.systemMode)})
	}
	if !equalTemperature(before.localTemp, after.localTemp) {
		cur := spec.Nullable[int16]{Null: after.localTemp == nil}
		if after.localTemp != nil {
			cur.Value = *after.localTemp
		}
		s.emit(ctx, tdef.EventLocalTemperatureChange, tdef.LocalTemperatureChangeEvent{CurrentLocalTemperature: cur})
	}
	if before.runningMode != after.runningMode {
		prev := tdef.ThermostatRunningModeEnum(before.runningMode)
		s.emit(ctx, tdef.EventRunningModeChange, tdef.RunningModeChangeEvent{PreviousRunningMode: &prev, CurrentRunningMode: tdef.ThermostatRunningModeEnum(after.runningMode)})
	}
	if before.runningState != after.runningState {
		prev := tdef.RelayStateBitmap(before.runningState)
		s.emit(ctx, tdef.EventRunningStateChange, tdef.RunningStateChangeEvent{PreviousRunningState: &prev, CurrentRunningState: tdef.RelayStateBitmap(after.runningState)})
	}
}

// queueSetpointEventLocked queues the SetpointChange of a setpoint
// commitSetpoints moved from was to is: ThermostatSetpointsBase::
// GenerateSetpointEvent (ThermostatClusterSetpointsBase.cpp:63-85), Heat
// or Cool with Occupied for an occupied setpoint and no occupancy for an
// unoccupied one, and nothing for a user limit (no case in its switch).
// The caller holds s.mu; [ThermostatServer.flushSetpointEvents] emits the
// queue once it let go.
func (s *ThermostatServer) queueSetpointEventLocked(attrID uint32, was, is int16) {
	if s.features&ThermostatFeatureTEVT == 0 {
		return
	}
	var (
		mode tdef.SystemModeEnum
		occ  tdef.OccupancyBitmap
	)
	switch attrID {
	case thermoAttrOccupiedHeatingSetpoint:
		mode, occ = tdef.SystemModeHeat, tdef.OccupancyOccupied
	case thermoAttrUnoccupiedHeatingSetpoint:
		mode = tdef.SystemModeHeat
	case thermoAttrOccupiedCoolingSetpoint:
		mode, occ = tdef.SystemModeCool, tdef.OccupancyOccupied
	case thermoAttrUnoccupiedCoolingSetpoint:
		mode = tdef.SystemModeCool
	default:
		return
	}
	prev := was
	s.pendingSetpointEvents = append(s.pendingSetpointEvents, tdef.SetpointChangeEvent{SystemMode: mode, Occupancy: &occ, PreviousSetpoint: &prev, CurrentSetpoint: is})
}

// flushSetpointEvents emits the queued SetpointChange events; the caller
// does not hold s.mu.
func (s *ThermostatServer) flushSetpointEvents(ctx context.Context) {
	s.mu.Lock()
	pending := s.pendingSetpointEvents
	s.pendingSetpointEvents = nil
	s.mu.Unlock()
	for _, e := range pending {
		s.emit(ctx, tdef.EventSetpointChange, e)
	}
}

// emitActivePresetChange is GenerateActivePresetChangeEvent
// (ThermostatClusterEvents.cpp:215-240) with the handles of a
// SetActivePreset (ThermostatClusterPresets.cpp:470-499): the previous
// handle, null included, and the new one.
func (s *ThermostatServer) emitActivePresetChange(ctx context.Context, previous, current []byte) {
	if s.features&ThermostatFeatureTEVT == 0 {
		return
	}
	prev := spec.Nullable[[]byte]{Null: previous == nil, Value: bytes.Clone(previous)}
	cur := spec.Nullable[[]byte]{Null: current == nil, Value: bytes.Clone(current)}
	s.emit(ctx, tdef.EventActivePresetChange, tdef.ActivePresetChangeEvent{PreviousPresetHandle: &prev, CurrentPresetHandle: cur})
}

// emit emits one event on the invoking command's endpoint, else on the
// endpoint the bridge stamped ([ThermostatServer.SetEndpoint]).
func (s *ThermostatServer) emit(ctx context.Context, eventID uint32, data any) {
	ep := uint16(s.endpoint.Load()) //nolint:gosec // SetEndpoint stores a uint16
	if path, ok := im.CommandPathFromContext(ctx); ok {
		ep = path.Endpoint
	}
	if err := s.events.Emit(s.inst, ep, eventID, data); err != nil && !errors.Is(err, spec.ErrNotEmitted) {
		s.logger().Warn("matter.thermostat.event_failed", slog.String("reason", err.Error()))
	}
}

// SetEndpoint stamps the endpoint the events of a host change are
// addressed to; the bridge calls it at reassembly.
func (s *ThermostatServer) SetEndpoint(endpoint uint16) { s.endpoint.Store(uint32(endpoint)) }

// SetThermostatRunningMode records the mode the thermostat runs in right
// now (ThermostatRunningModeEnum: Off, Cool, Heat), the attribute TEVT and
// AUTO make mandatory: stored, reported and, when it changed, the
// RunningModeChange event. Off is always accepted, Cool needs COOL and Heat
// HEAT; anything else is ConstraintError, as chip's
// ThermostatClusterBase::SetRunningMode refuses it
// (ThermostatClusterBase.cpp:213-252 at 6170af84). A server that does not
// serve the attribute refuses with UnsupportedAttribute.
func (s *ThermostatServer) SetThermostatRunningMode(mode uint8) error {
	if !s.inst.Serves(tdef.AttrThermostatRunningMode) {
		return spec.Errorf(im.StatusUnsupportedAttribute, "thermo: ThermostatRunningMode needs the Events and AutoMode features")
	}
	switch tdef.ThermostatRunningModeEnum(mode) {
	case tdef.ThermostatRunningModeOff:
	case tdef.ThermostatRunningModeCool:
		if s.features&ThermostatFeatureCOOL == 0 {
			return spec.Errorf(im.StatusConstraintError, "thermo: running mode Cool needs the Cooling feature")
		}
	case tdef.ThermostatRunningModeHeat:
		if s.features&ThermostatFeatureHEAT == 0 {
			return spec.Errorf(im.StatusConstraintError, "thermo: running mode Heat needs the Heating feature")
		}
	default:
		return spec.Errorf(im.StatusConstraintError, "thermo: running mode %d is not a ThermostatRunningModeEnum value", mode)
	}
	s.mu.Lock()
	before := s.eventStateLocked()
	s.runningMode = mode
	after := s.eventStateLocked()
	s.mu.Unlock()
	if before.runningMode != after.runningMode {
		s.changes.NotifyFrom(contract.OriginHostSet, tdef.AttrThermostatRunningMode)
		s.emitChanges(context.Background(), before, after)
	}
	return nil
}

// followSystemModeLocked sets ThermostatRunningMode from a SystemMode
// that changed to Off, Heat or Cool, when the attribute is served, and
// reports whether it moved; the caller holds s.mu and emits the
// RunningModeChange through emitChanges. Auto and the other modes leave
// it to the host ([ThermostatServer.SetThermostatRunningMode]). Mirrors
// matter.js packages/node/src/behaviors/thermostat/ThermostatServer.ts:1185,
// :1194-1206 at 862ad75f (#handleSystemModeChange on systemMode$Changed;
// W3-clua1-01).
func (s *ThermostatServer) followSystemModeLocked(mode uint8) bool {
	if !s.inst.Serves(tdef.AttrThermostatRunningMode) {
		return false
	}
	var running tdef.ThermostatRunningModeEnum
	switch tdef.SystemModeEnum(mode) {
	case tdef.SystemModeOff:
		running = tdef.ThermostatRunningModeOff
	case tdef.SystemModeHeat:
		running = tdef.ThermostatRunningModeHeat
	case tdef.SystemModeCool:
		running = tdef.ThermostatRunningModeCool
	default:
		return false
	}
	if s.runningMode == uint8(running) {
		return false
	}
	s.runningMode = uint8(running)
	return true
}

// endpointStamp holds the stamped endpoint ([ThermostatServer.SetEndpoint]).
type endpointStamp = atomic.Uint32
