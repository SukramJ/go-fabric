// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"context"
	"log/slog"
	"slices"

	"github.com/SukramJ/go-fabric/cluster/spec"
	csdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavstreammanagement"
	wpdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportprovider"
)

// Mode is how a session's SDP exchange runs.
type Mode string

// The two modes, spelled as matterjs-server's wire spells them.
const (
	// ModeProvideOffer: the controller sent the host's offer; the camera
	// answers with an Answer signal.
	ModeProvideOffer Mode = "provide_offer"
	// ModeSolicitOffer: the controller asked the camera for an offer; the
	// camera sends an Offer signal, and the host answers with
	// [Session.ProvideAnswer].
	ModeSolicitOffer Mode = "solicit_offer"
)

// startArgs is one session start. Mirrors matterjs-server
// packages/ws-controller/src/camera/CameraStreamManager.ts:537-563 at
// 0bd06f2.
type startArgs struct {
	t            target
	connectionID string
	usage        csdef.StreamUsageEnum
	sdp          *string
	videoState   trackState
	video        *videoHints
	videoCodecs  []string
	audioState   trackState
	audio        *audioHints
	iceServers   *[]wpdef.ICEServerStruct
	icePolicy    *string
	metadata     bool
	// allowEviction defaults to true in the request; here it is stated.
	allowEviction bool
	talkback      bool
	// establishing hears the id and the identity of the session the
	// registry knows under it.
	establishing     func(sessionID uint16, life *lifecycle)
	clientSessionKey string
}

// startResult is a started session. talkback: talkback was asked for, the
// offer's audio can send, the camera states half or full duplex, and the
// session carries an audio stream. Mirrors matterjs-server
// packages/ws-controller/src/camera/CameraStreamManager.ts:580-591 at
// 0bd06f2.
type startResult struct {
	sessionID uint16
	mode      Mode
	video     *resolvedStream
	audio     *resolvedStream
	talkback  bool
}

// refusalReason words why a registration was refused. Mirrors
// matterjs-server packages/ws-controller/src/camera/CameraStreamManager.ts:112-117
// at 0bd06f2.
func refusalReason(r trackRefusal) string {
	switch r {
	case refusalClaimed:
		return "the requesting owner was released or the service closed"
	case refusalStopped:
		return "the start was stopped by its key"
	default:
		return ""
	}
}

// cancelledBeforeOffer is the failure of a start stopped before its offer.
// Mirrors matterjs-server packages/ws-controller/src/camera/CameraStreamManager.ts:119-124
// at 0bd06f2.
func cancelledBeforeOffer(r trackRefusal) error {
	reason := refusalReason(r)
	if reason == "" {
		reason = "it ended"
	}
	return stackError("the start was cancelled before the offer: %s", reason)
}

// givenUpEnd is the answer to the EndSession a start sent for the session
// it gave up; answered false is no answer within the cleanup budget.
type givenUpEnd struct {
	answered bool
	ended    bool
	err      error
}

// givenUpSessionError is the failure of a start that gave up the session
// the camera made; it says whether the camera confirmed the end. Mirrors
// matterjs-server packages/ws-controller/src/camera/CameraStreamManager.ts:125-151
// at 0bd06f2.
func givenUpSessionError(r trackRefusal, id uint16, end givenUpEnd) error {
	reason := refusalReason(r)
	if !end.answered || end.err != nil {
		failure := "the camera did not answer EndSession within the cleanup budget"
		if end.err != nil {
			failure = "EndSession failed (" + end.err.Error() + ")"
		}
		why := reason
		if why == "" {
			why = "it ended before this controller could track it"
		}
		return stackError("WebRTC session %d was given up (%s), but %s; the controller ends it at the camera's next report if the camera still lists it", id, why, failure)
	}
	if reason == "" {
		return stackError("WebRTC session %d was ended before this controller could track it, so it is no longer usable", id)
	}
	if !end.ended {
		return stackError("WebRTC session %d was given up: %s while the camera was establishing it; the camera no longer held it (EndSession answered NOT_FOUND)", id, reason)
	}
	return stackError("WebRTC session %d was ended: %s while the camera was establishing it", id, reason)
}

