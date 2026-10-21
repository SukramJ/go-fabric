// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behaviors/thermostat/PresetsLocalWriteTest.ts
// at 862ad75f: local writes of the presets — handles, the announcement,
// persistence across a restart, the write rules, the active preset and the
// built-in presets.
//
// matter.js writes on a MockServerNode (`deviceEp.set({ thermostat:
// { presets } })`); the port runs a fabrictest device and writes with the
// server's SetPresets inside Device.Act — the one local write of the Go
// server, for the Presets attribute and for persistedPresets alike — and
// reads the stored presets with Presets() (`persistedPresets`) or the
// attribute with MatterRead. recordThermostatChanges is the server's
// OnAttributeChange. The restart case adds the thermostat to the
// running node (Aggregator.Add inside Device.Act), so its PresetStore is
// the node's own store, restarts the device and adds a new server on the
// restarted node's store.
//
// Where matter.js shares one transaction between several writes, the Go
// server has none: each SetPresets is one write, validated as a whole. A
// write an application reactor refuses is, in Go, one the PresetStore
// refuses (the host's say before anything is stored).
//
// Not ported, each relying on an application observer of
// persistedPresets$Changing that rewrites the presets in flight — a seam
// the Go server does not have: "refuses a preset an observer stripped the
// handle from, and stays writable", "checks the settled presets of every
// commit that shares one transaction", "refuses a preset an observer marks
// built-in after the thermostat issued its handle" and "validates a change
// an observer makes after the thermostat normalized".
package thermo_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/thermo"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
)

// localThermostat is PresetsLocalWriteTest.ts:22-26: the presets
// thermostat on a node that is online.
type localThermostat struct {
	site *fabrictest.Site
	dev  *fabrictest.Device
	srv  *thermo.ThermostatServer
}

func newLocalThermostat(t *testing.T, numberOfPresets uint8, presets []thermo.Preset) *localThermostat {
	t.Helper()
	site, dev, srv := onlineThermostat(t, thermostatConfig(numberOfPresets, presets))
	return &localThermostat{site: site, dev: dev, srv: srv}
}

// writePresets is PresetsLocalWriteTest.ts:28-30.
func (l *localThermostat) writePresets(t *testing.T, presets []thermo.Preset) error {
	t.Helper()
	return act(t, l.dev, func(ctx context.Context) error { return l.srv.SetPresets(ctx, presets) })
}

// mustWrite is a write the case expects to succeed.
func (l *localThermostat) mustWrite(t *testing.T, presets []thermo.Preset) {
	t.Helper()
	if err := l.writePresets(t, presets); err != nil {
		t.Fatal(err)
	}
}

// wantNoneStored checks that nothing is stored.
func (l *localThermostat) wantNoneStored(t *testing.T) {
	t.Helper()
	if got := l.srv.Presets(); len(got) != 0 {
		t.Errorf("stored presets %+v, want none", got)
	}
}

// refusingStore is a PresetStore whose saves the host refuses once err is
// set.
type refusingStore struct{ err error }

func (*refusingStore) LoadPresets(context.Context) (presets, activeHandle []byte, ok bool, err error) {
	return nil, nil, false, nil
}

func (r *refusingStore) SavePresets(context.Context, []byte, []byte) error { return r.err }

