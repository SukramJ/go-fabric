// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// The port of matter.js packages/node/test/behaviors/thermostat/schedule-helpers.ts
// at db91886c and the fabrictest scaffolding the three Schedules ports
// share. matter.js serves MatterScheduleConfiguration without a forward
// gate since #5044, and the device layer without an extension since the
// lift to db91886c; thermo.New never had a gate.
package thermo_test

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/client/interaction"
	"thingfabric.dev/go-fabric/cluster/spec"
	tdef "thingfabric.dev/go-fabric/cluster/spec/thermostat"
	"thingfabric.dev/go-fabric/cluster/thermo"
	"thingfabric.dev/go-fabric/fabrictest"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/internal/fabrictestseam"
	"thingfabric.dev/go-fabric/tlv"
)

// schedulesAttribute is schedule-helpers.ts:24 SCHEDULES_ATTRIBUTE.
const schedulesAttribute = tdef.AttrSchedules

// newScheduleTransition is schedule-helpers.ts:30-49: Monday at 06:00,
// heating 20.00 °C and cooling 26.00 °C — no setpoints when edit gives the
// transition a preset handle (the defaults apply only off the preset path).
func newScheduleTransition(edit ...func(*thermo.ScheduleTransition)) thermo.ScheduleTransition {
	tr := thermo.ScheduleTransition{DayOfWeek: tdef.ScheduleDayOfWeekMonday, TransitionTime: 360}
	for _, e := range edit {
		e(&tr)
	}
	if tr.PresetHandle != nil {
		return tr
	}
	heat, cool := int16(2000), int16(2600)
	out := thermo.ScheduleTransition{DayOfWeek: tdef.ScheduleDayOfWeekMonday, TransitionTime: 360, HeatingSetpoint: &heat, CoolingSetpoint: &cool}
	for _, e := range edit {
		e(&out)
	}
	return out
}

// newSchedule is schedule-helpers.ts:55-63: a new Auto schedule — no
// handle, one transition, not built in.
func newSchedule(edit ...func(*thermo.Schedule)) thermo.Schedule {
	sc := thermo.Schedule{
		ScheduleHandle: spec.NullOf[[]byte](),
		SystemMode:     tdef.SystemModeAuto,
		Transitions:    []thermo.ScheduleTransition{newScheduleTransition()},
		BuiltIn:        spec.ValueOf(false),
	}
	for _, e := range edit {
		e(&sc)
	}
	return sc
}

// The transition and schedule edits the cases use.

func trDay(d tdef.ScheduleDayOfWeekBitmap) func(*thermo.ScheduleTransition) {
	return func(tr *thermo.ScheduleTransition) { tr.DayOfWeek = d }
}

func trTime(minute uint16) func(*thermo.ScheduleTransition) {
	return func(tr *thermo.ScheduleTransition) { tr.TransitionTime = minute }
}

func trMode(m tdef.SystemModeEnum) func(*thermo.ScheduleTransition) {
	return func(tr *thermo.ScheduleTransition) { tr.SystemMode = &m }
}

func trPreset(h []byte) func(*thermo.ScheduleTransition) {
	return func(tr *thermo.ScheduleTransition) { tr.PresetHandle = &h }
}

func trHeat(v int16) func(*thermo.ScheduleTransition) {
	return func(tr *thermo.ScheduleTransition) { tr.HeatingSetpoint = &v }
}

func trCool(v int16) func(*thermo.ScheduleTransition) {
	return func(tr *thermo.ScheduleTransition) { tr.CoolingSetpoint = &v }
}

// trNoHeat and trNoCool are `heatingSetpoint: undefined` / `coolingSetpoint: undefined`.
func trNoHeat(tr *thermo.ScheduleTransition) { tr.HeatingSetpoint = nil }
func trNoCool(tr *thermo.ScheduleTransition) { tr.CoolingSetpoint = nil }

func scHandle(h []byte) func(*thermo.Schedule) {
	return func(sc *thermo.Schedule) { sc.ScheduleHandle = spec.ValueOf(h) }
}

func scBuiltIn(b bool) func(*thermo.Schedule) {
	return func(sc *thermo.Schedule) { sc.BuiltIn = spec.ValueOf(b) }
}

func scMode(m tdef.SystemModeEnum) func(*thermo.Schedule) {
	return func(sc *thermo.Schedule) { sc.SystemMode = m }
}

func scName(n string) func(*thermo.Schedule) {
	return func(sc *thermo.Schedule) { sc.Name = &n }
}

func scPreset(h []byte) func(*thermo.Schedule) {
	return func(sc *thermo.Schedule) { sc.PresetHandle = &h }
}

