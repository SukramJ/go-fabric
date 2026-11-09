// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"context"
	"log/slog"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/SukramJ/go-fabric/cluster/spec"
	wpdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportprovider"
	wrdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportrequestor"
	"github.com/SukramJ/go-fabric/im"
)

// streamListMinRevision is the WebRtcTransportProvider ClusterRevision from
// which VideoStreams and AudioStreams replace the singular ids. Mirrors
// matterjs-server packages/ws-controller/src/controller/webRtcSessionStreams.ts:44-45
// at 0bd06f2.
const streamListMinRevision = 2

// offerStreams are the four stream fields of an offer request.
type offerStreams struct {
	videoStreams, audioStreams *[]uint16
	videoID, audioID           *spec.Nullable[uint16]
}

func streamsOf(fields spec.Encodable) *offerStreams {
	switch f := fields.(type) {
	case wpdef.ProvideOfferRequest:
		return &offerStreams{videoStreams: f.VideoStreams, audioStreams: f.AudioStreams, videoID: f.VideoStreamId, audioID: f.AudioStreamId}
	case wpdef.SolicitOfferRequest:
		return &offerStreams{videoStreams: f.VideoStreams, audioStreams: f.AudioStreams, videoID: f.VideoStreamId, audioID: f.AudioStreamId}
	}
	return nil
}

func withStreams(fields spec.Encodable, s *offerStreams) spec.Encodable {
	switch f := fields.(type) {
	case wpdef.ProvideOfferRequest:
		f.VideoStreams, f.AudioStreams, f.VideoStreamId, f.AudioStreamId = s.videoStreams, s.audioStreams, s.videoID, s.audioID
		return f
	case wpdef.SolicitOfferRequest:
		f.VideoStreams, f.AudioStreams, f.VideoStreamId, f.AudioStreamId = s.videoStreams, s.audioStreams, s.videoID, s.audioID
		return f
	}
	return fields
}

// selectWebRTCStreamFields reconciles an offer's stream fields with the
// provider's revision: both forms is refused (the provider answers
// INVALID_COMMAND, §11.5.6.1, §11.5.6.3); an empty list is refused; below
// revision 2, or with the revision unknown, a single-entry list becomes
// the singular id and a longer one is refused. A refusal changes nothing.
// Mirrors matterjs-server packages/ws-controller/src/controller/webRtcSessionStreams.ts:52-104
// at 0bd06f2.
func selectWebRTCStreamFields(fields spec.Encodable, revision uint16, known bool) (spec.Encodable, error) {
	s := streamsOf(fields)
	if s == nil {
		return fields, nil
	}
	var listed, singular []string
	if s.videoStreams != nil {
		listed = append(listed, "videoStreams")
	}
	if s.audioStreams != nil {
		listed = append(listed, "audioStreams")
	}
	if s.videoID != nil {
		singular = append(singular, "videoStreamId")
	}
	if s.audioID != nil {
		singular = append(singular, "audioStreamId")
	}
	if len(listed) > 0 && len(singular) > 0 {
		return nil, invalidArguments("%s cannot be sent together with %s: the WebRTC provider refuses a request that states both the stream lists and the stream ids they deprecate. Send one form for both media kinds.",
			strings.Join(listed, " and "), strings.Join(singular, " and "))
	}
	type entry struct {
		list, kind string
		ids        *[]uint16
		id         **spec.Nullable[uint16]
	}
	entries := []entry{{"videoStreams", "video", s.videoStreams, &s.videoID}, {"audioStreams", "audio", s.audioStreams, &s.audioID}}
	for _, e := range entries {
		if e.ids != nil && len(*e.ids) == 0 {
			return nil, invalidArguments("%s names no stream. Leave it out to ask for no %s stream.", e.list, e.kind)
		}
	}
	if known && revision >= streamListMinRevision {
		return fields, nil
	}
	why := "this controller has not read this camera's WebRTC Provider ClusterRevision"
	if known {
		why = "this camera's WebRTC Provider states cluster revision " + strconv.Itoa(int(revision))
	}
	for _, e := range entries {
		if e.ids != nil && len(*e.ids) > 1 {
			return nil, invalidArguments("%s names %d streams, and %s, so this request can carry one %s stream at most. Ask for one %s stream.", e.list, len(*e.ids), why, e.kind, e.kind)
		}
	}
	out := *s
	if s.videoStreams != nil {
		out.videoID, out.videoStreams = &spec.Nullable[uint16]{Value: (*s.videoStreams)[0]}, nil
	}
	if s.audioStreams != nil {
		out.audioID, out.audioStreams = &spec.Nullable[uint16]{Value: (*s.audioStreams)[0]}, nil
	}
	return withStreams(fields, &out), nil
}

