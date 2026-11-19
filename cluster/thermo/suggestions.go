// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package thermo

import (
	"bytes"
	"context"
	"log/slog"
	"slices"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/cluster/spec"
	tdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// ThermostatSuggestions (TSUGGEST, "[PRES]"): a controller suggests one of
// the thermostat's presets for a span of time — AddThermostatSuggestion
// with the preset's handle, the Matter epoch second the suggestion takes
// effect (null: now) and its lifetime in minutes —, the thermostat lists
// the suggestions it holds (ThermostatSuggestions, at most
// MaxThermostatSuggestions), picks the one it follows
// (CurrentThermostatSuggestion), switches to its preset and says why when
// it does not (ThermostatSuggestionNotFollowingReason).
//
// Mirrors matter.js packages/node/src/behaviors/thermostat/ThermostatServer.ts
// at b25ed960 (#4993; class doc :196-213): the commands
// (:613-660), the re-evaluation (#reevaluateSuggestions and
// #determineCurrentSuggestion, :2368-2484), the timer (#restartSuggestionTimer,
// :2497-2529), the unique ids (#nextSuggestionUniqueId, :2532-2540), the
// manual override (#trackOverrideOnPresetChange, :2324-2348) and the
// suggestions of a removed preset (#reevaluateOnPresetsChange, :2556-2565).
// Ours, with matter.js's protected defaults as the model: the host's own
// not-following reasons arrive through SetAttributes (matter.js's
// overridable suggestionNotFollowingReason, :747-754), the Matter time
// through [ThermostatServer.AttachMatterTime] (matter.js's
// hasSynchronizedTime, :701-707). Not ported: matter.js's
// selectThermostatSuggestion seam (:731-737; the earliest EffectiveTime,
// which this server applies), and the retry back-off after a
// re-evaluation that did not take effect (:75-78, :2512-2519), which is
// transaction machinery with no counterpart here.

// ThermostatFeatureTSUGGEST is the ThermostatSuggestions feature (bit 10,
// conformance "[PRES]": New refuses it without Presets, as
// connectedhomeip's ThermostatCluster.h:80 static_assert does).
const ThermostatFeatureTSUGGEST = uint32(tdef.FeatureThermostatSuggestions)

// Suggestion is one ThermostatSuggestionStruct: its unique id, the preset
// it suggests, and the Matter epoch seconds it takes effect and expires.
type Suggestion = tdef.ThermostatSuggestionStruct

const (
	// defaultMaxSuggestions is MaxThermostatSuggestions when the
	// configuration names none: the constraint's minimum, and the
	// reference delegate's kMaxNumberOfThermostatSuggestions
	// (thermostat-suggestions-delegate-impl.h:36 at 6170af84).
	defaultMaxSuggestions = 5
	// minSuggestionMinutes and maxSuggestionMinutes bound
	// ExpirationInMinutes (ThermostatClusterSuggestions.cpp:38-39).
	minSuggestionMinutes = 30
	maxSuggestionMinutes = 1440
	// suggestionHorizon is how far ahead an EffectiveTime may lie: 24 h
	// (ThermostatClusterSuggestions.cpp:192-198).
	suggestionHorizon = 24 * 60 * 60
	// holdOn is TemperatureSetpointHoldEnum SetpointHoldOn.
	holdOn = uint8(tdef.TemperatureSetpointHoldSetpointHoldOn)
)

// suggestions is the TSUGGEST state; the server's mu guards it.
type suggestions struct {
	// max is MaxThermostatSuggestions.
	max uint8
	// list is ThermostatSuggestions in the order they were added.
	list []Suggestion
	// current is the unique id of CurrentThermostatSuggestion; has is false
	// for null.
	current uint8
	has     bool
	// hostReason is the device's own reasons for not following the current
	// suggestion (ThermostatSuggestionNotFollowingReasonBitmap), 0 for
	// none; reason the attribute as served, nil for null.
	hostReason uint16
	reason     *uint16
	// timer re-evaluates the suggestions when the current one expires or
	// a later one takes effect; nil when nothing is due.
	timer clock.Timer
	// pending marks a re-evaluation scheduled (scheduleSuggestionReevaluation)
	// that has not started: a re-evaluation already running read the Matter
	// time before it was scheduled, so it leaves the scheduled timer armed
	// instead of replacing it (armSuggestionTimerLocked).
	pending bool
	// quiesced marks the timer MatterQuiesce stopped: the SetEndpoint of
	// a reassembly that puts the server back re-evaluates and arms it.
	quiesced bool
	// lastID is the UniqueID issued last (matter.js
	// internal.lastSuggestionUniqueId, ThermostatServer.ts:2287 at
	// b25ed960, seeded with the largest stored id — 0 here, where the
	// list starts empty).
	lastID uint8
	// overridden is matter.js's overriddenSuggestionId (:3183 at
	// b25ed960): the UniqueID of the entry a client or the application
	// overrode by changing ActivePresetHandle away from its preset;
	// overriddenSet is false for none.
	overridden    uint8
	overriddenSet bool
	// setpointWritten marks a SetAttributes that wrote a setpoint directly
	// and scheduled the re-evaluation: matter.js's
	// #setpointChangedInTransaction (:796-808 at b25ed960) for the one
	// local change that, in Go, is a transaction. The next re-evaluation
	// reads and clears it.
	setpointWritten bool
}

// seedSuggestions takes the TSUGGEST configuration: MaxThermostatSuggestions
// (0 is 5), held to its constraint ("min 5"), and the node's Matter time.
func (s *ThermostatServer) seedSuggestions(cfg ThermostatConfig) error {
	limit := cfg.MaxThermostatSuggestions
	if limit == 0 {
		limit = defaultMaxSuggestions
	}
	if _, err := s.inst.CheckValue(tdef.AttrMaxThermostatSuggestions, limit, nil); err != nil {
		return spec.Errorf(im.StatusConstraintError, "thermo: MaxThermostatSuggestions %d: %v", limit, err)
	}
	s.suggest = suggestions{max: limit}
	return nil
}

// AttachMatterTime hands the server the Matter time its suggestions
// (TSUGGEST) are judged against — the server's only input of it. A node
// calls it as the server joins its topology, with the root
// TimeSynchronization's UTC in epoch seconds
// (core.TimeSynchronization.MatterEpochSeconds, false while the node has
// no time): connectedhomeip gates every suggestion on the one system clock
// SetUTCTime sets (ThermostatClusterSuggestions.cpp:173-178,
// TimeSynchronizationCluster.cpp:856-872 at 6170af84). A server outside a
// node has no Matter time until its host attaches one: every
// AddThermostatSuggestion answers INVALID_IN_STATE, as chip's does while
// GetClock_MatterEpochS fails. nil leaves the time unchanged.
func (s *ThermostatServer) AttachMatterTime(matterTime clock.MatterTime) {
	if matterTime != nil {
		s.matterTime.Store(&matterTime)
	}
}

// MatterTimeChanged re-evaluates the suggestions on the moved Matter time,
// on the command queue: a suggestion whose ExpirationTime the new time
// has passed leaves the list, one whose EffectiveTime it reached becomes
// current, and the timer is armed anew from the new time. A node calls it
// whenever it adopts a time; the host of a server outside a node calls it
// when its time does. Beyond connectedhomeip, whose suggestion timer runs
// on the monotonic clock and does not see a change of UTC
// (thermostat-suggestions-delegate-impl.cpp:264-269 at 6170af84): the
// times a controller wrote are Matter epoch seconds, so a moved time
// moves what is due.
func (s *ThermostatServer) MatterTimeChanged() { s.scheduleSuggestionReevaluation() }

// RequiresMatterTime is false: the Thermostat serves TSUGGEST without the
// node's time, each suggestion refused with INVALID_IN_STATE while there
// is none — connectedhomeip's thermostat has no TimeSynchronization
// dependency and gates each AddThermostatSuggestion on
// GetClock_MatterEpochS (ThermostatClusterSuggestions.cpp:173-178 at
// 6170af84).
func (s *ThermostatServer) RequiresMatterTime() bool { return false }

// matterNow is the Matter time in epoch seconds; false while it is not
// synchronised — always before [ThermostatServer.AttachMatterTime].
func (s *ThermostatServer) matterNow() (uint32, bool) {
	f := s.matterTime.Load()
	if f == nil {
		return 0, false
	}
	return (*f)()
}

// readSuggestionLocked reads the four TSUGGEST attributes; false for any
// other. The caller holds s.mu.
func (s *ThermostatServer) readSuggestionLocked(attrID uint32) (any, bool) {
	switch attrID {
	case tdef.AttrMaxThermostatSuggestions:
		return s.suggest.max, true
	case tdef.AttrThermostatSuggestions:
		return spec.List[Suggestion](cloneSuggestions(s.suggest.list)), true
	case tdef.AttrCurrentThermostatSuggestion:
		if c, ok := s.currentSuggestionLocked(); ok {
			return c, true
		}
		return nil, true
	case tdef.AttrThermostatSuggestionNotFollowingReason:
		if s.suggest.reason == nil {
			return nil, true
		}
		return *s.suggest.reason, true
	}
	return nil, false
}

// cloneSuggestions deep-copies a suggestion list.
func cloneSuggestions(in []Suggestion) []Suggestion {
	out := make([]Suggestion, len(in))
	for i := range in {
		out[i] = in[i].Clone() // the generated deep copy (decision 142)
	}
	return out
}

// currentSuggestionLocked is CurrentThermostatSuggestion; false for null.
// The caller holds s.mu.
func (s *ThermostatServer) currentSuggestionLocked() (Suggestion, bool) {
	if !s.suggest.has {
		return Suggestion{}, false
	}
	for _, sg := range s.suggest.list {
		if sg.UniqueId == s.suggest.current {
			return sg.Clone(), true
		}
	}
	return Suggestion{}, false
}

// Suggestions returns a copy of ThermostatSuggestions.
func (s *ThermostatServer) Suggestions() []Suggestion {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneSuggestions(s.suggest.list)
}

// CurrentSuggestion returns CurrentThermostatSuggestion; false for null.
func (s *ThermostatServer) CurrentSuggestion() (Suggestion, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.currentSuggestionLocked()
}

// suggestionAddRequest normalises the AddThermostatSuggestion payload.
func suggestionAddRequest(fields any) (tdef.AddThermostatSuggestionRequest, error) {
	switch v := fields.(type) {
	case tdef.AddThermostatSuggestionRequest:
		return v, nil
	case *tdef.AddThermostatSuggestionRequest:
		if v != nil {
			return *v, nil
		}
	}
	return tdef.AddThermostatSuggestionRequest{}, spec.Errorf(im.StatusInvalidCommand, "thermo: AddThermostatSuggestion carried %T", fields)
}

// suggestionRemoveRequest normalises the RemoveThermostatSuggestion payload.
func suggestionRemoveRequest(fields any) (tdef.RemoveThermostatSuggestionRequest, error) {
	switch v := fields.(type) {
	case tdef.RemoveThermostatSuggestionRequest:
		return v, nil
	case *tdef.RemoveThermostatSuggestionRequest:
		if v != nil {
			return *v, nil
		}
	}
	return tdef.RemoveThermostatSuggestionRequest{}, spec.Errorf(im.StatusInvalidCommand, "thermo: RemoveThermostatSuggestion carried %T", fields)
}

// handleAddThermostatSuggestion is AddThermostatSuggestion. Mirrors
// matter.js ThermostatServer.ts:613-646 at b25ed960
// (addThermostatSuggestion): the field constraints first — a preset handle
// longer than 16 bytes or a lifetime outside 30 to 1440 minutes is
// CONSTRAINT_ERROR, matter.js's schema check (:609) —, then a Matter time
// that is not synchronised INVALID_IN_STATE, a handle that names no preset
// NOT_FOUND, a full list RESOURCE_EXHAUSTED (expired entries count) and
// an EffectiveTime more than 24 h ahead INVALID_COMMAND. The suggestion
// takes the next unique id, takes effect at EffectiveTime (null: now) and
// expires ExpirationInMinutes later; the current suggestion is
// re-evaluated and the response carries the id.
func (s *ThermostatServer) handleAddThermostatSuggestion(ctx context.Context, fields any) (any, error) {
	req, err := suggestionAddRequest(fields)
	if err != nil {
		return nil, err
	}
	if len(req.PresetHandle) > presetHandleLength {
		return nil, spec.Errorf(im.StatusConstraintError, "thermo: PresetHandle of %d bytes, at most %d", len(req.PresetHandle), presetHandleLength)
	}
	if req.ExpirationInMinutes < minSuggestionMinutes || req.ExpirationInMinutes > maxSuggestionMinutes {
		return nil, spec.Errorf(im.StatusConstraintError, "thermo: ExpirationInMinutes %d outside %d to %d",
			req.ExpirationInMinutes, minSuggestionMinutes, maxSuggestionMinutes)
	}
	now, synced := s.matterNow()
	if !synced {
		return nil, spec.Errorf(im.StatusInvalidInState, "thermo: the node's Matter time is not synchronised")
	}
	s.op.Lock()
	defer s.op.Unlock()
	s.mu.Lock()
	known := s.presetFor(req.PresetHandle)
	full := len(s.suggest.list) >= int(s.suggest.max)
	switch {
	case !known:
		s.mu.Unlock()
		return nil, spec.Errorf(im.StatusNotFound, "thermo: PresetHandle %s names no preset", handleKey(req.PresetHandle))
	case full:
		s.mu.Unlock()
		return nil, spec.Errorf(im.StatusResourceExhausted, "thermo: %d thermostat suggestions held, the most there may be", s.suggest.max)
	case !req.EffectiveTime.Null && uint64(req.EffectiveTime.Value) > uint64(now)+suggestionHorizon:
		s.mu.Unlock()
		return nil, spec.Errorf(im.StatusInvalidCommand, "thermo: EffectiveTime %d is more than 24 h after %d", req.EffectiveTime.Value, now)
	}
	_, currentGone := s.removeExpiredLocked(now)
	id := s.nextSuggestionIDLocked()
	effective := now
	if !req.EffectiveTime.Null {
		effective = req.EffectiveTime.Value
	}
	s.suggest.list = append(s.suggest.list, Suggestion{
		UniqueId:       id,
		PresetHandle:   bytes.Clone(req.PresetHandle),
		EffectiveTime:  effective,
		ExpirationTime: effective + uint32(req.ExpirationInMinutes)*60,
	})
	s.mu.Unlock()
	ids := []uint32{tdef.AttrThermostatSuggestions}
	if currentGone {
		ids = append(ids, tdef.AttrCurrentThermostatSuggestion)
	}
	s.changes.NotifyFrom(contract.OriginFrom(ctx), ids...)
	s.reevaluateSuggestions(ctx)
	return tdef.AddThermostatSuggestionResponse{UniqueId: id}, nil
}

// handleRemoveThermostatSuggestion is RemoveThermostatSuggestion. Mirrors
// matter.js ThermostatServer.ts:653-660 at b25ed960
// (removeThermostatSuggestion): an id no suggestion carries is NOT_FOUND;
// otherwise the suggestion goes and the current suggestion is
// re-evaluated, which also removes the expired entries. Removing the
// current suggestion nulls it until the re-evaluation chooses again.
func (s *ThermostatServer) handleRemoveThermostatSuggestion(ctx context.Context, fields any) error {
	req, err := suggestionRemoveRequest(fields)
	if err != nil {
		return err
	}
	// The Matter time is a host's callback chain: read before s.mu, as Add
	// and reevaluateSuggestions read it.
	now, synced := s.matterNow()
	s.op.Lock()
	defer s.op.Unlock()
	s.mu.Lock()
	i := slices.IndexFunc(s.suggest.list, func(sg Suggestion) bool { return sg.UniqueId == req.UniqueId })
	if i < 0 {
		s.mu.Unlock()
		return spec.Errorf(im.StatusNotFound, "thermo: no thermostat suggestion has UniqueID %d", req.UniqueId)
	}
	s.suggest.list = slices.Delete(s.suggest.list, i, i+1)
	currentGone := s.suggest.has && s.suggest.current == req.UniqueId
	if currentGone {
		s.suggest.has = false
	}
	if synced {
		if _, gone := s.removeExpiredLocked(now); gone {
			currentGone = true
		}
	}
	s.mu.Unlock()
	ids := []uint32{tdef.AttrThermostatSuggestions}
	if currentGone {
		ids = append(ids, tdef.AttrCurrentThermostatSuggestion)
	}
	s.changes.NotifyFrom(contract.OriginFrom(ctx), ids...)
	s.reevaluateSuggestions(ctx)
	return nil
}

// removeExpiredLocked drops every suggestion whose ExpirationTime is at
// or before now (matter.js ThermostatServer.ts:2429 at b25ed960) and
// reports whether one went and whether the current one did — it is no
// longer current. The caller holds s.mu.
func (s *ThermostatServer) removeExpiredLocked(now uint32) (removed, currentGone bool) {
	n := len(s.suggest.list)
	s.suggest.list = slices.DeleteFunc(s.suggest.list, func(sg Suggestion) bool {
		if sg.ExpirationTime > now {
			return false
		}
		if s.suggest.has && sg.UniqueId == s.suggest.current {
			s.suggest.has, currentGone = false, true
		}
		return true
	})
	return len(s.suggest.list) != n, currentGone
}

// nextSuggestionIDLocked is the UniqueID after the last one issued that no
// entry carries, modulo 256, so a just-removed id is not issued again at
// once. Mirrors matter.js ThermostatServer.ts:2532-2540 at b25ed960
// (#nextSuggestionUniqueId); the list never holds all 256 values (at most
// MaxThermostatSuggestions, a uint8). The caller holds s.mu.
func (s *ThermostatServer) nextSuggestionIDLocked() uint8 {
	held := func(id uint8) bool {
		return slices.ContainsFunc(s.suggest.list, func(sg Suggestion) bool { return sg.UniqueId == id })
	}
	id := s.suggest.lastID + 1
	for held(id) {
		id++
	}
	s.suggest.lastID = id
	return id
}

// reevaluateSuggestions re-evaluates the current suggestion. Mirrors
// matter.js ThermostatServer.ts:2368-2484 at b25ed960
// (#reevaluateSuggestions, #determineCurrentSuggestion,
// #setCurrentSuggestion): without a synchronised Matter time
// CurrentThermostatSuggestion and the reason are null, the override stays
// and the timer stops (:2421-2426, :2499-2501); otherwise the expired
// entries go, the current suggestion is the one with the earliest
// EffectiveTime at or before now, the first in the list on a tie
// (:731-737), and the thermostat follows it — activates its preset as
// SetActivePresetRequest does and moves the setpoints (:2459) — unless a
// reason applies: the host's own reasons or OngoingHold while a hold is
// on (suggestionNotFollowingReason, :747-754), else the empty reason for
// an overridden entry (:2449-2450), which also starts with a setpoint the
// same SetAttributes wrote while the preset is not active (:2451-2454). A
// reason keeps the active preset. Another entry or none becoming current
// ends the override (:2467-2477). The timer is armed for the earliest
// EffectiveTime or ExpirationTime of all entries still ahead (:2505-2510).
// The caller holds s.op.
func (s *ThermostatServer) reevaluateSuggestions(ctx context.Context) {
	if s.features&ThermostatFeatureTSUGGEST == 0 {
		return
	}
	// A scheduled re-evaluation is this one from here on; one scheduled
	// after this point re-reads the time itself.
	s.mu.Lock()
	s.suggest.pending = false
	written := s.suggest.setpointWritten
	s.suggest.setpointWritten = false
	s.mu.Unlock()
	now, synced := s.matterNow()
	s.mu.Lock()
	before, hadBefore := s.suggest.current, s.suggest.has
	if !synced {
		// Without synchronised time no entry is known to be current, so an
		// override stays until time returns.
		s.suggest.has = false
		reasonChanged := s.suggest.reason != nil
		s.suggest.reason = nil
		s.armSuggestionTimerLocked(0, 0)
		s.mu.Unlock()
		s.notifySuggestionChanges(ctx, false, hadBefore, reasonChanged)
		return
	}
	removed, _ := s.removeExpiredLocked(now)
	follow, reason, wake := s.determineCurrentLocked(now, written)
	currentChanged := hadBefore != s.suggest.has || (s.suggest.has && before != s.suggest.current)
	reasonChanged := !equalReason(s.suggest.reason, reason)
	s.suggest.reason = reason
	s.armSuggestionTimerLocked(now, wake)
	s.mu.Unlock()

	s.notifySuggestionChanges(ctx, removed, currentChanged, reasonChanged)
	if follow != nil {
		if err := s.activatePreset(ctx, follow); err != nil {
			s.logger().Warn("matter.thermostat.suggestion_not_followed",
				slog.String("preset_handle", handleKey(follow)), slog.String("reason", err.Error()))
		}
	}
}

// determineCurrentLocked chooses the current suggestion among the entries
// in effect at now, sets it and the override, and returns the preset to
// follow (nil: none), the reason not to follow (nil: null) and the
// earliest EffectiveTime or ExpirationTime still ahead (0: none). written
// is suggest.setpointWritten. Mirrors matter.js ThermostatServer.ts:2434-2477
// at b25ed960 (#determineCurrentSuggestion, #setCurrentSuggestion), the
// timer's times :2505-2510 (#restartSuggestionTimer). The caller holds
// s.mu.
func (s *ThermostatServer) determineCurrentLocked(now uint32, written bool) (follow []byte, reason *uint16, wake uint32) {
	var current *Suggestion
	soonest := func(t uint32) {
		if t > now && (wake == 0 || t < wake) {
			wake = t
		}
	}
	for i := range s.suggest.list {
		sg := &s.suggest.list[i]
		if sg.EffectiveTime <= now && (current == nil || sg.EffectiveTime < current.EffectiveTime) {
			current = sg
		}
		soonest(sg.EffectiveTime)
		soonest(sg.ExpirationTime)
	}
	if current == nil {
		s.suggest.has = false
		s.suggest.overriddenSet = false
		return nil, nil, wake
	}
	id := current.UniqueId
	presetActive := s.activePresetHandle != nil && bytes.Equal(s.activePresetHandle, current.PresetHandle)
	bits := s.suggest.hostReason
	if s.hold == holdOn {
		bits |= uint16(tdef.ThermostatSuggestionNotFollowingReasonOngoingHold)
	}
	var none uint16
	switch {
	case bits != 0:
		reason = &bits
	case s.suggest.overriddenSet && s.suggest.overridden == id:
		reason = &none
	case !presetActive && written:
		s.suggest.overridden, s.suggest.overriddenSet = id, true
		reason = &none
	}
	s.suggest.current, s.suggest.has = id, true
	if s.suggest.overriddenSet && s.suggest.overridden != id {
		s.suggest.overriddenSet = false
	}
	if reason == nil && !presetActive {
		follow = bytes.Clone(current.PresetHandle)
	}
	return follow, reason, wake
}

// notifySuggestionChanges reports the suggestion attributes a
// re-evaluation changed.
func (s *ThermostatServer) notifySuggestionChanges(ctx context.Context, list, current, reason bool) {
	var ids []uint32
	if list {
		ids = append(ids, tdef.AttrThermostatSuggestions)
	}
	if current {
		ids = append(ids, tdef.AttrCurrentThermostatSuggestion)
	}
	if reason {
		ids = append(ids, tdef.AttrThermostatSuggestionNotFollowingReason)
	}
	if len(ids) > 0 {
		s.changes.NotifyFrom(contract.OriginFrom(ctx), ids...)
	}
}

// trackOverrideLocked is matter.js's #trackOverrideOnPresetChange
// (ThermostatServer.ts:2310-2348 at b25ed960), run when ActivePresetHandle
// changed to handle: against the current suggestion — or, while none is
// current, the overridden entry — a change to its preset ends the
// override, a change to another value (null included) overrides it,
// except a setpoint write (setpointWrite) during a hold, which belongs to
// the hold: the last re-evaluation's reason carries OngoingHold. Following
// only activates the suggestion's preset, so it never overrides. It
// reports whether the override changed; the caller then re-evaluates. The
// caller holds s.mu.
func (s *ThermostatServer) trackOverrideLocked(handle []byte, setpointWrite bool) bool {
	if s.features&ThermostatFeatureTSUGGEST == 0 {
		return false
	}
	var current *Suggestion
	for i := range s.suggest.list {
		sg := &s.suggest.list[i]
		if (s.suggest.has && sg.UniqueId == s.suggest.current) ||
			(!s.suggest.has && s.suggest.overriddenSet && sg.UniqueId == s.suggest.overridden) {
			current = sg
			break
		}
	}
	if current == nil {
		return false
	}
	matches := handle != nil && bytes.Equal(handle, current.PresetHandle)
	belongsToHold := setpointWrite && s.suggest.reason != nil &&
		*s.suggest.reason&uint16(tdef.ThermostatSuggestionNotFollowingReasonOngoingHold) != 0
	if !matches && belongsToHold {
		return false
	}
	if matches {
		if !s.suggest.overriddenSet {
			return false
		}
		s.suggest.overriddenSet = false
		return true
	}
	if s.suggest.overriddenSet && s.suggest.overridden == current.UniqueId {
		return false
	}
	s.suggest.overridden, s.suggest.overriddenSet = current.UniqueId, true
	return true
}

// reevaluateOnPresetsChange removes the suggestions that name a preset the
// presets no longer carry and re-evaluates. Mirrors matter.js
// ThermostatServer.ts:2556-2565 at b25ed960 (#reevaluateOnPresetsChange,
// called from the presets' commit, :1792-1794). The caller holds s.op.
func (s *ThermostatServer) reevaluateOnPresetsChange(ctx context.Context) {
	if s.features&ThermostatFeatureTSUGGEST == 0 {
		return
	}
	s.mu.Lock()
	n := len(s.suggest.list)
	currentGone := false
	s.suggest.list = slices.DeleteFunc(s.suggest.list, func(sg Suggestion) bool {
		if s.presetFor(sg.PresetHandle) {
			return false
		}
		if s.suggest.has && sg.UniqueId == s.suggest.current {
			s.suggest.has, currentGone = false, true
		}
		return true
	})
	removed := len(s.suggest.list) != n
	s.mu.Unlock()
	s.notifySuggestionChanges(ctx, removed, currentGone, false)
	s.reevaluateSuggestions(ctx)
}

// presetFor reports whether a stored preset carries handle (matter.js
// #presetFor, ThermostatServer.ts:2542-2546 at b25ed960). The caller holds
// s.mu.
func (s *ThermostatServer) presetFor(handle []byte) bool {
	return slices.ContainsFunc(s.presets, func(p Preset) bool {
		return !p.PresetHandle.Null && bytes.Equal(p.PresetHandle.Value, handle)
	})
}

// equalReason compares two nullable ThermostatSuggestionNotFollowingReason values.
func equalReason(a, b *uint16) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// armSuggestionTimerLocked replaces the suggestion timer with one that
// fires at the Matter epoch second wake (0: none), measured from now on the
// server's clock. The caller holds s.mu.
func (s *ThermostatServer) armSuggestionTimerLocked(now, wake uint32) {
	if s.suggest.pending {
		// A re-evaluation was scheduled after this one read the time (a
		// moved Matter time, a hold, a reason): its timer stays, and it
		// arms the next one from the time it reads.
		return
	}
	if s.suggest.timer != nil {
		s.suggest.timer.Stop()
		s.suggest.timer = nil
	}
	if wake == 0 || wake <= now {
		return
	}
	s.suggest.timer = s.clk.AfterFunc(time.Duration(wake-now)*time.Second, s.suggestionTimerFired)
}

// suggestionTimerFired re-evaluates the suggestions when an entry's
// EffectiveTime or ExpirationTime arrived (matter.js
// #reevaluateSuggestionsOnTimer, ThermostatServer.ts:2392-2406 at
// b25ed960). It takes the command queue, as a command does.
func (s *ThermostatServer) suggestionTimerFired() {
	s.op.Lock()
	defer s.op.Unlock()
	s.mu.Lock()
	stopped := s.suggest.timer == nil && !s.suggest.pending
	s.mu.Unlock()
	if stopped {
		// Stopped after it fired, before it got the queue: MatterQuiesce, or
		// a re-evaluation that found nothing due.
		return
	}
	s.reevaluateSuggestions(contract.WithOrigin(context.Background(), contract.OriginCommand))
}

// scheduleSuggestionReevaluation re-evaluates the suggestions on the
// command queue, off the caller's goroutine: the inbound setters take only
// s.mu, so a hold or a not-following reason the host reports — from inside
// the Sink as well — re-evaluates without taking the queue.
func (s *ThermostatServer) scheduleSuggestionReevaluation() {
	if s.features&ThermostatFeatureTSUGGEST == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.suggest.timer != nil {
		s.suggest.timer.Stop()
	}
	s.suggest.pending = true
	s.suggest.timer = s.clk.AfterFunc(0, s.suggestionTimerFired)
}
