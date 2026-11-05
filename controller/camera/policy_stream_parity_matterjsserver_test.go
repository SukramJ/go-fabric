// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"reflect"
	"testing"

	csdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavstreammanagement"
)

// The cases of matterjs-server's
// packages/ws-controller/test/StreamPolicyTest.ts:145-1607 at 0bd06f2,
// ported by meaning with its fixtures (:38-143): an absent hint is the zero
// value, an absent offer ceiling an unset ceiling.

const (
	spH264      = csdef.VideoCodecH264
	spH265      = csdef.VideoCodecHevc
	spLiveView  = csdef.StreamUsageLiveView
	spRecording = csdef.StreamUsageRecording
	spAnalysis  = csdef.StreamUsageAnalysis
)

// spCapabilities is the fixture's CAPABILITIES.
func spCapabilities() videoCapabilities {
	return videoCapabilities{
		sensor:       resolution{2560, 1440},
		maxFrameRate: 30,
		minViewport:  resolution{640, 360},
		rateDistortionPoints: []rateDistortionPoint{
			{codec: spH265, resolution: resolution{1920, 1080}, minBitRate: 800000},
			{codec: spH265, resolution: resolution{1280, 720}, minBitRate: 400000},
			{codec: spH264, resolution: resolution{1920, 1080}, minBitRate: 2000000},
		},
		maxNetworkBandwidth: stated(8000000),
	}
}

// spReferenceCamera is what a camera running the reference camera app
// reports.
var spReferenceCamera = videoCapabilities{
	sensor:               resolution{1920, 1080},
	maxFrameRate:         120,
	minViewport:          resolution{640, 480},
	rateDistortionPoints: []rateDistortionPoint{{codec: spH264, resolution: resolution{640, 480}, minBitRate: 10000}},
	maxNetworkBandwidth:  stated(128000000),
}

var (
	// offerH265Cap8160H264Cap3600 is a browser-shaped offer: both codecs in
	// one m-line, each with its own frame-size cap.
	offerH265Cap8160H264Cap3600 = sdpOffer("m=video 9 UDP/TLS/RTP/SAVPF 100 102", "c=IN IP4 0.0.0.0", "a=recvonly",
		"a=rtpmap:100 H265/90000", "a=fmtp:100 max-lps=2088960", "a=rtpmap:102 H264/90000", "a=fmtp:102 max-fs=3600")
	// offerH264Level31Only states its ceiling through the H.264 level
	// alone: level 3.1 is MaxFS 3600 macroblocks.
	offerH264Level31Only = sdpOffer("m=video 9 UDP/TLS/RTP/SAVPF 102", "c=IN IP4 0.0.0.0", "a=recvonly",
		"a=rtpmap:102 H264/90000", "a=fmtp:102 level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42e01f")
	firefoxLimits = limitsOf(offerH264Level31Only, spH264)
)

func codecOnly(codec csdef.VideoCodecEnum) selectedVideoCodecLimits {
	return selectedVideoCodecLimits{codec: codec}
}

func rangeHints(b videoRangeBounds) *videoHints { return &videoHints{videoRangeBounds: b} }

// selectVideo is computeVideoEnvelope with no overlay asked for.
func selectVideo(caps videoCapabilities, limits selectedVideoCodecLimits, hints *videoHints) videoSelection {
	return computeVideoEnvelope(videoEnvelopeArgs{capabilities: caps, limits: limits, hints: hints})
}

// planOfSelection is the plan a selection carries, failing the test when
// the caller's bounds were unsatisfiable.
func planOfSelection(t *testing.T, s videoSelection) *videoPlan {
	t.Helper()
	if s.plan == nil {
		t.Fatalf("unsatisfiable: %+v", *s.unsatisfiable)
	}
	return s.plan
}

func envelopeOf(t *testing.T, caps videoCapabilities, limits selectedVideoCodecLimits, hints *videoHints) videoEnvelope {
	t.Helper()
	return planOfSelection(t, selectVideo(caps, limits, hints)).envelope
}

