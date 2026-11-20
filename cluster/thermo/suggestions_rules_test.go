// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// ThermostatSuggestions beyond the matter.js port
// (thermostat_suggestions_parity_matterjs_test.go) and the chip cases
// (suggestions_tc_tstat_4_3_test.go): the expiry and effective-time timer
// on the server's clock, the expired suggestions an Add removes, the
// unique ids, the device's own reasons, the hold the host reports, a Sink
// that refuses the preset, and the configuration. Every case runs on the
// fake clock.
package thermo_test

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/spec"
	tdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	"github.com/SukramJ/go-fabric/cluster/thermo"
	"github.com/SukramJ/go-fabric/im"
)

// TestSuggestionExpiresOnTheTimer pins the reference delegate's expiry
// timer (thermostat-suggestions-delegate-impl.cpp:264-269 at 6170af84) and
// this server's removal of the expired suggestion when it fires: the list
// empties and the current suggestion turns null, the active preset stays.
func TestSuggestionExpiresOnTheTimer(t *testing.T) {
	t.Parallel()
	clk := newSuggestClock()
	srv := newTSUGGEST(t, tsuggestConfig(clk))
	changes := recordChanges(srv)
	if _, err := addSuggestion(srv, handleB, nil, 30); err != nil {
		t.Fatal(err)
	}
	changes()
	clk.Advance(30*time.Minute - time.Second)
	if _, ok := readCurrent(t, srv); !ok {
		t.Fatal("the suggestion is gone before it expired")
	}
	clk.Advance(time.Second)
	if got := readSuggestions(t, srv); len(got) != 0 {
		t.Errorf("ThermostatSuggestions %v after the expiry, want none", got)
	}
	if _, ok := readCurrent(t, srv); ok {
		t.Error("CurrentThermostatSuggestion not null after the expiry")
	}
	if got := changes(); !slices.Contains(got, tdef.AttrThermostatSuggestions) || !slices.Contains(got, tdef.AttrCurrentThermostatSuggestion) {
		t.Errorf("changes %v, want the list and the current suggestion", got)
	}
	if got := srv.ActivePresetHandle(); !bytes.Equal(got, handleB) {
		t.Errorf("ActivePresetHandle %q, want %q kept", got, handleB)
	}
}

// TestSuggestionTakesEffectLater pins the timer this server arms for a
// suggestion whose EffectiveTime lies ahead, which the reference delegate
// leaves to the next command: it is listed, not current, and becomes
// current — and followed — when its time comes.
func TestSuggestionTakesEffectLater(t *testing.T) {
	t.Parallel()
	clk := newSuggestClock()
	srv := newTSUGGEST(t, tsuggestConfig(clk))
	at := clk.epoch() + 600
	id, err := addSuggestion(srv, handleB, &at, 30)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := readCurrent(t, srv); ok {
		t.Fatal("a suggestion that takes effect in ten minutes is current")
	}
	if got := srv.ActivePresetHandle(); got != nil {
		t.Errorf("ActivePresetHandle %q before the suggestion took effect", got)
	}
	clk.Advance(600 * time.Second)
	if cur, ok := readCurrent(t, srv); !ok || cur.UniqueId != id || cur.ExpirationTime != at+30*60 {
		t.Errorf("CurrentThermostatSuggestion %+v, %v; want id %d expiring at %d", cur, ok, id, at+30*60)
	}
	if got := srv.ActivePresetHandle(); !bytes.Equal(got, handleB) {
		t.Errorf("ActivePresetHandle %q, want %q", got, handleB)
	}
}

