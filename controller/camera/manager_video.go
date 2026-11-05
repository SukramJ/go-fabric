// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"context"
	"log/slog"
	"slices"
	"sync"

	csdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavstreammanagement"
	"github.com/SukramJ/go-fabric/im"
)

// resolvedStream is the stream a track got: its id, what it delivers and
// how it came to serve. degraded: the stream misses the envelope the
// controller would have allocated (the last rung, or an allocate answered
// with an existing stream); evicted is the video streams the request
// deallocated; budgetNarrowed the first window's ceilings the encoder
// budget gave up (allocate path only). Mirrors matterjs-server
// packages/ws-controller/src/camera/cameraTypes.ts:182-192 at 0bd06f2.
type resolvedStream struct {
	streamID       uint16
	video          *videoEnvelope
	audio          *audioEnvelope
	provenance     Provenance
	degraded       bool
	evicted        []uint16
	budgetNarrowed *videoBudgetNarrowing
}

// envelopeOfVideoStream is what an allocated stream delivers; its overlays
// stay as the camera reports them, so a restore sends a conformant
// allocate. Mirrors matterjs-server
// packages/ws-controller/src/camera/CameraStreamManager.ts:175-188 at
// 0bd06f2.
func envelopeOfVideoStream(s allocatedVideoStream) videoEnvelope {
	return videoEnvelope{
		overlays: s.overlays, codec: s.videoCodec,
		minResolution: s.minResolution, maxResolution: s.maxResolution,
		minFrameRate: s.minFrameRate, maxFrameRate: s.maxFrameRate,
		minBitRate: s.minBitRate, maxBitRate: s.maxBitRate, keyFrameInterval: s.keyFrameInterval,
	}
}

// allocatedVideoStreamOf stands in for the camera's report until it
// arrives; the allocate response carries only the id. Mirrors
// matterjs-server packages/ws-controller/src/camera/CameraStreamManager.ts:190-205
// at 0bd06f2.
func allocatedVideoStreamOf(id uint16, usage csdef.StreamUsageEnum, e videoEnvelope) allocatedVideoStream {
	return allocatedVideoStream{
		videoStreamID: id, streamUsage: usage, videoCodec: e.codec,
		minResolution: e.minResolution, maxResolution: e.maxResolution,
		minFrameRate: e.minFrameRate, maxFrameRate: e.maxFrameRate,
		minBitRate: e.minBitRate, maxBitRate: e.maxBitRate,
		keyFrameInterval: e.keyFrameInterval, overlays: e.overlays,
	}
}

// videoAllocateRequest is the VideoStreamAllocate of a window.
func videoAllocateRequest(usage csdef.StreamUsageEnum, e videoEnvelope) csdef.VideoStreamAllocateRequest {
	return csdef.VideoStreamAllocateRequest{
		StreamUsage: usage, VideoCodec: e.codec,
		MinFrameRate: uint16(e.minFrameRate), MaxFrameRate: uint16(e.maxFrameRate), //nolint:gosec // G115: the plan keeps frame rates within uint16
		MinResolution: wireResolution(e.minResolution), MaxResolution: wireResolution(e.maxResolution),
		MinBitRate: uint32(e.minBitRate), MaxBitRate: uint32(e.maxBitRate), //nolint:gosec // G115: the plan keeps bit rates within uint32
		KeyFrameInterval: uint16(e.keyFrameInterval), //nolint:gosec // G115: keyFrameIntervalMS
		WatermarkEnabled: wireFlag(e.overlays.watermark), OsdEnabled: wireFlag(e.overlays.osd),
	}
}

// knownVideoCodecs are the codecs with a wire name, the fallback for a
// camera with no trade-off points. Mirrors matterjs-server
// packages/ws-controller/src/camera/wireNames.ts:76-78 at 0bd06f2.
var knownVideoCodecs = []csdef.VideoCodecEnum{csdef.VideoCodecH264, csdef.VideoCodecHevc, csdef.VideoCodecVvc, csdef.VideoCodecAv1}

