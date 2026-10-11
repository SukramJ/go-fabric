// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/closure"
	"github.com/SukramJ/go-fabric/cluster/spec"
	cdim "github.com/SukramJ/go-fabric/cluster/spec/closuredimension"
	clusterwire "github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/contract"
)

// deviceTypeClosure is the Closure device type (closure.element.ts, 0x0230).
const deviceTypeClosure uint16 = 0x0230

// garageTravel is how long the simulated drive takes between two stops.
const garageTravel = 2 * time.Second

// demoGarage is a garage door drive with a ventilation stop: ClosureControl
// with Positioning and Ventilation (closure.PositioningVentilationFeatureMap),
// the profile a host's garage projection uses. The drive is simulated: a
// MoveTo starts it (MainState Moving), and after garageTravel it reports
// the target position and stops, which emits MovementCompleted and, at or
// away from FullyClosed, SecureStateChanged.
type demoGarage struct {
	name string
	notifier

	mu     sync.Mutex
	travel *time.Timer
	ctl    *closure.ControlServer
}

var (
	_ contract.EndpointSource = (*demoGarage)(nil)
	_ contract.ChangeNotifier = (*demoGarage)(nil)
)

func newDemoGarage(name string) *demoGarage {
	g := &demoGarage{name: name}
	g.ctl = closure.NewControlServer(closure.Config{Move: g.move, Stop: g.stop})
	closed := clusterwire.ClosureCurrentPositionFullyClosed
	g.ctl.SetCurrentPosition(&closed)
	g.ctl.SetMainState(clusterwire.ClosureMainStateStopped)
	return g
}

// MatterDeviceType implements [contract.EndpointSource].
func (g *demoGarage) MatterDeviceType() uint16 { return deviceTypeClosure }

// MatterClusterServers implements [contract.EndpointSource]. The server is
// the same instance on every call: it holds the drive's state and the
// event emitter the bridge wires into it.
func (g *demoGarage) MatterClusterServers() []contract.ClusterServer {
	return []contract.ClusterServer{g.ctl}
}

// arrivalOf is where a target leaves the drive.
var arrivalOf = map[clusterwire.ClosureTargetPosition]clusterwire.ClosureCurrentPosition{
	clusterwire.ClosureTargetPositionMoveToFullyClosed:         clusterwire.ClosureCurrentPositionFullyClosed,
	clusterwire.ClosureTargetPositionMoveToFullyOpen:           clusterwire.ClosureCurrentPositionFullyOpened,
	clusterwire.ClosureTargetPositionMoveToVentilationPosition: clusterwire.ClosureCurrentPositionOpenedForVentilation,
	clusterwire.ClosureTargetPositionMoveToPedestrianPosition:  clusterwire.ClosureCurrentPositionOpenedForPedestrian,
	clusterwire.ClosureTargetPositionMoveToSignaturePosition:   clusterwire.ClosureCurrentPositionOpenedAtSignature,
}

// move is the drive's MoveTo: it starts travelling and arrives after
// garageTravel. The server sets MainState Moving and the target once this
// returns.
func (g *demoGarage) move(_ context.Context, target clusterwire.ClosureTargetPosition) error {
	arrival := arrivalOf[target]
	g.mu.Lock()
	if g.travel != nil {
		g.travel.Stop()
	}
	g.travel = time.AfterFunc(garageTravel, func() {
		g.mu.Lock()
		g.travel = nil
		g.mu.Unlock()
		pos := arrival
		g.ctl.SetCurrentPosition(&pos)
		g.ctl.SetMainState(clusterwire.ClosureMainStateStopped)
		slog.Info("garage.arrived", slog.String("device", g.name), slog.Int("position", int(pos)))
		g.notify()
	})
	g.mu.Unlock()
	slog.Info("garage.move", slog.String("device", g.name), slog.Int("target", int(target)))
	return nil
}

// stop halts the drive between its stops: the position is no longer one of
// them.
func (g *demoGarage) stop(context.Context) error {
	g.mu.Lock()
	moving := g.travel != nil
	if moving {
		g.travel.Stop()
		g.travel = nil
	}
	g.mu.Unlock()
	if moving {
		partial := clusterwire.ClosureCurrentPositionPartiallyOpened
		g.ctl.SetCurrentPosition(&partial)
	}
	go g.notify()
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
	switch trigger &^ (0xFFFF << 32) {
	case triggerClosureError:
		g.ctl.ReportError(clusterwire.ClosureErrorList{clusterwire.ClosureError(0)}) // PhysicallyBlocked
	case triggerClosureSetupRequired:
		g.ctl.SetMainState(clusterwire.ClosureMainStateSetupRequired)
	case triggerClosureClear:
		g.ctl.SetErrorList(clusterwire.ClosureErrorList{})
		g.ctl.SetMainState(clusterwire.ClosureMainStateStopped)
	default:
		// Protected (…01) and Disengaged (…02) too: their features are not
		// advertised.
		return false
	}
	g.notify()
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
	srv  *closure.DimensionServer

	mu      sync.Mutex
	motion  *time.Timer
	stepBy  uint16
	version cluster.DataVersionTracker
}

