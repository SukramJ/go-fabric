// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package light_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/SukramJ/go-fabric/cluster/light"
	"github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/im"
)

// onOff is the On/Off state of the endpoint under test.
type onOff struct {
	mu sync.Mutex
	on bool
}

func (o *onOff) OnOff() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.on
}

// stepWriter records every colour temperature the server pushes.
type stepWriter struct {
	mu     sync.Mutex
	values []uint16
	err    error
}

func (w *stepWriter) SetColorTemperatureMireds(_ context.Context, m uint16) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.err != nil {
		return w.err
	}
	w.values = append(w.values, m)
	return nil
}

// ctReports collects the server's reports with the value read at report
// time, as a subscription's report carries it.
type ctReports struct {
	mu     sync.Mutex
	mireds []uint16
	times  []uint16
}

func managedCT(on bool) (*light.ColorControlServer, *ctReports, *onOff) {
	o := &onOff{on: on}
	srv := light.NewColorControlServer(light.ColorControlServerConfig{
		MinMireds: 153, MaxMireds: 370, InitialMireds: 250, ManageTransitions: true, OnOff: o,
	})
	r := &ctReports{}
	srv.OnMatterAttributesChanged(func(ids []uint32) {
		for _, id := range ids {
			v, _ := srv.MatterRead(id)
			r.mu.Lock()
			switch id {
			case wire.ColorCtrlAttrColorTemperatureMireds:
				r.mireds = append(r.mireds, v.(uint16))
			case wire.ColorCtrlAttrRemainingTime:
				r.times = append(r.times, v.(uint16))
			}
			r.mu.Unlock()
		}
	})
	return srv, r, o
}

func (r *ctReports) snapshot() (mireds, times []uint16) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.mireds), slices.Clone(r.times)
}

func (r *ctReports) reset() {
	r.mu.Lock()
	r.mireds, r.times = nil, nil
	r.mu.Unlock()
}

func ctInvoke(t *testing.T, srv *light.ColorControlServer, cmd uint32, fields any) error {
	t.Helper()
	_, err := srv.MatterInvoke(context.Background(), cmd, fields)
	return err
}

func remainingCT(t *testing.T, srv *light.ColorControlServer) uint16 {
	t.Helper()
	v, _ := srv.MatterRead(wire.ColorCtrlAttrRemainingTime)
	return v.(uint16)
}

func ctSleep(d time.Duration) {
	time.Sleep(d)
	synctest.Wait()
}

func statusIs(err error, want im.StatusCode) bool {
	var sce im.StatusCodeError
	return errors.As(err, &sce) && sce.MatterStatusCode() == want
}

// moveCT / stepCT are the generic tag maps the bridge hands over for
// MoveColorTemperature and StepColorTemperature (color-control.element.ts
// field ids).
func moveCT(mode, rate, minimum, maximum uint64) map[uint8]any {
	return map[uint8]any{0: mode, 1: rate, 2: minimum, 3: maximum, 4: uint64(0), 5: uint64(0)}
}

func stepCT(mode, size, tenths, minimum, maximum uint64) map[uint8]any {
	return map[uint8]any{0: mode, 1: size, 2: tenths, 3: minimum, 4: maximum, 5: uint64(0), 6: uint64(0)}
}

