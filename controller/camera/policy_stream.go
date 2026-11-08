// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"math"
	"slices"
	"strconv"
	"strings"

	csdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavstreammanagement"
)

// defaultMaxBitRate is the bit rate a window starts from when the caller
// states no max_bit_rate. Mirrors matterjs-server
// packages/ws-controller/src/camera/streamPolicy.ts:27 at 0bd06f2.
const defaultMaxBitRate = 8000000

// rateDistortionPoint is one RateDistortionTradeOffPoints entry. Mirrors
// matterjs-server packages/ws-controller/src/camera/streamPolicy.ts:34-38 at
// 0bd06f2.
type rateDistortionPoint struct {
	codec      csdef.VideoCodecEnum
	resolution resolution
	minBitRate int
}

// videoCapabilities are what the camera states about its video. minViewport
// is the zero resolution when the camera states none. Mirrors
// matterjs-server packages/ws-controller/src/camera/streamPolicy.ts:40-46 at
// 0bd06f2.
type videoCapabilities struct {
	sensor               resolution
	maxFrameRate         int
	minViewport          resolution
	rateDistortionPoints []rateDistortionPoint
	maxNetworkBandwidth  ceiling
}

// videoRangeBounds are a caller's video bounds: a stated field is a hard
// bound, a zero one is left to the controller (the command boundary admits
// no 0). Mirrors matterjs-server
// packages/ws-controller/src/camera/streamPolicy.ts:48-56 at 0bd06f2.
type videoRangeBounds struct {
	minResolution resolution
	maxResolution resolution
	minFrameRate  int
	maxFrameRate  int
	minBitRate    int
	maxBitRate    int
}

// videoHints are a caller's video request; matterjs-server's hints also
// carry the codec names the caller accepts, which the stream manager
// (phase 37 wave C) reads. Mirrors matterjs-server
// packages/ws-controller/src/camera/streamPolicy.ts:58-60 at 0bd06f2.
type videoHints struct {
	videoRangeBounds
	overlays overlayBounds
}

// videoCallerBounds are the bounds no ladder rung may give up: the caller's
// and limits, the offer's decode ceiling for the resolved codec. Mirrors
// matterjs-server packages/ws-controller/src/camera/streamPolicy.ts:62-66 at
// 0bd06f2.
type videoCallerBounds struct {
	videoRangeBounds
	limits   selectedVideoCodecLimits
	overlays overlayBounds
}

// newVideoCallerBounds is the caller bounds of hints, nil for none. Mirrors
// matterjs-server packages/ws-controller/src/camera/streamPolicy.ts:68-79 at
// 0bd06f2.
func newVideoCallerBounds(limits selectedVideoCodecLimits, hints *videoHints) videoCallerBounds {
	b := videoCallerBounds{limits: limits}
	if hints != nil {
		b.videoRangeBounds = hints.videoRangeBounds
		b.overlays = hints.overlays
	}
	return b
}

// videoEnvelopeArgs are computeVideoEnvelope's inputs; overlays are as
// resolveOverlays resolved them, not the raw hints. Mirrors matterjs-server
// packages/ws-controller/src/camera/streamPolicy.ts:96-102 at 0bd06f2.
type videoEnvelopeArgs struct {
	capabilities videoCapabilities
	limits       selectedVideoCodecLimits
	hints        *videoHints
	overlays     overlayBounds
}

// floorInt is v rounded down to an int, saturating: the policies' numbers
// are JavaScript numbers in matterjs-server, which a Go int carries once
// they are whole.
func floorInt(v float64) int {
	switch {
	case v >= math.MaxInt:
		return math.MaxInt
	case v <= math.MinInt:
		return math.MinInt
	default:
		return int(math.Floor(v))
	}
}

// scaleToPixels fits a pixel budget on the same aspect ratio, in even
// dimensions the encoder can use. Mirrors matterjs-server
// packages/ws-controller/src/camera/streamPolicy.ts:108-114 at 0bd06f2.
func scaleToPixels(r resolution, maxPixels float64) resolution {
	if float64(r.pixels()) <= maxPixels {
		return r
	}
	factor := math.Sqrt(maxPixels / float64(r.pixels()))
	even := func(v int) int { return max(2, floorInt(float64(v)*factor/2)*2) }
	return resolution{width: even(r.width), height: even(r.height)}
}

// clampDown keeps r under ceiling on each side; VideoStreamAllocate checks
// width and height separately. Mirrors matterjs-server
// packages/ws-controller/src/camera/streamPolicy.ts:116-122 at 0bd06f2.
func clampDown(r, ceiling resolution) resolution {
	return resolution{width: min(r.width, ceiling.width), height: min(r.height, ceiling.height)}
}