// TestComputeVideoEnvelopeParityMatterjsServer is describe("computeVideoEnvelope")
// (StreamPolicyTest.ts:146-581, matterjs-server at 0bd06f2).
//
//nolint:funlen // one subtest per ported case
func TestComputeVideoEnvelopeParityMatterjsServer(t *testing.T) {
	caps := spCapabilities
	area := func(r resolution) int { return r.pixels() }
	t.Run("narrows by the selected codec's own level cap, not by another codec's", func(t *testing.T) {
		e := envelopeOf(t, caps(), limitsOf(offerH265Cap8160H264Cap3600, spH265), nil)
		if a := area(e.maxResolution); a > 8160*256 || a <= 3600*256 {
			t.Errorf("area %d outside (3600*256, 8160*256]", a)
		}
	})
	t.Run("applies the tighter cap when that is the codec selected", func(t *testing.T) {
		if a := area(envelopeOf(t, caps(), limitsOf(offerH265Cap8160H264Cap3600, spH264), nil).maxResolution); a > 3600*256 {
			t.Errorf("area %d above 3600*256", a)
		}
	})
	t.Run("narrows by a level the offer states with no max-fs beside it", func(t *testing.T) {
		e := envelopeOf(t, caps(), limitsOf(offerH264Level31Only, spH264), nil)
		if e.maxResolution != (resolution{1280, 720}) {
			t.Errorf("maxResolution = %v, want 1280x720", e.maxResolution)
		}
	})
	// Level 1 with max-fs raised to 8160 (RFC 6184 §8.1): the frame size
	// allows 1920x1080, but the level's MaxMBPS is under one frame per
	// second there.
	t.Run("spends the peer's pixel-rate budget on frame size rather than allocating past it", func(t *testing.T) {
		limits := limitsOf(offerWithFmtp("H264", "profile-level-id=42e00a;max-fs=8160"), spH264)
		if limits.maxPixels != stated(8160*256) || limits.maxPixelsPerSecond != stated(1485*256) {
			t.Fatalf("limits = %+v", limits)
		}
		e := envelopeOf(t, caps(), limits, nil)
		if e.maxFrameRate < 1 || area(e.maxResolution)*e.maxFrameRate > 1485*256 {
			t.Errorf("window %v at %d fps spends past the pixel rate", e.maxResolution, e.maxFrameRate)
		}
	})
	t.Run("defaults to the widest resolution the camera reports, at its top frame rate alone", func(t *testing.T) {
		p := planOfSelection(t, selectVideo(caps(), codecOnly(spH265), nil))
		e := p.envelope
		if e.maxResolution != (resolution{2560, 1440}) || e.minResolution != (resolution{640, 360}) || e.minFrameRate != 30 || e.maxFrameRate != 30 || p.frameRateFloor != 1 {
			t.Errorf("plan = %+v", p)
		}
	})
	t.Run("keeps a caller's minimum frame rate as the floor, not as the window's minimum", func(t *testing.T) {
		p := planOfSelection(t, selectVideo(caps(), codecOnly(spH265), rangeHints(videoRangeBounds{minFrameRate: 10})))
		if p.envelope.minFrameRate != 30 || p.envelope.maxFrameRate != 30 || p.frameRateFloor != 10 {
			t.Errorf("plan = %+v", p)
		}
	})
	// 1440x1440 has the same pixel count as 1920x1080, so an area test would
	// take its minBitRate.
	t.Run("ignores a trade-off point that fits the ceiling by pixel count but not by dimension", func(t *testing.T) {
		square := caps()
		square.sensor = resolution{1920, 1080}
		square.rateDistortionPoints = []rateDistortionPoint{
			{codec: spH265, resolution: resolution{1440, 1440}, minBitRate: 5000000},
			{codec: spH265, resolution: resolution{1280, 720}, minBitRate: 400000},
		}
		if got := envelopeOf(t, square, codecOnly(spH265), nil).minBitRate; got != 400000 {
			t.Errorf("minBitRate = %d, want 400000", got)
		}
	})
	t.Run("takes minBitRate from the trade-off point for the chosen codec", func(t *testing.T) {
		if got := envelopeOf(t, caps(), codecOnly(spH265), nil).minBitRate; got != 800000 {
			t.Errorf("minBitRate = %d, want 800000", got)
		}
	})
	// Over-reserved bandwidth is taken from every other viewer on the camera
	// (§11.2.1.2.2).
	t.Run("ignores another codec's trade-off point when it derives the bit-rate floor", func(t *testing.T) {
		mixed := caps()
		mixed.rateDistortionPoints = []rateDistortionPoint{
			{codec: spH264, resolution: resolution{2560, 1440}, minBitRate: 6000000},
			{codec: spH265, resolution: resolution{1920, 1080}, minBitRate: 800000},
		}
		if got := envelopeOf(t, mixed, codecOnly(spH265), nil).minBitRate; got != 800000 {
			t.Errorf("minBitRate = %d, want 800000", got)
		}
	})
	withLimits := func(f func(*selectedVideoCodecLimits)) selectedVideoCodecLimits {
		l := codecOnly(spH265)
		f(&l)
		return l
	}
	withCaps := func(f func(*videoCapabilities)) videoCapabilities {
		c := caps()
		f(&c)
		return c
	}
	maxBitRates := []struct {
		name   string
		caps   videoCapabilities
		limits selectedVideoCodecLimits
		hints  *videoHints
		want   int
	}{
		{"clamps a caller maxBitRate above the camera's network bandwidth to the camera's value", caps(), codecOnly(spH265), rangeHints(videoRangeBounds{maxBitRate: 50000000}), 8000000},
		{"clamps a caller maxBitRate above the SDP's maxBitRate to the SDP's value", caps(), withLimits(func(l *selectedVideoCodecLimits) { l.maxBitRate = stated(3000000) }), rangeHints(videoRangeBounds{maxBitRate: 50000000}), 3000000},
		{"falls back to the default maxBitRate when no ceiling is stated anywhere", withCaps(func(c *videoCapabilities) { c.maxNetworkBandwidth = ceiling{} }), codecOnly(spH265), nil, 8000000},
		// MaxNetworkBandwidth is a link capacity, not what the encoder serves.
		{"starts at the default when the camera's network bandwidth is higher", withCaps(func(c *videoCapabilities) { c.maxNetworkBandwidth = stated(128000000) }), codecOnly(spH265), nil, 8000000},
		{"starts at the default when the offer's bit-rate ceiling is higher", spReferenceCamera, firefoxLimits, nil, 8000000},
		{"starts at a caller's ceiling above the default, within the camera's network bandwidth", withCaps(func(c *videoCapabilities) { c.maxNetworkBandwidth = stated(20000000) }), codecOnly(spH265), rangeHints(videoRangeBounds{maxBitRate: 30000000}), 20000000},
	}
	for _, c := range maxBitRates {
		t.Run(c.name, func(t *testing.T) {
			if got := envelopeOf(t, c.caps, c.limits, c.hints).maxBitRate; got != c.want {
				t.Errorf("maxBitRate = %d, want %d", got, c.want)
			}
		})
	}
	unsatisfiable := []struct {
		name  string
		caps  videoCapabilities
		hints *videoHints
		want  IncompatibleBound
	}{
		{"fails a caller minBitRate above every stated ceiling instead of lowering it", caps(), rangeHints(videoRangeBounds{minBitRate: 50000000}), IncompatibleBound{BoundMinBitRate, "50000000", "8000000"}},
		{"fails a caller minResolution the sensor cannot reach instead of lowering it", withCaps(func(c *videoCapabilities) { c.sensor = resolution{1280, 720} }), rangeHints(videoRangeBounds{minResolution: resolution{1920, 1080}}), IncompatibleBound{BoundMinResolution, "1920x1080", "1280x720"}},
		{"fails a caller minFrameRate above the sensor's maximum instead of lowering it", caps(), rangeHints(videoRangeBounds{minFrameRate: 60}), IncompatibleBound{BoundMinFrameRate, "60", "30"}},
		{"fails a floor that exceeds the ceiling on one dimension while fitting it on pixel count", caps(), rangeHints(videoRangeBounds{minResolution: resolution{1440, 1440}, maxResolution: resolution{1920, 1080}}), IncompatibleBound{BoundMinResolution, "1440x1440", "1920x1080"}},
	}
	for _, c := range unsatisfiable {
		t.Run(c.name, func(t *testing.T) {
			s := selectVideo(c.caps, codecOnly(spH265), c.hints)
			if s.plan != nil || s.unsatisfiable == nil || *s.unsatisfiable != c.want {
				t.Errorf("selection = %+v, want unsatisfiable %+v", s, c.want)
			}
		})
	}
	t.Run("fails a caller floor the caller's own ceiling excludes", func(t *testing.T) {
		if s := selectVideo(caps(), codecOnly(spH265), rangeHints(videoRangeBounds{minFrameRate: 25, maxFrameRate: 15})); s.unsatisfiable == nil {
			t.Error("the selection holds")
		}
	})
	// A 4:3 sensor scaled to a 1920x1080 pixel budget is narrower than 1920
	// and would fail a 1080p floor.
	t.Run("keeps the caller's ceiling out of the offer's pixel budget", func(t *testing.T) {
		e := envelopeOf(t, withCaps(func(c *videoCapabilities) { c.sensor = resolution{2592, 1944} }),
			withLimits(func(l *selectedVideoCodecLimits) { l.maxPixels = stated(1920 * 1080) }),
			rangeHints(videoRangeBounds{minResolution: resolution{1920, 1080}, maxResolution: resolution{1920, 1080}}))
		if e.maxResolution != (resolution{1920, 1080}) || e.minResolution != (resolution{1920, 1080}) {
			t.Errorf("window %v..%v, want 1920x1080 both", e.minResolution, e.maxResolution)
		}
	})
	// Pinning min == max would take capacity from every other viewer
	// (§11.2.1.2.2).
	t.Run("drops a trade-off floor the camera's own bandwidth cannot carry, instead of pinning min to max", func(t *testing.T) {
		e := envelopeOf(t, withCaps(func(c *videoCapabilities) { c.maxNetworkBandwidth = stated(500000) }), codecOnly(spH265), nil)
		if e.maxBitRate != 500000 || e.minBitRate != 1 {
			t.Errorf("bit rate %d..%d, want 1..500000", e.minBitRate, e.maxBitRate)
		}
	})
	t.Run("keeps a caller floor the camera can reach", func(t *testing.T) {
		e := envelopeOf(t, caps(), codecOnly(spH265), rangeHints(videoRangeBounds{minFrameRate: 15, minBitRate: 1000000, minResolution: resolution{1280, 720}}))
		if e.minFrameRate != 30 || e.minBitRate != 1000000 || e.minResolution != (resolution{1280, 720}) {
			t.Errorf("envelope = %+v", e)
		}
	})
	// The viewport minimum is the camera's statement, not the caller's, so
	// it may give.
	t.Run("still clamps a server-derived floor down to the ceiling", func(t *testing.T) {
		if e := envelopeOf(t, caps(), codecOnly(spH265), rangeHints(videoRangeBounds{maxResolution: resolution{320, 180}})); e.minResolution != (resolution{320, 180}) {
			t.Errorf("minResolution = %v", e.minResolution)
		}
	})
	t.Run("starts at a caller's floor above the default", func(t *testing.T) {
		e := envelopeOf(t, withCaps(func(c *videoCapabilities) { c.maxNetworkBandwidth = stated(128000000) }), codecOnly(spH265), rangeHints(videoRangeBounds{minBitRate: 10000000}))
		if e.minBitRate != 10000000 || e.maxBitRate != 10000000 {
			t.Errorf("bit rate %d..%d, want 10000000 both", e.minBitRate, e.maxBitRate)
		}
	})
	t.Run("uses the smallest advertised point as the floor when no viewport minimum is reported", func(t *testing.T) {
		if e := envelopeOf(t, withCaps(func(c *videoCapabilities) { c.minViewport = resolution{} }), codecOnly(spH265), nil); e.minResolution != (resolution{1280, 720}) {
			t.Errorf("minResolution = %v", e.minResolution)
		}
	})
	t.Run("falls back to the maximum as the floor when the camera reports nothing smaller", func(t *testing.T) {
		e := envelopeOf(t, videoCapabilities{sensor: resolution{1920, 1080}, maxFrameRate: 30}, codecOnly(spH264), nil)
		if e.minResolution != (resolution{1920, 1080}) || e.maxResolution != (resolution{1920, 1080}) {
			t.Errorf("window %v..%v", e.minResolution, e.maxResolution)
		}
	})
	// 2560x1440 exceeds the cap; scaled down on the same aspect ratio.
	t.Run("narrows the ceiling to the SDP's pixel cap", func(t *testing.T) {
		if e := envelopeOf(t, caps(), withLimits(func(l *selectedVideoCodecLimits) { l.maxPixels = stated(1920 * 1080) }), nil); area(e.maxResolution) > 1920*1080 {
			t.Errorf("maxResolution = %v", e.maxResolution)
		}
	})
	t.Run("rounds a scaled resolution down to even dimensions", func(t *testing.T) {
		if e := envelopeOf(t, caps(), withLimits(func(l *selectedVideoCodecLimits) { l.maxPixels = stated(1000000) }), nil); e.maxResolution != (resolution{1332, 750}) {
			t.Errorf("maxResolution = %v, want 1332x750", e.maxResolution)
		}
	})
	t.Run("narrows the frame rate to the SDP's pixel-rate cap", func(t *testing.T) {
		if e := envelopeOf(t, caps(), withLimits(func(l *selectedVideoCodecLimits) { l.maxPixelsPerSecond = stated(2560 * 1440 * 10) }), nil); e.maxFrameRate != 10 {
			t.Errorf("maxFrameRate = %d, want 10", e.maxFrameRate)
		}
	})
	t.Run("clamps the floor to the ceiling when the SDP narrows below the viewport minimum", func(t *testing.T) {
		e := envelopeOf(t, caps(), withLimits(func(l *selectedVideoCodecLimits) { l.maxPixels = stated(320 * 180) }), nil)
		if e.maxResolution != (resolution{320, 180}) || e.minResolution != (resolution{320, 180}) {
			t.Errorf("window %v..%v, want 320x180 both", e.minResolution, e.maxResolution)
		}
	})
	t.Run("narrows to explicit caller hints", func(t *testing.T) {
		e := envelopeOf(t, caps(), codecOnly(spH265), rangeHints(videoRangeBounds{minResolution: resolution{1280, 720}, maxResolution: resolution{1920, 1080}, maxFrameRate: 15}))
		if e.minResolution != (resolution{1280, 720}) || e.maxResolution != (resolution{1920, 1080}) || e.maxFrameRate != 15 {
			t.Errorf("envelope = %+v", e)
		}
	})
	// 2560x1440 against a 3840x1080 ceiling: an area comparison would keep a
	// height the caller ruled out.
	t.Run("clamps a caller ceiling that binds on one dimension only", func(t *testing.T) {
		if e := envelopeOf(t, caps(), codecOnly(spH265), rangeHints(videoRangeBounds{maxResolution: resolution{3840, 1080}})); e.maxResolution != (resolution{2560, 1080}) {
			t.Errorf("maxResolution = %v, want 2560x1080", e.maxResolution)
		}
	})
	t.Run("clamps the frame rate to the offer's own max-fr", func(t *testing.T) {
		if e := envelopeOf(t, caps(), withLimits(func(l *selectedVideoCodecLimits) { l.maxFrameRate = stated(10) }), nil); e.maxFrameRate != 10 {
			t.Errorf("maxFrameRate = %d, want 10", e.maxFrameRate)
		}
	})
	t.Run("never widens past the camera's own bounds when a hint asks for more", func(t *testing.T) {
		e := envelopeOf(t, caps(), codecOnly(spH265), rangeHints(videoRangeBounds{maxResolution: resolution{7680, 4320}, maxFrameRate: 120}))
		if e.maxResolution != (resolution{2560, 1440}) || e.maxFrameRate != 30 {
			t.Errorf("envelope = %+v", e)
		}
	})
	t.Run("asks first for one frame rate, the best the offer decodes, with a 4000 ms key frame interval", func(t *testing.T) {
		e := envelopeOf(t, spReferenceCamera, firefoxLimits, nil)
		want := videoEnvelope{codec: spH264, minResolution: resolution{640, 480}, maxResolution: resolution{1280, 720}, minFrameRate: 30, maxFrameRate: 30, minBitRate: 10000, maxBitRate: 8000000, keyFrameInterval: 4000}
		if e != want {
			t.Errorf("envelope = %+v, want %+v", e, want)
		}
	})
	t.Run("asks for one frame rate inside a caller's range, the highest it allows", func(t *testing.T) {
		p := planOfSelection(t, selectVideo(spReferenceCamera, firefoxLimits, rangeHints(videoRangeBounds{minFrameRate: 15, maxFrameRate: 60})))
		if p.envelope.minFrameRate != 30 || p.envelope.maxFrameRate != 30 || p.frameRateFloor != 15 {
			t.Errorf("plan = %+v", p)
		}
	})
}

