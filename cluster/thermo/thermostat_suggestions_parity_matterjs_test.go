// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behaviors/thermostat/ThermostatSuggestionsTest.ts
// at b25ed960 (#4993): the commands, the unique ids, the
// selection, the timer, time synchronisation, following, the manual
// override, the setpoint hold, the suggestions of a removed preset,
// MaxThermostatSuggestions and the preset activation.
//
// matter.js runs a MockServerNode with SuggestionsServer
// (ThermostatSuggestionsTest.ts:40-117); the port runs the server on the
// fake clock (the Matter time attached as a node attaches its own), its
// commands through MatterInvoke — a client's, matter.js's `command: true`
// context — and the application's `set` through SetAttributes, which
// schedules its re-evaluation on the clock (settle). The two controller
// describes run over the wire on a commissioned fabrictest pair.
// overriddenSuggestionId is not served, so a case reads the override
// through ThermostatSuggestionNotFollowingReason: the empty bitmap while
// it is in effect, null once the thermostat follows again.
//
// The register entry (script/testparity/register.json) lists the cases
// left out with their reasons: matter.js's transactions and rollbacks,
// its retry back-off, the protected seams an application overrides
// (selectThermostatSuggestion, handleSetActivePresetRequest,
// hasSynchronizedTime) and the cases that need stored suggestions.
package thermo_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/cluster/spec"
	tdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	"github.com/SukramJ/go-fabric/cluster/thermo"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/internal/fabrictestseam"
	"github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/store/memstore"
	"github.com/SukramJ/go-fabric/tlv"
)

// suggestClock is a fake clock and the Matter time read from it, which a
// test may hold unsynchronised — matter.js's MockTime with ClockServer's
// hasSynchronizedTime (ThermostatSuggestionsTest.ts:139-146).
type suggestClock struct {
	*clocktest.Fake
	mu       sync.Mutex
	unsynced bool
}

func newSuggestClock() *suggestClock {
	return &suggestClock{Fake: clocktest.New(time.Unix(1700000000, 0))}
}

func (c *suggestClock) matterTime() (uint32, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.unsynced {
		return 0, false
	}
	return clock.MatterTimeOf(c.Fake)()
}

func (c *suggestClock) setSynced(synced bool) {
	c.mu.Lock()
	c.unsynced = !synced
	c.mu.Unlock()
}

// epoch is nowS() (ThermostatSuggestionsTest.ts:135-137).
func (c *suggestClock) epoch() uint32 { v, _ := c.matterTime(); return v }

// newTSUGGEST builds a server of cfg and attaches the Matter time of its
// suggestClock, as a node attaches its own.
func newTSUGGEST(t *testing.T, cfg thermo.ThermostatConfig) *thermo.ThermostatServer {
	t.Helper()
	srv, err := thermo.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if c, ok := cfg.Clock.(*suggestClock); ok {
		srv.AttachMatterTime(c.matterTime)
	}
	return srv
}

// addSuggestion invokes AddThermostatSuggestion; effective nil is null.
func addSuggestion(srv *thermo.ThermostatServer, handle []byte, effective *uint32, minutes uint16) (uint8, error) {
	req := tdef.AddThermostatSuggestionRequest{PresetHandle: handle, ExpirationInMinutes: minutes, EffectiveTime: spec.Nullable[uint32]{Null: true}}
	if effective != nil {
		req.EffectiveTime = spec.ValueOf(*effective)
	}
	resp, err := srv.MatterInvoke(context.Background(), tdef.CmdAddThermostatSuggestion, req)
	if err != nil {
		return 0, err
	}
	return resp.(tdef.AddThermostatSuggestionResponse).UniqueId, nil
}

func removeSuggestion(srv *thermo.ThermostatServer, id uint8) error {
	_, err := srv.MatterInvoke(context.Background(), tdef.CmdRemoveThermostatSuggestion, tdef.RemoveThermostatSuggestionRequest{UniqueId: id})
	return err
}

// readSuggestions reads ThermostatSuggestions.
func readSuggestions(t *testing.T, srv *thermo.ThermostatServer) []thermo.Suggestion {
	t.Helper()
	v, ok := srv.MatterRead(tdef.AttrThermostatSuggestions)
	if !ok {
		t.Fatal("ThermostatSuggestions not served")
	}
	return v.(spec.List[thermo.Suggestion])
}

// readCurrent reads CurrentThermostatSuggestion; false for null.
func readCurrent(t *testing.T, srv *thermo.ThermostatServer) (thermo.Suggestion, bool) {
	t.Helper()
	v, ok := srv.MatterRead(tdef.AttrCurrentThermostatSuggestion)
	if !ok {
		t.Fatal("CurrentThermostatSuggestion not served")
	}
	if v == nil {
		return thermo.Suggestion{}, false
	}
	return v.(thermo.Suggestion), true
}

// readReason reads ThermostatSuggestionNotFollowingReason; nil for null.
func readReason(t *testing.T, srv *thermo.ThermostatServer) *uint16 {
	t.Helper()
	v, ok := srv.MatterRead(tdef.AttrThermostatSuggestionNotFollowingReason)
	if !ok {
		t.Fatal("ThermostatSuggestionNotFollowingReason not served")
	}
	if v == nil {
		return nil
	}
	n := v.(uint16)
	return &n
}

// recordChanges collects the attribute ids srv reports.
func recordChanges(srv *thermo.ThermostatServer) func() []uint32 {
	var mu sync.Mutex
	var ids []uint32
	srv.OnAttributeChange(func(c contract.Change) {
		mu.Lock()
		ids = append(ids, c.Attributes...)
		mu.Unlock()
	})
	return func() []uint32 {
		mu.Lock()
		defer mu.Unlock()
		out := slices.Clone(ids)
		ids = nil
		return out
	}
}

func equalSuggestion(a, b thermo.Suggestion) bool {
	return a.UniqueId == b.UniqueId && bytes.Equal(a.PresetHandle, b.PresetHandle) &&
		a.EffectiveTime == b.EffectiveTime && a.ExpirationTime == b.ExpirationTime
}

// presetA, presetB and unknownPreset are ThermostatSuggestionsTest.ts:44-46.
var (
	presetA       = filled(16, 1)
	presetB       = filled(16, 2)
	unknownPreset = filled(16, 9)
)

// The ThermostatSuggestionNotFollowingReason bits the cases name.
const (
	ongoingHold         = uint16(tdef.ThermostatSuggestionNotFollowingReasonOngoingHold)
	demandResponseEvent = uint16(tdef.ThermostatSuggestionNotFollowingReasonDemandResponseEvent)
)

// suggestionPresets is ThermostatSuggestionsTest.ts:48-59: preset A
// matches the configured setpoints; activating preset B moves them.
func suggestionPresets() []thermo.Preset {
	return []thermo.Preset{
		newPreset(withHandle(presetA)),
		newPreset(withHandle(presetB), func(p *thermo.Preset) {
			heat, cool := int16(2100), int16(2500)
			p.PresetScenario = tdef.PresetScenarioUnoccupied
			p.HeatingSetpoint, p.CoolingSetpoint = &heat, &cool
		}),
	}
}

// suggestionsConfig is ThermostatSuggestionsTest.ts:61-78 on
// SuggestionsServer (:40, Heating, Cooling, AutoMode, Presets,
// ThermostatSuggestions): thermostatConfig's CoolingAndHeating, Auto and
// setpoints, the hold served and off, five presets of the Occupied and
// the Unoccupied type, no active preset, presets A and B.
func suggestionsConfig(clk *suggestClock, edit ...func(*thermo.ThermostatConfig)) thermo.ThermostatConfig {
	cfg := thermostatConfig(5, suggestionPresets())
	cfg.Features |= thermo.ThermostatFeatureTSUGGEST
	cfg.PresetTypes = []thermo.PresetType{
		{PresetScenario: tdef.PresetScenarioOccupied, NumberOfPresets: 5},
		{PresetScenario: tdef.PresetScenarioUnoccupied, NumberOfPresets: 5},
	}
	off := uint8(tdef.TemperatureSetpointHoldSetpointHoldOff)
	cfg.TemperatureSetpointHold = &off
	cfg.Clock = clk
	for _, e := range edit {
		e(&cfg)
	}
	return cfg
}