// requireCodecCandidates refuses an empty narrowing; device is the set
// before it. Mirrors matterjs-server
// packages/ws-controller/src/camera/CameraStreamManager.ts:237-252 at
// 0bd06f2.
func requireCodecCandidates(narrowed, before []csdef.VideoCodecEnum, requested []string) ([]csdef.VideoCodecEnum, error) {
	if len(narrowed) == 0 {
		return nil, &IncompatibleError{Reason: ReasonCodec, Track: TrackVideo, Device: videoCodecNamesOf(before), Requested: requested}
	}
	return narrowed, nil
}

// preferredVideoCodec is the device's codec, narrowed by what the offer
// can decode, then by the caller's preference (whose order wins). Either
// narrowing refuses when it leaves nothing; a codec the offer names but
// whose decode ceiling it cannot read is a level problem. A camera with no
// trade-off points falls back to every known codec. Mirrors
// matterjs-server packages/ws-controller/src/camera/CameraStreamManager.ts:253-286
// at 0bd06f2.
func preferredVideoCodec(deviceCodecs []csdef.VideoCodecEnum, sdp *sdpVideoConstraints, hintCodecs []string) (csdef.VideoCodecEnum, error) {
	candidates := deviceCodecs
	if len(candidates) == 0 {
		candidates = knownVideoCodecs
	}
	if sdp != nil {
		if decodable, unreadable, ok := decodableVideoCodecs(sdp); ok {
			var narrowed []csdef.VideoCodecEnum
			for _, c := range candidates {
				if slices.Contains(decodable, videoCodecName(c)) {
					narrowed = append(narrowed, c)
				}
			}
			if len(narrowed) == 0 && slices.ContainsFunc(candidates, func(c csdef.VideoCodecEnum) bool { return slices.Contains(unreadable, videoCodecName(c)) }) {
				return 0, &IncompatibleError{Reason: ReasonLevel, Track: TrackVideo, Device: videoCodecNamesOf(candidates), Requested: slices.Clone(unreadable)}
			}
			var err error
			if candidates, err = requireCodecCandidates(narrowed, candidates, append(slices.Clone(decodable), unreadable...)); err != nil {
				return 0, err
			}
		}
	}
	if hintCodecs != nil {
		var preferred []csdef.VideoCodecEnum
		for _, name := range hintCodecs {
			if i := slices.IndexFunc(candidates, func(c csdef.VideoCodecEnum) bool { return videoCodecName(c) == name }); i >= 0 {
				preferred = append(preferred, candidates[i])
			}
		}
		var err error
		if candidates, err = requireCodecCandidates(preferred, candidates, hintCodecs); err != nil {
			return 0, err
		}
	}
	if len(candidates) == 0 {
		return csdef.VideoCodecH264, nil
	}
	return candidates[0], nil
}

// videoRequest is one video track to resolve.
type videoRequest struct {
	t             target
	usage         csdef.StreamUsageEnum
	limits        selectedVideoCodecLimits
	hints         *videoHints
	allowEviction bool
}

// freedRoom is what one make-room step did: the camera refused, the
// stream was gone already (NOT_FOUND, so the room is free although this
// request evicted nothing), or this request evicted it.
type freedRoom struct {
	streamID uint16
	outcome  roomOutcome
	spend    func()
	// restore allocates the freed video stream's equivalent once, whoever
	// calls first; nil for a snapshot stream.
	restore func(context.Context) *allocatedVideoStream
}

type roomOutcome uint8

const (
	roomRefused roomOutcome = iota
	roomGone
	roomEvicted
)

// resolveVideoStream resolves a video track alone, under the endpoint lock
// and its own scope. Mirrors matterjs-server
// packages/ws-controller/src/camera/CameraStreamManager.ts:1059-1071 at
// 0bd06f2.
func (m *manager) resolveVideoStream(ctx context.Context, req videoRequest) (*resolvedStream, error) {
	return withEndpointLock(ctx, m, req.t, func() (*resolvedStream, error) {
		return withAllocationScope(ctx, m, func(s *allocationScope) (*resolvedStream, error) {
			return m.resolveVideoStreamLocked(ctx, req, s)
		})
	})
}