// clampUp keeps r above floor on each side. Mirrors matterjs-server
// packages/ws-controller/src/camera/streamPolicy.ts:124-129 at 0bd06f2.
func clampUp(r, floor resolution) resolution {
	return resolution{width: max(r.width, floor.width), height: max(r.height, floor.height)}
}

// offerFrameRateCeiling is the offer's frame rate ceiling at r, ok false
// when the offer states none. It can be 0 and is never rounded up, since it
// is a decode ceiling; H.265's fractional max-fps is rounded down to the
// whole rate a window can ask for. Mirrors matterjs-server
// packages/ws-controller/src/camera/streamPolicy.ts:135-143 at 0bd06f2.
func offerFrameRateCeiling(limits videoCodecLimits, r resolution) (int, bool) {
	var ceilings []float64
	if limits.maxPixelsPerSecond.set {
		ceilings = append(ceilings, math.Floor(limits.maxPixelsPerSecond.value/float64(r.pixels())))
	}
	if limits.maxFrameRate.set {
		ceilings = append(ceilings, limits.maxFrameRate.value)
	}
	if len(ceilings) == 0 {
		return 0, false
	}
	return floorInt(slices.Min(ceilings)), true
}

// videoPlan is the best window to request, and what later windows derive
// from. Every window asks for a single frame rate (minimum equals maximum);
// planFrameRate gives it for each frame size. Mirrors matterjs-server
// packages/ws-controller/src/camera/streamPolicy.ts:145-174 at 0bd06f2.
type videoPlan struct {
	envelope videoEnvelope
	// frameRateFloor is the caller's min_frame_rate, or 1: the lowest rate
	// a later window may ask for.
	frameRateFloor int
	// frameRateCeiling is maxFps, capped by the caller's max_frame_rate, or
	// by defaultMaxFrameRate without one.
	frameRateCeiling int
	// allowedFrameRate is the highest rate the camera, the caller and the
	// offer allow at the envelope's size. The default cap limits only what
	// is allocated: a stream the camera already runs above it is reused.
	allowedFrameRate int
	// limits are the offer's decode ceiling, which sets a further frame
	// rate ceiling per frame size.
	limits videoCodecLimits
	// bitRatePoints are the codec's trade-off points, which give each
	// window its bit rate floor (planBitRateFloor); empty when the caller
	// states min_bit_rate, which is then every window's floor.
	bitRatePoints []rateDistortionPoint
	// certificationWindow is the last rung, nil without one
	// (certificationWindowOf).
	certificationWindow *videoEnvelope
}

// certificationMinFrameRate is the lowest frame rate the CSA camera
// certification scripts request. Mirrors matterjs-server
// packages/ws-controller/src/camera/streamPolicy.ts:176-177 at 0bd06f2.
const certificationMinFrameRate = 30

// certificationWindowOf is the window the CSA certification test scripts
// allocate (TC_AVSM, reference camera-controller): the codec's first
// trade-off point's bit rate as both minimum and maximum, frame rate 30 up
// to the maximum, and the full resolution range. A certified camera accepts
// it, so it is the search's last try. It is clamped into envelope, and its
// bit rate is held only against the caller's and the offer's bounds
// (minBitRate, maxBitRate). Mirrors matterjs-server
// packages/ws-controller/src/camera/streamPolicy.ts:179-198 at 0bd06f2.
func certificationWindowOf(envelope videoEnvelope, frameRateFloor int, point *rateDistortionPoint, minBitRate, maxBitRate float64) *videoEnvelope {
	if point == nil {
		return nil
	}
	if float64(point.minBitRate) < minBitRate || float64(point.minBitRate) > maxBitRate {
		return nil
	}
	minFrameRate := max(certificationMinFrameRate, frameRateFloor)
	if minFrameRate > envelope.maxFrameRate {
		return nil
	}
	w := envelope
	w.minFrameRate, w.minBitRate, w.maxBitRate = minFrameRate, point.minBitRate, point.minBitRate
	return &w
}

// planFrameRate is the highest frame rate the plan allows at r: a smaller
// frame lets the offer decode more of them. Mirrors matterjs-server
// packages/ws-controller/src/camera/streamPolicy.ts:200-204 at 0bd06f2.
func planFrameRate(plan *videoPlan, r resolution) int {
	if offer, ok := offerFrameRateCeiling(plan.limits, r); ok {
		return min(plan.frameRateCeiling, offer)
	}
	return plan.frameRateCeiling
}