// suggestCase is the online() context of ThermostatSuggestionsTest.ts:80-117.
type suggestCase struct {
	t   *testing.T
	clk *suggestClock
	srv *thermo.ThermostatServer
}

func onlineSuggestions(t *testing.T, edit ...func(*thermo.ThermostatConfig)) *suggestCase {
	t.Helper()
	clk := newSuggestClock()
	return &suggestCase{t: t, clk: clk, srv: newTSUGGEST(t, suggestionsConfig(clk, edit...))}
}

// add is ctx.add: effective nil is null, minutes 0 is 30.
func (c *suggestCase) add(handle []byte, effective *uint32, minutes uint16) (uint8, error) {
	if minutes == 0 {
		minutes = 30
	}
	return addSuggestion(c.srv, handle, effective, minutes)
}

// mustAdd is an add the case expects to succeed.
func (c *suggestCase) mustAdd(handle []byte, effective *uint32, minutes uint16) uint8 {
	c.t.Helper()
	id, err := c.add(handle, effective, minutes)
	if err != nil {
		c.t.Fatalf("AddThermostatSuggestion: %v", err)
	}
	return id
}

func (c *suggestCase) mustRemove(id uint8) {
	c.t.Helper()
	if err := removeSuggestion(c.srv, id); err != nil {
		c.t.Fatalf("RemoveThermostatSuggestion %d: %v", id, err)
	}
}

// at is nowS() + d seconds.
func (c *suggestCase) at(d int64) *uint32 {
	v := uint32(int64(c.clk.epoch()) + d) //nolint:gosec // a Matter epoch second
	return &v
}

// activate is ctx.activate: a client's SetActivePresetRequest.
func (c *suggestCase) activate(handle []byte) {
	c.t.Helper()
	if err := activate(c.t, c.srv, handle); err != nil {
		c.t.Fatalf("SetActivePresetRequest: %v", err)
	}
}

// set is ctx.set: the application's change, its re-evaluation settled.
func (c *suggestCase) set(values map[uint32]any) {
	c.t.Helper()
	if err := c.srv.SetAttributes(values); err != nil {
		c.t.Fatalf("SetAttributes %v: %v", values, err)
	}
	c.settle()
}

// hold sets TemperatureSetpointHold as the application.
func (c *suggestCase) hold(on bool) {
	c.t.Helper()
	v := uint8(tdef.TemperatureSetpointHoldSetpointHoldOff)
	if on {
		v = uint8(tdef.TemperatureSetpointHoldSetpointHoldOn)
	}
	c.set(map[uint32]any{tdef.AttrTemperatureSetpointHold: v})
}

// reevaluate is ctx.reevaluate (reevaluateThermostatSuggestions): the Go
// server's MatterTimeChanged, settled.
func (c *suggestCase) reevaluate() {
	c.srv.MatterTimeChanged()
	c.settle()
}

// settle runs what is due on the clock now, as MockTime.resolve does.
func (c *suggestCase) settle() { c.clk.Advance(0) }

func (c *suggestCase) advance(d time.Duration) {
	c.clk.Advance(d)
	c.settle()
}

// The state reads of the cases.

func (c *suggestCase) current() (uint8, bool) {
	c.t.Helper()
	sg, ok := readCurrent(c.t, c.srv)
	return sg.UniqueId, ok
}

func (c *suggestCase) wantCurrent(id uint8) {
	c.t.Helper()
	if got, ok := c.current(); !ok || got != id {
		c.t.Errorf("CurrentThermostatSuggestion %d (%v), want %d", got, ok, id)
	}
}

func (c *suggestCase) wantNoCurrent() {
	c.t.Helper()
	if got, ok := c.current(); ok {
		c.t.Errorf("CurrentThermostatSuggestion %d, want null", got)
	}
}

// wantReason checks ThermostatSuggestionNotFollowingReason: nil is null,
// a value the bits (0 is the empty bitmap, matter.js's []).
func (c *suggestCase) wantReason(want *uint16) {
	c.t.Helper()
	got := readReason(c.t, c.srv)
	switch {
	case want == nil && got != nil:
		c.t.Errorf("ThermostatSuggestionNotFollowingReason %d, want null", *got)
	case want != nil && got == nil:
		c.t.Errorf("ThermostatSuggestionNotFollowingReason null, want %d", *want)
	case want != nil && *got != *want:
		c.t.Errorf("ThermostatSuggestionNotFollowingReason %d, want %d", *got, *want)
	}
}

func (c *suggestCase) wantActive(handle []byte) {
	c.t.Helper()
	got := c.srv.ActivePresetHandle()
	if (got == nil) != (handle == nil) || !bytes.Equal(got, handle) {
		c.t.Errorf("ActivePresetHandle %x, want %x", got, handle)
	}
}

func (c *suggestCase) wantHeating(want int16) {
	c.t.Helper()
	if got := readInt16(c.t, c.srv, tdef.AttrOccupiedHeatingSetpoint); got != want {
		c.t.Errorf("OccupiedHeatingSetpoint %d, want %d", got, want)
	}
}

func (c *suggestCase) wantIDs(want ...uint8) {
	c.t.Helper()
	list := readSuggestions(c.t, c.srv)
	got := make([]uint8, 0, len(list))
	for _, sg := range list {
		got = append(got, sg.UniqueId)
	}
	if !slices.Equal(got, want) {
		c.t.Errorf("ThermostatSuggestions ids %v, want %v", got, want)
	}
}

func (c *suggestCase) wantStatus(err error, status im.StatusCode) {
	c.t.Helper()
	var sc im.StatusCodeError
	if !errors.As(err, &sc) || sc.MatterStatusCode() != status {
		c.t.Errorf("error %v, want status 0x%02X", err, status)
	}
}

// reasonOf is a reason value for wantReason.
func reasonOf(bits uint16) *uint16 { return &bits }

