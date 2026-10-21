// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package levelcontrol_test

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/SukramJ/go-fabric/cluster/levelcontrol"
	"github.com/SukramJ/go-fabric/cluster/spec"
	lvldef "github.com/SukramJ/go-fabric/cluster/spec/levelcontrol"
	"github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/im"
)

// TestStepLevel holds StepLevel to matter.js LevelControlServer.ts:471-478
// stepLogic (current + stepSize * direction) under the transition bounds
// minLevel..maxLevel (:186-195).
func TestStepLevel(t *testing.T) {
	t.Parallel()
	up, down := levelcontrol.StepModeUp, levelcontrol.StepModeDown
	for _, c := range []struct {
		current, mode, size, min, want uint8
	}{
		{100, up, 10, 0, 110},
		{100, down, 10, 0, 90},
		{250, up, 10, 0, 254},  // cropped to maxLevel
		{5, down, 10, 0, 0},    // cropped to minLevel 0
		{5, down, 10, 1, 1},    // Lighting: minLevel 1
		{0, up, 0, 1, 1},       // a level below the Lighting minimum is lifted
		{254, up, 255, 0, 254}, // no uint8 overflow
		{200, down, 255, 0, 0}, // no uint8 underflow
		{100, up, 0, 0, 100},   // a zero step stays
		{120, 7, 10, 0, 110},   // any mode but Up steps down (`=== Up ? 1 : -1`, :471)
	} {
		want := c.want
		if got := levelcontrol.StepLevel(c.current, c.mode, c.size, c.min); got != want {
			t.Errorf("StepLevel(%d, %d, %d, %d) = %d, want %d", c.current, c.mode, c.size, c.min, got, want)
		}
	}
}

// TestCropLevel holds CropLevel to matter.js LevelControlServer.ts:274 /
// :291 cropValueRange(level, minLevel, maxLevel).
func TestCropLevel(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ level, min, want uint8 }{
		{0, 0, 0}, {0, 1, 1}, {1, 1, 1}, {127, 1, 127}, {254, 1, 254}, {255, 0, 254},
	} {
		if got := levelcontrol.CropLevel(c.level, c.min); got != c.want {
			t.Errorf("CropLevel(%d, %d) = %d, want %d", c.level, c.min, got, c.want)
		}
	}
}

// TestTransitionDuration holds TransitionDuration to matter.js
// moveToLevelLogic (a falsy transition time is at once,
// LevelControlServer.ts:332-338) in tenths (Transitions.ts:698).
func TestTransitionDuration(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		tenths *uint16
		want   time.Duration
	}{
		{nil, 0},
		{u16(0), 0},
		{u16(1), 100 * time.Millisecond},
		{u16(20), 2 * time.Second},
		{u16(0xFFFE), 6553400 * time.Millisecond},
	} {
		if got := levelcontrol.TransitionDuration(c.tenths); got != c.want {
			t.Errorf("TransitionDuration(%v) = %v, want %v", c.tenths, got, c.want)
		}
	}
}

// TestMoveTransition holds MoveTransition to matter.js Transitions.ts:429
// (distance / changePerMs) in tenths, Math.round (:645-647), a zero rate at
// once (:133).
func TestMoveTransition(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		distance, rate uint8
		want           uint16
	}{
		{154, 10, 154}, // 15.4 s
		{254, 1, 2540}, // the longest a uint8 move can take
		{10, 3, 33},    // 33.33 rounds down
		{1, 4, 3},      // 2.5 rounds half up, as Math.round
		{0, 10, 0},     // already there
		{100, 0, 0},    // a zero rate is at once
		{1, 255, 0},    // 0.039 rounds to at once
		{254, 255, 10}, // 9.96
	} {
		if got := levelcontrol.MoveTransition(c.distance, c.rate); got != c.want {
			t.Errorf("MoveTransition(%d, %d) = %d, want %d", c.distance, c.rate, got, c.want)
		}
	}
}

