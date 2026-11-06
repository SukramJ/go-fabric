// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	ccldef "github.com/SukramJ/go-fabric/cluster/spec/closurecontrol"
	valvedef "github.com/SukramJ/go-fabric/cluster/spec/valveconfigurationandcontrol"
	"github.com/SukramJ/go-fabric/cluster/valve"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/surface"
)

// TestGarageAheadFeatures: with closureRow enabled the garage serves
// MotionLatching, Speed and Calibration; a latch settles and reports
// Stopped, a move with a latch latches on arrival, a calibration ends
// Stopped after garageCalibration (TC-CLCTRL-3.1, 4.2). Without the row it
// is the PS|VT drive.
func TestGarageAheadFeatures(t *testing.T) {
	t.Parallel()
	plain, err := newDemoGarage("plain").device(device.Options{StableKey: endpoint.StringKey("p"), Name: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if fm := ccldef.Wrap(plain.Server(ccldef.ClusterID)).Get(); fm.LatchControlModes != 0 {
		t.Errorf("plain garage serves LatchControlModes %v", fm.LatchControlModes)
	}
	synctest.Test(t, func(t *testing.T) {
		g := newDemoGarage("g")
		opts := device.Options{StableKey: endpoint.StringKey("g"), Name: "g", Surface: surface.Policies{Extensions: surface.NewExtensions(closureRow)}}
		d, err := g.device(opts)
		if err != nil {
			t.Fatal(err)
		}
		srv := g.server()
		ctx := context.Background()
		latch := true
		if _, err := d.Server(ccldef.ClusterID).MatterInvoke(ctx, ccldef.CmdMoveTo, ccldef.MoveToRequest{Latch: &latch}); err != nil {
			t.Fatalf("MoveTo latch: %v", err)
		}
		if st := srv.Get(); st.MainState != ccldef.MainStateMoving {
			t.Fatalf("MainState after MoveTo latch = %v, want Moving", st.MainState)
		}
		time.Sleep(garageSettle)
		synctest.Wait()
		st := srv.Get()
		if st.MainState != ccldef.MainStateStopped || st.OverallCurrentState.Latch == nil || !st.OverallCurrentState.Latch.Value {
			t.Fatalf("after the settle: %+v", st)
		}
		// Unlatch and open at High: the latch and the speed go with the
		// travel.
		unlatch, open, high := false, ccldef.TargetPositionMoveToFullyOpen, ccldef.ThreeLevelAutoHigh
		if _, err := d.Server(ccldef.ClusterID).MatterInvoke(ctx, ccldef.CmdMoveTo, ccldef.MoveToRequest{Position: &open, Latch: &unlatch, Speed: &high}); err != nil {
			t.Fatalf("MoveTo open: %v", err)
		}
		time.Sleep(garageTravel)
		synctest.Wait()
		st = srv.Get()
		if st.OverallCurrentState.Position.Value != ccldef.CurrentPositionFullyOpened || st.OverallCurrentState.Latch.Value ||
			st.OverallCurrentState.Speed == nil || *st.OverallCurrentState.Speed != high || st.MainState != ccldef.MainStateStopped {
			t.Fatalf("after the travel: %+v / %+v", st, st.OverallCurrentState)
		}
		if _, err := d.Server(ccldef.ClusterID).MatterInvoke(ctx, ccldef.CmdCalibrate, nil); err != nil {
			t.Fatalf("Calibrate: %v", err)
		}
		if st := srv.Get(); st.MainState != ccldef.MainStateCalibrating {
			t.Fatalf("MainState after Calibrate = %v", st.MainState)
		}
		// Stop abandons the calibration.
		if err := g.Stop(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := d.Server(ccldef.ClusterID).MatterInvoke(ctx, ccldef.CmdStop, nil); err != nil {
			t.Fatal(err)
		}
		if _, err := d.Server(ccldef.ClusterID).MatterInvoke(ctx, ccldef.CmdCalibrate, nil); err != nil {
			t.Fatalf("Calibrate again: %v", err)
		}
		time.Sleep(garageCalibration)
		synctest.Wait()
		if st := srv.Get(); st.MainState != ccldef.MainStateStopped {
			t.Fatalf("MainState after the calibration = %v, want Stopped", st.MainState)
		}
		if err := g.SetSpeed(ctx, ccldef.ThreeLevelAutoLow); err != nil {
			t.Fatal(err)
		}
		time.Sleep(garageSettle)
		synctest.Wait()
		if st := srv.Get(); *st.OverallCurrentState.Speed != ccldef.ThreeLevelAutoLow {
			t.Fatalf("speed after SetSpeed = %v", *st.OverallCurrentState.Speed)
		}
	})
}

// TestValveLevelFeatures: with level the valve declares LVL, TS,
// DefaultOpenLevel and LevelStep; an Open travels to the resolved level and
// TargetLevel goes back to null on arrival, a Close to 0, a re-open at the
// same level does not travel (TC-VALCC-3.2, 3.3).
func TestValveLevelFeatures(t *testing.T) {
	t.Parallel()
	decl, ok := newDemoValve("d", 600).declaration(true).(valvedef.Declare)
	if !ok || decl.Features != valvedef.FeatureLevel|valvedef.FeatureTimeSync || *decl.Initial.LevelStep != valveLevelStep {
		t.Fatalf("declaration(true) = %+v", decl)
	}
	synctest.Test(t, func(t *testing.T) {
		v := newDemoValve("lvl", 300)
		ctx := context.Background()
		forty := uint8(40)
		if err := v.Open(ctx, valve.OpenRequest{TargetLevel: &forty}); err != nil {
			t.Fatal(err)
		}
		if l, ok := v.TargetLevel(); !ok || l != 40 {
			t.Fatalf("TargetLevel while travelling = %v/%v", l, ok)
		}
		time.Sleep(valveTravel)
		synctest.Wait()
		if l, _ := v.CurrentLevel(); l != 40 {
			t.Fatalf("CurrentLevel = %d, want 40", l)
		}
		if _, ok := v.TargetLevel(); ok {
			t.Fatal("TargetLevel after arrival is not null")
		}
		if err := v.Open(ctx, valve.OpenRequest{TargetLevel: &forty}); err != nil {
			t.Fatal(err)
		}
		if _, ok := v.TargetLevel(); ok {
			t.Fatal("re-open at the same level left a TargetLevel")
		}
		if err := v.Close(ctx); err != nil {
			t.Fatal(err)
		}
		time.Sleep(valveTravel)
		synctest.Wait()
		if l, _ := v.CurrentLevel(); l != 0 {
			t.Fatalf("CurrentLevel after Close = %d", l)
		}
		if err := v.SetDefaultOpenLevel(ctx, 70); err != nil || v.DefaultOpenLevel() != 70 {
			t.Fatalf("DefaultOpenLevel = %d (%v)", v.DefaultOpenLevel(), err)
		}
	})
}