func TestParityMatterJS_ThermostatSuggestionsTest(t *testing.T) { //nolint:funlen,gocognit,maintidx // one port, describe by describe
	t.Run("AddThermostatSuggestion", func(t *testing.T) {
		t.Run("adds the entry, makes it current and follows it", func(t *testing.T) {
			c := onlineSuggestions(t)
			effective := c.clk.epoch()
			id := c.mustAdd(presetB, &effective, 30)
			want := thermo.Suggestion{UniqueId: id, PresetHandle: presetB, EffectiveTime: effective, ExpirationTime: effective + 30*60}
			if got := readSuggestions(t, c.srv); len(got) != 1 || !equalSuggestion(got[0], want) {
				t.Errorf("ThermostatSuggestions %+v, want [%+v]", got, want)
			}
			if got, ok := readCurrent(t, c.srv); !ok || !equalSuggestion(got, want) {
				t.Errorf("CurrentThermostatSuggestion %+v (%v), want %+v", got, ok, want)
			}
			c.wantReason(nil)
			c.wantActive(presetB)
			c.wantHeating(2100)
			if got := readInt16(t, c.srv, tdef.AttrOccupiedCoolingSetpoint); got != 2500 {
				t.Errorf("OccupiedCoolingSetpoint %d, want 2500", got)
			}
		})

		t.Run("uses the current time when EffectiveTime is null", func(t *testing.T) {
			c := onlineSuggestions(t)
			now := c.clk.epoch()
			c.mustAdd(presetA, nil, 45)
			got := readSuggestions(t, c.srv)
			if len(got) != 1 || got[0].EffectiveTime != now || got[0].ExpirationTime != now+45*60 {
				t.Errorf("ThermostatSuggestions %+v, want effective %d, expiring %d", got, now, now+45*60)
			}
		})

		t.Run("refuses without synchronized time with INVALID_IN_STATE", func(t *testing.T) {
			c := onlineSuggestions(t)
			c.clk.setSynced(false)
			_, err := c.add(presetA, nil, 0)
			c.wantStatus(err, im.StatusInvalidInState)
			c.wantIDs()
		})

		t.Run("checks synchronized time before the preset", func(t *testing.T) {
			c := onlineSuggestions(t)
			c.clk.setSynced(false)
			_, err := c.add(unknownPreset, nil, 0)
			c.wantStatus(err, im.StatusInvalidInState)
		})

		t.Run("refuses an unknown preset with NOT_FOUND", func(t *testing.T) {
			c := onlineSuggestions(t)
			_, err := c.add(unknownPreset, nil, 0)
			c.wantStatus(err, im.StatusNotFound)
			c.wantIDs()
		})

		t.Run("refuses when the list is full with RESOURCE_EXHAUSTED, after the preset check", func(t *testing.T) {
			c := onlineSuggestions(t)
			for range 5 {
				c.mustAdd(presetA, nil, 0)
			}
			_, err := c.add(unknownPreset, nil, 0)
			c.wantStatus(err, im.StatusNotFound)
			_, err = c.add(presetA, nil, 0)
			c.wantStatus(err, im.StatusResourceExhausted)
			if n := len(readSuggestions(t, c.srv)); n != 5 {
				t.Errorf("%d entries, want 5", n)
			}
		})

		t.Run("checks the list size before the EffectiveTime", func(t *testing.T) {
			c := onlineSuggestions(t)
			for range 5 {
				c.mustAdd(presetA, nil, 0)
			}
			_, err := c.add(presetA, c.at(25*3600), 0)
			c.wantStatus(err, im.StatusResourceExhausted)
		})

		t.Run("refuses an EffectiveTime more than 24 hours ahead with INVALID_COMMAND", func(t *testing.T) {
			c := onlineSuggestions(t)
			_, err := c.add(presetA, c.at(24*3600+1), 0)
			c.wantStatus(err, im.StatusInvalidCommand)
			c.wantIDs()
		})

		t.Run("accepts an EffectiveTime exactly 24 hours ahead, which is not current yet", func(t *testing.T) {
			c := onlineSuggestions(t)
			c.mustAdd(presetB, c.at(24*3600), 0)
			if n := len(readSuggestions(t, c.srv)); n != 1 {
				t.Errorf("%d entries, want 1", n)
			}
			c.wantNoCurrent()
			c.wantActive(nil)
		})

		t.Run("checks the field constraints before synchronized time", func(t *testing.T) {
			// matter.js invokes over the interaction layer; the Go server's
			// MatterInvoke runs the same constraint check first.
			c := onlineSuggestions(t)
			c.clk.setSynced(false)
			_, err := c.add(presetA, nil, 29)
			c.wantStatus(err, im.StatusConstraintError)
		})
	})

	t.Run("UniqueID", func(t *testing.T) {
		t.Run("issues distinct values and does not reuse a removed one at once", func(t *testing.T) {
			c := onlineSuggestions(t)
			first := c.mustAdd(presetA, nil, 0)
			second := c.mustAdd(presetA, nil, 0)
			c.mustRemove(second)
			third := c.mustAdd(presetA, nil, 0)
			if first == second || second == third || first == third {
				t.Errorf("ids %d, %d, %d, want three distinct", first, second, third)
			}
		})

		t.Run("continues after the highest stored value", func(t *testing.T) {
			r := newRestartable(t)
			c := r.online(func(cfg *thermo.ThermostatConfig) { r.store(cfg, r.stored(10, presetA)) })
			if id := c.mustAdd(presetA, nil, 0); id != 11 {
				t.Errorf("UniqueID %d, want 11", id)
			}
		})

		t.Run("wraps after 255 and skips values in use", func(t *testing.T) {
			r := newRestartable(t)
			c := r.online(func(cfg *thermo.ThermostatConfig) { r.store(cfg, r.stored(0, presetA), r.stored(255, presetA)) })
			if id := c.mustAdd(presetA, nil, 0); id != 1 {
				t.Errorf("UniqueID %d, want 1", id)
			}
		})
	})

	t.Run("RemoveThermostatSuggestion", func(t *testing.T) {
		t.Run("removes the entry", func(t *testing.T) {
			c := onlineSuggestions(t)
			first := c.mustAdd(presetA, c.at(600), 0)
			second := c.mustAdd(presetB, c.at(600), 0)
			c.mustRemove(first)
			c.wantIDs(second)
		})

		t.Run("refuses an unknown UniqueID with NOT_FOUND", func(t *testing.T) {
			c := onlineSuggestions(t)
			id := c.mustAdd(presetA, nil, 0)
			c.wantStatus(removeSuggestion(c.srv, id+1), im.StatusNotFound)
			if n := len(readSuggestions(t, c.srv)); n != 1 {
				t.Errorf("%d entries, want 1", n)
			}
		})

		t.Run("clears the current suggestion and keeps the active preset when the current one goes", func(t *testing.T) {
			c := onlineSuggestions(t)
			id := c.mustAdd(presetB, nil, 0)
			c.mustRemove(id)
			c.wantNoCurrent()
			c.wantReason(nil)
			c.wantActive(presetB)
		})

		t.Run("makes the remaining effective entry current", func(t *testing.T) {
			c := onlineSuggestions(t)
			first := c.mustAdd(presetA, c.at(-10), 0)
			second := c.mustAdd(presetB, nil, 0)
			c.mustRemove(first)
			c.wantCurrent(second)
			c.wantActive(presetB)
		})
	})

	t.Run("selection", func(t *testing.T) {
		t.Run("chooses the entry with the earliest EffectiveTime", func(t *testing.T) {
			c := onlineSuggestions(t)
			c.mustAdd(presetA, c.at(-5), 0)
			earlier := c.mustAdd(presetB, c.at(-60), 0)
			c.wantCurrent(earlier)
			c.wantActive(presetB)
		})

		t.Run("chooses the first entry of those with the same EffectiveTime", func(t *testing.T) {
			c := onlineSuggestions(t)
			effective := c.at(0)
			first := c.mustAdd(presetA, effective, 0)
			c.mustAdd(presetB, effective, 0)
			c.wantCurrent(first)
		})

		t.Run("reports the reasons an application returns and does not follow", func(t *testing.T) {
			// The application's suggestionNotFollowingReason is the host's
			// reason through SetAttributes.
			c := onlineSuggestions(t)
			c.set(map[uint32]any{tdef.AttrThermostatSuggestionNotFollowingReason: tdef.ThermostatSuggestionNotFollowingReasonDemandResponseEvent})
			id := c.mustAdd(presetB, nil, 0)
			c.wantCurrent(id)
			c.wantReason(reasonOf(demandResponseEvent))
			c.wantActive(nil)
		})
	})

	t.Run("timer", func(t *testing.T) {
		t.Run("makes an entry current when its EffectiveTime arrives", func(t *testing.T) {
			c := onlineSuggestions(t)
			id := c.mustAdd(presetB, c.at(600), 0)
			c.wantNoCurrent()
			c.advance(599 * time.Second)
			c.wantNoCurrent()
			c.advance(time.Second)
			c.wantCurrent(id)
			c.wantActive(presetB)
		})

		t.Run("removes the current entry when it expires and clears the current suggestion", func(t *testing.T) {
			c := onlineSuggestions(t)
			c.mustAdd(presetB, nil, 30)
			c.advance(30 * time.Minute)
			c.wantIDs()
			c.wantNoCurrent()
			c.wantReason(nil)
			c.wantActive(presetB)
			if n := c.clk.Pending(); n != 0 {
				t.Errorf("%d timers armed, want none", n)
			}
		})

		t.Run("runs until the earliest time of all entries, not only of the current one", func(t *testing.T) {
			c := onlineSuggestions(t)
			current := c.mustAdd(presetA, nil, 60)
			c.mustAdd(presetB, c.at(300), 30)
			c.advance(35 * time.Minute)
			c.wantIDs(current)
			c.wantCurrent(current)
		})

		t.Run("stops when the node closes", func(t *testing.T) {
			// A node closing quiesces its servers (MatterQuiesce).
			c := onlineSuggestions(t)
			c.mustAdd(presetA, nil, 0)
			if n := c.clk.Pending(); n != 1 {
				t.Fatalf("%d timers armed, want 1", n)
			}
			c.srv.MatterQuiesce()
			if n := c.clk.Pending(); n != 0 {
				t.Errorf("%d timers armed after MatterQuiesce, want none", n)
			}
		})

		t.Run("does not retry a current entry after a re-evaluation that commits", func(t *testing.T) {
			// matter.js counts the re-evaluations; here the one timer armed
			// is the entry's expiry, so nothing runs within the minute.
			c := onlineSuggestions(t)
			c.mustAdd(presetB, nil, 30)
			wantTimerAt(t, c, 30*time.Minute)
		})

		t.Run("does not retry a current entry it does not follow after a re-evaluation that commits", func(t *testing.T) {
			c := onlineSuggestions(t)
			c.set(map[uint32]any{tdef.AttrThermostatSuggestionNotFollowingReason: tdef.ThermostatSuggestionNotFollowingReasonDemandResponseEvent})
			c.mustAdd(presetB, nil, 30)
			c.wantReason(reasonOf(demandResponseEvent))
			wantTimerAt(t, c, 30*time.Minute)
		})
	})

	t.Run("time synchronization", func(t *testing.T) {
		t.Run("clears the current suggestion while the time is not synchronized", func(t *testing.T) {
			c := onlineSuggestions(t)
			id := c.mustAdd(presetB, nil, 0)
			c.clk.setSynced(false)
			c.reevaluate()
			c.wantNoCurrent()
			c.wantReason(nil)
			if n := len(readSuggestions(t, c.srv)); n != 1 {
				t.Errorf("%d entries, want 1", n)
			}
			if n := c.clk.Pending(); n != 0 {
				t.Errorf("%d timers armed, want none", n)
			}
			c.clk.setSynced(true)
			c.reevaluate()
			c.wantCurrent(id)
		})

		t.Run("uses the UTCTime of a TimeSynchronization cluster on the node", func(t *testing.T) {
			// The node attaches the root TimeSynchronization's UTC and calls
			// MatterTimeChanged when it moves; here the attached time is the
			// test's own.
			c := onlineSuggestions(t)
			c.clk.setSynced(false)
			_, err := c.add(presetB, nil, 0)
			c.wantStatus(err, im.StatusInvalidInState)
			c.clk.setSynced(true)
			c.reevaluate()
			id := c.mustAdd(presetB, nil, 0)
			c.wantCurrent(id)
			c.clk.setSynced(false)
			c.reevaluate()
			c.wantNoCurrent()
			c.clk.setSynced(true)
			c.reevaluate()
			c.wantCurrent(id)
			// A step of the time re-evaluates on MatterTimeChanged.
			c.clk.StepWall(31 * time.Minute)
			c.reevaluate()
			c.wantIDs()
		})
	})

	t.Run("following", func(t *testing.T) {
		t.Run("re-evaluates when the application writes ThermostatSuggestions", func(t *testing.T) {
			c := onlineSuggestions(t)
			now := c.clk.epoch()
			entry := thermo.Suggestion{UniqueId: 7, PresetHandle: presetB, EffectiveTime: now, ExpirationTime: now + 1800}
			c.set(map[uint32]any{tdef.AttrThermostatSuggestions: spec.List[thermo.Suggestion]{entry}})
			if sg, ok := readCurrent(t, c.srv); !ok || !equalSuggestion(sg, entry) {
				t.Errorf("CurrentThermostatSuggestion %+v (%v), want %+v", sg, ok, entry)
			}
			c.wantActive(presetB)
		})

		t.Run("keeps adjusted setpoints while the suggestion's preset stays active", func(t *testing.T) {
			c := onlineSuggestions(t)
			c.mustAdd(presetB, nil, 0)
			if _, err := c.srv.MatterInvoke(context.Background(), tdef.CmdSetpointRaiseLower,
				tdef.SetpointRaiseLowerRequest{Mode: tdef.SetpointRaiseLowerModeHeat, Amount: 5}); err != nil {
				t.Fatal(err)
			}
			c.wantHeating(2150)
			c.mustAdd(presetA, c.at(600), 0)
			c.wantActive(presetB)
			c.wantHeating(2150)
		})
	})

	t.Run("manual override", func(t *testing.T) {
		// overridden follows a suggestion for preset B, then a client
		// activates preset A (ThermostatSuggestionsTest.ts:907-914).
		overridden := func(c *suggestCase) uint8 {
			c.t.Helper()
			id := c.mustAdd(presetB, nil, 0)
			c.wantActive(presetB)
			c.activate(presetA)
			c.settle()
			return id
		}

		t.Run("stops following when a client activates another preset and keeps that preset", func(t *testing.T) {
			c := onlineSuggestions(t)
			id := overridden(c)
			c.wantCurrent(id)
			c.wantReason(reasonOf(0))
			c.wantActive(presetA)

			c.mustAdd(presetB, c.at(600), 0)
			c.reevaluate()
			c.wantCurrent(id)
			c.wantReason(reasonOf(0))
			c.wantActive(presetA)
		})

		t.Run("stops following when the application activates another preset", func(t *testing.T) {
			// The application's activation is SetActivePresetHandle.
			c := onlineSuggestions(t)
			c.mustAdd(presetB, nil, 0)
			if err := c.srv.SetActivePresetHandle(context.Background(), presetA); err != nil {
				t.Fatal(err)
			}
			c.settle()
			c.wantReason(reasonOf(0))
			c.wantActive(presetA)
		})

		t.Run("stops following when a setpoint write leaves the preset", func(t *testing.T) {
			c := onlineSuggestions(t)
			c.mustAdd(presetB, nil, 0)
			c.set(map[uint32]any{tdef.AttrOccupiedHeatingSetpoint: int16(2050)})
			c.wantReason(reasonOf(0))
			c.wantActive(nil)
			c.wantHeating(2050)
		})

		t.Run("follows again when another entry becomes current and when the overridden entry returns", func(t *testing.T) {
			c := onlineSuggestions(t)
			followed := overridden(c)
			earlier := c.mustAdd(presetA, c.at(-60), 0)
			c.settle()
			c.wantCurrent(earlier)
			c.wantReason(nil)

			c.mustRemove(earlier)
			c.settle()
			c.wantCurrent(followed)
			c.wantReason(nil)
			c.wantActive(presetB)
		})

		t.Run("follows the next entry when the overridden entry expires", func(t *testing.T) {
			c := onlineSuggestions(t)
			overridden(c)
			next := c.mustAdd(presetB, c.at(600), 60)
			c.advance(30 * time.Minute)
			c.wantCurrent(next)
			c.wantReason(nil)
			c.wantActive(presetB)
		})

		t.Run("follows a new entry after the overridden entry is removed", func(t *testing.T) {
			c := onlineSuggestions(t)
			followed := overridden(c)
			c.mustRemove(followed)
			c.settle()
			c.wantNoCurrent()
			c.wantReason(nil)
			c.wantActive(presetA)

			c.mustAdd(presetB, nil, 0)
			c.wantActive(presetB)
		})

		t.Run("reports OngoingHold during a hold and keeps the override after it", func(t *testing.T) {
			c := onlineSuggestions(t)
			overridden(c)
			c.hold(true)
			c.wantReason(reasonOf(ongoingHold))
			c.hold(false)
			c.wantReason(reasonOf(0))
			c.wantActive(presetA)
		})

		t.Run("is an override when a client activates another preset during a hold", func(t *testing.T) {
			c := onlineSuggestions(t)
			c.hold(true)
			c.mustAdd(presetB, nil, 0)
			c.wantActive(nil)
			c.activate(presetA)
			c.settle()
			c.wantReason(reasonOf(ongoingHold))

			c.hold(false)
			c.wantReason(reasonOf(0))
			c.wantActive(presetA)
		})

		t.Run("is no override when a setpoint write during a hold leaves the preset", func(t *testing.T) {
			c := onlineSuggestions(t)
			c.mustAdd(presetB, nil, 0)
			c.hold(true)
			c.set(map[uint32]any{tdef.AttrOccupiedHeatingSetpoint: int16(2050)})
			c.wantActive(nil)

			c.hold(false)
			c.wantReason(nil)
			c.wantActive(presetB)
			c.wantHeating(2100)
		})

		t.Run("keeps an override when a setpoint write during a hold leaves the overriding preset", func(t *testing.T) {
			c := onlineSuggestions(t)
			overridden(c)
			c.hold(true)
			c.set(map[uint32]any{tdef.AttrOccupiedHeatingSetpoint: int16(2050)})

			c.hold(false)
			c.wantReason(reasonOf(0))
			c.wantActive(nil)
			c.wantHeating(2050)
		})

		t.Run("is no override when a setpoint write leaves the preset while the application reports OngoingHold", func(t *testing.T) {
			// The application's OngoingHold is the host's reason through
			// SetAttributes.
			c := onlineSuggestions(t)
			c.mustAdd(presetB, nil, 0)
			c.set(map[uint32]any{tdef.AttrThermostatSuggestionNotFollowingReason: tdef.ThermostatSuggestionNotFollowingReasonOngoingHold})
			c.wantReason(reasonOf(ongoingHold))

			c.set(map[uint32]any{tdef.AttrOccupiedHeatingSetpoint: int16(2050)})
			c.wantActive(nil)

			c.set(map[uint32]any{tdef.AttrThermostatSuggestionNotFollowingReason: nil})
			c.wantReason(nil)
			c.wantActive(presetB)
		})

		t.Run("ends when a client activates the suggestion's preset again, so later changes are followed", func(t *testing.T) {
			c := onlineSuggestions(t)
			overridden(c)
			c.activate(presetB)
			c.settle()
			c.wantReason(nil)
			c.reevaluate()
			c.wantReason(nil)

			earlier := c.mustAdd(presetA, c.at(-60), 0)
			c.settle()
			c.wantCurrent(earlier)
			c.wantActive(presetA)

			c.mustRemove(earlier)
			c.activate(presetA)
			c.settle()
			c.wantReason(reasonOf(0))
		})

		// A Go map carries no order, so both orders are one SetAttributes.
		for _, order := range []string{"hold first", "setpoint first"} {
			holdOffAndSetpoint := map[uint32]any{
				tdef.AttrTemperatureSetpointHold: uint8(tdef.TemperatureSetpointHoldSetpointHoldOff),
				tdef.AttrOccupiedHeatingSetpoint: int16(2050),
			}

			t.Run("is an override when the transaction that ends a hold of the followed preset writes a setpoint, "+order, func(t *testing.T) {
				c := onlineSuggestions(t)
				c.mustAdd(presetB, nil, 0)
				c.hold(true)
				c.set(holdOffAndSetpoint)
				c.wantReason(reasonOf(0))
				c.wantActive(nil)
				c.wantHeating(2050)
			})

			t.Run("is an override when the transaction that ends a hold without an active preset writes a setpoint, "+order, func(t *testing.T) {
				c := onlineSuggestions(t)
				c.hold(true)
				c.mustAdd(presetB, nil, 0)
				c.wantActive(nil)
				c.set(holdOffAndSetpoint)
				c.wantReason(reasonOf(0))
				c.wantActive(nil)
				c.wantHeating(2050)
			})
		}

		t.Run("ends when the suggestion's preset returns while the time is not synchronized", func(t *testing.T) {
			c := onlineSuggestions(t)
			id := overridden(c)
			c.clk.setSynced(false)
			c.reevaluate()
			c.wantNoCurrent()

			c.activate(presetB)
			c.settle()

			c.clk.setSynced(true)
			c.reevaluate()
			c.wantCurrent(id)
			c.wantReason(nil)
		})

		t.Run("ends during a hold when a client activates the suggestion's preset again", func(t *testing.T) {
			c := onlineSuggestions(t)
			overridden(c)
			c.hold(true)
			c.activate(presetB)
			c.settle()
			c.wantReason(reasonOf(ongoingHold))

			c.hold(false)
			c.wantReason(nil)
			c.wantActive(presetB)
		})
	})

	t.Run("manual override across a restart", func(t *testing.T) {
		// The two servers share the stores and the clock: matter.js's
		// MockStorageService environment (ThermostatSuggestionsTest.ts:1211-1215).
		// overriddenSuggestionId is not served; the case reads it from the
		// store, where the server keeps it.

		// overriddenBeforeRestart follows a suggestion for preset B that
		// expires in 30 minutes, then a client activates preset A
		// (ThermostatSuggestionsTest.ts:1218-1228).
		overriddenBeforeRestart := func(r *restartable, next *uint32) uint8 {
			r.t.Helper()
			c := r.online(r.stores)
			followed := c.mustAdd(presetB, nil, 0)
			if next != nil {
				c.mustAdd(presetB, next, 60)
			}
			c.activate(presetA)
			c.settle()
			r.wantOverride(&followed)
			c.srv.MatterQuiesce()
			return followed
		}

		t.Run("stays in effect when its entry is still current", func(t *testing.T) {
			r := newRestartable(t)
			id := overriddenBeforeRestart(r, nil)

			c := r.online(r.stores)
			c.wantCurrent(id)
			r.wantOverride(&id)
			c.wantReason(reasonOf(0))
			c.wantActive(presetA)
			c.wantHeating(2000)
		})

		t.Run("ends when another entry became current", func(t *testing.T) {
			r := newRestartable(t)
			followed := overriddenBeforeRestart(r, r.at(600))

			r.clk.Advance(31 * time.Minute)
			c := r.online(r.stores)
			if id, ok := c.current(); !ok || id == followed {
				t.Errorf("CurrentThermostatSuggestion %d (%v), want another than %d", id, ok, followed)
			}
			r.wantOverride(nil)
			c.wantReason(nil)
			c.wantActive(presetB)
		})

		t.Run("stays in effect while the time is not synchronized after the restart", func(t *testing.T) {
			r := newRestartable(t)
			first := r.online(r.stores)
			id := first.mustAdd(presetB, nil, 0)
			first.activate(presetA)
			first.settle()
			first.srv.MatterQuiesce()

			r.clk.setSynced(false)
			c := r.online(r.stores)
			c.wantNoCurrent()
			r.wantOverride(&id)

			r.clk.setSynced(true)
			c.reevaluate()
			c.wantCurrent(id)
			c.wantReason(reasonOf(0))
			c.wantActive(presetA)
		})

		t.Run("ends when no entry is current any more and keeps the active preset", func(t *testing.T) {
			r := newRestartable(t)
			overriddenBeforeRestart(r, nil)

			r.clk.Advance(31 * time.Minute)
			c := r.online(r.stores)
			c.wantNoCurrent()
			r.wantOverride(nil)
			c.wantActive(presetA)
		})
	})

	t.Run("startup", func(t *testing.T) {
		t.Run("removes stored entries that name no preset", func(t *testing.T) {
			r := newRestartable(t)
			c := r.online(func(cfg *thermo.ThermostatConfig) {
				r.store(cfg, r.stored(1, presetA), r.stored(2, unknownPreset))
			})
			c.wantIDs(1)
			c.wantCurrent(1)
			if n := r.logged("matter.thermostat.suggestions_dropped"); n != 1 {
				t.Errorf("%d notices of dropped entries, want 1", n)
			}
		})

		t.Run("removes stored entries that expired", func(t *testing.T) {
			r := newRestartable(t)
			now := r.clk.epoch()
			c := r.online(func(cfg *thermo.ThermostatConfig) {
				r.store(cfg, thermo.Suggestion{UniqueId: 1, PresetHandle: presetA, EffectiveTime: now - 3600, ExpirationTime: now - 1})
			})
			c.wantIDs()
			c.wantNoCurrent()
		})

		t.Run("raises MaxThermostatSuggestions to cover the stored entries", func(t *testing.T) {
			r := newRestartable(t)
			stored := make([]thermo.Suggestion, 0, 7)
			for id := range uint8(7) {
				stored = append(stored, r.stored(id+1, presetA))
			}
			c := r.online(func(cfg *thermo.ThermostatConfig) {
				r.store(cfg, stored...)
				cfg.MaxThermostatSuggestions = 5
			})
			if v, _ := c.srv.MatterRead(tdef.AttrMaxThermostatSuggestions); v != uint8(7) {
				t.Errorf("MaxThermostatSuggestions %v, want 7", v)
			}
			if n := len(readSuggestions(t, c.srv)); n != 7 {
				t.Errorf("%d entries, want 7", n)
			}
			if n := r.logged(`msg=matter.thermostat.limit_raised attribute=MaxThermostatSuggestions value=7`); n != 1 {
				t.Errorf("%d notices raising MaxThermostatSuggestions to 7, want 1", n)
			}
		})

		t.Run("sets up the endpoint when it is added to a node that is online", func(t *testing.T) {
			// A node attaches its Matter time to every server of each
			// topology it serves (node/matter_time.go attachMatterTime), the
			// one an Add brings included: the server built with a stored
			// entry follows it and arms the timer then. LocalTemperature has
			// no part in the Go server's start (matter.js's
			// externalMeasuredIndoorTemperature, LTNE, is the host's
			// SetLocalTemperature here).
			r := newRestartable(t)
			c := r.online(func(cfg *thermo.ThermostatConfig) { r.store(cfg, r.stored(1, presetB)) })
			c.wantCurrent(1)
			c.wantActive(presetB)
			if n := r.clk.Pending(); n != 1 {
				t.Errorf("%d timers armed, want 1", n)
			}
		})
	})

	t.Run("setpoint hold", func(t *testing.T) {
		t.Run("does not follow during a hold and follows once the hold ends", func(t *testing.T) {
			c := onlineSuggestions(t)
			c.hold(true)
			id := c.mustAdd(presetB, nil, 0)
			c.wantCurrent(id)
			c.wantReason(reasonOf(ongoingHold))
			c.wantActive(nil)

			c.hold(false)
			c.wantReason(nil)
			c.wantActive(presetB)
			c.wantHeating(2100)
		})

		t.Run("stops following when a hold starts and keeps the active preset", func(t *testing.T) {
			c := onlineSuggestions(t)
			c.mustAdd(presetB, nil, 0)
			c.hold(true)
			c.wantReason(reasonOf(ongoingHold))
			c.wantActive(presetB)
		})
	})

	t.Run("presets", func(t *testing.T) {
		withActiveA := func(cfg *thermo.ThermostatConfig) { cfg.ActivePresetHandle = presetA }
		onlyA := func() []thermo.Preset { return suggestionPresets()[:1] }

		t.Run("removes the current suggestion and the entries of a removed preset", func(t *testing.T) {
			c := onlineSuggestions(t, withActiveA)
			c.hold(true)
			c.mustAdd(presetB, nil, 0)
			c.mustAdd(presetB, c.at(600), 0)
			if sg, ok := readCurrent(t, c.srv); !ok || !bytes.Equal(sg.PresetHandle, presetB) {
				t.Fatalf("CurrentThermostatSuggestion %+v (%v), want one of preset B", sg, ok)
			}
			if err := c.srv.SetPresets(context.Background(), onlyA()); err != nil {
				t.Fatal(err)
			}
			c.wantIDs()
			c.wantNoCurrent()
			c.wantReason(nil)
			c.wantActive(presetA)
		})

		t.Run("removes the entries of a removed preset and keeps the current suggestion", func(t *testing.T) {
			c := onlineSuggestions(t)
			current := c.mustAdd(presetA, nil, 0)
			c.mustAdd(presetB, c.at(600), 0)
			if err := c.srv.SetPresets(context.Background(), onlyA()); err != nil {
				t.Fatal(err)
			}
			c.wantIDs(current)
			c.wantCurrent(current)
		})

		t.Run("keeps the entries when the removal of the preset rolls back", func(t *testing.T) {
			// The application refuses the presets: in Go its PresetStore.
			ps := &refusingStore{}
			c := onlineSuggestions(t, func(cfg *thermo.ThermostatConfig) { cfg.PresetStore = ps })
			c.mustAdd(presetA, nil, 0)
			c.mustAdd(presetB, c.at(600), 0)
			ps.err = errors.New("refuse the presets after the thermostat handled them")
			if err := c.srv.SetPresets(context.Background(), onlyA()); err == nil {
				t.Fatal("SetPresets accepted")
			}
			if n := len(readSuggestions(t, c.srv)); n != 2 {
				t.Errorf("%d entries, want 2", n)
			}
			if n := len(c.srv.Presets()); n != 2 {
				t.Errorf("%d presets, want 2", n)
			}
		})

		t.Run("keeps the entries when the removal is refused", func(t *testing.T) {
			c := onlineSuggestions(t)
			c.mustAdd(presetB, nil, 0)
			c.wantStatus(c.srv.SetPresets(context.Background(), onlyA()), im.StatusInvalidInState)
			if n := len(readSuggestions(t, c.srv)); n != 1 {
				t.Errorf("%d entries, want 1", n)
			}
			if sg, ok := readCurrent(t, c.srv); !ok || !bytes.Equal(sg.PresetHandle, presetB) {
				t.Errorf("CurrentThermostatSuggestion %+v (%v), want one of preset B", sg, ok)
			}
		})
	})

	t.Run("MaxThermostatSuggestions", func(t *testing.T) {
		t.Run("defaults to 5", func(t *testing.T) {
			c := onlineSuggestions(t)
			if v, _ := c.srv.MatterRead(tdef.AttrMaxThermostatSuggestions); v != uint8(5) {
				t.Errorf("MaxThermostatSuggestions %v, want 5", v)
			}
		})

		t.Run("accepts as many entries as configured", func(t *testing.T) {
			c := onlineSuggestions(t, func(cfg *thermo.ThermostatConfig) { cfg.MaxThermostatSuggestions = 7 })
			for range 7 {
				c.mustAdd(presetA, nil, 0)
			}
			_, err := c.add(presetA, nil, 0)
			c.wantStatus(err, im.StatusResourceExhausted)
		})

		t.Run("refuses to start with fewer than 5", func(t *testing.T) {
			cfg := suggestionsConfig(newSuggestClock(), func(cfg *thermo.ThermostatConfig) { cfg.MaxThermostatSuggestions = 4 })
			if _, err := thermo.New(cfg); err == nil {
				t.Error("New accepted MaxThermostatSuggestions 4")
			}
		})
	})

	t.Run("preset activation", func(t *testing.T) {
		t.Run("keeps a preset the application activates", func(t *testing.T) {
			c := onlineSuggestions(t)
			if err := c.srv.ActivatePreset(context.Background(), presetB); err != nil {
				t.Fatal(err)
			}
			c.settle()
			c.wantActive(presetB)
			c.wantHeating(2100)
			if got := readInt16(t, c.srv, tdef.AttrOccupiedCoolingSetpoint); got != 2500 {
				t.Errorf("OccupiedCoolingSetpoint %d, want 2500", got)
			}
		})

		t.Run("leaves the preset on a setpoint write after the activation committed", func(t *testing.T) {
			// The activation is a client's; the setpoint write the
			// application's, a change of its own.
			c := onlineSuggestions(t)
			c.activate(presetB)
			c.set(map[uint32]any{tdef.AttrOccupiedHeatingSetpoint: int16(2050)})
			c.wantActive(nil)
		})

		t.Run("still leaves the preset on a later setpoint write", func(t *testing.T) {
			c := onlineSuggestions(t)
			c.activate(presetB)
			c.wantHeating(2100)
			c.set(map[uint32]any{tdef.AttrOccupiedHeatingSetpoint: int16(2050)})
			c.wantActive(nil)
		})

		t.Run("keeps the preset when a limit change moves the setpoint", func(t *testing.T) {
			c := onlineSuggestions(t)
			c.mustAdd(presetB, nil, 0)
			c.wantHeating(2100)
			c.set(map[uint32]any{tdef.AttrMaxHeatSetpointLimit: int16(2050)})
			c.wantHeating(2050)
			c.wantActive(presetB)
			c.wantReason(nil)
		})

		t.Run("follows when the transaction that adds the entry moves the setpoint to keep a limit", func(t *testing.T) {
			c := onlineSuggestions(t, func(cfg *thermo.ThermostatConfig) { cfg.InitialHeatingSetpoint = 2100 })
			now := c.clk.epoch()
			entry := thermo.Suggestion{UniqueId: 7, PresetHandle: presetB, EffectiveTime: now, ExpirationTime: now + 1800}
			c.set(map[uint32]any{
				tdef.AttrMaxHeatSetpointLimit:  int16(2050),
				tdef.AttrThermostatSuggestions: spec.List[thermo.Suggestion]{entry},
			})
			c.wantReason(nil)
			c.wantActive(presetB)
			c.wantHeating(2050)
		})

		t.Run("follows the entry that becomes current after another one in the same transaction", func(t *testing.T) {
			// One after the other: Go has no transaction around the three
			// commands.
			c := onlineSuggestions(t)
			id := c.mustAdd(presetB, nil, 0)
			c.mustRemove(id)
			c.mustAdd(presetA, nil, 0)
			c.settle()
			c.wantReason(nil)
			c.wantActive(presetA)
		})

		t.Run("with the Occupancy feature", func(t *testing.T) {
			// OccupancyServer (:1620): Heating, Cooling, AutoMode, Presets
			// and Occupancy, preset A active, the unoccupied setpoints
			// 18.00 / 28.00 °C.
			occupancyOnline := func(t *testing.T, occupied bool) *thermo.ThermostatServer {
				t.Helper()
				cfg := thermostatConfig(5, suggestionPresets())
				cfg.Features = thermo.ThermostatFeatureHEAT | thermo.ThermostatFeatureCOOL | thermo.ThermostatFeatureAUTO |
					thermo.ThermostatFeaturePRES | thermo.ThermostatFeatureOCC
				cfg.PresetTypes = []thermo.PresetType{
					{PresetScenario: tdef.PresetScenarioOccupied, NumberOfPresets: 5},
					{PresetScenario: tdef.PresetScenarioUnoccupied, NumberOfPresets: 5},
				}
				cfg.ActivePresetHandle = presetA
				heat, cool := int16(1800), int16(2800)
				cfg.InitialUnoccupiedHeatingSetpoint, cfg.InitialUnoccupiedCoolingSetpoint = &heat, &cool
				cfg.Occupied = &occupied
				return mustNew(t, cfg)
			}
			for _, occupied := range []bool{true, false} {
				active, inactive := tdef.AttrOccupiedHeatingSetpoint, tdef.AttrUnoccupiedHeatingSetpoint
				activeName, inactiveName := "occupiedHeatingSetpoint", "unoccupiedHeatingSetpoint"
				if !occupied {
					active, inactive = inactive, active
					activeName, inactiveName = inactiveName, activeName
				}
				t.Run("keeps the preset on a write of "+inactiveName+" and leaves it on a write of "+activeName, func(t *testing.T) {
					srv := occupancyOnline(t, occupied)
					if err := srv.SetAttributes(map[uint32]any{inactive: int16(1900)}); err != nil {
						t.Fatal(err)
					}
					if h := srv.ActivePresetHandle(); !bytes.Equal(h, presetA) {
						t.Errorf("ActivePresetHandle %x after a write of %s, want %x", h, inactiveName, presetA)
					}
					if err := srv.SetAttributes(map[uint32]any{active: int16(1900)}); err != nil {
						t.Fatal(err)
					}
					if h := srv.ActivePresetHandle(); h != nil {
						t.Errorf("ActivePresetHandle %x after a write of %s, want null", h, activeName)
					}
				})
			}
		})
	})

	t.Run("through a controller", func(t *testing.T) {
		t.Run("follows once a client ends the hold and drops entries of a preset an atomic write removes", func(t *testing.T) {
			p, clk := commissionedSuggestions(t, func(cfg *thermo.ThermostatConfig) { cfg.ActivePresetHandle = presetA })
			ctx := parityCtx(t)
			p.writeHold(ctx, t, true)
			p.invokeAdd(ctx, t, presetB, nil)
			if r := readReason(t, p.srv); r == nil || *r != ongoingHold {
				t.Errorf("ThermostatSuggestionNotFollowingReason %v, want OngoingHold", r)
			}
			if h := p.srv.ActivePresetHandle(); !bytes.Equal(h, presetA) {
				t.Errorf("ActivePresetHandle %x, want %x", h, presetA)
			}

			p.writeHold(ctx, t, false)
			if r := readReason(t, p.srv); r != nil {
				t.Errorf("ThermostatSuggestionNotFollowingReason %d, want null", *r)
			}
			if h := p.srv.ActivePresetHandle(); !bytes.Equal(h, presetB) {
				t.Errorf("ActivePresetHandle %x, want %x", h, presetB)
			}
			if got := readInt16(t, p.srv, tdef.AttrOccupiedHeatingSetpoint); got != 2100 {
				t.Errorf("OccupiedHeatingSetpoint %d, want 2100", got)
			}

			later := clk.epoch() + 600
			p.invokeAdd(ctx, t, presetA, &later)
			if n := len(readSuggestions(t, p.srv)); n != 2 {
				t.Fatalf("%d entries, want 2", n)
			}

			if _, err := p.atomicRequest(ctx, p.pair, beginWrite, []uint32{presetsAttribute}, timeoutMs(5000)); err != nil {
				t.Fatal(err)
			}
			if statuses, err := p.writePresets(ctx, p.pair, suggestionPresets()[1:]); err != nil || statuses.Err() != nil {
				t.Fatalf("write: %+v, %v", statuses, err)
			}
			r, err := p.atomicRequest(ctx, p.pair, commitWrite, []uint32{presetsAttribute}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if got := decodeAtomicResponse(t, r); got.StatusCode != tdef.StatusSuccess {
				t.Fatalf("CommitWrite %+v", got)
			}
			got := readSuggestions(t, p.srv)
			if len(got) != 1 || !bytes.Equal(got[0].PresetHandle, presetB) {
				t.Errorf("ThermostatSuggestions %+v, want the one of preset B", got)
			}
			if sg, ok := readCurrent(t, p.srv); !ok || !bytes.Equal(sg.PresetHandle, presetB) {
				t.Errorf("CurrentThermostatSuggestion %+v (%v), want the one of preset B", sg, ok)
			}
		})
	})

	t.Run("manual override through a controller", func(t *testing.T) {
		t.Run("stops following after a client SetActivePresetRequest and after a client setpoint write", func(t *testing.T) {
			p, _ := commissionedSuggestions(t)
			ctx := parityCtx(t)
			id := p.invokeAdd(ctx, t, presetB, nil)
			if h := p.srv.ActivePresetHandle(); !bytes.Equal(h, presetB) {
				t.Fatalf("ActivePresetHandle %x, want %x", h, presetB)
			}
			if _, err := fabrictest.Invoke(ctx, p.pair, p.commandPath(tdef.CmdSetActivePresetRequest),
				tdef.SetActivePresetRequestRequest{PresetHandle: spec.ValueOf(presetA)}); err != nil {
				t.Fatal(err)
			}
			if r := readReason(t, p.srv); r == nil || *r != 0 {
				t.Errorf("ThermostatSuggestionNotFollowingReason %v, want the empty bitmap", r)
			}
			if h := p.srv.ActivePresetHandle(); !bytes.Equal(h, presetA) {
				t.Errorf("ActivePresetHandle %x, want %x", h, presetA)
			}

			if _, err := fabrictest.Invoke(ctx, p.pair, p.commandPath(tdef.CmdRemoveThermostatSuggestion),
				tdef.RemoveThermostatSuggestionRequest{UniqueId: id}); err != nil {
				t.Fatal(err)
			}
			p.invokeAdd(ctx, t, presetB, nil)
			if r := readReason(t, p.srv); r != nil {
				t.Errorf("ThermostatSuggestionNotFollowingReason %d, want null", *r)
			}
			if h := p.srv.ActivePresetHandle(); !bytes.Equal(h, presetB) {
				t.Errorf("ActivePresetHandle %x, want %x", h, presetB)
			}
			p.write(ctx, t, tdef.AttrOccupiedHeatingSetpoint, spec.Int16(2050))
			if h := p.srv.ActivePresetHandle(); h != nil {
				t.Errorf("ActivePresetHandle %x, want null", h)
			}
			if r := readReason(t, p.srv); r == nil || *r != 0 {
				t.Errorf("ThermostatSuggestionNotFollowingReason %v, want the empty bitmap", r)
			}
		})

		t.Run("follows when one client Write writes a setpoint and ends the hold", func(t *testing.T) {
			// Each attribute of a Write commits on its own, the setpoint
			// first, so the setpoint write belongs to the hold.
			p, _ := commissionedSuggestions(t, func(cfg *thermo.ThermostatConfig) {
				on := uint8(tdef.TemperatureSetpointHoldSetpointHoldOn)
				cfg.TemperatureSetpointHold = &on
			})
			ctx := parityCtx(t)
			p.invokeAdd(ctx, t, presetB, nil)
			p.writeAll(ctx, t, []interaction.WriteRequest{
				{Path: p.attrPath(tdef.AttrOccupiedHeatingSetpoint), Value: spec.Int16(2050)},
				{Path: p.attrPath(tdef.AttrTemperatureSetpointHold), Value: spec.Uint8(uint8(tdef.TemperatureSetpointHoldSetpointHoldOff))},
			})
			if h := p.srv.ActivePresetHandle(); !bytes.Equal(h, presetB) {
				t.Errorf("ActivePresetHandle %x, want %x", h, presetB)
			}
			if got := readInt16(t, p.srv, tdef.AttrOccupiedHeatingSetpoint); got != 2100 {
				t.Errorf("OccupiedHeatingSetpoint %d, want 2100", got)
			}
		})
	})

	t.Run("ActivePresetChange", func(t *testing.T) {
		t.Run("is generated when the thermostat follows a suggestion", func(t *testing.T) {
			c := onlineSuggestions(t, func(cfg *thermo.ThermostatConfig) { cfg.Features |= thermo.ThermostatFeatureTEVT })
			rec := &activePresetEvents{}
			c.srv.SetMatterEventEmitter(rec)
			c.mustAdd(presetB, c.at(60), 0)
			c.advance(60 * time.Second)
			got := rec.taken()
			if len(got) != 1 || !got[0].PreviousPresetHandle.Null || got[0].CurrentPresetHandle.Null ||
				!bytes.Equal(got[0].CurrentPresetHandle.Value, presetB) {
				t.Errorf("ActivePresetChange %+v, want one from null to preset B", got)
			}
		})
	})
}

// wantTimerAt checks that the one timer armed fires d from now.
func wantTimerAt(t *testing.T, c *suggestCase, d time.Duration) {
	t.Helper()
	next, ok := c.clk.Next()
	if n := c.clk.Pending(); n != 1 || !ok || !next.Equal(c.clk.Now().Add(d)) {
		t.Errorf("%d timers, the next at %v (%v), want one at %v", n, next, ok, c.clk.Now().Add(d))
	}
}

// activePresetEvents records the ActivePresetChange events.
type activePresetEvents struct {
	mu     sync.Mutex
	events []tdef.ActivePresetChangeEvent
}

func (r *activePresetEvents) MatterEmitEvent(_ uint16, _, event uint32, data any, _ contract.EventPriority) {
	if event != tdef.EventActivePresetChange {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, data.(tdef.ActivePresetChangeEvent))
}

func (r *activePresetEvents) taken() []tdef.ActivePresetChangeEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.events)
}