// TestAddRemovesExpiredSuggestionsAfterTheFullCheck pins the order of
// ThermostatClusterSuggestions.cpp:186-207 at 6170af84: a list full of
// expired suggestions is RESOURCE_EXHAUSTED — the check runs before the
// removal —, and an Add on a list with room removes the expired ones.
func TestAddRemovesExpiredSuggestionsAfterTheFullCheck(t *testing.T) {
	t.Parallel()
	clk := newSuggestClock()
	srv := newTSUGGEST(t, tsuggestConfig(clk))
	for range 5 {
		if _, err := addSuggestion(srv, handleB, nil, 30); err != nil {
			t.Fatal(err)
		}
	}
	clk.StepWall(31 * time.Minute) // the Matter time moves, the timer does not fire
	if _, err := addSuggestion(srv, handleA, nil, 30); statusOfErr(t, err) != im.StatusResourceExhausted {
		t.Errorf("full of expired suggestions: %v, want RESOURCE_EXHAUSTED", err)
	}
	if err := removeSuggestion(srv, 5); err != nil { // the remove drops the four expired ones too
		t.Fatal(err)
	}
	if got := readSuggestions(t, srv); len(got) != 0 {
		t.Errorf("ThermostatSuggestions %v after the remove, want the expired ones gone", got)
	}
	if _, err := addSuggestion(srv, handleA, nil, 30); err != nil {
		t.Fatal(err)
	}
	clk.StepWall(31 * time.Minute)
	id, err := addSuggestion(srv, handleB, nil, 30)
	if err != nil {
		t.Fatal(err)
	}
	if got := readSuggestions(t, srv); len(got) != 1 || got[0].UniqueId != id {
		t.Errorf("ThermostatSuggestions %v, want only the new one", got)
	}
}

// TestSuggestionUniqueIDs pins the unique ids after the last one issued,
// modulo 256 and skipping the ids held — matter.js
// ThermostatServer.ts:2532-2540 at b25ed960 (#nextSuggestionUniqueId) —
// on the ids this server issued itself: ThermostatSuggestionsTest.ts's
// "wraps after 255 and skips values in use" starts from stored entries,
// which the server does not hold (see the register entry).
func TestSuggestionUniqueIDs(t *testing.T) {
	t.Parallel()
	clk := newSuggestClock()
	srv := newTSUGGEST(t, tsuggestConfig(clk))
	held, err := addSuggestion(srv, handleB, nil, 30)
	if err != nil || held != 1 {
		t.Fatalf("first id %d, %v; want 1", held, err)
	}
	for want := 2; want <= 255; want++ {
		id, err := addSuggestion(srv, handleB, nil, 30)
		if err != nil || int(id) != want {
			t.Fatalf("id %d, %v; want %d", id, err, want)
		}
		if err := removeSuggestion(srv, id); err != nil {
			t.Fatal(err)
		}
	}
	if id, err := addSuggestion(srv, handleB, nil, 30); err != nil || id != 0 {
		t.Errorf("after 255: id %d, %v; want 0", id, err)
	}
	if id, err := addSuggestion(srv, handleB, nil, 30); err != nil || id != 2 {
		t.Errorf("after 0, with 1 held: id %d, %v; want 2", id, err)
	}
}

// TestSuggestionUniqueIDSkipsAnExpiredEntryNotYetRemoved pins the next id
// taken against the list as stored, expired entries included, before the
// Add removes them — matter.js ThermostatServer.ts:624, :637 at
// b25ed960 (addThermostatSuggestion), which prunes only in the
// re-evaluation (:2429-2432): after the wrap, id 1 is still held by an
// expired entry and is skipped.
func TestSuggestionUniqueIDSkipsAnExpiredEntryNotYetRemoved(t *testing.T) {
	t.Parallel()
	clk := newSuggestClock()
	srv := newTSUGGEST(t, tsuggestConfig(clk))
	if id, err := addSuggestion(srv, handleB, nil, 30); err != nil || id != 1 {
		t.Fatalf("first id %d, %v; want 1", id, err)
	}
	for want := 2; want <= 255; want++ {
		id, err := addSuggestion(srv, handleB, nil, 30)
		if err != nil || int(id) != want {
			t.Fatalf("id %d, %v; want %d", id, err, want)
		}
		if err := removeSuggestion(srv, id); err != nil {
			t.Fatal(err)
		}
	}
	if id, err := addSuggestion(srv, handleB, nil, 30); err != nil || id != 0 {
		t.Fatalf("after 255: id %d, %v; want 0", id, err)
	}
	clk.StepWall(31 * time.Minute) // 1 and 0 expire; the timer does not fire
	id, err := addSuggestion(srv, handleB, nil, 30)
	if err != nil || id != 2 {
		t.Errorf("with 1 held by an expired entry: id %d, %v; want 2", id, err)
	}
	if got := readSuggestions(t, srv); len(got) != 1 || got[0].UniqueId != id {
		t.Errorf("ThermostatSuggestions %v, want only the new one", got)
	}
}

