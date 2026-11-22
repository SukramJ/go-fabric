// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package light_test

import (
	"fmt"
	"testing"

	"thingfabric.dev/go-fabric/cluster/light"
	ccdef "thingfabric.dev/go-fabric/cluster/spec/colorcontrol"
)

// TestStartUpColorTemperatureMiredsIsApplied holds the construction to
// matter.js packages/node/src/behaviors/color-control/ColorControlServer.ts
// :376-393 at db021da0 (initializeColorTemperature, run with the CT
// feature, :364-366): unless the boot reason is SoftwareUpdateCompleted or
// an Aggregator owns the endpoint, a non-null StartUpColorTemperatureMireds,
// cropped to the physical range, that differs from the cropped current
// value sets ColorMode and EnhancedColorMode to ColorTemperatureMireds and
// ColorTemperatureMireds to it; an equal one changes nothing, the mode
// included.
func TestStartUpColorTemperatureMiredsIsApplied(t *testing.T) {
	t.Parallel()
	xy := light.ColorModeXY
	u16 := func(v uint16) *uint16 { return &v }
	base := light.ColorControlServerConfig{
		Features:  light.ColorFeatureColorTemperature | light.ColorFeatureXY,
		MinMireds: 153, MaxMireds: 370, InitialMireds: 370, InitialColorMode: &xy,
	}
	read := func(srv *light.ColorControlServer) string {
		mode, _ := srv.MatterRead(ccdef.AttrColorMode)
		enhanced, _ := srv.MatterRead(ccdef.AttrEnhancedColorMode)
		mireds, _ := srv.MatterRead(ccdef.AttrColorTemperatureMireds)
		return fmt.Sprintf("mode %v enhanced %v mireds %v", mode, enhanced, mireds)
	}
	const untouched = "mode 1 enhanced 1 mireds 370"
	for _, c := range []struct {
		name string
		edit func(*light.ColorControlServerConfig)
		want string
	}{
		{"applied", func(c *light.ColorControlServerConfig) { c.StartUpColorTemperatureMireds = u16(250) }, "mode 2 enhanced 2 mireds 250"},
		{"cropped to the physical range", func(c *light.ColorControlServerConfig) { c.StartUpColorTemperatureMireds = u16(100) }, "mode 2 enhanced 2 mireds 153"},
		{"null", func(*light.ColorControlServerConfig) {}, untouched},
		{"equal to the current value", func(c *light.ColorControlServerConfig) { c.StartUpColorTemperatureMireds = u16(400) }, untouched},
		{"bridged", func(c *light.ColorControlServerConfig) {
			c.StartUpColorTemperatureMireds, c.Bridged = u16(250), true
		}, untouched},
		{"after a software update", func(c *light.ColorControlServerConfig) {
			c.StartUpColorTemperatureMireds, c.BootReason = u16(250), func() uint8 { return uint8(5) }
		}, untouched},
		{"another boot reason", func(c *light.ColorControlServerConfig) {
			c.StartUpColorTemperatureMireds, c.BootReason = u16(250), func() uint8 { return uint8(1) }
		}, "mode 2 enhanced 2 mireds 250"},
	} {
		cfg := base
		c.edit(&cfg)
		srv, err := light.NewColorControl(cfg)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got := read(srv); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
		if c.name == "applied" {
			if v, _ := srv.MatterRead(ccdef.AttrStartUpColorTemperatureMireds); v != uint16(250) {
				t.Errorf("StartUpColorTemperatureMireds reads %v, want 250", v)
			}
		}
	}
}

// TestInitialMiredsCropsToTheEffectiveRange: the initial
// ColorTemperatureMireds is cropped to the range the server works in, where
// an unset (0) physical maximum is 0xFEFF and an unset minimum 0. Mirrors
// matter.js packages/node/src/behaviors/color-control/ColorControlServer.ts
// :1790-1796 at db021da0 (#cropColorTemperature over
// minimumColorTemperatureMireds and maximumColorTemperatureMireds,
// :239-253, MIN_TEMPERATURE_VALUE 0 and MAX_TEMPERATURE_VALUE 0xfeff,
// :54-55).
func TestInitialMiredsCropsToTheEffectiveRange(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		min, max, initial, want uint16
	}{
		{153, 0, 250, 250},
		{0, 0, 250, 250},
		{153, 500, 250, 250},
		{153, 500, 600, 500},
		{153, 0, 100, 153},
	} {
		srv, err := light.NewColorControl(light.ColorControlServerConfig{
			Features:  light.ColorFeatureColorTemperature,
			MinMireds: c.min, MaxMireds: c.max, InitialMireds: c.initial,
		})
		if err != nil {
			t.Fatalf("min %d max %d: %v", c.min, c.max, err)
		}
		if got, _ := srv.MatterRead(ccdef.AttrColorTemperatureMireds); got != c.want {
			t.Errorf("min %d max %d initial %d: ColorTemperatureMireds %v, want %d", c.min, c.max, c.initial, got, c.want)
		}
	}
}