// TestUnitScaling holds LevelFromUnit / UnitFromLevel to the CurrentLevel
// range 0..254 (level-control.element.ts:74), rounded half up as matter.js
// Math.round.
func TestUnitScaling(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		u    float64
		want uint8
	}{
		{0, 0}, {1, 254}, {0.5, 127}, {1.5, 254}, {-1, 0}, {math.NaN(), 0}, {1.0 / 254, 1}, {0.5 / 254, 1},
	} {
		if got := levelcontrol.LevelFromUnit(c.u); got != c.want {
			t.Errorf("LevelFromUnit(%v) = %d, want %d", c.u, got, c.want)
		}
	}
	for l := range uint8(255) {
		if got := levelcontrol.LevelFromUnit(levelcontrol.UnitFromLevel(l)); got != l {
			t.Errorf("round trip %d -> %d", l, got)
		}
	}
	if got := levelcontrol.UnitFromLevel(127); got != 0.5 {
		t.Errorf("UnitFromLevel(127) = %v, want 0.5", got)
	}
}

// rampDevice ramps by itself: a ramp of d > 0 arrives after d. It owns the
// On/Off state too, and switching it on applies the source's OnLevel.
type rampDevice struct {
	mu      sync.Mutex
	level   uint8
	known   bool
	on      bool
	log     []string
	refuse  error
	instant bool // every ramp arrives at once
	src     *levelcontrol.NativeSource
	notify  []func()
	arrival *time.Timer
}

func (d *rampDevice) Level() (uint8, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.level, d.known
}

func (d *rampDevice) Ramp(_ context.Context, level uint8, dur time.Duration, withOnOff bool) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.refuse != nil {
		return d.refuse
	}
	d.log = append(d.log, fmt.Sprintf("ramp %d %v %t", level, dur, withOnOff))
	if d.arrival != nil {
		d.arrival.Stop()
	}
	if dur == 0 || d.instant {
		d.level, d.known = level, true
		return nil
	}
	d.arrival = time.AfterFunc(dur, func() {
		d.mu.Lock()
		d.level, d.known = level, true
		cbs := slices.Clone(d.notify)
		d.mu.Unlock()
		for _, cb := range cbs {
			cb()
		}
	})
	return nil
}

func (d *rampDevice) Stop(context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.log = append(d.log, "stop")
	if d.arrival != nil {
		d.arrival.Stop()
	}
	return nil
}

func (d *rampDevice) OnOff() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.on
}

func (d *rampDevice) SetOnOff(_ context.Context, on bool) error {
	onLevel, has := d.src.OnLevel()
	d.mu.Lock()
	defer d.mu.Unlock()
	d.on = on
	if on {
		d.log = append(d.log, "on")
		if has {
			d.level, d.known = onLevel, true
		}
	} else {
		d.log = append(d.log, "off")
	}
	return nil
}

func (d *rampDevice) OnMatterValueChanged(cb func()) func() {
	d.mu.Lock()
	d.notify = append(d.notify, cb)
	d.mu.Unlock()
	return func() {}
}

func (d *rampDevice) take() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	l := d.log
	d.log = nil
	return l
}

// nativeServer is a LevelControl server on the hand-off path over a
// NativeSource of a device at level, on or off.
func nativeServer(level uint8, on, lighting bool, cfg levelcontrol.NativeConfig) (*levelcontrol.Server, *rampDevice) {
	d := &rampDevice{level: level, known: true, on: on}
	cfg.Lighting = lighting
	d.src = levelcontrol.NewNativeSource(d, d, cfg)
	return levelcontrol.NewServer(levelcontrol.Config{Source: d.src, Lighting: lighting}), d
}

func wantLog(t *testing.T, d *rampDevice, want ...string) {
	t.Helper()
	if got := d.take(); !slices.Equal(got, want) {
		t.Errorf("device saw %q, want %q", got, want)
	}
}