// TestSetpointOfTheOtherOccupancyIsNoOverride pins that only a setpoint of
// the occupancy in effect, written in the change that ends the hold, makes
// the current suggestion an override — matter.js ThermostatServer.ts:803-808
// at b25ed960 (#setpointChangedInTransaction): with the thermostat occupied,
// an UnoccupiedHeatingSetpoint written as the hold ends is followed past,
// and an OccupiedHeatingSetpoint is the override.
func TestSetpointOfTheOtherOccupancyIsNoOverride(t *testing.T) {
	t.Parallel()
	withOccupancy := func(cfg *thermo.ThermostatConfig) {
		cfg.Features |= thermo.ThermostatFeatureOCC
		heat, cool := int16(1800), int16(2800)
		cfg.InitialUnoccupiedHeatingSetpoint, cfg.InitialUnoccupiedCoolingSetpoint = &heat, &cool
		occupied := true
		cfg.Occupied = &occupied
	}
	holdOffAnd := func(attr uint32) map[uint32]any {
		return map[uint32]any{
			tdef.AttrTemperatureSetpointHold: uint8(tdef.TemperatureSetpointHoldSetpointHoldOff),
			attr:                             int16(1900),
		}
	}

	c := onlineSuggestions(t, withOccupancy)
	c.hold(true)
	c.mustAdd(presetB, nil, 0)
	c.wantActive(nil)
	c.set(holdOffAnd(tdef.AttrUnoccupiedHeatingSetpoint))
	c.wantReason(nil)
	c.wantActive(presetB)

	c = onlineSuggestions(t, withOccupancy)
	c.hold(true)
	c.mustAdd(presetB, nil, 0)
	c.set(holdOffAnd(tdef.AttrOccupiedHeatingSetpoint))
	c.wantReason(reasonOf(0))
	c.wantActive(nil)
}

// TestDeviceReasonsKeepTheThermostatOffTheSuggestion pins the device's own
// reasons (the reference delegate's SetThermostatSuggestionNotFollowingReason,
// thermostat-suggestions-delegate-impl.cpp:91-103 at 6170af84) and the hold
// the device reports itself: the reason is served while a suggestion is
// current, the active preset stays, and the thermostat follows once both
// are gone.
func TestDeviceReasonsKeepTheThermostatOffTheSuggestion(t *testing.T) {
	t.Parallel()
	clk := newSuggestClock()
	srv := newTSUGGEST(t, tsuggestConfig(clk))
	schedule := tdef.ThermostatSuggestionNotFollowingReasonSchedule
	if err := srv.SetAttributes(map[uint32]any{tdef.AttrThermostatSuggestionNotFollowingReason: schedule}); err != nil {
		t.Fatal(err)
	}
	clk.Advance(0)
	if r := readReason(t, srv); r != nil {
		t.Errorf("reason %d without a current suggestion, want null", *r)
	}
	if _, err := addSuggestion(srv, handleB, nil, 30); err != nil {
		t.Fatal(err)
	}
	if r := readReason(t, srv); r == nil || *r != uint16(schedule) {
		t.Errorf("reason %v, want Schedule", r)
	}
	if got := srv.ActivePresetHandle(); got != nil {
		t.Errorf("ActivePresetHandle %q while the device does not follow", got)
	}
	srv.SetTemperatureSetpointHold(uint8(tdef.TemperatureSetpointHoldSetpointHoldOn))
	clk.Advance(0)
	if r := readReason(t, srv); r == nil || *r != uint16(schedule|tdef.ThermostatSuggestionNotFollowingReasonOngoingHold) {
		t.Errorf("reason %v, want Schedule and OngoingHold", r)
	}
	if err := srv.SetAttributes(map[uint32]any{tdef.AttrThermostatSuggestionNotFollowingReason: nil}); err != nil {
		t.Fatal(err)
	}
	clk.Advance(0)
	if r := readReason(t, srv); r == nil || *r != uint16(tdef.ThermostatSuggestionNotFollowingReasonOngoingHold) {
		t.Errorf("reason %v, want OngoingHold", r)
	}
	srv.SetTemperatureSetpointHold(uint8(tdef.TemperatureSetpointHoldSetpointHoldOff))
	clk.Advance(0)
	if r := readReason(t, srv); r != nil {
		t.Errorf("reason %d, want null", *r)
	}
	if got := srv.ActivePresetHandle(); !bytes.Equal(got, handleB) {
		t.Errorf("ActivePresetHandle %q, want %q", got, handleB)
	}
}

