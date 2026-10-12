// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package light_test

import (
	"bytes"
	"context"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/SukramJ/go-fabric/cluster/light"
	ccdef "github.com/SukramJ/go-fabric/cluster/spec/colorcontrol"
)

// hookWriter is a ColorWriter and ColorTemperatureWriter that hands every
// value to a hook, on the writer's goroutine, before it records it.
type hookWriter struct {
	mu     sync.Mutex
	colors []light.Color
	mireds []uint16
	onCT   func(m uint16)
	onCol  func(c light.Color)
}

func (w *hookWriter) SetColor(_ context.Context, c light.Color) error {
	if w.onCol != nil {
		w.onCol(c)
	}
	w.mu.Lock()
	w.colors = append(w.colors, c)
	w.mu.Unlock()
	return nil
}

func (w *hookWriter) SetColorTemperatureMireds(_ context.Context, m uint16) error {
	if w.onCT != nil {
		w.onCT(m)
	}
	w.mu.Lock()
	w.mireds = append(w.mireds, m)
	w.mu.Unlock()
	return nil
}

func (w *hookWriter) calls() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.colors) + len(w.mireds)
}

// TestSetStoresAndReportsWithoutTheWriter: the device's colour in the
// current mode is stored, read back and reported; the writers are not
// called; an unchanged colour reports nothing (matter.js
// Datasource.ts:673-675).
func TestSetStoresAndReportsWithoutTheWriter(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		srv := colorLight(t, allColorFeatures, false)
		w := &hookWriter{}
		srv.SetColorWriter(w)
		srv.SetWriter(w)
		r := watch(srv, ccdef.AttrCurrentHue, ccdef.AttrCurrentSaturation, ccdef.AttrColorMode, ccdef.AttrEnhancedColorMode)

		srv.Set(light.Color{Mode: light.ColorModeHueSaturation, Hue: 100, Saturation: 200})
		synctest.Wait()
		if got := srv.Color(); got.Mode != light.ColorModeHueSaturation || got.Hue != 100 || got.Saturation != 200 {
			t.Errorf("Color() = %+v", got)
		}
		if read(t, srv, ccdef.AttrCurrentHue) != 100 || read(t, srv, ccdef.AttrCurrentSaturation) != 200 {
			t.Error("a Read does not return the device's colour")
		}
		if !slices.Equal(r.values(ccdef.AttrCurrentHue), []uint64{100}) || !slices.Equal(r.values(ccdef.AttrCurrentSaturation), []uint64{200}) {
			t.Errorf("reports: hue %v saturation %v, want [100] [200]", r.values(ccdef.AttrCurrentHue), r.values(ccdef.AttrCurrentSaturation))
		}
		if len(r.values(ccdef.AttrColorMode))+len(r.values(ccdef.AttrEnhancedColorMode)) != 0 {
			t.Error("an unchanged colour mode was reported")
		}
		if w.calls() != 0 {
			t.Errorf("the writers were called %d times", w.calls())
		}

		// Out-of-range fields are cropped to the attributes' ranges.
		srv.Set(light.Color{Mode: light.ColorModeHueSaturation, Hue: 255, Saturation: 255})
		if got := srv.Color(); got.Hue != 254 || got.Saturation != 254 {
			t.Errorf("hue/saturation 255 stored as %d/%d, want 254/254", got.Hue, got.Saturation)
		}

		// The same colour again is no change.
		time.Sleep(2 * time.Second) // past the quiet interval
		before := len(r.values(ccdef.AttrCurrentHue))
		srv.Set(light.Color{Mode: light.ColorModeHueSaturation, Hue: 254, Saturation: 254})
		synctest.Wait()
		if len(r.values(ccdef.AttrCurrentHue)) != before {
			t.Error("an unchanged colour was reported")
		}
	})
}

