// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"slices"
	"testing"

	csdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavstreammanagement"
)

// The cases of matterjs-server's
// packages/ws-controller/test/SnapshotPolicyTest.ts:45-701 at 0bd06f2,
// ported by meaning: an absent optional is the zero value or nil. The
// overlays of a stream that states none are noOverlays.

var noOverlays = overlayBounds{watermark: flagOff, osd: flagOff}

// snapshotCapabilities is a tested camera, verified from live attributes:
// entry 0 is encoder-free, entry 1 needs the encoder
// (SnapshotPolicyTest.ts:27-43, matterjs-server at 0bd06f2).
var snapshotCapabilities = []snapshotCapability{
	{resolution: resolution{640, 480}, maxFrameRate: 1, imageCodec: 0},
	{resolution: resolution{1920, 1080}, maxFrameRate: 1, imageCodec: 0, requiresEncodedPixels: true, requiresHardwareEncoder: true},
}

// chosen is the ordered candidates, failing the test when the selection was
// unsatisfiable instead.
func chosen(t *testing.T, s snapshotSelection) []snapshotCapability {
	t.Helper()
	if s.unsatisfiable != "" {
		t.Fatalf("unsatisfiable on %s", s.unsatisfiable)
	}
	return s.capabilities
}

func resolutionsOf(c []snapshotCapability) []resolution {
	out := make([]resolution, len(c))
	for i, e := range c {
		out[i] = e.resolution
	}
	return out
}

func codecOf(c csdef.ImageCodecEnum) *csdef.ImageCodecEnum { return &c }

// TestUsesHardwareEncoderParityMatterjsServer is describe("usesHardwareEncoder")
// (SnapshotPolicyTest.ts:46-61, matterjs-server at 0bd06f2).
func TestUsesHardwareEncoderParityMatterjsServer(t *testing.T) {
	unencoded := snapshotCapabilities[0]
	unencoded.requiresHardwareEncoder = true
	softwareEncoded := snapshotCapabilities[1]
	softwareEncoded.requiresHardwareEncoder = false
	for _, c := range []struct {
		name string
		c    snapshotCapability
		want bool
	}{
		{"reports an encoder user only when both flags are set", snapshotCapabilities[1], true},
		// §11.2.6.9.5: "This field is only considered if
		// RequiresEncodedPixels is true."
		{"ignores requiresHardwareEncoder when requiresEncodedPixels is false", unencoded, false},
		{"reports no encoder use for an encoded capability the device serves without one", softwareEncoded, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := usesHardwareEncoder(c.c); got != c.want {
				t.Errorf("usesHardwareEncoder = %v, want %v", got, c.want)
			}
		})
	}
}

// TestEncodersExhaustedParityMatterjsServer is describe("encodersExhausted")
// (SnapshotPolicyTest.ts:63-152, matterjs-server at 0bd06f2).
func TestEncodersExhaustedParityMatterjsServer(t *testing.T) {
	live := allocatedVideoStream{
		videoStreamID: 1, overlays: noOverlays, streamUsage: 3, videoCodec: 1,
		minResolution: resolution{1920, 1080}, maxResolution: resolution{1920, 1080},
		minFrameRate: 1, maxFrameRate: 30, minBitRate: 800000, maxBitRate: 4000000,
		keyFrameInterval: 4000, referenceCount: 1,
	}
	idle := live
	idle.referenceCount = 0
	// A snapshot stream the camera states uses one of its hardware encoders
	// (§11.2.6.13.9).
	encoding := allocatedSnapshotStream{
		snapshotStreamID: 8, overlays: noOverlays, imageCodec: 0,
		minResolution: resolution{1920, 1080}, maxResolution: resolution{1920, 1080},
		quality: 90, frameRate: 1, encodedPixels: true, hardwareEncoder: true,
	}
	software := encoding
	software.hardwareEncoder = false
	cases := []struct {
		name     string
		encoders ceiling
		video    []allocatedVideoStream
		snapshot []allocatedSnapshotStream
		want     bool
	}{
		{"leaves encoders free on a camera that states more than are taken", stated(4), []allocatedVideoStream{live}, nil, false},
		{"reports exhaustion once as many streams are live as the camera can encode", stated(1), []allocatedVideoStream{live}, nil, true},
		{"ignores an allocated video stream nothing references", stated(1), []allocatedVideoStream{idle}, nil, false},
		{"counts a snapshot stream the camera marks as using a hardware encoder", stated(1), nil, []allocatedSnapshotStream{encoding}, true},
		// HardwareEncoder states that the stream uses an encoder, not that
		// anyone is watching.
		{"counts such a snapshot stream although nothing references it", stated(2), []allocatedVideoStream{live}, []allocatedSnapshotStream{encoding}, true},
		{"ignores a snapshot stream the camera marks as using no hardware encoder", stated(1), nil, []allocatedSnapshotStream{software}, false},
		{"treats any live stream as the last encoder when the camera states no budget", ceiling{}, []allocatedVideoStream{live}, nil, true},
		{"treats any live stream as the last encoder when the camera states no budget", ceiling{}, nil, nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := encodersExhausted(c.encoders, c.video, c.snapshot); got != c.want {
				t.Errorf("encodersExhausted = %v, want %v", got, c.want)
			}
		})
	}
}