func TestParityMatterJS_PresetsLocalWriteTest(t *testing.T) {
	t.Run("stores a locally written preset and generates a handle", func(t *testing.T) {
		l := newLocalThermostat(t, 5, []thermo.Preset{})
		l.mustWrite(t, []thermo.Preset{newPreset()})
		stored := l.srv.Presets()
		if len(stored) != 1 || len(stored[0].PresetHandle.Value) != 16 || *stored[0].CoolingSetpoint != 2600 || stored[0].BuiltIn.Null || stored[0].BuiltIn.Value {
			t.Errorf("stored presets %+v", stored)
		}
	})

	t.Run("stores presets written directly to persistedPresets", func(t *testing.T) {
		// The Go server's one local write stores what persistedPresets holds.
		l := newLocalThermostat(t, 5, []thermo.Preset{})
		l.mustWrite(t, []thermo.Preset{newPreset()})
		if stored := l.srv.Presets(); len(stored) != 1 || len(stored[0].PresetHandle.Value) != 16 {
			t.Errorf("stored presets %+v", stored)
		}
	})

	t.Run("serves the stored presets through the attribute", func(t *testing.T) {
		l := newLocalThermostat(t, 5, []thermo.Preset{})
		l.mustWrite(t, []thermo.Preset{newPreset()})
		presets := servedPresets(t, l.srv)
		if len(presets) != 1 || *presets[0].CoolingSetpoint != 2600 || *presets[0].HeatingSetpoint != 2000 || len(presets[0].PresetHandle.Value) != 16 {
			t.Errorf("served presets %+v", presets)
		}
	})

	t.Run("announces the attribute as changed", func(t *testing.T) {
		l := newLocalThermostat(t, 5, []thermo.Preset{})
		announcements := recordThermostatChanges(l.srv)
		l.mustWrite(t, []thermo.Preset{newPreset()})
		if n := announcements.count(presetsAttribute); n != 1 {
			t.Errorf("Presets announced %d times, want 1", n)
		}
	})

	t.Run("announces nothing when the write is refused", func(t *testing.T) {
		l := newLocalThermostat(t, 2, []thermo.Preset{})
		announcements := recordThermostatChanges(l.srv)
		if err := l.writePresets(t, []thermo.Preset{newPreset(), newPreset(), newPreset()}); err == nil {
			t.Fatal("three presets for two accepted")
		}
		if n := announcements.count(presetsAttribute); n != 0 {
			t.Errorf("Presets announced %d times, want 0", n)
		}
	})

	t.Run("keeps the generated handle across a restart", func(t *testing.T) {
		site := fabrictest.NewSite(t, fabrictest.Options{})
		dev := site.AddDevice(fabrictest.Topology{})
		// addThermostat builds the presets thermostat on the running node's
		// store and adds it: matter.js node.add(presetsEndpoint()).
		addThermostat := func() *thermo.ThermostatServer {
			var srv *thermo.ThermostatServer
			if err := dev.Act(func(n *node.Node) error {
				cfg := thermostatConfig(5, []thermo.Preset{})
				cfg.Clock = site.Clock()
				cfg.PresetStore = n.Store().ThermostatPresets("thermostat")
				var err error
				if srv, err = thermo.New(cfg); err != nil {
					return err
				}
				return n.Aggregator().Add(context.Background(), thermostatEndpoint(srv))
			}); err != nil {
				t.Fatal(err)
			}
			return srv
		}

		first := addThermostat()
		if err := act(t, dev, func(ctx context.Context) error { return first.SetPresets(ctx, []thermo.Preset{newPreset()}) }); err != nil {
			t.Fatal(err)
		}
		handle := first.Presets()[0].PresetHandle.Value
		if len(handle) != 16 {
			t.Fatalf("handle %x, want 16 bytes", handle)
		}

		if err := dev.Restart(parityCtx(t)); err != nil {
			t.Fatal(err)
		}
		second := addThermostat()
		stored := second.Presets()
		if len(stored) != 1 || !bytes.Equal(stored[0].PresetHandle.Value, handle) {
			t.Errorf("presets after the restart %+v, want the one with handle %x", stored, handle)
		}
	})

	t.Run("keeps an element-wise edit inside the transaction that made it", func(t *testing.T) {
		// The Go server hands out a copy of its presets: an edit of an
		// element that is never written reaches nothing, as matter.js's
		// abandoned transaction does not.
		l := newLocalThermostat(t, 5, []thermo.Preset{})
		l.mustWrite(t, []thermo.Preset{newPreset()})
		edited := l.srv.Presets()
		h := int16(2100)
		edited[0].HeatingSetpoint = &h
		if got := *l.srv.Presets()[0].HeatingSetpoint; got != 2000 {
			t.Errorf("stored heating setpoint %d, want 2000", got)
		}
	})

	t.Run("edits a stored preset addressed by its handle", func(t *testing.T) {
		l := newLocalThermostat(t, 5, []thermo.Preset{})
		l.mustWrite(t, []thermo.Preset{newPreset()})
		handle := l.srv.Presets()[0].PresetHandle.Value
		h := int16(2100)
		l.mustWrite(t, []thermo.Preset{newPreset(withHandle(handle), func(p *thermo.Preset) { p.HeatingSetpoint = &h })})
		stored := l.srv.Presets()
		if len(stored) != 1 || !bytes.Equal(stored[0].PresetHandle.Value, handle) || *stored[0].HeatingSetpoint != 2100 {
			t.Errorf("stored presets %+v", stored)
		}
	})

	t.Run("rejects a preset citing an unknown handle and keeps the stored presets", func(t *testing.T) {
		l := newLocalThermostat(t, 5, []thermo.Preset{})
		l.mustWrite(t, []thermo.Preset{newPreset()})
		before := l.srv.Presets()
		err := l.writePresets(t, []thermo.Preset{newPreset(withHandle([]byte{1, 2, 3, 4}))})
		wantStatusCode(t, err, im.StatusNotFound, "does not exist in old Presets")
		after := l.srv.Presets()
		if len(after) != 1 || !bytes.Equal(after[0].PresetHandle.Value, before[0].PresetHandle.Value) {
			t.Errorf("stored presets %+v, want %+v", after, before)
		}
	})

	t.Run("validates every write that shares one transaction", func(t *testing.T) {
		// A refused write leaves the next one validated as well.
		l := newLocalThermostat(t, 5, []thermo.Preset{})
		unknownHandle := []thermo.Preset{newPreset(withHandle([]byte{1, 2, 3, 4}))}
		for range 2 {
			wantStatusCode(t, l.writePresets(t, unknownHandle), im.StatusNotFound, "does not exist in old Presets")
		}
		l.wantNoneStored(t)
	})

	t.Run("validates again after a write it had accepted was rolled back", func(t *testing.T) {
		// The application refuses the change — in Go its PresetStore, which
		// refuses every save after the seed — so a write the thermostat had
		// validated is not stored, and the next one is validated again.
		site := fabrictest.NewSite(t, fabrictest.Options{})
		ps := &refusingStore{}
		cfg := thermostatConfig(5, []thermo.Preset{})
		cfg.PresetStore = ps
		srv := newServer(t, site, cfg)
		dev := site.AddDevice(fabrictest.Topology{Bridged: []node.Endpoint{thermostatEndpoint(srv)}})
		ps.err = errors.New("application refuses these presets")
		l := &localThermostat{site: site, dev: dev, srv: srv}

		var outcomes []string
		for _, presets := range [][]thermo.Preset{{newPreset()}, {newPreset(withHandle([]byte{1, 2, 3, 4}))}} {
			if err := l.writePresets(t, presets); err != nil {
				outcomes = append(outcomes, err.Error())
			} else {
				outcomes = append(outcomes, "accepted")
			}
		}
		if len(outcomes) != 2 || !strings.Contains(outcomes[0], "application refuses these presets") ||
			!strings.Contains(outcomes[1], "does not exist in old Presets") {
			t.Errorf("outcomes %q", outcomes)
		}
		l.wantNoneStored(t)
	})

	t.Run("rejects more presets than the thermostat supports", func(t *testing.T) {
		l := newLocalThermostat(t, 2, []thermo.Preset{})
		wantStatusCode(t, l.writePresets(t, []thermo.Preset{newPreset(), newPreset(), newPreset()}), im.StatusResourceExhausted, "exceeds NumberOfPresets")
		l.wantNoneStored(t)
	})

	t.Run("rejects a named preset when the preset type does not support names", func(t *testing.T) {
		l := newLocalThermostat(t, 5, []thermo.Preset{})
		wantStatusCode(t, l.writePresets(t, []thermo.Preset{newPreset(withName("Home"))}), im.StatusConstraintError, "Preset names are not supported")
		l.wantNoneStored(t)
	})

	t.Run("accepts an unnamed preset, which carries no name", func(t *testing.T) {
		l := newLocalThermostat(t, 5, []thermo.Preset{})
		l.mustWrite(t, []thermo.Preset{newPreset()})
		if n := l.srv.Presets()[0].Name; n != nil {
			t.Errorf("name %+v, want none", n)
		}
	})

	t.Run("rejects two unnamed presets for one scenario", func(t *testing.T) {
		l := newLocalThermostat(t, 5, []thermo.Preset{})
		wantStatusCode(t, l.writePresets(t, []thermo.Preset{newPreset(), newPreset()}), im.StatusConstraintError, "Duplicate preset name")
		l.wantNoneStored(t)
	})

	t.Run("clears an activePresetHandle no preset carries", func(t *testing.T) {
		cfg := thermostatConfig(5, []thermo.Preset{})
		cfg.ActivePresetHandle = filled(16, 7)
		_, _, srv := onlineThermostat(t, cfg)
		if h := srv.ActivePresetHandle(); h != nil {
			t.Errorf("activePresetHandle %x, want null", h)
		}
		if v, _ := srv.MatterRead(0x004E); v != nil {
			t.Errorf("ActivePresetHandle reads %v, want null", v)
		}
	})

	t.Run("keeps an activePresetHandle a configured preset carries", func(t *testing.T) {
		configured := newPreset(withHandle(filled(16, 7)))
		cfg := thermostatConfig(5, []thermo.Preset{configured})
		cfg.ActivePresetHandle = configured.PresetHandle.Value
		_, _, srv := onlineThermostat(t, cfg)
		if h := srv.ActivePresetHandle(); !bytes.Equal(h, configured.PresetHandle.Value) {
			t.Errorf("activePresetHandle %x, want %x", h, configured.PresetHandle.Value)
		}
	})

	t.Run("refuses to remove the active preset", func(t *testing.T) {
		l := newLocalThermostat(t, 5, []thermo.Preset{})
		l.mustWrite(t, []thermo.Preset{newPreset()})
		handle := l.srv.Presets()[0].PresetHandle.Value
		if err := act(t, l.dev, func(ctx context.Context) error { return l.srv.SetActivePresetHandle(ctx, handle) }); err != nil {
			t.Fatal(err)
		}
		wantStatusCode(t, l.writePresets(t, []thermo.Preset{}), im.StatusInvalidInState, "while it is the active preset")
		if n := len(l.srv.Presets()); n != 1 {
			t.Errorf("%d stored presets, want 1", n)
		}
	})

	t.Run("refuses to remove a preset the application marked built-in", func(t *testing.T) {
		builtIn := newPreset(withHandle(filled(16, 7)), func(p *thermo.Preset) { p.BuiltIn.Value = true })
		l := newLocalThermostat(t, 5, []thermo.Preset{builtIn})
		wantStatusCode(t, l.writePresets(t, []thermo.Preset{}), im.StatusConstraintError, "Cannot remove built-in preset")
		stored := l.srv.Presets()
		if len(stored) != 1 || !bytes.Equal(stored[0].PresetHandle.Value, builtIn.PresetHandle.Value) || !stored[0].BuiltIn.Value {
			t.Errorf("stored presets %+v", stored)
		}
	})
}
