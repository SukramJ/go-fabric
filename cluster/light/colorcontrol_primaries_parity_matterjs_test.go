// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package light_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/light"
	"github.com/SukramJ/go-fabric/cluster/spec"
	ccdef "github.com/SukramJ/go-fabric/cluster/spec/colorcontrol"
)

// TestParityMatterJS_ColorControlPrimaries ports matter.js's "accepts a
// primary a light supplies without stating the number of primaries"
// (packages/node/test/behaviors/color-control/ColorControlServerTest.ts:72-87
// at 862ad75f) by meaning: a light that supplies Primary1X serves it with
// the value supplied. matter.js leaves NumberOfPrimaries null there; the
// server here derives it from the supplied primaries, as connectedhomeip
// does (BD-chip-ColorControl-NumberOfPrimaries).
func TestParityMatterJS_ColorControlPrimaries(t *testing.T) {
	t.Parallel()
	cfg := light.DefaultColorControlServerConfig()
	cfg.Features = light.ColorFeatureXY | light.ColorFeatureHueSaturation | light.ColorFeatureEnhancedHue | light.ColorFeatureColorTemperature
	cfg.Primaries = []light.Chromaticity{{X: 100}}
	srv, err := light.NewColorControl(cfg)
	if err != nil {
		t.Fatalf("NewColorControl: %v", err)
	}
	if v, ok := srv.MatterRead(ccdef.AttrPrimary1X); !ok || v != uint16(100) {
		t.Errorf("Primary1X = %v (%v), want 100", v, ok)
	}
	if !slices.Contains(srv.MatterAttributes(), ccdef.AttrPrimary1X) {
		t.Error("Primary1X is not in the AttributeList")
	}
	if v, _ := srv.MatterRead(ccdef.AttrNumberOfPrimaries); v != uint8(1) {
		t.Errorf("NumberOfPrimaries = %v, want 1 (the supplied count)", v)
	}
}

// TestColorControlPrimaries covers the colour points a light supplies:
// each primary serves its X, Y and Intensity (null when not given), the
// white point and colour points their attributes, nothing unsupplied is
// served, and connectedhomeip's TC-CC-2.1 step 24 holds — NumberOfPrimaries
// matches the PrimaryN attributes the server lists. The points are the
// configuration's: a host set of another value is refused, a value out of
// the attribute's range or a seventh primary refuses the configuration.
func TestColorControlPrimaries(t *testing.T) {
	t.Parallel()
	intensity := uint8(200)
	cfg := light.DefaultColorControlServerConfig()
	cfg.Primaries = []light.Chromaticity{{X: 45000, Y: 19000, Intensity: &intensity}, {X: 11000, Y: 48000}}
	cfg.WhitePoint = &light.CIEPoint{X: 20542, Y: 21562}
	cfg.ColorPointR = &light.Chromaticity{X: 45000, Y: 19000}
	srv, err := light.NewColorControl(cfg)
	if err != nil {
		t.Fatalf("NewColorControl: %v", err)
	}
	for id, want := range map[uint32]any{
		ccdef.AttrNumberOfPrimaries: uint8(2),
		ccdef.AttrPrimary1X:         uint16(45000), ccdef.AttrPrimary1Y: uint16(19000), ccdef.AttrPrimary1Intensity: uint8(200),
		ccdef.AttrPrimary2X: uint16(11000), ccdef.AttrPrimary2Y: uint16(48000), ccdef.AttrPrimary2Intensity: nil,
		ccdef.AttrWhitePointX: uint16(20542), ccdef.AttrWhitePointY: uint16(21562),
		ccdef.AttrColorPointRx: uint16(45000), ccdef.AttrColorPointRy: uint16(19000), ccdef.AttrColorPointRIntensity: nil,
	} {
		if v, ok := srv.MatterRead(id); !ok || v != want {
			t.Errorf("attribute 0x%04X = %v (%v), want %v", id, v, ok, want)
		}
		if id != ccdef.AttrNumberOfPrimaries && !slices.Contains(srv.MatterAttributes(), id) {
			t.Errorf("attribute 0x%04X is not in the AttributeList", id)
		}
	}
	listed := 0
	for _, id := range []uint32{
		ccdef.AttrPrimary1X, ccdef.AttrPrimary2X, ccdef.AttrPrimary3X,
		ccdef.AttrPrimary4X, ccdef.AttrPrimary5X, ccdef.AttrPrimary6X,
	} {
		if slices.Contains(srv.MatterAttributes(), id) {
			listed++
		}
	}
	if listed != 2 {
		t.Errorf("%d PrimaryNX attributes listed, NumberOfPrimaries is 2", listed)
	}
	for _, id := range []uint32{ccdef.AttrPrimary3X, ccdef.AttrColorPointGx, ccdef.AttrColorPointBIntensity} {
		if _, ok := srv.MatterRead(id); ok {
			t.Errorf("attribute 0x%04X served without being supplied", id)
		}
	}

	if err := srv.SetAttributes(map[uint32]any{ccdef.AttrPrimary1X: uint16(45000)}); err != nil {
		t.Errorf("setting Primary1X to its value: %v", err)
	}
	if err := srv.SetAttributes(map[uint32]any{ccdef.AttrPrimary1X: uint16(1)}); !errors.Is(err, spec.ErrNotHeld) {
		t.Errorf("setting Primary1X to another value: %v, want ErrNotHeld", err)
	}

	tooMany := light.DefaultColorControlServerConfig()
	tooMany.Primaries = make([]light.Chromaticity, 7)
	if _, err := light.NewColorControl(tooMany); err == nil {
		t.Error("seven primaries accepted")
	}
	outOfRange := light.DefaultColorControlServerConfig()
	outOfRange.WhitePoint = &light.CIEPoint{X: 0xFF00}
	if _, err := light.NewColorControl(outOfRange); err == nil {
		t.Error("a white point beyond 0xFEFF accepted")
	}
}
