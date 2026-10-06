// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package levelcontrol_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/SukramJ/go-fabric/cluster/levelcontrol"
	"github.com/SukramJ/go-fabric/cluster/light"
	"github.com/SukramJ/go-fabric/im"
)

// device is a host whose lamp cannot ramp: it applies every level at once
// and notifies, and it owns the On/Off state the engine couples to. Turning
// it on applies OnLevel, as an On/Off cluster's switch-on does.
type device struct {
	mu       sync.Mutex
	level    uint8
	known    bool
	options  uint8
	on       bool
	onLevel  *uint8
	notify   []func()
	applied  []uint8
	refuse   error
	switches []bool
	forward  []string // command methods reached; empty under the engine
}

func newDevice(level uint8, on bool) *device { return &device{level: level, known: true, on: on} }

func (d *device) CurrentLevel() (uint8, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.level, d.known
}

func (d *device) Options() uint8 {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.options
}

func (d *device) OnLevel() (uint8, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.onLevel == nil {
		return 0, false
	}
	return *d.onLevel, true
}

func (d *device) SetOptions(_ context.Context, o uint8) error {
	d.mu.Lock()
	d.options = o
	d.mu.Unlock()
	return nil
}

func (d *device) SetOnLevel(_ context.Context, l *uint8) error {
	d.mu.Lock()
	d.onLevel = l
	d.mu.Unlock()
	return nil
}

func (d *device) MoveToLevel(_ context.Context, req levelcontrol.MoveToLevelRequest) error {
	d.mu.Lock()
	if d.refuse != nil {
		d.mu.Unlock()
		return d.refuse
	}
	if req.TransitionTime == nil || *req.TransitionTime != 0 || req.OptionsMask&req.OptionsOverride&levelcontrol.OptionExecuteIfOff == 0 {
		d.forward = append(d.forward, "MoveToLevel")
	}
	d.level, d.known = req.Level, true
	d.applied = append(d.applied, req.Level)
	cbs := slices.Clone(d.notify)
	d.mu.Unlock()
	for _, cb := range cbs {
		cb()
	}
	return nil
}

func (d *device) record(name string) error {
	d.mu.Lock()
	d.forward = append(d.forward, name)
	d.mu.Unlock()
	return nil
}

func (d *device) MoveToLevelWithOnOff(context.Context, levelcontrol.MoveToLevelRequest) error {
	return d.record("MoveToLevelWithOnOff")
}
func (d *device) Move(context.Context, levelcontrol.MoveRequest) error { return d.record("Move") }
func (d *device) MoveWithOnOff(context.Context, levelcontrol.MoveRequest) error {
	return d.record("MoveWithOnOff")
}
func (d *device) Step(context.Context, levelcontrol.StepRequest) error { return d.record("Step") }
func (d *device) StepWithOnOff(context.Context, levelcontrol.StepRequest) error {
	return d.record("StepWithOnOff")
}
func (d *device) Stop(context.Context, levelcontrol.StopRequest) error { return d.record("Stop") }
func (d *device) StopWithOnOff(context.Context, levelcontrol.StopRequest) error {
	return d.record("StopWithOnOff")
}

func (d *device) OnMatterValueChanged(cb func()) func() {
	d.mu.Lock()
	d.notify = append(d.notify, cb)
	i := len(d.notify) - 1
	d.mu.Unlock()
	return func() {
		d.mu.Lock()
		d.notify[i] = func() {}
		d.mu.Unlock()
	}
}

// OnOff and SetOnOff are the On/Off cluster of the endpoint.
func (d *device) OnOff() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.on
}

func (d *device) SetOnOff(_ context.Context, on bool) error {
	d.mu.Lock()
	d.on = on
	d.switches = append(d.switches, on)
	if on && d.onLevel != nil {
		d.level = *d.onLevel
	}
	d.mu.Unlock()
	return nil
}

func (d *device) state() (level uint8, on bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.level, d.on
}

// reports collects what a server reports, with the value read at report
// time, as a subscription's report would carry it.
type reports struct {
	mu     sync.Mutex
	levels []uint8
	times  []uint16
}

