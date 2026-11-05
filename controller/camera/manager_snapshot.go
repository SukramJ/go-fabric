// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"context"
	"slices"
	"strconv"

	"github.com/SukramJ/go-fabric/cluster/spec"
	csdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavstreammanagement"
	"github.com/SukramJ/go-fabric/im"
)

// imageCodecName is an image codec's wire name. Mirrors matterjs-server
// packages/ws-controller/src/camera/wireNames.ts:64-66 at 0bd06f2.
func imageCodecName(codec csdef.ImageCodecEnum) string {
	switch codec {
	case csdef.ImageCodecJpeg:
		return "JPEG"
	case csdef.ImageCodecHeic:
		return "HEIC"
	default:
		return strconv.Itoa(int(codec))
	}
}

// snapshotArgs is one snapshot request: a zero maxResolution, a nil
// target and a nil codec state none.
type snapshotArgs struct {
	t             target
	maxResolution resolution
	target        *resolutionTarget
	codec         *csdef.ImageCodecEnum
	overlays      overlayBounds
}

// snapshotResult is one still frame and the stream it came from, which a
// successful call always leaves on the camera. degraded: without a target,
// the frame has fewer pixels than the best capability the caller's bounds
// allowed; with one, every stated side is smaller than the target and the
// capability it selects. Mirrors matterjs-server
// packages/ws-controller/src/camera/CameraStreamManager.ts:603-616 at
// 0bd06f2.
type snapshotResult struct {
	data       []byte
	codec      csdef.ImageCodecEnum
	resolution resolution
	degraded   bool
	streamID   uint16
	provenance Provenance
}

// captureArgs is one CaptureSnapshot. Mirrors matterjs-server
// packages/ws-controller/src/camera/CameraStreamManager.ts:593-601 at
// 0bd06f2.
type captureArgs struct {
	t               target
	state           *cameraState
	streamID        uint16
	requested       resolution
	deviceCodecs    []string
	requestedCodecs []string
}

// snapshotFailure is the typed error of a refused snapshot; a capacity
// refusal names every encoder holder. Mirrors matterjs-server
// packages/ws-controller/src/camera/CameraStreamManager.ts:2363-2393 at
// 0bd06f2.
func snapshotFailure(st *cameraState, status im.StatusCode, hasStatus bool, deviceCodecs, requestedCodecs []string) error {
	if ladderReactionOf(status, hasStatus) == reactCapacity {
		occupied := []OccupyingStream{}
		for _, s := range st.video {
			occupied = append(occupied, occupying(StreamVideo, s.videoStreamID, s.referenceCount))
		}
		return exhausted(st, append(occupied, encoderHoldingSnapshotStreams(st.snapshot)...))
	}
	e := &IncompatibleError{Reason: ReasonBounds, Device: deviceCodecs, Requested: requestedCodecs}
	if hasStatus {
		e.DeviceStatus = &status
	}
	return e
}

