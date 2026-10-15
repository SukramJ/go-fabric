// SPDX-License-Identifier: Apache-2.0
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
// Datasource.ts:759-761).
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

// TestSetDuringATransition: a device-side colour during a transition is
// stored at once — a Read returns it — but the transition goes on, its
// next step overwrites the value and RemainingTime does not move, as
// matter.js's next step overwrites an external write (Transitions.ts:455-534,
// :537-574). The value is reported by the "Q" quieter rules, which a
// running transition throttles to once a second.
func TestSetDuringATransition(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		srv := colorLight(t, allColorFeatures, true)
		w := &hookWriter{}
		srv.SetWriter(w)
		r := watch(srv, ccdef.AttrRemainingTime)
		invoke(t, srv, ccdef.CmdMoveToColorTemperature, ccdef.MoveToColorTemperatureRequest{ColorTemperatureMireds: 370, TransitionTime: 100})
		time.Sleep(2*time.Second + 50*time.Millisecond)
		remaining := read(t, srv, ccdef.AttrRemainingTime)
		before := read(t, srv, ccdef.AttrColorTemperatureMireds)
		calls := w.calls()
		srv.Set(light.Color{Mode: light.ColorModeColorTemperature, ColorTemperatureMireds: 160})
		if m := read(t, srv, ccdef.AttrColorTemperatureMireds); m != 160 {
			t.Fatalf("ColorTemperatureMireds reads %d right after the Set, want the device's 160", m)
		}
		if got := read(t, srv, ccdef.AttrRemainingTime); got != remaining {
			t.Errorf("RemainingTime reads %d after the Set, %d before", got, remaining)
		}
		if w.calls() != calls {
			t.Error("the Set called the writer")
		}
		time.Sleep(100 * time.Millisecond)
		synctest.Wait()
		if m := read(t, srv, ccdef.AttrColorTemperatureMireds); m <= before || m >= 370 {
			t.Errorf("the next step left %d, want the transition's own value past %d", m, before)
		}
		time.Sleep(10 * time.Second)
		synctest.Wait()
		if m := read(t, srv, ccdef.AttrColorTemperatureMireds); m != 370 {
			t.Errorf("the transition ended at %d, want its target 370", m)
		}
		if got := r.values(ccdef.AttrRemainingTime); !slices.Equal(got, []uint64{100, 0}) {
			t.Errorf("RemainingTime reports %v, want [100 0]", got)
		}
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
// one being written (the command answers SUCCESS), and a Set of the
// colour being written is the device confirming it.
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
		invoke(t, srv, ccdef.CmdMoveToColorTemperature, ccdef.MoveToColorTemperatureRequest{ColorTemperatureMireds: 370, TransitionTime: 10})
		select {
		case <-returned:
		case <-time.After(time.Second):
			t.Fatal("Set from inside a step's writer blocked")
		}
		// The step whose writer the device answered stored the device's
		// colour; the transition goes on to its target.
		deadline := time.Now().Add(2 * time.Second)
		for read(t, srv, ccdef.AttrColorTemperatureMireds) != 370 && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
		if m := read(t, srv, ccdef.AttrColorTemperatureMireds); m != 370 {
			t.Errorf("ColorTemperatureMireds = %d, want the transition's target 370", m)
		}
	})
}
