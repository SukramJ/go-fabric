// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package closure_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/closure"
	"github.com/SukramJ/go-fabric/cluster/spec"
	cd "github.com/SukramJ/go-fabric/cluster/spec/closuredimension"
	"github.com/SukramJ/go-fabric/im"
)

// panel is a host device: it records the commands it is handed.
type panel struct {
	mu       sync.Mutex
	targets  []*uint16
	latches  []*bool
	steps    []closure.StepDirection
	failWith error
}

func (p *panel) SetTarget(_ context.Context, position *uint16, latch *bool, _ *closure.ThreeLevelAuto) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.failWith != nil {
		return p.failWith
	}
	p.targets = append(p.targets, position)
	p.latches = append(p.latches, latch)
	return nil
}

func (p *panel) Step(_ context.Context, d closure.StepDirection, _ uint16, _ *closure.ThreeLevelAuto) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.failWith != nil {
		return p.failWith
	}
	p.steps = append(p.steps, d)
	return nil
}

func pos(v uint16) *spec.Nullable[uint16] { return &spec.Nullable[uint16]{Value: v} }

func latch(v bool) *spec.Nullable[bool] { return &spec.Nullable[bool]{Value: v} }

// chipPanel is connectedhomeip's closure-app panel at the harness pin
// (examples/closure-app/closure-common/src/ClosureDimensionEndpoint.cpp
// Init, linux/ClosureManager.cpp SetClosurePanelInitialState): PS, LT,
// UT, LM, SP, RO; Resolution 1 %, StepValue 10 %; fully closed, latched,
// speed Auto; the target fields null.
func chipPanel(p *panel) closure.DimensionConfig {
	auto := cd.ThreeLevelAutoAuto
	return closure.DimensionConfig{
		Features: closure.DimensionFeaturePositioning | closure.DimensionFeatureMotionLatching | closure.DimensionFeatureUnit |
			closure.DimensionFeatureLimitation | closure.DimensionFeatureSpeed | closure.DimensionFeatureRotation,
		Handler: p, Resolution: 100, StepValue: 1000,
		Unit: cd.ClosureUnitMillimeter, UnitRange: &closure.UnitRange{Min: 0, Max: 10000},
		LimitRange:   closure.LimitRange{Min: 0, Max: 10000},
		RotationAxis: cd.RotationAxisCenteredVertical, Overflow: cd.OverflowTopInside,
		LatchControlModes: cd.LatchControlModesRemoteLatching | cd.LatchControlModesRemoteUnlatching,
		CurrentState:      &closure.DimensionState{Position: pos(10000), Latch: latch(true), Speed: &auto},
		TargetState:       &closure.DimensionState{Position: &spec.Nullable[uint16]{Null: true}, Latch: &spec.Nullable[bool]{Null: true}, Speed: &auto},
	}
}

