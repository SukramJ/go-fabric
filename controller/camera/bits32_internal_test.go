// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"math"
	"testing"
	"time"

	csdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavstreammanagement"
)

// returnsWithin runs fn and reports whether it returned within d; a
// spinning fn is left behind (the test fails).
func returnsWithin(d time.Duration, fn func()) bool {
	done := make(chan struct{})
	go func() { fn(); close(done) }()
	select {
	case <-done:
		return true
	case <-time.After(d):
		return false
	}
}

// TestBitRateSearchTerminatesBelowZero (review II, R2-P6-bound-2250): the
// bit rate halvings and the window search's step after an unservable
// refusal terminate for every floor, also a negative one — the value a
// uint32 bit rate at or above 2^31 had once converted to int on a 32-bit
// build — and for a floor of 0. Before, bitRateSteps halved to 0 and
// looped there forever, under the endpoint lock.
func TestBitRateSearchTerminatesBelowZero(t *testing.T) {
	t.Parallel()
	for _, floor := range []int{math.MinInt32, -1, 0} {
		sel := computeVideoEnvelope(videoEnvelopeArgs{
			capabilities: videoCapabilities{
				sensor:       resolution{1920, 1080},
				maxFrameRate: 30,
				rateDistortionPoints: []rateDistortionPoint{
					{codec: csdef.VideoCodecH264, resolution: resolution{640, 480}, minBitRate: floor},
				},
			},
			limits: selectedVideoCodecLimits{codec: csdef.VideoCodecH264},
		})
		if sel.plan == nil {
			t.Fatalf("floor %d: no plan: %+v", floor, sel.unsatisfiable)
		}
		plan := sel.plan
		if !returnsWithin(5*time.Second, func() { bitRateSteps(plan, 0) }) {
			t.Errorf("floor %d: bitRateSteps did not return", floor)
			continue
		}
		s := newVideoWindowSearch(plan, func(videoEnvelope) bool { return true }, 0)
		if !returnsWithin(5*time.Second, func() { s.refuse(refusedUnservable) }) {
			t.Errorf("floor %d: the search's step after an unservable refusal did not return", floor)
		}
	}
}

// TestUint32ValuesSaturateAtTheIntCeiling (review II, R2-P6-bound-2250): a
// camera's uint32 value becomes an int saturated at the build's int
// ceiling, never a negative one: on a 32-bit build (ceiling 2^31−1) a bit
// rate of 2^31 reads as 2^31−1. The 32-bit ceiling is passed in, so the
// rule is held on every build.
func TestUint32ValuesSaturateAtTheIntCeiling(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		v       uint32
		ceiling uint64
		want    int
	}{
		{1 << 31, math.MaxInt32, math.MaxInt32},
		{math.MaxUint32, math.MaxInt32, math.MaxInt32},
		{math.MaxInt32, math.MaxInt32, math.MaxInt32},
		{8_000_000, math.MaxInt32, 8_000_000},
		{math.MaxUint32, math.MaxInt, int(min(uint64(math.MaxUint32), uint64(math.MaxInt)))},
	} {
		if got := saturatedInt(c.v, c.ceiling); got != c.want {
			t.Errorf("saturatedInt(%d, %d) = %d, want %d", c.v, c.ceiling, got, c.want)
		}
	}
	st := videoStreamOf(csdef.VideoStreamStruct{MinBitRate: 1 << 31, MaxBitRate: math.MaxUint32})
	if st.minBitRate <= 0 || st.maxBitRate < st.minBitRate {
		t.Errorf("a stream's bit rates read as %d..%d", st.minBitRate, st.maxBitRate)
	}
}

// TestPixelArithmeticIsInt64 (review II, R2-P6-bound-2251): a frame's area
// and the encoded pixel rates are int64, so neither wraps on a 32-bit
// build: 65535×65535 is 4 294 836 225, five 3840×2160 streams at 60 fps
// commit 2 488 320 000 pixels per second, and a scaled frame keeps its
// budget. Before, both were int and wrapped past 2^31−1 there.
func TestPixelArithmeticIsInt64(t *testing.T) {
	t.Parallel()
	var area int64 = resolution{65535, 65535}.pixels()
	if area != 4_294_836_225 {
		t.Errorf("65535x65535 = %d pixels", area)
	}
	uhd := allocatedVideoStream{maxResolution: resolution{3840, 2160}, maxFrameRate: 60}
	free, ok := freeEncodedPixelRate(videoPixelRateBudget{
		maxEncodedPixelRate: ceiling{set: true, value: 3e9},
		videoStreams:        []allocatedVideoStream{uhd, uhd, uhd, uhd, uhd},
	})
	if !ok || free != 3e9-2_488_320_000 {
		t.Errorf("free pixel rate after five 4K60 streams = %v, %v; want %v", free, ok, 3e9-2_488_320_000)
	}
	var rate int64 = encodedPixelRate(videoEnvelope{maxResolution: resolution{7680, 4320}, maxFrameRate: 30})
	if rate != 995_328_000 {
		t.Errorf("encodedPixelRate(8K30) = %d", rate)
	}
	if got := scaleToPixels(resolution{65535, 65535}, 1920*1080); got.pixels() > 1920*1080 {
		t.Errorf("scaleToPixels(65535x65535, 1080p) = %v, over the budget", got)
	}
}

// TestRequestedResolutionIsAtLeastOne (review II, R2-P6-doc-2252): the
// requested size is never larger than the source on a side the source
// states above 0, and at least 1 on each side — so a side of 0 is asked
// for as 1, as the comment states. Before, the comment said "never larger
// than available" without the exception.
func TestRequestedResolutionIsAtLeastOne(t *testing.T) {
	t.Parallel()
	if got := requestedResolution(resolution{0, 480}, &resolutionTarget{width: 320}); got != (resolution{1, 480}) {
		t.Errorf("requestedResolution(0x480, width 320) = %v, want 1x480", got)
	}
	if got := requestedResolution(resolution{640, 480}, &resolutionTarget{width: 320}); got != (resolution{320, 240}) {
		t.Errorf("requestedResolution(640x480, width 320) = %v, want 320x240", got)
	}
}
