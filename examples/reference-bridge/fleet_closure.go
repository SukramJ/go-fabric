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
//
// With the "Ahead of matter.js" row closureRow enabled
// (--extensions BD-chip-ClosureControl-RulesInServer) the drive also has a
// bolt it latches and unlatches remotely, three speeds and a calibration
// run (MotionLatching, Speed, Calibration — three of the closure-app's
// ClosureControl features, connectedhomeip
// examples/closure-app/closure-common/src/ClosureControlEndpoint.cpp:135-147
// at 6170af84, latching and unlatching remotely as there): a latch or
// speed change settles after garageSettle, a calibration after
// garageCalibration, each reporting Stopped.
type demoGarage struct {
	name string

	mu     sync.Mutex
	travel *time.Timer
	ctl    *ccldef.Server
	// latchTo and speedTo are what the running action leaves the drive
	// with; calibration is the calibration run.
	latchTo     *bool
	speedTo     *ccldef.ThreeLevelAutoEnum
	calibration *time.Timer
}

var (
	_ ccldef.Mover      = (*demoGarage)(nil)
	_ ccldef.Latcher    = (*demoGarage)(nil)
	_ ccldef.SpeedMover = (*demoGarage)(nil)
	_ ccldef.Calibrator = (*demoGarage)(nil)
)

// closureRow is the "Ahead of matter.js" register row of the
// ClosureControl features MotionLatching, Instantaneous, Speed and
// Calibration (cluster/closure.Ahead).
const closureRow = "BD-chip-ClosureControl-RulesInServer"

// The drive's timings beyond travel: a latch or speed change, and a
// calibration run, long enough for TC-CLCTRL-3.1 to read Calibrating and
// calibrate again before it ends.
const (
	garageSettle      = 500 * time.Millisecond
	garageCalibration = 3 * time.Second
)

func newDemoGarage(name string) *demoGarage { return &demoGarage{name: name} }

// device builds the garage: fully closed and stopped.
func (g *demoGarage) device(opts device.Options) (*device.Closure, error) {
	closed, stopped := ccldef.CurrentPositionFullyClosed, ccldef.MainStateStopped
	// The Descriptor's TagList, which matter.js's Closure device enables
	// (packages/node/src/devices/closure.ts:91 at 862ad75f): Closure
	// namespace (0x44), GarageDoor (0x05). The device kit leaves it off
	// unless a host opts in (BD-Matter-ClosureWithoutTagList).
	opts.Tags = []device.SemanticTag{{MfgCode: spec.NullOf[uint16](), NamespaceId: 0x44, Tag: 0x05}}
	decl := ccldef.Declare{
		Features: ccldef.FeaturePositioning | ccldef.FeatureVentilation,
		Mover:    g,
		Initial: ccldef.Patch{
			MainState:           &stopped,
			OverallCurrentState: &ccldef.OverallCurrentStateStruct{Position: &spec.Nullable[ccldef.CurrentPositionEnum]{Value: closed}},
		},
	}
	if opts.Surface.Extensions.Has(closureRow) {
		// Unlatched at Auto: the cases unlatch first where they find a
		// latched closure, and an unlatched one needs no preparation.
		auto := ccldef.ThreeLevelAutoAuto
		decl.Features |= ccldef.FeatureMotionLatching | ccldef.FeatureSpeed | ccldef.FeatureCalibration
		decl.Initial.OverallCurrentState.Latch = &spec.Nullable[bool]{Value: false}
		decl.Initial.OverallCurrentState.Speed = &auto
	}
	d, err := device.NewClosure(opts, decl)
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
		g.finish(&arrival)
		slog.Info("garage.arrived", slog.String("device", g.name), slog.Int("position", int(arrival)))
	})
	g.mu.Unlock()
	slog.Info("garage.move", slog.String("device", g.name), slog.Int("target", int(target)))
	return nil
}

// LatchControlModes implements [ccldef.Latcher]: the bolt latches and
// unlatches remotely.
func (*demoGarage) LatchControlModes() ccldef.LatchControlModesBitmap {
	return ccldef.LatchControlModesRemoteLatching | ccldef.LatchControlModesRemoteUnlatching
}