// TestSelectSnapshotCapabilitiesParityMatterjsServer is
// describe("selectSnapshotCapabilities") (SnapshotPolicyTest.ts:154-330,
// matterjs-server at 0bd06f2).
func TestSelectSnapshotCapabilitiesParityMatterjsServer(t *testing.T) {
	r := func(w, h int) resolution { return resolution{w, h} }
	withRes := func(c snapshotCapability, res resolution) snapshotCapability {
		c.resolution = res
		return c
	}
	softwareEncoded := snapshotCapabilities[1]
	softwareEncoded.requiresHardwareEncoder = false
	heic320 := withRes(snapshotCapabilities[0], r(320, 240))
	heic320.imageCodec = 1
	cases := []struct {
		name string
		caps []snapshotCapability
		opts snapshotOptions
		want []resolution
		// first: only the first entry is compared.
		first bool
	}{
		{"orders the highest resolution first when no stream holds the encoder", snapshotCapabilities, snapshotOptions{}, []resolution{r(1920, 1080)}, true},
		{"offers every eligible capability, so a rejected one has a fallback", snapshotCapabilities, snapshotOptions{}, []resolution{r(1920, 1080), r(640, 480)}, false},
		{"picks an encoder-free capability while a video stream is live", snapshotCapabilities, snapshotOptions{encodersExhausted: true}, []resolution{r(640, 480)}, true},
		// The encoder preference orders; it does not narrow.
		{"keeps the encoder-using capabilities as fallbacks behind the encoder-free ones", snapshotCapabilities, snapshotOptions{encodersExhausted: true}, []resolution{r(640, 480), r(1920, 1080)}, false},
		{"orders several encoder-free capabilities largest first before the encoder-using ones", append([]snapshotCapability{withRes(snapshotCapabilities[0], r(320, 240))}, snapshotCapabilities...), snapshotOptions{encodersExhausted: true}, []resolution{r(640, 480), r(320, 240), r(1920, 1080)}, false},
		// requiresEncodedPixels alone does not take an encoder.
		{"keeps an encoded capability the device serves without a hardware encoder while streaming", []snapshotCapability{softwareEncoded, snapshotCapabilities[0]}, snapshotOptions{encodersExhausted: true}, []resolution{r(1920, 1080)}, true},
		{"clamps a caller ceiling down to a capability the camera offers", snapshotCapabilities, snapshotOptions{maxResolution: r(1280, 720)}, []resolution{r(640, 480)}, true},
		// Comparable pixel count to 1920x1080, so only a per-dimension
		// check (as the device does) excludes it.
		{"excludes a capability that exceeds the ceiling on one dimension only", []snapshotCapability{withRes(snapshotCapabilities[0], r(1440, 1440)), snapshotCapabilities[0]}, snapshotOptions{maxResolution: r(1920, 1080)}, []resolution{r(640, 480)}, false},
		{"excludes a capability wider than the ceiling although it is no taller", []snapshotCapability{withRes(snapshotCapabilities[0], r(2400, 600)), snapshotCapabilities[0]}, snapshotOptions{maxResolution: r(1920, 1080)}, []resolution{r(640, 480)}, false},
		{"keeps the encoder-free capability under a ceiling that would also admit an encoder one", snapshotCapabilities, snapshotOptions{encodersExhausted: true, maxResolution: r(1920, 1080)}, []resolution{r(640, 480)}, true},
		// The encoder preference is the controller's own and yields to the
		// caller's bounds.
		{"keeps a capability inside the caller's ceiling even when it takes the busy encoder", []snapshotCapability{withRes(snapshotCapabilities[0], r(2560, 1440)), withRes(snapshotCapabilities[1], r(1280, 720))}, snapshotOptions{encodersExhausted: true, maxResolution: r(1280, 720)}, []resolution{r(1280, 720)}, false},
		{"filters to a requested codec", append(slices.Clone(snapshotCapabilities), heic320), snapshotOptions{codec: codecOf(1)}, []resolution{r(320, 240)}, true},
		{"falls back to an encoder capability when the camera offers no encoder-free one", []snapshotCapability{snapshotCapabilities[1]}, snapshotOptions{encodersExhausted: true}, []resolution{r(1920, 1080)}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := resolutionsOf(chosen(t, selectSnapshotCapabilities(c.caps, c.opts)))
			if c.first && len(got) > 0 {
				got = got[:1]
			}
			if !slices.Equal(got, c.want) {
				t.Errorf("order = %v, want %v", got, c.want)
			}
		})
	}
	t.Run("reports a bounds failure when a caller ceiling excludes every capability", func(t *testing.T) {
		if got := selectSnapshotCapabilities(snapshotCapabilities, snapshotOptions{maxResolution: r(100, 100)}); got.unsatisfiable != ReasonBounds || got.capabilities != nil {
			t.Errorf("selection = %+v, want bounds", got)
		}
	})
	t.Run("reports a codec failure when no capability uses the requested codec", func(t *testing.T) {
		if got := selectSnapshotCapabilities(snapshotCapabilities, snapshotOptions{codec: codecOf(7)}); got.unsatisfiable != ReasonCodec {
			t.Errorf("selection = %+v, want codec", got)
		}
	})
	t.Run("reports nothing when the camera advertises no capabilities", func(t *testing.T) {
		got := selectSnapshotCapabilities(nil, snapshotOptions{})
		if got.unsatisfiable != "" || len(got.capabilities) != 0 || got.bestWithinCallerBounds != nil {
			t.Errorf("selection = %+v, want empty", got)
		}
	})
}

