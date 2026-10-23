// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package closure_test

import (
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/closure"
	closuredef "github.com/SukramJ/go-fabric/cluster/spec/closurecontrol"
	clusterwire "github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/contract"
)

// observingMover is a Mover that is also a closuredef.StopObserver.
type observingMover struct {
	recordingHandlers
	ignored   int
	ignoreErr error
}

func (m *observingMover) StopIgnored(context.Context) error {
	m.ignored++
	return m.ignoreErr
}

var _ closuredef.StopObserver = (*observingMover)(nil)

// stopServer is a ControlServer on m in state, with the changes it
// reports counted.
func stopServer(t *testing.T, m closure.Mover, state clusterwire.ClosureMainState) (srv *closure.ControlServer, changes *int) {
	t.Helper()
	srv = closure.NewControlServer(closure.Config{Mover: m})
	srv.SetMainState(state)
	n, ok := any(srv).(contract.AttributeChangeNotifier)
	if !ok {
		t.Fatal("ControlServer reports no attribute changes")
	}
	changes = new(int)
	n.OnAttributeChange(func(contract.Change) { *changes++ })
	return srv, changes
}

func mainState(t *testing.T, srv *closure.ControlServer) uint8 {
	t.Helper()
	raw, ok := srv.MatterRead(clusterwire.ClosureControlAttrMainState)
	if !ok {
		t.Fatal("MainState unreadable")
	}
	return raw.(uint8)
}

// TestClosureControlIdleStopReachesTheObserver is #232: a Stop the server
// ignores (cluster §5.4.8.1) reaches a StopObserver, answers SUCCESS and
// changes neither MainState nor a report.
func TestClosureControlIdleStopReachesTheObserver(t *testing.T) {
	t.Parallel()
	m := &observingMover{}
	srv, changes := stopServer(t, m, clusterwire.ClosureMainStateStopped)
	if _, err := srv.MatterInvoke(context.Background(), clusterwire.ClosureControlCmdStop, nil); err != nil {
		t.Fatalf("idle Stop: %v, want SUCCESS", err)
	}
	if m.ignored != 1 || m.stopped != 0 {
		t.Errorf("StopIgnored %d, Mover.Stop %d; want 1, 0", m.ignored, m.stopped)
	}
	if got := mainState(t, srv); got != uint8(clusterwire.ClosureMainStateStopped) {
		t.Errorf("MainState %d after an idle Stop, want Stopped", got)
	}
	if *changes != 0 {
		t.Errorf("%d changes reported after an idle Stop, want none", *changes)
	}
}

// TestClosureControlIdleStopObserverError answers FAILURE.
func TestClosureControlIdleStopObserverError(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("drive unreachable")
	m := &observingMover{ignoreErr: sentinel}
	srv, changes := stopServer(t, m, clusterwire.ClosureMainStateStopped)
	if _, err := srv.MatterInvoke(context.Background(), clusterwire.ClosureControlCmdStop, nil); !errors.Is(err, sentinel) {
		t.Fatalf("idle Stop: %v, want it to wrap %v", err, sentinel)
	}
	if got := mainState(t, srv); got != uint8(clusterwire.ClosureMainStateStopped) || *changes != 0 {
		t.Errorf("MainState %d, %d changes after a refused idle Stop; want Stopped, none", got, *changes)
	}
}

// TestClosureControlMovingStopSkipsTheObserver: a Stop in motion goes to
// Mover.Stop alone.
func TestClosureControlMovingStopSkipsTheObserver(t *testing.T) {
	t.Parallel()
	m := &observingMover{}
	srv, changes := stopServer(t, m, clusterwire.ClosureMainStateMoving)
	if _, err := srv.MatterInvoke(context.Background(), clusterwire.ClosureControlCmdStop, nil); err != nil {
		t.Fatal(err)
	}
	if m.stopped != 1 || m.ignored != 0 {
		t.Errorf("Mover.Stop %d, StopIgnored %d; want 1, 0", m.stopped, m.ignored)
	}
	if *changes == 0 {
		t.Error("a Stop in motion reported no change: the counter sees nothing")
	}
}

// TestClosureControlIdleStopWithoutObserver keeps the v0.10.1 behaviour:
// SUCCESS, no call, nothing reported.
func TestClosureControlIdleStopWithoutObserver(t *testing.T) {
	t.Parallel()
	h := &recordingHandlers{}
	srv, changes := stopServer(t, h, clusterwire.ClosureMainStateStopped)
	if _, err := srv.MatterInvoke(context.Background(), clusterwire.ClosureControlCmdStop, nil); err != nil {
		t.Fatal(err)
	}
	if h.stopped != 0 || *changes != 0 {
		t.Errorf("Mover.Stop %d, %d changes; want 0, 0", h.stopped, *changes)
	}
}
