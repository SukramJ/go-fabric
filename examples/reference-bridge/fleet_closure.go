// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/cluster/spec"
	ccldef "github.com/SukramJ/go-fabric/cluster/spec/closurecontrol"
	cdim "github.com/SukramJ/go-fabric/cluster/spec/closuredimension"
	"github.com/SukramJ/go-fabric/device"
)

// garageTravel is how long the simulated drive takes between two stops.
const garageTravel = 2 * time.Second

// demoGarage is a garage door drive with a ventilation stop: ClosureControl
// with Positioning and Ventilation, the profile a host's garage projection
// uses. The drive is simulated: a MoveTo starts it (MainState Moving), and
// after garageTravel it reports the target position and stops, which emits
// MovementCompleted and, at or away from FullyClosed, SecureStateChanged —
// the device kit's ClosureControl server derives both from the drive's
// report.
type demoGarage struct {
	name string

	mu     sync.Mutex
	travel *time.Timer
	ctl    *ccldef.Server
}

var _ ccldef.Mover = (*demoGarage)(nil)

func newDemoGarage(name string) *demoGarage { return &demoGarage{name: name} }

// device builds the garage: fully closed and stopped.
func (g *demoGarage) device(opts device.Options) (*device.Closure, error) {
	closed, stopped := ccldef.CurrentPositionFullyClosed, ccldef.MainStateStopped
	d, err := device.NewClosure(opts, ccldef.Declare{
		Features: ccldef.FeaturePositioning | ccldef.FeatureVentilation,
		Mover:    g,
		Initial: ccldef.Patch{
			MainState:           &stopped,
			OverallCurrentState: &ccldef.OverallCurrentStateStruct{Position: &spec.Nullable[ccldef.CurrentPositionEnum]{Value: closed}},
		},
	})
	if err != nil {
		return withheldOr(d, err)
	}
	g.mu.Lock()
	g.ctl = d.ClosureControl()
	g.mu.Unlock()
	return d, nil
}

// server is the garage's ClosureControl; nil before the device is built.
func (g *demoGarage) server() *ccldef.Server {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.ctl
}

// report is the drive reporting its position (nil: between the stops) and,
// when given, its main state.
func (g *demoGarage) report(pos *ccldef.CurrentPositionEnum, state *ccldef.MainStateEnum) {
	current := &ccldef.OverallCurrentStateStruct{Position: &spec.Nullable[ccldef.CurrentPositionEnum]{Null: true}}
	if pos != nil {
		current.Position = &spec.Nullable[ccldef.CurrentPositionEnum]{Value: *pos}
	}
	if err := g.server().Set(ccldef.Patch{OverallCurrentState: current, MainState: state}); err != nil {
		slog.Warn("garage.report", slog.String("device", g.name), slog.Any("err", err))
	}
}

// arrivalOf is where a target leaves the drive.
var arrivalOf = map[ccldef.TargetPositionEnum]ccldef.CurrentPositionEnum{
	ccldef.TargetPositionMoveToFullyClosed:         ccldef.CurrentPositionFullyClosed,
	ccldef.TargetPositionMoveToFullyOpen:           ccldef.CurrentPositionFullyOpened,
	ccldef.TargetPositionMoveToVentilationPosition: ccldef.CurrentPositionOpenedForVentilation,
	ccldef.TargetPositionMoveToPedestrianPosition:  ccldef.CurrentPositionOpenedForPedestrian,
	ccldef.TargetPositionMoveToSignaturePosition:   ccldef.CurrentPositionOpenedAtSignature,
}

// MoveTo implements [ccldef.Mover]: the drive starts travelling and
// arrives after garageTravel. The server sets MainState Moving and the
// target once this returns.
func (g *demoGarage) MoveTo(_ context.Context, target ccldef.TargetPositionEnum) error {
	arrival := arrivalOf[target]
	g.mu.Lock()
	if g.travel != nil {
		g.travel.Stop()
	}
	g.travel = time.AfterFunc(garageTravel, func() {
		g.mu.Lock()
		g.travel = nil
		g.mu.Unlock()
		pos, stopped := arrival, ccldef.MainStateStopped
		g.report(&pos, &stopped)
		slog.Info("garage.arrived", slog.String("device", g.name), slog.Int("position", int(pos)))
	})
	g.mu.Unlock()
	slog.Info("garage.move", slog.String("device", g.name), slog.Int("target", int(target)))
	return nil
}

// Stop implements [ccldef.Mover]: the drive halts between its stops, so
// the position is no longer one of them.
func (g *demoGarage) Stop(context.Context) error {
	g.mu.Lock()
	moving := g.travel != nil
	if moving {
		g.travel.Stop()
		g.travel = nil
	}
	g.mu.Unlock()
	if moving {
		partial := ccldef.CurrentPositionPartiallyOpened
		g.report(&partial, nil)
	}
	return nil
}