func listen(t *testing.T, srv *levelcontrol.Server) *reports {
	t.Helper()
	r := &reports{}
	srv.OnMatterAttributesChanged(func(ids []uint32) {
		for _, id := range ids {
			v, _ := srv.MatterRead(id)
			r.mu.Lock()
			switch id {
			case levelcontrol.AttrCurrentLevel:
				r.levels = append(r.levels, v.(uint8))
			case levelcontrol.AttrRemainingTime:
				r.times = append(r.times, v.(uint16))
			}
			r.mu.Unlock()
		}
	})
	return r
}

func (r *reports) snapshot() (levels []uint8, times []uint16) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.levels), slices.Clone(r.times)
}

// engineServer is a Lighting LevelControl server with the engine over d.
func engineServer(d *device, ct levelcontrol.ColorTemperatureCoupling) *levelcontrol.Server {
	return levelcontrol.NewServer(levelcontrol.Config{
		Source: d, Lighting: true,
		Transitions: &levelcontrol.Transitions{OnOff: d, ColorTemperature: ct},
	})
}

func invoke(t *testing.T, srv *levelcontrol.Server, cmd uint32, fields any) error {
	t.Helper()
	_, err := srv.MatterInvoke(context.Background(), cmd, fields)
	return err
}

func u16(v uint16) *uint16 { return &v }
func u8(v uint8) *uint8    { return &v }

func level(t *testing.T, srv *levelcontrol.Server) uint8 {
	t.Helper()
	v, ok := srv.MatterRead(levelcontrol.AttrCurrentLevel)
	if !ok || v == nil {
		t.Fatalf("CurrentLevel = %v, %v", v, ok)
	}
	return v.(uint8)
}

func remaining(t *testing.T, srv *levelcontrol.Server) uint16 {
	t.Helper()
	v, ok := srv.MatterRead(levelcontrol.AttrRemainingTime)
	if !ok {
		t.Fatal("RemainingTime unreadable")
	}
	return v.(uint16)
}

func sleep(d time.Duration) {
	time.Sleep(d)
	synctest.Wait()
}

// within reports whether v lies in [lo, hi].
func within(v, lo, hi uint8) bool { return v >= lo && v <= hi }

// TestMoveToLevelTransitionReadsPartWay is the TC-LVL-3.1 step 4 shape
// (Test_TC_LVL_3_1.yaml): MoveToLevel 50 → 200 over 30 s reads 85-115
// after 10 s, 127-173 after 20 s, 170-200 after 30 s and 200 after 35 s.
// Mirrors matter.js LevelControlServer moveToLevelLogic (rate
// (level - current) / TransitionTime * 10).
func TestMoveToLevelTransitionReadsPartWay(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		d := newDevice(50, true)
		srv := engineServer(d, nil)
		if err := invoke(t, srv, levelcontrol.CmdMoveToLevel, levelcontrol.MoveToLevelRequest{Level: 200, TransitionTime: u16(300)}); err != nil {
			t.Fatal(err)
		}
		if got := remaining(t, srv); got != 300 {
			t.Errorf("RemainingTime at the start = %d, want 300", got)
		}
		for _, step := range []struct{ lo, hi uint8 }{{85, 115}, {127, 173}, {170, 200}} {
			sleep(10 * time.Second)
			if l := level(t, srv); !within(l, step.lo, step.hi) {
				t.Errorf("CurrentLevel = %d, want %d to %d", l, step.lo, step.hi)
			}
		}
		sleep(5 * time.Second)
		if l := level(t, srv); l != 200 || remaining(t, srv) != 0 {
			t.Errorf("after the transition: CurrentLevel %d, RemainingTime %d; want 200, 0", l, remaining(t, srv))
		}
		if len(d.forward) != 0 {
			t.Errorf("the engine forwarded commands to the host: %v", d.forward)
		}
	})
}