// TestSelectSnapshotCapabilitiesTargetParityMatterjsServer is
// describe("selectSnapshotCapabilities with a target")
// (SnapshotPolicyTest.ts:332-411, matterjs-server at 0bd06f2).
func TestSelectSnapshotCapabilitiesTargetParityMatterjsServer(t *testing.T) {
	capability := func(w, h int, encoder bool) snapshotCapability {
		c := snapshotCapabilities[0]
		c.resolution = resolution{w, h}
		c.requiresEncodedPixels, c.requiresHardwareEncoder = encoder, encoder
		return c
	}
	ladder := []snapshotCapability{capability(640, 360, false), capability(1280, 720, false), capability(1920, 1080, false)}
	widths := func(t *testing.T, s snapshotSelection) []int {
		t.Helper()
		capabilities := chosen(t, s)
		out := make([]int, 0, len(capabilities))
		for _, c := range capabilities {
			out = append(out, c.resolution.width)
		}
		return out
	}
	cases := []struct {
		name string
		caps []snapshotCapability
		opts snapshotOptions
		want []int
	}{
		{"puts the smallest capability that reaches the target first, then the larger, then the short ones", ladder, snapshotOptions{target: &resolutionTarget{width: 1000}}, []int{1280, 1920, 640}},
		{"keeps the largest-first order when no capability reaches the target", ladder, snapshotOptions{target: &resolutionTarget{width: 4000}}, []int{1920, 1280, 640}},
		{"reads a height-only target against the height", ladder, snapshotOptions{target: &resolutionTarget{height: 700}}, []int{1280, 1920, 640}},
		{"never picks a capability above the ceiling, whatever the target", ladder, snapshotOptions{maxResolution: resolution{1280, 720}, target: &resolutionTarget{width: 1500}}, []int{1280, 640}},
		{"still puts encoder-free capabilities first when encoders are exhausted", []snapshotCapability{capability(640, 360, false), capability(1280, 720, true)}, snapshotOptions{encodersExhausted: true, target: &resolutionTarget{width: 1000}}, []int{640, 1280}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := widths(t, selectSnapshotCapabilities(c.caps, c.opts)); !slices.Equal(got, c.want) {
				t.Errorf("widths = %v, want %v", got, c.want)
			}
		})
	}
	t.Run("requires every stated dimension of a two-sided target", func(t *testing.T) {
		withWide := append([]snapshotCapability{capability(2000, 500, false)}, ladder...)
		if got := widths(t, selectSnapshotCapabilities(withWide, snapshotOptions{target: &resolutionTarget{width: 640, height: 360}}))[0]; got != 640 {
			t.Errorf("first = %d, want 640", got)
		}
		if got := widths(t, selectSnapshotCapabilities(withWide, snapshotOptions{target: &resolutionTarget{width: 1000, height: 600}}))[0]; got != 1280 {
			t.Errorf("first = %d, want 1280", got)
		}
	})
	t.Run("takes bestWithinCallerBounds from the target order, before the encoder split", func(t *testing.T) {
		s := selectSnapshotCapabilities([]snapshotCapability{capability(640, 360, false), capability(1280, 720, true)}, snapshotOptions{encodersExhausted: true, target: &resolutionTarget{width: 1000}})
		if s.bestWithinCallerBounds == nil || s.bestWithinCallerBounds.resolution.width != 1280 {
			t.Errorf("best = %+v, want 1280 wide", s.bestWithinCallerBounds)
		}
	})
}