// TestMoveColorTemperatureReadsPartWay is TC-CC-6.2 (Test_TC_CC_6_2.yaml)
// on a 153-370 light: MoveColorTemperature up at a rate that spans the
// range in 20 s reads between the bounds after 10 and 20 s and the maximum
// after 25 s; down likewise to the minimum. matter.js
// ColorControlServer.moveColorTemperatureLogic: target the bound, rate
// Rate per second.
func TestMoveColorTemperatureReadsPartWay(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		srv, _, _ := managedCT(true)
		rate := uint64((370 - 153) / 20)
		if err := ctInvoke(t, srv, wire.ColorCtrlCmdMoveToColorTemperature, wire.MoveToColorTemperatureRequest{ColorTemperatureMireds: 153}); err != nil {
			t.Fatal(err)
		}
		if err := ctInvoke(t, srv, wire.ColorCtrlCmdMoveColorTemperature, moveCT(1, rate, 153, 370)); err != nil {
			t.Fatal(err)
		}
		ctSleep(10 * time.Second)
		if m := currentMireds(t, srv); m <= 153 || m >= 370 {
			t.Errorf("after 10 s: %d, want strictly between the bounds", m)
		}
		ctSleep(15 * time.Second)
		if m := currentMireds(t, srv); m != 370 {
			t.Errorf("after 25 s: %d, want the maximum 370", m)
		}
		if err := ctInvoke(t, srv, wire.ColorCtrlCmdMoveColorTemperature, moveCT(3, rate, 0, 0)); err != nil {
			t.Fatal(err)
		}
		ctSleep(10 * time.Second)
		if m := currentMireds(t, srv); m <= 153 || m >= 370 {
			t.Errorf("down after 10 s: %d, want between the bounds", m)
		}
		ctSleep(15 * time.Second)
		if m := currentMireds(t, srv); m != 153 {
			t.Errorf("down after 25 s: %d, want the minimum 153", m)
		}
		// At the maximum rate the move lands within a step (step 7-8).
		if err := ctInvoke(t, srv, wire.ColorCtrlCmdMoveColorTemperature, moveCT(1, 65535, 153, 0)); err != nil {
			t.Fatal(err)
		}
		ctSleep(time.Second)
		if m := currentMireds(t, srv); m != 370 {
			t.Errorf("rate 65535: %d, want 370", m)
		}
	})
}

// TestMoveColorTemperatureRateAndStop pins matter.js #assertRate (Up or
// Down at Rate 0 is INVALID_COMMAND, Stop at Rate 0 is not) and its Stop
// branch, which ends hue and saturation moves only — a colour temperature
// move keeps running (ColorControlServer.moveColorTemperature).
func TestMoveColorTemperatureRateAndStop(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		srv, _, _ := managedCT(true)
		for _, mode := range []uint64{1, 3} {
			if err := ctInvoke(t, srv, wire.ColorCtrlCmdMoveColorTemperature, moveCT(mode, 0, 0, 0)); !statusIs(err, im.StatusInvalidCommand) {
				t.Errorf("MoveMode %d at Rate 0 = %v, want INVALID_COMMAND", mode, err)
			}
		}
		if err := ctInvoke(t, srv, wire.ColorCtrlCmdMoveColorTemperature, moveCT(2, 10, 0, 0)); !statusIs(err, im.StatusConstraintError) {
			t.Errorf("MoveMode 2 = %v, want CONSTRAINT_ERROR (not in MoveModeEnum)", err)
		}
		if err := ctInvoke(t, srv, wire.ColorCtrlCmdMoveColorTemperature, moveCT(0, 0, 0, 0)); err != nil {
			t.Errorf("Stop at Rate 0 = %v, want success", err)
		}
		_ = ctInvoke(t, srv, wire.ColorCtrlCmdMoveColorTemperature, moveCT(1, 10, 0, 0))
		ctSleep(time.Second)
		_ = ctInvoke(t, srv, wire.ColorCtrlCmdMoveColorTemperature, moveCT(0, 0, 0, 0))
		before := currentMireds(t, srv)
		ctSleep(time.Second)
		if after := currentMireds(t, srv); after <= before {
			t.Errorf("MoveMode Stop halted the colour temperature move (%d → %d); matter.js leaves it running", before, after)
		}
	})
}