var (
	spSmallPoint = rateDistortionPoint{codec: spH265, resolution: resolution{640, 360}, minBitRate: 500000}
	spLargePoint = rateDistortionPoint{codec: spH265, resolution: resolution{1920, 1080}, minBitRate: 2000000}
)

func spPointCapabilities(points ...rateDistortionPoint) videoCapabilities {
	return videoCapabilities{sensor: resolution{1920, 1080}, maxFrameRate: 30, minViewport: resolution{640, 360}, rateDistortionPoints: points}
}

// TestPlanBitRateFloorParityMatterjsServer is describe("the bit rate floor
// of each window") (StreamPolicyTest.ts:583-661, matterjs-server at 0bd06f2).
func TestPlanBitRateFloorParityMatterjsServer(t *testing.T) {
	for _, order := range []struct {
		name   string
		points []rateDistortionPoint
	}{
		{"smallest first", []rateDistortionPoint{spSmallPoint, spLargePoint}},
		{"largest first", []rateDistortionPoint{spLargePoint, spSmallPoint}},
	} {
		plan := planOfSelection(t, selectVideo(spPointCapabilities(order.points...), codecOnly(spH265), nil))
		t.Run("with trade-off points listed "+order.name+"/is the trade-off point for the window's own frame size", func(t *testing.T) {
			for _, c := range []struct {
				size resolution
				want int
			}{{resolution{1920, 1080}, 2000000}, {resolution{960, 540}, 500000}, {resolution{640, 360}, 500000}} {
				if got := planBitRateFloor(plan, c.size); got != c.want {
					t.Errorf("floor at %v = %d, want %d", c.size, got, c.want)
				}
			}
		})
		t.Run("with trade-off points listed "+order.name+"/asks the first trade-off point's bit rate in the certification window", func(t *testing.T) {
			cert := plan.certificationWindow
			if cert == nil || cert.minBitRate != order.points[0].minBitRate || cert.maxBitRate != order.points[0].minBitRate {
				t.Errorf("certification window = %+v, want %d", cert, order.points[0].minBitRate)
			}
		})
	}
	hd := rateDistortionPoint{codec: spH265, resolution: resolution{1280, 720}, minBitRate: 1000000}
	hdPlan := planOfSelection(t, selectVideo(spPointCapabilities(spLargePoint, hd), codecOnly(spH265), nil))
	t.Run("with trade-off points at 1080p and 720p/is the point just below a frame size between two points", func(t *testing.T) {
		if got := planBitRateFloor(hdPlan, resolution{1600, 900}); got != 1000000 {
			t.Errorf("floor = %d, want 1000000", got)
		}
	})
	t.Run("with trade-off points at 1080p and 720p/is the lowest point's bit rate for a frame size below every point", func(t *testing.T) {
		if got := planBitRateFloor(hdPlan, resolution{960, 540}); got != 1000000 {
			t.Errorf("floor = %d, want 1000000", got)
		}
	})
	t.Run("with trade-off points at 1080p and 720p/is the lowest point's bit rate for a plan whose size is below every point", func(t *testing.T) {
		small := planOfSelection(t, selectVideo(spPointCapabilities(spLargePoint, hd), selectedVideoCodecLimits{codec: spH265, videoCodecLimits: videoCodecLimits{maxPixels: stated(960 * 540)}}, nil))
		if small.envelope.maxResolution != (resolution{960, 540}) || small.envelope.minBitRate != 1000000 {
			t.Errorf("envelope = %+v", small.envelope)
		}
	})
	t.Run("is 1 below every trade-off point when the lowest point's bit rate is above the start", func(t *testing.T) {
		p := planOfSelection(t, selectVideo(spPointCapabilities(spLargePoint), selectedVideoCodecLimits{codec: spH265, videoCodecLimits: videoCodecLimits{maxBitRate: stated(1500000)}}, nil))
		if got := planBitRateFloor(p, resolution{640, 360}); got != 1 {
			t.Errorf("floor = %d, want 1", got)
		}
	})
	t.Run("is the caller's min_bit_rate at every frame size", func(t *testing.T) {
		p := planOfSelection(t, selectVideo(spPointCapabilities(spSmallPoint, spLargePoint), codecOnly(spH265), rangeHints(videoRangeBounds{minBitRate: 3000000})))
		if got := planBitRateFloor(p, resolution{640, 360}); got != 3000000 {
			t.Errorf("floor = %d, want 3000000", got)
		}
	})
}