// ClosureControl test event triggers, as the CHIP cases send them
// (TC_CLCTRL_4_1.py, 5_1, 6_1): the cluster id in the upper half, the
// state in the lower.
const (
	triggerClosureError         uint64 = 0x0104000000000000
	triggerClosureSetupRequired uint64 = 0x0104000000000003
	triggerClosureClear         uint64 = 0x0104000000000004
)

// testEventTrigger applies a ClosureControl test event trigger. Protected
// and Disengaged belong to the Protection and ManuallyOperable features,
// which this drive does not advertise; the cases send them only with the
// feature.
//
// The CHIP harness writes the target endpoint into bits 32..47 of the
// trigger (matter_testing.py _update_legacy_test_event_triggers); the
// daemon has one closure, so the endpoint is masked off.
func (g *demoGarage) testEventTrigger(trigger uint64) (handled bool) {
	var patch ccldef.Patch
	switch trigger &^ (0xFFFF << 32) {
	case triggerClosureError:
		// The error state with the error that caused it: the
		// OperationalError event goes out.
		state, errs := ccldef.MainStateError, []ccldef.ClosureErrorEnum{ccldef.ClosureErrorPhysicallyBlocked}
		patch = ccldef.Patch{MainState: &state, CurrentErrorList: &errs}
	case triggerClosureSetupRequired:
		state := ccldef.MainStateSetupRequired
		patch = ccldef.Patch{MainState: &state}
	case triggerClosureClear:
		state, errs := ccldef.MainStateStopped, []ccldef.ClosureErrorEnum{}
		patch = ccldef.Patch{MainState: &state, CurrentErrorList: &errs}
	default:
		// Protected (…01) and Disengaged (…02) too: their features are not
		// advertised.
		return false
	}
	if err := g.server().Set(patch); err != nil {
		slog.Warn("garage.trigger", slog.String("device", g.name), slog.Any("err", err))
	}
	return true
}

// --- device: a closure panel ---------------------------------------------------

// Panel motion, as connectedhomeip's closure-app simulates it at the
// harness pin (examples/closure-app/linux/ClosureManager.cpp:38-39): one
// motion tick a second, 20 % per tick for SetTarget; a Step tick moves by
// StepValue (HandlePanelStepAction :787-799).
const (
	panelTick         = time.Second
	panelPositionStep = uint16(2000)
	// panelStepValue is the panel's StepValue, 10 %
	// (ClosureDimensionEndpoint.cpp:63).
	panelStepValue = uint16(1000)
	// percent100thsMax is 100.00 %.
	percent100thsMax = uint16(10000)
)

// demoPanel is a closure panel (ClosurePanel, 0x0231): one movable part
// with a position and a latch, the ClosureDimension of connectedhomeip's
// closure-app panels — PS, LT, UT, LM, SP, RO; Resolution 1 %, StepValue
// 10 %, millimetres, rotating about a centred vertical axis with a
// top-inside overflow, latching and unlatching remotely; it starts fully
// closed (100.00 %), latched, speed Auto, with a null target
// (ClosureDimensionEndpoint.cpp Init :50-71, ClosureManager.cpp
// SetClosurePanelInitialState :169-207). A command unlatches first when
// it must, moves a tick at a time, latches last when asked, and then
// takes the target's fields as current (HandlePanelUnlatchAction,
// HandlePanelSetTargetAction, UpdateCurrentStateFromTargetState).
type demoPanel struct {
	name string

	mu     sync.Mutex
	srv    *cdim.Server
	motion *time.Timer
	stepBy uint16
}

var _ cdim.DimensionHandler = (*demoPanel)(nil)

func newDemoPanel(name string) *demoPanel { return &demoPanel{name: name} }

