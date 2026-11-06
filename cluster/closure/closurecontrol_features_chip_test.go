// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package closure_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/closure"
	"github.com/SukramJ/go-fabric/cluster/spec"
	closuredef "github.com/SukramJ/go-fabric/cluster/spec/closurecontrol"
	clusterwire "github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/im"
)

// The MotionLatching, Instantaneous, Speed and Calibration rules, ported
// by meaning from connectedhomeip
// src/app/clusters/closure-control-server/tests/TestClosureControlCluster.cpp
// at 6170af84 — the server is ahead of matter.js, whose
// ClosureControlServer.ts:17 is the empty subclass
// (notes/parity/by_design.md BD-chip-ClosureControl-RulesInServer).

// fullMover is a Mover with every capability interface; calls records
// what the server forwarded, in order.
type fullMover struct {
	modes closuredef.LatchControlModesBitmap
	calls []string
	err   error
}

var (
	_ closuredef.Latcher    = (*fullMover)(nil)
	_ closuredef.SpeedMover = (*fullMover)(nil)
	_ closuredef.Calibrator = (*fullMover)(nil)
)

func (m *fullMover) record(call string) error {
	if m.err != nil {
		return m.err
	}
	m.calls = append(m.calls, call)
	return nil
}

func (m *fullMover) MoveTo(_ context.Context, p closuredef.TargetPositionEnum) error {
	return m.record("move" + string(rune('0'+p)))
}
func (m *fullMover) Stop(context.Context) error { return m.record("stop") }
func (m *fullMover) LatchControlModes() closuredef.LatchControlModesBitmap {
	return m.modes
}

func (m *fullMover) Latch(_ context.Context, latch bool) error {
	if latch {
		return m.record("latch")
	}
	return m.record("unlatch")
}

func (m *fullMover) SetSpeed(_ context.Context, sp closuredef.ThreeLevelAutoEnum) error {
	return m.record("speed" + string(rune('0'+sp)))
}
func (m *fullMover) Calibrate(context.Context) error { return m.record("calibrate") }

const (
	fPS = clusterwire.ClosureControlFeaturePositioning
	fLT = clusterwire.ClosureControlFeatureMotionLatching
	fIS = clusterwire.ClosureControlFeatureInstantaneous
	fSP = clusterwire.ClosureControlFeatureSpeed
	fCL = clusterwire.ClosureControlFeatureCalibration
)

const bothLatchModes = closuredef.LatchControlModesRemoteLatching | closuredef.LatchControlModesRemoteUnlatching

func newFeatured(t *testing.T, fm uint32, m *fullMover) *closure.ControlServer {
	t.Helper()
	s, err := closure.New(closure.Config{FeatureMap: fm, Mover: m})
	if err != nil {
		t.Fatalf("New(0x%X): %v", fm, err)
	}
	return ready(s)
}

func invokeStatus(t *testing.T, s *closure.ControlServer, cmd uint32, fields any) im.StatusCode {
	t.Helper()
	_, err := s.MatterInvoke(context.Background(), cmd, fields)
	if err == nil {
		return im.StatusSuccess
	}
	if sce, ok := errors.AsType[im.StatusCodeError](err); ok {
		return sce.MatterStatusCode()
	}
	return im.StatusFailure
}

func moveTo(pos *closuredef.TargetPositionEnum, latch *bool, speed *closuredef.ThreeLevelAutoEnum) closuredef.MoveToRequest {
	return closuredef.MoveToRequest{Position: pos, Latch: latch, Speed: speed}
}