// resolveWebRTCSessionStreams is a session's streams: a non-empty list
// verbatim; a stated id; a null id the provider's echo; nil when the
// request left the kind out. Mirrors matterjs-server
// packages/ws-controller/src/controller/webRtcSessionStreams.ts:24-39 at
// 0bd06f2.
func resolveWebRTCSessionStreams(list *[]uint16, requestID, responseID *spec.Nullable[uint16]) []uint16 {
	if list != nil && len(*list) > 0 {
		return slices.Clone(*list)
	}
	if requestID != nil && !requestID.Null {
		return []uint16{requestID.Value}
	}
	if requestID != nil && requestID.Null && responseID != nil && !responseID.Null {
		return []uint16{responseID.Value}
	}
	return nil
}

// tlsICEScheme is an ICE URL that needs a valid clock for its certificate
// check. Mirrors matterjs-server
// packages/ws-controller/src/controller/iceServerClock.ts:13 at 0bd06f2.
var tlsICEScheme = regexp.MustCompile(`(?i)^(turns|stuns):`)

// offerChecksClock reports whether the camera checks its clock for the
// offer: a SolicitOffer, or a ProvideOffer for a new session, with a
// turns: or stuns: ICE server (§11.5.6.1.10, §11.5.6.3.12). Mirrors
// matterjs-server packages/ws-controller/src/controller/iceServerClock.ts:16-40
// at 0bd06f2.
func offerChecksClock(fields spec.Encodable) bool {
	var servers *[]wpdef.ICEServerStruct
	switch f := fields.(type) {
	case wpdef.ProvideOfferRequest:
		if !f.WebRtcSessionId.Null {
			return false
		}
		servers = f.IceServers
	case wpdef.SolicitOfferRequest:
		servers = f.IceServers
	default:
		return false
	}
	if servers == nil {
		return false
	}
	for _, s := range *servers {
		if slices.ContainsFunc(s.Urls, tlsICEScheme.MatchString) {
			return true
		}
	}
	return false
}

// offerWithClockRetry sends an offer; when the camera checks its clock for
// it and refuses with INVALID_IN_STATE, it pushes time once and sends the
// same offer once more. A failed push, or a second offer failing without a
// device status, returns the first refusal; a second device refusal is
// returned. Mirrors matterjs-server
// packages/ws-controller/src/controller/iceServerClock.ts:42-93 at 0bd06f2.
func offerWithClockRetry(ctx context.Context, fields spec.Encodable, invoke func(context.Context) (any, error), syncTime func(context.Context) error, log *slog.Logger) (any, error) {
	resp, refusal := invoke(ctx)
	if refusal == nil || !statusIs(refusal, im.StatusInvalidInState) || !offerChecksClock(fields) {
		return resp, refusal
	}
	if err := syncTime(ctx); err != nil {
		log.Info("camera.offer.clock_push_failed", slog.Any("err", err))
		return nil, refusal
	}
	resp, err := invoke(ctx)
	if err != nil {
		if _, ok := deviceStatusOf(err); ok {
			return nil, err
		}
		return nil, refusal
	}
	return resp, nil
}

// providerKey is a session with the provider endpoint it runs on, the
// address every route uses for it. Mirrors matterjs-server
// packages/ws-controller/src/controller/webRtcSessionStreams.ts:136-143 at
// 0bd06f2.
type providerKey struct {
	id          uint16
	fabricIndex uint8
	peerNodeID  uint64
	peerEP      uint16
}

// providerNodes is what the session layer needs from the camera nodes.
// Mirrors matterjs-server packages/ws-controller/src/controller/WebRtcProviderSessions.ts:24-50
// at 0bd06f2.
type providerNodes interface {
	// providerRevision is the provider's ClusterRevision, known false
	// before it was read; an error when the endpoint has no provider.
	providerRevision(t target) (revision uint16, known bool, err error)
	invokeProvider(ctx context.Context, t target, command string, fields spec.Encodable) (any, error)
	syncTime(ctx context.Context, nodeID uint64) error
}

// requestorTable is the controller's WebRtcTransportRequestor: its
// endpoint, the OriginatingEndpointID of every offer, and its session
// table.
type requestorTable interface {
	endpointID() uint16
	sessions() []wrdef.WebRTCSessionStruct
	upsert(wrdef.WebRTCSessionStruct) error
	remove(wrdef.SessionKey)
}