// TestMoveAndStop is TC-LVL-4.1 / TC-LVL-6.1 (Test_TC_LVL_4_1.yaml,
// Test_TC_LVL_6_1.yaml): Move up at 5/s from 50 reads ~100 after 10 s; a
// Stop after 5 s leaves it at 64-86 and RemainingTime at 0; Rate 0 is
// INVALID_COMMAND and moves nothing (matter.js #assertRateValue); a null
// Rate with no DefaultMoveRate moves at once.
func TestMoveAndStop(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		d := newDevice(50, true)
		srv := engineServer(d, nil)
		r := listen(t, srv)
		if err := invoke(t, srv, levelcontrol.CmdMove, levelcontrol.MoveRequest{MoveMode: levelcontrol.MoveModeUp, Rate: u8(5)}); err != nil {
			t.Fatal(err)
		}
		sleep(5 * time.Second)
		if err := invoke(t, srv, levelcontrol.CmdStop, levelcontrol.StopRequest{}); err != nil {
			t.Fatal(err)
		}
		stopped := level(t, srv)
		if !within(stopped, 64, 86) || remaining(t, srv) != 0 {
			t.Fatalf("after Stop: CurrentLevel %d, RemainingTime %d; want 64 to 86, 0", stopped, remaining(t, srv))
		}
		sleep(10 * time.Second)
		if l := level(t, srv); l != stopped {
			t.Errorf("the level moved after Stop: %d → %d", stopped, l)
		}
		if _, times := r.snapshot(); !slices.Equal(times, []uint16{408, 0}) {
			t.Errorf("RemainingTime reports %v, want [408 0] (204 units at 5/s, then the stop)", times)
		}

		err := invoke(t, srv, levelcontrol.CmdMove, levelcontrol.MoveRequest{MoveMode: levelcontrol.MoveModeUp, Rate: u8(0)})
		var sce im.StatusCodeError
		if !errors.As(err, &sce) || sce.MatterStatusCode() != im.StatusInvalidCommand {
			t.Errorf("Move with Rate 0 = %v, want INVALID_COMMAND", err)
		}
		if l := level(t, srv); l != stopped {
			t.Errorf("Move with Rate 0 moved the level to %d", l)
		}

		if err := invoke(t, srv, levelcontrol.CmdMove, levelcontrol.MoveRequest{MoveMode: levelcontrol.MoveModeDown}); err != nil {
			t.Fatal(err)
		}
		if l := level(t, srv); l != levelcontrol.LightingLevelMin {
			t.Errorf("Move down without a rate = %d, want MinLevel at once", l)
		}
	})
}

// TestStepTransition is TC-LVL-5.1 (Test_TC_LVL_5_1.yaml): Step up 150 from
// 50 over 30 s reads 85-115, 127-173 and 170-200 every 10 s and 200 at the
// end (matter.js stepLogic, rate stepSize / TransitionTime * 10).
func TestStepTransition(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		d := newDevice(50, true)
		srv := engineServer(d, nil)
		if err := invoke(t, srv, levelcontrol.CmdStep, levelcontrol.StepRequest{StepMode: levelcontrol.StepModeUp, StepSize: 150, TransitionTime: u16(300)}); err != nil {
			t.Fatal(err)
		}
		for _, step := range []struct{ lo, hi uint8 }{{85, 115}, {127, 173}, {170, 200}} {
			sleep(10 * time.Second)
			if l := level(t, srv); !within(l, step.lo, step.hi) {
				t.Errorf("CurrentLevel = %d, want %d to %d", l, step.lo, step.hi)
			}
		}
		sleep(5 * time.Second)
		if l := level(t, srv); l != 200 {
			t.Errorf("CurrentLevel = %d, want 200", l)
		}
		// A step with TransitionTime 0 or null lands at once; one past the
		// maximum stops at it.
		if err := invoke(t, srv, levelcontrol.CmdStep, levelcontrol.StepRequest{StepMode: levelcontrol.StepModeUp, StepSize: 100, TransitionTime: u16(0)}); err != nil {
			t.Fatal(err)
		}
		if l := level(t, srv); l != levelcontrol.LevelMax {
			t.Errorf("CurrentLevel = %d, want %d", l, levelcontrol.LevelMax)
		}
		if err := invoke(t, srv, levelcontrol.CmdStep, levelcontrol.StepRequest{StepMode: levelcontrol.StepModeDown, StepSize: 54}); err != nil {
			t.Fatal(err)
		}
		if l := level(t, srv); l != 200 {
			t.Errorf("CurrentLevel = %d, want 200", l)
		}
	})
}

