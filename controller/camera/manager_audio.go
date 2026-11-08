// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"context"
	"log/slog"
	"slices"

	csdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavstreammanagement"
)

// trackState is what a request says of one track: declined (no stream),
// deferred (one when available) or demanded (a stream or the reason
// there is none). Mirrors matterjs-server
// packages/ws-controller/src/camera/streamPolicy.ts:81-94 at 0bd06f2.
type trackState uint8

const (
	trackDeferred trackState = iota
	trackDeclined
	trackDemanded
)

// trackOutcome is a track's stream, or why there is none; only
// resolveTrack decides whether no stream is a failure. Mirrors
// matterjs-server packages/ws-controller/src/camera/CameraStreamManager.ts:561-565
// at 0bd06f2.
type trackOutcome struct {
	stream      *resolvedStream
	unavailable error
}

// resolveTrack is the stream for a track, or nil: a deferred track may be
// absent, a demanded one returns the reason instead. Mirrors
// matterjs-server packages/ws-controller/src/camera/CameraStreamManager.ts:1036-1057
// at 0bd06f2.
func (m *manager) resolveTrack(track Track, nodeID uint64, state trackState, o trackOutcome) (*resolvedStream, error) {
	if o.stream != nil {
		return o.stream, nil
	}
	if state == trackDemanded {
		return nil, o.unavailable
	}
	m.log.Info("camera.track.unavailable", slog.String("track", string(track)), slog.Uint64("node_id", nodeID), slog.Any("err", o.unavailable))
	return nil, nil
}

// envelopeOfAudioStream is what an allocated audio stream delivers.
// Mirrors matterjs-server packages/ws-controller/src/camera/CameraStreamManager.ts:224-232
// at 0bd06f2.
func envelopeOfAudioStream(s allocatedAudioStream) audioEnvelope {
	return audioEnvelope{codec: s.audioCodec, channelCount: s.channelCount, sampleRate: s.sampleRate, bitRate: s.bitRate, bitDepth: s.bitDepth}
}

// audioRequest is one audio track to resolve.
type audioRequest struct {
	t     target
	usage csdef.StreamUsageEnum
	sdp   *sdpVideoConstraints
	hints *audioHints
}

// resolveAudioStream resolves an audio track alone; a declined track is
// nil. Mirrors matterjs-server
// packages/ws-controller/src/camera/CameraStreamManager.ts:1429-1444 at
// 0bd06f2.
func (m *manager) resolveAudioStream(ctx context.Context, req audioRequest, state trackState) (*resolvedStream, error) {
	if state == trackDeclined {
		return nil, nil
	}
	return withEndpointLock(ctx, m, req.t, func() (*resolvedStream, error) {
		return withAllocationScope(ctx, m, func(s *allocationScope) (*resolvedStream, error) {
			o, err := m.resolveAudioStreamLocked(ctx, req, s)
			if err != nil {
				return nil, err
			}
			return m.resolveTrack(TrackAudio, req.t.nodeID, state, o)
		})
	})
}