// TestClosureControlCapabilityRefusalNamesTheInterface: a declared LT, SP
// or CL on a mover without the interface is ErrFeatureMap naming it — the
// brief's port rule; Speed with Instantaneous is the definition's
// conformance refusal (chip's constructor dies on it,
// ClosureControlCluster.cpp:49-53).
func TestClosureControlCapabilityRefusalNamesTheInterface(t *testing.T) {
	t.Parallel()
	for fm, iface := range map[uint32]string{
		fPS | fLT: "closurecontrol.Latcher",
		fPS | fSP: "closurecontrol.SpeedMover",
		fPS | fCL: "closurecontrol.Calibrator",
	} {
		_, err := closure.New(closure.Config{FeatureMap: fm, Mover: &recordingHandlers{}})
		if !errors.Is(err, closure.ErrFeatureMap) || !strings.Contains(err.Error(), iface) {
			t.Errorf("FeatureMap 0x%X on a plain Mover: %v, want ErrFeatureMap naming %s", fm, err, iface)
		}
	}
	if _, err := closure.New(closure.Config{FeatureMap: fPS | fSP | fIS, Mover: &fullMover{}}); err == nil {
		t.Error("Speed with Instantaneous accepted, want the conformance refusal")
	}
	s, err := closure.New(closure.Config{FeatureMap: fPS | fLT | fSP | fCL, Mover: &fullMover{}})
	if err != nil || s.FeatureMap() != fPS|fLT|fSP|fCL {
		t.Fatalf("PS|LT|SP|CL on a full mover: %v, FeatureMap %v", err, s)
	}
}

// TestClosureControlFeatureLists ports TestCalibrationFeatureMapAndAcceptedCommands,
// TestInstantaneousFeatureMapAndAcceptedCommands and
// TestConformanceMotionLatchingExposesLatchControlModes (without
// GroupedMoveTo, which the matter.js snapshot does not carry).
func TestClosureControlFeatureLists(t *testing.T) {
	t.Parallel()
	cl := newFeatured(t, fPS|fCL, &fullMover{})
	if got := cl.MatterAcceptedCommands(); !slices.Equal(got, []uint32{0x00, 0x01, 0x02}) {
		t.Errorf("PS|CL accepted = %v, want Stop, MoveTo, Calibrate", got)
	}
	is := newFeatured(t, fPS|fIS, &fullMover{})
	if got := is.MatterAcceptedCommands(); !slices.Equal(got, []uint32{0x01}) {
		t.Errorf("PS|IS accepted = %v, want MoveTo alone", got)
	}
	if slices.Contains(is.MatterEvents(), clusterwire.ClosureControlEventMovementCompleted) {
		t.Error("PS|IS lists MovementCompleted")
	}
	lt := newFeatured(t, fPS|fLT, &fullMover{modes: closuredef.LatchControlModesRemoteLatching})
	if !slices.Contains(lt.MatterAttributes(), clusterwire.ClosureControlAttrLatchControlModes) {
		t.Error("PS|LT does not list LatchControlModes")
	}
	if v, ok := lt.MatterRead(clusterwire.ClosureControlAttrLatchControlModes); !ok || v != uint8(1) {
		t.Errorf("LatchControlModes = %v, %v; want 1 (the Latcher's, fixed)", v, ok)
	}
	if _, ok := cl.MatterRead(clusterwire.ClosureControlAttrLatchControlModes); ok {
		t.Error("LatchControlModes answered without MotionLatching")
	}
}