// TestCertificationWindowParityMatterjsServer is describe("the certification
// window") (StreamPolicyTest.ts:663-707, matterjs-server at 0bd06f2).
func TestCertificationWindowParityMatterjsServer(t *testing.T) {
	certification := func(t *testing.T, points []rateDistortionPoint, hints *videoHints, maxBitRate, maxNetworkBandwidth ceiling) *videoEnvelope {
		t.Helper()
		caps := spPointCapabilities(points...)
		caps.maxNetworkBandwidth = maxNetworkBandwidth
		return planOfSelection(t, selectVideo(caps, selectedVideoCodecLimits{codec: spH265, videoCodecLimits: videoCodecLimits{maxBitRate: maxBitRate}}, hints)).certificationWindow
	}
	expensive := spLargePoint
	expensive.minBitRate = 10000000
	cases := []struct {
		name                string
		points              []rateDistortionPoint
		hints               *videoHints
		maxBitRate, network ceiling
		want                int // 0: left out
	}{
		{"is left out when the caller's min_bit_rate is above the first point", []rateDistortionPoint{spSmallPoint, spLargePoint}, rangeHints(videoRangeBounds{minBitRate: 1000000}), ceiling{}, ceiling{}, 0},
		{"is left out when the caller's max_bit_rate is below the first point", []rateDistortionPoint{spLargePoint, spSmallPoint}, rangeHints(videoRangeBounds{maxBitRate: 1500000}), ceiling{}, ceiling{}, 0},
		{"is left out when the offer's bit rate ceiling is below the first point", []rateDistortionPoint{spLargePoint, spSmallPoint}, nil, stated(1500000), ceiling{}, 0},
		{"is kept when the first point is above the default start", []rateDistortionPoint{expensive}, nil, ceiling{}, ceiling{}, 10000000},
		{"is kept when only the camera's network bandwidth is below the first point", []rateDistortionPoint{spLargePoint, spSmallPoint}, nil, ceiling{}, stated(1500000), 2000000},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := certification(t, c.points, c.hints, c.maxBitRate, c.network)
			switch {
			case c.want == 0 && got != nil:
				t.Errorf("certification window = %+v, want none", got)
			case c.want != 0 && (got == nil || got.minBitRate != c.want || got.maxBitRate != c.want):
				t.Errorf("certification window = %+v, want bit rate %d", got, c.want)
			}
		})
	}
}

// spVideoStream is the fixtures' stream with its overrides.
func spVideoStream(change func(*allocatedVideoStream)) allocatedVideoStream {
	s := allocatedVideoStream{
		videoStreamID: 1, overlays: noOverlays, streamUsage: spLiveView, videoCodec: spH265,
		minResolution: resolution{1920, 1080}, maxResolution: resolution{1920, 1080},
		minFrameRate: 1, maxFrameRate: 30, minBitRate: 800000, maxBitRate: 4000000, keyFrameInterval: 4000,
	}
	if change != nil {
		change(&s)
	}
	return s
}

func streamID(s *allocatedVideoStream) int {
	if s == nil {
		return -1
	}
	return int(s.videoStreamID)
}

