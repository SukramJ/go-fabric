// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/cluster/spec"
	levelcontroldef "github.com/SukramJ/go-fabric/cluster/spec/levelcontrol"
	onoffdef "github.com/SukramJ/go-fabric/cluster/spec/onoff"
	thermostatdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/tlv"
)

// The gate-2 consumer's defects 1–3 (notes/briefs/reports/G2-consumer/
// NOTES.md on docs/gate2-consumer; brief BY-1), over fabrictest: a change
// of CurrentLevel reaches the host's OnChanged whoever made it and a
// subscriber's report, and a controller's write of a thermostat setpoint
// reaches OnChanged from the write. The matter.js Datasource queues a
// change notification on the commit of each state write
// (packages/node/src/behavior/state/managed/Datasource.ts:1473-1476); the quieter ("Q") throttle stands in front of
// the wire only (packages/node/src/behavior/Events.ts:267-297,
// internal/ServerBehaviorBacking.ts:265-273).

// levelSeen is one change of the light's level as the host saw it.
type levelSeen struct {
	Level  uint8
	Origin contract.Origin
}

// levelLog records what one hook saw.
type levelLog struct {
	mu   sync.Mutex
	seen []levelSeen
}

func (l *levelLog) add(level uint8, origin contract.Origin) {
	l.mu.Lock()
	l.seen = append(l.seen, levelSeen{level, origin})
	l.mu.Unlock()
}

func (l *levelLog) get() []levelSeen {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.seen)
}

// heldLevelSource is a host's LevelSource that notifies its changes: the
// device's own level, which a wall dimmer moves.
type heldLevelSource struct {
	mu      sync.Mutex
	level   uint8
	options levelcontroldef.OptionsBitmap
	onLevel *uint8
	notify  []func()
}

func (s *heldLevelSource) CurrentLevel() (uint8, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.level, true
}

func (s *heldLevelSource) Options() levelcontroldef.OptionsBitmap {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.options
}

func (s *heldLevelSource) OnLevel() (uint8, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.onLevel == nil {
		return 0, false
	}
	return *s.onLevel, true
}

func (s *heldLevelSource) SetOptions(_ context.Context, o levelcontroldef.OptionsBitmap) error {
	s.mu.Lock()
	s.options = o
	s.mu.Unlock()
	return nil
}

func (s *heldLevelSource) SetOnLevel(_ context.Context, l *uint8) error {
	s.mu.Lock()
	s.onLevel = l
	s.mu.Unlock()
	return nil
}

// set moves the level and notifies, as the device does.
func (s *heldLevelSource) set(level uint8) {
	s.mu.Lock()
	s.level = level
	cbs := slices.Clone(s.notify)
	s.mu.Unlock()
	for _, cb := range cbs {
		if cb != nil {
			cb()
		}
	}
}

func (s *heldLevelSource) MoveToLevel(_ context.Context, req levelcontroldef.MoveToLevelRequest) error {
	s.set(req.Level)
	return nil
}

func (s *heldLevelSource) MoveToLevelWithOnOff(_ context.Context, req levelcontroldef.MoveToLevelWithOnOffRequest) error {
	s.set(req.Level)
	return nil
}
func (*heldLevelSource) Move(context.Context, levelcontroldef.MoveRequest) error { return nil }
func (*heldLevelSource) MoveWithOnOff(context.Context, levelcontroldef.MoveWithOnOffRequest) error {
	return nil
}
func (*heldLevelSource) Step(context.Context, levelcontroldef.StepRequest) error { return nil }
func (*heldLevelSource) StepWithOnOff(context.Context, levelcontroldef.StepWithOnOffRequest) error {
	return nil
}
func (*heldLevelSource) Stop(context.Context, levelcontroldef.StopRequest) error { return nil }
func (*heldLevelSource) StopWithOnOff(context.Context, levelcontroldef.StopWithOnOffRequest) error {
	return nil
}

// OnMatterValueChanged implements contract.ChangeNotifier.
func (s *heldLevelSource) OnMatterValueChanged(cb func()) func() {
	s.mu.Lock()
	s.notify = append(s.notify, cb)
	i := len(s.notify) - 1
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		s.notify[i] = nil
		s.mu.Unlock()
	}
}