// TestQuieterReportsOfATransition is TC-LVL-2.3 (src/python_testing/
// TC_LVL_2_3.py): a 10 s MoveToLevelWithOnOff from the minimum reports
// CurrentLevel at most twelve times, rising, ending on the maximum; two
// MoveToLevel commands 5 s apart report RemainingTime three times — ~100,
// ~150, 0. CurrentLevel and RemainingTime carry the "Q" quality (matter.js
// QuietEvent, Transitions #updateRemainingTime).
func TestQuieterReportsOfATransition(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		d := newDevice(1, false)
		srv := engineServer(d, nil)
		r := listen(t, srv)
		if err := invoke(t, srv, levelcontrol.CmdMoveToLevelWithOnOff, levelcontrol.MoveToLevelRequest{Level: 254, TransitionTime: u16(100)}); err != nil {
			t.Fatal(err)
		}
		if _, on := d.state(); !on {
			t.Fatal("MoveToLevelWithOnOff towards on left the device off (matter.js #turnOnForLevel acts at once)")
		}
		sleep(30 * time.Second)
		levels, times := r.snapshot()
		if len(levels) == 0 || len(levels) > 12 || levels[len(levels)-1] != 254 || !slices.IsSorted(levels) {
			t.Errorf("CurrentLevel reports %v: want 1 to 12, rising, ending on 254", levels)
		}
		if !slices.Equal(times, []uint16{100, 0}) {
			t.Errorf("RemainingTime reports %v, want [100 0]", times)
		}

		r.mu.Lock()
		r.levels, r.times = nil, nil
		r.mu.Unlock()
		if err := invoke(t, srv, levelcontrol.CmdMoveToLevel, levelcontrol.MoveToLevelRequest{Level: 1, TransitionTime: u16(100)}); err != nil {
			t.Fatal(err)
		}
		sleep(5 * time.Second)
		if err := invoke(t, srv, levelcontrol.CmdMoveToLevel, levelcontrol.MoveToLevelRequest{Level: 1, TransitionTime: u16(150)}); err != nil {
			t.Fatal(err)
		}
		sleep(20 * time.Second)
		_, times = r.snapshot()
		if len(times) != 3 || times[0] < 95 || times[0] > 100 || times[1] < 145 || times[1] > 150 || times[2] != 0 {
			t.Errorf("RemainingTime reports %v, want [~100 ~150 0]", times)
		}
	})
}