// TestFindReusableVideoStreamParityMatterjsServer is
// describe("findReusableVideoStream") (StreamPolicyTest.ts:709-984,
// matterjs-server at 0bd06f2).
//
//nolint:funlen // one subtest per ported case
func TestFindReusableVideoStreamParityMatterjsServer(t *testing.T) {
	liveViewH265 := newVideoCallerBounds(codecOnly(spH265), nil)
	planOf := func(env videoEnvelope, floor int) *videoPlan {
		return &videoPlan{envelope: env, frameRateFloor: floor, frameRateCeiling: env.maxFrameRate, allowedFrameRate: env.maxFrameRate}
	}
	rate := func(r int) func(*allocatedVideoStream) (int, bool) {
		return func(*allocatedVideoStream) (int, bool) { return r, true }
	}
	noWindow := func(*allocatedVideoStream) (int, bool) { return 0, false }
	request := videoEnvelope{codec: spH265, minResolution: resolution{1920, 1080}, maxResolution: resolution{1920, 1080}, minFrameRate: 1, maxFrameRate: 30, minBitRate: 800000, maxBitRate: 4000000, keyFrameInterval: 2000}
	with := func(env videoEnvelope, f func(*videoEnvelope)) videoEnvelope {
		f(&env)
		return env
	}
	// reusable: a plan whose frame rate floor is the envelope's own
	// minimum, and a free encoder budget.
	reusable := func(streams []allocatedVideoStream, env videoEnvelope, bounds videoCallerBounds) int {
		return streamID(findReusableVideoStream(streams, planOf(env, env.minFrameRate), &bounds, rate(env.maxFrameRate), preferredUsage{}))
	}

	pointPlan := func(points ...rateDistortionPoint) *videoPlan {
		return planOfSelection(t, selectVideo(spPointCapabilities(points...), codecOnly(spH265), nil))
	}
	plan := pointPlan(spSmallPoint, spLargePoint)
	smaller := func(minBitRate int) allocatedVideoStream {
		return spVideoStream(func(s *allocatedVideoStream) {
			s.minResolution, s.maxResolution, s.minFrameRate, s.minBitRate, s.maxBitRate = resolution{640, 360}, resolution{960, 540}, 30, minBitRate, 1000000
		})
	}
	reused := func(p *videoPlan, c allocatedVideoStream) int {
		return streamID(findReusableVideoStream([]allocatedVideoStream{c}, p, &liveViewH265, rate(30), preferredUsage{}))
	}
	t.Run("with a bit rate floor per frame size/reuses a smaller stream down to the floor of its own frame size", func(t *testing.T) {
		if got := reused(plan, smaller(500000)); got != 1 {
			t.Errorf("at 500000: %d, want 1", got)
		}
		if got := reused(plan, smaller(400000)); got != -1 {
			t.Errorf("at 400000: %d, want none", got)
		}
	})
	t.Run("with a bit rate floor per frame size/reuses a stream allocated with the certification window", func(t *testing.T) {
		certified := spVideoStream(func(s *allocatedVideoStream) {
			s.minResolution, s.minFrameRate, s.minBitRate, s.maxBitRate = resolution{640, 360}, 30, 500000, 500000
		})
		if got := reused(plan, certified); got != 1 {
			t.Errorf("reused %d, want 1", got)
		}
	})
	t.Run("with a bit rate floor per frame size/reuses a stream below every trade-off point down to the lowest point's bit rate", func(t *testing.T) {
		hdPlan := pointPlan(spLargePoint, rateDistortionPoint{codec: spH265, resolution: resolution{1280, 720}, minBitRate: 1000000})
		if got := reused(hdPlan, smaller(1000000)); got != 1 {
			t.Errorf("at 1000000: %d, want 1", got)
		}
		if got := reused(hdPlan, smaller(900000)); got != -1 {
			t.Errorf("at 900000: %d, want none", got)
		}
	})

	single := func(change func(*allocatedVideoStream)) []allocatedVideoStream {
		return []allocatedVideoStream{spVideoStream(change)}
	}
	cases := []struct {
		name    string
		streams []allocatedVideoStream
		env     videoEnvelope
		bounds  videoCallerBounds
		want    int
	}{
		{"reuses a stream whose envelope sits inside the request", single(nil), request, liveViewH265, 1},
		// [720p..1080p] may deliver 720p, so it does not satisfy a 1080p
		// floor.
		{"refuses a stream whose floor is below the requested floor", single(func(s *allocatedVideoStream) { s.minResolution = resolution{1280, 720} }), request, liveViewH265, -1},
		{"refuses a stream whose ceiling is above the requested ceiling", single(func(s *allocatedVideoStream) { s.maxResolution = resolution{2560, 1440} }), request, liveViewH265, -1},
		{"refuses a stream using a different codec", single(func(s *allocatedVideoStream) { s.videoCodec = spH264 }), request, liveViewH265, -1},
		{"reuses a stream allocated for another usage", single(func(s *allocatedVideoStream) { s.streamUsage = spRecording }), request, liveViewH265, 1},
		{"reuses a stream the device allocated for its own use", single(func(s *allocatedVideoStream) { s.streamUsage = csdef.StreamUsageInternal }), request, liveViewH265, 1},
		// The envelope admits the stream, so only the caller's own bound
		// refuses it.
		{"refuses a stream whose bit-rate ceiling is above the one the caller stated", single(func(s *allocatedVideoStream) { s.maxBitRate = 8000000 }), with(request, func(e *videoEnvelope) { e.maxBitRate = 8000000 }), newVideoCallerBounds(codecOnly(spH265), rangeHints(videoRangeBounds{maxBitRate: 500000})), -1},
		{"refuses a stream below the bit-rate floor the caller stated", single(nil), request, newVideoCallerBounds(codecOnly(spH265), rangeHints(videoRangeBounds{minBitRate: 1000000})), -1},
		// Not a caller bound, so this stream is still available to the
		// degraded rung — flagged.
		{"refuses a stream outside the bit-rate range the server computed", single(nil), with(request, func(e *videoEnvelope) { e.maxBitRate = 2000000 }), liveViewH265, -1},
		{"prefers a stream whose encoder is already running", []allocatedVideoStream{spVideoStream(nil), spVideoStream(func(s *allocatedVideoStream) { s.videoStreamID, s.referenceCount = 2, 1 })}, request, liveViewH265, 2},
		{"refuses a stream whose frame-rate floor is below the requested floor", single(nil), with(request, func(e *videoEnvelope) { e.minFrameRate = 15 }), liveViewH265, -1},
		{"refuses a stream that does not reach the plan's frame rate", single(func(s *allocatedVideoStream) { s.maxFrameRate = 5 }), request, liveViewH265, -1},
		// An unstated overlay resolves to false, not to "either will do".
		{"refuses a stream whose overlays are not the ones the envelope asked for", single(func(s *allocatedVideoStream) { s.overlays = overlayBounds{watermark: flagOn, osd: flagOff} }), with(request, func(e *videoEnvelope) { e.overlays = noOverlays }), liveViewH265, -1},
		{"reuses a stream carrying exactly the overlays the envelope asked for", single(func(s *allocatedVideoStream) { s.overlays = overlayBounds{watermark: flagOn, osd: flagOff} }), with(request, func(e *videoEnvelope) { e.overlays = overlayBounds{watermark: flagOn, osd: flagOff} }), liveViewH265, 1},
		// 1440x1440 has the same area as 1920x1080 but is square.
		{"refuses a stream with the same pixel count but a different aspect ratio", single(func(s *allocatedVideoStream) {
			s.minResolution, s.maxResolution = resolution{1440, 1440}, resolution{1440, 1440}
		}), request, liveViewH265, -1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := reusable(c.streams, c.env, c.bounds); got != c.want {
				t.Errorf("reused %d, want %d", got, c.want)
			}
		})
	}
	t.Run("reuses a stream above the default frame rate cap, up to the rate the plan allows", func(t *testing.T) {
		fast := single(func(s *allocatedVideoStream) { s.minFrameRate, s.maxFrameRate = 30, 120 })
		allowing := func(allowed int) *videoPlan {
			p := planOf(request, 1)
			p.allowedFrameRate = allowed
			return p
		}
		if got := streamID(findReusableVideoStream(fast, allowing(120), &liveViewH265, rate(30), preferredUsage{})); got != 1 {
			t.Errorf("allowing 120: %d, want 1", got)
		}
		if got := streamID(findReusableVideoStream(fast, allowing(60), &liveViewH265, rate(30), preferredUsage{})); got != -1 {
			t.Errorf("allowing 60: %d, want none", got)
		}
	})
	preferLive := preferredUsage{usage: spLiveView, set: true}
	t.Run("prefers a stream of the session's usage among equal candidates", func(t *testing.T) {
		streams := []allocatedVideoStream{spVideoStream(func(s *allocatedVideoStream) { s.streamUsage = spRecording }), spVideoStream(func(s *allocatedVideoStream) { s.videoStreamID = 2 })}
		if got := streamID(findReusableVideoStream(streams, planOf(request, request.minFrameRate), &liveViewH265, rate(request.maxFrameRate), preferLive)); got != 2 {
			t.Errorf("reused %d, want 2", got)
		}
	})
	t.Run("prefers an idle stream of the session's usage over a running stream of another usage", func(t *testing.T) {
		streams := []allocatedVideoStream{
			spVideoStream(func(s *allocatedVideoStream) { s.streamUsage, s.referenceCount = spRecording, 1 }),
			spVideoStream(func(s *allocatedVideoStream) { s.videoStreamID = 2 }),
		}
		if got := streamID(findReusableVideoStream(streams, planOf(request, request.minFrameRate), &liveViewH265, rate(request.maxFrameRate), preferLive)); got != 2 {
			t.Errorf("reused %d, want 2", got)
		}
	})
	target := with(request, func(e *videoEnvelope) { e.minFrameRate = 30 })
	t.Run("measures the frame rate range from the plan's floor, not from its target rate", func(t *testing.T) {
		if got := streamID(findReusableVideoStream(single(func(s *allocatedVideoStream) { s.minFrameRate = 15 }), planOf(target, 1), &liveViewH265, rate(30), preferredUsage{})); got != 1 {
			t.Errorf("reused %d, want 1", got)
		}
	})
	// The encoder budget leaves room for 15 fps only, so that is what an
	// allocate would ask for.
	slower := single(func(s *allocatedVideoStream) { s.minFrameRate, s.maxFrameRate = 15, 15 })
	t.Run("reuses a stream at the frame rate the request would ask for first, below the plan's", func(t *testing.T) {
		if got := streamID(findReusableVideoStream(slower, planOf(target, 1), &liveViewH265, rate(15), preferredUsage{})); got != 1 {
			t.Errorf("at 15: %d, want 1", got)
		}
		if got := streamID(findReusableVideoStream(slower, planOf(target, 1), &liveViewH265, rate(30), preferredUsage{})); got != -1 {
			t.Errorf("at 30: %d, want none", got)
		}
	})
	// An allocate could only succeed by freeing room; the stream costs
	// nothing.
	t.Run("reuses a slower stream when no window fits the encoder budget", func(t *testing.T) {
		if got := streamID(findReusableVideoStream(slower, planOf(target, 1), &liveViewH265, noWindow, preferredUsage{})); got != 1 {
			t.Errorf("reused %d, want 1", got)
		}
	})
}

