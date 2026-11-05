// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"math"
	"slices"

	csdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavstreammanagement"
)

// snapshotQuality is the Quality of every SnapshotStreamAllocate, which the
// command makes mandatory (§11.2.8.8); 90 is what the reference SDK's tests
// and camera app use. Mirrors matterjs-server
// packages/ws-controller/src/camera/snapshotPolicy.ts:11-12 at 0bd06f2.
const snapshotQuality = 90

// snapshotCapability is one SnapshotCapabilities entry;
// requiresHardwareEncoder is optional on the wire and absent reads as
// false, as in the reference server. Mirrors matterjs-server
// packages/ws-controller/src/camera/snapshotPolicy.ts:14-21 at 0bd06f2.
type snapshotCapability struct {
	resolution              resolution
	maxFrameRate            int
	imageCodec              csdef.ImageCodecEnum
	requiresEncodedPixels   bool
	requiresHardwareEncoder bool
}

// reachesTarget reports whether r reaches target in every stated
// dimension. Mirrors matterjs-server
// packages/ws-controller/src/camera/snapshotPolicy.ts:31-36 at 0bd06f2.
func reachesTarget(r resolution, target resolutionTarget) bool {
	return (target.width == 0 || r.width >= target.width) && (target.height == 0 || r.height >= target.height)
}

// orderForTarget orders items for a snapshot target, target nil for none.
// Without a target, largest first. With one: the smallest entry that
// reaches the target in every stated dimension, then the larger ones, then
// the entries that fall short, largest first. A larger source still serves
// the target because the camera may downscale it (§11.2.8.13.3); it only
// costs the camera more. Mirrors matterjs-server
// packages/ws-controller/src/camera/snapshotPolicy.ts:38-53 at 0bd06f2.
func orderForTarget[T any](items []T, resolutionOf func(T) resolution, target *resolutionTarget) []T {
	largestFirst := func(a, b T) int { return resolutionOf(b).pixels() - resolutionOf(a).pixels() }
	if target == nil {
		out := slices.Clone(items)
		slices.SortStableFunc(out, largestFirst)
		return out
	}
	var reaching, short []T
	for _, item := range items {
		if reachesTarget(resolutionOf(item), *target) {
			reaching = append(reaching, item)
		} else {
			short = append(short, item)
		}
	}
	slices.SortStableFunc(reaching, func(a, b T) int { return largestFirst(b, a) })
	slices.SortStableFunc(short, largestFirst)
	return append(reaching, short...)
}

// jsRound is JavaScript's Math.round: halves round up.
func jsRound(v float64) int { return floorInt(v + 0.5) }

// requestedResolution is the RequestedResolution (§11.2.8.13.2) for a
// source of size available: the target fitted inside the source with the
// source's aspect ratio, so a stated width alone fixes the height; never
// larger than available, and available itself without a target. Mirrors
// matterjs-server packages/ws-controller/src/camera/snapshotPolicy.ts:55-71
// at 0bd06f2.
func requestedResolution(available resolution, target *resolutionTarget) resolution {
	if target == nil {
		return available
	}
	scale := 1.0
	if target.width != 0 {
		scale = math.Min(scale, float64(target.width)/float64(available.width))
	}
	if target.height != 0 {
		scale = math.Min(scale, float64(target.height)/float64(available.height))
	}
	return resolution{
		width:  max(1, jsRound(float64(available.width)*scale)),
		height: max(1, jsRound(float64(available.height)*scale)),
	}
}

// usesHardwareEncoder: RequiresHardwareEncoder "is only considered if
// RequiresEncodedPixels is true" (§11.2.6.9.5). Mirrors matterjs-server
// packages/ws-controller/src/camera/snapshotPolicy.ts:73-76 at 0bd06f2.
func usesHardwareEncoder(c snapshotCapability) bool {
	return c.requiresEncodedPixels && c.requiresHardwareEncoder
}