// resolveVideoStreamLocked walks the rungs, cheapest for everyone first:
// reuse, allocate within the remaining encoder budget, narrow, evict an
// unused stream (with allowEviction), then hand out a stream that meets
// the caller's bounds but not the envelope, degraded. No rung gives up the
// caller's bounds. The caller holds the endpoint lock and owns the scope.
// Mirrors matterjs-server packages/ws-controller/src/camera/CameraStreamManager.ts:1085-1427
// at 0bd06f2.
func (m *manager) resolveVideoStreamLocked(ctx context.Context, req videoRequest, scope *allocationScope) (*resolvedStream, error) { //nolint:gocognit,gocyclo,funlen // one function, as the source is: split, the order of its rungs would hide
	t, usage, codec := req.t, req.usage, req.limits.codec
	st, err := m.requireState(ctx, t)
	if err != nil {
		return nil, err
	}
	deviceCodecs := st.codecs()
	incompatible := func(reason IncompatibleReason) *IncompatibleError {
		return &IncompatibleError{Reason: reason, Track: TrackVideo, Device: videoCodecNamesOf(deviceCodecs), Requested: []string{videoCodecName(codec)}}
	}
	if len(deviceCodecs) > 0 && !slices.Contains(deviceCodecs, codec) {
		return nil, incompatible(ReasonCodec)
	}
	var stated overlayBounds
	if req.hints != nil {
		stated = req.hints.overlays
	}
	sel := resolveOverlays(stated, supportOf(st.features))
	if sel.unsupported != 0 {
		// Sending the field without the feature is INVALID_COMMAND
		// (§11.2.8.4, conformance WMARK / OSD).
		e := incompatible(ReasonFeature)
		e.Feature = featureName(sel.unsupported)
		return nil, e
	}
	caps := videoCapabilities{sensor: resolution{1920, 1080}, maxFrameRate: 30, rateDistortionPoints: st.rateDistortionPoints, maxNetworkBandwidth: st.maxNetworkBandwidth}
	if st.sensor != nil {
		caps.sensor = resolution{st.sensor.width, st.sensor.height}
		caps.maxFrameRate = st.sensor.maxFPS
	}
	if st.minViewport != nil {
		caps.minViewport = *st.minViewport
	}
	selection := computeVideoEnvelope(videoEnvelopeArgs{capabilities: caps, limits: req.limits, hints: req.hints, overlays: sel.overlays})
	if selection.plan == nil {
		e := incompatible(ReasonBounds)
		e.Bound = selection.unsatisfiable
		return nil, e
	}
	plan := selection.plan
	// Ladder-local copies; freeing updates these, never st. Unreported
	// snapshot streams count, because the report lags a snapshot followed
	// by a start.
	liveStreams := slices.Clone(st.video)
	liveSnapshots := append(slices.Clone(st.snapshot), m.unreportedSnapshotStreams(t, st.snapshot)...)
	// Never freed or listed as occupying: only the camera can state their
	// reference counts.
	unreported := m.unreportedVideoStreams(t, liveStreams)
	bounds := newVideoCallerBounds(req.limits, req.hints)
	known := func() []allocatedVideoStream { return append(slices.Clone(liveStreams), unreported...) }
	fitsBudget := func(without *allocatedVideoStream) func(videoEnvelope) bool {
		return func(window videoEnvelope) bool {
			streams := slices.DeleteFunc(known(), func(s allocatedVideoStream) bool {
				return without != nil && s.videoStreamID == without.videoStreamID
			})
			free, ok := freeEncodedPixelRate(videoPixelRateBudget{maxEncodedPixelRate: st.maxEncodedPixelRate, videoStreams: streams, snapshotStreams: liveSnapshots})
			return !ok || float64(encodedPixelRate(window)) <= free
		}
	}
	preferred := preferredUsage{usage: usage, set: true}
	if reused := findReusableVideoStream(known(), plan, &bounds, func(c *allocatedVideoStream) (int, bool) {
		w, ok := firstVideoWindow(plan, fitsBudget(c))
		return w.maxFrameRate, ok
	}, preferred); reused != nil {
		r := *reused
		env := envelopeOfVideoStream(r)
		return &resolvedStream{streamID: r.videoStreamID, video: &env, provenance: existingStreamProvenance(m.leaseReusedVideoStream(t, r))}, nil
	}

	search := newVideoWindowSearch(plan, fitsBudget(nil), 0)
	// Another controller's stream the degraded rung could hand out is
	// never evicted for the same request; each candidate is tried once.
	videoCandidates := slices.DeleteFunc(slices.Clone(liveStreams), func(s allocatedVideoStream) bool {
		return !m.ownsStream(t, StreamVideo, s.videoStreamID) && satisfiesVideoCallerBounds(&s, &bounds)
	})
	snapshotCandidates := slices.Clone(liveSnapshots)

	var (
		lastStatus      im.StatusCode
		hasLastStatus   bool
		answeredOutside *uint16
		freed           []func()
		freedServable   []freedVideo
		evicted         []uint16
	)
	reporting := func(r *resolvedStream) *resolvedStream {
		if len(evicted) > 0 {
			r.evicted = slices.Clone(evicted)
		}
		return r
	}
	freeSnapshot := func(ours bool) bool {
		for {
			group := slices.DeleteFunc(slices.Clone(snapshotCandidates), func(s allocatedSnapshotStream) bool { return m.ownsSnapshotStream(t, s) != ours })
			room := m.freeSnapshotStream(ctx, t, group, st.maxEncodedPixelRate, scope)
			if room == nil {
				return false
			}
			snapshotCandidates = slices.DeleteFunc(snapshotCandidates, func(s allocatedSnapshotStream) bool { return s.snapshotStreamID == room.streamID })
			if room.outcome == roomRefused {
				continue
			}
			liveSnapshots = slices.DeleteFunc(liveSnapshots, func(s allocatedSnapshotStream) bool { return s.snapshotStreamID == room.streamID })
			if room.outcome == roomEvicted {
				freed = append(freed, room.spend)
			}
			return true
		}
	}
	freeVideo := func(ours, servable bool) bool {
		for {
			group := slices.DeleteFunc(slices.Clone(videoCandidates), func(s allocatedVideoStream) bool {
				return m.ownsStream(t, StreamVideo, s.videoStreamID) != ours || satisfiesVideoCallerBounds(&s, &bounds) != servable
			})
			room := m.freeAnUnreferencedVideoStream(ctx, t, group, st.streamUsagePriorities, scope)
			if room == nil {
				return false
			}
			videoCandidates = slices.DeleteFunc(videoCandidates, func(s allocatedVideoStream) bool { return s.videoStreamID == room.streamID })
			if room.outcome == roomRefused {
				continue
			}
			liveStreams = slices.DeleteFunc(liveStreams, func(s allocatedVideoStream) bool { return s.videoStreamID == room.streamID })
			if room.outcome == roomEvicted {
				freed = append(freed, room.spend)
				evicted = append(evicted, room.streamID)
				if i := slices.IndexFunc(group, func(s allocatedVideoStream) bool { return s.videoStreamID == room.streamID }); servable && i >= 0 {
					freedServable = append(freedServable, freedVideo{stream: group[i], restore: room.restore})
				}
			}
			return true
		}
	}
	// makeRoom frees one stream this request may take: this controller's
	// own first, those the degraded rung could hand out last among them;
	// another controller's snapshot stream before its video stream.
	makeRoom := func() bool {
		return freeSnapshot(true) || freeVideo(true, false) || freeVideo(true, true) || freeSnapshot(false) || freeVideo(false, false)
	}
	for {
		move := search.move
		if move.kind == moveGiveUp {
			break
		}
		if move.kind == moveMakeRoom {
			// No allocate follows the last ask, so room made there would be
			// for nothing.
			if req.allowEviction && search.asksLeft() > 0 && makeRoom() {
				search.roomMade()
			} else {
				search.noRoom()
			}
			continue
		}
		window := move.window
		resp, err := m.io.invoke(ctx, deviceCall{target: t, cluster: clusterAVSM, command: cmdVideoStreamAllocate, fields: videoAllocateRequest(usage, window)})
		if err != nil {
			if isTyped(err) {
				return nil, err
			}
			lastStatus, hasLastStatus = deviceStatusOf(err)
			switch ladderReactionOf(lastStatus, hasLastStatus) {
			case reactRethrow:
				return nil, err
			case reactIncompatible:
				e := incompatible(ReasonBounds)
				s := lastStatus
				e.DeviceStatus = &s
				return nil, e
			case reactUnservable:
				search.refuse(refusedUnservable)
			default:
				search.refuse(refusedCapacity)
			}
			continue
		}
		r, ok := resp.(csdef.VideoStreamAllocateResponse)
		if !ok {
			// Without the id the stream cannot be given back.
			return nil, stackError("VideoStreamAllocate returned no VideoStreamID")
		}
		id := r.VideoStreamId
		// §11.2.8.4: the camera answers a request matching one of its
		// streams with that stream's id.
		if slices.ContainsFunc(known(), func(s allocatedVideoStream) bool { return s.videoStreamID == id }) {
			cur, err := m.videoStreamAfterAllocate(ctx, t, id)
			if err != nil {
				return nil, err
			}
			if cur == nil {
				return nil, stackError("VideoStreamAllocate answered with stream %d, which the camera no longer lists", id)
			}
			if !satisfiesVideoCallerBounds(cur, &bounds) {
				// A smaller window would be answered with the same stream.
				m.log.Info("camera.video.answered_outside_bounds", slog.Uint64("node", t.nodeID), slog.Int("endpoint", int(t.endpointID)), slog.Int("stream", int(id)))
				answeredOutside = &id
				break
			}
			within := findReusableVideoStream([]allocatedVideoStream{*cur}, plan, &bounds, func(*allocatedVideoStream) (int, bool) { return window.maxFrameRate, true }, preferredUsage{}) != nil
			env := envelopeOfVideoStream(*cur)
			return reporting(&resolvedStream{streamID: id, video: &env, degraded: !within, provenance: existingStreamProvenance(m.leaseReusedVideoStream(t, *cur))}), nil
		}
		alloc := allocatedVideoStreamOf(id, usage, window)
		lease := m.recordAllocation(t, streamLease{kind: StreamVideo, streamID: id, allocatedByUs: true, video: &alloc})
		scope.returnOnFailure(func(ctx context.Context) error { return m.deallocate(ctx, t, lease) })
		for _, spend := range freed {
			spend()
		}
		out := &resolvedStream{streamID: id, video: &window, provenance: ProvenanceAllocated}
		if n, ok := search.budgetNarrowing(window); ok {
			out.budgetNarrowed = &n
		}
		return reporting(out), nil
	}

	// Last rung: gives up the computed envelope, never the caller's bounds.
	if d := findDegradedVideoStream(slices.DeleteFunc(known(), func(s allocatedVideoStream) bool {
		return answeredOutside != nil && s.videoStreamID == *answeredOutside
	}), &bounds, preferred); d != nil {
		s := *d
		env := envelopeOfVideoStream(s)
		return reporting(&resolvedStream{streamID: s.videoStreamID, video: &env, degraded: true, provenance: existingStreamProvenance(m.leaseReusedVideoStream(t, s))}), nil
	}
	// The request freed a stream the degraded rung would have handed out;
	// its equivalent serves instead.
	servable := make([]allocatedVideoStream, len(freedServable))
	for i, f := range freedServable {
		servable[i] = f.stream
	}
	if d := findDegradedVideoStream(servable, &bounds, preferred); d != nil {
		i := slices.IndexFunc(freedServable, func(f freedVideo) bool { return f.stream.videoStreamID == d.videoStreamID })
		if restored := freedServable[i].restore(ctx); restored != nil {
			env := envelopeOfVideoStream(*restored)
			return reporting(&resolvedStream{streamID: restored.videoStreamID, video: &env, degraded: true, provenance: ProvenanceAllocated}), nil
		}
	}
	if answeredOutside != nil || search.outcome() == refusedUnservable {
		e := incompatible(ReasonBounds)
		if answeredOutside == nil && hasLastStatus {
			s := lastStatus
			e.DeviceStatus = &s
		}
		return nil, e
	}
	occupied := make([]OccupyingStream, 0, len(liveStreams))
	for _, s := range liveStreams {
		occupied = append(occupied, occupying(StreamVideo, s.videoStreamID, s.referenceCount))
	}
	// The ladder's list, so a snapshot stream this request freed is not
	// named.
	return nil, exhausted(st, append(occupied, encoderHoldingSnapshotStreams(liveSnapshots)...))
}

