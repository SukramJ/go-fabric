// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behaviors/thermostat/PresetsAtomicWriteTest.ts
// at 862ad75f: presets written through BeginWrite, the write and
// CommitWrite or RollbackWrite, and the writes the device declines.
//
// matter.js commissions a MockSite pair and drives a subscribedPeer
// (`ep1.commandsOf(ThermostatClient).atomicRequest`, `state.presets = …`);
// the port commissions a fabrictest pair and invokes and writes over the
// wire. The controller's cache of Presets is the pair's subscription to
// it (the reports it received); the device's announcements
// (recordThermostatChanges) are the server's OnAttributeChange;
// `deviceEp.state.thermostat.persistedPresets` is the server's Presets().
// A write matter.js rejects ("Not found", "Constraint error", "Multiple
// writes failed") is a WriteStatuses entry with that status.
//
// Not ported: "refuses a commit whose settled presets an observer stripped
// the handle from" — an application observer on persistedPresets$Changing
// that rewrites the value in flight; the Go server has no such observer
// seam (the host's refusal is its PresetStore's error).
package thermo_test

import (
	"testing"

	tdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	"github.com/SukramJ/go-fabric/cluster/thermo"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
)

// presetsThermostat is PresetsAtomicWriteTest.ts:15-27 commissionedThermostat
// on presetsEndpoint() (thermostatConfig with five presets, none stored).
func presetsThermostat(t *testing.T) *thermostatPair {
	t.Helper()
	return commissionedThermostat(t, func(*fabrictest.Site) thermo.ThermostatConfig { return thermostatConfig(5, []thermo.Preset{}) })
}

// statusesAre checks a write's statuses, one per chunk.
func statusesAre(t *testing.T, got []im.StatusCode, want ...im.StatusCode) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("write statuses %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("write statuses %v, want %v", got, want)
			return
		}
	}
}

