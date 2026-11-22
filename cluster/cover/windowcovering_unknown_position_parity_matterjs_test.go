// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// An unknown position reads null on both position attributes of its axis —
// CurrentPosition…Percent100ths and the deprecated CurrentPosition…Percentage
// — and its target reads null too, until the host reports a position.
// Mirrors matter.js at db021da0:
//
//   - the model's defaults: CurrentPositionLiftPercentage and
//     CurrentPositionLiftPercent100ths are "X N", default null
//     (packages/model/src/standard/elements/window-covering-cluster.element.ts:47-48,
//     :71-72), as is the tilt pair (:51-52, :75-76); both targets are "X",
//     default null (:59-64);
//   - the deprecated percentage follows the 100ths value with null passed
//     through (packages/node/src/behaviors/window-covering/WindowCoveringServer.ts:237-240,
//     :255-258);
//   - initialize copies the current position into the target (:138-143), so
//     an unknown position starts with a null target;
//   - UpOrOpen, DownOrClose and GoTo… set the target whatever the position
//     (:523-528, :547-552, :580, :602), and the default handleMovement moves
//     the position to it (:347-363); StopMotion copies the position into the
//     target, null included (:491-496);
//   - OperationalStatus of an axis whose position or target is null is
//     Stopped (#computeOperationalState, :273-275).
package cover_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/clock/clocktest"
	"thingfabric.dev/go-fabric/cluster/cover"
	wcdef "thingfabric.dev/go-fabric/cluster/spec/windowcovering"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/im"
)

// TestParityMatterJS_WindowCoveringSetNullPosition: the host's null for a
// position is stored, and both position attributes of the axis read null
// — matter.js's state takes null (the attribute is "X") and
// #syncLiftCurrentPositions passes it to the percentage
// (WindowCoveringServer.ts:237-240).
func TestParityMatterJS_WindowCoveringSetNullPosition(t *testing.T) {
	t.Parallel()
	s := typedCovering(t)
	var changes [][]uint32
	unsubscribe := s.OnChanged(func(_ wcdef.State, change contract.Change) { changes = append(changes, change.Attributes) })
	defer unsubscribe()
	if err := s.Set(wcdef.Patch{Null: []uint32{wcdef.AttrCurrentPositionLiftPercent100ths}}); err != nil {
		t.Fatalf("a null current position: %v", err)
	}
	st := s.Get()
	if st.CurrentPositionLiftPercent100ths != nil || st.CurrentPositionLiftPercentage != nil {
		t.Errorf("after null: 100ths %v, percentage %v, want both null", st.CurrentPositionLiftPercent100ths, st.CurrentPositionLiftPercentage)
	}
	if st.TargetPositionLiftPercent100ths == nil || *st.TargetPositionLiftPercent100ths != 5000 {
		t.Errorf("after a null position the target is %v, want 5000 unchanged", st.TargetPositionLiftPercent100ths)
	}
	want := []uint32{wcdef.AttrCurrentPositionLiftPercentage, wcdef.AttrCurrentPositionLiftPercent100ths}
	if len(changes) != 1 || len(changes[0]) != 2 || changes[0][0] != want[0] || changes[0][1] != want[1] {
		t.Errorf("changes %v, want [%v]", changes, want)
	}
	// Null again changes nothing.
	if err := s.Set(wcdef.Patch{Null: []uint32{wcdef.AttrCurrentPositionLiftPercent100ths}}); err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 {
		t.Errorf("a second null reported %v", changes[1:])
	}
}

// readPositions reads CurrentPosition…Percent100ths, CurrentPosition…Percentage
// and TargetPosition…Percent100ths of the lift axis, or of the tilt axis.
func readPositions(srv *cover.WindowCoveringServer, tilt bool) (current, percentage, target any) {
	ids := [3]uint32{wcdef.AttrCurrentPositionLiftPercent100ths, wcdef.AttrCurrentPositionLiftPercentage, wcdef.AttrTargetPositionLiftPercent100ths}
	if tilt {
		ids = [3]uint32{wcdef.AttrCurrentPositionTiltPercent100ths, wcdef.AttrCurrentPositionTiltPercentage, wcdef.AttrTargetPositionTiltPercent100ths}
	}
	current, _ = srv.MatterRead(ids[0])
	percentage, _ = srv.MatterRead(ids[1])
	target, _ = srv.MatterRead(ids[2])
	return current, percentage, target
}