// TestStepColorTemperatureReadsPartWay is TC-CC-6.3 (Test_TC_CC_6_3.yaml):
// a step over 20 s reads between the bounds after 10 s and at the target
// after 25 s; StepSize 0 is INVALID_COMMAND (matter.js #assertStepSize);
// the target is cropped to the bounds the command names.
func TestStepColorTemperatureReadsPartWay(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		srv, _, _ := managedCT(true)
		if err := ctInvoke(t, srv, wire.ColorCtrlCmdStepColorTemperature, stepCT(1, 100, 200, 0, 0)); err != nil {
			t.Fatal(err)
		}
		if got := remainingCT(t, srv); got != 200 {
			t.Errorf("RemainingTime = %d, want 200", got)
		}
		ctSleep(10 * time.Second)
		if m := currentMireds(t, srv); m <= 250 || m >= 350 {
			t.Errorf("after 10 s: %d, want between 250 and 350", m)
		}
		ctSleep(15 * time.Second)
		if m := currentMireds(t, srv); m != 350 {
			t.Errorf("after 25 s: %d, want 350", m)
		}
		if err := ctInvoke(t, srv, wire.ColorCtrlCmdStepColorTemperature, stepCT(3, 300, 0, 200, 0)); err != nil {
			t.Fatal(err)
		}
		if m := currentMireds(t, srv); m != 200 {
			t.Errorf("a step below the command's minimum = %d, want 200 (ColorTemperatureMinimumMireds)", m)
		}
		for _, mode := range []uint64{1, 3} {
			if err := ctInvoke(t, srv, wire.ColorCtrlCmdStepColorTemperature, stepCT(mode, 0, 0, 0, 0)); !statusIs(err, im.StatusInvalidCommand) {
				t.Errorf("StepSize 0 = %v, want INVALID_COMMAND", err)
			}
		}
	})
}

// TestColorTemperatureReportsQuietly is TC-CC-2.2 (src/python_testing/
// TC_CC_2_2.py): a 10 s MoveToColorTemperature reports
// ColorTemperatureMireds at most twelve times, ending on the target; two
// commands 5 s apart report RemainingTime 100, 150 and 0. A move at the
// maximum rate that lands within a step reports no RemainingTime at all
// (shorter than a second).
func TestColorTemperatureReportsQuietly(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		srv, r, _ := managedCT(true)
		_ = ctInvoke(t, srv, wire.ColorCtrlCmdMoveColorTemperature, moveCT(3, 65535, 0, 0))
		ctSleep(time.Second)
		if err := ctInvoke(t, srv, wire.ColorCtrlCmdMoveToColorTemperature, wire.MoveToColorTemperatureRequest{ColorTemperatureMireds: 370, TransitionTime: 100}); err != nil {
			t.Fatal(err)
		}
		ctSleep(20 * time.Second)
		mireds, times := r.snapshot()
		if len(mireds) == 0 || len(mireds) > 12 || mireds[len(mireds)-1] != 370 {
			t.Errorf("ColorTemperatureMireds reports %v: want 1 to 12, ending on 370", mireds)
		}
		if !slices.Equal(times, []uint16{100, 0}) {
			t.Errorf("RemainingTime reports %v, want [100 0] (the fast move reports none)", times)
		}

		_ = ctInvoke(t, srv, wire.ColorCtrlCmdMoveColorTemperature, moveCT(3, 65535, 0, 0))
		ctSleep(time.Second)
		r.reset()
		_ = ctInvoke(t, srv, wire.ColorCtrlCmdMoveToColorTemperature, wire.MoveToColorTemperatureRequest{ColorTemperatureMireds: 370 / 2, TransitionTime: 100})
		ctSleep(5 * time.Second)
		_ = ctInvoke(t, srv, wire.ColorCtrlCmdMoveToColorTemperature, wire.MoveToColorTemperatureRequest{ColorTemperatureMireds: 370, TransitionTime: 150})
		ctSleep(20 * time.Second)
		if _, times := r.snapshot(); len(times) != 3 || times[0] < 95 || times[0] > 100 || times[1] < 145 || times[1] > 150 || times[2] != 0 {
			t.Errorf("RemainingTime reports %v, want [~100 ~150 0]", times)
		}
	})
}