// TestRefusedPresetLeavesTheActivePreset pins a Sink that refuses the
// suggested preset's setpoints: the suggestion is added and current, the
// active preset and the setpoints stay.
func TestRefusedPresetLeavesTheActivePreset(t *testing.T) {
	t.Parallel()
	clk := newSuggestClock()
	sink := &recordingThermoSink{err: errors.New("the device stays where it is")}
	srv := newTSUGGEST(t, tsuggestConfig(clk, func(cfg *thermo.ThermostatConfig) { cfg.Sink = sink }))
	if _, err := addSuggestion(srv, handleB, nil, 30); err != nil {
		t.Fatal(err)
	}
	if len(sink.recorded()) == 0 {
		t.Error("the Sink was not asked")
	}
	if _, ok := readCurrent(t, srv); !ok {
		t.Error("CurrentThermostatSuggestion null")
	}
	if got := srv.ActivePresetHandle(); got != nil {
		t.Errorf("ActivePresetHandle %q after the Sink refused", got)
	}
	if v, _ := srv.MatterRead(tdef.AttrOccupiedHeatingSetpoint); v != int16(2000) {
		t.Errorf("OccupiedHeatingSetpoint %v, want 2000 kept", v)
	}
}

// TestSuggestionConfiguration pins the configuration: MaxThermostatSuggestions
// at least 5 (0 is 5), a thermostat without a Matter time refusing with
// INVALID_IN_STATE, MaxThermostatSuggestions and the current suggestion
// not the host's to set, the commands unsupported without the feature,
// and the accessors.
func TestSuggestionConfiguration(t *testing.T) {
	t.Parallel()
	clk := newSuggestClock()
	if _, err := thermo.New(tsuggestConfig(clk, func(cfg *thermo.ThermostatConfig) { cfg.MaxThermostatSuggestions = 4 })); !errors.Is(err, thermo.ErrFeatures) {
		t.Errorf("MaxThermostatSuggestions 4: %v, want ErrFeatures", err)
	}
	srv := newTSUGGEST(t, tsuggestConfig(clk, func(cfg *thermo.ThermostatConfig) { cfg.MaxThermostatSuggestions = 7 }))
	if v, _ := srv.MatterRead(tdef.AttrMaxThermostatSuggestions); v != uint8(7) {
		t.Errorf("MaxThermostatSuggestions %v, want 7", v)
	}
	for _, id := range []uint32{tdef.AttrMaxThermostatSuggestions, tdef.AttrCurrentThermostatSuggestion} {
		var v any
		if id == tdef.AttrMaxThermostatSuggestions {
			v = uint8(7)
		}
		if err := srv.SetAttributes(map[uint32]any{id: v}); !errors.Is(err, spec.ErrNotHeld) {
			t.Errorf("SetAttributes 0x%04X: %v, want ErrNotHeld", id, err)
		}
	}
	id, err := addSuggestion(srv, handleB, nil, 30)
	if err != nil {
		t.Fatal(err)
	}
	if got := srv.Suggestions(); len(got) != 1 || got[0].UniqueId != id {
		t.Errorf("Suggestions %v", got)
	}
	if cur, ok := srv.CurrentSuggestion(); !ok || cur.UniqueId != id {
		t.Errorf("CurrentSuggestion %+v, %v", cur, ok)
	}
	// A remove while the time is not synchronised removes, and changes
	// nothing else.
	clk.setSynced(false)
	if err := removeSuggestion(srv, id); err != nil {
		t.Fatal(err)
	}
	if _, ok := srv.CurrentSuggestion(); ok {
		t.Error("the removed suggestion is current")
	}

	never, err := thermo.New(tsuggestConfig(clk)) // no Matter time attached
	if err != nil {
		t.Fatal(err)
	}
	if _, err := addSuggestion(never, handleB, nil, 30); statusOfErr(t, err) != im.StatusInvalidInState {
		t.Errorf("no Matter time: %v, want INVALID_IN_STATE", err)
	}
	// The time arrives with AttachMatterTime, a node's as the server joins
	// its topology; nil leaves it as it is.
	never.AttachMatterTime(nil)
	if _, err := addSuggestion(never, handleB, nil, 30); statusOfErr(t, err) != im.StatusInvalidInState {
		t.Errorf("no Matter time after a nil attach: %v, want INVALID_IN_STATE", err)
	}
	never.AttachMatterTime(clk.matterTime)
	clk.setSynced(true)
	if _, err := addSuggestion(never, handleB, nil, 30); err != nil {
		t.Errorf("the node's attached time: %v, want success", err)
	}
	clk.setSynced(false)
	if _, err := never.MatterInvoke(context.Background(), tdef.CmdAddThermostatSuggestion, "junk"); statusOfErr(t, err) != im.StatusInvalidCommand {
		t.Errorf("a junk payload: %v, want INVALID_COMMAND", err)
	}
	if _, err := never.MatterInvoke(context.Background(), tdef.CmdRemoveThermostatSuggestion, (*tdef.RemoveThermostatSuggestionRequest)(nil)); statusOfErr(t, err) != im.StatusInvalidCommand {
		t.Errorf("a nil payload: %v, want INVALID_COMMAND", err)
	}

	plainClk := newSuggestClock()
	plain := newTSUGGEST(t, tsuggestConfig(plainClk, func(cfg *thermo.ThermostatConfig) { cfg.Features &^= thermo.ThermostatFeatureTSUGGEST }))
	if _, err := addSuggestion(plain, handleB, nil, 30); statusOfErr(t, err) != im.StatusUnsupportedCommand {
		t.Errorf("without TSUGGEST: %v, want UNSUPPORTED_COMMAND", err)
	}
	if _, ok := plain.MatterRead(tdef.AttrThermostatSuggestions); ok {
		t.Error("ThermostatSuggestions served without TSUGGEST")
	}
	plain.SetTemperatureSetpointHold(uint8(tdef.TemperatureSetpointHoldSetpointHoldOn)) // arms nothing
	if n := plainClk.Pending(); n != 0 {
		t.Errorf("%d timers armed by a thermostat without TSUGGEST", n)
	}
}