// TestClosureControlHandleCalibrateChip ports TestHandleCalibrate and
// TestHandleCalibrateDelegateFailure (ClosureControlCluster.cpp:642-665).
func TestClosureControlHandleCalibrateChip(t *testing.T) {
	t.Parallel()
	if got := invokeStatus(t, newFeatured(t, fPS, &fullMover{}), clusterwire.ClosureControlCmdCalibrate, nil); got != im.StatusUnsupportedCommand {
		t.Errorf("Calibrate without CL = %v, want UNSUPPORTED_COMMAND", got)
	}
	m := &fullMover{}
	s := newFeatured(t, fPS|fCL, m)
	s.SetMainState(clusterwire.ClosureMainStateMoving)
	if got := invokeStatus(t, s, clusterwire.ClosureControlCmdCalibrate, nil); got != im.StatusInvalidInState {
		t.Errorf("Calibrate while Moving = %v, want INVALID_IN_STATE", got)
	}
	s.SetMainState(clusterwire.ClosureMainStateStopped)
	if got := invokeStatus(t, s, clusterwire.ClosureControlCmdCalibrate, nil); got != im.StatusSuccess {
		t.Errorf("Calibrate while Stopped = %v", got)
	}
	if st, _ := s.MatterRead(clusterwire.ClosureControlAttrMainState); st != uint8(clusterwire.ClosureMainStateCalibrating) {
		t.Errorf("MainState after Calibrate = %v, want Calibrating", st)
	}
	// Already Calibrating: SUCCESS without another call (chip :650).
	if got := invokeStatus(t, s, clusterwire.ClosureControlCmdCalibrate, nil); got != im.StatusSuccess || !slices.Equal(m.calls, []string{"calibrate"}) {
		t.Errorf("Calibrate while Calibrating = %v, calls %v; want SUCCESS and one call", got, m.calls)
	}
	// SetupRequired is a starting state too (chip :655).
	sr := &fullMover{}
	s2, _ := closure.New(closure.Config{FeatureMap: fPS | fCL, Mover: sr})
	if got := invokeStatus(t, s2, clusterwire.ClosureControlCmdCalibrate, nil); got != im.StatusSuccess {
		t.Errorf("Calibrate in SetupRequired = %v", got)
	}
	failing := &fullMover{err: errors.New("busy")}
	s3 := newFeatured(t, fPS|fCL, failing)
	if got := invokeStatus(t, s3, clusterwire.ClosureControlCmdCalibrate, nil); got == im.StatusSuccess {
		t.Error("a failing Calibrator answered SUCCESS")
	}
	if st, _ := s3.MatterRead(clusterwire.ClosureControlAttrMainState); st != uint8(clusterwire.ClosureMainStateStopped) {
		t.Errorf("MainState after a refused Calibrate = %v, want Stopped", st)
	}
	// Stop ends a calibration (chip :541).
	s.SetMainState(clusterwire.ClosureMainStateCalibrating)
	if got := invokeStatus(t, s, clusterwire.ClosureControlCmdStop, nil); got != im.StatusSuccess {
		t.Errorf("Stop while Calibrating = %v", got)
	}
}

// TestClosureControlInstantaneousStopChip ports
// TestHandleStopFeatureAndDelegateFailure's first half (chip :535).
func TestClosureControlInstantaneousStopChip(t *testing.T) {
	t.Parallel()
	m := &fullMover{}
	s := newFeatured(t, fPS|fIS, m)
	s.SetMainState(clusterwire.ClosureMainStateMoving)
	if got := invokeStatus(t, s, clusterwire.ClosureControlCmdStop, nil); got != im.StatusUnsupportedCommand || len(m.calls) != 0 {
		t.Errorf("Stop with IS = %v, calls %v; want UNSUPPORTED_COMMAND and no call", got, m.calls)
	}
}

// TestClosureControlMoveToAllFeaturesChip ports TestHandleMoveToAllFeatures:
// Position, Latch false and Speed High on a latched closure are accepted,
// all three land in OverallTargetState, MainState is Moving; the mover
// hears unlatch, speed, motion in that order.
func TestClosureControlMoveToAllFeaturesChip(t *testing.T) {
	t.Parallel()
	m := &fullMover{modes: bothLatchModes}
	s := newFeatured(t, fPS|fLT|fSP, m)
	partial := clusterwire.ClosureCurrentPositionPartiallyOpened
	s.SetCurrentPosition(&partial)
	s.SetCurrentLatch(ptr(true))
	s.SetCurrentSpeed(closuredef.ThreeLevelAutoLow)
	req := moveTo(ptr(closuredef.TargetPositionMoveToFullyOpen), ptr(false), ptr(closuredef.ThreeLevelAutoHigh))
	if got := invokeStatus(t, s, clusterwire.ClosureControlCmdMoveTo, req); got != im.StatusSuccess {
		t.Fatalf("MoveTo = %v", got)
	}
	if want := []string{"unlatch", "speed3", "move1"}; !slices.Equal(m.calls, want) {
		t.Errorf("mover calls = %v, want %v", m.calls, want)
	}
	tgt := readOverallTarget(t, s)
	if tgt.Position == nil || *tgt.Position != clusterwire.ClosureTargetPositionMoveToFullyOpen ||
		!tgt.HasLatch || tgt.Latch == nil || *tgt.Latch || tgt.Speed == nil || *tgt.Speed != closuredef.ThreeLevelAutoHigh {
		t.Errorf("OverallTargetState = %+v", tgt)
	}
	if st, _ := s.MatterRead(clusterwire.ClosureControlAttrMainState); st != uint8(clusterwire.ClosureMainStateMoving) {
		t.Errorf("MainState = %v, want Moving", st)
	}
	cur := readOverallCurrent(t, s)
	if !cur.HasLatch || cur.Latch == nil || !*cur.Latch || cur.Speed == nil || *cur.Speed != closuredef.ThreeLevelAutoLow {
		t.Errorf("OverallCurrentState = %+v", cur)
	}
	// A latch after the motion: latch last.
	m.calls = nil
	s.SetCurrentLatch(ptr(false))
	if got := invokeStatus(t, s, clusterwire.ClosureControlCmdMoveTo, moveTo(ptr(closuredef.TargetPositionMoveToFullyClosed), ptr(true), nil)); got != im.StatusSuccess {
		t.Fatalf("MoveTo close+latch = %v", got)
	}
	if want := []string{"move0", "latch"}; !slices.Equal(m.calls, want) {
		t.Errorf("mover calls = %v, want %v", m.calls, want)
	}
}