// encodersExhausted reports whether the camera's encoders are all taken,
// maxConcurrentEncoders unset when the camera states none. Callers include
// streams allocated but not yet reported. The reference server counts every
// allocated video stream (IsResourceAvailableForStreamAllocation), not
// only referenced ones; this result only orders the snapshot ladder, so an
// undercount costs one device refusal. Mirrors matterjs-server
// packages/ws-controller/src/camera/snapshotPolicy.ts:78-94 at 0bd06f2.
func encodersExhausted(maxConcurrentEncoders ceiling, videoStreams []allocatedVideoStream, snapshotStreams []allocatedSnapshotStream) bool {
	taken := 0
	for _, s := range videoStreams {
		if s.referenceCount > 0 {
			taken++
		}
	}
	for _, s := range snapshotStreams {
		if s.hardwareEncoder {
			taken++
		}
	}
	if !maxConcurrentEncoders.set {
		return taken > 0
	}
	return float64(taken) >= maxConcurrentEncoders.value
}

// snapshotSelection is selectSnapshotCapabilities's answer: the
// capabilities to try in order and the best within the caller's bounds,
// taken before the encoder reordering (nil only when the camera advertises
// no snapshot capability); or unsatisfiable, ReasonCodec or ReasonBounds.
// Mirrors matterjs-server packages/ws-controller/src/camera/snapshotPolicy.ts:96-102
// at 0bd06f2.
type snapshotSelection struct {
	capabilities           []snapshotCapability
	bestWithinCallerBounds *snapshotCapability
	unsatisfiable          IncompatibleReason
}

// isDegradedFrom reports whether chosen, the returned frame's size (not the
// stream's ceiling: the device picks a size inside the range), is degraded
// against best. With a target, a frame is degraded when, in every stated
// dimension, it is smaller than the target or than best, whichever is
// smaller. Stated dimensions only: the stream that served the frame can have
// another aspect ratio than best. Mirrors matterjs-server
// packages/ws-controller/src/camera/snapshotPolicy.ts:104-123 at 0bd06f2.
func isDegradedFrom(chosen resolution, best *snapshotCapability, target *resolutionTarget) bool {
	if best == nil {
		return false
	}
	if target == nil {
		return chosen.pixels() < best.resolution.pixels()
	}
	short := func(frame, wanted, available int) bool { return wanted == 0 || frame < min(wanted, available) }
	return short(chosen.width, target.width, best.resolution.width) &&
		short(chosen.height, target.height, best.resolution.height)
}

// chooseSnapshotStreamToFree is the snapshot stream to deallocate for room,
// maxEncodedPixelRate unset when the camera states no budget. CaptureSnapshot
// does not raise ReferenceCount, so a stream another controller polls can
// read 0 (§11.2.8.10.2); the caller decides whose streams are candidates.
// frameRate × maxResolution is the reference server's measure of pixel
// rate. Mirrors matterjs-server
// packages/ws-controller/src/camera/snapshotPolicy.ts:125-146 at 0bd06f2.
func chooseSnapshotStreamToFree(candidates []allocatedSnapshotStream, maxEncodedPixelRate ceiling) *allocatedSnapshotStream {
	freesPixelRate := func(s *allocatedSnapshotStream) bool { return s.encodedPixels && maxEncodedPixelRate.set }
	footprint := func(s *allocatedSnapshotStream) int {
		if freesPixelRate(s) {
			return s.maxResolution.pixels() * s.frameRate
		}
		return 0
	}
	var eligible []*allocatedSnapshotStream
	for i := range candidates {
		s := &candidates[i]
		if s.referenceCount == 0 && (s.hardwareEncoder || freesPixelRate(s)) {
			eligible = append(eligible, s)
		}
	}
	hw := func(s *allocatedSnapshotStream) int {
		if s.hardwareEncoder {
			return 1
		}
		return 0
	}
	slices.SortStableFunc(eligible, func(a, b *allocatedSnapshotStream) int {
		if d := hw(b) - hw(a); d != 0 {
			return d
		}
		if d := footprint(b) - footprint(a); d != 0 {
			return d
		}
		return int(a.snapshotStreamID) - int(b.snapshotStreamID)
	})
	if len(eligible) == 0 {
		return nil
	}
	return eligible[0]
}

// guaranteedFrame is the smallest frame a capture with
// requestedResolution(stream.maxResolution, target) can return: the camera
// downscales to the requested size, but may answer with any native size in
// the stream's range (§11.2.8.13.3). Mirrors matterjs-server
// packages/ws-controller/src/camera/snapshotPolicy.ts:148-158 at 0bd06f2.
func guaranteedFrame(s *allocatedSnapshotStream, target *resolutionTarget) resolution {
	requested := requestedResolution(s.maxResolution, target)
	return resolution{width: min(requested.width, s.minResolution.width), height: min(requested.height, s.minResolution.height)}
}