// commissionedSuggestions is MockSite.addCommissionedPair with the
// suggestions thermostat; the Matter time is the test's, attached after
// the node attached its own.
func commissionedSuggestions(t *testing.T, edit ...func(*thermo.ThermostatConfig)) (*thermostatPair, *suggestClock) {
	t.Helper()
	clk := newSuggestClock()
	p := commissionedThermostat(t, func(*fabrictest.Site) thermo.ThermostatConfig { return suggestionsConfig(clk, edit...) })
	p.srv.AttachMatterTime(clk.matterTime)
	return p, clk
}

// invokeAdd invokes AddThermostatSuggestion from the pair's controller, 30
// minutes; effective nil is null.
func (p *thermostatPair) invokeAdd(ctx context.Context, t *testing.T, handle []byte, effective *uint32) uint8 {
	t.Helper()
	req := tdef.AddThermostatSuggestionRequest{PresetHandle: handle, ExpirationInMinutes: 30, EffectiveTime: spec.NullOf[uint32]()}
	if effective != nil {
		req.EffectiveTime = spec.ValueOf(*effective)
	}
	r, err := fabrictest.Invoke(ctx, p.pair, p.commandPath(tdef.CmdAddThermostatSuggestion), req)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := spec.DecodeStruct[tdef.AddThermostatSuggestionResponse](r.Fields)
	if err != nil {
		t.Fatal(err)
	}
	return resp.UniqueId
}