// TestSetSwitchesTheModeAsACommandDoes: a Set in another mode leaves the
// state a command of that mode leaves for the same colour — the mode
// switch's conversion (switchColorMode, matter.js ColorControlServer.ts:1454-1540)
// followed by the new mode's own fields — and reports the colour modes.
// The expectation comes from a second server that runs the command.
func TestSetSwitchesTheModeAsACommandDoes(t *testing.T) {
	cases := []struct {
		name  string
		setup []any // commands run on both servers first: {cmd, fields}
		cmd   uint32
		req   any
		color light.Color
	}{
		{
			"HS to XY", nil, ccdef.CmdMoveToColor,
			ccdef.MoveToColorRequest{ColorX: 30000, ColorY: 20000},
			light.Color{Mode: light.ColorModeXY, X: 30000, Y: 20000},
		},
		{
			"XY to CT, cropped to the physical maximum",
			[]any{ccdef.CmdMoveToColor, ccdef.MoveToColorRequest{ColorX: 21000, ColorY: 21500}},
			ccdef.CmdMoveToColorTemperature,
			ccdef.MoveToColorTemperatureRequest{ColorTemperatureMireds: 999},
			light.Color{Mode: light.ColorModeColorTemperature, ColorTemperatureMireds: 999},
		},
		{
			"CT to HS",
			[]any{ccdef.CmdMoveToColorTemperature, ccdef.MoveToColorTemperatureRequest{ColorTemperatureMireds: 300}},
			ccdef.CmdMoveToHueAndSaturation,
			ccdef.MoveToHueAndSaturationRequest{Hue: 40, Saturation: 120},
			light.Color{Mode: light.ColorModeHueSaturation, Hue: 40, Saturation: 120},
		},
		{
			"HS to enhanced HS", nil, ccdef.CmdEnhancedMoveToHueAndSaturation,
			ccdef.EnhancedMoveToHueAndSaturationRequest{EnhancedHue: 40000, Saturation: 90},
			light.Color{Mode: light.ColorModeEnhancedHueSaturation, EnhancedHue: 40000, Saturation: 90},
		},
		{
			"enhanced HS to CT",
			[]any{ccdef.CmdEnhancedMoveToHueAndSaturation, ccdef.EnhancedMoveToHueAndSaturationRequest{EnhancedHue: 1000, Saturation: 254}},
			ccdef.CmdMoveToColorTemperature,
			ccdef.MoveToColorTemperatureRequest{ColorTemperatureMireds: 10},
			light.Color{Mode: light.ColorModeColorTemperature, ColorTemperatureMireds: 10},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bySet, byCmd := colorLight(t, allColorFeatures, false), colorLight(t, allColorFeatures, false)
			for i := 0; i < len(tc.setup); i += 2 {
				invoke(t, bySet, tc.setup[i].(uint32), tc.setup[i+1])
				invoke(t, byCmd, tc.setup[i].(uint32), tc.setup[i+1])
			}
			r := watch(bySet, ccdef.AttrColorMode, ccdef.AttrEnhancedColorMode)
			oldMode, oldEnhanced := read(t, bySet, ccdef.AttrColorMode), read(t, bySet, ccdef.AttrEnhancedColorMode)
			bySet.Set(tc.color)
			invoke(t, byCmd, tc.cmd, tc.req)
			if got, want := bySet.Color(), byCmd.Color(); got != want {
				t.Errorf("after Set %+v\n  want %+v (the command's)", got, want)
			}
			for _, attr := range []uint32{ccdef.AttrColorMode, ccdef.AttrEnhancedColorMode} {
				if a, b := read(t, bySet, attr), read(t, byCmd, attr); a != b {
					t.Errorf("0x%04X reads %d after Set, %d after the command", attr, a, b)
				}
			}
			newMode, newEnhanced := read(t, bySet, ccdef.AttrColorMode), read(t, bySet, ccdef.AttrEnhancedColorMode)
			if (newMode != oldMode) != (len(r.values(ccdef.AttrColorMode)) == 1) {
				t.Errorf("ColorMode %d → %d reported %v", oldMode, newMode, r.values(ccdef.AttrColorMode))
			}
			if (newEnhanced != oldEnhanced) != (len(r.values(ccdef.AttrEnhancedColorMode)) == 1) {
				t.Errorf("EnhancedColorMode %d → %d reported %v", oldEnhanced, newEnhanced, r.values(ccdef.AttrEnhancedColorMode))
			}
		})
	}
}

