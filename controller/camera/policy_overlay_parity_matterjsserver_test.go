// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"testing"

	csdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavstreammanagement"
)

// The cases of matterjs-server's
// packages/ws-controller/test/OverlayPolicyTest.ts:18-111 at 0bd06f2,
// ported by meaning: a FeatureMap stands for the fixture's feature flags
// (cameraFixtures.ts), and overlayFlag for an optional boolean.

var (
	overlayBoth    = supportOf(csdef.FeatureVideo | csdef.FeatureWatermark | csdef.FeatureOnScreenDisplay)
	overlayNeither = supportOf(csdef.FeatureVideo)
	overlayUnknown = supportOf(0)
)

// overlaysOf fails the test when the camera lacks a demanded feature.
func overlaysOf(t *testing.T, s overlaySelection) overlayBounds {
	t.Helper()
	if s.unsupported != 0 {
		t.Fatalf("unsupported: %v", s.unsupported)
	}
	return s.overlays
}

// TestOverlaySupportParityMatterjsServer is describe("overlaySupport")
// (OverlayPolicyTest.ts:23-35, matterjs-server at 0bd06f2).
func TestOverlaySupportParityMatterjsServer(t *testing.T) {
	t.Run("states each feature against a camera that has stated its map", func(t *testing.T) {
		if got := supportOf(csdef.FeatureVideo | csdef.FeatureWatermark); got != (overlaySupport{watermark: flagOn, osd: flagOff}) {
			t.Errorf("support = %+v", got)
		}
	})
	t.Run("states neither feature for a map that has not arrived, so nothing is gated on it", func(t *testing.T) {
		if overlayUnknown != (overlaySupport{}) {
			t.Errorf("support = %+v", overlayUnknown)
		}
	})
}

// TestResolveOverlaysParityMatterjsServer is describe("resolveOverlays")
// (OverlayPolicyTest.ts:37-80, matterjs-server at 0bd06f2).
func TestResolveOverlaysParityMatterjsServer(t *testing.T) {
	cases := []struct {
		name    string
		stated  overlayBounds
		support overlaySupport
		want    overlayBounds
	}{
		{"sends both fields as false when the camera advertises both and the caller stated neither", overlayBounds{}, overlayBoth, overlayBounds{watermark: flagOff, osd: flagOff}},
		{"sends what the caller stated when the camera advertises the feature", overlayBounds{watermark: flagOn, osd: flagOff}, overlayBoth, overlayBounds{watermark: flagOn, osd: flagOff}},
		{"sends neither field to a camera that advertises neither feature", overlayBounds{}, overlayNeither, overlayBounds{}},
		// Met, not refused: the camera has no overlay to apply, and sending
		// the field is INVALID_COMMAND.
		{"sends neither field for a caller that declined both on a camera that has neither", overlayBounds{watermark: flagOff, osd: flagOff}, overlayNeither, overlayBounds{}},
		{"refuses nothing while the feature map has not arrived", overlayBounds{watermark: flagOn}, overlayUnknown, overlayBounds{watermark: flagOn}},
		{"resolves each feature against its own support", overlayBounds{}, supportOf(csdef.FeatureVideo | csdef.FeatureWatermark), overlayBounds{watermark: flagOff}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := overlaysOf(t, resolveOverlays(c.stated, c.support)); got != c.want {
				t.Errorf("overlays = %+v, want %+v", got, c.want)
			}
		})
	}
	t.Run("names the feature a caller demanded that the camera does not advertise", func(t *testing.T) {
		if got := resolveOverlays(overlayBounds{watermark: flagOn}, overlayNeither); got.unsupported != csdef.FeatureWatermark {
			t.Errorf("unsupported = %v, want watermark", got.unsupported)
		}
		if got := resolveOverlays(overlayBounds{osd: flagOn}, overlayNeither); got.unsupported != csdef.FeatureOnScreenDisplay {
			t.Errorf("unsupported = %v, want onScreenDisplay", got.unsupported)
		}
	})
	t.Run("sends only what the caller stated while the feature map has not arrived", func(t *testing.T) {
		if got := overlaysOf(t, resolveOverlays(overlayBounds{osd: flagOn}, overlayUnknown)); got != (overlayBounds{osd: flagOn}) {
			t.Errorf("overlays = %+v", got)
		}
		if got := overlaysOf(t, resolveOverlays(overlayBounds{}, overlayUnknown)); got != (overlayBounds{}) {
			t.Errorf("overlays = %+v", got)
		}
	})
}

// TestOverlaysMatchParityMatterjsServer is describe("overlaysMatch")
// (OverlayPolicyTest.ts:82-110, matterjs-server at 0bd06f2).
func TestOverlaysMatchParityMatterjsServer(t *testing.T) {
	carried := overlayBounds{watermark: flagOn, osd: flagOff}
	cases := []struct {
		name    string
		carried overlayBounds
		wanted  overlayBounds
		want    bool
	}{
		// The one place absence reads as a value; elsewhere it means the
		// field stays off an allocate.
		{"reads a flag the camera stated nothing about as off", overlayBounds{}, overlayBounds{watermark: flagOff, osd: flagOff}, true},
		{"reads a flag the camera stated nothing about as off", overlayBounds{}, overlayBounds{watermark: flagOn}, false},
		{"reads a flag the camera stated nothing about as off", overlayBounds{watermark: flagOn}, overlayBounds{osd: flagOn}, false},
		{"accepts a stream carrying exactly what was wanted", carried, overlayBounds{watermark: flagOn, osd: flagOff}, true},
		{"accepts anything for a preference nobody stated", carried, overlayBounds{}, true},
		{"refuses a stream that has an overlay the request wanted off", carried, overlayBounds{watermark: flagOff}, false},
		{"refuses a stream that lacks an overlay the request wanted on", carried, overlayBounds{osd: flagOn}, false},
		{"compares each overlay against its own statement", carried, overlayBounds{watermark: flagOn, osd: flagOn}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := overlaysMatch(c.carried, c.wanted); got != c.want {
				t.Errorf("overlaysMatch(%+v, %+v) = %v, want %v", c.carried, c.wanted, got, c.want)
			}
		})
	}
}