// freedVideo is a stream the degraded rung could have handed out that the
// request evicted, with its restore.
type freedVideo struct {
	stream  allocatedVideoStream
	restore func(context.Context) *allocatedVideoStream
}

// videoStreamAfterAllocate is the stream as the camera lists it after an
// allocate it answered with that existing stream. Mirrors matterjs-server
// packages/ws-controller/src/camera/CameraStreamManager.ts:818-833 at
// 0bd06f2.
func (m *manager) videoStreamAfterAllocate(ctx context.Context, t target, id uint16) (*allocatedVideoStream, error) {
	st, err := m.io.readCameraState(ctx, t)
	if err != nil || st == nil {
		return nil, err
	}
	for _, s := range append(slices.Clone(st.video), m.unreportedVideoStreams(t, st.video)...) {
		if s.videoStreamID == id {
			return &s, nil
		}
	}
	return nil, nil
}

// freeAnUnreferencedVideoStream deallocates one unreferenced video stream
// of streams (chooseEvictionVictim) and returns its room, nil when there
// was none. Unless the request spends the capacity and succeeds, an
// equivalent stream is allocated back under a new id. Mirrors
// matterjs-server packages/ws-controller/src/camera/CameraStreamManager.ts:1655-1714
// at 0bd06f2.
func (m *manager) freeAnUnreferencedVideoStream(ctx context.Context, t target, streams []allocatedVideoStream, priorities []csdef.StreamUsageEnum, scope *allocationScope) *freedRoom {
	ours := func(s *allocatedVideoStream) bool { return m.ownsStream(t, StreamVideo, s.videoStreamID) }
	v := chooseEvictionVictim(streams, priorities, ours)
	if v == nil {
		return nil
	}
	victim := *v
	if !ours(&victim) {
		m.log.Info("camera.video.evict_foreign", slog.Uint64("node", t.nodeID), slog.Int("stream", int(victim.videoStreamID)))
	}
	if _, err := m.io.invoke(ctx, deallocateCall(t, StreamVideo, victim.videoStreamID)); err != nil {
		return m.deallocateFailed(t, StreamVideo, victim.videoStreamID, err)
	}
	m.dropLease(t, StreamVideo, victim.videoStreamID)
	if m.onEvicted != nil {
		m.onEvicted(StreamEvicted{NodeID: t.nodeID, Endpoint: t.endpointID, Kind: StreamVideo, StreamID: victim.videoStreamID})
	}
	var (
		once     sync.Once
		restored *allocatedVideoStream
	)
	restore := func(ctx context.Context) *allocatedVideoStream {
		once.Do(func() {
			r, err := m.restoreFreedVideoStream(ctx, t, victim)
			if err != nil {
				m.log.Warn("camera.video.restore_failed", slog.Uint64("node", t.nodeID), slog.Int("stream", int(victim.videoStreamID)), slog.Any("err", err))
				return
			}
			restored = r
		})
		return restored
	}
	spend := scope.returnUnlessSpent(func(ctx context.Context) error { restore(ctx); return nil })
	return &freedRoom{streamID: victim.videoStreamID, outcome: roomEvicted, spend: spend, restore: restore}
}

