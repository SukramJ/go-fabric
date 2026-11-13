// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package closure_test

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/closure"
	closuredef "github.com/SukramJ/go-fabric/cluster/spec/closurecontrol"
	"github.com/SukramJ/go-fabric/cluster/wire"
)

// reportingMover reports through report while its MoveTo, Latch or
// Calibrate runs: inline, as a fast or Instantaneous drive does before it
// returns, or from another goroutine the call waits for, as a bridged
// drive's event loop does while its RPC is in flight.
type reportingMover struct {
	report func()
	async  bool
}

func (m *reportingMover) during() {
	if m.report == nil {
		return
	}
	if !m.async {
		m.report()
		return
	}
	done := make(chan struct{})
	go func() { m.report(); close(done) }()
	<-done
}

func (m *reportingMover) MoveTo(context.Context, closuredef.TargetPositionEnum) error {
	m.during()
	return nil
}

func (m *reportingMover) Stop(context.Context) error { return nil }

func (m *reportingMover) Calibrate(context.Context) error {
	m.during()
	return nil
}

func (m *reportingMover) LatchControlModes() closuredef.LatchControlModesBitmap {
	return closuredef.LatchControlModesRemoteLatching | closuredef.LatchControlModesRemoteUnlatching
}

func (m *reportingMover) Latch(context.Context, bool) error {
	m.during()
	return nil
}