// TestRequestedResolutionParityMatterjsServer is describe("requestedResolution")
// (SnapshotPolicyTest.ts:413-452, matterjs-server at 0bd06f2).
func TestRequestedResolutionParityMatterjsServer(t *testing.T) {
	full := resolution{1920, 1080}
	cases := []struct {
		name      string
		available resolution
		target    *resolutionTarget
		want      resolution
	}{
		{"returns the source size without a target", full, nil, full},
		{"derives the height from the aspect ratio for a width-only target", full, &resolutionTarget{width: 640}, resolution{640, 360}},
		{"derives the width from the aspect ratio for a height-only target", full, &resolutionTarget{height: 360}, resolution{640, 360}},
		{"fits a two-sided target inside the source aspect ratio instead of stretching", full, &resolutionTarget{width: 640, height: 640}, resolution{640, 360}},
		{"scales a height-only target by the height ratio alone when it is above one half", full, &resolutionTarget{height: 810}, resolution{1440, 810}},
		{"never asks for more than the source has", full, &resolutionTarget{width: 4000}, full},
		{"never asks for more than the source has", full, &resolutionTarget{width: 4000, height: 4000}, full},
		{"never asks for more than the source has", full, &resolutionTarget{width: 4000, height: 100}, resolution{178, 100}},
		{"never asks for a zero side", resolution{1920, 10}, &resolutionTarget{width: 1}, resolution{1, 1}},
		{"never asks for a zero side", resolution{10, 1920}, &resolutionTarget{height: 1}, resolution{1, 1}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := requestedResolution(c.available, c.target); got != c.want {
				t.Errorf("requestedResolution(%v, %+v) = %v, want %v", c.available, c.target, got, c.want)
			}
		})
	}
}

