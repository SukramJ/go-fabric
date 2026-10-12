// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package cover_test

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/cover"
	"github.com/SukramJ/go-fabric/cluster/spec"
	wcdef "github.com/SukramJ/go-fabric/cluster/spec/windowcovering"
	clusterwire "github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/im"
)

// moverCall is one call a recordingMover saw: "goto" with the lift target
// (tilt must be nil), or "stop".
type moverCall struct {
	op   string
	lift uint16
}

type recordingMover struct {
	mu    sync.Mutex
	calls []moverCall
	err   error
	// tiltSeen is set when GoTo was ever handed a tilt target.
	tiltSeen bool
	// srv, when set, is told the new position from inside GoTo — the
	// inbound setters may be called from the Mover.
	srv *cover.WindowCoveringServer
}

func (m *recordingMover) GoTo(_ context.Context, lift, tilt *uint16) error {
	m.mu.Lock()
	c := moverCall{op: "goto"}
	if lift != nil {
		c.lift = *lift
	}
	if tilt != nil {
		m.tiltSeen = true
	}
	m.calls = append(m.calls, c)
	srv, err := m.srv, m.err
	m.mu.Unlock()
	if srv != nil && err == nil && lift != nil {
		srv.SetCurrentPosition(lift, nil)
	}
	return err
}

func (m *recordingMover) Stop(context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, moverCall{op: "stop"})
	return m.err
}

func (m *recordingMover) recorded() []moverCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]moverCall(nil), m.calls...)
}

