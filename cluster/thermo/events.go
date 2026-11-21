// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package thermo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/cluster/spec"
	tdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// The Events feature (TEVT) and its events, as matter.js's ThermostatServer
// emits them behind its forward feature thermostat-events (the Ahead of
// matter.js register keeps that gate as the marker; here TEVT serves every
// event its conformance admits, as the selection's EventList, and
// [spec.Events.Emit] answers [spec.ErrNotEmitted] for one outside it).
// Every event reacts to a committed change of its attribute, never to a
// write that could still be refused, and none to a value that did not
// change. Mirrors matter.js
// packages/node/src/behaviors/thermostat/ThermostatServer.ts:2082-2148 at b25ed960
// (#setupEvents):
//
//   - SystemModeChange, RunningModeChange and RunningStateChange with the
//     previous and the current value;
//   - OccupancyChange (OCC) with the previous and the current occupancy;
//   - ActivePresetChange (PRES) and ActiveScheduleChange (MSCH) on every
//     change of the handle, whatever changed it — a command, the host's
//     setter, a direct setpoint write that ends the preset, the start that
//     clears a handle naming nothing — with the previous and the current
//     handle;
//   - SetpointChange for each of the four occupied and unoccupied setpoints
//     a change moved (none for a limit), Heat or Cool, with the Occupancy
//     field only with OCC (ThermostatServer.ts:2150-2187 at b25ed960,
//     #setupSetpointChange);
//   - LocalTemperatureChange (without LTNE) for a significant change only,
//     at most one per 60 s (ThermostatServer.ts:2190-2259 at b25ed960,
//     #setupLocalTemperatureChange; localTempEvent).
//
// The events a start produces — the handle a stale configuration names
// cleared, the setpoints the initial reconcile moved — are queued and
// emitted when the bridge stamps the endpoint the first time
// ([ThermostatServer.SetEndpoint]): before it the server has no endpoint
// to emit on, and matter.js records them with the start.

// ThermostatFeatureTEVT is the Events feature (bit 9).
const ThermostatFeatureTEVT = uint32(tdef.FeatureEvents)

// servedEvents are the TEVT events the selection emits. Each is
// provisional ("P, TEVT…", thermostat-cluster.element.ts:287-333 at
// b25ed960), which the definition reads as optional, so the server names
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
	occupied     bool
	runningMode  uint8
	runningState uint16
}

// eventStateLocked reads the evented state; the caller holds s.mu.
func (s *ThermostatServer) eventStateLocked() eventState {
	local := s.localTemp
	if s.features&ThermostatFeatureLTNE != 0 {
		local = nil // the attribute stays null under LTNE: no LocalTemperatureChange
	}
	return eventState{
		systemMode: s.systemMode, localTemp: local, occupied: s.occupiedLocked(),
		runningMode: s.runningMode, runningState: s.runningState,
	}
}

