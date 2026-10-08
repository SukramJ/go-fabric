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
	ccdef "github.com/SukramJ/go-fabric/cluster/spec/colorcontrol"
	"github.com/SukramJ/go-fabric/im"
)

// colorSink records every colour the server pushes.
type colorSink struct {
	mu     sync.Mutex
	colors []light.Color
	err    error
}

func (c *colorSink) SetColor(_ context.Context, col light.Color) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return c.err
	}
	c.colors = append(c.colors, col)
	return nil
}

func (c *colorSink) last() light.Color {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.colors) == 0 {
		return light.Color{}
	}
	return c.colors[len(c.colors)-1]
}

// reportLog collects the reports of chosen attributes with the value read
// at report time and the time since the start, as a subscription sees
// them.
type reportLog struct {
	mu      sync.Mutex
	start   time.Time
	entries []report
}

type report struct {
	attr  uint32
	value uint64
	at    time.Duration
}

func watch(srv *light.ColorControlServer, attrs ...uint32) *reportLog {
	r := &reportLog{start: time.Now()}
	srv.OnMatterAttributesChanged(func(ids []uint32) {
		for _, id := range ids {
			if !slices.Contains(attrs, id) {
				continue
			}
			v, _ := srv.MatterRead(id)
			var n uint64
			switch x := v.(type) {
			case uint8:
				n = uint64(x)
			case uint16:
				n = uint64(x)
			}
			r.mu.Lock()
			r.entries = append(r.entries, report{id, n, time.Since(r.start)})
			r.mu.Unlock()
		}
	})
	return r
}

func (r *reportLog) values(attr uint32) []uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []uint64
	for _, e := range r.entries {
		if e.attr == attr {
			out = append(out, e.value)
		}
	}
	return out
}