func scTransitions(trs ...thermo.ScheduleTransition) func(*thermo.Schedule) {
	return func(sc *thermo.Schedule) { sc.Transitions = trs }
}

// schedulesConfig is schedule-helpers.ts:65-98 thermostatConfig on
// SchedulesThermostatServer (:12-18: Heating, Cooling, AutoMode, Presets,
// MatterScheduleConfiguration): preset-helpers' thermostat (the occupied
// setpoints, MinSetpointDeadBand 2.5 °C, five Occupied presets, none
// stored) with numberOfSchedules schedules of one Auto type supporting
// presets and setpoints (no names, no Off), ten transitions per schedule,
// no per-day limit, no active schedule, and schedules configured.
func schedulesConfig(numberOfSchedules uint8, schedules []thermo.Schedule) thermo.ThermostatConfig {
	cfg := thermostatConfig(5, []thermo.Preset{})
	cfg.Features |= thermo.ThermostatFeatureMSCH
	cfg.NumberOfSchedules = numberOfSchedules
	cfg.NumberOfScheduleTransitions = 10
	cfg.NumberOfScheduleTransitionPerDay = nil
	cfg.ScheduleTypes = []thermo.ScheduleType{{
		SystemMode: tdef.SystemModeAuto, NumberOfSchedules: numberOfSchedules,
		ScheduleTypeFeatures: tdef.ScheduleTypeFeaturesSupportsPresets | tdef.ScheduleTypeFeaturesSupportsSetpoints,
	}}
	cfg.Schedules = schedules
	return cfg
}

// withScheduleTypes replaces the schedule types.
func withScheduleTypes(cfg thermo.ThermostatConfig, types ...thermo.ScheduleType) thermo.ThermostatConfig {
	cfg.ScheduleTypes = types
	return cfg
}

// autoType is the Auto schedule type of n schedules with feats.
func autoType(n uint8, feats tdef.ScheduleTypeFeaturesBitmap) thermo.ScheduleType {
	return thermo.ScheduleType{SystemMode: tdef.SystemModeAuto, NumberOfSchedules: n, ScheduleTypeFeatures: feats}
}

// heatingOnlySchedulesConfig is schedule-helpers.ts:116-137: Heating,
// Presets and MatterScheduleConfiguration without Cooling and AutoMode,
// HeatingOnly (derived from the features), one Heat schedule type
// supporting setpoints, and presets as well when presets is given.
func heatingOnlySchedulesConfig(presets []thermo.Preset) thermo.ThermostatConfig {
	cfg := schedulesConfig(5, nil)
	cfg.Features = thermo.ThermostatFeatureHEAT | thermo.ThermostatFeaturePRES | thermo.ThermostatFeatureMSCH
	cfg.MinSetpointDeadBand = nil
	feats := tdef.ScheduleTypeFeaturesSupportsSetpoints
	if presets != nil {
		cfg.Presets = presets
		feats |= tdef.ScheduleTypeFeaturesSupportsPresets
	}
	cfg.ScheduleTypes = []thermo.ScheduleType{{SystemMode: tdef.SystemModeHeat, NumberOfSchedules: 5, ScheduleTypeFeatures: feats}}
	return cfg
}