func TestParityMatterJS_PresetsAtomicWriteTest(t *testing.T) {
	presets := []uint32{presetsAttribute}
	presetsOK := []tdef.AtomicResponseAttributeStatusEntry{{AttributeId: presetsAttribute, StatusCode: tdef.StatusSuccess}}

	begin := func(t *testing.T, p *thermostatPair) {
		t.Helper()
		r, err := p.atomicRequest(parityCtx(t), p.pair, beginWrite, presets, timeoutMs(5000))
		if err != nil {
			t.Fatal(err)
		}
		wantAtomic(t, decodeAtomicResponse(t, r), tdef.StatusSuccess, presetsOK, timeoutMs(5000))
	}
	write := func(t *testing.T, p *thermostatPair, ps []thermo.Preset) []im.StatusCode {
		t.Helper()
		statuses, err := p.writePresets(parityCtx(t), p.pair, ps)
		if err != nil {
			t.Fatal(err)
		}
		out := make([]im.StatusCode, len(statuses))
		for i, s := range statuses {
			out[i] = s.Status
		}
		return out
	}

	t.Run("encodes a whole-list assignment as replace-all followed by ADD", func(t *testing.T) {
		// matter.js checks the Write request it formulates; the port writes
		// the same assignment and reads the paths of the device's answer,
		// one per AttributeDataIB: the REPLACE-ALL without a list index, then
		// the append with a null one (ListAppend). Outside an atomic write
		// the device refuses both, which leaves the paths to compare.
		p := presetsThermostat(t)
		statuses, err := p.writePresets(parityCtx(t), p.pair, []thermo.Preset{newPreset()})
		if err != nil {
			t.Fatal(err)
		}
		if len(statuses) != 2 || statuses[0].Path.HasListIndex || statuses[0].Path.ListAppend || !statuses[1].Path.ListAppend {
			t.Errorf("write paths %+v, want REPLACE-ALL then one append", statuses)
		}
	})

	t.Run("applies presets through BeginWrite, write and CommitWrite", func(t *testing.T) {
		p := presetsThermostat(t)
		ctx := parityCtx(t)
		deviceAnnouncements := recordThermostatChanges(p.srv)
		clientReports := p.subscribePresets(ctx, t)

		begin(t, p)
		statusesAre(t, write(t, p, []thermo.Preset{newPreset()}), im.StatusSuccess, im.StatusSuccess)

		// The write is staged only; the device's stored value stays empty
		// until commit.
		if got := p.srv.Presets(); len(got) != 0 {
			t.Fatalf("persisted presets before commit %+v, want none", got)
		}

		r, err := p.atomicRequest(ctx, p.pair, commitWrite, presets, nil)
		if err != nil {
			t.Fatal(err)
		}
		wantAtomic(t, decodeAtomicResponse(t, r), tdef.StatusSuccess, presetsOK, nil)

		persisted := p.srv.Presets()
		if len(persisted) != 1 {
			t.Fatalf("persisted presets %+v, want one", persisted)
		}
		pr := persisted[0]
		if pr.PresetHandle.Null || len(pr.PresetHandle.Value) != 16 || pr.PresetScenario != tdef.PresetScenarioOccupied ||
			pr.CoolingSetpoint == nil || *pr.CoolingSetpoint != 2600 || pr.HeatingSetpoint == nil || *pr.HeatingSetpoint != 2000 ||
			pr.BuiltIn.Null || pr.BuiltIn.Value {
			t.Errorf("persisted preset %+v", pr)
		}
		if n := deviceAnnouncements.count(presetsAttribute); n != 1 {
			t.Errorf("Presets announced %d times, want 1", n)
		}

		// The controller learns the handle the device generated, so it can
		// address the preset in a later write.
		if err := p.site.Eventually(ctx, "a Presets report with the issued handle", func() bool {
			last, ok := clientReports.last()
			return ok && len(last) == 1 && !last[0].PresetHandle.Null && len(last[0].PresetHandle.Value) == 16
		}); err != nil {
			t.Fatal(err)
		}
		cached := p.readPresets(ctx, t, p.pair)
		if len(cached) != 1 || cached[0].PresetHandle.Null || len(cached[0].PresetHandle.Value) != 16 {
			t.Errorf("read presets %+v, want the one with its 16-byte handle", cached)
		}
	})

	t.Run("declines a write outside an atomic write", func(t *testing.T) {
		p := presetsThermostat(t)
		statusesAre(t, write(t, p, []thermo.Preset{newPreset()}), im.StatusInvalidInState, im.StatusInvalidInState)
		if got := p.srv.Presets(); len(got) != 0 {
			t.Errorf("persisted presets %+v, want none", got)
		}
		if got := p.readPresets(parityCtx(t), t, p.pair); len(got) != 0 {
			t.Errorf("read presets %+v, want none", got)
		}
	})

	t.Run("declines a preset carrying a handle the device does not know", func(t *testing.T) {
		p := presetsThermostat(t)
		begin(t, p)
		statusesAre(t, write(t, p, []thermo.Preset{newPreset(withHandle([]byte{1, 2, 3, 4}))}), im.StatusSuccess, im.StatusNotFound)
		if got := p.srv.Presets(); len(got) != 0 {
			t.Errorf("persisted presets %+v, want none", got)
		}
		// The holding peer reads what it staged: the REPLACE-ALL's empty
		// list, the append refused.
		if got := p.readPresets(parityCtx(t), t, p.pair); len(got) != 0 {
			t.Errorf("read presets %+v, want none", got)
		}
	})

	t.Run("declines two presets that share a scenario and carry no name", func(t *testing.T) {
		p := presetsThermostat(t)
		begin(t, p)
		// The specification counts "no name" as a value that may not repeat
		// within a scenario.
		statusesAre(t, write(t, p, []thermo.Preset{newPreset(), newPreset()}), im.StatusSuccess, im.StatusSuccess, im.StatusConstraintError)
		if got := p.srv.Presets(); len(got) != 0 {
			t.Errorf("persisted presets %+v, want none", got)
		}
	})

	t.Run("declines a named preset when the preset type does not support names", func(t *testing.T) {
		p := presetsThermostat(t)
		begin(t, p)
		statusesAre(t, write(t, p, []thermo.Preset{newPreset(withName("Home"))}), im.StatusSuccess, im.StatusConstraintError)
	})

	t.Run("discards a staged write on RollbackWrite", func(t *testing.T) {
		p := presetsThermostat(t)
		begin(t, p)
		statusesAre(t, write(t, p, []thermo.Preset{newPreset()}), im.StatusSuccess, im.StatusSuccess)
		r, err := p.atomicRequest(parityCtx(t), p.pair, rollbackWrite, presets, nil)
		if err != nil {
			t.Fatal(err)
		}
		wantAtomic(t, decodeAtomicResponse(t, r), tdef.StatusSuccess, presetsOK, nil)
		if got := p.srv.Presets(); len(got) != 0 {
			t.Errorf("persisted presets %+v, want none", got)
		}
	})
}