// colorLight is matter.js ColorControlServerTest's colorLightState: every
// feature, CT 153..370, in the hue and saturation mode, ExecuteIfOff set,
// transitions managed.
func colorLight(t *testing.T, features light.ColorFeature, managed bool) *light.ColorControlServer {
	t.Helper()
	mode := light.ColorModeHueSaturation
	if features&light.ColorFeatureHueSaturation == 0 {
		mode = light.ColorModeXY
	}
	srv, err := light.NewColorControl(light.ColorControlServerConfig{
		Features: features, MinMireds: 153, MaxMireds: 370, InitialMireds: 250,
		InitialColorMode: &mode, ManageTransitions: managed, OnOff: &onOff{on: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.MatterWrite(context.Background(), ccdef.AttrOptions, uint64(1)); err != nil {
		t.Fatal(err)
	}
	return srv
}

func invoke(t *testing.T, srv *light.ColorControlServer, cmd uint32, fields any) {
	t.Helper()
	if _, err := srv.MatterInvoke(context.Background(), cmd, fields); err != nil {
		t.Fatalf("command 0x%02X: %v", cmd, err)
	}
}

func read(t *testing.T, srv *light.ColorControlServer, attr uint32) uint64 {
	t.Helper()
	v, ok := srv.MatterRead(attr)
	if !ok {
		t.Fatalf("0x%04X not served", attr)
	}
	switch x := v.(type) {
	case uint8:
		return uint64(x)
	case uint16:
		return uint64(x)
	}
	t.Fatalf("0x%04X reads %T", attr, v)
	return 0
}

// TestHueTransitionsDownwardsWithQuietReports ports matter.js
// ColorControlServerTest "transitions cyclic hue downwards with correct
// events": from hue 128, MoveToHue 129 Down over 6 s travels 253 the long
// way round, about 42 a second; CurrentHue reports at most once a second
// and its last value at the end, RemainingTime 60 then 0.
func TestHueTransitionsDownwardsWithQuietReports(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		srv := colorLight(t, allColorFeatures, true)
		invoke(t, srv, ccdef.CmdMoveToHue, ccdef.MoveToHueRequest{Hue: 128}) // "Startup": hue 128 at once
		r := watch(srv, ccdef.AttrCurrentHue, ccdef.AttrRemainingTime)
		invoke(t, srv, ccdef.CmdMoveToHue, ccdef.MoveToHueRequest{Hue: 129, Direction: ccdef.DirectionDown, TransitionTime: 60})
		time.Sleep(10 * time.Second)
		synctest.Wait()
		if got := r.values(ccdef.AttrRemainingTime); !slices.Equal(got, []uint64{60, 0}) {
			t.Errorf("RemainingTime reports %v, want [60 0]", got)
		}
		hues := r.values(ccdef.AttrCurrentHue)
		// matter.js: 86, 44, 1, 213, 171 a second apart, then 129. The
		// engine's first step counts one interval ahead (cluster/transition
		// package doc), so a reported value may sit one step (~4) further
		// round the wheel.
		want := []uint64{86, 44, 1, 213, 171, 129}
		if len(hues) != len(want) {
			t.Fatalf("CurrentHue reports %v, want six like %v", hues, want)
		}
		for i := range want {
			d := (int(hues[i]) - int(want[i]) + 255) % 255 // the wheel's 255 values
			if d > 5 && d < 250 {
				t.Errorf("CurrentHue report %d = %d, matter.js %d", i, hues[i], want[i])
			}
		}
		if read(t, srv, ccdef.AttrCurrentHue) != 129 || read(t, srv, ccdef.AttrRemainingTime) != 0 {
			t.Error("the transition did not end at hue 129")
		}
	})
}

// TestRemainingTimeOnStops ports matter.js ColorControlServerTest
// "ColorControl stop": StopMoveStep and MoveColor with both rates 0 report
// RemainingTime 0 once, however many transitions they end; a command that
// switches the mode reports only the new remaining time.
func TestRemainingTimeOnStops(t *testing.T) {
	cases := []struct {
		name     string
		features light.ColorFeature
		run      func(t *testing.T, srv *light.ColorControlServer)
		want     []uint64
	}{
		{"StopMoveStep after MoveToColorTemperature", allColorFeatures, func(t *testing.T, srv *light.ColorControlServer) {
			t.Helper()
			invoke(t, srv, ccdef.CmdMoveToColorTemperature, ccdef.MoveToColorTemperatureRequest{ColorTemperatureMireds: 370, TransitionTime: 100})
			invoke(t, srv, ccdef.CmdStopMoveStep, ccdef.StopMoveStepRequest{})
		}, []uint64{100, 0}},
		{"StopMoveStep ends several transitions", allColorFeatures, func(t *testing.T, srv *light.ColorControlServer) {
			t.Helper()
			invoke(t, srv, ccdef.CmdMoveToHue, ccdef.MoveToHueRequest{Hue: 200, Direction: ccdef.DirectionUp, TransitionTime: 200})
			invoke(t, srv, ccdef.CmdMoveToSaturation, ccdef.MoveToSaturationRequest{Saturation: 254, TransitionTime: 100})
			invoke(t, srv, ccdef.CmdStopMoveStep, ccdef.StopMoveStepRequest{})
		}, []uint64{200, 0}},
		{"MoveColor with zero rates", allColorFeatures, func(t *testing.T, srv *light.ColorControlServer) {
			t.Helper()
			invoke(t, srv, ccdef.CmdMoveToColor, ccdef.MoveToColorRequest{ColorX: 32768, ColorY: 19660, TransitionTime: 100})
			invoke(t, srv, ccdef.CmdMoveColor, ccdef.MoveColorRequest{})
		}, []uint64{100, 0}},
		{"StopMoveStep without the ColorLoop feature", allColorFeatures &^ light.ColorFeatureColorLoop, func(t *testing.T, srv *light.ColorControlServer) {
			t.Helper()
			invoke(t, srv, ccdef.CmdMoveToColorTemperature, ccdef.MoveToColorTemperatureRequest{ColorTemperatureMireds: 370, TransitionTime: 100})
			invoke(t, srv, ccdef.CmdStopMoveStep, ccdef.StopMoveStepRequest{})
		}, []uint64{100, 0}},
		{"a mode switch reports only the new remaining time", allColorFeatures, func(t *testing.T, srv *light.ColorControlServer) {
			t.Helper()
			invoke(t, srv, ccdef.CmdMoveToHue, ccdef.MoveToHueRequest{Hue: 200, Direction: ccdef.DirectionUp, TransitionTime: 200})
			invoke(t, srv, ccdef.CmdMoveToColorTemperature, ccdef.MoveToColorTemperatureRequest{ColorTemperatureMireds: 370, TransitionTime: 100})
			if read(t, srv, ccdef.AttrColorMode) != uint64(light.ColorModeColorTemperature) {
				t.Error("ColorMode did not switch to the colour temperature")
			}
		}, []uint64{200, 100}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				srv := colorLight(t, tc.features, true)
				r := watch(srv, ccdef.AttrRemainingTime)
				tc.run(t, srv)
				synctest.Wait()
				if got := r.values(ccdef.AttrRemainingTime); !slices.Equal(got, tc.want) {
					t.Errorf("RemainingTime reports %v, want %v", got, tc.want)
				}
				srv.MatterQuiesce()
			})
		})
	}
}