// TestClosureControlReportDuringTheCallIsKept: a report the drive makes
// while MoveTo or Calibrate is with the mover — inline or from another
// goroutine — is not overwritten by the command's outcome written after
// the mover returned. A MainState the drive reported stays (Stopped after
// a fast motion or calibration, Error); a target the drive reported
// reaching is not outstanding. Without a report MainState becomes Moving
// or Calibrating, as before. The command still answers SUCCESS (review
// II, R2-P1-clu-01).
func TestClosureControlReportDuringTheCallIsKept(t *testing.T) {
	t.Parallel()
	for _, async := range []bool{false, true} {
		name := "inline"
		if async {
			name = "another goroutine"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			newServer := func(t *testing.T, features uint32) (*closure.ControlServer, *reportingMover) {
				t.Helper()
				m := &reportingMover{async: async}
				srv, err := closure.New(closure.Config{FeatureMap: wire.ClosureControlFeaturePositioning | features, Mover: m})
				if err != nil {
					t.Fatal(err)
				}
				srv.SetMainState(wire.ClosureMainStateStopped)
				return srv, m
			}
			mainState := func(t *testing.T, srv *closure.ControlServer) wire.ClosureMainState {
				t.Helper()
				v, _ := srv.MatterRead(wire.ClosureControlAttrMainState)
				return wire.ClosureMainState(v.(uint8))
			}
			target := func(t *testing.T, srv *closure.ControlServer) *wire.ClosureOverallTargetState {
				t.Helper()
				v, _ := srv.MatterRead(wire.ClosureControlAttrOverallTargetState)
				return v.(*wire.ClosureOverallTargetState)
			}
			invoke := func(t *testing.T, srv *closure.ControlServer, cmd uint32, fields any) {
				t.Helper()
				if _, err := srv.MatterInvoke(context.Background(), cmd, fields); err != nil {
					t.Fatalf("command 0x%02X: %v, want SUCCESS", cmd, err)
				}
			}
			fullyOpen := closuredef.TargetPositionMoveToFullyOpen
			opened := wire.ClosureCurrentPositionFullyOpened

			t.Run("MoveTo: arrival and Stopped", func(t *testing.T) {
				t.Parallel()
				srv, m := newServer(t, 0)
				m.report = func() {
					srv.SetCurrentPosition(&opened)
					srv.SetMainState(wire.ClosureMainStateStopped)
				}
				invoke(t, srv, closuredef.CmdMoveTo, closuredef.MoveToRequest{Position: &fullyOpen})
				if got := mainState(t, srv); got != wire.ClosureMainStateStopped {
					t.Errorf("MainState %d, want Stopped (0) as the drive reported", got)
				}
				if got := target(t, srv).Position; got != nil {
					t.Errorf("OverallTargetState.Position %d, want null: the drive reported arriving", *got)
				}
			})
			t.Run("MoveTo: Error", func(t *testing.T) {
				t.Parallel()
				srv, m := newServer(t, 0)
				m.report = func() { srv.SetMainState(wire.ClosureMainStateError) }
				invoke(t, srv, closuredef.CmdMoveTo, closuredef.MoveToRequest{Position: &fullyOpen})
				if got := mainState(t, srv); got != wire.ClosureMainStateError {
					t.Errorf("MainState %d, want Error (3) as the drive reported", got)
				}
			})
			t.Run("MoveTo: a position on the way keeps Moving and the target", func(t *testing.T) {
				t.Parallel()
				srv, m := newServer(t, 0)
				partly := wire.ClosureCurrentPositionPartiallyOpened
				m.report = func() { srv.SetCurrentPosition(&partly) }
				invoke(t, srv, closuredef.CmdMoveTo, closuredef.MoveToRequest{Position: &fullyOpen})
				if got := mainState(t, srv); got != wire.ClosureMainStateMoving {
					t.Errorf("MainState %d, want Moving (1)", got)
				}
				if got := target(t, srv).Position; got == nil || *got != wire.ClosureTargetPositionMoveToFullyOpen {
					t.Errorf("OverallTargetState.Position %v, want FullyOpen outstanding", got)
				}
			})
			t.Run("MoveTo Latch: the latch reported", func(t *testing.T) {
				t.Parallel()
				srv, m := newServer(t, wire.ClosureControlFeatureMotionLatching)
				latched := true
				m.report = func() { srv.SetCurrentLatch(&latched) }
				invoke(t, srv, closuredef.CmdMoveTo, closuredef.MoveToRequest{Latch: &latched})
				if got := target(t, srv).Latch; got != nil {
					t.Errorf("OverallTargetState.Latch %t, want null: the drive reported latched", *got)
				}
			})
			t.Run("MoveTo: no report", func(t *testing.T) {
				t.Parallel()
				srv, _ := newServer(t, 0)
				invoke(t, srv, closuredef.CmdMoveTo, closuredef.MoveToRequest{Position: &fullyOpen})
				if got := mainState(t, srv); got != wire.ClosureMainStateMoving {
					t.Errorf("MainState %d, want Moving (1)", got)
				}
				if got := target(t, srv).Position; got == nil || *got != wire.ClosureTargetPositionMoveToFullyOpen {
					t.Errorf("OverallTargetState.Position %v, want FullyOpen", got)
				}
			})
			t.Run("Calibrate: Stopped", func(t *testing.T) {
				t.Parallel()
				srv, m := newServer(t, wire.ClosureControlFeatureCalibration)
				m.report = func() {
					srv.SetMainState(wire.ClosureMainStateCalibrating)
					srv.SetMainState(wire.ClosureMainStateStopped)
				}
				invoke(t, srv, closuredef.CmdCalibrate, nil)
				if got := mainState(t, srv); got != wire.ClosureMainStateStopped {
					t.Errorf("MainState %d, want Stopped (0) as the drive reported", got)
				}
				// Not stuck in Calibrating: a MoveTo is accepted.
				m.report = nil
				invoke(t, srv, closuredef.CmdMoveTo, closuredef.MoveToRequest{Position: &fullyOpen})
			})
			t.Run("Calibrate: no report", func(t *testing.T) {
				t.Parallel()
				srv, _ := newServer(t, wire.ClosureControlFeatureCalibration)
				invoke(t, srv, closuredef.CmdCalibrate, nil)
				if got := mainState(t, srv); got != wire.ClosureMainStateCalibrating {
					t.Errorf("MainState %d, want Calibrating (4)", got)
				}
			})
		})
	}
}
