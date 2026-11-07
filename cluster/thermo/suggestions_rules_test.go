// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// ThermostatSuggestions beyond the chip ports (suggestions_chip_test.go):
// the expiry and effective-time timer on the server's clock, the expired
// suggestions an Add removes, the unique ids, the device's own reasons, the
// hold the host reports, a Sink that refuses the preset, and the
// configuration. Every case runs on the fake clock.
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

// TestSuggestionUniqueIDs pins the reference delegate's GetUniqueID
// (thermostat-suggestions-delegate-impl.cpp:169-197 at 6170af84): one
// above the largest id held, and past 255 the lowest free id from 0.
func TestSuggestionUniqueIDs(t *testing.T) {
	t.Parallel()
	clk := newSuggestClock()
	srv := newTSUGGEST(t, tsuggestConfig(clk))
	prev, err := addSuggestion(srv, handleB, nil, 30)
	if err != nil || prev != 1 {
		t.Fatalf("first id %d, %v; want 1", prev, err)
	}
	for want := 2; want <= 255; want++ {
		id, err := addSuggestion(srv, handleB, nil, 30)
		if err != nil || int(id) != want {
			t.Fatalf("id %d, %v; want %d", id, err, want)
		}
		if err := removeSuggestion(srv, prev); err != nil {
			t.Fatal(err)
		}
		prev = id
	}
	if id, err := addSuggestion(srv, handleB, nil, 30); err != nil || id != 0 {
		t.Errorf("after 255: id %d, %v; want 0", id, err)
	}
	if id, err := addSuggestion(srv, handleB, nil, 30); err != nil || id != 1 {
		t.Errorf("after 255 and 0: id %d, %v; want 1", id, err)
	}
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
// INVALID_IN_STATE, the suggestions' state not the host's to set, the
// commands unsupported without the feature, and the accessors.
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
	for _, id := range []uint32{tdef.AttrMaxThermostatSuggestions, tdef.AttrThermostatSuggestions, tdef.AttrCurrentThermostatSuggestion} {
		var v any
		switch id {
		case tdef.AttrMaxThermostatSuggestions:
			v = uint8(7)
		case tdef.AttrThermostatSuggestions:
			v = spec.List[thermo.Suggestion]{}
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