// TestNativeSourceCommands drives every command through the server and
// checks the one ramp each becomes. Mirrors matter.js
// LevelControlServer.ts moveToLevelLogic :326-342, moveLogic :388-405,
// stepLogic :464-479 and stopLogic :505-506.
func TestNativeSourceCommands(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	srv, d := nativeServer(100, true, true, levelcontrol.NativeConfig{})
	d.instant = true

	steps := []struct {
		name   string
		cmd    uint32
		fields any
		want   []string
	}{
		{
			"MoveToLevel over 2 s", levelcontrol.CmdMoveToLevel,
			wire.MoveToLevelRequest{Level: 150, TransitionTime: u16(20)},
			[]string{"ramp 150 2s false"},
		},
		{
			"MoveToLevel null time is at once", levelcontrol.CmdMoveToLevel,
			wire.MoveToLevelRequest{Level: 120},
			[]string{"ramp 120 0s false"},
		},
		{
			"MoveToLevel 0 is cropped to the Lighting minimum", levelcontrol.CmdMoveToLevel,
			wire.MoveToLevelRequest{Level: 0, TransitionTime: u16(0)},
			[]string{"ramp 1 0s false"},
		},
		{
			"MoveToLevel to where the device is ramps nothing", levelcontrol.CmdMoveToLevel,
			wire.MoveToLevelRequest{Level: 1, TransitionTime: u16(5)},
			nil,
		},
		{
			"Move up at 10/s from 1: 25.3 s", levelcontrol.CmdMove,
			wire.MoveRequest{MoveMode: levelcontrol.MoveModeUp, Rate: u8(10)},
			[]string{"ramp 254 25.3s false"},
		},
		{
			"Move down with a null rate is at once", levelcontrol.CmdMove,
			wire.MoveRequest{MoveMode: levelcontrol.MoveModeDown},
			[]string{"ramp 1 0s false"},
		},
		{
			"Step up 100 over 4 s", levelcontrol.CmdStep,
			wire.StepRequest{StepMode: levelcontrol.StepModeUp, StepSize: 100, TransitionTime: u16(40)},
			[]string{"ramp 101 4s false"},
		},
		{
			"Step up cropped arrives sooner: 153 of 200 over 2 s", levelcontrol.CmdStep,
			wire.StepRequest{StepMode: levelcontrol.StepModeUp, StepSize: 200, TransitionTime: u16(20)},
			[]string{"ramp 254 1.5s false"},
		},
		{
			"Step down with a null time is at once", levelcontrol.CmdStep,
			wire.StepRequest{StepMode: levelcontrol.StepModeDown, StepSize: 4},
			[]string{"ramp 250 0s false"},
		},
		{"Stop", levelcontrol.CmdStop, wire.StopRequest{}, []string{"stop"}},
		{"StopWithOnOff", levelcontrol.CmdStopWithOnOff, wire.StopRequest{}, []string{"stop"}},
	}
	for _, s := range steps {
		if _, err := srv.MatterInvoke(ctx, s.cmd, s.fields); err != nil {
			t.Fatalf("%s: %v", s.name, err)
		}
		if got := d.take(); !slices.Equal(got, s.want) {
			t.Errorf("%s: device saw %q, want %q", s.name, got, s.want)
		}
	}
	if v, _ := srv.MatterRead(levelcontrol.AttrCurrentLevel); v != uint8(250) {
		t.Errorf("CurrentLevel = %v, want the device's 250", v)
	}
}

// TestNativeSourceExecuteIfOff holds the gate to matter.js
// LevelControlServer.ts:729-736 #optionsAllowExecution: while the device
// is off, a plain command runs only with ExecuteIfOff in effect (from the
// attribute or the command's override); the "with On/Off" commands and
// StopWithOnOff (:491-500) are not gated.
func TestNativeSourceExecuteIfOff(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	srv, d := nativeServer(100, false, false, levelcontrol.NativeConfig{})
	gated := []struct {
		cmd    uint32
		fields any
	}{
		{levelcontrol.CmdMoveToLevel, wire.MoveToLevelRequest{Level: 50}},
		{levelcontrol.CmdMove, wire.MoveRequest{MoveMode: levelcontrol.MoveModeUp}},
		{levelcontrol.CmdStep, wire.StepRequest{StepMode: levelcontrol.StepModeUp, StepSize: 5}},
		{levelcontrol.CmdStop, wire.StopRequest{}},
	}
	for _, g := range gated {
		if _, err := srv.MatterInvoke(ctx, g.cmd, g.fields); err != nil {
			t.Fatalf("0x%x: %v", g.cmd, err)
		}
	}
	wantLog(t, d) // all four gated out, reported as success

	// The override lets one command through without touching the
	// attribute.
	ex := levelcontrol.OptionExecuteIfOff
	if _, err := srv.MatterInvoke(ctx, levelcontrol.CmdMoveToLevel,
		wire.MoveToLevelRequest{Level: 50, OptionsMask: ex, OptionsOverride: ex}); err != nil {
		t.Fatal(err)
	}
	wantLog(t, d, "ramp 50 0s false")
	// A mask bit with a clear override closes the gate even when the
	// attribute opens it.
	if err := srv.MatterWrite(ctx, levelcontrol.AttrOptions, uint64(ex)); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.MatterInvoke(ctx, levelcontrol.CmdStop, wire.StopRequest{OptionsMask: ex}); err != nil {
		t.Fatal(err)
	}
	wantLog(t, d)
	if _, err := srv.MatterInvoke(ctx, levelcontrol.CmdStop, wire.StopRequest{}); err != nil {
		t.Fatal(err)
	}
	wantLog(t, d, "stop")

	// Ungated: StopWithOnOff with the attribute cleared again.
	if err := srv.MatterWrite(ctx, levelcontrol.AttrOptions, uint64(0)); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.MatterInvoke(ctx, levelcontrol.CmdStopWithOnOff, wire.StopRequest{}); err != nil {
		t.Fatal(err)
	}
	wantLog(t, d, "stop")

	// With the device on, the plain command runs.
	_ = d.SetOnOff(ctx, true)
	d.take()
	if _, err := srv.MatterInvoke(ctx, levelcontrol.CmdStep,
		wire.StepRequest{StepMode: levelcontrol.StepModeDown, StepSize: 10}); err != nil {
		t.Fatal(err)
	}
	wantLog(t, d, "ramp 40 0s false")
}