// Latch implements [ccldef.Latcher]: the bolt moves when the drive's
// running action ends — a latch-only request after garageSettle, a latch
// that accompanies a MoveTo on arrival.
func (g *demoGarage) Latch(_ context.Context, latch bool) error {
	g.mu.Lock()
	g.latchTo = &latch
	g.settleLocked()
	g.mu.Unlock()
	slog.Info("garage.latch", slog.String("device", g.name), slog.Bool("latch", latch))
	return nil
}

// SetSpeed implements [ccldef.SpeedMover]: the drive takes the speed when
// its running action ends, as Latch.
func (g *demoGarage) SetSpeed(_ context.Context, speed ccldef.ThreeLevelAutoEnum) error {
	g.mu.Lock()
	g.speedTo = &speed
	g.settleLocked()
	g.mu.Unlock()
	slog.Info("garage.speed", slog.String("device", g.name), slog.Int("speed", int(speed)))
	return nil
}

// settleLocked arms the short action of a latch or speed change unless a
// travel runs, which carries it out on arrival. Caller holds g.mu.
func (g *demoGarage) settleLocked() {
	if g.travel != nil {
		return
	}
	g.travel = time.AfterFunc(garageSettle, func() { g.finish(nil) })
}

// finish ends the running action: the drive reports where it is — at
// arrival, otherwise where it was — with the latch and speed the action
// left it with, and Stopped.
func (g *demoGarage) finish(arrival *ccldef.CurrentPositionEnum) {
	g.mu.Lock()
	g.travel = nil
	latch, speed := g.latchTo, g.speedTo
	g.latchTo, g.speedTo = nil, nil
	g.mu.Unlock()
	srv := g.server()
	current := srv.Get().OverallCurrentState
	if current == nil {
		current = &ccldef.OverallCurrentStateStruct{}
	}
	if arrival != nil {
		current.Position = &spec.Nullable[ccldef.CurrentPositionEnum]{Value: *arrival}
	}
	if latch != nil {
		current.Latch = &spec.Nullable[bool]{Value: *latch}
	}
	if speed != nil {
		current.Speed = speed
	}
	stopped := ccldef.MainStateStopped
	if err := srv.Set(ccldef.Patch{OverallCurrentState: current, MainState: &stopped}); err != nil {
		slog.Warn("garage.report", slog.String("device", g.name), slog.Any("err", err))
	}
}

// Calibrate implements [ccldef.Calibrator]: a calibration run that ends,
// Stopped, after garageCalibration; the server sets Calibrating once this
// returns.
func (g *demoGarage) Calibrate(context.Context) error {
	g.mu.Lock()
	if g.calibration != nil {
		g.calibration.Stop()
	}
	g.calibration = time.AfterFunc(garageCalibration, func() {
		g.mu.Lock()
		g.calibration = nil
		g.mu.Unlock()
		stopped := ccldef.MainStateStopped
		if err := g.server().Set(ccldef.Patch{MainState: &stopped}); err != nil {
			slog.Warn("garage.calibrated", slog.String("device", g.name), slog.Any("err", err))
		}
		slog.Info("garage.calibrated", slog.String("device", g.name))
	})
	g.mu.Unlock()
	slog.Info("garage.calibrate", slog.String("device", g.name))
	return nil
}

// Stop implements [ccldef.Mover]: the drive halts between its stops, so
// the position is no longer one of them; a calibration run is abandoned.
func (g *demoGarage) Stop(context.Context) error {
	g.mu.Lock()
	moving := g.travel != nil
	if moving {
		g.travel.Stop()
		g.travel = nil
	}
	g.latchTo, g.speedTo = nil, nil
	if g.calibration != nil {
		g.calibration.Stop()
		g.calibration = nil
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
	// The Descriptor's TagList, which matter.js's ClosurePanel device
	// enables (packages/node/src/devices/closure-panel.ts:59 at 862ad75f):
	// ClosurePanel namespace (0x45), Rotate (0x03).
	opts.Tags = []device.SemanticTag{{MfgCode: spec.NullOf[uint16](), NamespaceId: 0x45, Tag: 0x03}}
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