// spStream is the fixture STREAM of describe("satisfiesVideoCallerBounds").
var spStream = allocatedVideoStream{
	videoStreamID: 4, overlays: noOverlays, streamUsage: spLiveView, videoCodec: spH265,
	minResolution: resolution{1280, 720}, maxResolution: resolution{2560, 1440},
	minFrameRate: 1, maxFrameRate: 30, minBitRate: 800000, maxBitRate: 4000000, keyFrameInterval: 4000, referenceCount: 1,
}

// TestSatisfiesVideoCallerBoundsParityMatterjsServer is
// describe("satisfiesVideoCallerBounds") (StreamPolicyTest.ts:986-1050,
// matterjs-server at 0bd06f2).
func TestSatisfiesVideoCallerBoundsParityMatterjsServer(t *testing.T) {
	offer := func(l videoCodecLimits) videoCallerBounds {
		return newVideoCallerBounds(selectedVideoCodecLimits{codec: spH265, videoCodecLimits: l}, nil)
	}
	overlaid := func(o overlayBounds) videoCallerBounds {
		return newVideoCallerBounds(codecOnly(spH265), &videoHints{overlays: o})
	}
	slowest := spStream
	slowest.minFrameRate, slowest.maxFrameRate = 1, 1
	watermarked := spStream
	watermarked.overlays = overlayBounds{watermark: flagOn, osd: flagOff}
	bothOverlays := spStream
	bothOverlays.overlays = overlayBounds{watermark: flagOn, osd: flagOn}
	cases := []struct {
		name   string
		stream allocatedVideoStream
		bounds videoCallerBounds
		want   bool
	}{
		{"refuses a stream past the pixel budget the offer stated for the selected codec", spStream, offer(videoCodecLimits{maxPixels: stated(1280 * 720)}), false},
		{"refuses a stream past the frame rate the offer stated", spStream, offer(videoCodecLimits{maxFrameRate: stated(15)}), false},
		// 2560x1440 at 380160 pixels a second is 0.10 frames a second; the
		// stream runs at 1.
		{"refuses a stream whose size leaves the offer's pixel rate no whole frame per second", slowest, offer(videoCodecLimits{maxPixelsPerSecond: stated(380160)}), false},
		{"refuses a stream past the bit rate the offer stated", spStream, offer(videoCodecLimits{maxBitRate: stated(1000000)}), false},
		{"refuses a stream whose overlays differ from the ones the caller stated", watermarked, overlaid(overlayBounds{watermark: flagOff}), false},
		{"refuses a stream whose overlays differ from the ones the caller stated", spStream, overlaid(overlayBounds{osd: flagOn}), false},
		{"accepts any overlays for a caller that stated none, which is the envelope's business", bothOverlays, overlaid(overlayBounds{}), true},
		{"accepts a stream inside every limit the offer stated", spStream, offer(videoCodecLimits{maxPixels: stated(2560 * 1440), maxFrameRate: stated(30), maxBitRate: stated(4000000)}), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := satisfiesVideoCallerBounds(&c.stream, &c.bounds); got != c.want {
				t.Errorf("satisfies = %v, want %v", got, c.want)
			}
		})
	}
}