// TestNativeSourceOnOffCoupling holds the "with On/Off" coupling to
// matter.js LevelControlServer.ts:529-550 transition and :557-633: on
// before a ramp whose target lies above the minimum (the switch-on applies
// OnLevel, from which the ramp's duration is taken), off once a ramp to
// the minimum has arrived, and a later command cancels a pending
// switch-off.
func TestNativeSourceOnOffCoupling(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		srv, d := nativeServer(1, false, true, levelcontrol.NativeConfig{OnLevel: u8(51)})

		// MoveWithOnOff up from off: on first (OnLevel 51), then 203 left
		// at 10/s.
		if _, err := srv.MatterInvoke(ctx, levelcontrol.CmdMoveWithOnOff,
			wire.MoveRequest{MoveMode: levelcontrol.MoveModeUp, Rate: u8(10)}); err != nil {
			t.Fatal(err)
		}
		wantLog(t, d, "on", "ramp 254 20.3s true")
		sleep(21 * time.Second)

		// MoveToLevelWithOnOff to the minimum over 1 s: the device stays
		// on while it ramps and goes off once it is there.
		if _, err := srv.MatterInvoke(ctx, levelcontrol.CmdMoveToLevelWithOnOff,
			wire.MoveToLevelRequest{Level: 0, TransitionTime: u16(10)}); err != nil {
			t.Fatal(err)
		}
		wantLog(t, d, "ramp 1 1s true")
		sleep(500 * time.Millisecond)
		if !d.OnOff() {
			t.Fatal("switched off before the ramp arrived")
		}
		sleep(time.Second)
		wantLog(t, d, "off")

		// StepWithOnOff down to the minimum when already there and off:
		// no ramp, no switch.
		if _, err := srv.MatterInvoke(ctx, levelcontrol.CmdStepWithOnOff,
			wire.StepRequest{StepMode: levelcontrol.StepModeDown, StepSize: 10, TransitionTime: u16(5)}); err != nil {
			t.Fatal(err)
		}
		wantLog(t, d)

		// StepWithOnOff up from off: on (OnLevel 51) — the target was
		// taken from the level before the coupling (1 + 20), as matter.js
		// evaluates it (:478), and the rest of the way at 20 per 2 s.
		if _, err := srv.MatterInvoke(ctx, levelcontrol.CmdStepWithOnOff,
			wire.StepRequest{StepMode: levelcontrol.StepModeUp, StepSize: 20, TransitionTime: u16(20)}); err != nil {
			t.Fatal(err)
		}
		wantLog(t, d, "on", "ramp 21 3s true")
		sleep(4 * time.Second)

		// MoveWithOnOff down, then StopWithOnOff part-way: the pending
		// switch-off is dropped with the ramp.
		if _, err := srv.MatterInvoke(ctx, levelcontrol.CmdMoveWithOnOff,
			wire.MoveRequest{MoveMode: levelcontrol.MoveModeDown, Rate: u8(10)}); err != nil {
			t.Fatal(err)
		}
		wantLog(t, d, "ramp 1 2s true")
		sleep(time.Second)
		if _, err := srv.MatterInvoke(ctx, levelcontrol.CmdStopWithOnOff, wire.StopRequest{}); err != nil {
			t.Fatal(err)
		}
		sleep(5 * time.Second)
		wantLog(t, d, "stop")
		if !d.OnOff() {
			t.Error("a stopped ramp switched the device off")
		}

		// The plain commands never couple: MoveToLevel to the minimum
		// leaves the device on.
		if _, err := srv.MatterInvoke(ctx, levelcontrol.CmdMoveToLevel,
			wire.MoveToLevelRequest{Level: 1}); err != nil {
			t.Fatal(err)
		}
		wantLog(t, d, "ramp 1 0s false")
		// MoveToLevelWithOnOff to the minimum at once, already there:
		// switched off at once (#coupleOnCommit).
		if _, err := srv.MatterInvoke(ctx, levelcontrol.CmdMoveToLevelWithOnOff,
			wire.MoveToLevelRequest{Level: 1}); err != nil {
			t.Fatal(err)
		}
		wantLog(t, d, "off")
	})
}