func newMoverServer(t *testing.T, m *recordingMover, initial uint16) *cover.WindowCoveringServer {
	t.Helper()
	srv, err := cover.New(cover.Config{InitialPositionPercent100ths: initial, Mover: m})
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

func readU16(t *testing.T, srv *cover.WindowCoveringServer, attrID uint32) uint16 {
	t.Helper()
	v, ok := srv.MatterRead(attrID)
	if !ok {
		t.Fatalf("MatterRead(0x%04X): ok=false", attrID)
	}
	return v.(uint16)
}

// TestMoverGetsOneGoToPerCommand: each moving command is one GoTo with the
// target the server inferred — UpOrOpen 0, DownOrClose 10000,
// GoToLiftPercentage the value (matter.js WindowCoveringServer.ts:518-530,
// :542-554, :576-584 at 4633bfd7) — the target attribute holds it, and the
// server does not move the lift itself (handleMovement is the host's,
// :328, :338).
func TestMoverGetsOneGoToPerCommand(t *testing.T) {
	t.Parallel()
	m := &recordingMover{}
	srv := newMoverServer(t, m, 5000)
	ctx := context.Background()
	for _, step := range []struct {
		cmd    uint32
		fields any
		want   uint16
	}{
		{wcdef.CmdUpOrOpen, nil, 0},
		{clusterwire.WindowCoveringCmdDownOrClose, nil, 10000},
		{clusterwire.WindowCoveringCmdGoToLiftPercentage, wcdef.GoToLiftPercentageRequest{LiftPercent100thsValue: 3500}, 3500},
	} {
		if _, err := srv.MatterInvoke(ctx, step.cmd, step.fields); err != nil {
			t.Fatalf("command 0x%02X: %v", step.cmd, err)
		}
		if got := readU16(t, srv, clusterwire.WindowCoveringAttrTargetPositionLiftPercent100ths); got != step.want {
			t.Errorf("command 0x%02X: target %d, want %d", step.cmd, got, step.want)
		}
		if got := readU16(t, srv, clusterwire.WindowCoveringAttrCurrentPositionLiftPercent100ths); got != 5000 {
			t.Errorf("command 0x%02X: current %d, want 5000 (the device moves, not the server)", step.cmd, got)
		}
	}
	want := []moverCall{{"goto", 0}, {"goto", 10000}, {"goto", 3500}}
	if got := m.recorded(); !reflect.DeepEqual(got, want) {
		t.Errorf("Mover calls %v, want %v", got, want)
	}
	if m.tiltSeen {
		t.Error("GoTo was handed a tilt target on a lift-only server")
	}
}

// TestMoverStopSetsTargetToCurrent: StopMotion calls Stop, and the target
// follows the current position (matter.js handleStopMovement,
// WindowCoveringServer.ts:491-493 at 4633bfd7).
func TestMoverStopSetsTargetToCurrent(t *testing.T) {
	t.Parallel()
	m := &recordingMover{}
	srv := newMoverServer(t, m, 2000)
	ctx := context.Background()
	if _, err := srv.MatterInvoke(ctx, clusterwire.WindowCoveringCmdDownOrClose, nil); err != nil {
		t.Fatal(err)
	}
	srv.SetCurrentPosition(new(uint16(4200)), nil)
	if _, err := srv.MatterInvoke(ctx, clusterwire.WindowCoveringCmdStopMotion, nil); err != nil {
		t.Fatal(err)
	}
	if got := readU16(t, srv, clusterwire.WindowCoveringAttrTargetPositionLiftPercent100ths); got != 4200 {
		t.Errorf("target after StopMotion %d, want 4200", got)
	}
	want := []moverCall{{"goto", 10000}, {"stop", 0}}
	if got := m.recorded(); !reflect.DeepEqual(got, want) {
		t.Errorf("Mover calls %v, want %v", got, want)
	}
}

// TestMoverErrorAnswersFailureAndRestoresTheTarget: a refused movement
// answers FAILURE and the target attribute goes back to what it held.
func TestMoverErrorAnswersFailureAndRestoresTheTarget(t *testing.T) {
	t.Parallel()
	m := &recordingMover{err: errors.New("device unreachable")}
	srv := newMoverServer(t, m, 6000)
	ctx := context.Background()
	for _, cmd := range []uint32{wcdef.CmdUpOrOpen, clusterwire.WindowCoveringCmdStopMotion} {
		_, err := srv.MatterInvoke(ctx, cmd, nil)
		var sc im.StatusCodeError
		if !errors.As(err, &sc) || sc.MatterStatusCode() != im.StatusFailure {
			t.Fatalf("command 0x%02X: error %v, want FAILURE", cmd, err)
		}
		if got := readU16(t, srv, clusterwire.WindowCoveringAttrTargetPositionLiftPercent100ths); got != 6000 {
			t.Errorf("command 0x%02X: target %d after a refusal, want 6000", cmd, got)
		}
	}
	// A command the server refuses itself never reaches the Mover.
	_, err := srv.MatterInvoke(ctx, clusterwire.WindowCoveringCmdGoToLiftPercentage, uint16(10001))
	var sc im.StatusCodeError
	if !errors.As(err, &sc) || sc.MatterStatusCode() != im.StatusConstraintError {
		t.Errorf("GoToLiftPercentage 10001: error %v, want CONSTRAINT_ERROR", err)
	}
	if n := len(m.recorded()); n != 2 {
		t.Errorf("Mover called %d times, want 2", n)
	}
}

// TestInboundSettersReportWithoutTheMover: the device's position and
// status are stored and reported through the change notifier, without a
// Mover call; tilt is accepted and unused; a position above "max 10000"
// is clamped.
func TestInboundSettersReportWithoutTheMover(t *testing.T) {
	t.Parallel()
	m := &recordingMover{}
	srv := newMoverServer(t, m, 0)
	var mu sync.Mutex
	var reported []uint32
	unsub := srv.OnMatterAttributesChanged(func(ids []uint32) {
		mu.Lock()
		reported = append(reported, ids...)
		mu.Unlock()
	})
	defer unsub()

	srv.SetCurrentPosition(new(uint16(2550)), new(uint16(9999)))
	srv.SetCurrentPosition(nil, nil)                // nothing to record
	srv.SetOperationalStatus(0b0000_1010)           // global Closing, lift Closing
	srv.SetOperationalStatus(0b0000_1010)           // unchanged: not reported
	srv.SetCurrentPosition(new(uint16(12000)), nil) // clamped to 10000
	if got := readU16(t, srv, clusterwire.WindowCoveringAttrCurrentPositionLiftPercent100ths); got != 10000 {
		t.Errorf("current %d, want 10000", got)
	}
	if v, _ := srv.MatterRead(clusterwire.WindowCoveringAttrCurrentPositionLiftPercentage); v != uint8(100) {
		t.Errorf("CurrentPositionLiftPercentage %v, want 100", v)
	}
	if v, _ := srv.MatterRead(clusterwire.WindowCoveringAttrOperationalStatus); v != uint8(0b0000_1010) {
		t.Errorf("OperationalStatus %v, want 0x0A", v)
	}
	if n := len(m.recorded()); n != 0 {
		t.Errorf("Mover called %d times by the inbound setters, want 0", n)
	}
	mu.Lock()
	got := append([]uint32(nil), reported...)
	mu.Unlock()
	pos := []uint32{clusterwire.WindowCoveringAttrCurrentPositionLiftPercent100ths, clusterwire.WindowCoveringAttrCurrentPositionLiftPercentage}
	want := append(append(append([]uint32(nil), pos...), clusterwire.WindowCoveringAttrOperationalStatus), pos...)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("reported %v, want %v", got, want)
	}
}