// TestFindDegradedVideoStreamParityMatterjsServer is
// describe("findDegradedVideoStream") (StreamPolicyTest.ts:1052-1233,
// matterjs-server at 0bd06f2).
func TestFindDegradedVideoStreamParityMatterjsServer(t *testing.T) {
	inUseWide := spStream
	inUseWide.maxResolution = resolution{1920, 1080}
	bounds := func(b videoRangeBounds) *videoCallerBounds {
		cb := newVideoCallerBounds(codecOnly(spH265), rangeHints(b))
		return &cb
	}
	derive := func(change func(*allocatedVideoStream)) allocatedVideoStream {
		s := inUseWide
		change(&s)
		return s
	}
	pinned := videoRangeBounds{minResolution: resolution{1920, 1080}, maxResolution: resolution{1920, 1080}}
	h264 := newVideoCallerBounds(codecOnly(spH264), nil)
	recording := derive(func(s *allocatedVideoStream) { s.streamUsage = spRecording })
	live := preferredUsage{usage: spLiveView, set: true}
	cases := []struct {
		name      string
		streams   []allocatedVideoStream
		bounds    *videoCallerBounds
		preferred preferredUsage
		want      int
	}{
		{"hands out a wider in-use stream when the caller stated no bounds", []allocatedVideoStream{inUseWide}, bounds(videoRangeBounds{}), preferredUsage{}, 4},
		// The caller pinned 1080p, so [720p..1080p] may deliver less.
		{"refuses a stream below a floor the caller stated", []allocatedVideoStream{inUseWide}, bounds(videoRangeBounds{minResolution: resolution{1920, 1080}}), preferredUsage{}, -1},
		{"refuses a stream whose width alone is below the floor the caller stated", []allocatedVideoStream{inUseWide}, bounds(videoRangeBounds{minResolution: resolution{1400, 700}}), preferredUsage{}, -1},
		{"refuses a stream whose height alone is below the floor the caller stated", []allocatedVideoStream{inUseWide}, bounds(videoRangeBounds{minResolution: resolution{1200, 900}}), preferredUsage{}, -1},
		{"refuses a stream above a ceiling the caller stated", []allocatedVideoStream{inUseWide}, bounds(videoRangeBounds{maxResolution: resolution{1280, 720}}), preferredUsage{}, -1},
		{"refuses a stream whose width alone is above the ceiling the caller stated", []allocatedVideoStream{inUseWide}, bounds(videoRangeBounds{maxResolution: resolution{1800, 1200}}), preferredUsage{}, -1},
		{"refuses a stream below a frame-rate floor the caller stated", []allocatedVideoStream{inUseWide}, bounds(videoRangeBounds{minFrameRate: 15}), preferredUsage{}, -1},
		{"refuses a stream above a frame-rate ceiling the caller stated", []allocatedVideoStream{inUseWide}, bounds(videoRangeBounds{maxFrameRate: 15}), preferredUsage{}, -1},
		{"refuses a stream using a different codec", []allocatedVideoStream{inUseWide}, &h264, preferredUsage{}, -1},
		{"refuses a stream below a bit-rate floor the caller stated", []allocatedVideoStream{inUseWide}, bounds(videoRangeBounds{minBitRate: 1000000}), preferredUsage{}, -1},
		// The caller's ceiling is what its link can carry; a 4 Mbit/s stream
		// overruns it.
		{"refuses a stream above a bit-rate ceiling the caller stated", []allocatedVideoStream{inUseWide}, bounds(videoRangeBounds{maxBitRate: 2000000}), preferredUsage{}, -1},
		{"accepts a stream inside the bit-rate bounds the caller stated", []allocatedVideoStream{inUseWide}, bounds(videoRangeBounds{minBitRate: 500000, maxBitRate: 4000000}), preferredUsage{}, 4},
		{"hands out a stream allocated for another usage", []allocatedVideoStream{recording}, bounds(videoRangeBounds{}), preferredUsage{}, 4},
		{"prefers a stream of the session's usage among equally large ones", []allocatedVideoStream{derive(func(s *allocatedVideoStream) { s.videoStreamID, s.streamUsage = 5, spRecording }), inUseWide}, bounds(videoRangeBounds{}), live, 4},
		{"prefers a smaller stream of the session's usage over a larger one of another usage", []allocatedVideoStream{derive(func(s *allocatedVideoStream) {
			s.videoStreamID, s.streamUsage, s.maxResolution = 5, spRecording, resolution{2560, 1440}
		}), inUseWide}, bounds(videoRangeBounds{}), live, 4},
		// Matching the pins exactly is not a degradation of anything the
		// caller stated.
		{"accepts a stream that matches pinned bounds exactly", []allocatedVideoStream{derive(func(s *allocatedVideoStream) { s.minResolution = resolution{1920, 1080} })}, bounds(pinned), preferredUsage{}, 4},
		{"prefers the most capable stream when several degraded candidates qualify", []allocatedVideoStream{
			derive(func(s *allocatedVideoStream) { s.videoStreamID, s.maxResolution = 5, resolution{1280, 720} }),
			derive(func(s *allocatedVideoStream) { s.videoStreamID = 6 }),
		}, bounds(videoRangeBounds{}), preferredUsage{}, 6},
		// 1440x1440 has the same area as 1920x1080 but is square.
		{"refuses a stream with the same pixel count but a different aspect ratio than the pinned bounds", []allocatedVideoStream{derive(func(s *allocatedVideoStream) {
			s.minResolution, s.maxResolution = resolution{1440, 1440}, resolution{1440, 1440}
		})}, bounds(pinned), preferredUsage{}, -1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := streamID(findDegradedVideoStream(c.streams, c.bounds, c.preferred)); got != c.want {
				t.Errorf("degraded %d, want %d", got, c.want)
			}
		})
	}
}

// TestFreeEncodedPixelRateParityMatterjsServer is describe("freeEncodedPixelRate")
// (StreamPolicyTest.ts:1235-1317, matterjs-server at 0bd06f2).
func TestFreeEncodedPixelRateParityMatterjsServer(t *testing.T) {
	sensor := videoEnvelope{codec: spH265, minResolution: resolution{640, 360}, maxResolution: resolution{2560, 1440}, minFrameRate: 30, maxFrameRate: 30, minBitRate: 800000, maxBitRate: 4000000, keyFrameInterval: 4000}
	const sensorRate = 2560 * 1440 * 30
	video := func(w, h, fps int) allocatedVideoStream {
		return allocatedVideoStream{videoStreamID: 7, overlays: noOverlays, streamUsage: spLiveView, videoCodec: spH265, minResolution: resolution{w, h}, maxResolution: resolution{w, h}, minFrameRate: 1, maxFrameRate: fps, minBitRate: 800000, maxBitRate: 4000000, keyFrameInterval: 4000}
	}
	snapshot := func(w, h, fps int, encoded bool) allocatedSnapshotStream {
		return allocatedSnapshotStream{snapshotStreamID: 1, overlays: noOverlays, minResolution: resolution{w, h}, maxResolution: resolution{w, h}, quality: 90, frameRate: fps, encodedPixels: encoded}
	}
	t.Run("states nothing for a camera that states no budget", func(t *testing.T) {
		if _, ok := freeEncodedPixelRate(videoPixelRateBudget{}); ok {
			t.Error("a budget-less camera states a free rate")
		}
	})
	t.Run("charges a window its frame size times its frame rate", func(t *testing.T) {
		if got := encodedPixelRate(sensor); got != sensorRate {
			t.Errorf("rate = %d, want %d", got, sensorRate)
		}
	})
	cases := []struct {
		name   string
		budget videoPixelRateBudget
		want   float64
	}{
		{"subtracts what the camera's video streams reserve", videoPixelRateBudget{maxEncodedPixelRate: stated(sensorRate), videoStreams: []allocatedVideoStream{video(1280, 720, 30)}}, sensorRate - 1280*720*30},
		// EncodedPixels false means the stream draws nothing from that budget
		// (§11.2.6.13.8).
		{"subtracts a snapshot stream the camera counts in its encoded pixel rate, and no other", videoPixelRateBudget{maxEncodedPixelRate: stated(sensorRate), snapshotStreams: []allocatedSnapshotStream{snapshot(2560, 1440, 15, true), snapshot(2560, 1440, 15, false)}}, sensorRate / 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got, ok := freeEncodedPixelRate(c.budget); !ok || got != c.want {
				t.Errorf("free = %v, %v; want %v", got, ok, c.want)
			}
		})
	}
	t.Run("can be negative once the camera's streams reserve more than the budget", func(t *testing.T) {
		if got, ok := freeEncodedPixelRate(videoPixelRateBudget{maxEncodedPixelRate: stated(1000), videoStreams: []allocatedVideoStream{video(640, 360, 30)}}); !ok || got >= 0 {
			t.Errorf("free = %v, %v; want below 0", got, ok)
		}
	})
}

// TestSatisfiesAudioCallerBoundsParityMatterjsServer is
// describe("satisfiesAudioCallerBounds") (StreamPolicyTest.ts:1319-1339,
// matterjs-server at 0bd06f2).
func TestSatisfiesAudioCallerBoundsParityMatterjsServer(t *testing.T) {
	liveViewAudio := allocatedAudioStream{audioStreamID: 4, streamUsage: spLiveView, audioCodec: 0, channelCount: 1, sampleRate: 48000, bitRate: 64000, bitDepth: 16, referenceCount: 1}
	recording := liveViewAudio
	recording.streamUsage = spRecording
	t.Run("accepts a stream allocated for another usage", func(t *testing.T) {
		if !satisfiesAudioCallerBounds(&recording, &audioHints{}) {
			t.Error("refused")
		}
	})
	t.Run("refuses a stream whose sample rate is not the one the caller stated", func(t *testing.T) {
		if satisfiesAudioCallerBounds(&liveViewAudio, &audioHints{sampleRate: 16000}) {
			t.Error("accepted")
		}
	})
}