// bitRateFloor is the bit rate the trade-off point at or just below r
// states (§11.2.6.8). Below every point, the lowest bit rate any point
// states: no point speaks for that size, and a larger size's floor would
// refuse what the camera serves. A floor above maxBitRate is dropped (ok
// false) rather than pinning the minimum to the maximum. Mirrors
// matterjs-server packages/ws-controller/src/camera/streamPolicy.ts:206-227
// at 0bd06f2.
func bitRateFloor(points []rateDistortionPoint, r resolution, maxBitRate int) (int, bool) {
	var applicable *rateDistortionPoint
	for i := range points {
		p := &points[i]
		// The first of the largest points that fit, as a stable sort by
		// size, largest first, then [0] picks it.
		if p.resolution.fitsUnder(r) && (applicable == nil || p.resolution.pixels() > applicable.resolution.pixels()) {
			applicable = p
		}
	}
	floor, found := 0, false
	if applicable != nil {
		floor, found = applicable.minBitRate, true
	} else {
		for _, p := range points {
			if !found || p.minBitRate < floor {
				floor, found = p.minBitRate, true
			}
		}
	}
	if !found || floor > maxBitRate {
		return 0, false
	}
	return floor, true
}

// planBitRateFloor is the lowest bit rate a window with this maxResolution
// asks for: the plan's own floor when no trade-off point gives one (the
// caller states min_bit_rate, the camera states no point, or the point's
// bit rate is above the plan's start). Mirrors matterjs-server
// packages/ws-controller/src/camera/streamPolicy.ts:229-236 at 0bd06f2.
func planBitRateFloor(plan *videoPlan, r resolution) int {
	if floor, ok := bitRateFloor(plan.bitRatePoints, r, plan.envelope.maxBitRate); ok {
		return floor
	}
	return plan.envelope.minBitRate
}

// videoSelection is computeVideoEnvelope's answer: a plan, or the caller
// bound no window can meet; its limit is the ceiling in force after every
// narrowing (sensor, offer or caller). Mirrors matterjs-server
// packages/ws-controller/src/camera/streamPolicy.ts:238-246 at 0bd06f2.
type videoSelection struct {
	plan *videoPlan
	// unsatisfiable is set when plan is nil.
	unsatisfiable *IncompatibleBound
}

// computeVideoEnvelope is the best window both the camera and the offer
// allow. Resolution and bit rate are ranges, so the camera can adapt
// (§11.2.1.2.2). Frame rate is one value, the highest the caller's bounds
// allow, or at most defaultMaxFrameRate without a caller ceiling: cameras
// refuse a range reaching below a frame rate floor they do not publish. A
// caller-stated floor out of reach fails the request; server-derived floors
// (viewport minimum, trade-off point bit rate) are clamped down instead.
// Mirrors matterjs-server packages/ws-controller/src/camera/streamPolicy.ts:252-371
// at 0bd06f2.
func computeVideoEnvelope(args videoEnvelopeArgs) videoSelection {
	caps, limits := args.capabilities, args.limits
	var hints videoHints
	if args.hints != nil {
		hints = *args.hints
	}
	allowedFrameRate, frameRateCeiling := callerFrameRates(caps.maxFrameRate, &hints)

	// Apply the caller's ceiling before the offer's pixel budget, or the
	// budget is spent on dimensions the ceiling then cuts again.
	maxResolution := caps.sensor
	if hints.maxResolution != (resolution{}) {
		maxResolution = clampDown(maxResolution, hints.maxResolution)
	}
	if limits.maxPixels.set {
		maxResolution = scaleToPixels(maxResolution, limits.maxPixels.value)
	}
	if limits.maxPixelsPerSecond.set {
		// At 1 fps minimum, the pixel-rate budget is also a frame-size
		// ceiling.
		maxResolution = scaleToPixels(maxResolution, limits.maxPixelsPerSecond.value)
	}
	underOffer := func(rate int) int {
		if offer, ok := offerFrameRateCeiling(limits.videoCodecLimits, maxResolution); ok {
			return min(rate, offer)
		}
		return rate
	}
	maxFrameRate := underOffer(frameRateCeiling)
	allowedFrameRateAtSize := underOffer(allowedFrameRate)

	if hints.minResolution != (resolution{}) && !hints.minResolution.fitsUnder(maxResolution) {
		return videoSelection{unsatisfiable: &IncompatibleBound{Field: BoundMinResolution, Requested: hints.minResolution.String(), Limit: maxResolution.String()}}
	}
	if hints.minFrameRate != 0 && hints.minFrameRate > maxFrameRate {
		return videoSelection{unsatisfiable: &IncompatibleBound{Field: BoundMinFrameRate, Requested: strconv.Itoa(hints.minFrameRate), Limit: strconv.Itoa(maxFrameRate)}}
	}

	var codecPoints []rateDistortionPoint
	for _, p := range caps.rateDistortionPoints {
		if p.codec == limits.codec {
			codecPoints = append(codecPoints, p)
		}
	}
	minResolution := minResolutionOf(caps, codecPoints, maxResolution, hints.minResolution)
	minBitRate, maxBitRate, bound := bitRatesOf(caps, limits, &hints, codecPoints, maxResolution)
	if bound != nil {
		return videoSelection{unsatisfiable: bound}
	}

	envelope := videoEnvelope{
		overlays:         args.overlays,
		codec:            limits.codec,
		minResolution:    minResolution,
		maxResolution:    maxResolution,
		minFrameRate:     maxFrameRate,
		maxFrameRate:     maxFrameRate,
		minBitRate:       minBitRate,
		maxBitRate:       maxBitRate,
		keyFrameInterval: keyFrameIntervalMS,
	}
	frameRateFloor := hints.minFrameRate
	if frameRateFloor == 0 {
		frameRateFloor = min(1, maxFrameRate)
	}
	plan := &videoPlan{
		envelope:         envelope,
		frameRateFloor:   frameRateFloor,
		frameRateCeiling: frameRateCeiling,
		allowedFrameRate: allowedFrameRateAtSize,
		limits:           limits.videoCodecLimits,
	}
	if hints.minBitRate == 0 {
		plan.bitRatePoints = codecPoints
	}
	plan.certificationWindow = planCertificationWindow(envelope, allowedFrameRateAtSize, frameRateFloor, codecPoints, limits, &hints)
	return videoSelection{plan: plan}
}