// TestStopMoveStepLeavesTheColorLoop ports "leaves the remaining time alone
// on StopMoveStep while a color loop runs": the loop survives StopMoveStep,
// and so does the remaining time it does not end.
func TestStopMoveStepLeavesTheColorLoop(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		srv := colorLight(t, allColorFeatures, true)
		r := watch(srv, ccdef.AttrRemainingTime)
		invoke(t, srv, ccdef.CmdColorLoopSet, ccdef.ColorLoopSetRequest{
			UpdateFlags: ccdef.UpdateFlagsUpdateAction, Action: ccdef.ColorLoopActionActivateFromEnhancedCurrentHue,
			Direction: ccdef.ColorLoopDirectionIncrement, Time: 25,
		})
		invoke(t, srv, ccdef.CmdMoveToSaturation, ccdef.MoveToSaturationRequest{Saturation: 254, TransitionTime: 100})
		invoke(t, srv, ccdef.CmdStopMoveStep, ccdef.StopMoveStepRequest{})
		synctest.Wait()
		if got := r.values(ccdef.AttrRemainingTime); !slices.Equal(got, []uint64{100}) {
			t.Errorf("RemainingTime reports %v, want [100]", got)
		}
		if read(t, srv, ccdef.AttrColorLoopActive) != 1 {
			t.Fatal("StopMoveStep ended the colour loop")
		}
		before := read(t, srv, ccdef.AttrEnhancedCurrentHue)
		time.Sleep(time.Second)
		synctest.Wait()
		if read(t, srv, ccdef.AttrEnhancedCurrentHue) == before {
			t.Error("the enhanced hue stopped looping")
		}
		srv.MatterQuiesce()
	})
}

// TestRemainingTimeOfAHueTransition ports matter.js RemainingTimeTest:
// MoveToHue 200 Up over 15 s reads a remaining time part-way and 0 at the
// end.
func TestRemainingTimeOfAHueTransition(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		srv := colorLight(t, light.ColorFeatureHueSaturation|light.ColorFeatureXY|light.ColorFeatureColorTemperature, true)
		invoke(t, srv, ccdef.CmdMoveToHue, ccdef.MoveToHueRequest{Hue: 200, Direction: ccdef.DirectionUp, TransitionTime: 150})
		time.Sleep(time.Second)
		if read(t, srv, ccdef.AttrRemainingTime) == 0 {
			t.Error("no remaining time part-way")
		}
		time.Sleep(20 * time.Second)
		synctest.Wait()
		if read(t, srv, ccdef.AttrRemainingTime) != 0 || read(t, srv, ccdef.AttrCurrentHue) != 200 {
			t.Error("the transition did not end at 200")
		}
	})
}

// TestXYCommands: MoveToColor, MoveColor and StepColor as matter.js runs
// them on the engine — MoveToColor reads part-way and ends at its target,
// MoveColor moves at its rates to the bound in their direction, StepColor
// steps from the current x / y, both step sizes 0 are INVALID_COMMAND
// before the ExecuteIfOff gate; every value reaches the ColorWriter in the
// XY mode.
func TestXYCommands(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		srv := colorLight(t, light.ColorFeatureXY|light.ColorFeatureColorTemperature, true)
		sink := &colorSink{}
		srv.SetColorWriter(sink)
		invoke(t, srv, ccdef.CmdMoveToColor, ccdef.MoveToColorRequest{ColorX: 40000, ColorY: 20000, TransitionTime: 100})
		if read(t, srv, ccdef.AttrColorMode) != uint64(light.ColorModeXY) || read(t, srv, ccdef.AttrEnhancedColorMode) != uint64(light.ColorModeXY) {
			t.Fatal("MoveToColor did not switch to the XY mode")
		}
		time.Sleep(5 * time.Second)
		if x := read(t, srv, ccdef.AttrCurrentX); x <= 24939 || x >= 40000 {
			t.Errorf("CurrentX %d part-way, want between start and 40000", x)
		}
		time.Sleep(6 * time.Second)
		synctest.Wait()
		if c := sink.last(); c.X != 40000 || c.Y != 20000 || c.Mode != light.ColorModeXY {
			t.Errorf("the ColorWriter's last colour %+v, want x 40000 y 20000 in XY", c)
		}

		invoke(t, srv, ccdef.CmdMoveColor, ccdef.MoveColorRequest{RateX: 10000, RateY: -5000})
		time.Sleep(3 * time.Second)
		synctest.Wait()
		// y moves 5000 a second; the engine's first step counts one
		// interval ahead (cluster/transition package doc).
		if x, y := read(t, srv, ccdef.AttrCurrentX), read(t, srv, ccdef.AttrCurrentY); x != 0xFEFF || y < 4000 || y > 5000 {
			t.Errorf("after MoveColor x %d y %d; x stops at 65279, y moves 5000/s from 20000", x, y)
		}
		invoke(t, srv, ccdef.CmdMoveColor, ccdef.MoveColorRequest{})
		synctest.Wait()
		y := read(t, srv, ccdef.AttrCurrentY)
		time.Sleep(time.Second)
		synctest.Wait()
		if read(t, srv, ccdef.AttrCurrentY) != y {
			t.Error("MoveColor with both rates 0 did not stop y")
		}

		invoke(t, srv, ccdef.CmdStepColor, ccdef.StepColorRequest{StepX: -1000, StepY: 300})
		if x, y2 := read(t, srv, ccdef.AttrCurrentX), read(t, srv, ccdef.AttrCurrentY); x != 0xFEFF-1000 || y2 != y+300 {
			t.Errorf("StepColor at once: x %d y %d", x, y2)
		}
		_, err := srv.MatterInvoke(context.Background(), ccdef.CmdStepColor, ccdef.StepColorRequest{})
		if !statusIs(err, im.StatusInvalidCommand) {
			t.Errorf("StepColor 0/0: %v, want INVALID_COMMAND", err)
		}
	})
}