// TestNativeSourceAttributesAndRefusals covers the attribute storage, the
// Persist hook, a null level and a device refusal.
func TestNativeSourceAttributesAndRefusals(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var saved []string
	fail := errors.New("flash full")
	var refuse error
	cfg := levelcontrol.NativeConfig{
		Options: levelcontrol.OptionExecuteIfOff,
		OnLevel: u8(80),
		Persist: func(_ context.Context, options uint8, onLevel *uint8) error {
			if refuse != nil {
				return refuse
			}
			s := "null"
			if onLevel != nil {
				s = strconv.Itoa(int(*onLevel))
			}
			saved = append(saved, fmt.Sprintf("%d/%s", options, s))
			return nil
		},
	}
	srv, d := nativeServer(100, true, false, cfg)
	if v, _ := srv.MatterRead(levelcontrol.AttrOptions); v != levelcontrol.OptionExecuteIfOff {
		t.Errorf("Options = %v, want the configured 1", v)
	}
	if v, _ := srv.MatterRead(levelcontrol.AttrOnLevel); v != uint8(80) {
		t.Errorf("OnLevel = %v, want the configured 80", v)
	}
	if err := srv.MatterWrite(ctx, levelcontrol.AttrOnLevel, uint64(30)); err != nil {
		t.Fatal(err)
	}
	if err := srv.MatterWrite(ctx, levelcontrol.AttrOnLevel, nil); err != nil {
		t.Fatal(err)
	}
	if err := srv.MatterWrite(ctx, levelcontrol.AttrOptions, uint64(0)); err != nil {
		t.Fatal(err)
	}
	if want := []string{"1/30", "1/null", "0/null"}; !slices.Equal(saved, want) {
		t.Errorf("persisted %q, want %q", saved, want)
	}
	if v, _ := srv.MatterRead(levelcontrol.AttrOnLevel); v != nil {
		t.Errorf("OnLevel = %v, want null", v)
	}
	refuse = fail
	if err := srv.MatterWrite(ctx, levelcontrol.AttrOnLevel, uint64(9)); !errors.Is(err, fail) {
		t.Errorf("refused persist: %v, want %v", err, fail)
	}
	if err := srv.MatterWrite(ctx, levelcontrol.AttrOptions, uint64(1)); !errors.Is(err, fail) {
		t.Errorf("refused persist: %v, want %v", err, fail)
	}
	if v, _ := srv.MatterRead(levelcontrol.AttrOnLevel); v != nil {
		t.Errorf("OnLevel after a refused write = %v, want null kept", v)
	}
	if v, _ := srv.MatterRead(levelcontrol.AttrOptions); v != uint8(0) {
		t.Errorf("Options after a refused write = %v, want 0 kept", v)
	}

	// A device refusal is the command's failure.
	d.mu.Lock()
	d.refuse = fail
	d.mu.Unlock()
	if _, err := srv.MatterInvoke(ctx, levelcontrol.CmdMoveToLevel, wire.MoveToLevelRequest{Level: 3}); !errors.Is(err, fail) {
		t.Errorf("refused ramp: %v, want %v", err, fail)
	}
	// A null level cannot be stepped (matter.js stepLogic reads the
	// currentLevel getter, LevelControlServer.ts:478, which throws
	// Failure, :103-111).
	d.mu.Lock()
	d.refuse, d.known = nil, false
	d.mu.Unlock()
	_, err := srv.MatterInvoke(ctx, levelcontrol.CmdStep, wire.StepRequest{StepSize: 1})
	wantStatus(t, err, im.StatusFailure)
	if v, _ := srv.MatterRead(levelcontrol.AttrCurrentLevel); v != nil {
		t.Errorf("CurrentLevel of an unknown level = %v, want null", v)
	}
	d.take()

	// The device's notifier reaches the server.
	fired := 0
	srv.OnMatterValueChanged(func() { fired++ })
	d.mu.Lock()
	cbs := slices.Clone(d.notify)
	d.mu.Unlock()
	for _, cb := range cbs {
		cb()
	}
	if fired != 1 {
		t.Errorf("notifier fired %d times, want 1", fired)
	}
}

