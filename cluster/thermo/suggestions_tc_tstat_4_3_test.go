// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// ThermostatSuggestions, the connectedhomeip cases matter.js's
// ThermostatSuggestionsTest.ts (ported at db91886c in
// thermostat_suggestions_parity_matterjs_test.go) does not hold: chip's
// unit test TestSuggestionsAttributesAndAddRemoveCommands
// (src/app/clusters/thermostat-server/tests/TestThermostatSuggestionsAndHold.cpp:161-242
// at 6170af84) — a 17-byte handle, a lifetime above 1440 minutes and the
// change notifications of the commands — and the certification case
// src/python_testing/TC_TSTAT_4_3.py, which the chip-tool harness runs
// against the reference daemon; its steps here pin the server's answers
// at the API. Kept because neither appears in the matter.js port.
package thermo_test

import (
	"bytes"
	"context"
	"slices"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/cluster/spec"
	tdef "thingfabric.dev/go-fabric/cluster/spec/thermostat"
	"thingfabric.dev/go-fabric/cluster/thermo"
	"thingfabric.dev/go-fabric/im"
)

// handleA, handleB and handleC are the presets of tsuggestConfig.
var (
	handleA = []byte{1, 2, 3, 4}
	handleB = []byte("UserDef")
	handleC = []byte("TestOcc")
)

// tsuggestConfig is chip's fixture (:161-172) — HEAT, COOL, PRES and
// TSUGGEST with an Occupied preset of handle {1,2,3,4} — with the two
// presets of the harness's thermostat besides (TC_TSTAT_4_3 needs two),
// the hold served (its steps 7a-7c), and the Matter time of clk.
func tsuggestConfig(clk *suggestClock, edit ...func(*thermo.ThermostatConfig)) thermo.ThermostatConfig {
	cfg := thermo.DefaultThermostatConfig()
	cfg.Features = thermo.ThermostatFeatureHEAT | thermo.ThermostatFeatureCOOL | thermo.ThermostatFeaturePRES | thermo.ThermostatFeatureTSUGGEST
	cfg.NumberOfPresets = 5
	cfg.PresetTypes = []thermo.PresetType{
		{PresetScenario: tdef.PresetScenarioOccupied, NumberOfPresets: 1},
		{PresetScenario: tdef.PresetScenarioUserDefined, NumberOfPresets: 1},
		{PresetScenario: tdef.PresetScenarioUnoccupied, NumberOfPresets: 1},
	}
	heat, cool, warm := int16(2000), int16(2600), int16(2200)
	cfg.Presets = []thermo.Preset{
		{PresetHandle: spec.ValueOf(handleA), PresetScenario: tdef.PresetScenarioOccupied, HeatingSetpoint: &heat, CoolingSetpoint: &cool, BuiltIn: spec.ValueOf(false)},
		{PresetHandle: spec.ValueOf(handleB), PresetScenario: tdef.PresetScenarioUserDefined, HeatingSetpoint: &warm, CoolingSetpoint: &cool, BuiltIn: spec.ValueOf(false)},
		{PresetHandle: spec.ValueOf(handleC), PresetScenario: tdef.PresetScenarioUnoccupied, HeatingSetpoint: &heat, CoolingSetpoint: &cool, BuiltIn: spec.ValueOf(true)},
	}
	off := uint8(tdef.TemperatureSetpointHoldSetpointHoldOff)
	cfg.TemperatureSetpointHold = &off
	cfg.Clock = clk
	for _, e := range edit {
		e(&cfg)
	}
	return cfg
}