// stoppedBy is the end of a session another owner stopped. Mirrors
// matterjs-server packages/ws-controller/src/camera/CameraStreamManager.ts:153-156
// at 0bd06f2.
func stoppedBy(requestedBy string) sessionEnd {
	return sessionEnd{cause: endOtherConnection, requestedBy: requestedBy}
}

// startStream resolves the streams and establishes the session, under one
// endpoint lock through the offer (ReferenceCount rises only at session
// establishment, so another request could evict a stream before it), one
// registration and one allocation scope. Mirrors matterjs-server
// packages/ws-controller/src/camera/CameraStreamManager.ts:1955-1995 at
// 0bd06f2.
func (m *manager) startStream(ctx context.Context, args startArgs) (*startResult, error) {
	var sdp *sdpVideoConstraints
	offer := args.sdp
	if args.sdp != nil {
		parsed := parseSdpVideoConstraints(*args.sdp)
		sdp = &parsed
		if !args.talkback {
			rewritten := withoutTalkback(*args.sdp, parsed.sendingAudioSections)
			offer = &rewritten
		}
	}
	// Before the first wait, so a release from here on claims this
	// registration. The key check is part of it: two concurrent starts of
	// the connection cannot both register one key.
	pending, err := m.sessions.beginKeyed(args.t.nodeID, args.t.endpointID, args.connectionID, args.clientSessionKey)
	if err != nil {
		return nil, err
	}
	defer m.sessions.finish(pending)
	return withEndpointLock(ctx, m, args.t, func() (*startResult, error) {
		if err := m.throwIfClaimed(pending); err != nil {
			return nil, err
		}
		st, err := m.requireStreamingState(ctx, args.t)
		if err != nil {
			return nil, err
		}
		return withAllocationScope(ctx, m, func(scope *allocationScope) (*startResult, error) {
			return m.establishSession(ctx, pending, args, offer, st, sdp, scope)
		})
	})
}

// throwIfClaimed is checked once the endpoint is ours, after the video
// stream and before the offer. Mirrors matterjs-server
// packages/ws-controller/src/camera/CameraStreamManager.ts:2173-2185 at
// 0bd06f2.
func (m *manager) throwIfClaimed(p *pendingSession) error {
	if r := m.sessions.claimRefusal(p); r != trackAccepted {
		return cancelledBeforeOffer(r)
	}
	return nil
}