// TestHueAndSaturationCommands: the hue and saturation commands at once
// (transitions not managed), with the hue's direction and wrap rules.
func TestHueAndSaturationCommands(t *testing.T) {
	t.Parallel()
	srv := colorLight(t, allColorFeatures, false)
	invoke(t, srv, ccdef.CmdMoveToHue, ccdef.MoveToHueRequest{Hue: 10, Direction: ccdef.DirectionLongest, TransitionTime: 50})
	if read(t, srv, ccdef.AttrCurrentHue) != 10 {
		t.Error("MoveToHue unmanaged does not apply at once")
	}
	invoke(t, srv, ccdef.CmdStepHue, ccdef.StepHueRequest{StepMode: ccdef.StepModeDown, StepSize: 20, TransitionTime: 5})
	// addValueWithOverflow wraps 10 less 20 to 244 on the 0..254 wheel.
	if h := read(t, srv, ccdef.AttrCurrentHue); h != 244 {
		t.Errorf("StepHue down 20 from 10 = %d, want 244 (addValueWithOverflow)", h)
	}
	invoke(t, srv, ccdef.CmdMoveToSaturation, ccdef.MoveToSaturationRequest{Saturation: 100})
	invoke(t, srv, ccdef.CmdStepSaturation, ccdef.StepSaturationRequest{StepMode: ccdef.StepModeUp, StepSize: 200})
	if s := read(t, srv, ccdef.AttrCurrentSaturation); s != 254 {
		t.Errorf("StepSaturation beyond the bound = %d, want 254", s)
	}
	invoke(t, srv, ccdef.CmdMoveToHueAndSaturation, ccdef.MoveToHueAndSaturationRequest{Hue: 77, Saturation: 33})
	if read(t, srv, ccdef.AttrCurrentHue) != 77 || read(t, srv, ccdef.AttrCurrentSaturation) != 33 {
		t.Error("MoveToHueAndSaturation")
	}
	// Move and the unmanaged engine: matter.js moves without a target, so
	// nothing changes without managed transitions.
	invoke(t, srv, ccdef.CmdMoveHue, ccdef.MoveHueRequest{MoveMode: ccdef.MoveModeUp, Rate: 10})
	invoke(t, srv, ccdef.CmdMoveSaturation, ccdef.MoveSaturationRequest{MoveMode: ccdef.MoveModeDown, Rate: 10})
	if read(t, srv, ccdef.AttrCurrentHue) != 77 || read(t, srv, ccdef.AttrCurrentSaturation) != 33 {
		t.Error("an unmanaged Move changed a value")
	}
	for name, tc := range map[string]struct {
		cmd    uint32
		fields any
		want   im.StatusCode
	}{
		"MoveHue rate 0":           {ccdef.CmdMoveHue, ccdef.MoveHueRequest{MoveMode: ccdef.MoveModeUp}, im.StatusInvalidCommand},
		"MoveSaturation rate 0":    {ccdef.CmdMoveSaturation, ccdef.MoveSaturationRequest{MoveMode: ccdef.MoveModeDown}, im.StatusInvalidCommand},
		"StepHue size 0":           {ccdef.CmdStepHue, ccdef.StepHueRequest{StepMode: ccdef.StepModeUp}, im.StatusInvalidCommand},
		"StepSaturation size 0":    {ccdef.CmdStepSaturation, ccdef.StepSaturationRequest{StepMode: ccdef.StepModeUp}, im.StatusInvalidCommand},
		"EnhancedStepHue size 0":   {ccdef.CmdEnhancedStepHue, ccdef.EnhancedStepHueRequest{StepMode: ccdef.StepModeUp}, im.StatusInvalidCommand},
		"EnhancedMoveHue rate 0":   {ccdef.CmdEnhancedMoveHue, ccdef.EnhancedMoveHueRequest{MoveMode: ccdef.MoveModeUp}, im.StatusInvalidCommand},
		"MoveToHue hue 255":        {ccdef.CmdMoveToHue, map[uint8]any{0: uint64(255), 1: uint64(0), 2: uint64(0), 3: uint64(0), 4: uint64(0)}, im.StatusConstraintError},
		"MoveToHue direction 4":    {ccdef.CmdMoveToHue, ccdef.MoveToHueRequest{Direction: 4}, im.StatusConstraintError},
		"MoveHue mode 2":           {ccdef.CmdMoveHue, ccdef.MoveHueRequest{MoveMode: 2, Rate: 1}, im.StatusConstraintError},
		"StepSaturation mode 0":    {ccdef.CmdStepSaturation, ccdef.StepSaturationRequest{StepMode: 0, StepSize: 1}, im.StatusConstraintError},
		"MoveToSaturation 255":     {ccdef.CmdMoveToSaturation, ccdef.MoveToSaturationRequest{Saturation: 255}, im.StatusConstraintError},
		"MoveToColor x 65280":      {ccdef.CmdMoveToColor, ccdef.MoveToColorRequest{ColorX: 65280}, im.StatusConstraintError},
		"MoveToColor tt 65535":     {ccdef.CmdMoveToColor, ccdef.MoveToColorRequest{TransitionTime: 65535}, im.StatusConstraintError},
		"ColorLoopSet action 3":    {ccdef.CmdColorLoopSet, ccdef.ColorLoopSetRequest{Action: 3}, im.StatusConstraintError},
		"ColorLoopSet direction 2": {ccdef.CmdColorLoopSet, ccdef.ColorLoopSetRequest{Direction: 2}, im.StatusConstraintError},
		"MoveToHue no fields":      {ccdef.CmdMoveToHue, nil, im.StatusInvalidCommand},
	} {
		_, err := srv.MatterInvoke(context.Background(), tc.cmd, tc.fields)
		if !statusIs(err, tc.want) {
			t.Errorf("%s: %v, want %v", name, err, tc.want)
		}
	}
}