// TestClosureControlMoveToLatchedNeedsUnlatchChip ports
// TestHandleMoveToLatchedPositionChangeRequiresUnlatch (chip :607-620).
func TestClosureControlMoveToLatchedNeedsUnlatchChip(t *testing.T) {
	t.Parallel()
	m := &fullMover{modes: bothLatchModes}
	s := newFeatured(t, fPS|fLT, m)
	s.SetCurrentLatch(ptr(true))
	open := ptr(closuredef.TargetPositionMoveToFullyOpen)
	for _, c := range []struct {
		latch *bool
		want  im.StatusCode
	}{{nil, im.StatusInvalidInState}, {ptr(true), im.StatusInvalidInState}, {ptr(false), im.StatusSuccess}} {
		if got := invokeStatus(t, s, clusterwire.ClosureControlCmdMoveTo, moveTo(open, c.latch, nil)); got != c.want {
			t.Errorf("MoveTo FullyOpen latch %v on a latched closure = %v, want %v", c.latch, got, c.want)
		}
	}
}

// TestClosureControlMoveToRemoteLatchingChip ports
// TestHandleMoveToRemoteLatchingConformanceChecks (chip :579-590): a
// Latch the LatchControlModes do not allow remotely is INVALID_IN_STATE
// and reaches no mover.
func TestClosureControlMoveToRemoteLatchingChip(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		modes closuredef.LatchControlModesBitmap
		latch bool
		want  im.StatusCode
	}{
		{closuredef.LatchControlModesRemoteLatching, false, im.StatusInvalidInState},
		{closuredef.LatchControlModesRemoteLatching, true, im.StatusSuccess},
		{closuredef.LatchControlModesRemoteUnlatching, true, im.StatusInvalidInState},
		{closuredef.LatchControlModesRemoteUnlatching, false, im.StatusSuccess},
		{0, true, im.StatusInvalidInState},
		{0, false, im.StatusInvalidInState},
	} {
		m := &fullMover{modes: c.modes}
		s := newFeatured(t, fPS|fLT, m)
		got := invokeStatus(t, s, clusterwire.ClosureControlCmdMoveTo, moveTo(nil, ptr(c.latch), nil))
		if got != c.want || (got != im.StatusSuccess && len(m.calls) != 0) {
			t.Errorf("modes %d latch %t = %v, calls %v; want %v", c.modes, c.latch, got, m.calls, c.want)
		}
	}
}