// establishSession is startStream's body, inside the lock, the
// registration and the scope; every effect is registered with the scope
// where it happens. Mirrors matterjs-server
// packages/ws-controller/src/camera/CameraStreamManager.ts:1992-2171 at
// 0bd06f2.
func (m *manager) establishSession( //nolint:gocyclo // one function, as the source is: video, audio, offer, track in order
	ctx context.Context, pending *pendingSession, args startArgs, offer *string, st *cameraState, sdp *sdpVideoConstraints, scope *allocationScope,
) (*startResult, error) {
	t := args.t
	talkbackOffered := args.talkback && sdp != nil && len(sdp.sendingAudioSections) > 0
	cameraTalks := st.twoWayTalkSupport != nil && (*st.twoWayTalkSupport == csdef.TwoWayTalkSupportTypeHalfDuplex || *st.twoWayTalkSupport == csdef.TwoWayTalkSupportTypeFullDuplex)
	if talkbackOffered && !cameraTalks {
		m.log.Info("camera.talkback.unsupported", slog.Uint64("node_id", t.nodeID), slog.Int("endpoint", int(t.endpointID)))
	}
	var video, audio *resolvedStream
	if args.videoState != trackDeclined {
		requested := []string{}
		if args.videoCodecs != nil {
			requested = slices.Clone(args.videoCodecs)
		}
		var o trackOutcome
		switch {
		case func() bool { _, refused := mediaRefusal(sdp, "video"); return refused }():
			// A stream the peer cannot receive would hold an encoder for
			// nothing.
			o.unavailable = &IncompatibleError{Reason: ReasonOffer, Track: TrackVideo, Device: []string{}, Requested: requested}
		case lacksFeature(st.features, csdef.FeatureVideo):
			o.unavailable = &IncompatibleError{Reason: ReasonFeature, Track: TrackVideo, Feature: featureName(csdef.FeatureVideo), Device: []string{}, Requested: requested}
		default:
			codecs := make([]csdef.VideoCodecEnum, len(st.rateDistortionPoints))
			for i, p := range st.rateDistortionPoints {
				codecs[i] = p.codec
			}
			codec, err := preferredVideoCodec(codecs, sdp, args.videoCodecs)
			if err != nil {
				return nil, err
			}
			s, err := m.resolveVideoStreamLocked(ctx, videoRequest{t: t, usage: args.usage, limits: videoCodecLimitsOf(sdp, codec), hints: args.video, allowEviction: args.allowEviction}, scope)
			if err != nil {
				return nil, err
			}
			o.stream = s
		}
		var err error
		if video, err = m.resolveTrack(TrackVideo, t.nodeID, args.videoState, o); err != nil {
			return nil, err
		}
	}
	if err := m.throwIfClaimed(pending); err != nil {
		return nil, err
	}
	if args.audioState != trackDeclined {
		o, err := m.resolveAudioStreamLocked(ctx, audioRequest{t: t, usage: args.usage, sdp: sdp, hints: args.audio}, scope)
		if err != nil {
			return nil, err
		}
		if audio, err = m.resolveTrack(TrackAudio, t.nodeID, args.audioState, o); err != nil {
			return nil, err
		}
	}
	if talkbackOffered && cameraTalks && audio == nil {
		m.log.Info("camera.talkback.no_audio", slog.Uint64("node_id", t.nodeID), slog.Int("endpoint", int(t.endpointID)))
	}
	if video == nil && audio == nil {
		return nil, &IncompatibleError{Reason: ReasonNoMedia, Device: []string{}, Requested: []string{}}
	}
	if err := m.throwIfClaimed(pending); err != nil {
		return nil, err
	}
	id, err := m.invokeEstablishingOffer(ctx, pending, args, offer, video, audio)
	if err != nil {
		return nil, err
	}
	session := managedSession{webRTCSessionID: id, nodeID: t.nodeID, endpointID: t.endpointID, connectionID: args.connectionID, clientSessionKey: args.clientSessionKey}
	switch refusal := m.sessions.track(pending, session); refusal {
	case trackAccepted:
	case refusalSuperseded:
		return nil, stackError("WebRTC session %d ended before this controller could track it, and the camera gave its id to another session", id)
	default:
		return nil, m.giveUp(ctx, pending, t, id, refusal)
	}
	mode := ModeProvideOffer
	if args.sdp == nil {
		mode = ModeSolicitOffer
	}
	return &startResult{sessionID: id, mode: mode, video: video, audio: audio, talkback: talkbackOffered && cameraTalks && audio != nil}, nil
}

// giveUp ends the session a start may not keep, through the registration,
// so a stop by id already in flight is joined instead of sent twice; an
// end that failed or was not answered hands the session to the leftover
// sweep. Mirrors matterjs-server
// packages/ws-controller/src/camera/CameraStreamManager.ts:2117-2160 at
// 0bd06f2.
func (m *manager) giveUp(ctx context.Context, pending *pendingSession, t target, id uint16, refusal trackRefusal) error {
	answer := newOutcome[bool]()
	m.sessions.ending(pending, answer)
	cctx, cancel := m.cleanupContext(ctx)
	ended, err := m.sessions.stop(cctx, t.nodeID, t.endpointID, id, sessionEnd{cause: endReleased}, m.sendEndSession(t, id))
	timedOut := cctx.Err() != nil && err != nil
	cancel()
	end := givenUpEnd{answered: !timedOut, ended: ended, err: err}
	if timedOut {
		end.err = nil
		answer.resolve(false, stackError("the camera did not answer EndSession for WebRTC session %d within the cleanup budget", id))
	} else {
		answer.resolve(ended, err)
	}
	if !end.answered || end.err != nil {
		actx, acancel := m.cleanupContext(ctx)
		if aerr := m.io.abandonSession(actx, t, id); aerr != nil {
			m.log.Warn("camera.session.abandon_failed", slog.Int("webrtc_session_id", int(id)), slog.Any("err", aerr))
		}
		acancel()
	}
	return givenUpSessionError(refusal, id, end)
}