// silentRamp has no notifier and no On/Off cluster beside it.
type silentRamp struct{ log []string }

func (s *silentRamp) Level() (uint8, bool) { return 10, true }
func (s *silentRamp) Ramp(_ context.Context, l uint8, d time.Duration, w bool) error {
	s.log = append(s.log, fmt.Sprintf("ramp %d %v %t", l, d, w))
	return nil
}
func (s *silentRamp) Stop(context.Context) error { s.log = append(s.log, "stop"); return nil }

// TestNativeSourceWithoutOnOff: no On/Off cluster means every command
// executes and none couples (matter.js LevelControlServer.ts:733, :558).
func TestNativeSourceWithoutOnOff(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dev := &silentRamp{}
	src := levelcontrol.NewNativeSource(dev, nil, levelcontrol.NativeConfig{})
	srv := levelcontrol.NewServer(levelcontrol.Config{Source: src})
	srv.OnMatterValueChanged(func() {})()
	for _, c := range []struct {
		cmd    uint32
		fields any
	}{
		{levelcontrol.CmdMoveToLevel, wire.MoveToLevelRequest{Level: 0}},
		{levelcontrol.CmdMoveToLevelWithOnOff, wire.MoveToLevelRequest{Level: 0, TransitionTime: u16(3)}},
		{levelcontrol.CmdStop, wire.StopRequest{}},
	} {
		if _, err := srv.MatterInvoke(ctx, c.cmd, c.fields); err != nil {
			t.Fatal(err)
		}
	}
	if want := []string{"ramp 0 0s false", "ramp 0 300ms true", "stop"}; !slices.Equal(dev.log, want) {
		t.Errorf("device saw %q, want %q", dev.log, want)
	}
}

// TestNativeSourceRatedMoveOnUnknownLevel (#127): a Move with a Rate on a
// device that has reported no level ramps to the MoveMode's bound at once
// and succeeds — matter.js never reads currentLevel for a Move
// (LevelControlServer.ts:388-403, :529-550) and, with managed transitions
// off, sets the cropped target immediately (Transitions.ts:120-148).
func TestNativeSourceRatedMoveOnUnknownLevel(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, c := range []struct {
		name string
		cmd  uint32
		req  wire.MoveRequest
		want []string
	}{
		{"up", levelcontrol.CmdMove, wire.MoveRequest{MoveMode: levelcontrol.MoveModeUp, Rate: u8(10)}, []string{"ramp 254 0s false"}},
		{"down with on/off", levelcontrol.CmdMoveWithOnOff, wire.MoveRequest{MoveMode: levelcontrol.MoveModeDown, Rate: u8(10)}, []string{"ramp 1 0s true", "off"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			srv, d := nativeServer(0, true, true, levelcontrol.NativeConfig{})
			d.mu.Lock()
			d.known = false
			d.mu.Unlock()
			if _, err := srv.MatterInvoke(ctx, c.cmd, c.req); err != nil {
				t.Fatalf("rated Move on an unknown level: %v, want success", err)
			}
			wantLog(t, d, c.want...)
		})
	}
}

// TestNativeSourceSettlesBelowMinLevel (#127): a dimmer that reports 0
// after a ramp to the Lighting minimum of 1 still has its owed switch-off
// paid — matter.js decides it with `<= minLevel` (LevelControlServer.ts
// couple).
func TestNativeSourceSettlesBelowMinLevel(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	srv, d := nativeServer(100, true, true, levelcontrol.NativeConfig{})
	if _, err := srv.MatterInvoke(ctx, levelcontrol.CmdMoveToLevelWithOnOff, wire.MoveToLevelRequest{Level: 1, TransitionTime: u16(5)}); err != nil {
		t.Fatalf("MoveToLevelWithOnOff: %v", err)
	}
	if !d.OnOff() {
		t.Fatal("switched off before the ramp arrived")
	}
	// The device lands on 0 — its "off" — instead of the 1 it was sent
	// to, and notifies.
	d.mu.Lock()
	if d.arrival != nil {
		d.arrival.Stop()
	}
	d.level = 0
	cbs := slices.Clone(d.notify)
	d.mu.Unlock()
	for _, cb := range cbs {
		cb()
	}
	if d.OnOff() {
		t.Error("OnOff still true after the device reported 0 below MinLevel 1")
	}
}