// TestChipSuggestionsAttributesAndAddRemoveCommands ports
// TestSuggestionsAttributesAndAddRemoveCommands (:161-242).
func TestChipSuggestionsAttributesAndAddRemoveCommands(t *testing.T) {
	t.Parallel()
	clk := newSuggestClock()
	clk.setSynced(false) // :177-178, the mock clock at 0: unsynced
	srv := newTSUGGEST(t, tsuggestConfig(clk))
	changes := recordChanges(srv)

	// :180-183
	if v, _ := srv.MatterRead(tdef.AttrMaxThermostatSuggestions); v != uint8(5) {
		t.Errorf("MaxThermostatSuggestions %v, want 5", v)
	}
	// 1. :185-192, no synced clock.
	if _, err := addSuggestion(srv, handleA, nil, 60); statusOfErr(t, err) != im.StatusInvalidInState {
		t.Errorf("unsynced: %v, want INVALID_IN_STATE", err)
	}
	// 2. :194-195
	clk.setSynced(true)
	// 3. :197-204, the lifetime outside 30 to 1440 minutes.
	for _, minutes := range []uint16{10, 2000} {
		if _, err := addSuggestion(srv, handleA, nil, minutes); statusOfErr(t, err) != im.StatusConstraintError {
			t.Errorf("%d minutes: %v, want CONSTRAINT_ERROR", minutes, err)
		}
	}
	// 4. :206-211
	if _, err := addSuggestion(srv, []byte{9, 9, 9, 9}, nil, 60); statusOfErr(t, err) != im.StatusNotFound {
		t.Errorf("unknown handle: %v, want NOT_FOUND", err)
	}
	// 5. :213-227
	id, err := addSuggestion(srv, handleA, nil, 60)
	if err != nil || id == 0 {
		t.Fatalf("add: id %d, %v", id, err)
	}
	if got := readSuggestions(t, srv); len(got) != 1 {
		t.Fatalf("ThermostatSuggestions %v, want one", got)
	}
	if got := changes(); !slices.Contains(got, tdef.AttrThermostatSuggestions) || !slices.Contains(got, tdef.AttrCurrentThermostatSuggestion) {
		t.Errorf("changes %v, want ThermostatSuggestions and CurrentThermostatSuggestion (the re-evaluation)", got)
	}
	// 6. :229-234
	if err := removeSuggestion(srv, id); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if got := readSuggestions(t, srv); len(got) != 0 {
		t.Errorf("ThermostatSuggestions %v after the remove, want none", got)
	}
	// 7. :236-239
	if err := removeSuggestion(srv, 0xFE); statusOfErr(t, err) != im.StatusNotFound {
		t.Errorf("remove 0xFE: %v, want NOT_FOUND", err)
	}
	// A handle longer than 16 bytes (ThermostatClusterSuggestions.cpp:163-166).
	if _, err := addSuggestion(srv, bytes.Repeat([]byte{1}, 17), nil, 60); statusOfErr(t, err) != im.StatusConstraintError {
		t.Errorf("17-byte handle: %v, want CONSTRAINT_ERROR", err)
	}
}