// deallocateFailed: only NOT_FOUND proves the stream gone; any other
// status keeps the lease. A stream already gone was not evicted by this
// request, so nothing is announced or restored. Mirrors matterjs-server
// packages/ws-controller/src/camera/CameraStreamManager.ts:1716-1732 at
// 0bd06f2.
func (m *manager) deallocateFailed(t target, kind StreamKind, id uint16, err error) *freedRoom {
	if statusIs(err, im.StatusNotFound) {
		m.dropLease(t, kind, id)
		return &freedRoom{streamID: id, outcome: roomGone}
	}
	m.log.Info("camera.deallocate.refused", slog.String("kind", string(kind)), slog.Int("stream", int(id)), slog.Any("err", err))
	return &freedRoom{streamID: id, outcome: roomRefused}
}

// freeSnapshotStream deallocates one unreferenced snapshot stream of
// streams (chooseSnapshotStreamToFree), registered with the scope like
// freeAnUnreferencedVideoStream. Mirrors matterjs-server
// packages/ws-controller/src/camera/CameraStreamManager.ts:1734-1777 at
// 0bd06f2.
func (m *manager) freeSnapshotStream(ctx context.Context, t target, streams []allocatedSnapshotStream, maxEncodedPixelRate ceiling, scope *allocationScope) *freedRoom {
	v := chooseSnapshotStreamToFree(streams, maxEncodedPixelRate)
	if v == nil {
		return nil
	}
	victim := *v
	if _, err := m.io.invoke(ctx, deallocateCall(t, StreamSnapshot, victim.snapshotStreamID)); err != nil {
		return m.deallocateFailed(t, StreamSnapshot, victim.snapshotStreamID, err)
	}
	m.dropLease(t, StreamSnapshot, victim.snapshotStreamID)
	if m.onEvicted != nil {
		m.onEvicted(StreamEvicted{NodeID: t.nodeID, Endpoint: t.endpointID, Kind: StreamSnapshot, StreamID: victim.snapshotStreamID})
	}
	spend := scope.returnUnlessSpent(func(ctx context.Context) error {
		if err := m.restoreFreedSnapshotStream(ctx, t, victim); err != nil {
			m.log.Warn("camera.snapshot.restore_failed", slog.Uint64("node", t.nodeID), slog.Int("stream", int(victim.snapshotStreamID)), slog.Any("err", err))
		}
		return nil
	})
	return &freedRoom{streamID: victim.snapshotStreamID, outcome: roomEvicted, spend: spend}
}