// TestOnOffCoupling ports matter.js
// packages/node/test/behaviors/level-control/OnOffCouplingTest.ts: a
// "with On/Off" move down that reaches the minimum, or a step down that
// overshoots it, switches the device off; a move down while off stays
// off; a move up while off switches on at once. LevelControlServerTest.ts
// "transitions to off with correct events": a 4 s MoveToLevelWithOnOff
// to the minimum turns off only once the level is there.
func TestOnOffCoupling(t *testing.T) {
	for name, tc := range map[string]struct {
		level  uint8
		on     bool
		cmd    uint32
		fields any
		after  time.Duration
		wantOn bool
		want   uint8
	}{
		"move down reaches the minimum":   {254, true, levelcontrol.CmdMoveWithOnOff, levelcontrol.MoveRequest{MoveMode: levelcontrol.MoveModeDown, Rate: u8(254)}, 2 * time.Second, false, 1},
		"step down overshoots":            {50, true, levelcontrol.CmdStepWithOnOff, levelcontrol.StepRequest{StepMode: levelcontrol.StepModeDown, StepSize: 100}, 0, false, 1},
		"move down while off":             {50, false, levelcontrol.CmdMoveWithOnOff, levelcontrol.MoveRequest{MoveMode: levelcontrol.MoveModeDown, Rate: u8(254)}, 2 * time.Second, false, 1},
		"move up while off":               {50, false, levelcontrol.CmdMoveWithOnOff, levelcontrol.MoveRequest{MoveMode: levelcontrol.MoveModeUp, Rate: u8(254)}, 2 * time.Second, true, 254},
		"MoveToLevel 0 crops to MinLevel": {128, true, levelcontrol.CmdMoveToLevelWithOnOff, levelcontrol.MoveToLevelRequest{Level: 0, TransitionTime: u16(0)}, 0, false, 1},
	} {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				d := newDevice(tc.level, tc.on)
				srv := engineServer(d, nil)
				if err := invoke(t, srv, tc.cmd, tc.fields); err != nil {
					t.Fatal(err)
				}
				sleep(tc.after)
				if l, on := d.state(); l != tc.want || on != tc.wantOn {
					t.Errorf("level %d on %v, want %d on %v", l, on, tc.want, tc.wantOn)
				}
			})
		})
	}

	synctest.Test(t, func(t *testing.T) {
		d := newDevice(128, true)
		srv := engineServer(d, nil)
		if err := invoke(t, srv, levelcontrol.CmdMoveToLevelWithOnOff, levelcontrol.MoveToLevelRequest{Level: 1, TransitionTime: u16(40)}); err != nil {
			t.Fatal(err)
		}
		sleep(3800 * time.Millisecond)
		if l, on := d.state(); l == 1 || !on {
			t.Fatalf("at 3.8 s: level %d on %v; want above the minimum and on", l, on)
		}
		sleep(100 * time.Millisecond)
		if l, on := d.state(); l != 1 || on {
			t.Errorf("at 3.9 s: level %d on %v; want the minimum and off", l, on)
		}
	})
}

// TestOnLevelOnSwitchOn: a MoveToLevelWithOnOff that switches the device
// on moves from OnLevel, which the host applies with the switch-on
// (matter.js #turnOnForLevel → handleOnOffChange), and derives its rate
// from there.
func TestOnLevelOnSwitchOn(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		d := newDevice(1, false)
		d.onLevel = u8(100)
		srv := engineServer(d, nil)
		if err := invoke(t, srv, levelcontrol.CmdMoveToLevelWithOnOff, levelcontrol.MoveToLevelRequest{Level: 200, TransitionTime: u16(100)}); err != nil {
			t.Fatal(err)
		}
		if l, on := d.state(); l != 100 || !on {
			t.Fatalf("level %d on %v, want 100 (OnLevel) and on", l, on)
		}
		sleep(5 * time.Second)
		if l := level(t, srv); !within(l, 145, 155) {
			t.Errorf("after 5 s: CurrentLevel %d, want ~150 (100 → 200 in 10 s)", l)
		}
	})
}

// TestExecuteIfOff ports matter.js LevelControlServer #optionsAllowExecution
// and LevelControlServerTest.ts "LevelControl stop": a plain command on an
// off device runs only with ExecuteIfOff in effect; Stop is gated, so a
// transition an override started survives it, StopWithOnOff is not.
func TestExecuteIfOff(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		d := newDevice(1, false)
		srv := engineServer(d, nil)
		if err := invoke(t, srv, levelcontrol.CmdMoveToLevel, levelcontrol.MoveToLevelRequest{Level: 120, TransitionTime: u16(0)}); err != nil {
			t.Fatal(err)
		}
		if l := level(t, srv); l != 1 {
			t.Fatalf("MoveToLevel on an off device moved the level to %d", l)
		}
		if err := invoke(t, srv, levelcontrol.CmdMoveToLevel, levelcontrol.MoveToLevelRequest{
			Level: 254, TransitionTime: u16(300),
			OptionsMask: levelcontrol.OptionExecuteIfOff, OptionsOverride: levelcontrol.OptionExecuteIfOff,
		}); err != nil {
			t.Fatal(err)
		}
		if remaining(t, srv) == 0 {
			t.Fatal("the ExecuteIfOff override started no transition")
		}
		if err := invoke(t, srv, levelcontrol.CmdStop, levelcontrol.StopRequest{}); err != nil {
			t.Fatal(err)
		}
		if remaining(t, srv) == 0 {
			t.Error("Stop on an off device ended the transition")
		}
		if err := invoke(t, srv, levelcontrol.CmdStopWithOnOff, levelcontrol.StopRequest{}); err != nil {
			t.Fatal(err)
		}
		if remaining(t, srv) != 0 {
			t.Error("StopWithOnOff on an off device left the transition running")
		}
		// The Options attribute's own ExecuteIfOff lets the plain command
		// run.
		if err := srv.MatterWrite(context.Background(), levelcontrol.AttrOptions, levelcontrol.OptionExecuteIfOff); err != nil {
			t.Fatal(err)
		}
		if err := invoke(t, srv, levelcontrol.CmdMoveToLevel, levelcontrol.MoveToLevelRequest{Level: 60, TransitionTime: u16(0)}); err != nil {
			t.Fatal(err)
		}
		if l := level(t, srv); l != 60 {
			t.Errorf("MoveToLevel with Options ExecuteIfOff = %d, want 60", l)
		}
	})
}