// TestChooseEvictionVictimParityMatterjsServer is describe("chooseEvictionVictim")
// (StreamPolicyTest.ts:1341-1399, matterjs-server at 0bd06f2).
func TestChooseEvictionVictimParityMatterjsServer(t *testing.T) {
	// The camera's own ranking: LiveView highest, then Recording, then
	// Analysis.
	priorities := []csdef.StreamUsageEnum{spLiveView, spRecording, spAnalysis}
	idle := func(id uint16, usage csdef.StreamUsageEnum) allocatedVideoStream {
		return allocatedVideoStream{videoStreamID: id, overlays: noOverlays, streamUsage: usage, videoCodec: spH265, minResolution: resolution{1280, 720}, maxResolution: resolution{1280, 720}, minFrameRate: 1, maxFrameRate: 30, minBitRate: 400000, maxBitRate: 4000000, keyFrameInterval: 4000}
	}
	oursBy := func(ids ...uint16) func(*allocatedVideoStream) bool {
		return func(s *allocatedVideoStream) bool {
			for _, id := range ids {
				if s.videoStreamID == id {
					return true
				}
			}
			return false
		}
	}
	victim := func(streams []allocatedVideoStream, ours func(*allocatedVideoStream) bool) int {
		return streamID(chooseEvictionVictim(streams, priorities, ours))
	}
	ours, foreign := idle(30, spRecording), idle(31, spAnalysis)
	unranked := idle(30, 9)
	referenced := idle(30, spRecording)
	referenced.referenceCount = 1
	cases := []struct {
		name    string
		streams []allocatedVideoStream
		ours    func(*allocatedVideoStream) bool
		want    int
	}{
		// StreamUsagePriorities guides the camera's own arbitration, not
		// which controller pays for a request.
		{"takes a stream of its own before a foreign one the camera ranks lower", []allocatedVideoStream{ours, foreign}, oursBy(30), 30},
		{"takes a stream of its own before a foreign one the camera ranks lower", []allocatedVideoStream{foreign, ours}, oursBy(30), 30},
		{"ranks by the camera's priorities among its own streams", []allocatedVideoStream{ours, foreign}, oursBy(30, 31), 31},
		{"ranks by the camera's priorities among foreign streams once it holds none of its own", []allocatedVideoStream{ours, foreign}, oursBy(), 31},
		{"takes a usage the camera does not rank last of its own, ahead of no foreign stream", []allocatedVideoStream{unranked, foreign}, oursBy(30), 30},
		{"takes a usage the camera does not rank last of its own, ahead of no foreign stream", []allocatedVideoStream{unranked, foreign}, oursBy(31), 31},
		{"never takes a referenced stream or an Internal one, whoever allocated it", []allocatedVideoStream{referenced, idle(31, csdef.StreamUsageInternal)}, oursBy(30, 31), -1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := victim(c.streams, c.ours); got != c.want {
				t.Errorf("victim %d, want %d", got, c.want)
			}
		})
	}
}

// TestComputeAudioEnvelopeParityMatterjsServer is describe("computeAudioEnvelope")
// (StreamPolicyTest.ts:1401-1606, matterjs-server at 0bd06f2).
func TestComputeAudioEnvelopeParityMatterjsServer(t *testing.T) {
	const opus, aac = csdef.AudioCodecOpus, csdef.AudioCodecAacLc
	caps := func(codecs ...csdef.AudioCodecEnum) audioCapabilities {
		return audioCapabilities{supportedCodecs: codecs, maxNumberOfChannels: 2, supportedSampleRates: []int{48000, 16000}, supportedBitDepths: []int{16}}
	}
	offering := func(audio mediaDisposition) *sdpVideoConstraints {
		return &sdpVideoConstraints{video: mediaDisposition{state: mediaAbsent}, audio: audio}
	}
	sampleRateBound := &IncompatibleBound{Field: BoundSampleRate, Requested: "44100", Limit: "48000, 16000"}
	cases := []struct {
		name  string
		caps  audioCapabilities
		sdp   *sdpVideoConstraints
		hints *audioHints
		// want nil: no envelope; check, when set, holds the envelope.
		want  *audioEnvelope
		bound *IncompatibleBound
	}{
		{"picks the camera's codec when the caller offers no SDP", caps(opus), nil, nil, &audioEnvelope{codec: opus, channelCount: 2, sampleRate: 48000, bitRate: 64000, bitDepth: 16}, nil},
		{"reports nothing when no codec suits both sides", caps(aac), offering(mediaDisposition{state: mediaReceiving, codecs: []string{"OPUS"}}), nil, nil, nil},
		// Static payload types state no codec, so they must not filter out
		// the camera's codecs.
		{"keeps the camera's codecs when an offered audio section states none", caps(opus, aac), offering(mediaDisposition{state: mediaReceiving}), nil, &audioEnvelope{codec: opus, channelCount: 2, sampleRate: 48000, bitRate: 64000, bitDepth: 16}, nil},
		{"narrows to a caller codec preference", caps(opus, aac), nil, &audioHints{codecs: []string{"AAC"}}, &audioEnvelope{codec: aac, channelCount: 2, sampleRate: 48000, bitRate: 64000, bitDepth: 16}, nil},
		// Only the manager knows whether the caller asked for audio, so
		// failing is its decision.
		{"describes no envelope when the caller's codec preference leaves the camera nothing", caps(opus), nil, &audioHints{codecs: []string{"AAC"}}, nil, nil},
		{"describes no envelope for a hint codec name the device has no matching number for", caps(opus, aac), nil, &audioHints{codecs: []string{"UNKNOWN_CODEC"}}, nil, nil},
		// The caller stated no codec, so an audio-less session drops nothing
		// it asked for.
		{"reports nothing, without failing, when the offer shares no codec with the camera", caps(opus), offering(mediaDisposition{state: mediaReceiving, codecs: []string{"AAC"}}), nil, nil, nil},
		// Both are unmet; the sample rate is the value the caller can
		// actually change.
		{"reports a sample rate the camera cannot serve ahead of a codec the offer ruled out", caps(opus), offering(mediaDisposition{state: mediaReceiving, codecs: []string{"AAC"}}), &audioHints{codecs: []string{"OPUS"}, sampleRate: 44100}, nil, sampleRateBound},
		{"does not filter by codec on an SDP audio m-line marked absent", caps(opus), &sdpVideoConstraints{video: mediaDisposition{state: mediaReceiving}, audio: mediaDisposition{state: mediaAbsent}}, nil, &audioEnvelope{codec: opus, channelCount: 2, sampleRate: 48000, bitRate: 64000, bitDepth: 16}, nil},
		{"fails a channel count above what the camera states, rather than clamping it", caps(opus), nil, &audioHints{channelCount: 8}, nil, &IncompatibleBound{Field: BoundChannelCount, Requested: "8", Limit: "2"}},
		{"uses a channel count the camera can serve", caps(opus), nil, &audioHints{channelCount: 1}, &audioEnvelope{codec: opus, channelCount: 1, sampleRate: 48000, bitRate: 64000, bitDepth: 16}, nil},
		{"uses a requested sample rate the camera supports", caps(opus), nil, &audioHints{sampleRate: 16000}, &audioEnvelope{codec: opus, channelCount: 2, sampleRate: 16000, bitRate: 64000, bitDepth: 16}, nil},
		{"fails a sample rate the camera does not list, rather than substituting its own", caps(opus), nil, &audioHints{sampleRate: 44100}, nil, sampleRateBound},
		{"keeps the caller's bit rate rather than the default", caps(opus), nil, &audioHints{bitRate: 32000}, &audioEnvelope{codec: opus, channelCount: 2, sampleRate: 48000, bitRate: 32000, bitDepth: 16}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := computeAudioEnvelope(c.caps, c.sdp, c.hints)
			if !reflect.DeepEqual(got.envelope, c.want) || !reflect.DeepEqual(got.unsatisfiable, c.bound) {
				t.Errorf("selection = %+v / %+v, want %+v / %+v", got.envelope, got.unsatisfiable, c.want, c.bound)
			}
		})
	}
}
