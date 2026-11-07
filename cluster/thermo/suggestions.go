// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package thermo

import (
	"bytes"
	"context"
	"log/slog"
	"math"
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
// matter.js does not implement the feature (ThermostatServer.ts at
// 862ad75f has no suggestion logic; register row
// BD-chip-Thermostat-Suggestions, notes/parity/by_design.md). The server
// follows connectedhomeip, which keeps the rules in the cluster and the
// list in a delegate: the command checks and their order
// (src/app/clusters/thermostat-server/ThermostatClusterSuggestions.cpp:160-246
// at 6170af84), RemoveThermostatSuggestion (:248-269), the removal of
// expired suggestions (:41-67) and the change notifications (:271-333).
// The list, the unique ids, the choice of the current suggestion and its
// expiry timer are the reference delegate's
// (examples/thermostat/thermostat-common/src/thermostat-suggestions-delegate-impl.cpp:120-298
// at 6170af84), held here by the server itself. Where this server goes
// beyond the reference delegate the entry says so.

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
	for i, sg := range in {
		sg.PresetHandle = bytes.Clone(sg.PresetHandle)
		out[i] = sg
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
			sg.PresetHandle = bytes.Clone(sg.PresetHandle)
			return sg, true
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

// handleAddThermostatSuggestion is connectedhomeip's
// ThermostatSuggestions::AddThermostatSuggestion
// (ThermostatClusterSuggestions.cpp:159-246 at 6170af84), its checks in its
// order: a preset handle longer than 16 bytes or a lifetime outside 30 to
// 1440 minutes is CONSTRAINT_ERROR (:163-171), a Matter time that is not
// synchronised INVALID_IN_STATE (:173-178), a handle that names no preset
// NOT_FOUND (:180-184), a full list RESOURCE_EXHAUSTED — before the
// expired suggestions are removed, as there (:186-190, :200-207) — and an
// EffectiveTime more than 24 h ahead INVALID_COMMAND (:192-198). The
// suggestion takes the next unique id, takes effect at EffectiveTime (null:
// now) and expires ExpirationInMinutes later (:209-233); the list changed
// (:235), the current suggestion is re-evaluated (:237-238) and the
// response carries the id (:240-243).
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
	known := slices.ContainsFunc(s.presets, func(p Preset) bool {
		return !p.PresetHandle.Null && bytes.Equal(p.PresetHandle.Value, req.PresetHandle)
	})
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
	id, ok := s.nextSuggestionIDLocked()
	if !ok {
		s.mu.Unlock()
		return nil, spec.Errorf(im.StatusFailure, "thermo: no free suggestion id")
	}
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

// handleRemoveThermostatSuggestion is connectedhomeip's
// RemoveThermostatSuggestion (ThermostatClusterSuggestions.cpp:248-269 at
// 6170af84): an id no suggestion carries is NOT_FOUND (:69-95); otherwise
// the suggestion goes, the list changed, the expired suggestions go too
// and the current suggestion is re-evaluated (:262-266). Removing the
// current suggestion nulls it (the reference delegate's
// RemoveFromThermostatSuggestionsList, thermostat-suggestions-delegate-impl.cpp:144-148).
func (s *ThermostatServer) handleRemoveThermostatSuggestion(ctx context.Context, fields any) error {
	req, err := suggestionRemoveRequest(fields)
	if err != nil {
		return err
	}
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
	if now, ok := s.matterNow(); ok {
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
// or before now (ThermostatClusterSuggestions.cpp:41-67 at 6170af84) and
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

// nextSuggestionIDLocked is the reference delegate's GetUniqueID
// (thermostat-suggestions-delegate-impl.cpp:169-197 at 6170af84): one
// above the largest id held; past 255 the lowest free id from 0, and none
// when the search reaches 255. The caller holds s.mu.
func (s *ThermostatServer) nextSuggestionIDLocked() (uint8, bool) {
	var highest uint8
	for _, sg := range s.suggest.list {
		highest = max(highest, sg.UniqueId)
	}
	id := highest + 1
	if id != 0 {
		return id, true
	}
	held := func(id uint8) bool {
		return slices.ContainsFunc(s.suggest.list, func(sg Suggestion) bool { return sg.UniqueId == id })
	}
	for held(id) {
		id++
		if id == math.MaxUint8 {
			return 0, false
		}
	}
	return id, true
}

// reevaluateSuggestions is connectedhomeip's ReEvaluateCurrentSuggestion
// (ThermostatClusterSuggestions.cpp:271-333 at 6170af84) over the
// reference delegate's (thermostat-suggestions-delegate-impl.cpp:234-298):
// without a synchronised Matter time nothing changes (:238-244); otherwise
// the expired suggestions go, the current suggestion is the one with the
// earliest EffectiveTime at or before now (:248-249, :275-298; the first
// added on a tie), and CurrentThermostatSuggestion is reported when it
// changed (ThermostatClusterSuggestions.cpp:297-308). The thermostat follows it — switches to its preset,
// as SetActivePresetRequest does, and reports
// ThermostatSuggestionNotFollowingReason null (:254-262) — unless a hold
// is on (OngoingHold, the reason the reference delegate's TODO at
// thermostat-suggestions-delegate-impl.cpp:258-260 and the commented
// asserts of src/python_testing/TC_TSTAT_4_3.py:368-375 name) or the device reported
// reasons of its own; then the reason is reported and the active preset
// stays. The timer is armed for the current suggestion's expiry
// (thermostat-suggestions-delegate-impl.cpp:264-269) and, beyond the reference delegate, for the next suggestion to take
// effect. The caller holds s.op.
func (s *ThermostatServer) reevaluateSuggestions(ctx context.Context) {
	if s.features&ThermostatFeatureTSUGGEST == 0 {
		return
	}
	now, synced := s.matterNow()
	if !synced {
		return
	}
	s.mu.Lock()
	before, hadBefore := s.suggest.current, s.suggest.has
	removed, _ := s.removeExpiredLocked(now)
	var current *Suggestion
	var wake uint32
	for i := range s.suggest.list {
		sg := &s.suggest.list[i]
		if sg.EffectiveTime <= now {
			if current == nil || sg.EffectiveTime < current.EffectiveTime {
				current = sg
			}
		} else if wake == 0 || sg.EffectiveTime < wake {
			wake = sg.EffectiveTime
		}
	}
	var follow []byte
	var reason *uint16
	if current != nil {
		s.suggest.current, s.suggest.has = current.UniqueId, true
		if wake == 0 || current.ExpirationTime < wake {
			wake = current.ExpirationTime
		}
		bits := s.suggest.hostReason
		if s.hold == holdOn {
			bits |= uint16(tdef.ThermostatSuggestionNotFollowingReasonOngoingHold)
		}
		if bits != 0 {
			reason = &bits
		} else if !bytes.Equal(s.activePresetHandle, current.PresetHandle) || s.activePresetHandle == nil {
			follow = bytes.Clone(current.PresetHandle)
		}
	} else {
		s.suggest.has = false
	}
	currentChanged := hadBefore != s.suggest.has || (s.suggest.has && before != s.suggest.current)
	reasonChanged := !equalReason(s.suggest.reason, reason)
	s.suggest.reason = reason
	s.armSuggestionTimerLocked(now, wake)
	s.mu.Unlock()

	var ids []uint32
	if removed {
		ids = append(ids, tdef.AttrThermostatSuggestions)
	}
	if currentChanged {
		ids = append(ids, tdef.AttrCurrentThermostatSuggestion)
	}
	if reasonChanged {
		ids = append(ids, tdef.AttrThermostatSuggestionNotFollowingReason)
	}
	if len(ids) > 0 {
		s.changes.NotifyFrom(contract.OriginFrom(ctx), ids...)
	}
	if follow != nil {
		if err := s.activatePreset(ctx, follow); err != nil {
			s.logger().Warn("matter.thermostat.suggestion_not_followed",
				slog.String("preset_handle", handleKey(follow)), slog.String("reason", err.Error()))
		}
	}
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
	if s.suggest.timer != nil {
		s.suggest.timer.Stop()
		s.suggest.timer = nil
	}
	if wake == 0 || wake <= now {
		return
	}
	s.suggest.timer = s.clk.AfterFunc(time.Duration(wake-now)*time.Second, s.suggestionTimerFired)
}

// suggestionTimerFired re-evaluates the suggestions when the current one
// expired or a later one took effect — the reference delegate's
// TimerExpiredCallback (thermostat-suggestions-delegate-impl.cpp:213-222
// at 6170af84), which removes no expired suggestion; this server does,
// so the expired current suggestion is not chosen again. It takes the
// command queue, as a command does.
func (s *ThermostatServer) suggestionTimerFired() {
	s.op.Lock()
	defer s.op.Unlock()
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
	s.suggest.timer = s.clk.AfterFunc(0, s.suggestionTimerFired)
}