// moverDevice is a rampDevice that declines Move (a speaker's volume).
type moverDevice struct {
	*rampDevice
	moves int
}

func (m *moverDevice) MoveLevel(context.Context, wire.MoveRequest, bool) error {
	m.moves++
	return nil
}

// TestNativeSourceMover (#129): a device implementing NativeMover takes
// Move and MoveWithOnOff itself (matter.js moveLogic override,
// LevelControlServer.ts:378-403) — no ramp, no stop, no coupling; one
// without it ramps to the bound.
func TestNativeSourceMover(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	d := &rampDevice{level: 100, known: true, on: true}
	m := &moverDevice{rampDevice: d}
	d.src = levelcontrol.NewNativeSource(m, d, levelcontrol.NativeConfig{Lighting: true})
	srv := levelcontrol.NewServer(levelcontrol.Config{Source: d.src, Lighting: true})
	for _, cmd := range []uint32{levelcontrol.CmdMove, levelcontrol.CmdMoveWithOnOff} {
		if _, err := srv.MatterInvoke(ctx, cmd, wire.MoveRequest{MoveMode: levelcontrol.MoveModeDown, Rate: u8(10)}); err != nil {
			t.Fatalf("Move %#x through a NativeMover: %v", cmd, err)
		}
	}
	if m.moves != 2 {
		t.Errorf("MoveLevel called %d times, want 2", m.moves)
	}
	wantLog(t, d)
	if !d.OnOff() {
		t.Error("MoveWithOnOff down switched off a device whose mover kept its level")
	}

	plain, pd := nativeServer(100, true, true, levelcontrol.NativeConfig{})
	if _, err := plain.MatterInvoke(ctx, levelcontrol.CmdMove, wire.MoveRequest{MoveMode: levelcontrol.MoveModeUp}); err != nil {
		t.Fatalf("Move without a NativeMover: %v", err)
	}
	wantLog(t, pd, "ramp 254 0s false")
}

// slowReportDevice takes a ramp but does not change its reported level
// synchronously (a device that confirms after a round trip, with no
// change notifier).
type slowReportDevice struct{ *rampDevice }

func (s *slowReportDevice) Ramp(_ context.Context, level uint8, dur time.Duration, withOnOff bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.log = append(s.log, fmt.Sprintf("ramp %d %v %t", level, dur, withOnOff))
	return nil
}

// TestNativeSourceImmediateSwitchOff (#129): MoveToLevelWithOnOff to the
// minimum with no transition switches off before the command returns,
// without waiting for the device's report — matter.js sets the target in
// state at once (Transitions.ts:147, :238-241) and couples off on that
// commit (LevelControlServer.ts:624-627).
func TestNativeSourceImmediateSwitchOff(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	d := &rampDevice{level: 100, known: true, on: true}
	s := &slowReportDevice{rampDevice: d}
	d.src = levelcontrol.NewNativeSource(s, d, levelcontrol.NativeConfig{Lighting: true})
	srv := levelcontrol.NewServer(levelcontrol.Config{Source: d.src, Lighting: true})
	if _, err := srv.MatterInvoke(ctx, levelcontrol.CmdMoveToLevelWithOnOff, wire.MoveToLevelRequest{Level: 1, TransitionTime: u16(0)}); err != nil {
		t.Fatalf("MoveToLevelWithOnOff(1, 0): %v", err)
	}
	if d.OnOff() {
		t.Error("OnOff still true after an immediate MoveToLevelWithOnOff to MinLevel returned")
	}
	if lvl, _ := d.Level(); lvl != 100 {
		t.Fatalf("test device changed its level synchronously (%d)", lvl)
	}
}

// failingOnOff refuses to switch off.
type failingOnOff struct{ on bool }

func (f *failingOnOff) OnOff() bool { return f.on }