// dimmable is one form of the consumer's light, with its hooks.
type dimmable struct {
	light              *device.DimmableLight
	src                *heldLevelSource // nil for the kit's held default
	typed, untyped, as *levelLog
}

func newDimmable(t *testing.T, src *heldLevelSource, transitions bool) *dimmable {
	t.Helper()
	decl := levelcontroldef.Declare{Initial: levelcontroldef.Patch{CurrentLevel: new(uint8(10))}, Transitions: transitions}
	if src != nil {
		decl = levelcontroldef.Declare{LevelSource: src, Transitions: transitions}
	}
	light, err := device.NewDimmableLight(device.Options{StableKey: endpoint.StringKey("light"), Name: "Hall light"},
		onoffdef.Declare{Initial: onoffdef.Patch{OnOff: new(true)}}, decl)
	if err != nil {
		t.Fatal(err)
	}
	d := &dimmable{light: light, src: src, typed: &levelLog{}, untyped: &levelLog{}, as: &levelLog{}}
	light.LevelControl().OnChanged(func(s levelcontroldef.State, c contract.Change) {
		if s.CurrentLevel != nil && slices.Contains(c.Attributes, levelcontroldef.AttrCurrentLevel) {
			d.typed.add(*s.CurrentLevel, c.Origin)
		}
	})
	light.OnChanged(levelcontroldef.ClusterID, func(c contract.Change) {
		if l := light.LevelControl().Get().CurrentLevel; l != nil && slices.Contains(c.Attributes, levelcontroldef.AttrCurrentLevel) {
			d.untyped.add(*l, c.Origin)
		}
	})
	light.LevelControl().OnChangedAsync(func(s levelcontroldef.State, c contract.Change) {
		if s.CurrentLevel != nil && slices.Contains(c.Attributes, levelcontroldef.AttrCurrentLevel) {
			d.as.add(*s.CurrentLevel, c.Origin)
		}
	})
	return d
}

// hostSet is the host's change of the level: a Set on the kit's held
// server, the device's own move on a declared LevelSource.
func (d *dimmable) hostSet(level uint8) error {
	if d.src != nil {
		d.src.set(level)
		return nil
	}
	return d.light.LevelControl().Set(levelcontroldef.Patch{CurrentLevel: &level})
}

// reportedLevels collects the CurrentLevel values a subscription reports
// after its priming report.
type reportedLevels struct {
	mu     sync.Mutex
	primed bool
	levels []uint64
}

func (r *reportedLevels) update(rep *interaction.Report) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.primed {
		r.primed = true
		return
	}
	for i := range rep.Attributes {
		if a := &rep.Attributes[i]; a.Path.Cluster == levelcontroldef.ClusterID && a.Path.Attribute == levelcontroldef.AttrCurrentLevel {
			r.levels = append(r.levels, a.Value.El.Uint)
		}
	}
}

func (r *reportedLevels) get() []uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.levels)
}