// newSchedulesServer is a thermostat of cfg on a plain clock: the
// MockServerNode of the local-write and configuration ports.
func newSchedulesServer(t *testing.T, cfg thermo.ThermostatConfig) *thermo.ThermostatServer {
	t.Helper()
	srv, err := thermo.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

// schedulesThermostatPair commissions a pair whose device serves the
// thermostat of cfg — MockSite.addCommissionedPair with the endpoint.
func schedulesThermostatPair(t *testing.T, cfg thermo.ThermostatConfig) *thermostatPair {
	t.Helper()
	return commissionedThermostat(t, func(*fabrictest.Site) thermo.ThermostatConfig { return cfg })
}

// writeSchedulesUnchecked is SchedulesAtomicWriteTest.ts:67-81: the
// controller's whole-list write of Schedules from q — REPLACE-ALL and one
// append per schedule — and the status of each write request.
func (p *thermostatPair) writeSchedulesUnchecked(t *testing.T, q *fabrictest.Pair, schedules []thermo.Schedule) []im.StatusCode {
	t.Helper()
	ctx := parityCtx(t)
	ia, err := fabrictestseam.Connect(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	var statuses interaction.WriteStatuses
	err = p.site.Run(ctx, func(ctx context.Context) error {
		var werr error
		statuses, werr = ia.Write(ctx, []interaction.WriteRequest{{Path: p.attrPath(schedulesAttribute), Value: spec.List[thermo.Schedule](schedules)}}, interaction.WriteOptions{})
		return werr
	})
	if err != nil {
		t.Fatal(err)
	}
	out := make([]im.StatusCode, len(statuses))
	for i, s := range statuses {
		out[i] = s.Status
	}
	return out
}

// readSchedules reads Schedules as q (cachedSchedules, read fresh).
func (p *thermostatPair) readSchedules(t *testing.T, q *fabrictest.Pair) []thermo.Schedule {
	t.Helper()
	d, err := fabrictest.Read(parityCtx(t), q, p.attrPath(schedulesAttribute))
	if err != nil {
		t.Fatal(err)
	}
	v, err := tdef.Definition.Attribute(schedulesAttribute).Decode(d.Value)
	if err != nil {
		t.Fatal(err)
	}
	return []thermo.Schedule(v.(spec.List[thermo.Schedule]))
}

// atomic invokes AtomicRequest of attrs from q and decodes the answer;
// BeginWrite carries a timeout of 5000 ms.
func (p *thermostatPair) atomic(t *testing.T, q *fabrictest.Pair, requestType uint8, attrs ...uint32) tdef.AtomicResponse {
	t.Helper()
	var timeout *uint16
	if requestType == beginWrite {
		timeout = timeoutMs(5000)
	}
	r, err := p.atomicRequest(parityCtx(t), q, requestType, attrs, timeout)
	if err != nil {
		t.Fatal(err)
	}
	return decodeAtomicResponse(t, r)
}

// scheduleReports is the subscribed controller's cache of Schedules: every
// value it received, in order.
type scheduleReports struct {
	mu      sync.Mutex
	reports [][]thermo.Schedule
}

func (r *scheduleReports) last() ([]thermo.Schedule, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.reports) == 0 {
		return nil, false
	}
	return r.reports[len(r.reports)-1], true
}

// subscribeSchedules subscribes to Schedules from the pair's controller.
func (p *thermostatPair) subscribeSchedules(ctx context.Context, t *testing.T) *scheduleReports {
	t.Helper()
	r := &scheduleReports{}
	floor, ceiling := time.Duration(0), 60*time.Second
	_, err := fabrictest.Subscribe(ctx, p.pair, interaction.SubscribeRequest{
		Attributes:         []im.ConcreteAttributePath{p.attrPath(schedulesAttribute)},
		MinIntervalFloor:   &floor,
		MaxIntervalCeiling: &ceiling,
		Updated: func(rep *interaction.Report) error {
			for i := range rep.Attributes {
				if rep.Attributes[i].Path.Attribute != schedulesAttribute {
					continue
				}
				if v, err := tdef.Definition.Attribute(schedulesAttribute).Decode(rep.Attributes[i].Value); err == nil {
					r.mu.Lock()
					r.reports = append(r.reports, []thermo.Schedule(v.(spec.List[thermo.Schedule])))
					r.mu.Unlock()
				}
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// writeSchedulesPaths writes schedules from the pair's controller and
// returns the paths of the device's answer, one per write request.
func (p *thermostatPair) writeSchedulesPaths(t *testing.T, schedules []thermo.Schedule) []im.ConcreteAttributePath {
	t.Helper()
	ctx := parityCtx(t)
	ia, err := fabrictestseam.Connect(ctx, p.pair)
	if err != nil {
		t.Fatal(err)
	}
	var statuses interaction.WriteStatuses
	err = p.site.Run(ctx, func(ctx context.Context) error {
		var werr error
		statuses, werr = ia.Write(ctx, []interaction.WriteRequest{{Path: p.attrPath(schedulesAttribute), Value: spec.List[thermo.Schedule](schedules)}}, interaction.WriteOptions{})
		return werr
	})
	if err != nil {
		t.Fatal(err)
	}
	out := make([]im.ConcreteAttributePath, len(statuses))
	for i, s := range statuses {
		out[i] = s.Path
	}
	return out
}

// invokeAs invokes cmd with fields from the pair's controller.
func invokeAs(t *testing.T, p *thermostatPair, cmd uint32, fields spec.Encodable) (interaction.Result, error) {
	t.Helper()
	return fabrictest.Invoke(parityCtx(t), p.pair, p.commandPath(cmd), fields)
}

// schedulesEqual reports whether a and b encode alike.
func schedulesEqual(a, b []thermo.Schedule) bool {
	enc := func(l []thermo.Schedule) []byte {
		e := tlv.NewEncoder()
		spec.List[thermo.Schedule](l).EncodeTLV(e, tlv.AnonymousTag())
		out, _ := e.Bytes()
		return out
	}
	return bytes.Equal(enc(a), enc(b))
}