// TestColorTemperatureCoupling ports matter.js
// packages/node/test/behaviors/level-control/ColorTemperatureCouplingTest.ts
// against this module's ColorControl server (physical range 153-370, whose
// CoupleColorTempToLevelMinMireds is the physical minimum): the minimum
// level maps to 370 mireds, 128 to 370 - floor((370-153) * 128 / 254) =
// 261; without the option nothing couples; every step of a managed
// transition couples.
func TestColorTemperatureCoupling(t *testing.T) {
	newCT := func() *light.ColorControlServer {
		return light.NewColorControlServer(light.ColorControlServerConfig{MinMireds: 153, MaxMireds: 370, InitialMireds: 300})
	}
	mireds := func(t *testing.T, ct *light.ColorControlServer) uint16 {
		t.Helper()
		v, _ := ct.MatterRead(0x0007)
		return v.(uint16)
	}
	couple := levelcontrol.OptionCoupleColorTempToLevel
	for name, tc := range map[string]struct {
		options, mask, override uint8
		cmd                     uint32
		fields                  any
		want                    uint16
	}{
		"WithOnOff to the minimum":           {couple, 0, 0, levelcontrol.CmdMoveToLevelWithOnOff, levelcontrol.MoveToLevelRequest{Level: 1}, 370},
		"to 128":                             {couple, 0, 0, levelcontrol.CmdMoveToLevelWithOnOff, levelcontrol.MoveToLevelRequest{Level: 128}, 261},
		"step down to 128":                   {couple, 0, 0, levelcontrol.CmdStepWithOnOff, levelcontrol.StepRequest{StepMode: levelcontrol.StepModeDown, StepSize: 126}, 261},
		"plain command":                      {couple, 0, 0, levelcontrol.CmdMoveToLevel, levelcontrol.MoveToLevelRequest{Level: 1}, 370},
		"option not set":                     {0, 0, 0, levelcontrol.CmdMoveToLevelWithOnOff, levelcontrol.MoveToLevelRequest{Level: 1}, 300},
		"override sets it for one command":   {0, couple, couple, levelcontrol.CmdMoveToLevelWithOnOff, levelcontrol.MoveToLevelRequest{Level: 1}, 370},
		"override clears it for one command": {couple, couple, 0, levelcontrol.CmdMoveToLevelWithOnOff, levelcontrol.MoveToLevelRequest{Level: 1}, 300},
	} {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				d := newDevice(254, true)
				d.options = tc.options
				ct := newCT()
				srv := engineServer(d, ct)
				switch f := tc.fields.(type) {
				case levelcontrol.MoveToLevelRequest:
					f.OptionsMask, f.OptionsOverride = tc.mask, tc.override
					tc.fields = f
				case levelcontrol.StepRequest:
					f.OptionsMask, f.OptionsOverride = tc.mask, tc.override
					tc.fields = f
				}
				if err := invoke(t, srv, tc.cmd, tc.fields); err != nil {
					t.Fatal(err)
				}
				if got := mireds(t, ct); got != tc.want {
					t.Errorf("ColorTemperatureMireds = %d, want %d", got, tc.want)
				}
			})
		})
	}

	t.Run("every step of a managed transition", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			d := newDevice(254, true)
			d.options = couple
			ct := newCT()
			srv := engineServer(d, ct)
			if err := invoke(t, srv, levelcontrol.CmdMoveWithOnOff, levelcontrol.MoveRequest{MoveMode: levelcontrol.MoveModeDown, Rate: u8(100)}); err != nil {
				t.Fatal(err)
			}
			sleep(time.Second)
			if m := mireds(t, ct); m <= 153 || m >= 370 {
				t.Errorf("mid-transition ColorTemperatureMireds = %d, want between 153 and 370", m)
			}
			sleep(3 * time.Second)
			if l := level(t, srv); l != 1 || mireds(t, ct) != 370 {
				t.Errorf("at the end: level %d, mireds %d; want 1, 370", l, mireds(t, ct))
			}
		})
	})
}