// TestMoverMayReportFromInsideGoTo: the inbound setters do not wait for
// the command that called the Mover.
func TestMoverMayReportFromInsideGoTo(t *testing.T) {
	t.Parallel()
	m := &recordingMover{}
	srv := newMoverServer(t, m, 0)
	m.mu.Lock()
	m.srv = srv
	m.mu.Unlock()
	if _, err := srv.MatterInvoke(context.Background(), clusterwire.WindowCoveringCmdGoToLiftPercentage, uint16(7000)); err != nil {
		t.Fatal(err)
	}
	if got := readU16(t, srv, clusterwire.WindowCoveringAttrCurrentPositionLiftPercent100ths); got != 7000 {
		t.Errorf("current %d, want 7000", got)
	}
}

// TestNewRefusesMoverWithMoveStep: the device moves; the server does not
// also simulate the travel.
func TestNewRefusesMoverWithMoveStep(t *testing.T) {
	t.Parallel()
	_, err := cover.New(cover.Config{Mover: &recordingMover{}, MoveStep: time.Second})
	if !errors.Is(err, cover.ErrMoverWithMoveStep) {
		t.Errorf("New error %v, want ErrMoverWithMoveStep", err)
	}
}

// TestMoverPathRefusesWhatTheServerRefuses: an unknown command and an
// OperationalStatus above "max 63" (window-covering-cluster.element.ts:55-56)
// — the reserved bits are dropped.
func TestMoverPathRefusesWhatTheServerRefuses(t *testing.T) {
	t.Parallel()
	m := &recordingMover{}
	srv := newMoverServer(t, m, 0)
	if _, err := srv.MatterInvoke(context.Background(), 0x7F, nil); err == nil {
		t.Error("unknown command accepted")
	}
	srv.SetOperationalStatus(0xC5)
	if v, _ := srv.MatterRead(clusterwire.WindowCoveringAttrOperationalStatus); v != uint8(0x05) {
		t.Errorf("OperationalStatus %v, want 0x05", v)
	}
	if n := len(m.recorded()); n != 0 {
		t.Errorf("Mover called %d times, want 0", n)
	}
}

// TestMoverStatusErrorPassesThrough: a Mover refusal that carries an IM
// status answers with it (the rule thermo.Sink follows too), and the
// target goes back.
func TestMoverStatusErrorPassesThrough(t *testing.T) {
	t.Parallel()
	m := &recordingMover{err: spec.Errorf(im.StatusBusy, "motor busy")}
	srv := newMoverServer(t, m, 3000)
	for _, cmd := range []uint32{clusterwire.WindowCoveringCmdDownOrClose, clusterwire.WindowCoveringCmdStopMotion} {
		_, err := srv.MatterInvoke(context.Background(), cmd, nil)
		var sc im.StatusCodeError
		if !errors.As(err, &sc) || sc.MatterStatusCode() != im.StatusBusy {
			t.Errorf("command 0x%02X: error %v, want BUSY", cmd, err)
		}
		if got := readU16(t, srv, clusterwire.WindowCoveringAttrTargetPositionLiftPercent100ths); got != 3000 {
			t.Errorf("command 0x%02X: target %d after a refusal, want 3000", cmd, got)
		}
	}
}