// TestSuggestionsFollowAMovedMatterTime: when the Matter time moves
// (MatterTimeChanged, a node's on every move of its UTC) the suggestions
// are re-evaluated against the new time — a jump past the current
// suggestion's ExpirationTime removes it and leaves no current one, ahead
// of connectedhomeip, whose expiration timer runs on the monotonic clock
// (thermostat-suggestions-delegate-impl.cpp:264-269 at 6170af84) and does
// not see a change of UTC; the server's own timer has not fired.
func TestSuggestionsFollowAMovedMatterTime(t *testing.T) {
	t.Parallel()
	clk := newSuggestClock()
	srv := newTSUGGEST(t, tsuggestConfig(clk))
	var jump uint32
	srv.AttachMatterTime(func() (uint32, bool) {
		v, ok := clk.matterTime()
		return v + jump, ok
	})
	if _, err := addSuggestion(srv, handleB, nil, 30); err != nil {
		t.Fatal(err)
	}
	if _, ok := srv.CurrentSuggestion(); !ok {
		t.Fatal("the suggestion is not current")
	}
	srv.MatterTimeChanged()
	clk.Advance(0)
	if _, ok := srv.CurrentSuggestion(); !ok {
		t.Error("an unmoved time dropped the current suggestion")
	}
	jump = 31 * 60
	srv.MatterTimeChanged()
	clk.Advance(0)
	if c, ok := srv.CurrentSuggestion(); ok {
		t.Errorf("current suggestion %+v after the time passed its expiry", c)
	}
	if got := srv.Suggestions(); len(got) != 0 {
		t.Errorf("suggestions %+v after the time passed their expiry, want none", got)
	}
}