// TestEnhancedHueCommands: the enhanced commands switch EnhancedColorMode
// to the enhanced hue and ColorMode to hue and saturation; a plain hue
// command afterwards leaves EnhancedColorMode enhanced, as matter.js's
// setColorMode finds ColorMode unchanged.
func TestEnhancedHueCommands(t *testing.T) {
	t.Parallel()
	srv := colorLight(t, allColorFeatures, false)
	invoke(t, srv, ccdef.CmdEnhancedMoveToHue, ccdef.EnhancedMoveToHueRequest{EnhancedHue: 40000, Direction: ccdef.DirectionUp})
	if read(t, srv, ccdef.AttrEnhancedCurrentHue) != 40000 || read(t, srv, ccdef.AttrEnhancedColorMode) != 3 || read(t, srv, ccdef.AttrColorMode) != 0 {
		t.Error("EnhancedMoveToHue")
	}
	invoke(t, srv, ccdef.CmdEnhancedStepHue, ccdef.EnhancedStepHueRequest{StepMode: ccdef.StepModeUp, StepSize: 30000})
	// addValueWithOverflow wraps 40000 plus 30000 to 4465 on the 0..65535 wheel.
	if h := read(t, srv, ccdef.AttrEnhancedCurrentHue); h != 4465 {
		t.Errorf("EnhancedStepHue wrap = %d, want 4465", h)
	}
	invoke(t, srv, ccdef.CmdEnhancedMoveToHueAndSaturation, ccdef.EnhancedMoveToHueAndSaturationRequest{EnhancedHue: 1234, Saturation: 200})
	if read(t, srv, ccdef.AttrEnhancedCurrentHue) != 1234 || read(t, srv, ccdef.AttrCurrentSaturation) != 200 {
		t.Error("EnhancedMoveToHueAndSaturation")
	}
	invoke(t, srv, ccdef.CmdMoveToHue, ccdef.MoveToHueRequest{Hue: 5})
	if read(t, srv, ccdef.AttrEnhancedColorMode) != 3 {
		t.Error("MoveToHue changed EnhancedColorMode from the enhanced mode")
	}
}