// device builds the ClosurePanel.
func (p *demoPanel) device(opts device.Options) (*device.ClosurePanel, error) {
	auto := cdim.ThreeLevelAutoAuto
	resolution, step, unit := uint16(100), panelStepValue, cdim.ClosureUnitMillimeter
	axis, overflow := cdim.RotationAxisCenteredVertical, cdim.OverflowTopInside
	latching := cdim.LatchControlModesRemoteLatching | cdim.LatchControlModesRemoteUnlatching
	d, err := device.NewClosurePanel(opts, cdim.Declare{
		Features: cdim.FeaturePositioning | cdim.FeatureMotionLatching | cdim.FeatureUnit |
			cdim.FeatureLimitation | cdim.FeatureSpeed | cdim.FeatureRotation,
		DimensionHandler: p,
		Initial: cdim.Patch{
			Resolution: &resolution, StepValue: &step, Unit: &unit,
			UnitRange:         &cdim.UnitRangeStruct{Min: 0, Max: 10000},
			LimitRange:        &cdim.RangePercent100thsStruct{Min: 0, Max: percent100thsMax},
			RotationAxis:      &axis,
			Overflow:          &overflow,
			LatchControlModes: &latching,
			CurrentState: &cdim.DimensionStateStruct{
				Position: &spec.Nullable[uint16]{Value: percent100thsMax},
				Latch:    &spec.Nullable[bool]{Value: true},
				Speed:    &auto,
			},
			TargetState: &cdim.DimensionStateStruct{
				Position: &spec.Nullable[uint16]{Null: true},
				Latch:    &spec.Nullable[bool]{Null: true},
				Speed:    &auto,
			},
		},
	})
	if err != nil {
		return withheldOr(d, err)
	}
	p.mu.Lock()
	p.srv = d.ClosureDimension()
	p.mu.Unlock()
	return d, nil
}

// SetTarget implements [cdim.DimensionHandler]: both commands start the
// motion one tick later, as the closure-app's OnSetTargetCommand and
// OnStepCommand start their timers. The server sets the target once they
// return.
func (p *demoPanel) SetTarget(_ context.Context, position *uint16, latch *bool, _ *cdim.ThreeLevelAutoEnum) error {
	slog.Info("panel.set_target", slog.String("device", p.name), slog.Any("position", position), slog.Any("latch", latch))
	p.start(panelPositionStep)
	return nil
}

// Step implements [cdim.DimensionHandler].
func (p *demoPanel) Step(_ context.Context, direction cdim.StepDirectionEnum, steps uint16, _ *cdim.ThreeLevelAutoEnum) error {
	slog.Info("panel.step", slog.String("device", p.name), slog.Int("direction", int(direction)), slog.Int("steps", int(steps)))
	p.start(panelStepValue)
	return nil
}

// start (re)arms the motion timer.
func (p *demoPanel) start(stepBy uint16) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.motion != nil {
		p.motion.Stop()
	}
	p.stepBy = stepBy
	p.motion = time.AfterFunc(panelTick, p.tick)
}

// setCurrent records the panel's current state.
func (p *demoPanel) setCurrent(srv *cdim.Server, cur *cdim.DimensionStateStruct) {
	if err := srv.Set(cdim.Patch{CurrentState: cur}); err != nil {
		slog.Warn("panel.state", slog.String("device", p.name), slog.Any("err", err))
	}
}

// tick is one motion step: unlatch when the target unlatches, move toward
// the target position, and at the target latch when asked and take the
// target's fields as current.
func (p *demoPanel) tick() {
	p.mu.Lock()
	p.motion = nil
	stepBy, srv := p.stepBy, p.srv
	p.mu.Unlock()
	st := srv.Get()
	cur, tgt := st.CurrentState, st.TargetState
	if cur == nil || tgt == nil {
		return
	}
	if isTrue(cur.Latch) && isFalse(tgt.Latch) {
		cur.Latch = &spec.Nullable[bool]{Value: false}
		p.setCurrent(srv, cur)
	}
	if next, moving := nextPosition(cur.Position, tgt.Position, stepBy); moving {
		cur.Position = &spec.Nullable[uint16]{Value: next}
		p.setCurrent(srv, cur)
		if next != tgt.Position.Value {
			p.mu.Lock()
			p.motion = time.AfterFunc(panelTick, p.tick)
			p.mu.Unlock()
			return
		}
	}
	// UpdateCurrentStateFromTargetState: the target's non-null fields.
	if tgt.Position != nil && !tgt.Position.Null {
		cur.Position = tgt.Position
	}
	if tgt.Latch != nil && !tgt.Latch.Null {
		cur.Latch = tgt.Latch
	}
	if tgt.Speed != nil {
		cur.Speed = tgt.Speed
	}
	p.setCurrent(srv, cur)
	slog.Info("panel.arrived", slog.String("device", p.name))
}

func isTrue(b *spec.Nullable[bool]) bool  { return b != nil && !b.Null && b.Value }
func isFalse(b *spec.Nullable[bool]) bool { return b != nil && !b.Null && !b.Value }

// nextPosition is the closure-app's GetPanelNextPosition (:978-1013) with
// the step size a parameter: one step toward the target, never past it.
func nextPosition(cur, tgt *spec.Nullable[uint16], by uint16) (uint16, bool) {
	if cur == nil || cur.Null || tgt == nil || tgt.Null || cur.Value == tgt.Value {
		return 0, false
	}
	if cur.Value < tgt.Value {
		return min(cur.Value+by, tgt.Value), true
	}
	return max(cur.Value-min(cur.Value, by), tgt.Value), true
}