// subscribeLevel subscribes the pair's controller to the light's
// CurrentLevel.
func subscribeLevel(t *testing.T, pair *fabrictest.Pair, ep uint16) *reportedLevels {
	t.Helper()
	r := &reportedLevels{}
	sub, err := fabrictest.Subscribe(parityCtx(t), pair, interaction.SubscribeRequest{
		Attributes:       []im.ConcreteAttributePath{lifecycleAttr(ep, levelcontroldef.ClusterID, levelcontroldef.AttrCurrentLevel)},
		MinIntervalFloor: interaction.Interval(0), MaxIntervalCeiling: interaction.Interval(60 * time.Second),
		Updated: r.update,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sub.Close)
	return r
}

// waitFor advances the site's clock past the quieter's interval and waits
// until cond holds.
func waitFor(t *testing.T, site *fabrictest.Site, what string, cond func() bool) {
	t.Helper()
	short, cancel := context.WithTimeout(parityCtx(t), 10*time.Second)
	defer cancel()
	if err := site.Eventually(short, what, func() bool {
		site.Fake().Advance(100 * time.Millisecond)
		return cond()
	}); err != nil {
		t.Fatal(err)
	}
}

// TestLevelControlChangesReachHostAndSubscriber is defects 1 and 2: for
// the kit's held DimmableLight and for one over a declared LevelSource,
// (a) the host's change reaches the typed OnChanged, Device.OnChanged and
// OnChangedAsync from OriginHostSet and a subscriber's report; (b) a
// controller's MoveToLevel reaches them from OriginCommand and the
// subscriber.
func TestLevelControlChangesReachHostAndSubscriber(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  *heldLevelSource
	}{
		{"held", nil},
		{"level source", &heldLevelSource{level: 10}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := parityCtx(t)
			site := quietSite(t)
			d := newDimmable(t, tc.src, false)
			pair := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Bridged: []node.Endpoint{d.light}}})
			ep, _ := d.light.EndpointNumber()
			reports := subscribeLevel(t, pair, ep)

			// (a) the host's change.
			if err := pair.Device.Act(func(*node.Node) error { return d.hostSet(20) }); err != nil {
				t.Fatal(err)
			}
			want := make([]levelSeen, 0, 2)
			want = append(want, levelSeen{20, contract.OriginHostSet})
			if got := d.typed.get(); !slices.Equal(got, want) {
				t.Errorf("typed OnChanged saw %v, want %v", got, want)
			}
			if got := d.untyped.get(); !slices.Equal(got, want) {
				t.Errorf("Device.OnChanged saw %v, want %v", got, want)
			}
			waitFor(t, site, "the host's level report", func() bool { return slices.Contains(reports.get(), 20) })
			waitFor(t, site, "OnChangedAsync of the host's level", func() bool { return slices.Equal(d.as.get(), want) })

			// (b) a controller's MoveToLevel.
			move := im.ConcreteCommandPath{Endpoint: ep, Cluster: levelcontroldef.ClusterID, Command: levelcontroldef.CmdMoveToLevel}
			if _, err := fabrictest.Invoke(ctx, pair, move, levelcontroldef.MoveToLevelRequest{Level: 200}); err != nil {
				t.Fatal(err)
			}
			want = append(want, levelSeen{200, contract.OriginCommand})
			if got := d.typed.get(); !slices.Equal(got, want) {
				t.Errorf("typed OnChanged saw %v, want %v", got, want)
			}
			if got := d.untyped.get(); !slices.Equal(got, want) {
				t.Errorf("Device.OnChanged saw %v, want %v", got, want)
			}
			waitFor(t, site, "the command's level report", func() bool { return slices.Contains(reports.get(), 200) })
			waitFor(t, site, "OnChangedAsync of the command's level", func() bool { return slices.Equal(d.as.get(), want) })
		})
	}
}

// TestLevelControlTransitionReachesHostAndSubscriber is defect 1 for a
// managed transition (Declare.Transitions): every step reaches the host
// from OriginCommand, rising, the target once; the subscriber is reported
// the target at the end.
func TestLevelControlTransitionReachesHostAndSubscriber(t *testing.T) {
	t.Parallel()
	ctx := parityCtx(t)
	site := quietSite(t)
	d := newDimmable(t, nil, true)
	pair := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Bridged: []node.Endpoint{d.light}}})
	ep, _ := d.light.EndpointNumber()
	reports := subscribeLevel(t, pair, ep)
	move := im.ConcreteCommandPath{Endpoint: ep, Cluster: levelcontroldef.ClusterID, Command: levelcontroldef.CmdMoveToLevel}
	// 2 s
	if _, err := fabrictest.Invoke(ctx, pair, move, levelcontroldef.MoveToLevelRequest{Level: 200, TransitionTime: spec.ValueOf(uint16(20))}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, site, "the transition's end", func() bool {
		seen := d.typed.get()
		return len(seen) > 0 && seen[len(seen)-1].Level == 200
	})
	steps := d.typed.get()
	levels := make([]uint8, 0, len(steps))
	for _, s := range steps {
		if s.Origin != contract.OriginCommand {
			t.Errorf("step %v from %v, want the command", s.Level, s.Origin)
		}
		levels = append(levels, s.Level)
	}
	if len(levels) < 3 || !slices.IsSorted(levels) || slices.Index(levels, 200) != len(levels)-1 {
		t.Errorf("the host saw %v, want the steps rising and the target 200 once", levels)
	}
	waitFor(t, site, "the transition's final report", func() bool {
		r := reports.get()
		return len(r) > 0 && r[len(r)-1] == 200
	})
	if r := reports.get(); len(r) >= len(levels) {
		t.Errorf("the subscriber was reported %v, every step of %v; want the quieter's throttle", r, levels)
	}
	waitFor(t, site, "OnChangedAsync of the transition", func() bool { return slices.Equal(d.as.get(), steps) })
}