// callerFrameRates are the highest rate the camera and the caller allow and
// the plan's frame rate ceiling: the caller's max_frame_rate, or without
// one at most defaultMaxFrameRate (raised to the caller's min_frame_rate).
// Part of computeVideoEnvelope (matterjs-server
// packages/ws-controller/src/camera/streamPolicy.ts:263-271 at 0bd06f2).
func callerFrameRates(cameraMax int, hints *videoHints) (allowed, ceiling int) {
	if hints.maxFrameRate != 0 {
		allowed = min(cameraMax, hints.maxFrameRate)
		return allowed, allowed
	}
	return cameraMax, min(cameraMax, max(defaultMaxFrameRate, hints.minFrameRate))
}

// minResolutionOf is the window's floor: the viewport minimum, else the
// smallest trade-off point, else the ceiling, clamped under the ceiling and
// raised to the caller's floor: a part of the envelope computation, as
// matterjs-server packages/ws-controller/src/camera/streamPolicy.ts:307-316
// at 0bd06f2 has it.
func minResolutionOf(caps videoCapabilities, codecPoints []rateDistortionPoint, maxResolution, callerFloor resolution) resolution {
	derivedFloor := maxResolution
	switch {
	case caps.minViewport != (resolution{}):
		derivedFloor = caps.minViewport
	case len(codecPoints) > 0:
		derivedFloor = smallestPointResolution(codecPoints)
	}
	derivedFloor = clampDown(derivedFloor, maxResolution)
	if callerFloor != (resolution{}) {
		return clampUp(derivedFloor, callerFloor)
	}
	return derivedFloor
}

// bitRatesOf is the window's bit rate range, or the caller floor above
// every stated ceiling. MaxNetworkBandwidth is a link capacity, not an
// encoder limit, so it may lower the default start but never raise it;
// only a caller-stated ceiling or floor sets the start above the default.
// It is a part of the envelope computation, as matterjs-server
// packages/ws-controller/src/camera/streamPolicy.ts:318-334 at 0bd06f2 has
// it.
func bitRatesOf(caps videoCapabilities, limits selectedVideoCodecLimits, hints *videoHints, codecPoints []rateDistortionPoint, maxResolution resolution) (minBitRate, maxBitRate int, bound *IncompatibleBound) {
	bitRateCeiling := ceiling{}
	if hints.maxBitRate != 0 {
		bitRateCeiling = stated(float64(hints.maxBitRate))
	}
	bitRateCeiling = bitRateCeiling.tighter(limits.maxBitRate).tighter(caps.maxNetworkBandwidth)
	if hints.minBitRate != 0 && bitRateCeiling.set && float64(hints.minBitRate) > bitRateCeiling.value {
		return 0, 0, &IncompatibleBound{Field: BoundMinBitRate, Requested: strconv.Itoa(hints.minBitRate), Limit: formatNumber(bitRateCeiling.value)}
	}
	start := float64(defaultMaxBitRate)
	if bitRateCeiling.set {
		start = bitRateCeiling.value
	}
	if hints.maxBitRate == 0 {
		start = math.Min(start, defaultMaxBitRate)
	}
	maxBitRate = max(floorInt(start), hints.minBitRate)
	minBitRate = hints.minBitRate
	if minBitRate == 0 {
		minBitRate = 1
		if floor, ok := bitRateFloor(codecPoints, maxResolution, maxBitRate); ok {
			minBitRate = floor
		}
	}
	return minBitRate, maxBitRate, nil
}