// TestLightingSurface pins what Config.Lighting adds: FeatureMap OO|LT,
// MinLevel 1, RemainingTime, StartUpCurrentLevel ("RW VM", stored and
// reported, never applied), the CoupleColorTempToLevel option, OnLevel
// constrained to MinLevel..MaxLevel (level-control.element.ts).
func TestLightingSurface(t *testing.T) {
	ctx := context.Background()
	d := newDevice(50, true)
	srv := levelcontrol.NewServer(levelcontrol.Config{Source: d, Lighting: true})
	if v, _ := srv.MatterRead(0xFFFC); v != levelcontrol.FeatureOnOff|levelcontrol.FeatureLighting {
		t.Errorf("FeatureMap = %v, want OO|LT", v)
	}
	if v, _ := srv.MatterRead(levelcontrol.AttrMinLevel); v != uint8(1) {
		t.Errorf("MinLevel = %v, want 1", v)
	}
	if v, _ := srv.MatterRead(levelcontrol.AttrRemainingTime); v != uint16(0) {
		t.Errorf("RemainingTime on the hand-off path = %v, want 0", v)
	}
	if !slices.Contains(srv.MatterAttributes(), levelcontrol.AttrRemainingTime) || !slices.Contains(srv.MatterAttributes(), levelcontrol.AttrStartUpCurrentLevel) {
		t.Errorf("attributes %v lack the LT ones", srv.MatterAttributes())
	}
	if v, ok := srv.MatterRead(levelcontrol.AttrStartUpCurrentLevel); !ok || v != nil {
		t.Errorf("StartUpCurrentLevel = %v, %v; want null", v, ok)
	}
	if err := srv.MatterWrite(ctx, levelcontrol.AttrStartUpCurrentLevel, uint64(77)); err != nil {
		t.Fatal(err)
	}
	if v, _ := srv.MatterRead(levelcontrol.AttrStartUpCurrentLevel); v != uint8(77) {
		t.Errorf("StartUpCurrentLevel = %v, want 77", v)
	}
	if err := srv.MatterWrite(ctx, levelcontrol.AttrStartUpCurrentLevel, uint64(255)); err == nil {
		t.Error("StartUpCurrentLevel 255 accepted")
	}
	if err := srv.MatterWrite(ctx, levelcontrol.AttrStartUpCurrentLevel, nil); err != nil {
		t.Error(err)
	}
	if srv.MinWritePrivilege(levelcontrol.AttrStartUpCurrentLevel) != 4 || srv.MinWritePrivilege(levelcontrol.AttrOnLevel) != 3 {
		t.Error("StartUpCurrentLevel is RW VM, OnLevel RW VO")
	}
	if err := srv.MatterWrite(ctx, levelcontrol.AttrOptions, levelcontrol.OptionCoupleColorTempToLevel); err != nil {
		t.Errorf("CoupleColorTempToLevel refused under LT: %v", err)
	}
	if v, _ := srv.MatterRead(levelcontrol.AttrOptions); v != levelcontrol.OptionCoupleColorTempToLevel {
		t.Errorf("Options = %v, want the coupling bit readable", v)
	}
	if err := srv.MatterWrite(ctx, levelcontrol.AttrOnLevel, uint8(0)); err == nil {
		t.Error("OnLevel 0 accepted under LT (constraint minLevel to maxLevel)")
	}
	if len(srv.MatterSelfReportedAttributes()) != 0 || !slices.Equal(srv.MatterReportable(), []uint32{levelcontrol.AttrCurrentLevel}) {
		t.Error("the hand-off path reports CurrentLevel through the host notifier")
	}

	plain := levelcontrol.NewServer(levelcontrol.Config{Source: d})
	if _, ok := plain.MatterRead(levelcontrol.AttrRemainingTime); ok {
		t.Error("RemainingTime served without LT")
	}
	if err := plain.MatterWrite(ctx, levelcontrol.AttrStartUpCurrentLevel, uint64(1)); err == nil {
		t.Error("StartUpCurrentLevel written without LT")
	}

	eng := engineServer(d, nil)
	if !slices.Equal(eng.MatterSelfReportedAttributes(), []uint32{levelcontrol.AttrCurrentLevel, levelcontrol.AttrRemainingTime}) || len(eng.MatterReportable()) != 0 {
		t.Errorf("under the engine the server reports CurrentLevel and RemainingTime itself: %v / %v",
			eng.MatterSelfReportedAttributes(), eng.MatterReportable())
	}
	called := false
	eng.OnMatterValueChanged(func() { called = true })()
	if called {
		t.Error("the host-notifier hop fired under the engine")
	}
}