// emitChanges emits the events of what moved from before to after —
// SystemMode, Occupancy, LocalTemperature (through its 60 s window), the
// running mode and state — then the SetpointChange events
// commitSetpoints queued; the caller does not hold s.mu. No matter.js test
// asserts an order between the events of one change.
func (s *ThermostatServer) emitChanges(ctx context.Context, before, after eventState) {
	defer s.flushSetpointEvents(ctx)
	if s.features&ThermostatFeatureTEVT == 0 {
		return
	}
	if before.systemMode != after.systemMode {
		prev := tdef.SystemModeEnum(before.systemMode)
		s.emit(ctx, tdef.EventSystemModeChange, tdef.SystemModeChangeEvent{PreviousSystemMode: &prev, CurrentSystemMode: tdef.SystemModeEnum(after.systemMode)})
	}
	if s.features&ThermostatFeatureOCC != 0 && before.occupied != after.occupied {
		prev, cur := occupancyBitmap(before.occupied), occupancyBitmap(after.occupied)
		s.emit(ctx, tdef.EventOccupancyChange, tdef.OccupancyChangeEvent{PreviousOccupancy: &prev, CurrentOccupancy: cur})
	}
	if !equalTemperature(before.localTemp, after.localTemp) {
		s.localTemperatureChanged(ctx, after.localTemp)
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

// occupancyBitmap is the Occupancy attribute of occupied.
func occupancyBitmap(occupied bool) tdef.OccupancyBitmap {
	if occupied {
		return tdef.OccupancyOccupied
	}
	return 0
}

// queueSetpointEventLocked queues the SetpointChange of a setpoint
// commitSetpoints moved from was to is: Heat or Cool, the Occupancy field
// only with OCC — occupied for an occupied setpoint, the empty bitmap
// (value 0) for an unoccupied one — and nothing for a user limit. Mirrors
// matter.js ThermostatServer.ts:2150-2187 at b25ed960 (#setupSetpointChange:
// `occupancy: features.occupancy ? { occupied } : undefined`, the four
// setpoint reactions).
// The caller holds s.mu; [ThermostatServer.flushSetpointEvents] emits the
// queue once it let go.
func (s *ThermostatServer) queueSetpointEventLocked(attrID uint32, was, is int16) {
	if s.features&ThermostatFeatureTEVT == 0 {
		return
	}
	var (
		mode     tdef.SystemModeEnum
		occupied bool
	)
	switch attrID {
	case thermoAttrOccupiedHeatingSetpoint:
		mode, occupied = tdef.SystemModeHeat, true
	case thermoAttrUnoccupiedHeatingSetpoint:
		mode = tdef.SystemModeHeat
	case thermoAttrOccupiedCoolingSetpoint:
		mode, occupied = tdef.SystemModeCool, true
	case thermoAttrUnoccupiedCoolingSetpoint:
		mode = tdef.SystemModeCool
	default:
		return
	}
	prev := was
	event := tdef.SetpointChangeEvent{SystemMode: mode, PreviousSetpoint: &prev, CurrentSetpoint: is}
	if s.features&ThermostatFeatureOCC != 0 {
		occ := occupancyBitmap(occupied)
		event.Occupancy = &occ
	}
	s.pendingSetpointEvents = append(s.pendingSetpointEvents, event)
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

// sameHandle reports whether two handles are equal, null (nil) only to
// null.
func sameHandle(a, b []byte) bool {
	return (a == nil) == (b == nil) && bytes.Equal(a, b)
}

// activePresetChange is the ActivePresetChange of a handle that moved from
// previous to current, null (nil) included.
func activePresetChange(previous, current []byte) tdef.ActivePresetChangeEvent {
	prev := spec.Nullable[[]byte]{Null: previous == nil, Value: bytes.Clone(previous)}
	return tdef.ActivePresetChangeEvent{
		PreviousPresetHandle: &prev,
		CurrentPresetHandle:  spec.Nullable[[]byte]{Null: current == nil, Value: bytes.Clone(current)},
	}
}

// activeScheduleChange is the ActiveScheduleChange of a handle that moved
// from previous to current, null (nil) included.
func activeScheduleChange(previous, current []byte) tdef.ActiveScheduleChangeEvent {
	prev := spec.Nullable[[]byte]{Null: previous == nil, Value: bytes.Clone(previous)}
	return tdef.ActiveScheduleChangeEvent{
		PreviousScheduleHandle: &prev,
		CurrentScheduleHandle:  spec.Nullable[[]byte]{Null: current == nil, Value: bytes.Clone(current)},
	}
}

// emitActivePresetChange emits the ActivePresetChange of a stored change
// of ActivePresetHandle from previous to current, and nothing when the
// handle stayed. Mirrors matter.js
// ThermostatServer.ts:2126-2134 at b25ed960 (the activePresetHandle$Changed
// reaction of #setupEvents, whatever source changed the handle).
func (s *ThermostatServer) emitActivePresetChange(ctx context.Context, previous, current []byte) {
	if s.features&ThermostatFeatureTEVT == 0 || sameHandle(previous, current) {
		return
	}
	s.emit(ctx, tdef.EventActivePresetChange, activePresetChange(previous, current))
}

// emitActiveScheduleChange is [ThermostatServer.emitActivePresetChange]
// for ActiveScheduleHandle. Mirrors matter.js
// ThermostatServer.ts:2136-2144 at b25ed960 (the activeScheduleHandle$Changed
// reaction).
func (s *ThermostatServer) emitActiveScheduleChange(ctx context.Context, previous, current []byte) {
	if s.features&ThermostatFeatureTEVT == 0 || sameHandle(previous, current) {
		return
	}
	s.emit(ctx, tdef.EventActiveScheduleChange, activeScheduleChange(previous, current))
}

// startupEvent is an event the construction produced, emitted at the first
// [ThermostatServer.SetEndpoint].
type startupEvent struct {
	id   uint32
	data any
}

// queueStartupEventLocked queues an event of the start; the caller owns s
// (the constructor) or holds s.mu.
func (s *ThermostatServer) queueStartupEventLocked(id uint32, data any) {
	if s.features&ThermostatFeatureTEVT == 0 {
		return
	}
	s.startupEvents = append(s.startupEvents, startupEvent{id: id, data: data})
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
// addressed to; the bridge calls it at reassembly, for every server of the
// new topology. A server [ThermostatServer.MatterQuiesce] stopped comes
// back here — a reassembly that puts the same object back — and its
// suggestions are re-evaluated and the timer armed from the state, as a
// moved Matter time does (review II, R2-P1-dr-rest-03).
//
// The first call starts the server for its events: the events its
// construction produced are emitted on the endpoint now, and a later change
// of LocalTemperature is measured against the value it has then (the
// configured one; localTempEvent).
func (s *ThermostatServer) SetEndpoint(endpoint uint16) {
	s.endpoint.Store(uint32(endpoint))
	s.events.SetEndpoint(endpoint) // EmitEvent, the typed Record<Event>
	s.mu.Lock()
	back := s.suggest.quiesced
	s.suggest.quiesced = false
	startup := s.startupEvents
	s.startupEvents = nil
	s.ltEvent.started = true
	s.mu.Unlock()
	for _, e := range startup {
		s.emit(context.Background(), e.id, e.data)
	}
	if back {
		s.scheduleSuggestionReevaluation()
	}
}

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
// matter.js packages/node/src/behaviors/thermostat/ThermostatServer.ts:1661,
// :1669-1681 at b25ed960 (#handleSystemModeChange on systemMode$Changed;
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

// localTemperatureChangeInterval is the shortest time between two
// LocalTemperatureChange events. Mirrors matter.js
// ThermostatServer.ts:51 at b25ed960 (LOCAL_TEMPERATURE_CHANGE_INTERVAL).
const localTemperatureChangeInterval = 60 * time.Second

// defaultLocalTemperatureChangeThreshold is the threshold without
// [ThermostatConfig.LocalTemperatureChangeThreshold], in 0.01 °C. Mirrors
// matter.js ThermostatServer.ts:3159 at b25ed960
// (localTemperatureChangeThreshold = 50).
const defaultLocalTemperatureChangeThreshold = 50

// localTempEvent is the state of the LocalTemperatureChange event, guarded
// by s.mu. Mirrors matter.js ThermostatServer.ts:3385-3391 at b25ed960
// (lastReportedLocalTemperature, lastLocalTemperatureReportAt,
// localTemperatureChangeTimer). Before the first
// [ThermostatServer.SetEndpoint] (started false) a change of
// LocalTemperature is the configuration: it becomes the reported value
// and emits nothing, as matter.js takes the configured value as reported
// when the behavior initializes (:2198).
type localTempEvent struct {
	// threshold is the smallest significant change, > 0 with the event.
	threshold int16
	// reported is the value of the last event, or the value at the start
	// before the first event (nil for null).
	reported *int16
	// reportedAt is the monotonic reading of the last event; hasReport is
	// false until the first.
	reportedAt time.Duration
	hasReport  bool
	// timer runs while a significant change waits for the end of the
	// window; gen tells a callback that lost the race against a stop that
	// its decision was taken over.
	timer clock.Timer
	gen   uint64
	// started is set by the first SetEndpoint.
	started bool
}

// servesLocalTemperatureChange reports whether the selection has the
// event: TEVT without LTNE (thermostat-cluster.element.ts:292 at b25ed960,
// "P, TEVT & !LTNE").
func (s *ThermostatServer) servesLocalTemperatureChange() bool {
	return s.features&ThermostatFeatureTEVT != 0 && s.features&ThermostatFeatureLTNE == 0
}

// checkLocalTemperatureChangeThreshold refuses a threshold of 0 or below.
// Mirrors matter.js ThermostatServer.ts:2202-2208 at b25ed960
// (#assertLocalTemperatureChangeThreshold: finite and greater than 0;
// every int16 is finite).
func checkLocalTemperatureChangeThreshold(threshold int16) error {
	if threshold <= 0 {
		return fmt.Errorf("thermo: localTemperatureChangeThreshold is %d but must be a finite number greater than 0", threshold)
	}
	return nil
}

// SetLocalTemperatureChangeThreshold changes the smallest change of
// LocalTemperature, in 0.01 °C, that the LocalTemperatureChange event
// reports ([ThermostatConfig.LocalTemperatureChangeThreshold]). With the
// event (TEVT without LTNE) a value of 0 or below is refused and the
// threshold kept. A change waiting for the end of the 60 s window is
// judged against the new threshold when the window ends; the change of the
// threshold itself reports nothing. Mirrors matter.js
// ThermostatServer.ts:2195-2196 at b25ed960 (the threshold checked at the
// start and on every change, before it commits).
func (s *ThermostatServer) SetLocalTemperatureChangeThreshold(threshold int16) error {
	if s.servesLocalTemperatureChange() {
		if err := checkLocalTemperatureChangeThreshold(threshold); err != nil {
			return err
		}
	}
	s.mu.Lock()
	s.ltEvent.threshold = threshold
	s.mu.Unlock()
	return nil
}

// localTemperatureChanged is a stored change of LocalTemperature to v
// (nil for null): reported at once, held back to the end of the window, or
// nothing. The caller does not hold s.mu.
func (s *ThermostatServer) localTemperatureChanged(ctx context.Context, v *int16) {
	if !s.servesLocalTemperatureChange() {
		return
	}
	s.mu.Lock()
	report := s.decideLocalTemperatureLocked(v)
	s.mu.Unlock()
	if report {
		s.emitLocalTemperatureChange(ctx, v)
	}
}

// decideLocalTemperatureLocked decides on a change of LocalTemperature to
// v and reports whether to emit it now; the caller holds s.mu. A pending
// report always stops first; a change that is not significant leaves
// nothing pending; a significant one is reported when 60 s passed since
// the last report (or there was none), else a timer waits for the rest of
// the window. Mirrors matter.js
// ThermostatServer.ts:2215-2237 at b25ed960 (#handleLocalTemperatureChange).
func (s *ThermostatServer) decideLocalTemperatureLocked(v *int16) bool {
	lt := &s.ltEvent
	if lt.timer != nil {
		lt.timer.Stop()
		lt.timer = nil
	}
	lt.gen++
	if !lt.started {
		lt.reported = cloneTemperature(v)
		return false
	}
	if !s.significantLocalTemperatureLocked(v) {
		return false
	}
	now := clock.MonotonicOf(s.clk)
	elapsed := localTemperatureChangeInterval
	if lt.hasReport {
		elapsed = now - lt.reportedAt
	}
	if elapsed >= localTemperatureChangeInterval {
		lt.reported, lt.reportedAt, lt.hasReport = cloneTemperature(v), now, true
		return true
	}
	gen := lt.gen
	lt.timer = s.clk.AfterFunc(localTemperatureChangeInterval-elapsed, func() { s.localTemperatureTimerFired(gen) })
	return false
}

// localTemperatureTimerFired runs the decision again on the value
// LocalTemperature has when the window ends: a timer that expires early
// waits for the rest, a change no longer significant is dropped. Mirrors
// matter.js ThermostatServer.ts:2239-2241 at b25ed960
// (#handlePendingLocalTemperatureChange).
func (s *ThermostatServer) localTemperatureTimerFired(gen uint64) {
	s.mu.Lock()
	if s.ltEvent.gen != gen {
		s.mu.Unlock()
		return
	}
	s.ltEvent.timer = nil
	v := cloneTemperature(s.eventStateLocked().localTemp)
	report := s.decideLocalTemperatureLocked(v)
	s.mu.Unlock()
	if report {
		s.emitLocalTemperatureChange(context.Background(), v)
	}
}

// significantLocalTemperatureLocked reports whether v differs
// significantly from the last reported value: a switch between null and a
// value whatever the threshold, else a difference of at least the
// threshold. Mirrors matter.js
// ThermostatServer.ts:2253-2259 at b25ed960 (#isSignificantLocalTemperatureChange).
func (s *ThermostatServer) significantLocalTemperatureLocked(v *int16) bool {
	reported := s.ltEvent.reported
	if v == nil || reported == nil {
		return (v == nil) != (reported == nil)
	}
	return absInt(int(*v)-int(*reported)) >= int(s.ltEvent.threshold)
}

// stopLocalTemperatureTimerLocked drops a report waiting for the end of
// the window; the caller holds s.mu.
func (s *ThermostatServer) stopLocalTemperatureTimerLocked() {
	if s.ltEvent.timer != nil {
		s.ltEvent.timer.Stop()
		s.ltEvent.timer = nil
	}
	s.ltEvent.gen++
}

// emitLocalTemperatureChange emits the event of v (nil for null). Mirrors
// matter.js ThermostatServer.ts:2243-2247 at b25ed960
// (#reportLocalTemperatureChange).
func (s *ThermostatServer) emitLocalTemperatureChange(ctx context.Context, v *int16) {
	cur := spec.Nullable[int16]{Null: v == nil}
	if v != nil {
		cur.Value = *v
	}
	s.emit(ctx, tdef.EventLocalTemperatureChange, tdef.LocalTemperatureChangeEvent{CurrentLocalTemperature: cur})
}

func cloneTemperature(v *int16) *int16 {
	if v == nil {
		return nil
	}
	c := *v
	return &c
}

// endpointStamp holds the stamped endpoint ([ThermostatServer.SetEndpoint]).
type endpointStamp = atomic.Uint32