// snapshot captures one still frame from a snapshot stream the camera
// keeps: it adopts a reported stream within the caller's bounds, whoever
// allocated it, or allocates one and leases it. A successful call never
// deallocates. Mirrors matterjs-server
// packages/ws-controller/src/camera/CameraStreamManager.ts:2389-2649 at
// 0bd06f2.
func (m *manager) snapshot(ctx context.Context, args snapshotArgs) (*snapshotResult, error) { //nolint:gocognit,funlen // one function, as the source is: split, the order of its rungs would hide
	t := args.t
	return withEndpointLock(ctx, m, t, func() (*snapshotResult, error) {
		return withAllocationScope(ctx, m, func(scope *allocationScope) (*snapshotResult, error) {
			st, err := m.requireState(ctx, t)
			if err != nil {
				return nil, err
			}
			requestedCodecs := []string{}
			if args.codec != nil {
				requestedCodecs = []string{imageCodecName(*args.codec)}
			}
			if lacksFeature(st.features, csdef.FeatureSnapshot) {
				return nil, &IncompatibleError{Reason: ReasonFeature, Feature: featureName(csdef.FeatureSnapshot), Device: []string{}, Requested: requestedCodecs}
			}
			sel := resolveOverlays(args.overlays, supportOf(st.features))
			if sel.unsupported != 0 {
				return nil, &IncompatibleError{Reason: ReasonFeature, Feature: featureName(sel.unsupported), Device: []string{}, Requested: requestedCodecs}
			}
			overlays := sel.overlays
			// Unreported allocations count, so a stream allocated moments ago
			// is not missed.
			selection := selectSnapshotCapabilities(st.snapshotCapabilities, snapshotOptions{
				encodersExhausted: encodersExhausted(st.maxConcurrentEncoders,
					append(slices.Clone(st.video), m.unreportedVideoStreams(t, st.video)...),
					append(slices.Clone(st.snapshot), m.unreportedSnapshotStreams(t, st.snapshot)...)),
				maxResolution: args.maxResolution, codec: args.codec, target: args.target,
			})
			deviceCodecs := []string{}
			for _, c := range st.snapshotCapabilities {
				if name := imageCodecName(c.imageCodec); !slices.Contains(deviceCodecs, name) {
					deviceCodecs = append(deviceCodecs, name)
				}
			}
			if selection.unsatisfiable != "" {
				return nil, &IncompatibleError{Reason: selection.unsatisfiable, Device: deviceCodecs, Requested: requestedCodecs}
			}
			candidates, bestWithin := selection.capabilities, selection.bestWithinCallerBounds
			if len(candidates) == 0 {
				// Only reachable when the camera advertises no snapshot
				// capability.
				return nil, &IncompatibleError{Reason: ReasonCapability, Device: deviceCodecs, Requested: requestedCodecs}
			}
			best := candidates[0]
			known := append(slices.Clone(st.snapshot), m.unreportedSnapshotStreams(t, st.snapshot)...)
			if adopted := findAdoptableSnapshotStream(st.snapshot, best, snapshotBounds{maxResolution: args.maxResolution, codec: args.codec, target: args.target, overlays: overlays}); adopted != nil {
				a := *adopted
				captured, err := m.captureAdoptedSnapshot(ctx, captureArgs{t: t, state: st, streamID: a.snapshotStreamID, requested: requestedResolution(a.maxResolution, args.target), deviceCodecs: deviceCodecs, requestedCodecs: requestedCodecs})
				if err != nil {
					return nil, err
				}
				if captured != nil {
					captured.degraded = isDegradedFrom(captured.resolution, bestWithin, args.target)
					captured.streamID = a.snapshotStreamID
					captured.provenance = existingStreamProvenance(m.ownsStream(t, StreamSnapshot, a.snapshotStreamID))
					return captured, nil
				}
				known = slices.DeleteFunc(known, func(s allocatedSnapshotStream) bool { return s.snapshotStreamID == a.snapshotStreamID })
			}
			// The camera answers DynamicConstraintError when no
			// SnapshotCapabilities entry matches, so the next capability is
			// the only retry that can succeed.
			var (
				allocated     *snapshotCapability
				id            uint16
				lastStatus    im.StatusCode
				hasLastStatus bool
			)
			for _, c := range candidates {
				resp, err := m.io.invoke(ctx, deviceCall{target: t, cluster: clusterAVSM, command: cmdSnapshotStreamAllocate, fields: csdef.SnapshotStreamAllocateRequest{
					ImageCodec: c.imageCodec, MaxFrameRate: uint16(c.maxFrameRate), //nolint:gosec // G115: reported as uint16
					MinResolution: wireResolution(c.resolution), MaxResolution: wireResolution(c.resolution), Quality: snapshotQuality,
					WatermarkEnabled: wireFlag(overlays.watermark), OsdEnabled: wireFlag(overlays.osd),
				}})
				if err == nil {
					r, ok := resp.(csdef.SnapshotStreamAllocateResponse)
					if !ok {
						return nil, stackError("SnapshotStreamAllocate returned no SnapshotStreamID")
					}
					c := c
					allocated, id = &c, r.SnapshotStreamId
					break
				}
				if isTyped(err) {
					return nil, err
				}
				lastStatus, hasLastStatus = deviceStatusOf(err)
				switch ladderReactionOf(lastStatus, hasLastStatus) {
				case reactRethrow:
					return nil, err
				case reactIncompatible:
					s := lastStatus
					return nil, &IncompatibleError{Reason: ReasonBounds, Device: deviceCodecs, Requested: requestedCodecs, DeviceStatus: &s}
				case reactUnservable, reactCapacity:
					// The next capability is the retry.
				}
			}
			if allocated == nil {
				return nil, snapshotFailure(st, lastStatus, hasLastStatus, deviceCodecs, requestedCodecs)
			}
			// §11.2.8.8: the camera answers a request matching one of its
			// streams with that stream's id.
			if slices.ContainsFunc(known, func(s allocatedSnapshotStream) bool { return s.snapshotStreamID == id }) {
				cur, err := m.snapshotStreamAfterAllocate(ctx, t, id)
				if err != nil {
					return nil, err
				}
				if cur == nil {
					return nil, stackError("SnapshotStreamAllocate answered with stream %d, which the camera no longer lists", id)
				}
				outside := &IncompatibleError{Reason: ReasonBounds, Device: deviceCodecs, Requested: requestedCodecs}
				// Stated overlays only: the camera matches a stream whatever its
				// overlays.
				if (args.codec != nil && cur.imageCodec != *args.codec) || !overlaysMatch(cur.overlays, args.overlays) {
					return nil, outside
				}
				captured, err := m.captureSnapshot(ctx, captureArgs{t: t, state: st, streamID: id, requested: requestedResolution(allocated.resolution, args.target), deviceCodecs: deviceCodecs, requestedCodecs: requestedCodecs})
				if err != nil {
					return nil, err
				}
				// The stream's range may exceed the ceiling, so only the frame
				// it returned decides (§11.2.8.13.3).
				if args.maxResolution != (resolution{}) && !captured.resolution.fitsUnder(args.maxResolution) {
					return nil, outside
				}
				captured.degraded = isDegradedFrom(captured.resolution, bestWithin, args.target)
				captured.streamID = id
				captured.provenance = existingStreamProvenance(m.ownsStream(t, StreamSnapshot, id))
				return captured, nil
			}
			alloc := allocatedSnapshotStream{
				overlays: overlays, snapshotStreamID: id, imageCodec: allocated.imageCodec,
				minResolution: allocated.resolution, maxResolution: allocated.resolution, quality: snapshotQuality,
				frameRate: allocated.maxFrameRate, encodedPixels: allocated.requiresEncodedPixels, hardwareEncoder: usesHardwareEncoder(*allocated),
			}
			lease := m.recordAllocation(t, streamLease{kind: StreamSnapshot, streamID: id, allocatedByUs: true, snapshot: &alloc})
			// A failed call names no stream id, so this is the stream's only
			// way back.
			scope.returnOnFailure(func(ctx context.Context) error { return m.deallocate(ctx, t, lease) })
			captured, err := m.captureSnapshot(ctx, captureArgs{t: t, state: st, streamID: id, requested: requestedResolution(allocated.resolution, args.target), deviceCodecs: deviceCodecs, requestedCodecs: requestedCodecs})
			if err != nil {
				return nil, err
			}
			captured.degraded = isDegradedFrom(captured.resolution, bestWithin, args.target)
			captured.streamID = id
			captured.provenance = ProvenanceAllocated
			return captured, nil
		})
	})
}