// TestHostChangeIsReported: under the engine a level the device changed
// by itself is reported through the server, by the quieter rules.
func TestHostChangeIsReported(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		d := newDevice(50, true)
		srv := engineServer(d, nil)
		r := listen(t, srv)
		zero := uint16(0)
		_ = d.MoveToLevel(context.Background(), levelcontrol.MoveToLevelRequest{Level: 80, TransitionTime: &zero, OptionsMask: 1, OptionsOverride: 1})
		if levels, _ := r.snapshot(); !slices.Equal(levels, []uint8{80}) {
			t.Errorf("CurrentLevel reports %v, want [80]", levels)
		}
	})
}

// TestQuiesceAndRefusal: MatterQuiesce stops a transition where it is,
// silently; a host that refuses a step ends the transition; a null
// CurrentLevel cannot be stepped from (Status.Failure, matter.js
// currentLevel getter).
func TestQuiesceAndRefusal(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		d := newDevice(1, true)
		srv := engineServer(d, nil)
		r := listen(t, srv)
		_ = invoke(t, srv, levelcontrol.CmdMoveToLevel, levelcontrol.MoveToLevelRequest{Level: 254, TransitionTime: u16(100)})
		sleep(2 * time.Second)
		srv.MatterQuiesce()
		at := level(t, srv)
		sleep(time.Minute)
		if level(t, srv) != at || remaining(t, srv) != 0 {
			t.Error("the transition ran on after MatterQuiesce")
		}
		if _, times := r.snapshot(); !slices.Equal(times, []uint16{100}) {
			t.Errorf("RemainingTime reports %v, want [100]: quiescing reports nothing", times)
		}

		_ = invoke(t, srv, levelcontrol.CmdMoveToLevel, levelcontrol.MoveToLevelRequest{Level: 1, TransitionTime: u16(100)})
		d.mu.Lock()
		d.refuse = errors.New("lamp gone")
		d.mu.Unlock()
		sleep(time.Second)
		if remaining(t, srv) != 0 {
			t.Error("a refused step left the transition running")
		}
		if err := invoke(t, srv, levelcontrol.CmdMoveToLevel, levelcontrol.MoveToLevelRequest{Level: 5, TransitionTime: u16(0)}); err == nil {
			t.Error("an immediate MoveToLevel the host refused reported success")
		}

		d.mu.Lock()
		d.known = false
		d.mu.Unlock()
		err := invoke(t, srv, levelcontrol.CmdStep, levelcontrol.StepRequest{StepMode: levelcontrol.StepModeUp, StepSize: 1})
		var sce im.StatusCodeError
		if !errors.As(err, &sce) || sce.MatterStatusCode() != im.StatusFailure {
			t.Errorf("Step from a null level = %v, want FAILURE", err)
		}
	})
}