// planCertificationWindow is the plan's last rung: a range the camera
// chooses from, so it keeps the full allowed rate rather than the default,
// held against the caller's and the offer's bit rate bounds. It is a part
// of the envelope computation, as matterjs-server
// packages/ws-controller/src/camera/streamPolicy.ts:356-368 at 0bd06f2 has
// it.
func planCertificationWindow(envelope videoEnvelope, allowedFrameRate, frameRateFloor int, codecPoints []rateDistortionPoint, limits selectedVideoCodecLimits, hints *videoHints) *videoEnvelope {
	certificationRange := envelope
	certificationRange.maxFrameRate = allowedFrameRate
	var firstPoint *rateDistortionPoint
	if len(codecPoints) > 0 {
		firstPoint = &codecPoints[0]
	}
	maxBitRate := math.Inf(1)
	if hints.maxBitRate != 0 {
		maxBitRate = float64(hints.maxBitRate)
	}
	if limits.maxBitRate.set {
		maxBitRate = math.Min(maxBitRate, limits.maxBitRate.value)
	}
	return certificationWindowOf(certificationRange, frameRateFloor, firstPoint, float64(hints.minBitRate), maxBitRate)
}

// smallestPointResolution is the smallest trade-off point's resolution, the
// first of equals.
func smallestPointResolution(points []rateDistortionPoint) resolution {
	smallest := points[0].resolution
	for _, p := range points[1:] {
		if p.resolution.pixels() < smallest.pixels() {
			smallest = p.resolution
		}
	}
	return smallest
}