// providerSessions is this controller's side of the WebRTC sessions it
// holds with cameras: it establishes them, keeps the local requestor's
// records and the offers the answers are checked against. Mirrors
// matterjs-server packages/ws-controller/src/controller/WebRtcProviderSessions.ts:79-278
// at 0bd06f2.
type providerSessions struct {
	nodes       providerNodes
	requestor   requestorTable
	fabricIndex uint8
	watch       *sessionWatch
	log         *slog.Logger
	// cleanup is the context a cleanup end runs under: detached, within
	// the cleanup budget.
	cleanup func(context.Context) (context.Context, context.CancelFunc)

	mu     sync.Mutex
	offers map[providerKey]string
}

func (p *providerSessions) key(t target, id uint16) providerKey {
	return providerKey{id: id, fabricIndex: p.fabricIndex, peerNodeID: t.nodeID, peerEP: t.endpointID}
}

// offerSent records the SDP sent for the session, so the camera's answer
// can be narrowed to it; the returned function withdraws it.
func (p *providerSessions) offerSent(k providerKey, sdp string) func() {
	p.mu.Lock()
	p.offers[k] = sdp
	p.mu.Unlock()
	return func() {
		p.mu.Lock()
		if p.offers[k] == sdp {
			delete(p.offers, k)
		}
		p.mu.Unlock()
	}
}

// offerOf is the recorded offer of a session.
func (p *providerSessions) offerOf(k providerKey) (string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	sdp, ok := p.offers[k]
	return sdp, ok
}

// sessionEnded drops the recorded offer of a session that ended.
func (p *providerSessions) sessionEnded(k providerKey) {
	p.mu.Lock()
	delete(p.offers, k)
	p.mu.Unlock()
}

// trackedSessionOf is the requestor's entry for the session on the
// endpoint. Mirrors matterjs-server
// packages/ws-controller/src/controller/webRtcSessionStreams.ts:145-157 at
// 0bd06f2.
func trackedSessionOf(sessions []wrdef.WebRTCSessionStruct, k providerKey) (wrdef.WebRTCSessionStruct, bool) {
	for _, s := range sessions {
		if s.Id == k.id && s.FabricIndex == k.fabricIndex && s.PeerNodeId == k.peerNodeID && s.PeerEndpointId == k.peerEP {
			return s, true
		}
	}
	return wrdef.WebRTCSessionStruct{}, false
}

func (p *providerSessions) tracks(k providerKey) bool {
	_, ok := trackedSessionOf(p.requestor.sessions(), k)
	return ok
}

// establish sends a ProvideOffer or SolicitOffer and stores the session
// with the local requestor; while it runs, the leftover sweep skips the
// endpoint. establishing is called with the id before the requestor holds
// the session. Mirrors matterjs-server
// packages/ws-controller/src/controller/WebRtcProviderSessions.ts:181-233
// at 0bd06f2.
func (p *providerSessions) establish(ctx context.Context, t target, fields spec.Encodable, establishing func(uint16)) (any, error) {
	revision, known, err := p.nodes.providerRevision(t)
	if err != nil {
		return nil, err
	}
	origin := p.requestor.endpointID()
	switch f := fields.(type) {
	case wpdef.ProvideOfferRequest:
		f.OriginatingEndpointId = &origin
		fields = f
	case wpdef.SolicitOfferRequest:
		f.OriginatingEndpointId = origin
		fields = f
	}
	if fields, err = selectWebRTCStreamFields(fields, revision, known); err != nil {
		return nil, err
	}
	run := func() (any, error) {
		if po, ok := fields.(wpdef.ProvideOfferRequest); ok && !po.WebRtcSessionId.Null {
			return p.reOffer(ctx, t, po)
		}
		return p.newSession(ctx, t, fields, establishing)
	}
	if p.watch == nil {
		return run()
	}
	return establishingDo(p.watch, t, run)
}

// send invokes an establishing command with the clock retry.
func (p *providerSessions) send(ctx context.Context, t target, fields spec.Encodable) (any, error) {
	command := callSolicitOffer
	if _, ok := fields.(wpdef.ProvideOfferRequest); ok {
		command = callProvideOffer
	}
	return offerWithClockRetry(ctx, fields,
		func(ctx context.Context) (any, error) { return p.nodes.invokeProvider(ctx, t, command, fields) },
		func(ctx context.Context) error { return p.nodes.syncTime(ctx, t.nodeID) }, p.log)
}