// snapshotStreamAfterAllocate is videoStreamAfterAllocate for snapshot
// streams. Mirrors matterjs-server
// packages/ws-controller/src/camera/CameraStreamManager.ts:847-859 at
// 0bd06f2.
func (m *manager) snapshotStreamAfterAllocate(ctx context.Context, t target, id uint16) (*allocatedSnapshotStream, error) {
	st, err := m.io.readCameraState(ctx, t)
	if err != nil || st == nil {
		return nil, err
	}
	for _, s := range append(slices.Clone(st.snapshot), m.unreportedSnapshotStreams(t, st.snapshot)...) {
		if s.snapshotStreamID == id {
			return &s, nil
		}
	}
	return nil, nil
}

// captureAdoptedSnapshot is captureSnapshot on an adopted stream, nil on
// NOT_FOUND: the cached state may still list a stream deallocated since.
// Mirrors matterjs-server packages/ws-controller/src/camera/CameraStreamManager.ts:2651-2661
// at 0bd06f2.
func (m *manager) captureAdoptedSnapshot(ctx context.Context, args captureArgs) (*snapshotResult, error) {
	r, err := m.captureSnapshot(ctx, args)
	if err != nil && statusIs(err, im.StatusNotFound) {
		return nil, nil
	}
	return r, err
}

// captureSnapshot is CaptureSnapshot against one allocated stream, with
// the snapshot path's own error mapping. Mirrors matterjs-server
// packages/ws-controller/src/camera/CameraStreamManager.ts:2663-2701 at
// 0bd06f2.
func (m *manager) captureSnapshot(ctx context.Context, args captureArgs) (*snapshotResult, error) {
	resp, err := m.io.invoke(ctx, deviceCall{target: args.t, cluster: clusterAVSM, command: cmdCaptureSnapshot, fields: csdef.CaptureSnapshotRequest{
		SnapshotStreamId: spec.Nullable[uint16]{Value: args.streamID}, RequestedResolution: wireResolution(args.requested),
	}})
	if err != nil {
		if isTyped(err) {
			return nil, err
		}
		status, ok := deviceStatusOf(err)
		if ladderReactionOf(status, ok) == reactRethrow {
			return nil, m.privacyFailure(ctx, args.t, err, snapshotPrivacyModes)
		}
		return nil, snapshotFailure(args.state, status, ok, args.deviceCodecs, args.requestedCodecs)
	}
	r, ok := resp.(csdef.CaptureSnapshotResponse)
	if !ok || r.Data == nil {
		return nil, stackError("CaptureSnapshot returned an incomplete response")
	}
	return &snapshotResult{data: r.Data, codec: r.ImageCodec, resolution: resolutionOf(r.Resolution)}, nil
}

// releaseStream deallocates one stream, passing the camera's answer on
// with no refusal of its own; INVALID_IN_STATE is ErrStreamInUse (the
// reference camera answers it only for ReferenceCount > 0). Mirrors
// matterjs-server packages/ws-controller/src/camera/CameraStreamManager.ts:2702-2736
// at 0bd06f2.
func (m *manager) releaseStream(ctx context.Context, t target, kind StreamKind, id uint16) error {
	_, err := withEndpointLock(ctx, m, t, func() (struct{}, error) {
		st, err := m.requireState(ctx, t)
		if err != nil {
			return struct{}{}, err
		}
		refs := referenceCountOf(st, kind, id)
		if _, err := m.io.invoke(ctx, deallocateCall(t, kind, id)); err != nil {
			switch {
			case statusIs(err, im.StatusInvalidInState):
				return struct{}{}, &InUseError{StreamID: id, ReferenceCount: refs, cause: err}
			case statusIs(err, im.StatusNotFound):
				m.dropLease(t, kind, id)
			}
			return struct{}{}, err
		}
		m.dropLease(t, kind, id)
		return struct{}{}, nil
	})
	return err
}