// TestStopMoveStepReportsZero ports matter.js
// packages/node/test/behaviors/color-control/ColorControlServerTest.ts
// "reports zero remaining time on StopMoveStep": RemainingTime reports
// [100 0] and reads 0; the colour temperature stays where it was.
func TestStopMoveStepReportsZero(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		srv, r, _ := managedCT(true)
		if err := ctInvoke(t, srv, wire.ColorCtrlCmdMoveToColorTemperature, wire.MoveToColorTemperatureRequest{ColorTemperatureMireds: 370, TransitionTime: 100}); err != nil {
			t.Fatal(err)
		}
		ctSleep(2 * time.Second)
		if err := ctInvoke(t, srv, wire.ColorCtrlCmdStopMoveStep, map[uint8]any{0: uint64(0), 1: uint64(0)}); err != nil {
			t.Fatal(err)
		}
		at := currentMireds(t, srv)
		ctSleep(time.Minute)
		if _, times := r.snapshot(); !slices.Equal(times, []uint16{100, 0}) || remainingCT(t, srv) != 0 {
			t.Errorf("RemainingTime reports %v (reads %d), want [100 0] and 0", times, remainingCT(t, srv))
		}
		if m := currentMireds(t, srv); m != at || m <= 250 || m >= 370 {
			t.Errorf("after StopMoveStep: %d (stopped at %d), want it held between start and target", m, at)
		}
	})
}

// TestColorControlExecuteIfOff pins matter.js #optionsAllowExecution: on an
// off device a command runs only with ExecuteIfOff in effect.
func TestColorControlExecuteIfOff(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		srv, _, o := managedCT(false)
		_ = ctInvoke(t, srv, wire.ColorCtrlCmdMoveToColorTemperature, wire.MoveToColorTemperatureRequest{ColorTemperatureMireds: 300})
		if m := currentMireds(t, srv); m != 250 {
			t.Errorf("an off device took MoveToColorTemperature: %d", m)
		}
		_ = ctInvoke(t, srv, wire.ColorCtrlCmdMoveToColorTemperature, wire.MoveToColorTemperatureRequest{ColorTemperatureMireds: 300, OptionsMask: 1, OptionsOverride: 1})
		if m := currentMireds(t, srv); m != 300 {
			t.Errorf("ExecuteIfOff override: %d, want 300", m)
		}
		_ = ctInvoke(t, srv, wire.ColorCtrlCmdMoveColorTemperature, map[uint8]any{0: uint64(1), 1: uint64(10), 4: uint64(1), 5: uint64(1)})
		ctSleep(time.Second)
		_ = ctInvoke(t, srv, wire.ColorCtrlCmdStopMoveStep, nil)
		if remainingCT(t, srv) == 0 {
			t.Error("StopMoveStep on an off device ended the move")
		}
		o.mu.Lock()
		o.on = true
		o.mu.Unlock()
		_ = ctInvoke(t, srv, wire.ColorCtrlCmdStopMoveStep, nil)
		if remainingCT(t, srv) != 0 {
			t.Error("StopMoveStep on an on device left the move running")
		}
	})
}

// TestUnmanagedCommandsApplyAtOnce: without ManageTransitions every command
// applies its target at once (matter.js managedTransitionTimeHandling
// false), Move and Step included, and RemainingTime stays 0.
func TestUnmanagedCommandsApplyAtOnce(t *testing.T) {
	srv := light.NewColorControlServer(light.ColorControlServerConfig{MinMireds: 153, MaxMireds: 370, InitialMireds: 250})
	w := &stepWriter{}
	srv.SetWriter(w)
	if err := ctInvoke(t, srv, wire.ColorCtrlCmdMoveColorTemperature, moveCT(1, 1, 0, 0)); err != nil {
		t.Fatal(err)
	}
	if m := currentMireds(t, srv); m != 370 || remainingCT(t, srv) != 0 {
		t.Errorf("unmanaged Move up: %d (RemainingTime %d), want 370 at once", m, remainingCT(t, srv))
	}
	if err := ctInvoke(t, srv, wire.ColorCtrlCmdStepColorTemperature, stepCT(3, 20, 600, 0, 0)); err != nil {
		t.Fatal(err)
	}
	if m := currentMireds(t, srv); m != 350 {
		t.Errorf("unmanaged Step down 20: %d, want 350 at once", m)
	}
	if !slices.Equal(w.values, []uint16{370, 350}) {
		t.Errorf("writer received %v, want [370 350]", w.values)
	}
	if err := ctInvoke(t, srv, wire.ColorCtrlCmdStopMoveStep, nil); err != nil {
		t.Error(err)
	}
	if err := ctInvoke(t, srv, 0x00, nil); err == nil {
		t.Error("MoveToHue accepted by a CT-only server")
	}
	for _, cmd := range []uint32{wire.ColorCtrlCmdMoveToColorTemperature, wire.ColorCtrlCmdMoveColorTemperature, wire.ColorCtrlCmdStepColorTemperature} {
		if err := ctInvoke(t, srv, cmd, "bogus"); !statusIs(err, im.StatusInvalidCommand) {
			t.Errorf("command 0x%02X with a bogus payload = %v, want INVALID_COMMAND", cmd, err)
		}
	}
}