// snapshotBounds are the caller's snapshot bounds: a zero maxResolution and
// a nil codec or target state none; overlays must be the resolved overlays,
// not the caller's statement.
type snapshotBounds struct {
	maxResolution resolution
	codec         *csdef.ImageCodecEnum
	overlays      overlayBounds
	target        *resolutionTarget
}

// findAdoptableSnapshotStream is the stream a snapshot can use instead of
// allocating one; best is the capability that would otherwise be
// allocated. A candidate is adopted when the frame it is sure to return
// for its own fitted request (guaranteedFrame) reaches the size best would
// give, in every stated dimension of the target (in both without one); such
// a frame is never isDegradedFrom best. With a target, the candidate
// closest above it comes first. Mirrors matterjs-server
// packages/ws-controller/src/camera/snapshotPolicy.ts:160-184 at 0bd06f2.
func findAdoptableSnapshotStream(streams []allocatedSnapshotStream, best snapshotCapability, bounds snapshotBounds) *allocatedSnapshotStream {
	needed := requestedResolution(best.resolution, bounds.target)
	checksWidth := bounds.target == nil || bounds.target.width != 0
	checksHeight := bounds.target == nil || bounds.target.height != 0
	var candidates []*allocatedSnapshotStream
	for i := range streams {
		s := &streams[i]
		switch {
		case bounds.codec != nil && s.imageCodec != *bounds.codec,
			!overlaysMatch(s.overlays, bounds.overlays),
			bounds.maxResolution != (resolution{}) && !s.maxResolution.fitsUnder(bounds.maxResolution):
			continue
		}
		frame := guaranteedFrame(s, bounds.target)
		if (!checksWidth || frame.width >= needed.width) && (!checksHeight || frame.height >= needed.height) {
			candidates = append(candidates, s)
		}
	}
	ordered := orderForTarget(candidates, func(s *allocatedSnapshotStream) resolution { return s.maxResolution }, bounds.target)
	if len(ordered) == 0 {
		return nil
	}
	return ordered[0]
}

// snapshotOptions are selectSnapshotCapabilities's request: a zero
// maxResolution and a nil codec or target state none.
type snapshotOptions struct {
	encodersExhausted bool
	maxResolution     resolution
	codec             *csdef.ImageCodecEnum
	target            *resolutionTarget
}

// selectSnapshotCapabilities orders the capabilities a snapshot may
// allocate from. Encoder-using capabilities stay in the list when encoders
// are exhausted: encodersExhausted reads a lagging view, so the device
// decides whether an encoder is really free. The encoder split comes first
// and the target order applies inside each part, so a target never moves
// an encoder-using capability ahead of an encoder-free one. Mirrors
// matterjs-server packages/ws-controller/src/camera/snapshotPolicy.ts:186-224
// at 0bd06f2.
func selectSnapshotCapabilities(capabilities []snapshotCapability, opts snapshotOptions) snapshotSelection {
	eligible := capabilities
	if opts.codec != nil {
		before := len(eligible)
		eligible = slices.DeleteFunc(slices.Clone(eligible), func(c snapshotCapability) bool { return c.imageCodec != *opts.codec })
		if len(eligible) == 0 && before > 0 {
			return snapshotSelection{unsatisfiable: ReasonCodec}
		}
	}
	if opts.maxResolution != (resolution{}) {
		before := len(eligible)
		eligible = slices.DeleteFunc(slices.Clone(eligible), func(c snapshotCapability) bool {
			return !c.resolution.fitsUnder(opts.maxResolution)
		})
		if len(eligible) == 0 && before > 0 {
			return snapshotSelection{unsatisfiable: ReasonBounds}
		}
	}
	preferred := orderForTarget(eligible, func(c snapshotCapability) resolution { return c.resolution }, opts.target)
	var best *snapshotCapability
	if len(preferred) > 0 {
		b := preferred[0]
		best = &b
	}
	if opts.encodersExhausted {
		free := slices.DeleteFunc(slices.Clone(preferred), usesHardwareEncoder)
		using := slices.DeleteFunc(slices.Clone(preferred), func(c snapshotCapability) bool { return !usesHardwareEncoder(c) })
		return snapshotSelection{capabilities: append(free, using...), bestWithinCallerBounds: best}
	}
	return snapshotSelection{capabilities: preferred, bestWithinCallerBounds: best}
}
