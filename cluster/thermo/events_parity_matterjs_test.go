// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package thermo_test

// The Events feature (TEVT): matter.js
// packages/node/test/behaviors/thermostat/ThermostatEventsTest.ts at
// b25ed960, ported by meaning — 46 cases (43 `it`, the SetpointChange loop
// running four), 44 of them here. Left: "generates nothing and does not
// fail when the events are not enabled" (by design: TEVT selects every
// event its conformance admits, there is no per-event enabling) and the
// describe "without the thermostat-events forward feature" (the forward
// gate is matter.js's release switch; here TEVT is served as the Ahead of
// matter.js register records it).
//
// matter.js's application writes (device.set) are the host's setters, a
// command context (node.online({command: true})) is a command path on the
// context, the node starting is the bridge's SetEndpoint (the state a
// test sets before it is the configuration), MockTime is the server's
// clock port (clocktest.Fake) and node.close is MatterQuiesce.

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/cluster/spec"
	tdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	"github.com/SukramJ/go-fabric/cluster/thermo"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

type emitted struct {
	endpoint uint16
	id       uint32
	data     any
}

// payloadRecorder keeps every event with its endpoint and payload.
type payloadRecorder struct {
	mu     sync.Mutex
	events []emitted
}

func (r *payloadRecorder) MatterEmitEvent(endpoint uint16, _, event uint32, data any, _ contract.EventPriority) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, emitted{endpoint, event, data})
}

// take returns the events so far and forgets them.
func (r *payloadRecorder) take() []emitted {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.events
	r.events = nil
	return out
}

// of returns the payloads of the events with id so far — matter.js's
// record(events.x) after a take.
func (r *payloadRecorder) of(id uint32) []any {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []any
	for _, e := range r.events {
		if e.id == id {
			out = append(out, e.data)
		}
	}
	return out
}

func tevtServer(t *testing.T, features uint32, edit ...func(*thermo.ThermostatConfig)) (*thermo.ThermostatServer, *payloadRecorder) {
	t.Helper()
	cfg := thermo.DefaultThermostatConfig()
	cfg.Features = features
	for _, e := range edit {
		e(&cfg)
	}
	srv := mustNew(t, cfg)
	rec := &payloadRecorder{}
	srv.SetMatterEventEmitter(rec)
	srv.SetEndpoint(3)
	return srv, rec
}

func wantIDs(t *testing.T, got []emitted, want ...uint32) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("events %+v, want ids %v", got, want)
	}
	for i := range want {
		if got[i].id != want[i] {
			t.Fatalf("event %d is 0x%02X, want 0x%02X (%+v)", i, got[i].id, want[i], got)
		}
	}
}

const (
	featHeatTEVT     = thermo.ThermostatFeatureHEAT | thermo.ThermostatFeatureTEVT
	featHeatCoolTEVT = thermo.ThermostatFeatureHEAT | thermo.ThermostatFeatureCOOL | thermo.ThermostatFeatureTEVT
	featAutoTEVT     = featHeatCoolTEVT | thermo.ThermostatFeatureAUTO
)

func isStatus(err error, code im.StatusCode) bool {
	var s interface{ MatterStatusCode() im.StatusCode }
	return errors.As(err, &s) && s.MatterStatusCode() == code
}

// eventsCase is one online thermostat of ThermostatEventsTest.ts: the
// server, the events it emitted since the test took them, and its clock.
type eventsCase struct {
	srv *thermo.ThermostatServer
	rec *payloadRecorder
	clk *clocktest.Fake
}

// onlineEvents is online(device) (ThermostatEventsTest.ts:132-135): the
// server of cfg on a fake clock, configured by before, then started —
// the emitter and the endpoint the bridge hands it.
func onlineEvents(t *testing.T, cfg thermo.ThermostatConfig, before ...func(*thermo.ThermostatServer)) *eventsCase {
	t.Helper()
	fake := clocktest.New(time.Unix(1_700_000_000, 0))
	return onlineEventsOn(t, cfg, fake, fake, before...)
}

func onlineEventsOn(t *testing.T, cfg thermo.ThermostatConfig, clk clock.Clock, fake *clocktest.Fake, before ...func(*thermo.ThermostatServer)) *eventsCase {
	t.Helper()
	cfg.Clock = clk
	srv := mustNew(t, cfg)
	for _, b := range before {
		b(srv)
	}
	rec := &payloadRecorder{}
	srv.SetMatterEventEmitter(rec)
	srv.SetEndpoint(1)
	t.Cleanup(srv.MatterQuiesce)
	return &eventsCase{srv: srv, rec: rec, clk: fake}
}

// record starts recording: what the start emitted is dropped.
func (c *eventsCase) record() { _ = c.rec.take() }

// setTemperature is the test's setTemperature: the application sets
// LocalTemperature.
func (c *eventsCase) setTemperature(t *testing.T, v *int16) {
	t.Helper()
	c.srv.SetLocalTemperature(v)
}

func temp(v int16) *int16 { return &v }

// localTemperatures are the LocalTemperatureChange payloads, nil for null.
func (c *eventsCase) localTemperatures() []*int16 {
	var out []*int16
	for _, d := range c.rec.of(tdef.EventLocalTemperatureChange) {
		e := d.(tdef.LocalTemperatureChangeEvent)
		if e.CurrentLocalTemperature.Null {
			out = append(out, nil)
			continue
		}
		out = append(out, temp(e.CurrentLocalTemperature.Value))
	}
	return out
}