// TestIsDegradedFromTargetParityMatterjsServer is describe("isDegradedFrom with
// a target") (SnapshotPolicyTest.ts:454-494, matterjs-server at 0bd06f2).
func TestIsDegradedFromTargetParityMatterjsServer(t *testing.T) {
	best := &snapshotCapabilities[1]
	fourThirds := &snapshotCapabilities[0]
	tw := func(w int) *resolutionTarget { return &resolutionTarget{width: w} }
	th := func(h int) *resolutionTarget { return &resolutionTarget{height: h} }
	cases := []struct {
		name   string
		frame  resolution
		best   *snapshotCapability
		target *resolutionTarget
		want   bool
	}{
		{"does not flag a frame that meets the target although the capability is larger", resolution{320, 180}, best, tw(320), false},
		{"flags a frame below the requested size", resolution{160, 90}, best, tw(320), true},
		{"does not flag a correct frame from a stream whose aspect ratio differs from the capability", resolution{320, 180}, fourThirds, tw(320), false},
		{"does not flag a correct frame from a stream whose aspect ratio differs from the capability", resolution{427, 240}, fourThirds, th(240), false},
		{"accepts a frame that reaches either side of a two-sided target", resolution{320, 180}, best, &resolutionTarget{320, 320}, false},
		{"accepts a frame that reaches either side of a two-sided target", resolution{160, 90}, best, &resolutionTarget{320, 320}, true},
		{"measures a height-only target against the height", resolution{1000, 100}, best, th(200), true},
		{"measures a height-only target against the height", resolution{100, 200}, best, th(200), false},
		{"never flags a frame when the camera states no capability", resolution{160, 90}, nil, nil, false},
		{"never flags a frame when the camera states no capability", resolution{160, 90}, nil, tw(320), false},
		{"measures against the whole capability when the target is larger than it", resolution{1280, 720}, best, tw(4000), true},
		{"measures against the whole capability when the target is larger than it", resolution{1920, 1080}, best, tw(4000), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isDegradedFrom(c.frame, c.best, c.target); got != c.want {
				t.Errorf("isDegradedFrom(%v) = %v, want %v", c.frame, got, c.want)
			}
		})
	}
}