// TestColorLoopSet: activating keeps the current enhanced hue and starts
// from the start hue or the current one; a changed time restarts a running
// loop; deactivating restores the kept hue.
func TestColorLoopSet(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		srv := colorLight(t, allColorFeatures, true)
		invoke(t, srv, ccdef.CmdEnhancedMoveToHue, ccdef.EnhancedMoveToHueRequest{EnhancedHue: 5000})
		invoke(t, srv, ccdef.CmdColorLoopSet, ccdef.ColorLoopSetRequest{
			UpdateFlags: ccdef.UpdateFlagsUpdateAction | ccdef.UpdateFlagsUpdateDirection | ccdef.UpdateFlagsUpdateTime | ccdef.UpdateFlagsUpdateStartHue,
			Action:      ccdef.ColorLoopActionActivateFromColorLoopStartEnhancedHue,
			Direction:   ccdef.ColorLoopDirectionDecrement, Time: 10, StartHue: 30000,
		})
		if read(t, srv, ccdef.AttrColorLoopActive) != 1 || read(t, srv, ccdef.AttrColorLoopStoredEnhancedHue) != 5000 ||
			read(t, srv, ccdef.AttrColorLoopStartEnhancedHue) != 30000 || read(t, srv, ccdef.AttrColorLoopTime) != 10 ||
			read(t, srv, ccdef.AttrColorLoopDirection) != 0 {
			t.Fatal("ColorLoopSet did not take its fields")
		}
		time.Sleep(time.Second)
		// One turn per 10 s, downwards from 30000: floor(65535/10) a second.
		if h := read(t, srv, ccdef.AttrEnhancedCurrentHue); h > 30000-6000 || h < 30000-7300 {
			t.Errorf("after 1 s the loop is at %d, want about %d", h, 30000-6553)
		}
		invoke(t, srv, ccdef.CmdColorLoopSet, ccdef.ColorLoopSetRequest{UpdateFlags: ccdef.UpdateFlagsUpdateTime, Time: 20})
		if read(t, srv, ccdef.AttrColorLoopTime) != 20 || read(t, srv, ccdef.AttrColorLoopActive) != 1 {
			t.Error("a changed loop time did not keep the loop running")
		}
		invoke(t, srv, ccdef.CmdColorLoopSet, ccdef.ColorLoopSetRequest{UpdateFlags: ccdef.UpdateFlagsUpdateAction, Action: ccdef.ColorLoopActionDeactivate})
		if read(t, srv, ccdef.AttrColorLoopActive) != 0 || read(t, srv, ccdef.AttrEnhancedCurrentHue) != 5000 {
			t.Error("deactivating did not restore the kept enhanced hue")
		}
		time.Sleep(time.Second)
		synctest.Wait()
		if read(t, srv, ccdef.AttrEnhancedCurrentHue) != 5000 {
			t.Error("the hue moved after the loop stopped")
		}
	})
}

// TestColorModeSwitchConverts: a command of another mode converts the
// current colour into the new mode's attributes by matter.js's conversions
// (switchColorMode), and stops what moved in the old mode.
func TestColorModeSwitchConverts(t *testing.T) {
	t.Parallel()
	srv := colorLight(t, allColorFeatures, false)
	sink := &colorSink{}
	srv.SetColorWriter(sink)
	// CT → XY: 250 mireds is 4000 K, the table's x/y.
	invoke(t, srv, ccdef.CmdMoveToColorTemperature, ccdef.MoveToColorTemperatureRequest{ColorTemperatureMireds: 250})
	invoke(t, srv, ccdef.CmdStepColor, ccdef.StepColorRequest{StepX: 1})
	wx, wy, _ := light.MiredsToXY(250)
	if x, y := read(t, srv, ccdef.AttrCurrentX), read(t, srv, ccdef.AttrCurrentY); x != uint64(jsRound(wx*65536))+1 || y != uint64(jsRound(wy*65536)) {
		t.Errorf("CT 250 → XY gives %d/%d, want %v/%v (+1 step)", x, y, jsRound(wx*65536), jsRound(wy*65536))
	}
	// XY → CT: McCamy, cropped to 153..370.
	invoke(t, srv, ccdef.CmdMoveToColor, ccdef.MoveToColorRequest{ColorX: 21000, ColorY: 21500}) // x 0.32 y 0.33
	invoke(t, srv, ccdef.CmdStepColorTemperature, ccdef.StepColorTemperatureRequest{StepMode: ccdef.StepModeUp, StepSize: 1})
	want := min(max(light.XYToMireds(21000.0/65536, 21500.0/65536), 153), 370) + 1
	if m := read(t, srv, ccdef.AttrColorTemperatureMireds); float64(m) != want {
		t.Errorf("XY → CT gives %d, want %v", m, want)
	}
	// CT → HS through x/y.
	invoke(t, srv, ccdef.CmdMoveSaturation, ccdef.MoveSaturationRequest{MoveMode: ccdef.MoveModeStop})
	h, s, ok := light.MiredsToHSV(float64(read(t, srv, ccdef.AttrColorTemperatureMireds)))
	if !ok || read(t, srv, ccdef.AttrCurrentHue) != uint64(jsRound(h*254/360)) || read(t, srv, ccdef.AttrCurrentSaturation) != uint64(jsRound(s*254)) {
		t.Errorf("CT → HS gives hue %d sat %d, want %v %v", read(t, srv, ccdef.AttrCurrentHue), read(t, srv, ccdef.AttrCurrentSaturation), h, s)
	}
	// HS → XY.
	invoke(t, srv, ccdef.CmdMoveToHueAndSaturation, ccdef.MoveToHueAndSaturationRequest{Hue: 85, Saturation: 254})
	invoke(t, srv, ccdef.CmdMoveColor, ccdef.MoveColorRequest{RateX: 1})
	hx, hy := light.HSVToXY(85.0*360/254, 1)
	if read(t, srv, ccdef.AttrCurrentX) != uint64(jsRound(hx*65536)) || read(t, srv, ccdef.AttrCurrentY) != uint64(jsRound(hy*65536)) {
		t.Error("HS → XY")
	}
	if c := sink.last(); c.Mode != light.ColorModeXY {
		t.Errorf("the ColorWriter's last colour is in mode %d, want XY", c.Mode)
	}
	// XY → HS, as matter.js has it: hsvToXy with x and y.
	x, y := read(t, srv, ccdef.AttrCurrentX), read(t, srv, ccdef.AttrCurrentY)
	invoke(t, srv, ccdef.CmdMoveHue, ccdef.MoveHueRequest{MoveMode: ccdef.MoveModeStop})
	ch, cs := light.HSVToXY(float64(x)/65536, float64(y)/65536)
	if read(t, srv, ccdef.AttrCurrentHue) != uint64(jsRound(ch*254/360)) || read(t, srv, ccdef.AttrCurrentSaturation) != uint64(jsRound(cs*254)) {
		t.Error("XY → HS does not follow matter.js's switchColorMode")
	}
}