// TestClosureControlMoveToConstraintChip ports
// TestHandleMoveToConstraintValidation (chip :572-597) and the rule that a
// field of an unadvertised feature is ignored (chip :579, :592).
func TestClosureControlMoveToConstraintChip(t *testing.T) {
	t.Parallel()
	m := &fullMover{}
	s := newFeatured(t, fPS|fLT|fSP, m)
	if got := invokeStatus(t, s, clusterwire.ClosureControlCmdMoveTo, moveTo(ptr(closuredef.TargetPositionEnum(9)), nil, nil)); got != im.StatusConstraintError {
		t.Errorf("unknown Position = %v, want CONSTRAINT_ERROR", got)
	}
	if got := invokeStatus(t, s, clusterwire.ClosureControlCmdMoveTo, moveTo(nil, nil, ptr(closuredef.ThreeLevelAutoEnum(4)))); got != im.StatusConstraintError {
		t.Errorf("unknown Speed = %v, want CONSTRAINT_ERROR", got)
	}
	// Speed alone: accepted, recorded, Moving (chip :592-597, :626-635).
	if got := invokeStatus(t, s, clusterwire.ClosureControlCmdMoveTo, moveTo(nil, nil, ptr(closuredef.ThreeLevelAutoMedium))); got != im.StatusSuccess || !slices.Equal(m.calls, []string{"speed2"}) {
		t.Errorf("Speed alone = %v, calls %v", got, m.calls)
	}
	// Latch and Speed without their features: ignored; the request falls
	// back to OverallTargetState.Position, null here, so nothing moves.
	plain := &fullMover{}
	ps := newFeatured(t, fPS, plain)
	if got := invokeStatus(t, ps, clusterwire.ClosureControlCmdMoveTo, moveTo(nil, ptr(true), ptr(closuredef.ThreeLevelAutoHigh))); got != im.StatusSuccess || len(plain.calls) != 0 {
		t.Errorf("unserved Latch/Speed = %v, calls %v; want SUCCESS and nothing forwarded", got, plain.calls)
	}
	if tgt := readOverallTarget(t, ps); tgt.HasLatch || tgt.Speed != nil {
		t.Errorf("PS-only OverallTargetState carries Latch/Speed: %+v", tgt)
	}
}

// TestClosureControlSecureStateWithLatchChip ports
// TestSetOverallCurrentStateSecureStateValidation's rule as a derivation:
// with MotionLatching SecureState is true only FullyClosed and latched
// (chip :364-386), unknown with an unknown latch.
func TestClosureControlSecureStateWithLatchChip(t *testing.T) {
	t.Parallel()
	s := newFeatured(t, fPS|fLT, &fullMover{modes: bothLatchModes})
	closed := clusterwire.ClosureCurrentPositionFullyClosed
	s.SetCurrentPosition(&closed)
	if cur := readOverallCurrent(t, s); cur.SecureState != nil {
		t.Errorf("SecureState with an unknown latch = %v, want null", *cur.SecureState)
	}
	s.SetCurrentLatch(ptr(false))
	if cur := readOverallCurrent(t, s); cur.SecureState == nil || *cur.SecureState {
		t.Errorf("SecureState closed, unlatched = %v, want false", cur.SecureState)
	}
	s.SetCurrentLatch(ptr(true))
	if cur := readOverallCurrent(t, s); cur.SecureState == nil || !*cur.SecureState {
		t.Errorf("SecureState closed, latched = %v, want true", cur.SecureState)
	}
}

// TestClosureControlSetAttributesLatchSpeedChip ports
// TestSetOverallCurrentStateFeatureValidation (chip :342-360): the
// drive's report carries Latch and Speed only with their features; a
// reached latch clears OverallTargetState's Latch.
func TestClosureControlSetAttributesLatchSpeedChip(t *testing.T) {
	t.Parallel()
	ps := newFeatured(t, fPS, &fullMover{})
	for _, st := range []closuredef.OverallCurrentStateStruct{
		{Latch: &spec.Nullable[bool]{Value: true}},
		{Speed: ptr(closuredef.ThreeLevelAutoLow)},
	} {
		if err := ps.SetAttributes(map[uint32]any{clusterwire.ClosureControlAttrOverallCurrentState: st}); !errors.Is(err, spec.ErrInvalidValue) {
			t.Errorf("PS-only report %+v: %v, want ErrInvalidValue", st, err)
		}
	}
	m := &fullMover{modes: bothLatchModes}
	s := newFeatured(t, fPS|fLT|fSP, m)
	if got := invokeStatus(t, s, clusterwire.ClosureControlCmdMoveTo, moveTo(nil, ptr(true), nil)); got != im.StatusSuccess {
		t.Fatalf("MoveTo latch = %v", got)
	}
	err := s.SetAttributes(map[uint32]any{clusterwire.ClosureControlAttrOverallCurrentState: closuredef.OverallCurrentStateStruct{
		Position: &spec.Nullable[closuredef.CurrentPositionEnum]{Value: closuredef.CurrentPositionFullyClosed},
		Latch:    &spec.Nullable[bool]{Value: true},
		Speed:    ptr(closuredef.ThreeLevelAutoHigh),
	}})
	if err != nil {
		t.Fatal(err)
	}
	cur, tgt := readOverallCurrent(t, s), readOverallTarget(t, s)
	if cur.Latch == nil || !*cur.Latch || cur.Speed == nil || *cur.Speed != closuredef.ThreeLevelAutoHigh || cur.SecureState == nil || !*cur.SecureState {
		t.Errorf("OverallCurrentState = %+v", cur)
	}
	if tgt.Latch != nil {
		t.Errorf("OverallTargetState.Latch = %v after the latch was reached, want null", *tgt.Latch)
	}
	// SetCurrentLatch and SetCurrentSpeed change nothing without their
	// features.
	ps.SetCurrentLatch(ptr(true))
	ps.SetCurrentSpeed(closuredef.ThreeLevelAutoHigh)
	if cur := readOverallCurrent(t, ps); cur.Latch != nil || cur.Speed != nil {
		t.Errorf("PS-only current state = %+v", cur)
	}
}