// sessionIDOf is the id and echoed stream ids of an establishing
// command's response.
func sessionIDOf(resp any) (id uint16, video, audio *spec.Nullable[uint16], ok bool) {
	switch r := resp.(type) {
	case wpdef.ProvideOfferResponse:
		return r.WebRtcSessionId, r.VideoStreamId, r.AudioStreamId, true
	case wpdef.SolicitOfferResponse:
		return r.WebRtcSessionId, r.VideoStreamId, r.AudioStreamId, true
	}
	return 0, nil, nil, false
}

// isTrackableWebRTCSession reports whether a session can be stored in the
// WebRTCSessionStruct: a stream usage (mandatory) and a video or audio
// stream (choice a, min 1). Mirrors matterjs-server
// packages/ws-controller/src/controller/webRtcSessionStreams.ts:106-124 at
// 0bd06f2.
func isTrackableWebRTCSession(usage *wpdef.StreamUsageEnum, video, audio []uint16) bool {
	return usage != nil && (video != nil || audio != nil)
}

// trackableSession is the requestor entry for the session, false when the
// request lacks what the struct requires: a stream usage and a video or
// audio stream. Mirrors matterjs-server
// packages/ws-controller/src/controller/webRtcSessionStreams.ts:339-364 at
// 0bd06f2.
func (p *providerSessions) trackableSession(t target, fields spec.Encodable, resp any) (wrdef.WebRTCSessionStruct, bool) {
	id, rv, ra, _ := sessionIDOf(resp)
	s := streamsOf(fields)
	var usage *wpdef.StreamUsageEnum
	metadata := false
	switch f := fields.(type) {
	case wpdef.ProvideOfferRequest:
		usage = f.StreamUsage
		metadata = f.MetadataEnabled != nil && *f.MetadataEnabled
	case wpdef.SolicitOfferRequest:
		u := f.StreamUsage
		usage = &u
		metadata = f.MetadataEnabled != nil && *f.MetadataEnabled
	}
	video := resolveWebRTCSessionStreams(s.videoStreams, s.videoID, rv)
	audio := resolveWebRTCSessionStreams(s.audioStreams, s.audioID, ra)
	if !isTrackableWebRTCSession(usage, video, audio) {
		return wrdef.WebRTCSessionStruct{}, false
	}
	out := wrdef.WebRTCSessionStruct{Id: id, PeerNodeId: t.nodeID, PeerEndpointId: t.endpointID, StreamUsage: wrdef.StreamUsageEnum(*usage), MetadataEnabled: metadata, FabricIndex: p.fabricIndex}
	if video != nil {
		out.VideoStreams = &video
	}
	if audio != nil {
		out.AudioStreams = &audio
	}
	return out, true
}

// endWithinBudget ends a session the layer cannot track, with
// OutOfResources, within the cleanup budget. Mirrors matterjs-server
// packages/ws-controller/src/controller/webRtcSessionStreams.ts:193-215 at
// 0bd06f2.
func (p *providerSessions) endWithinBudget(ctx context.Context, cleanup func(context.Context) (context.Context, context.CancelFunc), t target, id uint16) {
	cctx, cancel := cleanup(ctx)
	defer cancel()
	if _, err := p.nodes.invokeProvider(cctx, t, callEndSession, wpdef.EndSessionRequest{WebRtcSessionId: id, Reason: endReasonOutOfResources}); err != nil {
		p.log.Warn("camera.session.cleanup_failed", slog.Int("webrtc_session_id", int(id)), slog.Any("err", err))
	}
}

// newSession establishes a session: the id goes to establishing, the
// offer is recorded before the requestor holds the session, and a session
// that cannot be tracked or stored is ended. Mirrors matterjs-server
// packages/ws-controller/src/controller/webRtcSessionStreams.ts:270-317 at
// 0bd06f2.
//
// The requestor entry is derived before establishing runs, so that
// establishing, the recorded offer and the requestor's upsert follow one
// another with nothing else on the response in between: from establishing
// on, the camera's Answer may already be on its way, and the served
// requestor answers NOT_FOUND to it until it holds the session
// (cluster/camera Requestor.findSessionStrict; matter.js
// packages/node/src/behaviors/web-rtc-transport-requestor/WebRtcTransportRequestorServer.ts:170-182
// at db021da0). This is the contract matterjs-server's registry states —
// "the local requestor accepts the camera's signalling from then on"
// (packages/ws-controller/src/camera/sessionRegistry.ts:283-296 at 0bd06f2)
// — held where Go's scheduling, unlike Node's, lets the Answer's handler
// run while this goroutine still works on the response (issue 464). The
// window before establishing — the response decoded — is
// notes/parity/matter_behaviour_findings.md's WebRTC decode-window row.
func (p *providerSessions) newSession(ctx context.Context, t target, fields spec.Encodable, establishing func(uint16)) (any, error) {
	resp, err := p.send(ctx, t, fields)
	if err != nil {
		return nil, err
	}
	id, _, _, ok := sessionIDOf(resp)
	if !ok {
		return nil, stackError("the offer did not return a WebRTCSessionID for node 0x%016X", t.nodeID)
	}
	session, trackable := p.trackableSession(t, fields, resp)
	if establishing != nil {
		establishing(id)
	}
	if !trackable {
		p.endWithinBudget(ctx, p.cleanup, t, id)
		return nil, stackError("the offer for node 0x%016X produced a session with no stream usage or video/audio stream; deferred/auto-select streaming is not supported", t.nodeID)
	}
	var withdraw func()
	if po, ok := fields.(wpdef.ProvideOfferRequest); ok {
		withdraw = p.offerSent(p.key(t, id), po.Sdp)
	}
	if err := p.requestor.upsert(session); err != nil {
		if withdraw != nil {
			withdraw()
		}
		p.endWithinBudget(ctx, p.cleanup, t, id)
		return nil, err
	}
	return resp, nil
}

