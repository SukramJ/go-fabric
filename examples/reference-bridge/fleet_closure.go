// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/cluster/closure"
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