func wantTemperatures(t *testing.T, c *eventsCase, want ...*int16) {
	t.Helper()
	got := c.localTemperatures()
	if !slices.EqualFunc(got, want, func(a, b *int16) bool { return (a == nil) == (b == nil) && (a == nil || *a == *b) }) {
		t.Fatalf("LocalTemperatureChange %s, want %s", temps(got), temps(want))
	}
}

func temps(vs []*int16) string {
	parts := make([]string, len(vs))
	for i, v := range vs {
		parts[i] = "null"
		if v != nil {
			parts[i] = strconv.Itoa(int(*v))
		}
	}
	return "[" + strings.Join(parts, " ") + "]"
}

// configBase is CONFIG (ThermostatEventsTest.ts:89-95):
// CoolingAndHeating and Auto (the features' here), the occupied setpoints
// 2000 and 2600, the deadband 25.
func configBase(features uint32, edit ...func(*thermo.ThermostatConfig)) thermo.ThermostatConfig {
	deadband := int8(25)
	cfg := thermo.DefaultThermostatConfig()
	cfg.Features = features
	cfg.MinSetpointDeadBand = &deadband
	for _, e := range edit {
		e(&cfg)
	}
	return cfg
}

// basicConfig is basicThermostat (:145-147): BasicServer, Heating,
// Cooling, AutoMode and Events (:51-61).
func basicConfig(edit ...func(*thermo.ThermostatConfig)) thermo.ThermostatConfig {
	return configBase(featAutoTEVT, edit...)
}

// fullConfig is fullThermostat (:137-143): FullServer (:33-41) — Heating,
// Cooling, Occupancy, AutoMode, Presets, MatterScheduleConfiguration and
// Events — with PRESETS_CONFIG (:97-121).
func fullConfig(edit ...func(*thermo.ThermostatConfig)) thermo.ThermostatConfig {
	return configBase(featAutoTEVT|thermo.ThermostatFeatureOCC|thermo.ThermostatFeaturePRES|thermo.ThermostatFeatureMSCH, func(cfg *thermo.ThermostatConfig) {
		cfg.NumberOfPresets = 5
		cfg.PresetTypes = []thermo.PresetType{
			{PresetScenario: tdef.PresetScenarioOccupied, NumberOfPresets: 5},
			{PresetScenario: tdef.PresetScenarioUnoccupied, NumberOfPresets: 5},
		}
		cfg.Presets = []thermo.Preset{}
		cfg.NumberOfSchedules, cfg.NumberOfScheduleTransitions = 5, 10
		cfg.ScheduleTypes = []thermo.ScheduleType{autoType(5, tdef.ScheduleTypeFeaturesSupportsPresets|tdef.ScheduleTypeFeaturesSupportsSetpoints)}
		cfg.Schedules = []thermo.Schedule{}
		for _, e := range edit {
			e(cfg)
		}
	})
}

// refusingSink refuses every change a controller asks for.
type refusingSink struct{}

func (refusingSink) SetSystemMode(context.Context, uint8) error {
	return errors.New("abandon the write")
}

func (refusingSink) SetSetpoint(context.Context, uint32, int16, []thermo.SetpointChange) error {
	return errors.New("abandon the write")
}

func (refusingSink) SetTemperatureSetpointHold(context.Context, uint8) error {
	return errors.New("abandon the write")
}

// earlyClock is the getTimer replacement of "waits for the end of the
// window when the timer expires early": the next timer armed after arm
// fires one millisecond early.
type earlyClock struct {
	*clocktest.Fake
	armed atomic.Bool
}

func (e *earlyClock) AfterFunc(d time.Duration, f func()) clock.Timer {
	if e.armed.CompareAndSwap(true, false) {
		d -= time.Millisecond
	}
	return e.Fake.AfterFunc(d, f)
}

// warnLog captures the warnings a server logs once capturing
// (captureLog(LogLevel.WARN), which the test starts after the node is
// online).
type warnLog struct {
	mu        sync.Mutex
	buf       bytes.Buffer
	capturing bool
}

func (w *warnLog) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.capturing {
		return len(p), nil
	}
	return w.buf.Write(p)
}

func (w *warnLog) capture() {
	w.mu.Lock()
	w.capturing = true
	w.mu.Unlock()
}

func (w *warnLog) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

func activatePreset(t *testing.T, srv *thermo.ThermostatServer, handle []byte) {
	t.Helper()
	ctx := im.WithCommandPath(context.Background(), im.ConcreteCommandPath{Endpoint: 1, Cluster: tdef.ClusterID, Command: tdef.CmdSetActivePresetRequest, HasEndpoint: true})
	h := spec.NullOf[[]byte]()
	if handle != nil {
		h = spec.ValueOf(handle)
	}
	if _, err := srv.MatterInvoke(ctx, tdef.CmdSetActivePresetRequest, tdef.SetActivePresetRequestRequest{PresetHandle: h}); err != nil {
		t.Fatal(err)
	}
}