// invokeEstablishingOffer sends the SolicitOffer or ProvideOffer that
// establishes the session, with privacy refusals typed; the id is recorded
// before the local requestor can route an End for it. Mirrors
// matterjs-server packages/ws-controller/src/camera/CameraStreamManager.ts:1918-1953
// at 0bd06f2.
func (m *manager) invokeEstablishingOffer(ctx context.Context, pending *pendingSession, args startArgs, offer *string, video, audio *resolvedStream) (uint16, error) {
	usage := wpdef.StreamUsageEnum(args.usage)
	metadata := args.metadata
	var videoStreams, audioStreams *[]uint16
	if video != nil {
		videoStreams = &[]uint16{video.streamID}
	}
	if audio != nil {
		audioStreams = &[]uint16{audio.streamID}
	}
	call := deviceCall{target: args.t, cluster: clusterProvider, establishing: func(id uint16) *lifecycle {
		life := m.sessions.establishing(pending, id)
		if args.establishing != nil {
			args.establishing(id, life)
		}
		return life
	}}
	if offer == nil {
		call.command = callSolicitOffer
		call.fields = wpdef.SolicitOfferRequest{StreamUsage: usage, VideoStreams: videoStreams, AudioStreams: audioStreams, IceServers: args.iceServers, IceTransportPolicy: args.icePolicy, MetadataEnabled: &metadata}
	} else {
		// §11.5.6.3.1: mandatory and nullable; null asks for a new session.
		call.command = callProvideOffer
		call.fields = wpdef.ProvideOfferRequest{WebRtcSessionId: spec.Nullable[uint16]{Null: true}, Sdp: *offer, StreamUsage: &usage, VideoStreams: videoStreams, AudioStreams: audioStreams, IceServers: args.iceServers, IceTransportPolicy: args.icePolicy, MetadataEnabled: &metadata}
	}
	resp, err := m.io.invoke(ctx, call)
	if err != nil {
		return 0, m.privacyFailure(ctx, args.t, err, func(p privacyState) []PrivacyMode { return sessionPrivacyModes(p, args.usage) })
	}
	switch r := resp.(type) {
	case wpdef.ProvideOfferResponse:
		return r.WebRtcSessionId, nil
	case wpdef.SolicitOfferResponse:
		return r.WebRtcSessionId, nil
	default:
		// Without an id the session can never be ended.
		return 0, stackError("the provider returned no WebRTCSessionID")
	}
}

// stopStreamByKey stops the session the owner named key; a session still
// being established is cancelled, and this answers once that start
// finished. false for a key that names no session of the owner. Mirrors
// matterjs-server packages/ws-controller/src/camera/CameraStreamManager.ts:2213-2230
// at 0bd06f2.
func (m *manager) stopStreamByKey(ctx context.Context, t target, owner, key string) (bool, error) {
	keyed, ok := m.sessions.byClientKey(t.nodeID, t.endpointID, owner, key)
	if !ok {
		return false, nil
	}
	if keyed.pending != nil {
		return m.sessions.cancel(keyed.pending)(ctx)
	}
	return m.stopStream(ctx, t, keyed.session.webRTCSessionID, owner)
}

// clientSessionID is the id of the tracked session the owner named key;
// false while it is still being established. Mirrors matterjs-server
// packages/ws-controller/src/camera/CameraStreamManager.ts:2232-2241 at
// 0bd06f2.
func (m *manager) clientSessionID(t target, owner, key string) (uint16, bool) {
	keyed, ok := m.sessions.byClientKey(t.nodeID, t.endpointID, owner, key)
	if !ok || keyed.session == nil {
		return 0, false
	}
	return keyed.session.webRTCSessionID, true
}