var _ contract.EndpointSource = (*demoPanel)(nil)

func newDemoPanel(name string) *demoPanel {
	p := &demoPanel{name: name}
	auto := cdim.ThreeLevelAutoAuto
	srv, err := closure.NewDimension(closure.DimensionConfig{
		Features: closure.DimensionFeaturePositioning | closure.DimensionFeatureMotionLatching | closure.DimensionFeatureUnit |
			closure.DimensionFeatureLimitation | closure.DimensionFeatureSpeed | closure.DimensionFeatureRotation,
		Handler:    panelHandler{p},
		Resolution: 100, StepValue: panelStepValue,
		Unit:              cdim.ClosureUnitMillimeter,
		UnitRange:         &closure.UnitRange{Min: 0, Max: 10000},
		LimitRange:        closure.LimitRange{Min: 0, Max: closure.Percent100thsMax},
		RotationAxis:      cdim.RotationAxisCenteredVertical,
		Overflow:          cdim.OverflowTopInside,
		LatchControlModes: cdim.LatchControlModesRemoteLatching | cdim.LatchControlModesRemoteUnlatching,
		CurrentState: &closure.DimensionState{
			Position: &spec.Nullable[uint16]{Value: closure.Percent100thsMax},
			Latch:    &spec.Nullable[bool]{Value: true},
			Speed:    &auto,
		},
		TargetState: &closure.DimensionState{
			Position: &spec.Nullable[uint16]{Null: true},
			Latch:    &spec.Nullable[bool]{Null: true},
			Speed:    &auto,
		},
		DataVersion: &p.version,
	})
	if err != nil {
		panic(fmt.Sprintf("panel ClosureDimension: %v", err))
	}
	p.srv = srv
	return p
}

// MatterDeviceType implements [contract.EndpointSource].
func (p *demoPanel) MatterDeviceType() uint16 { return closure.DeviceTypeClosurePanel }

// MatterClusterServers implements [contract.EndpointSource].
func (p *demoPanel) MatterClusterServers() []contract.ClusterServer {
	return []contract.ClusterServer{p.srv}
}

// panelHandler is the panel's [closure.DimensionHandler]: both commands
// start the motion one tick later, as the closure-app's OnSetTargetCommand
// and OnStepCommand start their timers. The server sets the target once
// they return.
type panelHandler struct{ p *demoPanel }

func (h panelHandler) SetTarget(_ context.Context, position *uint16, latch *bool, _ *closure.ThreeLevelAuto) error {
	slog.Info("panel.set_target", slog.String("device", h.p.name), slog.Any("position", position), slog.Any("latch", latch))
	h.p.start(panelPositionStep)
	return nil
}

func (h panelHandler) Step(_ context.Context, direction closure.StepDirection, steps uint16, _ *closure.ThreeLevelAuto) error {
	slog.Info("panel.step", slog.String("device", h.p.name), slog.Int("direction", int(direction)), slog.Int("steps", int(steps)))
	h.p.start(panelStepValue)
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

// tick is one motion step: unlatch when the target unlatches, move toward
// the target position, and at the target latch when asked and take the
// target's fields as current.
func (p *demoPanel) tick() {
	p.mu.Lock()
	p.motion = nil
	stepBy := p.stepBy
	p.mu.Unlock()
	cur, tgt := p.srv.CurrentState(), p.srv.TargetState()
	if cur == nil || tgt == nil {
		return
	}
	if isTrue(cur.Latch) && isFalse(tgt.Latch) {
		cur.Latch = &spec.Nullable[bool]{Value: false}
		_ = p.srv.SetCurrentState(cur)
	}
	if next, moving := nextPosition(cur.Position, tgt.Position, stepBy); moving {
		cur.Position = &spec.Nullable[uint16]{Value: next}
		_ = p.srv.SetCurrentState(cur)
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
	_ = p.srv.SetCurrentState(cur)
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