// ownsSnapshotStream reports whether a reported snapshot stream is one
// this controller allocated; stricter than ownsStream, the reported
// parameters must match the lease, since a reissued id alone can name
// another controller's stream. Mirrors matterjs-server
// packages/ws-controller/src/camera/CameraStreamManager.ts:1779-1801 at
// 0bd06f2.
func (m *manager) ownsSnapshotStream(t target, reported allocatedSnapshotStream) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	l := m.leaseForLocked(t, StreamSnapshot, reported.snapshotStreamID)
	if l == nil || !l.allocatedByUs || l.snapshot == nil {
		return false
	}
	a := l.snapshot
	return a.imageCodec == reported.imageCodec && a.minResolution == reported.minResolution && a.maxResolution == reported.maxResolution
}

// reportedStreamIDs are the ids the camera reports now; read before a
// restore, since the camera answers a request matching one of them with
// its id. Mirrors matterjs-server
// packages/ws-controller/src/camera/CameraStreamManager.ts:1803-1819 at
// 0bd06f2.
func (m *manager) reportedStreamIDs(ctx context.Context, t target, kind StreamKind) (map[uint16]bool, error) {
	st, err := m.io.readCameraState(ctx, t)
	if err != nil {
		return nil, err
	}
	out := map[uint16]bool{}
	if st == nil {
		return out, nil
	}
	if kind == StreamVideo {
		for _, s := range st.video {
			out[s.videoStreamID] = true
		}
	} else {
		for _, s := range st.snapshot {
			out[s.snapshotStreamID] = true
		}
	}
	return out, nil
}