func newDimension(t *testing.T, cfg closure.DimensionConfig) *closure.DimensionServer {
	t.Helper()
	srv, err := closure.NewDimension(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

func wantStatus(t *testing.T, what string, err error, want im.StatusCode) {
	t.Helper()
	sce, ok := errors.AsType[im.StatusCodeError](err)
	if !ok || sce.MatterStatusCode() != want {
		t.Errorf("%s: %v, want status 0x%02X", what, err, want)
	}
}

// TestDimensionConstruction refuses what chip's constructor and setters
// refuse, and the configuration a server needs.
func TestDimensionConstruction(t *testing.T) {
	t.Parallel()
	base := chipPanel(&panel{})
	for name, mutate := range map[string]func(*closure.DimensionConfig){
		"no handler":               func(c *closure.DimensionConfig) { c.Handler = nil },
		"neither PS nor LT":        func(c *closure.DimensionConfig) { c.Features = 0 },
		"Resolution 0":             func(c *closure.DimensionConfig) { c.Resolution = 0 },
		"StepValue above 100 %":    func(c *closure.DimensionConfig) { c.StepValue = 10001 },
		"undefined unit":           func(c *closure.DimensionConfig) { c.Unit = 2 },
		"undefined rotation axis":  func(c *closure.DimensionConfig) { c.RotationAxis = 11 },
		"undefined latch mode bit": func(c *closure.DimensionConfig) { c.LatchControlModes = 4 },
		"UnitRange reversed":       func(c *closure.DimensionConfig) { c.UnitRange = &closure.UnitRange{Min: 5, Max: 1} },
		"UnitRange negative mm":    func(c *closure.DimensionConfig) { c.UnitRange = &closure.UnitRange{Min: -1, Max: 1} },
		"LimitRange reversed":      func(c *closure.DimensionConfig) { c.LimitRange = closure.LimitRange{Min: 200, Max: 100} },
		"LimitRange above 100 %":   func(c *closure.DimensionConfig) { c.LimitRange = closure.LimitRange{Max: 10100} },
		"LimitRange off the grid":  func(c *closure.DimensionConfig) { c.LimitRange = closure.LimitRange{Min: 50, Max: 10000} },
		"current above 100 %":      func(c *closure.DimensionConfig) { c.CurrentState = &closure.DimensionState{Position: pos(10001)} },
		"target off the grid":      func(c *closure.DimensionConfig) { c.TargetState = &closure.DimensionState{Position: pos(150)} },
		"unknown speed":            func(c *closure.DimensionConfig) { c.TargetState = &closure.DimensionState{Speed: speedOf(4)} },
	} {
		cfg := base
		mutate(&cfg)
		if _, err := closure.NewDimension(cfg); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// Degrees: a span of at most 360 within -360..360.
	deg := closure.DimensionConfig{
		Features: closure.DimensionFeaturePositioning | closure.DimensionFeatureUnit | closure.DimensionFeatureRotation,
		Handler:  &panel{}, Resolution: 1, StepValue: 1, Unit: cd.ClosureUnitDegree,
	}
	for _, r := range []closure.UnitRange{{Min: -360, Max: 1}, {Min: -400, Max: 0}, {Min: 0, Max: 361}} {
		deg.UnitRange = &r
		if _, err := closure.NewDimension(deg); !errors.Is(err, closure.ErrDimensionValue) {
			t.Errorf("degree range %+v: %v", r, err)
		}
	}
	deg.UnitRange = &closure.UnitRange{Min: -180, Max: 180}
	newDimension(t, deg)
	// Fields of features the server lacks.
	lt := closure.DimensionConfig{Features: closure.DimensionFeatureMotionLatching, Handler: &panel{}}
	for _, st := range []closure.DimensionState{{Position: pos(0)}, {Speed: speedOf(0)}} {
		lt.CurrentState = &st
		if _, err := closure.NewDimension(lt); !errors.Is(err, closure.ErrDimensionValue) {
			t.Errorf("LT-only state %+v: %v", st, err)
		}
	}
	ps := closure.DimensionConfig{Features: closure.DimensionFeaturePositioning | closure.DimensionFeatureRotation, Handler: &panel{}, Resolution: 1, StepValue: 1}
	ps.CurrentState = &closure.DimensionState{Latch: latch(true)}
	if _, err := closure.NewDimension(ps); !errors.Is(err, closure.ErrDimensionValue) {
		t.Errorf("PS-only latch: %v", err)
	}
}

func speedOf(v uint8) *closure.ThreeLevelAuto { s := closure.ThreeLevelAuto(v); return &s }

func ptr[T any](v T) *T { return &v }

// TestDimensionReads answers every attribute of chip's panel.
func TestDimensionReads(t *testing.T) {
	t.Parallel()
	srv := newDimension(t, chipPanel(&panel{}))
	for attr, want := range map[uint32]any{
		cd.AttrResolution:        uint16(100),
		cd.AttrStepValue:         uint16(1000),
		cd.AttrUnit:              uint8(cd.ClosureUnitMillimeter),
		cd.AttrUnitRange:         closure.UnitRange{Min: 0, Max: 10000},
		cd.AttrLimitRange:        closure.LimitRange{Min: 0, Max: 10000},
		cd.AttrRotationAxis:      uint8(cd.RotationAxisCenteredVertical),
		cd.AttrOverflow:          uint8(cd.OverflowTopInside),
		cd.AttrLatchControlModes: uint8(3),
	} {
		if got, ok := srv.MatterRead(attr); !ok || got != want {
			t.Errorf("attribute 0x%04X = %v (%v), want %v", attr, got, ok, want)
		}
	}
	if v, _ := srv.MatterRead(cd.AttrCurrentState); v.(closure.DimensionState).Position.Value != 10000 {
		t.Errorf("CurrentState %+v", v)
	}
	if _, ok := srv.MatterRead(cd.AttrTranslationDirection); ok {
		t.Error("TranslationDirection read without TR")
	}
	if err := srv.SetUnitRange(nil); err != nil {
		t.Fatal(err)
	}
	if v, ok := srv.MatterRead(cd.AttrUnitRange); !ok || v != nil {
		t.Errorf("a null UnitRange reads %v", v)
	}
	if err := srv.MatterWrite(context.Background(), cd.AttrLimitRange, closure.LimitRange{}); err == nil {
		t.Error("a write to LimitRange was accepted")
	}
	tr := newDimension(t, closure.DimensionConfig{
		Features: closure.DimensionFeaturePositioning | closure.DimensionFeatureTranslation, Handler: &panel{},
		Resolution: 1, StepValue: 1, TranslationDirection: cd.TranslationDirectionUpward,
	})
	if v, _ := tr.MatterRead(cd.AttrTranslationDirection); v != uint8(cd.TranslationDirectionUpward) {
		t.Errorf("TranslationDirection %v", v)
	}
	if v, ok := tr.MatterRead(cd.AttrCurrentState); !ok || v != nil {
		t.Errorf("a null CurrentState reads %v", v)
	}
	md := newDimension(t, closure.DimensionConfig{
		Features: closure.DimensionFeaturePositioning | closure.DimensionFeatureModulation, Handler: &panel{},
		Resolution: 1, StepValue: 1, ModulationType: cd.ModulationTypeOpacity,
	})
	if v, _ := md.MatterRead(cd.AttrModulationType); v != uint8(cd.ModulationTypeOpacity) {
		t.Errorf("ModulationType %v", v)
	}
	if v, ok := md.MatterRead(0xFFFC); !ok || v != uint32(md.FeatureMap()) {
		t.Errorf("FeatureMap %v", v)
	}
}

// TestSetTarget walks chip's HandleSetTargetCommand.
func TestSetTarget(t *testing.T) {
	t.Parallel()
	p := &panel{}
	srv := newDimension(t, chipPanel(p))
	ctx := context.Background()
	invoke := func(req cd.SetTargetRequest) error {
		_, err := srv.MatterInvoke(ctx, cd.CmdSetTarget, req)
		return err
	}

	wantStatus(t, "no field", invoke(cd.SetTargetRequest{}), im.StatusInvalidCommand)
	wantStatus(t, "position above 100 %", invoke(cd.SetTargetRequest{Position: ptr[uint16](10001)}), im.StatusConstraintError)
	wantStatus(t, "unknown speed", invoke(cd.SetTargetRequest{Speed: speedOf(4)}), im.StatusConstraintError)
	// Latched: a position alone, or with latch=true, is INVALID_IN_STATE.
	wantStatus(t, "latched position", invoke(cd.SetTargetRequest{Position: ptr[uint16](5000)}), im.StatusInvalidInState)
	wantStatus(t, "latched position latch=true", invoke(cd.SetTargetRequest{Position: ptr[uint16](5000), Latch: ptr(true)}), im.StatusInvalidInState)
	if len(p.targets) != 0 {
		t.Fatalf("the handler ran for a refused command: %v", p.targets)
	}

	// Unlatch and move in one command; the position is rounded to the
	// Resolution grid (5050 → 5100).
	if err := invoke(cd.SetTargetRequest{Position: ptr[uint16](5050), Latch: ptr(false)}); err != nil {
		t.Fatal(err)
	}
	if *p.targets[0] != 5100 || *p.latches[0] {
		t.Errorf("handler got position %d latch %v", *p.targets[0], *p.latches[0])
	}
	tgt := srv.TargetState()
	if tgt.Position.Value != 5100 || tgt.Latch.Value || tgt.Latch.Null || *tgt.Speed != cd.ThreeLevelAutoAuto {
		t.Errorf("TargetState %+v", tgt)
	}

	// With LM the position is clamped into LimitRange first.
	if err := srv.SetLimitRange(closure.LimitRange{Min: 1000, Max: 9000}); err != nil {
		t.Fatal(err)
	}
	if err := srv.SetCurrentState(&closure.DimensionState{Position: pos(5100), Latch: latch(false), Speed: speedOf(0)}); err != nil {
		t.Fatal(err)
	}
	_ = invoke(cd.SetTargetRequest{Position: ptr[uint16](9500)})
	_ = invoke(cd.SetTargetRequest{Position: ptr[uint16](0), Speed: speedOf(2)})
	if got := []uint16{*p.targets[1], *p.targets[2]}; !slices.Equal(got, []uint16{9000, 1000}) {
		t.Errorf("clamped positions %v, want [9000 1000]", got)
	}
	if *srv.TargetState().Speed != cd.ThreeLevelAutoMedium {
		t.Errorf("speed not taken: %+v", srv.TargetState())
	}

	// The handler's error is FAILURE and leaves TargetState alone.
	before := srv.TargetState()
	p.failWith = errors.New("motor fault")
	wantStatus(t, "handler error", invoke(cd.SetTargetRequest{Position: ptr[uint16](2000)}), im.StatusFailure)
	if got := srv.TargetState(); got.Position.Value != before.Position.Value {
		t.Errorf("TargetState changed after a failure: %+v", got)
	}
	p.failWith = nil

	// Rounding to the grid can leave 100 %: TargetState refuses it, and
	// chip answers FAILURE (:557).
	coarse := newDimension(t, closure.DimensionConfig{
		Features: closure.DimensionFeaturePositioning | closure.DimensionFeatureRotation, Handler: &panel{},
		Resolution: 6000, StepValue: 6000, CurrentState: &closure.DimensionState{Position: pos(0)},
	})
	_, err := coarse.MatterInvoke(ctx, cd.CmdSetTarget, cd.SetTargetRequest{Position: ptr[uint16](9500)})
	wantStatus(t, "rounded past 100 %", err, im.StatusFailure)

	// The current position unknown, or CurrentState null: INVALID_IN_STATE.
	_ = srv.SetCurrentState(&closure.DimensionState{Position: &spec.Nullable[uint16]{Null: true}, Latch: latch(false)})
	wantStatus(t, "unknown position", invoke(cd.SetTargetRequest{Latch: ptr(true)}), im.StatusInvalidInState)
	_ = srv.SetCurrentState(nil)
	wantStatus(t, "null CurrentState", invoke(cd.SetTargetRequest{Latch: ptr(true)}), im.StatusInvalidInState)
}

// TestSetTargetLatchModes: a latch change the LatchControlModes do not
// allow remotely is INVALID_IN_STATE; a latch-only closure takes a latch
// without a position, and the position field is ignored without PS.
func TestSetTargetLatchModes(t *testing.T) {
	t.Parallel()
	p := &panel{}
	srv := newDimension(t, closure.DimensionConfig{
		Features: closure.DimensionFeatureMotionLatching, Handler: p,
		LatchControlModes: cd.LatchControlModesRemoteLatching,
		CurrentState:      &closure.DimensionState{Latch: latch(false)},
	})
	ctx := context.Background()
	_, err := srv.MatterInvoke(ctx, cd.CmdSetTarget, &cd.SetTargetRequest{Latch: ptr(false)})
	wantStatus(t, "remote unlatching not allowed", err, im.StatusInvalidInState)
	if _, err := srv.MatterInvoke(ctx, cd.CmdSetTarget, cd.SetTargetRequest{Latch: ptr(true), Position: ptr[uint16](100)}); err != nil {
		t.Fatal(err)
	}
	if tgt := srv.TargetState(); tgt.Position != nil || !tgt.Latch.Value {
		t.Errorf("TargetState %+v", tgt)
	}
	_ = srv.SetCurrentState(&closure.DimensionState{Latch: latch(true)})
	_, err = srv.MatterInvoke(ctx, cd.CmdSetTarget, cd.SetTargetRequest{Position: ptr[uint16](100)})
	wantStatus(t, "latched, position without PS", err, im.StatusInvalidInState)
	_, err = srv.MatterInvoke(ctx, cd.CmdStep, cd.StepRequest{Direction: cd.StepDirectionIncrease, NumberOfSteps: 1})
	wantStatus(t, "Step without PS", err, im.StatusUnsupportedCommand)
	for _, fields := range []any{nil, (*cd.SetTargetRequest)(nil), cd.StepRequest{}} {
		_, err := srv.MatterInvoke(ctx, cd.CmdSetTarget, fields)
		wantStatus(t, "SetTarget fields", err, im.StatusInvalidCommand)
	}
}

// TestStep walks chip's HandleStepCommand.
func TestStep(t *testing.T) {
	t.Parallel()
	p := &panel{}
	cfg := chipPanel(p)
	cfg.CurrentState = &closure.DimensionState{Position: pos(5000), Latch: latch(false)}
	srv := newDimension(t, cfg)
	ctx := context.Background()
	step := func(d closure.StepDirection, n uint16) error {
		_, err := srv.MatterInvoke(ctx, cd.CmdStep, cd.StepRequest{Direction: d, NumberOfSteps: n, Speed: speedOf(3)})
		return err
	}
	wantStatus(t, "unknown direction", step(2, 1), im.StatusConstraintError)
	wantStatus(t, "zero steps", step(cd.StepDirectionIncrease, 0), im.StatusConstraintError)
	_, err := srv.MatterInvoke(ctx, cd.CmdStep, cd.StepRequest{Direction: cd.StepDirectionIncrease, NumberOfSteps: 1, Speed: speedOf(7)})
	wantStatus(t, "unknown speed", err, im.StatusConstraintError)

	for _, c := range []struct {
		dir  closure.StepDirection
		n    uint16
		want uint16
	}{
		{cd.StepDirectionIncrease, 2, 7000},
		{cd.StepDirectionIncrease, 9, 10000}, // capped at 100 %
		{cd.StepDirectionDecrease, 1, 4000},
		{cd.StepDirectionDecrease, 60, 0}, // no underflow
	} {
		if err := step(c.dir, c.n); err != nil {
			t.Fatal(err)
		}
		if got := srv.TargetState().Position.Value; got != c.want {
			t.Errorf("step %d×%d from 5000: target %d, want %d", c.dir, c.n, got, c.want)
		}
	}
	if *srv.TargetState().Speed != cd.ThreeLevelAutoHigh {
		t.Errorf("speed not taken: %+v", srv.TargetState())
	}
	// With a LimitRange the step stops at its bounds.
	_ = srv.SetLimitRange(closure.LimitRange{Min: 2000, Max: 8000})
	_ = step(cd.StepDirectionDecrease, 9)
	if got := srv.TargetState().Position.Value; got != 2000 {
		t.Errorf("decrease into LimitRange: %d", got)
	}
	_ = step(cd.StepDirectionIncrease, 9)
	if got := srv.TargetState().Position.Value; got != 8000 {
		t.Errorf("increase into LimitRange: %d", got)
	}

	// A target off the Resolution grid is FAILURE (chip :659).
	odd := chipPanel(&panel{})
	odd.StepValue = 150
	odd.CurrentState = &closure.DimensionState{Position: pos(5000), Latch: latch(false)}
	oddSrv := newDimension(t, odd)
	_, err = oddSrv.MatterInvoke(ctx, cd.CmdStep, &cd.StepRequest{Direction: cd.StepDirectionIncrease, NumberOfSteps: 1})
	wantStatus(t, "target off the grid", err, im.StatusFailure)

	p.failWith = errors.New("jammed")
	wantStatus(t, "handler error", step(cd.StepDirectionIncrease, 1), im.StatusFailure)
	p.failWith = nil
	_ = srv.SetCurrentState(&closure.DimensionState{Position: pos(5000), Latch: latch(true)})
	wantStatus(t, "latched", step(cd.StepDirectionIncrease, 1), im.StatusInvalidInState)
	_ = srv.SetCurrentState(&closure.DimensionState{Position: &spec.Nullable[uint16]{Null: true}})
	wantStatus(t, "unknown position", step(cd.StepDirectionIncrease, 1), im.StatusInvalidInState)
	for _, fields := range []any{nil, cd.SetTargetRequest{}} {
		_, err := srv.MatterInvoke(ctx, cd.CmdStep, fields)
		wantStatus(t, "Step fields", err, im.StatusInvalidCommand)
	}
}

// fakeClock drives the CurrentState throttle by hand.
type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }

// TestCurrentStateReporting follows chip's rules (:210-324): the first
// change, a change of nullness, reaching the target, and a Latch or Speed
// change report at once; any other Position change at most every 5 s.
func TestCurrentStateReporting(t *testing.T) {
	t.Parallel()
	clk := &fakeClock{now: time.Unix(1000, 0)}
	cfg := chipPanel(&panel{})
	cfg.Now = clk.Now
	dv := &cluster.DataVersionTracker{}
	cfg.DataVersion = dv
	srv := newDimension(t, cfg)
	var mu sync.Mutex
	var reports int
	srv.OnMatterAttributesChanged(func(ids []uint32) {
		mu.Lock()
		defer mu.Unlock()
		if slices.Contains(ids, cd.AttrCurrentState) {
			reports++
		}
	})
	set := func(p uint16, l bool) {
		t.Helper()
		if err := srv.SetCurrentState(&closure.DimensionState{Position: pos(p), Latch: latch(l)}); err != nil {
			t.Fatal(err)
		}
	}
	set(10000, false) // latch change
	if reports != 1 {
		t.Fatalf("latch change: %d reports", reports)
	}
	set(8000, false) // first Position change: no earlier report to wait for
	set(6000, false) // within 5 s: held back
	if reports != 2 {
		t.Errorf("position changes inside 5 s: %d reports, want 2", reports)
	}
	if v, _ := srv.MatterRead(cd.AttrCurrentState); v.(closure.DimensionState).Position.Value != 6000 {
		t.Error("a held-back value does not read")
	}
	version := srv.MatterDataVersion()
	clk.now = clk.now.Add(5 * time.Second)
	set(5000, false) // 5 s later: reported
	if reports != 3 || srv.MatterDataVersion() == version || dv.Current() != srv.MatterDataVersion() {
		t.Errorf("after 5 s: %d reports", reports)
	}
	_ = srv.SetTargetState(&closure.DimensionState{Position: pos(4000)})
	set(4000, false) // target reached: at once
	if reports != 4 {
		t.Errorf("target reached: %d reports", reports)
	}
	set(4000, false) // equal: nothing
	_ = srv.SetCurrentState(&closure.DimensionState{Position: &spec.Nullable[uint16]{Null: true}, Latch: latch(false)})
	if reports != 5 {
		t.Errorf("to a null position: %d reports", reports)
	}
	_ = srv.SetCurrentState(nil)
	if reports != 6 {
		t.Errorf("to null: %d reports", reports)
	}
	if err := srv.SetCurrentState(&closure.DimensionState{Position: pos(10001)}); !errors.Is(err, closure.ErrDimensionValue) {
		t.Errorf("position above 100 %%: %v", err)
	}
	if got := srv.MatterReportable(); slices.Contains(got, cd.AttrCurrentState) || !slices.Contains(got, cd.AttrTargetState) {
		t.Errorf("MatterReportable %v", got)
	}
}

// TestDimensionSetters holds the host's range setters to chip's rules and
// their features.
func TestDimensionSetters(t *testing.T) {
	t.Parallel()
	srv := newDimension(t, chipPanel(&panel{}))
	if err := srv.SetUnitRange(&closure.UnitRange{Min: 0, Max: 500}); err != nil {
		t.Fatal(err)
	}
	if err := srv.SetUnitRange(&closure.UnitRange{Min: 0, Max: 500}); err != nil {
		t.Fatal(err)
	}
	if err := srv.SetUnitRange(&closure.UnitRange{Min: 9, Max: 1}); !errors.Is(err, closure.ErrDimensionValue) {
		t.Errorf("reversed UnitRange: %v", err)
	}
	if err := srv.SetLimitRange(closure.LimitRange{Min: 0, Max: 10000}); err != nil {
		t.Fatal(err)
	}
	if err := srv.SetLimitRange(closure.LimitRange{Min: 1, Max: 10000}); !errors.Is(err, closure.ErrDimensionValue) {
		t.Errorf("LimitRange off the grid: %v", err)
	}
	for range 2 { // the second is equal: nothing changes
		if err := srv.SetTargetState(nil); err != nil {
			t.Fatal(err)
		}
	}
	if v, ok := srv.MatterRead(cd.AttrTargetState); !ok || v != nil {
		t.Errorf("a null TargetState reads %v", v)
	}
	if err := srv.SetTargetState(&closure.DimensionState{Position: pos(10100)}); !errors.Is(err, closure.ErrDimensionValue) {
		t.Errorf("target above 100 %%: %v", err)
	}
	lt := newDimension(t, closure.DimensionConfig{Features: closure.DimensionFeatureMotionLatching, Handler: &panel{}})
	if err := lt.SetUnitRange(nil); !errors.Is(err, closure.ErrDimensionValue) {
		t.Errorf("UnitRange without UT: %v", err)
	}
	if err := lt.SetLimitRange(closure.LimitRange{}); !errors.Is(err, closure.ErrDimensionValue) {
		t.Errorf("LimitRange without LM: %v", err)
	}
	if lt.CurrentState() != nil {
		t.Error("an unset CurrentState is not null")
	}
}