// formatNumber spells a number as JavaScript's String does for the values
// a bound can carry: whole numbers without a fraction.
func formatNumber(v float64) string {
	if v == math.Trunc(v) && math.Abs(v) < 1e21 {
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	return strconv.FormatFloat(v, 'g', -1, 64)
}

// withFrameRate asks env at a single frame rate: cameras refuse a range
// reaching below a floor they do not publish. Mirrors matterjs-server
// packages/ws-controller/src/camera/streamPolicy.ts:373-376 at 0bd06f2.
func withFrameRate(env videoEnvelope, frameRate int) videoEnvelope {
	env.minFrameRate, env.maxFrameRate = frameRate, frameRate
	return env
}

// halveResolution halves each side (a quarter of the pixels), which is how
// encoders step resolution down, never below floor on either side. Mirrors
// matterjs-server packages/ws-controller/src/camera/streamPolicy.ts:378-384
// at 0bd06f2.
func halveResolution(r, floor resolution) resolution {
	return clampUp(scaleToPixels(r, float64(r.pixels())/4), floor)
}

// sameVideoEnvelope reports whether two windows are equal in every field a
// search window can differ in, the certification window's ranges included.
// Mirrors matterjs-server packages/ws-controller/src/camera/streamPolicy.ts:386-396
// at 0bd06f2.
func sameVideoEnvelope(a, b videoEnvelope) bool {
	return a.minBitRate == b.minBitRate &&
		a.maxBitRate == b.maxBitRate &&
		a.minFrameRate == b.minFrameRate &&
		a.maxFrameRate == b.maxFrameRate &&
		a.maxResolution == b.maxResolution
}

// videoPixelRateBudget is the camera's encoded-pixel budget and the streams
// that spend it; maxEncodedPixelRate unset when the camera states none.
// Mirrors matterjs-server packages/ws-controller/src/camera/streamPolicy.ts:402-406
// at 0bd06f2.
type videoPixelRateBudget struct {
	maxEncodedPixelRate ceiling
	videoStreams        []allocatedVideoStream
	snapshotStreams     []allocatedSnapshotStream
}

// encodedPixelRate is what MaxEncodedPixelRate (§11.2.7.2) charges for a
// stream allocated with this window. Mirrors matterjs-server
// packages/ws-controller/src/camera/streamPolicy.ts:398-411 at 0bd06f2.
func encodedPixelRate(window videoEnvelope) int {
	return window.maxResolution.pixels() * window.maxFrameRate
}

// freeEncodedPixelRate is what is left of MaxEncodedPixelRate after the
// camera's streams, ok false for a camera that states no budget. The spec
// states no per-stream formula; this is the reference server's accounting
// (CameraAVStreamManagementCluster.cpp,
// IsResourceAvailableForStreamAllocation). Mirrors matterjs-server
// packages/ws-controller/src/camera/streamPolicy.ts:413-427 at 0bd06f2.
func freeEncodedPixelRate(budget videoPixelRateBudget) (float64, bool) {
	if !budget.maxEncodedPixelRate.set {
		return 0, false
	}
	committed := 0
	for _, s := range budget.videoStreams {
		committed += s.maxResolution.pixels() * s.maxFrameRate
	}
	for _, s := range budget.snapshotStreams {
		if s.encodedPixels {
			committed += s.maxResolution.pixels() * s.frameRate
		}
	}
	return budget.maxEncodedPixelRate.value - float64(committed), true
}

// contains reports whether inner lies inside outer. Mirrors matterjs-server
// packages/ws-controller/src/camera/streamPolicy.ts:429-431 at 0bd06f2.
func contains(outerMin, outerMax, innerMin, innerMax int) bool {
	return innerMin >= outerMin && innerMax <= outerMax
}

// resolutionContains is contains per dimension: areas would ignore aspect
// ratio. Mirrors matterjs-server
// packages/ws-controller/src/camera/streamPolicy.ts:433-444 at 0bd06f2.
func resolutionContains(outerMin, outerMax, innerMin, innerMax resolution) bool {
	return innerMin.width >= outerMin.width &&
		innerMin.height >= outerMin.height &&
		innerMax.width <= outerMax.width &&
		innerMax.height <= outerMax.height
}

// satisfiesVideoCallerBounds holds a stream against the caller's bounds:
// hard at the reuse and degraded rungs; the allocate rungs carry these
// bounds in the envelope. Mirrors matterjs-server
// packages/ws-controller/src/camera/streamPolicy.ts:446-462 at 0bd06f2.
func satisfiesVideoCallerBounds(c *allocatedVideoStream, b *videoCallerBounds) bool {
	limits := b.limits
	switch {
	case c.videoCodec != limits.codec,
		!overlaysMatch(c.overlays, b.overlays),
		limits.maxPixels.set && float64(c.maxResolution.pixels()) > limits.maxPixels.value:
		return false
	}
	if offer, ok := offerFrameRateCeiling(limits.videoCodecLimits, c.maxResolution); ok && c.maxFrameRate > offer {
		return false
	}
	switch {
	case limits.maxBitRate.set && float64(c.maxBitRate) > limits.maxBitRate.value,
		b.minResolution != (resolution{}) && !b.minResolution.fitsUnder(c.minResolution),
		b.maxResolution != (resolution{}) && !c.maxResolution.fitsUnder(b.maxResolution),
		b.minFrameRate != 0 && c.minFrameRate < b.minFrameRate,
		b.maxFrameRate != 0 && c.maxFrameRate > b.maxFrameRate,
		b.minBitRate != 0 && c.minBitRate < b.minBitRate,
		b.maxBitRate != 0 && c.maxBitRate > b.maxBitRate:
		return false
	}
	return true
}

// fitsPlan reports whether a stream can stand in for the plan: it must
// reach frameRate, the rate an allocate would ask first, and may go down to
// the plan's floor, so a slow stream never stands in for a better one the
// request can get. Without a frameRate (hasRate false) an allocate could
// only succeed by freeing room or if the camera accepts a window the budget
// said would not fit, so any rate within the plan does. Mirrors
// matterjs-server packages/ws-controller/src/camera/streamPolicy.ts:464-485
// at 0bd06f2.
func fitsPlan(c *allocatedVideoStream, plan *videoPlan, frameRate int, hasRate bool) bool {
	env := plan.envelope
	return (!hasRate || c.maxFrameRate >= frameRate) &&
		overlaysMatch(c.overlays, env.overlays) &&
		resolutionContains(env.minResolution, env.maxResolution, c.minResolution, c.maxResolution) &&
		contains(plan.frameRateFloor, plan.allowedFrameRate, c.minFrameRate, c.maxFrameRate) &&
		fitsPlanBitRate(c, plan)
}

// fitsPlanBitRate: inside the bit rate range a window of the candidate's
// size asks, or the certification window's. Mirrors matterjs-server
// packages/ws-controller/src/camera/streamPolicy.ts:487-496 at 0bd06f2.
func fitsPlanBitRate(c *allocatedVideoStream, plan *videoPlan) bool {
	if contains(planBitRateFloor(plan, c.maxResolution), plan.envelope.maxBitRate, c.minBitRate, c.maxBitRate) {
		return true
	}
	cert := plan.certificationWindow
	return cert != nil && contains(cert.minBitRate, cert.maxBitRate, c.minBitRate, c.maxBitRate)
}

// usageRank is 0 for a stream of the preferred usage, else 1: the session's
// own usage is the first preference (§11.2.1.2.1); hasPreferred false
// prefers none. Mirrors matterjs-server
// packages/ws-controller/src/camera/streamPolicy.ts:538-541 at 0bd06f2.
func usageRank(usage, preferred csdef.StreamUsageEnum, hasPreferred bool) int {
	if hasPreferred && usage == preferred {
		return 0
	}
	return 1
}

// preferredUsage is an optional stream usage.
type preferredUsage struct {
	usage csdef.StreamUsageEnum
	set   bool
}

// findReusableVideoStream is the stream a request can reuse instead of
// allocating: its range must lie inside the plan's, not merely overlap it.
// firstFrameRate is the frame rate of the first window within the encoder
// budget an allocate would ask if the candidate did not exist (ok false
// when none fits): reusing it is free, so its own reservation must not
// count against the alternative. Then a running stream, since starting an
// encoder is the expensive part. Mirrors matterjs-server
// packages/ws-controller/src/camera/streamPolicy.ts:498-520 at 0bd06f2.
func findReusableVideoStream(
	streams []allocatedVideoStream,
	plan *videoPlan,
	bounds *videoCallerBounds,
	firstFrameRate func(*allocatedVideoStream) (int, bool),
	preferred preferredUsage,
) *allocatedVideoStream {
	var candidates []*allocatedVideoStream
	for i := range streams {
		c := &streams[i]
		if !satisfiesVideoCallerBounds(c, bounds) {
			continue
		}
		if rate, ok := firstFrameRate(c); fitsPlan(c, plan, rate, ok) {
			candidates = append(candidates, c)
		}
	}
	slices.SortStableFunc(candidates, func(a, b *allocatedVideoStream) int {
		if d := usageRank(a.streamUsage, preferred.usage, preferred.set) - usageRank(b.streamUsage, preferred.usage, preferred.set); d != 0 {
			return d
		}
		return b.referenceCount - a.referenceCount
	})
	if len(candidates) == 0 {
		return nil
	}
	return candidates[0]
}

// findDegradedVideoStream is the stream for when nothing can be allocated
// (degraded): it ignores the envelope and keeps the caller's bounds, then
// takes the most capable stream, since this rung is already a compromise.
// Mirrors matterjs-server packages/ws-controller/src/camera/streamPolicy.ts:522-536
// at 0bd06f2.
func findDegradedVideoStream(streams []allocatedVideoStream, bounds *videoCallerBounds, preferred preferredUsage) *allocatedVideoStream {
	var candidates []*allocatedVideoStream
	for i := range streams {
		if satisfiesVideoCallerBounds(&streams[i], bounds) {
			candidates = append(candidates, &streams[i])
		}
	}
	slices.SortStableFunc(candidates, func(a, b *allocatedVideoStream) int {
		if d := usageRank(a.streamUsage, preferred.usage, preferred.set) - usageRank(b.streamUsage, preferred.usage, preferred.set); d != 0 {
			return d
		}
		return b.maxResolution.pixels() - a.maxResolution.pixels()
	})
	if len(candidates) == 0 {
		return nil
	}
	return candidates[0]
}

// defaultAudioBitRate is the audio bit rate when the caller states none.
// Mirrors matterjs-server packages/ws-controller/src/camera/streamPolicy.ts:543
// at 0bd06f2.
const defaultAudioBitRate = 64000

// audioCapabilities are what the camera states about its audio. Mirrors
// matterjs-server packages/ws-controller/src/camera/streamPolicy.ts:545-550
// at 0bd06f2.
type audioCapabilities struct {
	supportedCodecs      []csdef.AudioCodecEnum
	maxNumberOfChannels  int
	supportedSampleRates []int
	supportedBitDepths   []int
}

// audioHints are exact values, each a hard bound; 0 (and nil codecs) is
// unstated. Codec names are as SDP rtpmap advertises them, e.g. "OPUS".
// Mirrors matterjs-server packages/ws-controller/src/camera/streamPolicy.ts:552-559
// at 0bd06f2.
type audioHints struct {
	codecs       []string
	channelCount int
	sampleRate   int
	bitRate      int
}

// satisfiesAudioCallerBounds holds an audio stream against the caller's
// exact values. Mirrors matterjs-server
// packages/ws-controller/src/camera/streamPolicy.ts:561-567 at 0bd06f2.
func satisfiesAudioCallerBounds(c *allocatedAudioStream, b *audioHints) bool {
	switch {
	case b.codecs != nil && !slices.Contains(b.codecs, AudioCodecName(c.audioCodec)),
		b.channelCount != 0 && c.channelCount != b.channelCount,
		b.sampleRate != 0 && c.sampleRate != b.sampleRate,
		b.bitRate != 0 && c.bitRate != b.bitRate:
		return false
	}
	return true
}

// audioSelection is computeAudioEnvelope's answer: an envelope (nil when no
// codec is left; the caller decides whether that is a failure), or the
// caller bound the camera cannot meet, whose limit is what the device
// states: its ceiling, or the set of values it accepts. Mirrors
// matterjs-server packages/ws-controller/src/camera/streamPolicy.ts:575-584
// at 0bd06f2.
type audioSelection struct {
	envelope      *audioEnvelope
	unsatisfiable *IncompatibleBound
}

// computeAudioEnvelope is the audio window to allocate. Mirrors
// matterjs-server packages/ws-controller/src/camera/streamPolicy.ts:586-628
// at 0bd06f2.
func computeAudioEnvelope(caps audioCapabilities, sdp *sdpVideoConstraints, hints *audioHints) audioSelection {
	var h audioHints
	if hints != nil {
		h = *hints
	}
	// Checked before codec narrowing, so the caller hears about the value
	// it can change.
	if h.sampleRate != 0 && !slices.Contains(caps.supportedSampleRates, h.sampleRate) {
		rates := make([]string, len(caps.supportedSampleRates))
		for i, r := range caps.supportedSampleRates {
			rates[i] = strconv.Itoa(r)
		}
		return audioSelection{unsatisfiable: &IncompatibleBound{Field: BoundSampleRate, Requested: strconv.Itoa(h.sampleRate), Limit: strings.Join(rates, ", ")}}
	}
	if h.channelCount != 0 && h.channelCount > caps.maxNumberOfChannels {
		return audioSelection{unsatisfiable: &IncompatibleBound{Field: BoundChannelCount, Requested: strconv.Itoa(h.channelCount), Limit: strconv.Itoa(caps.maxNumberOfChannels)}}
	}

	codecs := caps.supportedCodecs
	if sdp != nil {
		if offered := receivableCodecs(sdp.audio); offered != nil {
			codecs = slices.DeleteFunc(slices.Clone(codecs), func(c csdef.AudioCodecEnum) bool {
				return !slices.Contains(offered, AudioCodecName(c))
			})
		}
	}
	if h.codecs != nil {
		codecs = slices.DeleteFunc(slices.Clone(codecs), func(c csdef.AudioCodecEnum) bool {
			return !slices.Contains(h.codecs, AudioCodecName(c))
		})
	}
	if len(codecs) == 0 {
		return audioSelection{}
	}
	env := audioEnvelope{
		codec:        codecs[0],
		channelCount: h.channelCount,
		sampleRate:   h.sampleRate,
		bitRate:      h.bitRate,
		bitDepth:     jsMax(caps.supportedBitDepths),
	}
	if env.channelCount == 0 {
		env.channelCount = caps.maxNumberOfChannels
	}
	if env.sampleRate == 0 {
		env.sampleRate = jsMax(caps.supportedSampleRates)
	}
	if env.bitRate == 0 {
		env.bitRate = defaultAudioBitRate
	}
	return audioSelection{envelope: &env}
}

// jsMax is Math.max over values; an empty list, which matterjs-server's
// Math.max reads as -Infinity, is 0 here, a value no allocation accepts
// either.
func jsMax(values []int) int {
	if len(values) == 0 {
		return 0
	}
	return slices.Max(values)
}

// chooseEvictionVictim is the stream to deallocate to make room: candidates
// are what VideoStreamDeallocate (§11.2.8.7.2) accepts, unreferenced and
// not Internal, whoever allocated them. This controller's own streams come
// first, then the lowest in StreamUsagePriorities (§11.2.7.19, highest
// first); a usage missing from priorities is taken last within its
// ownership group. Mirrors matterjs-server
// packages/ws-controller/src/camera/streamPolicy.ts:630-645 at 0bd06f2.
func chooseEvictionVictim(streams []allocatedVideoStream, priorities []csdef.StreamUsageEnum, ours func(*allocatedVideoStream) bool) *allocatedVideoStream {
	var candidates []*allocatedVideoStream
	for i := range streams {
		s := &streams[i]
		if s.referenceCount == 0 && s.streamUsage != csdef.StreamUsageInternal {
			candidates = append(candidates, s)
		}
	}
	ownRank := func(s *allocatedVideoStream) int {
		if ours(s) {
			return 1
		}
		return 0
	}
	slices.SortStableFunc(candidates, func(a, b *allocatedVideoStream) int {
		if d := ownRank(b) - ownRank(a); d != 0 {
			return d
		}
		return slices.Index(priorities, b.streamUsage) - slices.Index(priorities, a.streamUsage)
	})
	if len(candidates) == 0 {
		return nil
	}
	return candidates[0]
}