// TestAMovedMatterTimeDuringAReevaluationIsKept: a change of the Matter
// time that lands while a re-evaluation runs — after it read the time —
// keeps its own scheduled re-evaluation; the running one does not replace
// that timer, so the moved time is evaluated and the expired suggestion
// leaves.
func TestAMovedMatterTimeDuringAReevaluationIsKept(t *testing.T) {
	t.Parallel()
	clk := newSuggestClock()
	srv := newTSUGGEST(t, tsuggestConfig(clk))
	var jump uint32
	trigger := false
	srv.AttachMatterTime(func() (uint32, bool) {
		v, ok := clk.matterTime()
		old := v + jump
		if trigger {
			// The time moves after this read: the change is told while the
			// re-evaluation that read the old time still runs.
			trigger = false
			jump = 31 * 60
			srv.MatterTimeChanged()
		}
		return old, ok
	})
	if _, err := addSuggestion(srv, handleB, nil, 30); err != nil {
		t.Fatal(err)
	}
	trigger = true
	srv.MatterTimeChanged()
	clk.Advance(0)
	clk.Advance(0)
	if c, ok := srv.CurrentSuggestion(); ok {
		t.Errorf("current suggestion %+v: the change told during a re-evaluation was lost", c)
	}
}

// TestSuggestionsFollowAMovedMatterTimeBothWays: a jump to a suggestion's
// EffectiveTime makes it current — its preset followed — before the
// server's timer fires; a jump back before it takes the current status
// away again, while the preset it activated stays active (chip never sees
// the case: its timer is monotonic,
// thermostat-suggestions-delegate-impl.cpp:264-269 at 6170af84).
func TestSuggestionsFollowAMovedMatterTimeBothWays(t *testing.T) {
	t.Parallel()
	clk := newSuggestClock()
	srv := newTSUGGEST(t, tsuggestConfig(clk))
	var jump uint32
	srv.AttachMatterTime(func() (uint32, bool) {
		v, ok := clk.matterTime()
		return v + jump, ok
	})
	effective := clk.epoch() + 2*60*60
	if _, err := addSuggestion(srv, handleB, &effective, 30); err != nil {
		t.Fatal(err)
	}
	if c, ok := srv.CurrentSuggestion(); ok {
		t.Fatalf("a suggestion effective in 2 h is current: %+v", c)
	}
	jump = 2*60*60 + 60
	srv.MatterTimeChanged()
	clk.Advance(0)
	if _, ok := srv.CurrentSuggestion(); !ok {
		t.Fatal("the jump to its EffectiveTime did not make the suggestion current")
	}
	if got := srv.ActivePresetHandle(); !bytes.Equal(got, handleB) {
		t.Errorf("ActivePresetHandle %q after the jump, want %q", got, handleB)
	}
	jump = 0
	srv.MatterTimeChanged()
	clk.Advance(0)
	if c, ok := srv.CurrentSuggestion(); ok {
		t.Errorf("current suggestion %+v after the jump back before its EffectiveTime", c)
	}
	if got := srv.Suggestions(); len(got) != 1 {
		t.Errorf("suggestions %+v after the jump back, want the one still pending", got)
	}
	if got := srv.ActivePresetHandle(); !bytes.Equal(got, handleB) {
		t.Errorf("ActivePresetHandle %q after the jump back, want the preset it activated, %q", got, handleB)
	}
}