// writeHold writes TemperatureSetpointHold from the pair's controller.
func (p *thermostatPair) writeHold(ctx context.Context, t *testing.T, on bool) {
	t.Helper()
	v := uint8(tdef.TemperatureSetpointHoldSetpointHoldOff)
	if on {
		v = uint8(tdef.TemperatureSetpointHoldSetpointHoldOn)
	}
	p.write(ctx, t, tdef.AttrTemperatureSetpointHold, spec.Uint8(v))
}

// write writes one attribute from the pair's controller.
func (p *thermostatPair) write(ctx context.Context, t *testing.T, attr uint32, v spec.Encodable) {
	t.Helper()
	p.writeAll(ctx, t, []interaction.WriteRequest{{Path: p.attrPath(attr), Value: v}})
}

// writeAll sends one Write of reqs from the pair's controller.
func (p *thermostatPair) writeAll(ctx context.Context, t *testing.T, reqs []interaction.WriteRequest) {
	t.Helper()
	ia, err := fabrictestseam.Connect(ctx, p.pair)
	if err != nil {
		t.Fatal(err)
	}
	var statuses interaction.WriteStatuses
	if err := p.site.Run(ctx, func(ctx context.Context) error {
		var werr error
		statuses, werr = ia.Write(ctx, reqs, interaction.WriteOptions{})
		return werr
	}); err != nil {
		t.Fatal(err)
	}
	if err := statuses.Err(); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// restartable is a server's stores and clock across restarts: matter.js's
// MockStorageService environment (ThermostatSuggestionsTest.ts:1211-1215)
// and MockTime. The log of every server it builds is kept.
type restartable struct {
	t      *testing.T
	clk    *suggestClock
	ms     *memstore.Store
	log    syncBuffer
	logger *slog.Logger
}

func newRestartable(t *testing.T) *restartable {
	t.Helper()
	r := &restartable{t: t, clk: newSuggestClock(), ms: memstore.New()}
	r.logger = slog.New(slog.NewTextHandler(&r.log, nil))
	return r
}

// stores wires the presets' and the suggestions' stores.
func (r *restartable) stores(cfg *thermo.ThermostatConfig) {
	cfg.PresetStore = store.NewThermostatPresetStore(r.ms, "thermostat")
	cfg.SuggestionStore = store.NewThermostatSuggestionStore(r.ms, "thermostat")
}

// store wires the stores with list stored before the start.
func (r *restartable) store(cfg *thermo.ThermostatConfig, list ...thermo.Suggestion) {
	r.t.Helper()
	r.stores(cfg)
	enc := tlv.NewEncoder()
	spec.List[thermo.Suggestion](list).EncodeTLV(enc, tlv.AnonymousTag())
	b, err := enc.Bytes()
	if err != nil {
		r.t.Fatal(err)
	}
	if err := cfg.SuggestionStore.SaveSuggestions(context.Background(), b, nil); err != nil {
		r.t.Fatal(err)
	}
}

// stored is storedSuggestion (ThermostatSuggestionsTest.ts:129-133): an
// entry current for the next 30 minutes.
func (r *restartable) stored(id uint8, handle []byte) thermo.Suggestion {
	now := r.clk.epoch()
	return thermo.Suggestion{UniqueId: id, PresetHandle: handle, EffectiveTime: now, ExpirationTime: now + 1800}
}

// at is nowS() + d seconds.
func (r *restartable) at(d int64) *uint32 {
	v := uint32(int64(r.clk.epoch()) + d) //nolint:gosec // a Matter epoch second
	return &v
}

// online starts a server on the shared clock and log, attaches the Matter
// time as a node does and settles its start.
func (r *restartable) online(edit ...func(*thermo.ThermostatConfig)) *suggestCase {
	r.t.Helper()
	cfg := suggestionsConfig(r.clk, edit...)
	cfg.Logger = r.logger
	c := &suggestCase{t: r.t, clk: r.clk, srv: newTSUGGEST(r.t, cfg)}
	c.settle()
	return c
}

// wantOverride checks the stored override (matter.js's
// overriddenSuggestionId, :3183): nil for none.
func (r *restartable) wantOverride(want *uint8) {
	r.t.Helper()
	_, got, _, err := store.NewThermostatSuggestionStore(r.ms, "thermostat").LoadSuggestions(context.Background())
	if err != nil {
		r.t.Fatal(err)
	}
	if (got == nil) != (want == nil) || (got != nil && *got != *want) {
		r.t.Errorf("stored override %v, want %v", overrideText(got), overrideText(want))
	}
}

func overrideText(p *uint8) any {
	if p == nil {
		return "none"
	}
	return *p
}

// logged counts the log lines that contain s.
func (r *restartable) logged(s string) int {
	return strings.Count(r.log.String(), s)
}

// syncBuffer is a bytes.Buffer the servers' timer goroutines log into.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