// reOffer is a ProvideOffer naming a session the camera holds: it never
// ends a session nor overwrites a requestor entry; it adds one only when
// absent and trackable. The offer is withdrawn when the camera refuses it
// or the requestor does not hold the session. Mirrors matterjs-server
// packages/ws-controller/src/controller/webRtcSessionStreams.ts:240-268 at
// 0bd06f2.
func (p *providerSessions) reOffer(ctx context.Context, t target, fields wpdef.ProvideOfferRequest) (any, error) {
	k := p.key(t, fields.WebRtcSessionId.Value)
	withdraw := p.offerSent(k, fields.Sdp)
	resp, err := p.send(ctx, t, fields)
	if err != nil {
		withdraw()
		return nil, err
	}
	if _, _, _, ok := sessionIDOf(resp); !ok {
		withdraw()
		return nil, stackError("ProvideOffer did not return a WebRTCSessionID for node 0x%016X", t.nodeID)
	}
	if session, ok := p.trackableSession(t, fields, resp); ok && !p.tracks(k) {
		if err := p.requestor.upsert(session); err != nil {
			p.log.Warn("camera.reoffer.untracked", slog.Int("webrtc_session_id", int(k.id)), slog.Any("err", err))
		}
	}
	if !p.tracks(k) {
		withdraw()
	}
	return resp, nil
}

// removeTracked drops a session this controller ended from the requestor
// and the recorded offers. Mirrors matterjs-server
// packages/ws-controller/src/controller/WebRtcProviderSessions.ts:235-258
// at 0bd06f2.
func (p *providerSessions) removeTracked(t target, id uint16) {
	k := p.key(t, id)
	if s, ok := trackedSessionOf(p.requestor.sessions(), k); ok {
		p.requestor.remove(wrdef.KeyOf(s))
	}
	p.sessionEnded(k)
}

// abandon stops tracking a session this controller gave up and could not
// end; the next batch end sweeps the endpoint. Mirrors matterjs-server
// packages/ws-controller/src/controller/WebRtcProviderSessions.ts:260-267
// at 0bd06f2.
func (p *providerSessions) abandon(t target, id uint16) {
	p.removeTracked(t, id)
	if p.watch != nil {
		p.watch.sweepAgain(t.nodeID, t.endpointID)
	}
}

// nodeRemoved forgets a node and drops its requestor sessions. Mirrors
// matterjs-server packages/ws-controller/src/controller/WebRtcProviderSessions.ts:157-176
// at 0bd06f2.
func (p *providerSessions) nodeRemoved(nodeID uint64) {
	if p.watch != nil {
		p.watch.nodeRemoved(nodeID)
	}
	for _, s := range p.requestor.sessions() {
		if s.PeerNodeId == nodeID && s.FabricIndex == p.fabricIndex {
			p.requestor.remove(wrdef.KeyOf(s))
			p.sessionEnded(providerKey{id: s.Id, fabricIndex: s.FabricIndex, peerNodeID: s.PeerNodeId, peerEP: s.PeerEndpointId})
		}
	}
}

// requestorSessionIDs are the ids of the provider's sessions the requestor
// holds on the endpoint.
func (p *providerSessions) requestorSessionIDs(nodeID uint64, endpointID uint16) map[uint16]bool {
	out := map[uint16]bool{}
	for _, s := range p.requestor.sessions() {
		if s.PeerNodeId == nodeID && s.FabricIndex == p.fabricIndex && s.PeerEndpointId == endpointID {
			out[s.Id] = true
		}
	}
	return out
}