// TestSuggestionsSetAttributes pins the application's write of
// ThermostatSuggestions (decision 317): held to the attribute's own
// constraints — at most MaxThermostatSuggestions entries, a PresetHandle
// of at most 16 bytes — and nothing else, an entry naming no preset
// included; an equal list is no change; a changed one is announced and
// re-evaluated.
func TestSuggestionsSetAttributes(t *testing.T) {
	t.Parallel()
	clk := newSuggestClock()
	srv := newTSUGGEST(t, tsuggestConfig(clk))
	now := clk.epoch()
	entry := func(id uint8, handle []byte) thermo.Suggestion {
		return thermo.Suggestion{UniqueId: id, PresetHandle: handle, EffectiveTime: now, ExpirationTime: now + 1800}
	}
	six := make(spec.List[thermo.Suggestion], 0, 6)
	for id := range uint8(6) {
		six = append(six, entry(id, handleB))
	}
	if err := srv.SetAttributes(map[uint32]any{tdef.AttrThermostatSuggestions: six}); !errors.Is(err, spec.ErrInvalidValue) {
		t.Errorf("six entries over MaxThermostatSuggestions 5: %v, want ErrInvalidValue", err)
	}
	long := spec.List[thermo.Suggestion]{entry(1, make([]byte, 17))}
	if err := srv.SetAttributes(map[uint32]any{tdef.AttrThermostatSuggestions: long}); !errors.Is(err, spec.ErrInvalidValue) {
		t.Errorf("a PresetHandle of 17 bytes: %v, want ErrInvalidValue", err)
	}
	if got := srv.Suggestions(); len(got) != 0 {
		t.Fatalf("a refused write stored %v", got)
	}

	changes := recordChanges(srv)
	unknown := spec.List[thermo.Suggestion]{entry(3, []byte{9})}
	if err := srv.SetAttributes(map[uint32]any{tdef.AttrThermostatSuggestions: unknown}); err != nil {
		t.Fatalf("an entry naming no preset: %v, want accepted", err)
	}
	clk.Advance(0)
	if got := srv.Suggestions(); len(got) != 1 || got[0].UniqueId != 3 {
		t.Errorf("Suggestions %v, want the entry of id 3", got)
	}
	if got := changes(); !slices.Contains(got, tdef.AttrThermostatSuggestions) {
		t.Errorf("changes %v, want ThermostatSuggestions", got)
	}
	if err := srv.SetAttributes(map[uint32]any{tdef.AttrThermostatSuggestions: unknown}); err != nil {
		t.Fatal(err)
	}
	clk.Advance(0)
	if got := changes(); slices.Contains(got, tdef.AttrThermostatSuggestions) {
		t.Errorf("an equal list announced %v", got)
	}
}

// memSuggestionStore is a SuggestionStore in memory that counts its saves.
type memSuggestionStore struct {
	list       []byte
	overridden *uint8
	ok         bool
	saves      int
}

func (m *memSuggestionStore) LoadSuggestions(context.Context) (list []byte, overridden *uint8, ok bool, err error) {
	return bytes.Clone(m.list), m.overridden, m.ok, nil
}

func (m *memSuggestionStore) SaveSuggestions(_ context.Context, list []byte, overridden *uint8) error {
	m.list, m.overridden, m.ok = bytes.Clone(list), overridden, true
	m.saves++
	return nil
}