// TestSyncColorTemperatureWithLevel pins matter.js
// ColorControlServer.syncColorTemperatureWithLevelLogic: MinLevel maps to
// the physical maximum, MaxLevel to CoupleColorTempToLevelMinMireds, the
// levels between linearly, applied at once.
func TestSyncColorTemperatureWithLevel(t *testing.T) {
	srv := light.NewColorControlServer(light.ColorControlServerConfig{MinMireds: 153, MaxMireds: 370, InitialMireds: 250, ManageTransitions: true})
	for level, want := range map[uint8]uint16{1: 370, 0: 370, 254: 153, 128: 370 - (370-153)*128/254, 64: 370 - (370-153)*64/254} {
		if err := srv.SyncColorTemperatureWithLevel(context.Background(), level); err != nil {
			t.Fatal(err)
		}
		if m := currentMireds(t, srv); m != want {
			t.Errorf("level %d: %d mireds, want %d", level, m, want)
		}
	}
}

// TestColorTransitionWriterAndQuiesce: a managed transition pushes every
// step to the writer; a refused step ends it; MatterQuiesce stops it
// without a report.
func TestColorTransitionWriterAndQuiesce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		srv, r, _ := managedCT(true)
		w := &stepWriter{}
		srv.SetWriter(w)
		_ = ctInvoke(t, srv, wire.ColorCtrlCmdMoveToColorTemperature, wire.MoveToColorTemperatureRequest{ColorTemperatureMireds: 350, TransitionTime: 100})
		ctSleep(time.Second)
		w.mu.Lock()
		steps := len(w.values)
		w.mu.Unlock()
		if steps < 5 {
			t.Errorf("the writer saw %d steps in a second, want one per 100 ms", steps)
		}
		srv.MatterQuiesce()
		at := currentMireds(t, srv)
		ctSleep(time.Minute)
		if currentMireds(t, srv) != at || remainingCT(t, srv) != 0 {
			t.Error("the transition ran on after MatterQuiesce")
		}
		if _, times := r.snapshot(); !slices.Equal(times, []uint16{100}) {
			t.Errorf("RemainingTime reports %v, want [100]", times)
		}
		_ = ctInvoke(t, srv, wire.ColorCtrlCmdMoveToColorTemperature, wire.MoveToColorTemperatureRequest{ColorTemperatureMireds: 160, TransitionTime: 100})
		w.mu.Lock()
		w.err = errors.New("lamp gone")
		w.mu.Unlock()
		ctSleep(time.Second)
		if remainingCT(t, srv) != 0 || currentMireds(t, srv) != at {
			t.Error("a refused step changed the value or left the transition running")
		}
		if got := srv.MatterSelfReportedAttributes(); !slices.Equal(got, []uint32{wire.ColorCtrlAttrColorTemperatureMireds, wire.ColorCtrlAttrRemainingTime}) {
			t.Errorf("self-reported %v", got)
		}
		if len(srv.MatterReportable()) != 0 {
			t.Error("ColorTemperatureMireds is reported by the server, not by a source notifier")
		}
	})
}