// resolveAudioStreamLocked has no narrowing ladder: dead ends that still
// allow a video-only session are unavailable, a caller-stated value the
// camera cannot serve fails. The caller holds the endpoint lock and owns
// the scope. Mirrors matterjs-server
// packages/ws-controller/src/camera/CameraStreamManager.ts:1446-1653 at
// 0bd06f2.
func (m *manager) resolveAudioStreamLocked(ctx context.Context, req audioRequest, scope *allocationScope) (trackOutcome, error) { //nolint:gocyclo,funlen // one function, as the source is: each dead end a return
	t, usage := req.t, req.usage
	st, err := m.requireState(ctx, t)
	if err != nil {
		return trackOutcome{}, err
	}
	requested := []string{}
	if req.hints != nil && req.hints.codecs != nil {
		requested = slices.Clone(req.hints.codecs)
	}
	// The offer's refusal is answered before anything the camera states.
	if _, refused := mediaRefusal(req.sdp, "audio"); refused {
		return trackOutcome{unavailable: &IncompatibleError{Reason: ReasonOffer, Track: TrackAudio, Device: []string{}, Requested: requested}}, nil
	}
	mic := st.microphone
	lacksAudio := lacksFeature(st.features, csdef.FeatureAudio)
	if lacksAudio || mic == nil || len(mic.supportedCodecs) == 0 || len(mic.supportedSampleRates) == 0 || len(mic.supportedBitDepths) == 0 {
		// Without the Audio feature, AudioStreamAllocate is not in the
		// AcceptedCommandList even if MicrophoneCapabilities is reported.
		e := &IncompatibleError{Reason: ReasonCapability, Track: TrackAudio, Device: []string{}, Requested: requested}
		if lacksAudio {
			e.Reason, e.Feature = ReasonFeature, featureName(csdef.FeatureAudio)
		}
		return trackOutcome{unavailable: e}, nil
	}
	deviceCodecs := make([]string, len(mic.supportedCodecs))
	for i, c := range mic.supportedCodecs {
		deviceCodecs[i] = AudioCodecName(c)
	}
	sel := computeAudioEnvelope(*mic, req.sdp, req.hints)
	if sel.unsatisfiable != nil {
		return trackOutcome{}, &IncompatibleError{Reason: ReasonBounds, Track: TrackAudio, Device: deviceCodecs, Requested: requested, Bound: sel.unsatisfiable}
	}
	if sel.envelope == nil {
		return trackOutcome{unavailable: &IncompatibleError{Reason: ReasonCodec, Track: TrackAudio, Device: deviceCodecs, Requested: requested}}, nil
	}
	env := *sel.envelope
	var bounds audioHints
	if req.hints != nil {
		bounds = *req.hints
	}
	known := append(slices.Clone(st.audio), m.unreportedAudioStreams(t, st.audio)...)
	var existing []allocatedAudioStream
	for _, s := range known {
		if satisfiesAudioCallerBounds(&s, &bounds) && s.audioCodec == env.codec && s.channelCount == env.channelCount && s.sampleRate == env.sampleRate {
			existing = append(existing, s)
		}
	}
	slices.SortStableFunc(existing, func(a, b allocatedAudioStream) int {
		return usageRank(a.streamUsage, usage, true) - usageRank(b.streamUsage, usage, true)
	})
	if len(existing) > 0 {
		s := existing[0]
		e := envelopeOfAudioStream(s)
		return trackOutcome{stream: &resolvedStream{streamID: s.audioStreamID, audio: &e, provenance: existingStreamProvenance(m.leaseReusedAudioStream(t, s))}}, nil
	}
	resp, err := m.io.invoke(ctx, deviceCall{target: t, cluster: clusterAVSM, command: cmdAudioStreamAllocate, fields: csdef.AudioStreamAllocateRequest{
		StreamUsage: usage, AudioCodec: env.codec,
		ChannelCount: uint8(env.channelCount), SampleRate: uint32(env.sampleRate), //nolint:gosec // G115: from the camera's own lists
		BitRate: uint32(env.bitRate), BitDepth: uint8(env.bitDepth), //nolint:gosec // G115: from the camera's own lists
	}})
	if err != nil {
		if isTyped(err) {
			return trackOutcome{}, err
		}
		status, ok := deviceStatusOf(err)
		switch ladderReactionOf(status, ok) {
		case reactRethrow:
			return trackOutcome{unavailable: err}, nil
		case reactCapacity:
			occupied := []OccupyingStream{}
			for _, s := range st.audio {
				occupied = append(occupied, occupying(StreamAudio, s.audioStreamID, s.referenceCount))
			}
			return trackOutcome{unavailable: exhausted(st, occupied)}, nil
		default:
			return trackOutcome{unavailable: &IncompatibleError{Reason: ReasonBounds, Track: TrackAudio, Device: deviceCodecs, Requested: requested, DeviceStatus: &status}}, nil
		}
	}
	r, ok := resp.(csdef.AudioStreamAllocateResponse)
	if !ok {
		return trackOutcome{unavailable: stackError("AudioStreamAllocate returned no AudioStreamID")}, nil
	}
	id := r.AudioStreamId
	// §11.2.8.1: the camera answers a request matching one of its streams
	// with that stream's id.
	if slices.ContainsFunc(known, func(s allocatedAudioStream) bool { return s.audioStreamID == id }) {
		cur, err := m.audioStreamAfterAllocate(ctx, t, id)
		if err != nil {
			return trackOutcome{unavailable: err}, nil //nolint:nilerr // a failed read leaves the track without a stream; resolveTrack decides
		}
		if cur == nil {
			return trackOutcome{unavailable: stackError("AudioStreamAllocate answered with stream %d, which the camera no longer lists", id)}, nil
		}
		// A reference camera matches a stream with more channels, a higher
		// sample rate or any bit rate.
		if cur.audioCodec != env.codec || !satisfiesAudioCallerBounds(cur, &bounds) {
			return trackOutcome{unavailable: &IncompatibleError{Reason: ReasonBounds, Track: TrackAudio, Device: deviceCodecs, Requested: requested}}, nil
		}
		e := envelopeOfAudioStream(*cur)
		return trackOutcome{stream: &resolvedStream{streamID: id, audio: &e, provenance: existingStreamProvenance(m.leaseReusedAudioStream(t, *cur))}}, nil
	}
	alloc := allocatedAudioStream{audioStreamID: id, streamUsage: usage, audioCodec: env.codec, channelCount: env.channelCount, sampleRate: env.sampleRate, bitRate: env.bitRate, bitDepth: env.bitDepth}
	lease := m.recordAllocation(t, streamLease{kind: StreamAudio, streamID: id, allocatedByUs: true, audio: &alloc})
	scope.returnOnFailure(func(ctx context.Context) error { return m.deallocate(ctx, t, lease) })
	return trackOutcome{stream: &resolvedStream{streamID: id, audio: &env, provenance: ProvenanceAllocated}}, nil
}

// audioStreamAfterAllocate is videoStreamAfterAllocate for audio. Mirrors
// matterjs-server packages/ws-controller/src/camera/CameraStreamManager.ts:834-846
// at 0bd06f2.
func (m *manager) audioStreamAfterAllocate(ctx context.Context, t target, id uint16) (*allocatedAudioStream, error) {
	st, err := m.io.readCameraState(ctx, t)
	if err != nil || st == nil {
		return nil, err
	}
	for _, s := range append(slices.Clone(st.audio), m.unreportedAudioStreams(t, st.audio)...) {
		if s.audioStreamID == id {
			return &s, nil
		}
	}
	return nil, nil
}