// unknownCovering is a lift-and-tilt blind (WindowCoveringServerTest.ts:13-37's
// type) whose two axes start without a known position.
func unknownCovering(t *testing.T, cfg cover.Config) *cover.WindowCoveringServer {
	t.Helper()
	cfg.FeatureMap = cover.ServedLiftTiltFeatureMap
	cfg.Type, cfg.EndProductType = uint8(wcdef.TypeTiltBlindLift), uint8(wcdef.EndProductTypeInteriorVenetianBlind)
	cfg.InitialPositionUnknown, cfg.InitialTiltPositionUnknown = true, true
	srv, err := cover.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

func TestParityMatterJS_WindowCoveringUnknownPosition(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("reads null until the host reports a position", func(t *testing.T) {
		t.Parallel()
		srv := unknownCovering(t, cover.Config{})
		for _, tilt := range []bool{false, true} {
			if c, p, g := readPositions(srv, tilt); c != nil || p != nil || g != nil {
				t.Errorf("tilt %v: current %v, percentage %v, target %v, want all null", tilt, c, p, g)
			}
		}
		lift := uint16(2550)
		srv.SetCurrentPosition(&lift, nil)
		if c, p, g := readPositions(srv, false); c != uint16(2550) || p != uint8(25) || g != nil {
			t.Errorf("after the report: current %v, percentage %v, target %v, want 2550, 25, null", c, p, g)
		}
		if c, p, _ := readPositions(srv, true); c != nil || p != nil {
			t.Errorf("the tilt axis no report reached: current %v, percentage %v, want null", c, p)
		}
		target := uint16(0)
		srv.SetTargetPosition(nil, &target)
		if _, _, g := readPositions(srv, true); g != uint16(0) {
			t.Errorf("a reported tilt target reads %v, want 0", g)
		}
	})

	t.Run("StopMotion keeps the target null", func(t *testing.T) {
		t.Parallel()
		srv := unknownCovering(t, cover.Config{})
		if _, err := srv.MatterInvoke(ctx, wcdef.CmdStopMotion, nil); err != nil {
			t.Fatal(err)
		}
		if _, _, g := readPositions(srv, false); g != nil {
			t.Errorf("target after StopMotion %v, want null", g)
		}
		if v, _ := srv.MatterRead(wcdef.AttrOperationalStatus); v != operationalStatus(0, 0, 0) {
			t.Errorf("OperationalStatus %v, want Stopped", v)
		}
	})

	t.Run("a command moves the covering to its target", func(t *testing.T) {
		t.Parallel()
		srv := unknownCovering(t, cover.Config{})
		if _, err := srv.MatterInvoke(ctx, wcdef.CmdGoToLiftPercentage, wcdef.GoToLiftPercentageRequest{LiftPercent100thsValue: 4000}); err != nil {
			t.Fatal(err)
		}
		if c, p, g := readPositions(srv, false); c != uint16(4000) || p != uint8(40) || g != uint16(4000) {
			t.Errorf("after GoToLiftPercentage: current %v, percentage %v, target %v, want 4000, 40, 4000", c, p, g)
		}
		if _, err := srv.MatterInvoke(ctx, wcdef.CmdDownOrClose, nil); err != nil {
			t.Fatal(err)
		}
		if c, _, g := readPositions(srv, true); c != uint16(10000) || g != uint16(10000) {
			t.Errorf("tilt after DownOrClose: current %v, target %v, want 10000, 10000", c, g)
		}
	})

	t.Run("a travel does not start from an unknown position", func(t *testing.T) {
		t.Parallel()
		srv := unknownCovering(t, cover.Config{MoveStep: time.Second, Clock: clocktest.New(time.Unix(0, 0))})
		if _, err := srv.MatterInvoke(ctx, wcdef.CmdUpOrOpen, nil); err != nil {
			t.Fatal(err)
		}
		if c, p, g := readPositions(srv, false); c != nil || p != nil || g != uint16(0) {
			t.Errorf("after UpOrOpen: current %v, percentage %v, target %v, want null, null, 0", c, p, g)
		}
		if v, _ := srv.MatterRead(wcdef.AttrOperationalStatus); v != operationalStatus(0, 0, 0) {
			t.Errorf("OperationalStatus %v, want Stopped", v)
		}
	})

	t.Run("a Mover's StopMotion stores the null position as the target", func(t *testing.T) {
		t.Parallel()
		m := &recordingMover{}
		srv := unknownCovering(t, cover.Config{Mover: m})
		if _, err := srv.MatterInvoke(ctx, wcdef.CmdGoToLiftPercentage, wcdef.GoToLiftPercentageRequest{LiftPercent100thsValue: 3000}); err != nil {
			t.Fatal(err)
		}
		if c, _, g := readPositions(srv, false); c != nil || g != uint16(3000) {
			t.Errorf("after GoTo: current %v, target %v, want null (the device's to report), 3000", c, g)
		}
		if _, err := srv.MatterInvoke(ctx, wcdef.CmdStopMotion, nil); err != nil {
			t.Fatal(err)
		}
		if _, _, g := readPositions(srv, false); g != nil {
			t.Errorf("target after StopMotion %v, want null", g)
		}
	})

	t.Run("a failed Mover restores the null target", func(t *testing.T) {
		t.Parallel()
		m := &recordingMover{err: errors.New("unreachable")}
		srv := unknownCovering(t, cover.Config{Mover: m})
		_, err := srv.MatterInvoke(ctx, wcdef.CmdUpOrOpen, nil)
		var sc im.StatusCodeError
		if !errors.As(err, &sc) || sc.MatterStatusCode() != im.StatusFailure {
			t.Fatalf("UpOrOpen with a failing Mover: error %v, want FAILURE", err)
		}
		for _, tilt := range []bool{false, true} {
			if _, _, g := readPositions(srv, tilt); g != nil {
				t.Errorf("tilt %v: target after the failure %v, want null again", tilt, g)
			}
		}
	})

	t.Run("a zero initial position is a position", func(t *testing.T) {
		t.Parallel()
		srv, err := cover.New(cover.Config{FeatureMap: cover.ServedFeatureMap})
		if err != nil {
			t.Fatal(err)
		}
		if c, p, g := readPositions(srv, false); c != uint16(0) || p != uint8(0) || g != uint16(0) {
			t.Errorf("current %v, percentage %v, target %v, want 0, 0, 0", c, p, g)
		}
	})
}