// TestSetStopsARunningTransition: a device-side colour ends the
// controller's transition — RemainingTime reports 0, the colour stays the
// device's — as StopMoveStep ends it.
func TestSetStopsARunningTransition(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		srv := colorLight(t, allColorFeatures, true)
		w := &hookWriter{}
		srv.SetWriter(w)
		r := watch(srv, ccdef.AttrRemainingTime, ccdef.AttrColorTemperatureMireds)
		invoke(t, srv, ccdef.CmdMoveToColorTemperature, ccdef.MoveToColorTemperatureRequest{ColorTemperatureMireds: 370, TransitionTime: 100})
		time.Sleep(2 * time.Second)
		calls := w.calls()
		srv.Set(light.Color{Mode: light.ColorModeColorTemperature, ColorTemperatureMireds: 200})
		synctest.Wait()
		if got := r.values(ccdef.AttrRemainingTime); !slices.Equal(got, []uint64{100, 0}) {
			t.Errorf("RemainingTime reports %v, want [100 0]", got)
		}
		if read(t, srv, ccdef.AttrRemainingTime) != 0 {
			t.Error("RemainingTime does not read 0")
		}
		time.Sleep(10 * time.Second)
		synctest.Wait()
		if m := read(t, srv, ccdef.AttrColorTemperatureMireds); m != 200 {
			t.Errorf("ColorTemperatureMireds = %d after the transition's end, want the device's 200", m)
		}
		if got := r.values(ccdef.AttrColorTemperatureMireds); len(got) == 0 || got[len(got)-1] != 200 {
			t.Errorf("the last ColorTemperatureMireds report is not the device's: %v", got)
		}
		if w.calls() != calls {
			t.Error("the writer was called after the Set")
		}
		// The next command runs from the device's colour.
		invoke(t, srv, ccdef.CmdStepColorTemperature, ccdef.StepColorTemperatureRequest{StepMode: ccdef.StepModeUp, StepSize: 5})
		if m := read(t, srv, ccdef.AttrColorTemperatureMireds); m != 205 {
			t.Errorf("a step after the Set gives %d, want 205", m)
		}
	})
}