// stopStream ends the session on the camera; the allocation is kept. false
// when the camera answers NotFound; a failed EndSession is returned, also
// when this call joined one another path sent. An id no entry names is
// sent all the same. requestedBy "" is a release. Mirrors matterjs-server
// packages/ws-controller/src/camera/CameraStreamManager.ts:2243-2269 at
// 0bd06f2.
func (m *manager) stopStream(ctx context.Context, t target, id uint16, requestedBy string) (bool, error) {
	end := sessionEnd{cause: endReleased}
	if requestedBy != "" {
		end = stoppedBy(requestedBy)
	}
	return m.sessions.stop(ctx, t.nodeID, t.endpointID, id, end, m.sendEndSession(t, id))
}

// endSessionForClient is a client's own EndSession through the raw route:
// send invokes it and its error is passed on; the end is announced as
// stopStream does. Mirrors matterjs-server
// packages/ws-controller/src/camera/CameraStreamManager.ts:2303-2321 at
// 0bd06f2.
func (m *manager) endSessionForClient(ctx context.Context, t target, id uint16, requestedBy string, send func(context.Context) error) error {
	return m.sessions.endSession(ctx, t.nodeID, t.endpointID, id, stoppedBy(requestedBy), send)
}

// releaseConnection ends every session a released owner owned. Mirrors
// matterjs-server packages/ws-controller/src/camera/CameraStreamManager.ts:2327-2331
// at 0bd06f2.
func (m *manager) releaseConnection(ctx context.Context, owner string) {
	m.releaseSessions(ctx, func(s sessionScope) bool { return s.connectionID == owner })
}

// stopAll ends every tracked session regardless of owner, at shutdown.
// Mirrors matterjs-server packages/ws-controller/src/camera/CameraStreamManager.ts:2332-2336
// at 0bd06f2.
func (m *manager) stopAll(ctx context.Context) {
	m.releaseSessions(ctx, func(sessionScope) bool { return true })
}

// shutdown is the Service's Close: every start that registers from now on
// is refused with ErrClosed — one that passed StartStream's closed check
// just before Close would otherwise open a session nothing ends — and
// stopAll ends the rest (review II, R2-P1-cam-b-04). stopAll itself keeps
// the manager usable, as a ported case starts again after it
// (manager_slice_d_parity_matterjsserver_test.go).
func (m *manager) shutdown(ctx context.Context) {
	m.sessions.closeStarts()
	m.stopAll(ctx)
}

// releaseSessions ends every session in scope, including ones still being
// established (their starts end them; this waits for them), concurrently
// and within one cleanup budget. Mirrors matterjs-server
// packages/ws-controller/src/camera/CameraStreamManager.ts:2337-2361 at
// 0bd06f2.
func (m *manager) releaseSessions(ctx context.Context, matches func(sessionScope) bool) {
	claimed := m.sessions.claim(matches)
	if len(claimed.tracked) == 0 && len(claimed.starting) == 0 {
		return
	}
	cctx, cancel := m.cleanupContext(ctx)
	defer cancel()
	done := make(chan struct{}, len(claimed.tracked))
	for _, s := range claimed.tracked {
		go func() {
			defer func() { done <- struct{}{} }()
			if _, err := m.stopStream(cctx, target{s.nodeID, s.endpointID}, s.webRTCSessionID, ""); err != nil {
				m.log.Warn("camera.session.release_failed", slog.Int("webrtc_session_id", int(s.webRTCSessionID)), slog.Uint64("node_id", s.nodeID), slog.Any("err", err))
			}
		}()
	}
	for range claimed.tracked {
		select {
		case <-done:
		case <-cctx.Done():
			return
		}
	}
	for _, c := range claimed.starting {
		select {
		case <-c:
		case <-cctx.Done():
			return
		}
	}
}