func TestParityMatterJS_ThermostatEventsTest(t *testing.T) { //nolint:funlen,gocognit,maintidx // one port, describe by describe
	t.Parallel()

	t.Run("SystemModeChange", func(t *testing.T) {
		t.Parallel()
		t.Run("generates one event with previous and current mode", func(t *testing.T) {
			t.Parallel()
			c := onlineEvents(t, basicConfig())
			c.record()
			c.srv.SetSystemMode(uint8(tdef.SystemModeHeat))
			got := c.rec.of(tdef.EventSystemModeChange)
			if len(got) != 1 {
				t.Fatalf("SystemModeChange %+v, want one", got)
			}
			if e := got[0].(tdef.SystemModeChangeEvent); e.PreviousSystemMode == nil || *e.PreviousSystemMode != tdef.SystemModeAuto || e.CurrentSystemMode != tdef.SystemModeHeat {
				t.Errorf("event %+v, want Auto to Heat", e)
			}
		})

		t.Run("generates no event when the value does not change", func(t *testing.T) {
			t.Parallel()
			c := onlineEvents(t, basicConfig())
			c.record()
			c.srv.SetSystemMode(uint8(tdef.SystemModeAuto))
			if got := c.rec.of(tdef.EventSystemModeChange); len(got) != 0 {
				t.Fatalf("SystemModeChange %+v, want none", got)
			}
		})

		// The transaction that throws after the state write is a write the
		// Sink refuses: nothing is stored, nothing emitted.
		t.Run("generates no event when the transaction rolls back", func(t *testing.T) {
			t.Parallel()
			c := onlineEvents(t, basicConfig(func(cfg *thermo.ThermostatConfig) { cfg.Sink = refusingSink{} }))
			c.record()
			if err := c.srv.MatterWrite(context.Background(), tdef.AttrSystemMode, uint8(tdef.SystemModeHeat)); !isStatus(err, im.StatusFailure) {
				t.Fatalf("the refused write: %v, want FAILURE", err)
			}
			if got := c.rec.of(tdef.EventSystemModeChange); len(got) != 0 {
				t.Fatalf("SystemModeChange %+v, want none", got)
			}
			if v, _ := c.srv.MatterRead(tdef.AttrSystemMode); v != uint8(tdef.SystemModeAuto) {
				t.Errorf("SystemMode %v, want Auto", v)
			}
		})
	})

	t.Run("RunningModeChange and RunningStateChange", func(t *testing.T) {
		t.Parallel()
		t.Run("generates RunningModeChange with previous and current mode", func(t *testing.T) {
			t.Parallel()
			c := onlineEvents(t, basicConfig())
			c.record()
			for range 2 {
				if err := c.srv.SetThermostatRunningMode(uint8(tdef.ThermostatRunningModeHeat)); err != nil {
					t.Fatal(err)
				}
			}
			got := c.rec.of(tdef.EventRunningModeChange)
			if len(got) != 1 {
				t.Fatalf("RunningModeChange %+v, want one", got)
			}
			if e := got[0].(tdef.RunningModeChangeEvent); *e.PreviousRunningMode != tdef.ThermostatRunningModeOff || e.CurrentRunningMode != tdef.ThermostatRunningModeHeat {
				t.Errorf("event %+v, want Off to Heat", e)
			}
		})

		t.Run("generates RunningModeChange when a SystemMode change sets the running mode", func(t *testing.T) {
			t.Parallel()
			c := onlineEvents(t, basicConfig())
			c.record()
			if err := c.srv.SetAttributes(map[uint32]any{tdef.AttrSystemMode: uint8(tdef.SystemModeHeat)}); err != nil {
				t.Fatal(err)
			}
			got := c.rec.of(tdef.EventRunningModeChange)
			if len(got) != 1 {
				t.Fatalf("RunningModeChange %+v, want exactly one", got)
			}
			if e := got[0].(tdef.RunningModeChangeEvent); *e.PreviousRunningMode != tdef.ThermostatRunningModeOff || e.CurrentRunningMode != tdef.ThermostatRunningModeHeat {
				t.Errorf("event %+v, want Off to Heat", e)
			}
		})

		t.Run("generates RunningStateChange with previous and current state", func(t *testing.T) {
			t.Parallel()
			state := uint16(0)
			c := onlineEvents(t, basicConfig(func(cfg *thermo.ThermostatConfig) { cfg.ThermostatRunningState = &state }))
			c.record()
			c.srv.SetThermostatRunningState(uint16(tdef.RelayStateHeat))
			c.srv.SetThermostatRunningState(uint16(tdef.RelayStateHeat))
			got := c.rec.of(tdef.EventRunningStateChange)
			if len(got) != 1 {
				t.Fatalf("RunningStateChange %+v, want one", got)
			}
			if e := got[0].(tdef.RunningStateChangeEvent); *e.PreviousRunningState != 0 || e.CurrentRunningState != tdef.RelayStateHeat {
				t.Errorf("event %+v, want {} to {heat}", e)
			}
		})
	})

	t.Run("OccupancyChange", func(t *testing.T) {
		t.Parallel()
		t.Run("generates one event with previous and current occupancy", func(t *testing.T) {
			t.Parallel()
			occupied := true
			c := onlineEvents(t, fullConfig(func(cfg *thermo.ThermostatConfig) { cfg.Occupied = &occupied }))
			c.record()
			c.srv.SetOccupied(false)
			c.srv.SetOccupied(false)
			got := c.rec.of(tdef.EventOccupancyChange)
			if len(got) != 1 {
				t.Fatalf("OccupancyChange %+v, want one", got)
			}
			if e := got[0].(tdef.OccupancyChangeEvent); e.PreviousOccupancy == nil || *e.PreviousOccupancy != tdef.OccupancyOccupied || e.CurrentOccupancy != 0 {
				t.Errorf("event %+v, want occupied to not occupied", e)
			}
		})
	})

	t.Run("SetpointChange", func(t *testing.T) {
		t.Parallel()
		for _, tc := range []struct {
			attribute         string
			attrID            uint32
			systemMode        tdef.SystemModeEnum
			occupied          bool
			previous, current int16
		}{
			{"occupiedHeatingSetpoint", tdef.AttrOccupiedHeatingSetpoint, tdef.SystemModeHeat, true, 2000, 2100},
			{"unoccupiedHeatingSetpoint", tdef.AttrUnoccupiedHeatingSetpoint, tdef.SystemModeHeat, false, 1900, 1950},
			{"occupiedCoolingSetpoint", tdef.AttrOccupiedCoolingSetpoint, tdef.SystemModeCool, true, 2600, 2700},
			{"unoccupiedCoolingSetpoint", tdef.AttrUnoccupiedCoolingSetpoint, tdef.SystemModeCool, false, 2700, 2800},
		} {
			t.Run("reports "+tc.attribute+" with the Occupancy field", func(t *testing.T) {
				t.Parallel()
				c := onlineEvents(t, fullConfig(func(cfg *thermo.ThermostatConfig) {
					cfg.InitialUnoccupiedHeatingSetpoint, cfg.InitialUnoccupiedCoolingSetpoint = temp(1900), temp(2700)
				}))
				c.record()
				if err := c.srv.SetAttributes(map[uint32]any{tc.attrID: tc.current}); err != nil {
					t.Fatal(err)
				}
				got := c.rec.of(tdef.EventSetpointChange)
				if len(got) != 1 {
					t.Fatalf("SetpointChange %+v, want one", got)
				}
				want := tdef.OccupancyBitmap(0)
				if tc.occupied {
					want = tdef.OccupancyOccupied
				}
				e := got[0].(tdef.SetpointChangeEvent)
				if e.SystemMode != tc.systemMode || e.Occupancy == nil || *e.Occupancy != want || e.PreviousSetpoint == nil || *e.PreviousSetpoint != tc.previous || e.CurrentSetpoint != tc.current {
					t.Errorf("event %+v, want %v occupied=%v %d to %d", e, tc.systemMode, tc.occupied, tc.previous, tc.current)
				}
			})
		}

		t.Run("encodes the Occupancy field of an Unoccupied setpoint change as 0", func(t *testing.T) {
			t.Parallel()
			c := onlineEvents(t, fullConfig(func(cfg *thermo.ThermostatConfig) { cfg.InitialUnoccupiedHeatingSetpoint = temp(1900) }))
			c.record()
			if err := c.srv.SetAttributes(map[uint32]any{tdef.AttrUnoccupiedHeatingSetpoint: int16(1950)}); err != nil {
				t.Fatal(err)
			}
			got := c.rec.of(tdef.EventSetpointChange)
			if len(got) != 1 {
				t.Fatalf("SetpointChange %+v, want one", got)
			}
			enc := tlv.NewEncoder()
			got[0].(tdef.SetpointChangeEvent).EncodeTLV(enc, tlv.AnonymousTag())
			b, err := enc.Bytes()
			if err != nil {
				t.Fatal(err)
			}
			dec := tlv.NewDecoder(b)
			found := false
			for {
				el, err := dec.Next()
				if err != nil {
					break
				}
				if el.Tag == tlv.ContextTag(1) {
					found = true
					if el.Uint != 0 {
						t.Errorf("occupancy %d, want 0", el.Uint)
					}
				}
			}
			if !found {
				t.Error("the Occupancy field is missing")
			}
		})

		heatingOnly := func(cfg *thermo.ThermostatConfig) { cfg.InitialHeatingSetpoint = 2000 }
		t.Run("generates events on a Heating-only server", func(t *testing.T) {
			t.Parallel()
			c := onlineEvents(t, configBase(thermo.ThermostatFeatureHEAT|thermo.ThermostatFeatureTEVT, heatingOnly))
			c.record()
			if err := c.srv.SetAttributes(map[uint32]any{tdef.AttrOccupiedHeatingSetpoint: int16(2100)}); err != nil {
				t.Fatal(err)
			}
			got := c.rec.of(tdef.EventSetpointChange)
			if len(got) != 1 {
				t.Fatalf("SetpointChange %+v, want one", got)
			}
			if e := got[0].(tdef.SetpointChangeEvent); e.SystemMode != tdef.SystemModeHeat || e.Occupancy != nil || *e.PreviousSetpoint != 2000 || e.CurrentSetpoint != 2100 {
				t.Errorf("event %+v, want Heat, no occupancy, 2000 to 2100", e)
			}
		})

		t.Run("generates events on a Cooling-only server", func(t *testing.T) {
			t.Parallel()
			c := onlineEvents(t, configBase(thermo.ThermostatFeatureCOOL|thermo.ThermostatFeatureTEVT, func(cfg *thermo.ThermostatConfig) { cfg.InitialCoolingSetpoint = 2600 }))
			c.record()
			if err := c.srv.SetAttributes(map[uint32]any{tdef.AttrOccupiedCoolingSetpoint: int16(2500)}); err != nil {
				t.Fatal(err)
			}
			got := c.rec.of(tdef.EventSetpointChange)
			if len(got) != 1 {
				t.Fatalf("SetpointChange %+v, want one", got)
			}
			if e := got[0].(tdef.SetpointChangeEvent); e.SystemMode != tdef.SystemModeCool || e.Occupancy != nil || *e.PreviousSetpoint != 2600 || e.CurrentSetpoint != 2500 {
				t.Errorf("event %+v, want Cool, no occupancy, 2600 to 2500", e)
			}
		})

		t.Run("generates no event for an identical value", func(t *testing.T) {
			t.Parallel()
			c := onlineEvents(t, fullConfig())
			c.record()
			if err := c.srv.SetAttributes(map[uint32]any{tdef.AttrOccupiedHeatingSetpoint: int16(2000)}); err != nil {
				t.Fatal(err)
			}
			if got := c.rec.of(tdef.EventSetpointChange); len(got) != 0 {
				t.Fatalf("SetpointChange %+v, want none", got)
			}
		})

		t.Run("leaves out the Occupancy field without the Occupancy feature", func(t *testing.T) {
			t.Parallel()
			c := onlineEvents(t, basicConfig())
			c.record()
			if err := c.srv.SetAttributes(map[uint32]any{tdef.AttrOccupiedHeatingSetpoint: int16(2100)}); err != nil {
				t.Fatal(err)
			}
			got := c.rec.of(tdef.EventSetpointChange)
			if len(got) != 1 {
				t.Fatalf("SetpointChange %+v, want one", got)
			}
			if e := got[0].(tdef.SetpointChangeEvent); e.SystemMode != tdef.SystemModeHeat || e.Occupancy != nil || e.CurrentSetpoint != 2100 {
				t.Errorf("event %+v, want Heat, no occupancy, 2100", e)
			}
		})

		t.Run("generates one event per setpoint changed by SetpointRaiseLower, including one pushed by the deadband", func(t *testing.T) {
			t.Parallel()
			c := onlineEvents(t, basicConfig(func(cfg *thermo.ThermostatConfig) {
				cfg.InitialHeatingSetpoint, cfg.InitialCoolingSetpoint = 2000, 2025
			}))
			c.record()
			ctx := im.WithCommandPath(context.Background(), im.ConcreteCommandPath{Endpoint: 1, Cluster: tdef.ClusterID, Command: tdef.CmdSetpointRaiseLower, HasEndpoint: true})
			if _, err := c.srv.MatterInvoke(ctx, tdef.CmdSetpointRaiseLower, tdef.SetpointRaiseLowerRequest{Mode: tdef.SetpointRaiseLowerModeHeat, Amount: 60}); err != nil {
				t.Fatal(err)
			}
			got := make([][2]int, 0, 2)
			for _, d := range c.rec.of(tdef.EventSetpointChange) {
				e := d.(tdef.SetpointChangeEvent)
				got = append(got, [2]int{int(e.SystemMode), int(e.CurrentSetpoint)})
			}
			want := [][2]int{{int(tdef.SystemModeCool), 2850}, {int(tdef.SystemModeHeat), 2600}}
			if !slices.Equal(got, want) {
				t.Fatalf("SetpointChange %v, want %v", got, want)
			}
		})
	})

	t.Run("ActivePresetChange", func(t *testing.T) {
		t.Parallel()
		withPresets := func(t *testing.T) (*eventsCase, [][]byte) {
			t.Helper()
			heat, cool := int16(2100), int16(2500)
			c := onlineEvents(t, fullConfig(func(cfg *thermo.ThermostatConfig) {
				cfg.Presets = []thermo.Preset{
					newPreset(withHandle(filled(16, 1))),
					newPreset(withHandle(filled(16, 2)), func(p *thermo.Preset) {
						p.PresetScenario = tdef.PresetScenarioUnoccupied
						p.HeatingSetpoint, p.CoolingSetpoint = &heat, &cool
					}),
				}
			}))
			presets := c.srv.Presets()
			handles := make([][]byte, 0, len(presets))
			for _, p := range presets {
				handles = append(handles, p.PresetHandle.Value)
			}
			return c, handles
		}
		presetEvents := func(c *eventsCase) []tdef.ActivePresetChangeEvent {
			recorded := c.rec.of(tdef.EventActivePresetChange)
			out := make([]tdef.ActivePresetChangeEvent, 0, len(recorded))
			for _, d := range recorded {
				out = append(out, d.(tdef.ActivePresetChangeEvent))
			}
			return out
		}

		t.Run("generates one event per change of the active preset", func(t *testing.T) {
			t.Parallel()
			c, handles := withPresets(t)
			c.record()
			activatePreset(t, c.srv, handles[0])
			activatePreset(t, c.srv, handles[1])
			got := presetEvents(c)
			if len(got) != 2 {
				t.Fatalf("ActivePresetChange %+v, want two", got)
			}
			if !got[0].PreviousPresetHandle.Null || !bytes.Equal(got[0].CurrentPresetHandle.Value, handles[0]) ||
				!bytes.Equal(got[1].PreviousPresetHandle.Value, handles[0]) || !bytes.Equal(got[1].CurrentPresetHandle.Value, handles[1]) {
				t.Errorf("events %+v, want null to h0, h0 to h1", got)
			}
		})

		t.Run("generates one event plus the SetpointChange events when the preset changes the setpoints", func(t *testing.T) {
			t.Parallel()
			c, handles := withPresets(t)
			c.record()
			activatePreset(t, c.srv, handles[1])
			got := presetEvents(c)
			if len(got) != 1 || !got[0].PreviousPresetHandle.Null || !bytes.Equal(got[0].CurrentPresetHandle.Value, handles[1]) {
				t.Fatalf("ActivePresetChange %+v, want one from null to h1", got)
			}
			recorded := c.rec.of(tdef.EventSetpointChange)
			setpoints := make([][3]int, 0, len(recorded))
			for _, d := range recorded {
				e := d.(tdef.SetpointChangeEvent)
				setpoints = append(setpoints, [3]int{int(e.SystemMode), int(*e.PreviousSetpoint), int(e.CurrentSetpoint)})
			}
			slices.SortFunc(setpoints, func(a, b [3]int) int { return a[0] - b[0] })
			want := [][3]int{{int(tdef.SystemModeCool), 2600, 2500}, {int(tdef.SystemModeHeat), 2000, 2100}}
			if !slices.Equal(setpoints, want) {
				t.Errorf("SetpointChange %v, want the members %v", setpoints, want)
			}
		})

		t.Run("generates no event when the handle stays", func(t *testing.T) {
			t.Parallel()
			c, handles := withPresets(t)
			activatePreset(t, c.srv, handles[0])
			c.record()
			activatePreset(t, c.srv, handles[0])
			if got := presetEvents(c); len(got) != 0 {
				t.Fatalf("ActivePresetChange %+v, want none", got)
			}
		})

		// The recorder sees every event from the start, as the event log
		// matter.js reads (OccurrenceManager) holds them.
		t.Run("generates an event when initialization clears a handle that matches no preset", func(t *testing.T) {
			t.Parallel()
			stale := filled(16, 9)
			c := onlineEvents(t, fullConfig(func(cfg *thermo.ThermostatConfig) {
				cfg.Presets = []thermo.Preset{newPreset(withHandle(filled(16, 1)))}
				cfg.ActivePresetHandle = stale
			}))
			if h := c.srv.ActivePresetHandle(); h != nil {
				t.Fatalf("ActivePresetHandle %x, want null", h)
			}
			got := presetEvents(c)
			if len(got) != 1 || got[0].PreviousPresetHandle == nil || !bytes.Equal(got[0].PreviousPresetHandle.Value, stale) || !got[0].CurrentPresetHandle.Null {
				t.Fatalf("ActivePresetChange %+v, want one from the stale handle to null", got)
			}
			if ev := c.rec.take(); ev[len(ev)-1].endpoint != 1 {
				t.Errorf("start-up event on endpoint %d, want the stamped 1", ev[len(ev)-1].endpoint)
			}
		})

		t.Run("generates an event when the application sets the handle", func(t *testing.T) {
			t.Parallel()
			c, handles := withPresets(t)
			c.record()
			if err := c.srv.SetActivePresetHandle(context.Background(), handles[1]); err != nil {
				t.Fatal(err)
			}
			got := presetEvents(c)
			if len(got) != 1 || !bytes.Equal(got[0].CurrentPresetHandle.Value, handles[1]) {
				t.Fatalf("ActivePresetChange %+v, want one to h1", got)
			}
		})
	})

	t.Run("ActiveScheduleChange", func(t *testing.T) {
		t.Parallel()
		t.Run("generates one event per change of the active schedule", func(t *testing.T) {
			t.Parallel()
			c := onlineEvents(t, fullConfig())
			if err := c.srv.SetSchedules([]thermo.Schedule{newSchedule()}); err != nil {
				t.Fatal(err)
			}
			handle := c.srv.Schedules()[0].ScheduleHandle.Value
			c.record()
			for range 2 {
				if err := c.srv.SetActiveScheduleHandle(handle); err != nil {
					t.Fatal(err)
				}
			}
			got := c.rec.of(tdef.EventActiveScheduleChange)
			if len(got) != 1 {
				t.Fatalf("ActiveScheduleChange %+v, want one", got)
			}
			if e := got[0].(tdef.ActiveScheduleChangeEvent); e.PreviousScheduleHandle == nil || !e.PreviousScheduleHandle.Null || !bytes.Equal(e.CurrentScheduleHandle.Value, handle) {
				t.Errorf("event %+v, want null to the handle", e)
			}
		})
	})

	t.Run("LocalTemperatureChange", func(t *testing.T) {
		t.Parallel()
		// The host configures no measurement here (matter.js's
		// externalMeasuredIndoorTemperature); its first report of the sensor
		// after the start is the measurement.
		t.Run("reports the first measurement at startup", func(t *testing.T) {
			t.Parallel()
			c := onlineEvents(t, basicConfig())
			c.record()
			c.srv.SetMeasuredTemperature(temp(2000))
			wantTemperatures(t, c, temp(2000))
		})

		// NaN and +Inf have no int16 form.
		t.Run("refuses to start with a threshold that is not greater than 0", func(t *testing.T) {
			t.Parallel()
			for _, threshold := range []int16{0, -1} {
				_, err := thermo.New(basicConfig(func(cfg *thermo.ThermostatConfig) { cfg.LocalTemperatureChangeThreshold = &threshold }))
				if err == nil || !strings.Contains(err.Error(), "is "+strconv.Itoa(int(threshold))+" but must be a finite number greater than 0") {
					t.Errorf("threshold %d: %v, want the refusal", threshold, err)
				}
			}
		})

		t.Run("refuses a threshold that is not greater than 0 when it is set", func(t *testing.T) {
			t.Parallel()
			c := onlineEvents(t, basicConfig())
			for _, threshold := range []int16{0, -1} {
				err := c.srv.SetLocalTemperatureChangeThreshold(threshold)
				if err == nil || !strings.Contains(err.Error(), "is "+strconv.Itoa(int(threshold))+" but must be a finite number greater than 0") {
					t.Errorf("threshold %d: %v, want the refusal", threshold, err)
				}
			}
			if got := thermo.LocalTemperatureChangeThresholdOf(c.srv); got != 50 {
				t.Fatalf("threshold %d after the refusals, want 50", got)
			}
			if err := c.srv.SetLocalTemperatureChangeThreshold(200); err != nil {
				t.Fatal(err)
			}
			if got := thermo.LocalTemperatureChangeThresholdOf(c.srv); got != 200 {
				t.Fatalf("threshold %d, want 200", got)
			}
		})

		t.Run("reports the change from null to a value", func(t *testing.T) {
			t.Parallel()
			c := onlineEvents(t, basicConfig())
			c.record()
			c.setTemperature(t, temp(2000))
			wantTemperatures(t, c, temp(2000))
		})

		t.Run("reports a change of at least the threshold and no smaller one", func(t *testing.T) {
			t.Parallel()
			c := onlineEvents(t, basicConfig())
			c.setTemperature(t, temp(2000))
			c.clk.Advance(61 * time.Second)
			c.record()
			c.setTemperature(t, temp(2049))
			c.clk.Advance(61 * time.Second)
			wantTemperatures(t, c)
			c.setTemperature(t, temp(2050))
			wantTemperatures(t, c, temp(2050))
		})

		t.Run("measures the change against the last reported value", func(t *testing.T) {
			t.Parallel()
			c := onlineEvents(t, basicConfig())
			c.setTemperature(t, temp(2000))
			c.clk.Advance(61 * time.Second)
			c.record()
			c.setTemperature(t, temp(2030))
			c.setTemperature(t, temp(2060))
			wantTemperatures(t, c, temp(2060))
		})

		t.Run("uses the configured threshold", func(t *testing.T) {
			t.Parallel()
			threshold := int16(200)
			c := onlineEvents(t, basicConfig(func(cfg *thermo.ThermostatConfig) { cfg.LocalTemperatureChangeThreshold = &threshold }))
			c.setTemperature(t, temp(2000))
			c.clk.Advance(61 * time.Second)
			c.record()
			c.setTemperature(t, temp(2150))
			wantTemperatures(t, c)
			c.setTemperature(t, temp(2200))
			wantTemperatures(t, c, temp(2200))
		})

		configured := func(srv *thermo.ThermostatServer) {
			if err := srv.SetAttributes(map[uint32]any{tdef.AttrLocalTemperature: int16(2000)}); err != nil {
				panic(err)
			}
		}
		t.Run("treats the configured temperature as already reported", func(t *testing.T) {
			t.Parallel()
			c := onlineEvents(t, basicConfig(), configured)
			c.record()
			c.setTemperature(t, temp(2020))
			wantTemperatures(t, c)
		})

		t.Run("reports the change from a value to null", func(t *testing.T) {
			t.Parallel()
			c := onlineEvents(t, basicConfig())
			c.setTemperature(t, temp(2000))
			c.clk.Advance(61 * time.Second)
			c.record()
			c.setTemperature(t, nil)
			wantTemperatures(t, c, nil)
		})

		t.Run("reports the changes from and to null regardless of the threshold", func(t *testing.T) {
			t.Parallel()
			threshold := int16(10000)
			c := onlineEvents(t, basicConfig(func(cfg *thermo.ThermostatConfig) { cfg.LocalTemperatureChangeThreshold = &threshold }))
			c.record()
			c.setTemperature(t, temp(10))
			c.clk.Advance(61 * time.Second)
			c.setTemperature(t, nil)
			wantTemperatures(t, c, temp(10), nil)
		})

		t.Run("reports no change from a reported null to null", func(t *testing.T) {
			t.Parallel()
			c := onlineEvents(t, basicConfig(), configured)
			c.record()
			c.setTemperature(t, nil)
			c.clk.Advance(10 * time.Second)
			c.setTemperature(t, temp(2000))
			c.setTemperature(t, nil)
			c.clk.Advance(120 * time.Second)
			wantTemperatures(t, c, nil)
		})

		t.Run("reports a second significant change after the 60 second window with the value of that time", func(t *testing.T) {
			t.Parallel()
			c := onlineEvents(t, basicConfig())
			c.record()
			c.setTemperature(t, temp(2000))
			c.clk.Advance(10 * time.Second)
			c.setTemperature(t, temp(2100))
			wantTemperatures(t, c, temp(2000))
			c.clk.Advance(10 * time.Second)
			c.setTemperature(t, temp(2200))
			wantTemperatures(t, c, temp(2000))
			if n := c.clk.Pending(); n != 1 {
				t.Fatalf("%d timers, want the one report timer", n)
			}
			c.clk.Advance(39 * time.Second)
			wantTemperatures(t, c, temp(2000))
			c.clk.Advance(time.Second)
			wantTemperatures(t, c, temp(2000), temp(2200))
		})

		// The timer fires a millisecond early here (the replaced getTimer of
		// matter.js): the expiry re-decides on the value then and re-arms
		// for what is left of the window.
		t.Run("waits for the end of the window when the timer expires early", func(t *testing.T) {
			t.Parallel()
			early := &earlyClock{Fake: clocktest.New(time.Unix(1_700_000_000, 0))}
			c := onlineEventsOn(t, basicConfig(), early, early.Fake)
			c.record()
			c.setTemperature(t, temp(2000))
			c.clk.Advance(10 * time.Second)
			early.armed.Store(true)
			c.setTemperature(t, temp(2100))
			c.clk.Advance(50*time.Second - time.Millisecond)
			wantTemperatures(t, c, temp(2000))
			if n := c.clk.Pending(); n != 1 {
				t.Fatalf("%d timers after the early expiry, want the re-armed report timer", n)
			}
			c.clk.Advance(time.Millisecond)
			wantTemperatures(t, c, temp(2000), temp(2100))
		})

		t.Run("reports at once when exactly 60 seconds passed since the last report", func(t *testing.T) {
			t.Parallel()
			c := onlineEvents(t, basicConfig())
			c.record()
			c.setTemperature(t, temp(2000))
			c.clk.Advance(60 * time.Second)
			c.setTemperature(t, temp(2100))
			wantTemperatures(t, c, temp(2000), temp(2100))
		})

		t.Run("drops the deferred report when a raised threshold makes the change not significant", func(t *testing.T) {
			t.Parallel()
			c := onlineEvents(t, basicConfig())
			c.record()
			c.setTemperature(t, temp(2000))
			c.clk.Advance(10 * time.Second)
			c.setTemperature(t, temp(2100))
			if err := c.srv.SetLocalTemperatureChangeThreshold(200); err != nil {
				t.Fatal(err)
			}
			c.clk.Advance(120 * time.Second)
			wantTemperatures(t, c, temp(2000))
		})

		t.Run("drops the deferred report when the value moves back inside the window", func(t *testing.T) {
			t.Parallel()
			c := onlineEvents(t, basicConfig())
			c.record()
			c.setTemperature(t, temp(2000))
			c.clk.Advance(10 * time.Second)
			c.setTemperature(t, temp(2100))
			c.setTemperature(t, temp(2000))
			if n := c.clk.Pending(); n != 0 {
				t.Fatalf("%d timers, want none", n)
			}
			c.clk.Advance(120 * time.Second)
			wantTemperatures(t, c, temp(2000))
		})

		t.Run("defers the change to null until the window ends", func(t *testing.T) {
			t.Parallel()
			c := onlineEvents(t, basicConfig())
			c.record()
			c.setTemperature(t, temp(2000))
			c.clk.Advance(10 * time.Second)
			c.setTemperature(t, nil)
			wantTemperatures(t, c, temp(2000))
			c.clk.Advance(50 * time.Second)
			wantTemperatures(t, c, temp(2000), nil)
		})

		t.Run("generates no event after the node closed", func(t *testing.T) {
			t.Parallel()
			c := onlineEvents(t, basicConfig())
			c.record()
			c.setTemperature(t, temp(2000))
			c.clk.Advance(10 * time.Second)
			c.setTemperature(t, temp(2100))
			if n := c.clk.Pending(); n == 0 {
				t.Fatal("no report timer pending")
			}
			c.srv.MatterQuiesce()
			if n := c.clk.Pending(); n != 0 {
				t.Fatalf("%d timers running after the close, want none", n)
			}
			c.clk.Advance(120 * time.Second)
			wantTemperatures(t, c, temp(2000))
		})
	})

	t.Run("event availability", func(t *testing.T) {
		t.Parallel()
		t.Run("has no events for features that are not selected", func(t *testing.T) {
			t.Parallel()
			c := onlineEvents(t, basicConfig())
			events := c.srv.MatterEvents()
			for _, id := range []uint32{tdef.EventSystemModeChange, tdef.EventLocalTemperatureChange, tdef.EventSetpointChange, tdef.EventRunningStateChange, tdef.EventRunningModeChange} {
				if !slices.Contains(events, id) {
					t.Errorf("EventList %v lacks 0x%02X", events, id)
				}
			}
			for _, id := range []uint32{tdef.EventOccupancyChange, tdef.EventActivePresetChange, tdef.EventActiveScheduleChange} {
				if slices.Contains(events, id) {
					t.Errorf("EventList %v has 0x%02X", events, id)
				}
			}
		})

		t.Run("has no LocalTemperatureChange with LocalTemperatureNotExposed", func(t *testing.T) {
			t.Parallel()
			c := onlineEvents(t, configBase(featAutoTEVT|thermo.ThermostatFeatureLTNE))
			events := c.srv.MatterEvents()
			if !slices.Contains(events, tdef.EventSystemModeChange) || slices.Contains(events, tdef.EventLocalTemperatureChange) {
				t.Errorf("EventList %v, want SystemModeChange without LocalTemperatureChange", events)
			}
		})

		t.Run("generates nothing and does not fail without the Events feature", func(t *testing.T) {
			t.Parallel()
			log := &warnLog{}
			c := onlineEvents(t, configBase(thermo.ThermostatFeatureHEAT|thermo.ThermostatFeatureCOOL|thermo.ThermostatFeatureAUTO, func(cfg *thermo.ThermostatConfig) {
				cfg.Logger = slog.New(slog.NewTextHandler(log, &slog.HandlerOptions{Level: slog.LevelWarn}))
			}))
			c.record()
			log.capture()
			if err := c.srv.SetAttributes(map[uint32]any{
				tdef.AttrSystemMode: uint8(tdef.SystemModeHeat), tdef.AttrOccupiedHeatingSetpoint: int16(2100), tdef.AttrLocalTemperature: int16(2000),
			}); err != nil {
				t.Fatal(err)
			}
			c.clk.Advance(120 * time.Second)
			if events := c.srv.MatterEvents(); len(events) != 0 {
				t.Errorf("EventList %v, want none", events)
			}
			wantIDs(t, c.rec.take())
			if v, _ := c.srv.MatterRead(tdef.AttrOccupiedHeatingSetpoint); v != int16(2100) {
				t.Errorf("OccupiedHeatingSetpoint %v, want 2100", v)
			}
			if s := log.String(); s != "" {
				t.Errorf("logged %q, want nothing", s)
			}
		})
	})
}