// TestSetLeavesTheColorLoop: a running colour loop is a command the device
// did not end; only ColorLoopSet ends it (ColorControlServer.ts:1218-1249,
// stopMoveStep), so it keeps looping after a Set while the other movement
// stops.
func TestSetLeavesTheColorLoop(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		srv := colorLight(t, allColorFeatures, true)
		invoke(t, srv, ccdef.CmdColorLoopSet, ccdef.ColorLoopSetRequest{
			UpdateFlags: ccdef.UpdateFlagsUpdateAction, Action: ccdef.ColorLoopActionActivateFromEnhancedCurrentHue,
			Direction: ccdef.ColorLoopDirectionIncrement, Time: 25,
		})
		invoke(t, srv, ccdef.CmdMoveToSaturation, ccdef.MoveToSaturationRequest{Saturation: 254, TransitionTime: 100})
		time.Sleep(time.Second)
		srv.Set(light.Color{Mode: light.ColorModeEnhancedHueSaturation, EnhancedHue: srv.Color().EnhancedHue, Saturation: 10})
		synctest.Wait()
		time.Sleep(2 * time.Second)
		synctest.Wait()
		if read(t, srv, ccdef.AttrColorLoopActive) != 1 {
			t.Fatal("the Set ended the colour loop")
		}
		if read(t, srv, ccdef.AttrCurrentSaturation) != 10 {
			t.Error("the saturation transition went on after the Set")
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

// TestSetIgnoresAnUnservedMode: a colour of a mode the server has no
// feature for is logged and changes nothing.
func TestSetIgnoresAnUnservedMode(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(prev)

	srv := light.NewColorControlServer(light.DefaultColorControlServerConfig()) // CT only
	r := watch(srv, ccdef.AttrColorTemperatureMireds)
	before := srv.Color()
	srv.Set(light.Color{Mode: light.ColorModeXY, X: 100, Y: 100})
	srv.Set(light.Color{Mode: light.ColorMode(7)})
	if srv.Color() != before {
		t.Errorf("Color() = %+v, want the unchanged %+v", srv.Color(), before)
	}
	if len(r.entries) != 0 {
		t.Error("an ignored colour was reported")
	}
	if n := strings.Count(buf.String(), "matter.colorcontrol.inbound_color_mode_ignored"); n != 2 {
		t.Errorf("logged %d times, want 2:\n%s", n, buf.String())
	}
}

// within fails the test unless fn returns within a second.
func within(t *testing.T, what string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("%s blocked", what)
	}
}

// TestSetFromInsideTheWriter: brief AA's rule — the writer may call Set on
// its own goroutine before it returns, from a command and from a
// transition step, without blocking; the device's colour wins over the
// one being written, and a Set of the colour being written is the device
// confirming it.
func TestSetFromInsideTheWriter(t *testing.T) {
	t.Run("from a command", func(t *testing.T) {
		srv := colorLight(t, allColorFeatures, false)
		w := &hookWriter{onCol: func(light.Color) {
			srv.Set(light.Color{Mode: light.ColorModeXY, X: 1234, Y: 4321})
		}}
		srv.SetColorWriter(w)
		within(t, "MoveToColor with a Set in the writer", func() {
			invoke(t, srv, ccdef.CmdMoveToColor, ccdef.MoveToColorRequest{ColorX: 30000, ColorY: 20000})
		})
		if got := srv.Color(); got.Mode != light.ColorModeXY || got.X != 1234 || got.Y != 4321 {
			t.Errorf("Color() = %+v, want the device's 1234/4321", got)
		}
		within(t, "the next command", func() {
			w.onCol = nil
			invoke(t, srv, ccdef.CmdMoveToColor, ccdef.MoveToColorRequest{ColorX: 30000, ColorY: 20000})
		})
		if got := srv.Color(); got.X != 30000 || got.Y != 20000 {
			t.Errorf("the next command left %+v", got)
		}
	})
	t.Run("the device confirms the written colour", func(t *testing.T) {
		srv := colorLight(t, allColorFeatures, false)
		w := &hookWriter{onCol: func(c light.Color) { srv.Set(c) }}
		srv.SetColorWriter(w)
		ref := colorLight(t, allColorFeatures, false)
		ref.SetColorWriter(&hookWriter{})
		r, rr := watch(srv, ccdef.AttrCurrentX, ccdef.AttrColorMode), watch(ref, ccdef.AttrCurrentX, ccdef.AttrColorMode)
		within(t, "MoveToColor with a confirming writer", func() {
			invoke(t, srv, ccdef.CmdMoveToColor, ccdef.MoveToColorRequest{ColorX: 30000, ColorY: 20000})
		})
		invoke(t, ref, ccdef.CmdMoveToColor, ccdef.MoveToColorRequest{ColorX: 30000, ColorY: 20000})
		if got, want := srv.Color(), ref.Color(); got != want || got.X != 30000 || got.Y != 20000 {
			t.Errorf("Color() = %+v, want %+v", got, want)
		}
		for _, attr := range []uint32{ccdef.AttrCurrentX, ccdef.AttrColorMode} {
			if got, want := r.values(attr), rr.values(attr); !slices.Equal(got, want) {
				t.Errorf("0x%04X reports %v, a writer that does not confirm %v", attr, got, want)
			}
		}
	})
	t.Run("from a transition step", func(t *testing.T) {
		srv, err := light.NewColorControl(light.ColorControlServerConfig{
			MinMireds: 153, MaxMireds: 370, InitialMireds: 250, ManageTransitions: true,
			TransitionStepInterval: 10 * time.Millisecond,
		})
		if err != nil {
			t.Fatal(err)
		}
		defer srv.MatterQuiesce()
		returned := make(chan struct{})
		var once sync.Once
		w := &hookWriter{}
		w.onCT = func(uint16) {
			once.Do(func() {
				srv.Set(light.Color{Mode: light.ColorModeColorTemperature, ColorTemperatureMireds: 160})
				close(returned)
			})
		}
		srv.SetWriter(w)
		invoke(t, srv, ccdef.CmdMoveToColorTemperature, ccdef.MoveToColorTemperatureRequest{ColorTemperatureMireds: 370, TransitionTime: 50})
		select {
		case <-returned:
		case <-time.After(time.Second):
			t.Fatal("Set from inside a step's writer blocked")
		}
		deadline := time.Now().Add(time.Second)
		for read(t, srv, ccdef.AttrRemainingTime) != 0 && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
		if read(t, srv, ccdef.AttrRemainingTime) != 0 {
			t.Fatal("the transition did not stop")
		}
		time.Sleep(50 * time.Millisecond)
		if m := read(t, srv, ccdef.AttrColorTemperatureMireds); m != 160 {
			t.Errorf("ColorTemperatureMireds = %d, want the device's 160", m)
		}
	})
}