// TestClosureControlStateReadsRenderAlike: two reads of the state structs
// render alike — the bridge compares rendered values before and after a
// command to decide what to report (bridge reportInvokeChanges), so a
// fresh pointer per read would report an unchanged OverallCurrentState
// after every command, which TC-CLCTRL-5.1 step 4i reads as the latch it
// waits for.
func TestClosureControlStateReadsRenderAlike(t *testing.T) {
	t.Parallel()
	s := newFeatured(t, fPS|fLT|fSP, &fullMover{modes: bothLatchModes})
	s.SetCurrentLatch(ptr(false))
	latch := true
	wrap := func(v any) string { return fmt.Sprintf("%#v", struct{ V any }{v}) }
	beforeCmd, _ := s.MatterRead(clusterwire.ClosureControlAttrOverallCurrentState)
	if _, err := s.MatterInvoke(context.Background(), clusterwire.ClosureControlCmdMoveTo, moveTo(nil, &latch, nil)); err != nil {
		t.Fatal(err)
	}
	// A command that moves only the target leaves OverallCurrentState
	// rendering as before (bridge reportInvokeChanges).
	if afterCmd, _ := s.MatterRead(clusterwire.ClosureControlAttrOverallCurrentState); wrap(beforeCmd) != wrap(afterCmd) {
		t.Error("a MoveTo of the latch alone renders OverallCurrentState as changed")
	}
	before, _ := s.MatterRead(clusterwire.ClosureControlAttrOverallCurrentState)
	s.SetCurrentLatch(ptr(true))
	if after, _ := s.MatterRead(clusterwire.ClosureControlAttrOverallCurrentState); fmt.Sprintf("%#v", struct{ V any }{before}) == fmt.Sprintf("%#v", struct{ V any }{after}) {
		t.Error("a changed OverallCurrentState renders as before")
	}
	// An equal state set again — fresh pointers from the drive — renders
	// as the state before (bridge pathValues keeps an older rendering as
	// its baseline).
	deref := func(v any) string { return fmt.Sprintf("%#v", *v.(*clusterwire.ClosureOverallCurrentState)) }
	again, _ := s.MatterRead(clusterwire.ClosureControlAttrOverallCurrentState)
	s.SetCurrentLatch(ptr(true))
	s.SetCurrentSpeed(closuredef.ThreeLevelAutoAuto)
	if now, _ := s.MatterRead(clusterwire.ClosureControlAttrOverallCurrentState); deref(again) != deref(now) {
		t.Errorf("an equal state renders differently: %s / %s", deref(again), deref(now))
	}
	for _, id := range []uint32{clusterwire.ClosureControlAttrOverallCurrentState, clusterwire.ClosureControlAttrOverallTargetState} {
		// The bridge renders the value wrapped (im.AttributeValue), so a
		// pointer renders as its address.
		a, _ := s.MatterRead(id)
		b, _ := s.MatterRead(id)
		if fmt.Sprintf("%#v", struct{ V any }{a}) != fmt.Sprintf("%#v", struct{ V any }{b}) {
			t.Errorf("attribute 0x%X renders differently on two reads: %#v / %#v", id, a, b)
		}
	}
}