// TestSuggestionsSurviveARestart pins decision 316: the commands' entries
// are saved, a server started on the same store serves them again and
// issues the next id after the largest one; a start without a stored list
// saves nothing; a re-evaluation that changes nothing saves nothing.
func TestSuggestionsSurviveARestart(t *testing.T) {
	t.Parallel()
	clk := newSuggestClock()
	st := &memSuggestionStore{}
	cfg := tsuggestConfig(clk, func(cfg *thermo.ThermostatConfig) { cfg.SuggestionStore = st })
	srv := newTSUGGEST(t, cfg)
	if st.saves != 0 {
		t.Fatalf("a start without stored suggestions saved %d times", st.saves)
	}
	first, err := addSuggestion(srv, handleB, nil, 30)
	if err != nil {
		t.Fatal(err)
	}
	later := clk.epoch() + 600
	second, err := addSuggestion(srv, handleB, &later, 30)
	if err != nil {
		t.Fatal(err)
	}
	saves := st.saves
	srv.MatterTimeChanged()
	clk.Advance(0)
	if st.saves != saves {
		t.Errorf("a re-evaluation that changed nothing saved")
	}
	srv.MatterQuiesce()

	again := newTSUGGEST(t, cfg)
	clk.Advance(0)
	got := again.Suggestions()
	if len(got) != 2 || got[0].UniqueId != first || got[1].UniqueId != second {
		t.Fatalf("Suggestions after the restart %v, want ids %d and %d", got, first, second)
	}
	if cur, ok := again.CurrentSuggestion(); !ok || cur.UniqueId != first {
		t.Errorf("CurrentSuggestion %+v, %v, want %d", cur, ok, first)
	}
	id, err := addSuggestion(again, handleB, nil, 30)
	if err != nil {
		t.Fatal(err)
	}
	if id != second+1 {
		t.Errorf("UniqueID after the restart %d, want %d", id, second+1)
	}
}

// TestSuggestionsSetAttributesAnnouncesCurrentOnce: a write that drops
// the current entry announces CurrentThermostatSuggestion once, from the
// re-evaluation, with its final value — no intermediate null (matter.js
// evaluates in the writing transaction).
func TestSuggestionsSetAttributesAnnouncesCurrentOnce(t *testing.T) {
	t.Parallel()
	clk := newSuggestClock()
	srv := newTSUGGEST(t, tsuggestConfig(clk))
	now := clk.epoch()
	first := thermo.Suggestion{UniqueId: 1, PresetHandle: handleB, EffectiveTime: now, ExpirationTime: now + 1800}
	next := thermo.Suggestion{UniqueId: 2, PresetHandle: handleB, EffectiveTime: now, ExpirationTime: now + 1800}
	if err := srv.SetAttributes(map[uint32]any{tdef.AttrThermostatSuggestions: spec.List[thermo.Suggestion]{first}}); err != nil {
		t.Fatal(err)
	}
	clk.Advance(0)
	changes := recordChanges(srv)
	if err := srv.SetAttributes(map[uint32]any{tdef.AttrThermostatSuggestions: spec.List[thermo.Suggestion]{next}}); err != nil {
		t.Fatal(err)
	}
	if got := changes(); slices.Contains(got, tdef.AttrCurrentThermostatSuggestion) {
		t.Errorf("the write announced CurrentThermostatSuggestion before the re-evaluation: %v", got)
	}
	clk.Advance(0)
	if got := changes(); !slices.Contains(got, tdef.AttrCurrentThermostatSuggestion) {
		t.Errorf("the re-evaluation did not announce CurrentThermostatSuggestion: %v", got)
	}
	if cur, ok := srv.CurrentSuggestion(); !ok || cur.UniqueId != 2 {
		t.Errorf("CurrentSuggestion %+v, %v, want 2", cur, ok)
	}
}
