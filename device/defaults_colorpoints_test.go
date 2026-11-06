// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/light"
	ccdef "github.com/SukramJ/go-fabric/cluster/spec/colorcontrol"
)

// TestColorControlDefaultServesColorPoints: the colour points a light's
// declaration supplies reach the self-held ColorControl server, which
// serves them and counts the primaries; a primary numbered past a gap is
// refused.
func TestColorControlDefaultServesColorPoints(t *testing.T) {
	t.Parallel()
	x1, y1, x2, y2, in, wx, wy := uint16(45000), uint16(19000), uint16(11000), uint16(48000), uint8(254), uint16(20542), uint16(21562)
	d, err := NewExtendedColorLight(testOptions("colorpoints"), ccdef.Declare{
		Features: ccdef.FeatureXy | ccdef.FeatureColorTemperature,
		Initial: ccdef.Patch{
			Primary1X: &x1, Primary1Y: &y1, Primary1Intensity: &in, Primary2X: &x2, Primary2Y: &y2,
			WhitePointX: &wx, WhitePointY: &wy, ColorPointRx: &x1, ColorPointRy: &y1,
		},
	})
	if err != nil {
		t.Fatalf("NewExtendedColorLight: %v", err)
	}
	srv, ok := d.Server(ccdef.ClusterID).(*light.ColorControlServer)
	if !ok {
		t.Fatalf("ColorControl is %T, want the self-held *light.ColorControlServer", d.Server(ccdef.ClusterID))
	}
	for id, want := range map[uint32]any{
		ccdef.AttrNumberOfPrimaries: uint8(2), ccdef.AttrPrimary1Intensity: in, ccdef.AttrPrimary2Intensity: nil,
		ccdef.AttrPrimary2Y: y2, ccdef.AttrWhitePointY: wy, ccdef.AttrColorPointRIntensity: nil,
	} {
		if v, ok := srv.MatterRead(id); !ok || v != want {
			t.Errorf("attribute 0x%04X = %v (%v), want %v", id, v, ok, want)
		}
	}

	_, err = NewExtendedColorLight(testOptions("colorpoints-gap"), ccdef.Declare{
		Features: ccdef.FeatureXy | ccdef.FeatureColorTemperature,
		Initial:  ccdef.Patch{Primary2X: &x2, Primary2Y: &y2},
	})
	if err == nil || !strings.Contains(err.Error(), "Primary2 without Primary1") {
		t.Errorf("a primary past a gap: %v, want the refusal", err)
	}
}