// heating is the host's heating: the Sink of the setpoint a controller
// asks for.
type heating struct {
	mu   sync.Mutex
	seen []int16
}

func (h *heating) SetSystemMode(context.Context, uint8) error { return nil }

func (h *heating) SetSetpoint(_ context.Context, attrID uint32, value int16, _ []thermostatdef.SetpointChange) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if attrID == thermostatdef.AttrOccupiedHeatingSetpoint {
		h.seen = append(h.seen, value)
	}
	return nil
}

func (h *heating) get() []int16 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.seen)
}

// int16Value is a temperature for a write.
type int16Value int16

func (v int16Value) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) { enc.PutInt(tag, int64(v)) }

// TestThermostatControllerWriteReachesOnChanged is defect 3: a
// controller's write of OccupiedHeatingSetpoint reaches the Sink, Get
// serves it, OnChanged sees it from OriginControllerWrite, and a
// subscriber is reported it once.
func TestThermostatControllerWriteReachesOnChanged(t *testing.T) {
	t.Parallel()
	ctx := parityCtx(t)
	site := quietSite(t)
	heat := &heating{}
	thermo, err := device.NewThermostat(device.Options{StableKey: endpoint.StringKey("thermo"), Name: "Living room"},
		thermostatdef.Declare{
			Features: thermostatdef.FeatureHeating,
			Initial:  thermostatdef.Patch{LocalTemperature: new(int16(1950)), OccupiedHeatingSetpoint: new(int16(2000))},
			Sink:     heat,
		})
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var changes []contract.Change
	thermo.Thermostat().OnChanged(func(_ thermostatdef.State, c contract.Change) {
		mu.Lock()
		changes = append(changes, c)
		mu.Unlock()
	})
	pair := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Bridged: []node.Endpoint{thermo}}})
	ep, _ := thermo.EndpointNumber()
	path := lifecycleAttr(ep, thermostatdef.ClusterID, thermostatdef.AttrOccupiedHeatingSetpoint)
	var rmu sync.Mutex
	primed, reported := false, []int64{}
	sub, err := fabrictest.Subscribe(ctx, pair, interaction.SubscribeRequest{
		Attributes:       []im.ConcreteAttributePath{path},
		MinIntervalFloor: interaction.Interval(0), MaxIntervalCeiling: interaction.Interval(60 * time.Second),
		Updated: func(r *interaction.Report) {
			rmu.Lock()
			defer rmu.Unlock()
			if !primed {
				primed = true
				return
			}
			for _, a := range r.Attributes {
				reported = append(reported, a.Value.El.Int)
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sub.Close)

	if err := fabrictest.Write(ctx, pair, path, int16Value(2250)); err != nil {
		t.Fatal(err)
	}
	if got := heat.get(); !slices.Equal(got, []int16{2250}) {
		t.Errorf("the Sink saw %v, want the controller's 2250", got)
	}
	if got := thermo.Thermostat().Get().OccupiedHeatingSetpoint; got != 2250 {
		t.Errorf("Get serves %d after the write, want 2250", got)
	}
	mu.Lock()
	got := slices.Clone(changes)
	mu.Unlock()
	if len(got) != 1 || got[0].Origin != contract.OriginControllerWrite || !slices.Contains(got[0].Attributes, thermostatdef.AttrOccupiedHeatingSetpoint) {
		t.Errorf("OnChanged saw %+v, want the write of OccupiedHeatingSetpoint from the controller", got)
	}
	waitFor(t, site, "the setpoint report", func() bool {
		rmu.Lock()
		defer rmu.Unlock()
		return len(reported) > 0
	})
	site.Fake().Advance(2 * time.Second)
	if err := site.Settle(ctx); err != nil {
		t.Fatal(err)
	}
	rmu.Lock()
	defer rmu.Unlock()
	if !slices.Equal(reported, []int64{2250}) {
		t.Errorf("the subscriber was reported %v, want 2250 once", reported)
	}
}
