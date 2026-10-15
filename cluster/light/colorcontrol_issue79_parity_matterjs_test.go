// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package light_test

import (
	"context"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/light"
	ccdef "github.com/SukramJ/go-fabric/cluster/spec/colorcontrol"
)

// hsDevice is a host whose device takes hue and saturation only (issue
// #79): it converts an x/y colour through XYToHSV, writes HUE in whole
// degrees and SATURATION as a fraction, and the device echoes each write
// before the write's reply — Set runs from inside the writer, in the
// device's colour space.
type hsDevice struct {
	srv   *light.ColorControlServer
	mu    sync.Mutex
	calls []light.Color
}

func (d *hsDevice) SetColor(_ context.Context, c light.Color) error {
	d.mu.Lock()
	d.calls = append(d.calls, c)
	d.mu.Unlock()
	var deg, sat float64
	switch c.Mode {
	case light.ColorModeXY:
		deg, sat = light.XYToHSV(float64(c.X)/65536, float64(c.Y)/65536)
	case light.ColorModeHueSaturation:
		deg, sat = float64(c.Hue)*360/254, float64(c.Saturation)/254
	default:
		return nil
	}
	// The device quantises the hue to whole degrees and echoes.
	hue := float64(int(deg+0.5) % 360)
	d.srv.Set(light.Color{
		Mode: light.ColorModeHueSaturation,
		Hue:  uint8(min(int(hue*254/360+0.5), 254)), Saturation: uint8(min(int(sat*254+0.5), 254)),
	})
	return nil
}

func (d *hsDevice) writes() []light.Color {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]light.Color(nil), d.calls...)
}

// TestIssue79_MoveToColorOnAnHSDevice: a MoveToColor on a light whose
// device takes hue and saturation — the mode switch converts the
// attributes without writing (matter.js switchColorMode changes state
// only, ColorControlServer.ts:1454-1540 at 0ceb202d), the device's echo in
// its own colour space is the confirmation of the x/y write, and the
// target lands.
func TestIssue79_MoveToColorOnAnHSDevice(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		observed *light.Color
	}{
		{"nothing observed yet", nil},
		{"a colour observed first", &light.Color{Mode: light.ColorModeHueSaturation, Hue: 127, Saturation: 127}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			start := light.ColorModeHueSaturation
			srv := light.NewColorControlServer(light.ColorControlServerConfig{
				Features: light.ColorFeatureHueSaturation | light.ColorFeatureXY, InitialColorMode: &start,
			})
			dev := &hsDevice{srv: srv}
			srv.SetColorWriter(dev)
			if tc.observed != nil {
				srv.Set(*tc.observed)
			}
			if _, err := srv.MatterInvoke(context.Background(), ccdef.CmdMoveToColor, map[uint8]any{0: uint64(45914), 1: uint64(19615), 3: uint64(1), 4: uint64(1)}); err != nil {
				t.Fatalf("MoveToColor: %v", err)
			}
			got := srv.Color()
			if got.Mode != light.ColorModeXY || got.X != 45914 || got.Y != 19615 {
				t.Errorf("colour after = %+v, want XY 45914/19615", got)
			}
			writes := dev.writes()
			for _, w := range writes {
				if w.Mode != light.ColorModeXY {
					t.Errorf("the writer was driven with a %d-mode colour %+v (the mode switch must not write)", w.Mode, w)
				}
			}
			if last := writes[len(writes)-1]; last.X != 45914 || last.Y != 19615 {
				t.Errorf("last write %+v, want the target", last)
			}
		})
	}
}

// TestIssue79_ConfirmationWithinQuantisation: an echo of the in-flight
// colour in another mode confirms it within one attribute unit; a
// different colour is the device's and wins.
func TestIssue79_ConfirmationWithinQuantisation(t *testing.T) {
	t.Parallel()
	start := light.ColorModeHueSaturation
	var inflightHue, inflightSat uint8
	srv := light.NewColorControlServer(light.ColorControlServerConfig{
		Features: light.ColorFeatureHueSaturation | light.ColorFeatureXY, InitialColorMode: &start,
	})
	w := &hookWriter{onCol: func(c light.Color) {
		deg, sat := light.XYToHSV(float64(c.X)/65536, float64(c.Y)/65536)
		inflightHue, inflightSat = uint8(deg*254/360+0.5), uint8(sat*254+0.5)
		srv.Set(light.Color{Mode: light.ColorModeHueSaturation, Hue: inflightHue + 1, Saturation: inflightSat})
	}}
	srv.SetColorWriter(w)
	invoke(t, srv, ccdef.CmdMoveToColor, ccdef.MoveToColorRequest{ColorX: 30000, ColorY: 20000})
	if got := srv.Color(); got.Mode != light.ColorModeXY || got.X != 30000 || got.Y != 20000 {
		t.Errorf("an echo one unit off did not confirm: %+v", got)
	}
	w.onCol = func(light.Color) {
		srv.Set(light.Color{Mode: light.ColorModeHueSaturation, Hue: 200, Saturation: 10})
	}
	invoke(t, srv, ccdef.CmdMoveToColor, ccdef.MoveToColorRequest{ColorX: 31000, ColorY: 21000})
	if got := srv.Color(); got.Mode != light.ColorModeHueSaturation || got.Hue != 200 || got.Saturation != 10 {
		t.Errorf("the device's own colour did not win: %+v", got)
	}
}