// restoreFreedSnapshotStream allocates a stream with the parameters of a
// snapshot stream the make-room rung took and the request did not use.
// Mirrors matterjs-server packages/ws-controller/src/camera/CameraStreamManager.ts:1821-1869
// at 0bd06f2.
func (m *manager) restoreFreedSnapshotStream(ctx context.Context, t target, freed allocatedSnapshotStream) error {
	known, err := m.reportedStreamIDs(ctx, t, StreamSnapshot)
	if err != nil {
		return err
	}
	resp, err := m.io.invoke(ctx, deviceCall{target: t, cluster: clusterAVSM, command: cmdSnapshotStreamAllocate, fields: csdef.SnapshotStreamAllocateRequest{
		ImageCodec: freed.imageCodec, MaxFrameRate: uint16(freed.frameRate), //nolint:gosec // G115: reported as uint16
		MinResolution: wireResolution(freed.minResolution), MaxResolution: wireResolution(freed.maxResolution),
		Quality:          uint8(freed.quality), //nolint:gosec // G115: reported as uint8
		WatermarkEnabled: wireFlag(freed.overlays.watermark), OsdEnabled: wireFlag(freed.overlays.osd),
	}})
	if err != nil {
		return err
	}
	r, ok := resp.(csdef.SnapshotStreamAllocateResponse)
	if !ok {
		return stackError("SnapshotStreamAllocate returned no SnapshotStreamID")
	}
	if known[r.SnapshotStreamId] {
		return nil
	}
	alloc := freed
	alloc.snapshotStreamID, alloc.referenceCount = r.SnapshotStreamId, 0
	m.recordAllocation(t, streamLease{kind: StreamSnapshot, streamID: r.SnapshotStreamId, allocatedByUs: true, snapshot: &alloc})
	return nil
}