// TestChooseSnapshotStreamToFreeParityMatterjsServer is
// describe("chooseSnapshotStreamToFree") (SnapshotPolicyTest.ts:496-571,
// matterjs-server at 0bd06f2).
func TestChooseSnapshotStreamToFreeParityMatterjsServer(t *testing.T) {
	budgeted := stated(248832000)
	held := func(id uint16, change func(*allocatedSnapshotStream)) allocatedSnapshotStream {
		s := allocatedSnapshotStream{
			snapshotStreamID: id, imageCodec: 0, minResolution: resolution{640, 480}, maxResolution: resolution{640, 480},
			quality: 90, frameRate: 1, encodedPixels: true, hardwareEncoder: true, overlays: noOverlays,
		}
		if change != nil {
			change(&s)
		}
		return s
	}
	idOf := func(s *allocatedSnapshotStream) int {
		if s == nil {
			return -1
		}
		return int(s.snapshotStreamID)
	}
	pixelRateOnly := held(1, func(s *allocatedSnapshotStream) { s.hardwareEncoder = false })
	cases := []struct {
		name    string
		streams []allocatedSnapshotStream
		budget  ceiling
		want    int
	}{
		{"takes nothing while every candidate is referenced", []allocatedSnapshotStream{held(1, func(s *allocatedSnapshotStream) { s.referenceCount = 1 })}, budgeted, -1},
		{"takes nothing that holds neither an encoder nor a share of the pixel rate", []allocatedSnapshotStream{held(1, func(s *allocatedSnapshotStream) { s.encodedPixels, s.hardwareEncoder = false, false })}, budgeted, -1},
		// Without MaxEncodedPixelRate the pixel rate is not accounted, so
		// freeing gains nothing.
		{"takes nothing for the pixel rate on a camera that states no budget", []allocatedSnapshotStream{pixelRateOnly}, ceiling{}, -1},
		{"takes nothing for the pixel rate on a camera that states no budget", []allocatedSnapshotStream{pixelRateOnly}, budgeted, 1},
		{"still takes an encoder holder on a camera that states no pixel-rate budget", []allocatedSnapshotStream{held(1, func(s *allocatedSnapshotStream) { s.encodedPixels = false })}, ceiling{}, 1},
		// MaxConcurrentEncoders is 1 on the hardware this rung exists for,
		// so the encoder is the scarce resource.
		{"takes the encoder holder before a larger stream that holds no encoder", []allocatedSnapshotStream{held(1, func(s *allocatedSnapshotStream) {
			s.hardwareEncoder, s.maxResolution, s.frameRate = false, resolution{1920, 1080}, 30
		}), held(2, nil)}, budgeted, 2},
		{"takes the largest pixel-rate footprint among equals, whatever order the camera reported", []allocatedSnapshotStream{held(1, nil), held(2, func(s *allocatedSnapshotStream) { s.maxResolution = resolution{1920, 1080} }), held(3, func(s *allocatedSnapshotStream) { s.frameRate = 30 })}, budgeted, 3},
		{"takes the largest pixel-rate footprint among equals, whatever order the camera reported", []allocatedSnapshotStream{held(2, func(s *allocatedSnapshotStream) { s.maxResolution = resolution{1920, 1080} }), held(1, nil)}, budgeted, 2},
		{"breaks a tie on the id, so the choice does not depend on the report order", []allocatedSnapshotStream{held(9, nil), held(4, nil)}, budgeted, 4},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := idOf(chooseSnapshotStreamToFree(c.streams, c.budget)); got != c.want {
				t.Errorf("chose %d, want %d", got, c.want)
			}
		})
	}
}