func (f *failingOnOff) SetOnOff(_ context.Context, on bool) error {
	if !on {
		return errors.New("refused")
	}
	f.on = on
	return nil
}

// TestNativeSourceImmediateSwitchOffRefused: the immediate switch-off of
// a "with On/Off" command to MinLevel is the command's result — a refusal
// fails the command, as matter.js's preCommit failing aborts the
// transaction (LevelControlServer.ts:624-627).
func TestNativeSourceImmediateSwitchOffRefused(t *testing.T) {
	t.Parallel()
	d := &rampDevice{level: 100, known: true}
	src := levelcontrol.NewNativeSource(d, &failingOnOff{on: true}, levelcontrol.NativeConfig{Lighting: true})
	err := src.MoveToLevelWithOnOff(context.Background(), lvldef.MoveToLevelWithOnOffRequest{Level: 1, TransitionTime: spec.NullOf[uint16]()})
	if err == nil || !strings.Contains(err.Error(), "switch off") {
		t.Fatalf("refused switch-off: %v, want the switch-off error", err)
	}
}

// switcherDevice writes On/Off and level together (#131).
type switcherDevice struct {
	*rampDevice
	calls []string
}

func (s *switcherDevice) SwitchOn(_ context.Context, level uint8, d time.Duration) error {
	s.calls = append(s.calls, fmt.Sprintf("on %d %v", level, d))
	return nil
}

func (s *switcherDevice) SwitchOff(_ context.Context, d time.Duration) error {
	s.calls = append(s.calls, fmt.Sprintf("off %v", d))
	return nil
}

// TestNativeSourceSwitcher (#131): a NativeSwitcher takes a "with On/Off"
// command that turns the device on as one SwitchOn and one that ends at
// the minimum as one SwitchOff — neither SetOnOff nor Ramp.
func TestNativeSourceSwitcher(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	d := &rampDevice{level: 1, known: true, on: false}
	sw := &switcherDevice{rampDevice: d}
	d.src = levelcontrol.NewNativeSource(sw, d, levelcontrol.NativeConfig{Lighting: true})
	srv := levelcontrol.NewServer(levelcontrol.Config{Source: d.src, Lighting: true})
	if _, err := srv.MatterInvoke(ctx, levelcontrol.CmdMoveToLevelWithOnOff, wire.MoveToLevelRequest{Level: 80, TransitionTime: u16(10)}); err != nil {
		t.Fatalf("MoveToLevelWithOnOff from off: %v", err)
	}
	d.mu.Lock()
	d.on = true
	d.mu.Unlock()
	if _, err := srv.MatterInvoke(ctx, levelcontrol.CmdMoveToLevelWithOnOff, wire.MoveToLevelRequest{Level: 1, TransitionTime: u16(5)}); err != nil {
		t.Fatalf("MoveToLevelWithOnOff to the minimum: %v", err)
	}
	if want := []string{"on 80 1s", "off 500ms"}; !slices.Equal(sw.calls, want) {
		t.Errorf("switcher saw %q, want %q", sw.calls, want)
	}
	wantLog(t, d)
}

// TestNativeSourceSettlesAtDeadline (#131): a "with On/Off" ramp to the
// minimum with a duration switches off when the duration has elapsed even
// if the device never reports a level — matter.js's transition ends at
// its target by construction (LevelControlServer.ts:624-627).
func TestNativeSourceSettlesAtDeadline(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	d := &rampDevice{level: 100, known: true, on: true}
	s := &slowReportDevice{rampDevice: d}
	d.src = levelcontrol.NewNativeSource(s, d, levelcontrol.NativeConfig{Lighting: true})
	srv := levelcontrol.NewServer(levelcontrol.Config{Source: d.src, Lighting: true})
	if _, err := srv.MatterInvoke(ctx, levelcontrol.CmdMoveToLevelWithOnOff, wire.MoveToLevelRequest{Level: 1, TransitionTime: u16(1)}); err != nil {
		t.Fatalf("MoveToLevelWithOnOff(1, 0.1 s): %v", err)
	}
	if !d.OnOff() {
		t.Fatal("switched off before the ramp's duration elapsed")
	}
	deadline := time.Now().Add(2 * time.Second)
	for d.OnOff() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if d.OnOff() {
		t.Error("OnOff still true after the ramp's duration on a device that never reported its level")
	}
}