// restoreFreedVideoStream allocates a stream with the parameters of one
// the make-room rung took: when the request did not use the capacity, or
// as its degraded stream. Not an undo: the camera issues a new id. The
// replacement is leased as ours, unless the camera answers with a stream it
// already held; then nil. Mirrors matterjs-server
// packages/ws-controller/src/camera/CameraStreamManager.ts:1871-1916 at
// 0bd06f2.
func (m *manager) restoreFreedVideoStream(ctx context.Context, t target, freed allocatedVideoStream) (*allocatedVideoStream, error) {
	env := envelopeOfVideoStream(freed)
	known, err := m.reportedStreamIDs(ctx, t, StreamVideo)
	if err != nil {
		return nil, err
	}
	resp, err := m.io.invoke(ctx, deviceCall{target: t, cluster: clusterAVSM, command: cmdVideoStreamAllocate, fields: videoAllocateRequest(freed.streamUsage, env)})
	if err != nil {
		return nil, err
	}
	r, ok := resp.(csdef.VideoStreamAllocateResponse)
	if !ok {
		return nil, stackError("VideoStreamAllocate returned no VideoStreamID")
	}
	if known[r.VideoStreamId] {
		return nil, nil
	}
	alloc := allocatedVideoStreamOf(r.VideoStreamId, freed.streamUsage, env)
	m.recordAllocation(t, streamLease{kind: StreamVideo, streamID: r.VideoStreamId, allocatedByUs: true, video: &alloc})
	return &alloc, nil
}