// TestFindAdoptableSnapshotStreamParityMatterjsServer is
// describe("findAdoptableSnapshotStream") (SnapshotPolicyTest.ts:573-700,
// matterjs-server at 0bd06f2).
func TestFindAdoptableSnapshotStreamParityMatterjsServer(t *testing.T) {
	best := snapshotCapabilities[1]
	stream := func(id uint16, w, h int, codec csdef.ImageCodecEnum, carried overlayBounds) allocatedSnapshotStream {
		o := noOverlays
		if carried.watermark == flagOn {
			o.watermark = flagOn
		}
		return allocatedSnapshotStream{
			snapshotStreamID: id, imageCodec: codec, minResolution: resolution{w, h}, maxResolution: resolution{w, h},
			quality: 90, frameRate: 1, overlays: o,
		}
	}
	plain := func(id uint16, w, h int) allocatedSnapshotStream { return stream(id, w, h, 0, overlayBounds{}) }
	ranged := func(s allocatedSnapshotStream, floor resolution) allocatedSnapshotStream {
		s.minResolution = floor
		return s
	}
	watermarked := stream(8, 1920, 1080, 0, overlayBounds{watermark: flagOn})
	idOf := func(s *allocatedSnapshotStream) int {
		if s == nil {
			return -1
		}
		return int(s.snapshotStreamID)
	}
	tw := &resolutionTarget{width: 1000}
	cases := []struct {
		name    string
		streams []allocatedSnapshotStream
		best    snapshotCapability
		bounds  snapshotBounds
		want    int
	}{
		{"adopts a stream whose whole range covers the capability that would be allocated", []allocatedSnapshotStream{plain(8, 1920, 1080)}, best, snapshotBounds{}, 8},
		{"refuses a stream whose overlays are not the ones the request resolved to", []allocatedSnapshotStream{watermarked}, best, snapshotBounds{overlays: noOverlays}, -1},
		{"adopts a stream carrying exactly those overlays", []allocatedSnapshotStream{watermarked}, best, snapshotBounds{overlays: overlayBounds{watermark: flagOn, osd: flagOff}}, 8},
		{"refuses a stream smaller than that capability, since adoption may not cost picture size", []allocatedSnapshotStream{plain(8, 640, 480)}, best, snapshotBounds{}, -1},
		// §11.2.8.13.3 lets the camera answer with any size in the stream's
		// range.
		{"refuses a stream whose floor is below the capability, however high its ceiling is", []allocatedSnapshotStream{ranged(plain(8, 1920, 1080), resolution{640, 480})}, best, snapshotBounds{}, -1},
		// 3000x700 is 2.10 Mpx against 1920x1080's 2.07, and 380 rows short.
		{"refuses a stream that outnumbers the capability in pixels but is shorter", []allocatedSnapshotStream{plain(8, 3000, 700)}, best, snapshotBounds{}, -1},
		{"refuses a stream that outnumbers the capability in pixels but is narrower", []allocatedSnapshotStream{plain(8, 1000, 2100)}, best, snapshotBounds{}, -1},
		{"refuses a stream in a codec the caller did not ask for", []allocatedSnapshotStream{stream(8, 1920, 1080, 1, overlayBounds{})}, best, snapshotBounds{codec: codecOf(0)}, -1},
		{"refuses a stream above the ceiling the caller stated", []allocatedSnapshotStream{plain(8, 1920, 1080)}, best, snapshotBounds{maxResolution: resolution{1280, 720}}, -1},
		{"refuses a stream wider than the caller's ceiling although it is no taller", []allocatedSnapshotStream{ranged(plain(8, 3000, 1080), resolution{1920, 1080})}, best, snapshotBounds{maxResolution: resolution{2000, 1080}}, -1},
		{"takes the largest of several candidates", []allocatedSnapshotStream{plain(8, 1920, 1080), plain(9, 2560, 1440)}, best, snapshotBounds{}, 9},
		{"adopts a stream that covers the requested size although it is below the capability", []allocatedSnapshotStream{plain(8, 1280, 720)}, best, snapshotBounds{}, -1},
		{"adopts a stream that covers the requested size although it is below the capability", []allocatedSnapshotStream{plain(8, 1280, 720)}, best, snapshotBounds{target: tw}, 8},
		{"refuses a stream whose floor is below the requested size", []allocatedSnapshotStream{plain(8, 640, 360)}, best, snapshotBounds{target: tw}, -1},
		{"refuses a candidate short of the capability's fitted size in a stated dimension", []allocatedSnapshotStream{plain(8, 640, 360)}, snapshotCapabilities[0], snapshotBounds{target: &resolutionTarget{640, 480}}, -1},
		{"refuses a ranged candidate whose floor is below its own fitted request", []allocatedSnapshotStream{ranged(plain(8, 1920, 1080), resolution{640, 360})}, best, snapshotBounds{target: tw}, -1},
		{"takes the smallest candidate that reaches the target", []allocatedSnapshotStream{plain(8, 1920, 1080), plain(9, 1280, 720), plain(10, 640, 360)}, best, snapshotBounds{target: tw}, 9},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := idOf(findAdoptableSnapshotStream(c.streams, c.best, c.bounds)); got != c.want {
				t.Errorf("adopted %d, want %d", got, c.want)
			}
		})
	}
	// Capture asks the 16:9 stream for 640x360; that meets a width-only
	// target the 4:3 capability would give as 640x480.
	t.Run("fits the target to the candidate's own aspect ratio, not to the capability's", func(t *testing.T) {
		wide := plain(8, 640, 360)
		target := &resolutionTarget{width: 640}
		if got := idOf(findAdoptableSnapshotStream([]allocatedSnapshotStream{wide}, snapshotCapabilities[0], snapshotBounds{target: target})); got != 8 {
			t.Errorf("adopted %d, want 8", got)
		}
		if isDegradedFrom(wide.minResolution, &snapshotCapabilities[0], target) {
			t.Error("the adopted stream's frame is degraded")
		}
	})
}