// TestColorWriterRefusal: a ColorWriter error fails the command and keeps
// every attribute, the colour mode included.
func TestColorWriterRefusal(t *testing.T) {
	t.Parallel()
	srv := colorLight(t, allColorFeatures, false)
	srv.SetColorWriter(&colorSink{err: errors.New("bulb gone")})
	if _, err := srv.MatterInvoke(context.Background(), ccdef.CmdMoveToColor, ccdef.MoveToColorRequest{ColorX: 100, ColorY: 100}); err == nil {
		t.Fatal("a refused colour succeeded")
	}
	if read(t, srv, ccdef.AttrColorMode) != uint64(light.ColorModeHueSaturation) || read(t, srv, ccdef.AttrCurrentX) != 24939 {
		t.Error("a refused colour changed the state")
	}
	srv.SetColorWriter(nil)
	invoke(t, srv, ccdef.CmdMoveToColor, ccdef.MoveToColorRequest{ColorX: 100, ColorY: 100})
	if got := srv.Color(); got.X != 100 || got.Y != 100 || got.Mode != light.ColorModeXY {
		t.Errorf("Color() = %+v", got)
	}
}

// TestNewCommandsExecuteIfOff: every command of the new features runs the
// ExecuteIfOff gate: off without the option it changes nothing; the
// override runs it.
func TestNewCommandsExecuteIfOff(t *testing.T) {
	t.Parallel()
	o := &onOff{}
	srv := light.NewColorControlServer(light.ColorControlServerConfig{Features: allColorFeatures, OnOff: o})
	before := srv.Color()
	for cmd, fields := range map[uint32]any{
		ccdef.CmdMoveToHue:                      ccdef.MoveToHueRequest{Hue: 9},
		ccdef.CmdMoveToSaturation:               ccdef.MoveToSaturationRequest{Saturation: 9},
		ccdef.CmdMoveToHueAndSaturation:         ccdef.MoveToHueAndSaturationRequest{Hue: 9, Saturation: 9},
		ccdef.CmdMoveToColor:                    ccdef.MoveToColorRequest{ColorX: 9, ColorY: 9},
		ccdef.CmdStepColor:                      ccdef.StepColorRequest{StepX: 9},
		ccdef.CmdEnhancedMoveToHue:              ccdef.EnhancedMoveToHueRequest{EnhancedHue: 9},
		ccdef.CmdEnhancedMoveToHueAndSaturation: ccdef.EnhancedMoveToHueAndSaturationRequest{EnhancedHue: 9, Saturation: 9},
		ccdef.CmdColorLoopSet:                   ccdef.ColorLoopSetRequest{UpdateFlags: ccdef.UpdateFlagsUpdateAction, Action: 2},
	} {
		invoke(t, srv, cmd, fields)
		if srv.Color() != before {
			t.Errorf("command 0x%02X ran while off", cmd)
		}
	}
	invoke(t, srv, ccdef.CmdMoveToColor, ccdef.MoveToColorRequest{ColorX: 9, ColorY: 9, OptionsMask: 1, OptionsOverride: 1})
	if c := srv.Color(); c.X != 9 || c.Y != 9 {
		t.Error("the ExecuteIfOff override did not run the command")
	}
}