// TestChipSuggestionsTC_TSTAT_4_3 runs TC_TSTAT_4_3's steps against the
// server: step 3-4b are the time check (synchronised here), 5 the unknown
// handle, 6a-6b one suggestion followed and removed, 7a-7c the hold, 8a-8c
// the remove of an unknown id, 9a-9b two suggestions, 10 an EffectiveTime
// past 24 h, 11 the full list.
func TestChipSuggestionsTC_TSTAT_4_3(t *testing.T) { //nolint:funlen // one case, step by step
	t.Parallel()
	clk := newSuggestClock()
	srv := newTSUGGEST(t, tsuggestConfig(clk))
	ctx := context.Background()
	if v, _ := srv.MatterRead(0xFFFC); v.(uint32)&thermo.ThermostatFeatureTSUGGEST == 0 { // 2a
		t.Fatal("FeatureMap without ThermostatSuggestions")
	}

	// 5
	if _, err := addSuggestion(srv, []byte("0"), new(clk.epoch()), 30); statusOfErr(t, err) != im.StatusNotFound {
		t.Errorf("step 5: %v, want NOT_FOUND", err)
	}

	// 6a: a preset other than the active one, effective now, 30 minutes.
	now := clk.epoch()
	id, err := addSuggestion(srv, handleB, &now, 30)
	if err != nil {
		t.Fatalf("step 6a: %v", err)
	}
	cur, ok := readCurrent(t, srv)
	want := thermo.Suggestion{UniqueId: id, PresetHandle: handleB, EffectiveTime: now, ExpirationTime: now + 30*60}
	if !ok || !equalSuggestion(cur, want) {
		t.Errorf("step 6a: CurrentThermostatSuggestion %+v, %v; want %+v", cur, ok, want)
	}
	if r := readReason(t, srv); r != nil {
		t.Errorf("step 6a: ThermostatSuggestionNotFollowingReason %d, want null", *r)
	}
	if got := srv.ActivePresetHandle(); !bytes.Equal(got, handleB) {
		t.Errorf("step 6a: ActivePresetHandle %q, want %q", got, handleB)
	}
	// The thermostat took the preset's heating setpoint, as
	// SetActivePresetRequest does.
	if v, _ := srv.MatterRead(tdef.AttrOccupiedHeatingSetpoint); v != int16(2200) {
		t.Errorf("step 6a: OccupiedHeatingSetpoint %v, want the preset's 2200", v)
	}
	// 6b
	if err := removeSuggestion(srv, id); err != nil {
		t.Fatalf("step 6b: %v", err)
	}
	if got := readSuggestions(t, srv); len(got) != 0 {
		t.Errorf("step 6b: %v, want no suggestion", got)
	}
	if _, ok := readCurrent(t, srv); ok {
		t.Error("step 6b: CurrentThermostatSuggestion not null")
	}

	// 7a: hold on, a suggestion of another preset.
	if err := srv.MatterWrite(ctx, tdef.AttrTemperatureSetpointHold, uint8(tdef.TemperatureSetpointHoldSetpointHoldOn)); err != nil {
		t.Fatal(err)
	}
	if err := srv.MatterWrite(ctx, tdef.AttrTemperatureSetpointHoldDuration, nil); err != nil {
		t.Fatal(err)
	}
	now = clk.epoch()
	id, err = addSuggestion(srv, handleA, &now, 30)
	if err != nil {
		t.Fatalf("step 7a: %v", err)
	}
	if cur, ok := readCurrent(t, srv); !ok || cur.UniqueId != id {
		t.Errorf("step 7a: CurrentThermostatSuggestion %+v, %v; want id %d", cur, ok, id)
	}
	// The commented asserts of step 7a (TC_TSTAT_4_3.py:368-375): OngoingHold,
	// and the active preset not the suggestion's.
	if r := readReason(t, srv); r == nil || *r != uint16(tdef.ThermostatSuggestionNotFollowingReasonOngoingHold) {
		t.Errorf("step 7a: ThermostatSuggestionNotFollowingReason %v, want OngoingHold", r)
	}
	if got := srv.ActivePresetHandle(); !bytes.Equal(got, handleB) {
		t.Errorf("step 7a: ActivePresetHandle %q, want %q kept", got, handleB)
	}
	// 7b: hold off; the thermostat follows.
	clk.Advance(2 * time.Second)
	if err := srv.MatterWrite(ctx, tdef.AttrTemperatureSetpointHold, uint8(tdef.TemperatureSetpointHoldSetpointHoldOff)); err != nil {
		t.Fatal(err)
	}
	if r := readReason(t, srv); r != nil {
		t.Errorf("step 7b: ThermostatSuggestionNotFollowingReason %d, want null", *r)
	}
	if got := srv.ActivePresetHandle(); !bytes.Equal(got, handleA) {
		t.Errorf("step 7b: ActivePresetHandle %q, want %q", got, handleA)
	}
	// 7c
	if err := removeSuggestion(srv, id); err != nil {
		t.Fatalf("step 7c: %v", err)
	}

	// 8a-8c
	now = clk.epoch()
	id, err = addSuggestion(srv, handleB, &now, 30)
	if err != nil {
		t.Fatalf("step 8a: %v", err)
	}
	if err := removeSuggestion(srv, id+1); statusOfErr(t, err) != im.StatusNotFound {
		t.Errorf("step 8b: %v, want NOT_FOUND", err)
	}
	if err := removeSuggestion(srv, id); err != nil {
		t.Fatalf("step 8c: %v", err)
	}

	// 9a: two suggestions, 35 and 30 minutes; the current one is the first
	// (the earliest EffectiveTime, the first added on a tie).
	active := srv.ActivePresetHandle()
	now = clk.epoch()
	first, err := addSuggestion(srv, handleA, &now, 35)
	if err != nil {
		t.Fatalf("step 9a: %v", err)
	}
	second, err := addSuggestion(srv, active, &now, 30)
	if err != nil {
		t.Fatalf("step 9a: %v", err)
	}
	if first == second || len(readSuggestions(t, srv)) != 2 {
		t.Errorf("step 9a: ids %d, %d; suggestions %v", first, second, readSuggestions(t, srv))
	}
	if cur, ok := readCurrent(t, srv); !ok || cur.UniqueId != first {
		t.Errorf("step 9a: CurrentThermostatSuggestion %+v, want id %d", cur, first)
	}
	if got := srv.ActivePresetHandle(); !bytes.Equal(got, handleA) {
		t.Errorf("step 9a: ActivePresetHandle %q, want %q", got, handleA)
	}
	// 9b
	if err := removeSuggestion(srv, first); err != nil {
		t.Fatal(err)
	}
	if got := readSuggestions(t, srv); len(got) != 1 {
		t.Errorf("step 9b: %v, want one", got)
	}
	if err := removeSuggestion(srv, second); err != nil {
		t.Fatal(err)
	}

	// 10: 25 hours ahead.
	later := clk.epoch() + 25*60*60
	if _, err := addSuggestion(srv, handleB, &later, 30); statusOfErr(t, err) != im.StatusInvalidCommand {
		t.Errorf("step 10: %v, want INVALID_COMMAND", err)
	}

	// 11: MaxThermostatSuggestions, then one more.
	for i := range 5 {
		if _, err := addSuggestion(srv, handleB, new(clk.epoch()), 30); err != nil {
			t.Fatalf("step 11, suggestion %d: %v", i+1, err)
		}
	}
	if _, err := addSuggestion(srv, handleB, new(clk.epoch()), 30); statusOfErr(t, err) != im.StatusResourceExhausted {
		t.Errorf("step 11: %v, want RESOURCE_EXHAUSTED", err)
	}
}
