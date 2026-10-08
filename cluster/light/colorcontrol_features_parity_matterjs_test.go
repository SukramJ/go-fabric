// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package light_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/light"
	ccdef "github.com/SukramJ/go-fabric/cluster/spec/colorcontrol"
	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/internal/paritytest"
)

const allColorFeatures = light.ColorFeatureHueSaturation | light.ColorFeatureEnhancedHue |
	light.ColorFeatureColorLoop | light.ColorFeatureXY | light.ColorFeatureColorTemperature

// validSelection is the ColorControl conformance of the features: HS is
// "EHUE, O" and EHUE "CL, O".
func validSelection(f light.ColorFeature) bool {
	if f&light.ColorFeatureEnhancedHue != 0 && f&light.ColorFeatureHueSaturation == 0 {
		return false
	}
	return f&light.ColorFeatureColorLoop == 0 || f&light.ColorFeatureEnhancedHue != 0
}

// TestParityMatterJS_ColorControlEveryFeatureSelection builds the server
// for each of the 32 feature selections and holds every one the
// conformance allows against the element file through the generated
// definition (spectest.CheckServer: FeatureMap, ClusterRevision, the
// attribute and command lists by conformance, the write privileges, the
// read-only writes); a selection the conformance forbids is refused.
func TestParityMatterJS_ColorControlEveryFeatureSelection(t *testing.T) {
	t.Parallel()
	spectest.CheckDefinition(t, ccdef.Definition)
	for f := range light.ColorFeature(1 << 5) {
		srv, err := light.NewColorControl(light.ColorControlServerConfig{Features: f, MinMireds: 153, MaxMireds: 500, InitialMireds: 250})
		want := f
		if f == 0 {
			want = light.ColorFeatureColorTemperature // zero is CT only
		}
		if !validSelection(want) {
			if !errors.Is(err, light.ErrColorFeatures) {
				t.Errorf("features 0b%05b: err %v, want ErrColorFeatures", f, err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("features 0b%05b: %v", f, err)
		}
		spectest.CheckServer(t, srv, ccdef.Definition, uint32(want))
		if v, _ := srv.MatterRead(ccdef.AttrColorCapabilities); v != uint16(want) {
			t.Errorf("features 0b%05b: ColorCapabilities %v, want the FeatureMap (matter.js initialize)", f, v)
		}
		if !slices.Contains(srv.MatterAttributes(), ccdef.AttrRemainingTime) {
			t.Errorf("features 0b%05b: RemainingTime not served", f)
		}
	}
}

// TestParityMatterJS_ExtendedColorLightFeatures pins what matter.js's
// ExtendedColorLight enables (devices/extended-color-light.ts:
// ColorControlServer.with("Xy", "ColorTemperature") with remainingTime
// mandatory) against the snapshot's element requirements, and that a
// server with those features meets them.
func TestParityMatterJS_ExtendedColorLightFeatures(t *testing.T) {
	t.Parallel()
	js := paritytest.ClusterSnapshot(t, ccdef.ClusterID)
	for name, bit := range map[string]light.ColorFeature{
		"HS": light.ColorFeatureHueSaturation, "EHUE": light.ColorFeatureEnhancedHue, "CL": light.ColorFeatureColorLoop,
		"XY": light.ColorFeatureXY, "CT": light.ColorFeatureColorTemperature,
	} {
		found := false
		for _, f := range js.Features {
			if f.Name == name {
				found = true
				if light.ColorFeature(1)<<f.Bit != bit {
					t.Errorf("feature %s bit %d, module 0x%X", name, f.Bit, bit)
				}
			}
		}
		if !found {
			t.Errorf("feature %s not in the snapshot", name)
		}
	}
	srv := light.NewColorControlServer(light.ColorControlServerConfig{Features: light.ColorFeatureXY | light.ColorFeatureColorTemperature})
	for _, id := range []uint32{ccdef.AttrCurrentX, ccdef.AttrCurrentY, ccdef.AttrRemainingTime, ccdef.AttrColorTemperatureMireds} {
		if !slices.Contains(srv.MatterAttributes(), id) {
			t.Errorf("XY+CT server lacks 0x%04X", id)
		}
	}
	for _, id := range []uint32{ccdef.CmdMoveToColor, ccdef.CmdMoveColor, ccdef.CmdStepColor, ccdef.CmdStopMoveStep, ccdef.CmdMoveToColorTemperature} {
		if !slices.Contains(srv.MatterAcceptedCommands(), id) {
			t.Errorf("XY+CT server does not accept 0x%02X", id)
		}
	}
	if v, _ := srv.MatterRead(ccdef.AttrColorMode); v != uint8(light.ColorModeXY) {
		t.Errorf("XY+CT server starts in mode %v, want XY (matter.js default EnhancedColorMode)", v)
	}
	if v, _ := srv.MatterRead(ccdef.AttrCurrentX); v != uint16(24939) {
		t.Errorf("CurrentX starts at %v, want matter.js's 24939", v)
	}
}

// TestColorControlUnsupportedCommandAndInitialMode: a command the feature
// selection does not serve is UNSUPPORTED_COMMAND; an initial mode of a
// missing feature is refused.
func TestColorControlUnsupportedCommandAndInitialMode(t *testing.T) {
	t.Parallel()
	srv := light.NewColorControlServer(light.DefaultColorControlServerConfig())
	for _, cmd := range []uint32{ccdef.CmdMoveToHue, ccdef.CmdMoveToColor, ccdef.CmdEnhancedMoveHue, ccdef.CmdColorLoopSet} {
		_, err := srv.MatterInvoke(context.Background(), cmd, map[uint8]any{})
		if !statusIs(err, im.StatusUnsupportedCommand) {
			t.Errorf("CT-only command 0x%02X: %v, want UNSUPPORTED_COMMAND", cmd, err)
		}
	}
	mode := light.ColorModeXY
	if _, err := light.NewColorControl(light.ColorControlServerConfig{InitialColorMode: &mode}); !errors.Is(err, light.ErrColorFeatures) {
		t.Errorf("XY initial mode on a CT server: %v", err)
	}
	ehue := light.ColorModeEnhancedHueSaturation
	srv, err := light.NewColorControl(light.ColorControlServerConfig{
		Features: light.ColorFeatureHueSaturation | light.ColorFeatureEnhancedHue, InitialColorMode: &ehue,
	})
	if err != nil {
		t.Fatal(err)
	}
	if m, _ := srv.MatterRead(ccdef.AttrColorMode); m != uint8(0) {
		t.Errorf("ColorMode %v in the enhanced mode, want hue and saturation (0)", m)
	}
	if m, _ := srv.MatterRead(ccdef.AttrEnhancedColorMode); m != uint8(3) {
		t.Errorf("EnhancedColorMode %v, want 3", m)
	}
	defer func() {
		if recover() == nil {
			t.Error("NewColorControlServer accepted CL without EHUE")
		}
	}()
	light.NewColorControlServer(light.ColorControlServerConfig{Features: light.ColorFeatureColorLoop})
}

// TestColorControlNegativeWrites is the behavioural negative-write table
// for the attributes the full feature set adds: every colour attribute is
// read-only (UNSUPPORTED_WRITE), Options refuses an undefined bit and the
// writable ones take their privileges from the element file.
func TestColorControlNegativeWrites(t *testing.T) {
	t.Parallel()
	srv := light.NewColorControlServer(light.ColorControlServerConfig{Features: allColorFeatures})
	for _, id := range []uint32{
		ccdef.AttrCurrentHue, ccdef.AttrCurrentSaturation, ccdef.AttrCurrentX, ccdef.AttrCurrentY,
		ccdef.AttrEnhancedCurrentHue, ccdef.AttrEnhancedColorMode, ccdef.AttrColorMode, ccdef.AttrColorLoopActive,
		ccdef.AttrColorLoopDirection, ccdef.AttrColorLoopTime, ccdef.AttrColorLoopStartEnhancedHue,
		ccdef.AttrColorLoopStoredEnhancedHue, ccdef.AttrColorCapabilities, ccdef.AttrRemainingTime,
	} {
		if err := srv.MatterWrite(context.Background(), id, uint64(1)); !statusIs(err, im.StatusUnsupportedWrite) {
			t.Errorf("write 0x%04X: %v, want UNSUPPORTED_WRITE", id, err)
		}
	}
	if err := srv.MatterWrite(context.Background(), ccdef.AttrDriftCompensation, uint64(1)); !statusIs(err, im.StatusUnsupportedAttribute) {
		t.Errorf("write DriftCompensation (not served): %v, want UNSUPPORTED_ATTRIBUTE", err)
	}
	if err := srv.MatterWrite(context.Background(), ccdef.AttrOptions, uint64(2)); !statusIs(err, im.StatusConstraintError) {
		t.Errorf("Options 0x02: %v, want CONSTRAINT_ERROR", err)
	}
	if got := srv.MinWritePrivilege(ccdef.AttrOptions); got != 3 {
		t.Errorf("Options write privilege %d, want Operate", got)
	}
	if got := srv.MinWritePrivilege(ccdef.AttrStartUpColorTemperatureMireds); got != 4 {
		t.Errorf("StartUpColorTemperatureMireds write privilege %d, want Manage", got)
	}
	xyOnly := light.NewColorControlServer(light.ColorControlServerConfig{Features: light.ColorFeatureXY})
	if err := xyOnly.MatterWrite(context.Background(), ccdef.AttrStartUpColorTemperatureMireds, uint64(250)); !statusIs(err, im.StatusUnsupportedAttribute) {
		t.Errorf("StartUpColorTemperatureMireds without CT: %v, want UNSUPPORTED_ATTRIBUTE", err)
	}
}