// TestSyncColorTemperatureOnlyInCTMode: matter.js
// syncColorTemperatureWithLevel couples only in the colour temperature
// mode.
func TestSyncColorTemperatureOnlyInCTMode(t *testing.T) {
	t.Parallel()
	srv := colorLight(t, allColorFeatures, false)
	if err := srv.SyncColorTemperatureWithLevel(context.Background(), 254); err != nil {
		t.Fatal(err)
	}
	if read(t, srv, ccdef.AttrColorTemperatureMireds) != 250 {
		t.Error("the level coupled the colour temperature in the hue and saturation mode")
	}
	invoke(t, srv, ccdef.CmdMoveToColorTemperature, ccdef.MoveToColorTemperatureRequest{ColorTemperatureMireds: 300})
	_ = srv.SyncColorTemperatureWithLevel(context.Background(), 254)
	if read(t, srv, ccdef.AttrColorTemperatureMireds) != 153 {
		t.Error("the level did not couple in the colour temperature mode")
	}
}

// TestSceneRecall: MatterApplySceneValues recalls each mode's values,
// switches to the scene's mode and restarts a colour loop
// (BD-Matter-ColorControl-SceneRecall).
func TestSceneRecall(t *testing.T) {
	t.Parallel()
	srv := colorLight(t, allColorFeatures, false)
	ctx := context.Background()
	srv.MatterApplySceneValues(ctx, map[uint32]uint64{ccdef.AttrEnhancedColorMode: 1, ccdef.AttrCurrentX: 16000, ccdef.AttrCurrentY: 13000}, 0)
	if c := srv.Color(); c.Mode != light.ColorModeXY || c.X != 16000 || c.Y != 13000 {
		t.Errorf("XY scene: %+v", c)
	}
	srv.MatterApplySceneValues(ctx, map[uint32]uint64{ccdef.AttrEnhancedColorMode: 2, ccdef.AttrColorTemperatureMireds: 250}, 0)
	if c := srv.Color(); c.Mode != light.ColorModeColorTemperature || c.ColorTemperatureMireds != 250 {
		t.Errorf("CT scene: %+v", c)
	}
	srv.MatterApplySceneValues(ctx, map[uint32]uint64{ccdef.AttrEnhancedColorMode: 3, ccdef.AttrEnhancedCurrentHue: 12000, ccdef.AttrCurrentSaturation: 70}, 0)
	if c := srv.Color(); c.Mode != light.ColorModeEnhancedHueSaturation || c.EnhancedHue != 12000 || c.Saturation != 70 {
		t.Errorf("enhanced hue scene: %+v", c)
	}
	srv.MatterApplySceneValues(ctx, map[uint32]uint64{ccdef.AttrEnhancedColorMode: 0, ccdef.AttrEnhancedCurrentHue: 0xC800, ccdef.AttrCurrentSaturation: 50}, 0)
	if c := srv.Color(); c.Mode != light.ColorModeHueSaturation || c.Hue != 0xC8 || c.Saturation != 50 {
		t.Errorf("hue and saturation scene: %+v", c)
	}
	srv.MatterApplySceneValues(ctx, map[uint32]uint64{ccdef.AttrColorLoopActive: 1, ccdef.AttrColorLoopDirection: 1, ccdef.AttrColorLoopTime: 5}, 0)
	if read(t, srv, ccdef.AttrColorLoopActive) != 1 || read(t, srv, ccdef.AttrColorLoopTime) != 5 || read(t, srv, ccdef.AttrEnhancedColorMode) != 3 {
		t.Error("loop scene did not start the colour loop")
	}
	ct := light.NewColorControlServer(light.DefaultColorControlServerConfig())
	ct.MatterApplySceneValues(ctx, map[uint32]uint64{ccdef.AttrColorTemperatureMireds: 200}, 0)
	if read(t, ct, ccdef.AttrColorTemperatureMireds) != 200 {
		t.Error("a CT-only light did not recall a scene stored without its mode")
	}
}

func jsRound(v float64) float64 {
	if v < 0 {
		return -jsRound(-v)
	}
	return float64(int64(v + 0.5))
}
