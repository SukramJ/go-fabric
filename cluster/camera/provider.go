// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"thingfabric.dev/go-fabric/internal/deepcopy"

	"thingfabric.dev/go-fabric/cluster"
	"thingfabric.dev/go-fabric/cluster/spec"
	wpdef "thingfabric.dev/go-fabric/cluster/spec/webrtctransportprovider"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/im"
)

// ProviderClusterID is the WebRtcTransportProvider cluster id.
const ProviderClusterID = wpdef.ClusterID

// Generated types the provider speaks.
type (
	// ProviderSession is an entry of the provider's CurrentSessions.
	ProviderSession = wpdef.WebRTCSessionStruct
	// ICEServer is an ICE server of an offer.
	ICEServer = wpdef.ICEServerStruct
	// ProviderPort is the camera's WebRTC stack.
	ProviderPort = wpdef.ProviderPort
	// OfferArgs is an offer the provider validated.
	OfferArgs = wpdef.OfferArgs
)

// MaxSessionID is the highest WebRTC session id (chip
// WebRTCTransportProviderCluster.cpp:42).
const MaxSessionID uint16 = 65534

// ErrNoProviderPort reports a WebRtcTransportProvider built without its
// port.
var ErrNoProviderPort = errors.New("camera: WebRtcTransportProvider needs a ProviderPort")

// ErrNegativeMaxSessions reports a WebRtcTransportProvider built with a
// negative [ProviderConfig.MaxSessions] or
// [ProviderConfig.MaxSessionsPerFabric].
var ErrNegativeMaxSessions = errors.New("camera: WebRtcTransportProvider session bound is negative")

// ProviderConfig carries the construction parameters of a [Provider].
type ProviderConfig struct {
	// Port is the camera's WebRTC stack; required.
	Port ProviderPort
	// Streams is the camera's CameraAvStreamManagement server; nil is a
	// camera without streams, whose every offer fails the stream checks.
	Streams StreamSource
	// UTCTimeKnown reports whether the node's UTCTime is not null — a
	// turns: or stuns: ICE server needs it. nil reports known, as the
	// camera app's IsUTCTimeNull (webrtc-provider-manager.cpp:830-836).
	UTCTimeKnown func() bool
	// MaxSessions bounds the provider's sessions, CurrentSessions entries
	// the host set included: a new session over it is refused before a
	// session id is taken and before the port is asked — ProvideOffer
	// answers RESOURCE_EXHAUSTED, SolicitOffer is answered with an id and
	// the requestor is then sent End with OutOfResources. The bound of the
	// camera app's delegate, kMaxConcurrentWebRTCSessions (5; connectedhomeip
	// examples/camera-app/linux/src/clusters/webrtc-provider/
	// webrtc-provider-manager.cpp:39 at 6170af84). 0 is no bound beyond the
	// session ids; negative is [ErrNegativeMaxSessions]. The refused
	// SolicitOffer answers DeferredOffer false (decision 621). Its End has
	// no deadline of its own: the PeerConnector's response timeouts bound
	// it, and the Ends wait behind one sender in a queue of at most the
	// bound; at worst a send that never returns holds that sender, and every
	// later over-bound SolicitOffer answers RESOURCE_EXHAUSTED (decision
	// 620).
	MaxSessions int
	// MaxSessionsPerFabric bounds the sessions of one fabric, counted over
	// the entries of the accessing fabric, the host's included, and refused
	// as [ProviderConfig.MaxSessions] is: this module's own bound, so one
	// fabric cannot take every session of the camera. 0 is no bound;
	// negative is [ErrNegativeMaxSessions].
	MaxSessionsPerFabric int
	// DataVersion is an optional host-owned tracker.
	//
	// Deprecated: a host-owned tracker adds nothing — the wire carries the
	// endpoint's DataVersion for the cluster, or the server's own where it
	// serves one (decision 165, F-db021da0-46); leave it nil. Removable in
	// v0.25.0.
	DataVersion *cluster.DataVersionTracker
}

// Provider implements [contract.ClusterServer] for WebRtcTransportProvider:
// the camera side of a WebRTC stream. connectedhomeip's server logic over
// the generated server — the five commands with chip's validation and
// statuses, the session table with the provider's own session ids, the
// requestor's node and fabric matched on every signal — while the
// [ProviderPort] runs the WebRTC stack. The streams are checked against
// the camera's [StreamSource]. Mirrors WebRTCTransportProviderCluster.cpp
// at 6170af84 (matter.js's server is the generated behaviour). The
// camera's own signals to the requestor — [Provider.Offer], [Provider.Answer],
// [Provider.IceCandidates], [Provider.End] — go through the
// [PeerConnector] the node attaches (provider_peer.go).
type Provider struct {
	*spec.Instance
	spec.SelfHeld

	srv  *spec.Server
	port ProviderPort
	cfg  ProviderConfig
	view streamView

	// op serialises the commands and the fabric removal: each holds it
	// from its checks through the port to its commit, as chip's handlers
	// run one at a time on its single-threaded stack. The host's methods
	// never take it, so a port may call them (review II, R2-P6-conc-1609).
	op sync.Mutex
	// pub serialises each change of the session table with its
	// publication, so no change is lost to a concurrent one and the table
	// and CurrentSessions agree (R2-P6-conc-1601); mu guards the state
	// below.
	pub      sync.Mutex
	mu       sync.Mutex
	sessions []ProviderSession
	nextID   uint16
	// peers reaches a session's requestor for the camera's signals
	// ([Provider.AttachPeerConnector]); nil until the node attaches one.
	peers PeerConnector
	// ends sends the End of a SolicitOffer refused at a session bound
	// (provider_cap.go).
	ends endQueue
}

// Compile-time assertions.
var (
	_ contract.ClusterServer                  = (*Provider)(nil)
	_ contract.ClusterDataVersion             = (*Provider)(nil)
	_ contract.ClusterAttributeLister         = (*Provider)(nil)
	_ contract.ClusterCommandLister           = (*Provider)(nil)
	_ contract.ClusterAttributeWritePrivilege = (*Provider)(nil)
	_ contract.ClusterCommandInvokePrivilege  = (*Provider)(nil)
	_ contract.AttributeChangeNotifier        = (*Provider)(nil)
	_ contract.FabricScopedReader             = (*Provider)(nil)
	_ spec.CommandRegistrar                   = (*Provider)(nil)
	_ wpdef.Host                              = (*Provider)(nil)
)

// NewProvider builds a WebRtcTransportProvider server with no session
// (chip's constructor takes no configuration, :216-219).
func NewProvider(cfg ProviderConfig) (*Provider, error) {
	if cfg.Port == nil {
		return nil, ErrNoProviderPort
	}
	if cfg.MaxSessions < 0 || cfg.MaxSessionsPerFabric < 0 {
		return nil, fmt.Errorf("%w: MaxSessions %d, MaxSessionsPerFabric %d", ErrNegativeMaxSessions, cfg.MaxSessions, cfg.MaxSessionsPerFabric)
	}
	p := &Provider{port: cfg.Port, cfg: cfg, view: streamView{cfg.Streams}}
	handlers := map[uint32]spec.CommandHandler{
		wpdef.CmdSolicitOffer:         serial(&p.op, p.solicitOffer),
		wpdef.CmdProvideOffer:         serial(&p.op, p.provideOffer),
		wpdef.CmdProvideAnswer:        serial(&p.op, p.provideAnswer),
		wpdef.CmdProvideIceCandidates: serial(&p.op, p.provideIceCandidates),
		wpdef.CmdEndSession:           serial(&p.op, p.endSession),
	}
	srv, err := spec.NewServer(wpdef.Definition, spec.Options{}, spec.ServerConfig{Handlers: handlers, DataVersion: cfg.DataVersion}) //nolint:staticcheck // the deprecated tracker is passed on for its window (decision 165)
	if err != nil {
		return nil, fmt.Errorf("camera: %w", err)
	}
	p.srv, p.Instance, p.SelfHeld = srv, srv.Instance, spec.NewSelfHeld(srv)
	if err := srv.SetAttributes(map[uint32]any{wpdef.AttrCurrentSessions: spec.List[ProviderSession]{}}); err != nil {
		return nil, fmt.Errorf("camera: %w", err)
	}
	return p, nil
}

// Sessions returns CurrentSessions, in the order the sessions were added:
// a deep copy, which the caller may change.
func (p *Provider) Sessions() []ProviderSession {
	p.mu.Lock()
	defer p.mu.Unlock()
	return deepcopy.Each(p.sessions)
}

// UpsertSession implements [wpdef.Host]. Mirrors chip UpsertSession
// (:308-330): the session of the id is replaced in place, else appended.
// A table the schema refuses is not stored; the error says why.
func (p *Provider) UpsertSession(session ProviderSession) error {
	return p.upsertSession(contract.OriginHostSet, session)
}

func (p *Provider) upsertSession(origin contract.Origin, session ProviderSession) error {
	session = session.Clone()
	return p.change(origin, func(next []ProviderSession) ([]ProviderSession, bool) {
		if i := slices.IndexFunc(next, func(s ProviderSession) bool { return s.Id == session.Id }); i >= 0 {
			next[i] = session
		} else {
			next = append(next, session)
		}
		return next, true
	})
}

// RemoveSession implements [wpdef.Host]. Mirrors chip RemoveSession
// (:332-348): only a removal reports a change.
func (p *Provider) RemoveSession(sessionID uint16) {
	_ = p.removeSession(contract.OriginHostSet, sessionID) // a shorter table the schema refused before cannot fail now
}

func (p *Provider) removeSession(origin contract.Origin, sessionID uint16) error {
	return p.change(origin, func(next []ProviderSession) ([]ProviderSession, bool) {
		kept := slices.DeleteFunc(next, func(s ProviderSession) bool { return s.Id == sessionID })
		return kept, len(kept) != len(next)
	})
}

// change applies edit to a copy of the session table and, when edit
// reports a change, publishes the result and keeps it — both under pub, so
// a concurrent change applies to this one's result; a table the schema
// refuses is neither published nor kept (R2-P6-err-1611). As
// [Requestor]'s change.
func (p *Provider) change(origin contract.Origin, edit func([]ProviderSession) ([]ProviderSession, bool)) error {
	p.pub.Lock()
	defer p.pub.Unlock()
	next, changed := edit(p.Sessions())
	if !changed {
		return nil
	}
	if next == nil {
		next = []ProviderSession{}
	}
	if err := p.srv.SetAttributesFrom(origin, map[uint32]any{wpdef.AttrCurrentSessions: spec.List[ProviderSession](next)}); err != nil {
		return fmt.Errorf("camera: CurrentSessions: %w", err)
	}
	p.mu.Lock()
	p.sessions = next
	p.mu.Unlock()
	return nil
}

// generateSessionID is chip's GenerateSessionId (:371-398): the next id
// no session holds, wrapping after 65534; RESOURCE_EXHAUSTED when all are
// in use.
func (p *Provider) generateSessionID() (uint16, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for range int(MaxSessionID) + 1 {
		id := p.nextID
		p.nextID++
		if p.nextID > MaxSessionID {
			p.nextID = 0
		}
		if !slices.ContainsFunc(p.sessions, func(s ProviderSession) bool { return s.Id == id }) {
			return id, nil
		}
	}
	return 0, spec.Errorf(im.StatusResourceExhausted, "WebRtcTransportProvider: every session id is in use")
}

// peerOf is the invoker's node (chip's GetNodeIdFromCtx, :54-62: 0, the
// undefined node, without a CASE session) and fabric.
func peerOf(ctx context.Context) (node uint64, fabric uint8) {
	_, fabric, _ = im.LookupFabricFilter(ctx)
	if !im.IsPASEFromContext(ctx) {
		node, _ = im.SubjectFromContext(ctx)
	}
	return node, fabric
}

// matchingSession is chip's CheckForMatchingSession (:350-369): the
// session of the id, if the invoker is its peer on its fabric.
func (p *Provider) matchingSession(ctx context.Context, id uint16) (ProviderSession, bool) {
	node, fabric := peerOf(ctx)
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, s := range p.sessions {
		if s.Id == id {
			return s, s.PeerNodeId == node && s.FabricIndex == fabric
		}
	}
	return ProviderSession{}, false
}

// checkICEServers is chip's ICEServersFollowsSpecConstraints (:133-209):
// at most 10 servers of at most 10 URLs of at most 2000 characters, a
// username of at most 508 and a credential of at most 512 bytes, a CAID
// up to 65534 — CONSTRAINT_ERROR otherwise.
func checkICEServers(servers *[]ICEServer) error {
	if servers == nil {
		return nil
	}
	if len(*servers) > 10 { // :144
		return spec.Errorf(im.StatusConstraintError, "WebRtcTransportProvider: %d ICE servers", len(*servers))
	}
	for _, s := range *servers {
		if len(s.Urls) > 10 { // :158
			return spec.Errorf(im.StatusConstraintError, "WebRtcTransportProvider: %d ICE server URLs", len(s.Urls))
		}
		for _, u := range s.Urls {
			if len(u) > 2000 { // :166
				return spec.Errorf(im.StatusConstraintError, "WebRtcTransportProvider: ICE server URL of %d characters", len(u))
			}
		}
		switch {
		case s.Username != nil && len(*s.Username) > 508: // :182
			return spec.Errorf(im.StatusConstraintError, "WebRtcTransportProvider: ICE server username of %d bytes", len(*s.Username))
		case s.Credential != nil && len(*s.Credential) > 512: // :189
			return spec.Errorf(im.StatusConstraintError, "WebRtcTransportProvider: ICE server credential of %d bytes", len(*s.Credential))
		case s.Caid != nil && *s.Caid > 65534: // :196
			return spec.Errorf(im.StatusConstraintError, "WebRtcTransportProvider: ICE server CAID %d", *s.Caid)
		}
	}
	return nil
}

// checkTurnsOrStuns is chip's CheckTurnsOrStunsRequiresUTCTime
// (:447-495) with HasTurnsOrStunsScheme (:107-115): a turns: or stuns: URL
// needs a known UTCTime (INVALID_IN_STATE).
func (p *Provider) checkTurnsOrStuns(servers *[]ICEServer) error {
	if servers == nil || p.cfg.UTCTimeKnown == nil || p.cfg.UTCTimeKnown() {
		return nil
	}
	for _, s := range *servers {
		for _, u := range s.Urls {
			if strings.HasPrefix(u, "turns:") || strings.HasPrefix(u, "stuns:") {
				return spec.Errorf(im.StatusInvalidInState, "WebRtcTransportProvider: a turns or stuns URL needs UTCTime")
			}
		}
	}
	return nil
}

// streamID is chip's ValidateStreamID (:497-538) for a deprecated
// VideoStreamID or AudioStreamID: absent leaves the kind out, null asks
// for automatic selection (INVALID_IN_STATE without an allocated stream),
// an id must name an allocated stream (DYNAMIC_CONSTRAINT_ERROR).
func (p *Provider) streamID(id *spec.Nullable[uint16], video bool) (*[]uint16, error) {
	if id == nil {
		return nil, nil
	}
	kind, has := "audio", len(p.view.audios()) > 0
	if video {
		kind, has = "video", len(p.view.videos()) > 0
	}
	if id.Null {
		if !has { // :517
			return nil, spec.Errorf(im.StatusInvalidInState, "WebRtcTransportProvider: no allocated %s stream", kind)
		}
		return &[]uint16{}, nil
	}
	if !p.view.allocated(id.Value, video) { // :531
		return nil, spec.Errorf(im.StatusDynamicConstraintError, "WebRtcTransportProvider: %s stream %d is not allocated", kind, id.Value)
	}
	return &[]uint16{id.Value}, nil
}

// streamList is chip's ValidateStreams (:540-592) for VideoStreams or
// AudioStreams: INVALID_IN_STATE without allocated streams, ALREADY_EXISTS
// for a duplicate, DYNAMIC_CONSTRAINT_ERROR for an id not allocated.
func (p *Provider) streamList(in *[]uint16, video bool, out *[]uint16) (*[]uint16, error) {
	if in == nil {
		return out, nil
	}
	kind, has := "audio", len(p.view.audios()) > 0
	if video {
		kind, has = "video", len(p.view.videos()) > 0
	}
	if !has { // :567
		return nil, spec.Errorf(im.StatusInvalidInState, "WebRtcTransportProvider: no allocated %s stream", kind)
	}
	seen := map[uint16]bool{}
	for _, id := range *in {
		if seen[id] { // :576
			return nil, spec.Errorf(im.StatusAlreadyExists, "WebRtcTransportProvider: %s stream %d twice", kind, id)
		}
		seen[id] = true
	}
	for _, id := range *in {
		if !p.view.allocated(id, video) { // :587
			return nil, spec.Errorf(im.StatusDynamicConstraintError, "WebRtcTransportProvider: %s stream %d is not allocated", kind, id)
		}
	}
	streams := slices.Clone(*in)
	return &streams, nil
}

// offerRequest is what SolicitOffer and a new ProvideOffer validate alike.
type offerRequest struct {
	usage                      uint8
	videoID, audioID           *spec.Nullable[uint16]
	videoStreams, audioStreams *[]uint16
	iceServers                 *[]ICEServer
	policy                     *string
}

// checkConstraints is the data model validation both offers open with
// (SolicitOffer :597-643, ProvideOffer :858-901): the usage, one stream
// field at least, the ICE servers and the transport policy.
// Mirrors chip WebRTCTransportProviderCluster.cpp:866-872 and :982-988: a ProvideOffer naming none
// of VideoStreamID, AudioStreamID, VideoStreams, AudioStreams is INVALID_COMMAND. matter.js has no provider
// server (WebRtcTransportProviderServer.ts:14).
// A session that names no stream — as the port may return one — fails
// CurrentSessions' "O.a+" and is not stored (FAILURE; review II,
// R2-P6-par-1304).
func checkConstraints(r offerRequest) error {
	switch {
	case r.usage > usageLiveView: // :601-605, :859-863
		return spec.Errorf(im.StatusConstraintError, "WebRtcTransportProvider: StreamUsage %d", r.usage)
	case r.videoID == nil && r.audioID == nil && r.videoStreams == nil && r.audioStreams == nil: // :608-613, :866-871
		return spec.Errorf(im.StatusInvalidCommand, "WebRtcTransportProvider: no stream requested")
	}
	if err := checkICEServers(r.iceServers); err != nil { // :625-633
		return err
	}
	if r.policy != nil && len(*r.policy) > 16 { // :636-643
		return spec.Errorf(im.StatusConstraintError, "WebRtcTransportProvider: ICETransportPolicy of %d characters", len(*r.policy))
	}
	return nil
}

// checkStreams is the effect-on-receipt validation of a new offer
// (SolicitOffer :647-711, ProvideOffer :955-1016): the privacy modes, the
// prioritised usage, the deprecated and the list fields exclusive, each
// stream field against the allocated streams.
func (p *Provider) checkStreams(r offerRequest) (videos, audios *[]uint16, err error) {
	if err := p.view.checkPrivacy("WebRtcTransportProvider", r.usage); err != nil { // :647-652
		return nil, nil, err
	}
	if !p.view.usagePrioritised(r.usage) { // :654-659
		return nil, nil, spec.Errorf(im.StatusDynamicConstraintError, "WebRtcTransportProvider: StreamUsage %d is not in StreamUsagePriorities", r.usage)
	}
	if (r.videoStreams != nil || r.audioStreams != nil) && (r.videoID != nil || r.audioID != nil) { // :663-668
		return nil, nil, spec.Errorf(im.StatusInvalidCommand, "WebRtcTransportProvider: the stream lists and the stream ids together")
	}
	if videos, err = p.streamID(r.videoID, true); err != nil { // :686-690
		return nil, nil, err
	}
	if audios, err = p.streamID(r.audioID, false); err != nil { // :693-697
		return nil, nil, err
	}
	if videos, err = p.streamList(r.videoStreams, true, videos); err != nil { // :700-704
		return nil, nil, err
	}
	if audios, err = p.streamList(r.audioStreams, false, audios); err != nil { // :707-711
		return nil, nil, err
	}
	return videos, audios, nil
}

func providerRequest[T any](fields any, name string) (T, error) {
	req, ok := spec.RequestOf[T](fields)
	if !ok {
		return req, spec.Errorf(im.StatusInvalidCommand, "WebRtcTransportProvider: %s payload %T", name, fields)
	}
	return req, nil
}

// solicitOffer carries out SolicitOffer. Mirrors chip HandleSolicitOffer
// (:595-842): the constraints, the streams, the turns/stuns rule, the
// camera's stream selection (DYNAMIC_CONSTRAINT_ERROR), a new session id,
// then the port; the session it returns is stored, and the response
// carries the stream ids the request named.
func (p *Provider) solicitOffer(ctx context.Context, fields any) (any, error) {
	req, err := providerRequest[wpdef.SolicitOfferRequest](fields, "SolicitOffer")
	if err != nil {
		return nil, err
	}
	r := offerRequest{
		usage: uint8(req.StreamUsage), videoID: req.VideoStreamId, audioID: req.AudioStreamId,
		videoStreams: req.VideoStreams, audioStreams: req.AudioStreams, iceServers: req.IceServers, policy: req.IceTransportPolicy,
	}
	if err := checkConstraints(r); err != nil {
		return nil, err
	}
	videos, audios, err := p.checkStreams(r)
	if err != nil {
		return nil, err
	}
	if err := p.checkTurnsOrStuns(req.IceServers); err != nil { // :724-730
		return nil, err
	}
	if !p.view.autoSelect(r.usage, videos, audios) { // :735-740
		return nil, spec.Errorf(im.StatusDynamicConstraintError, "WebRtcTransportProvider: the camera cannot provide StreamUsage %d", r.usage)
	}
	node, fabric := peerOf(ctx)
	if p.atSessionBound(fabric) {
		return p.refuseSolicitOffer(ctx, req, node, fabric, videos, audios)
	}
	id, err := p.generateSessionID() // :744-750
	if err != nil {
		return nil, err
	}
	args := OfferArgs{
		SessionID: id, StreamUsage: req.StreamUsage, VideoStreams: videos, AudioStreams: audios,
		IceServers: req.IceServers, IceTransportPolicy: req.IceTransportPolicy, MetadataEnabled: req.MetadataEnabled,
		PeerNodeID: node, FabricIndex: fabric, OriginatingEndpointID: req.OriginatingEndpointId,
	}
	session, deferred, err := p.port.SolicitOffer(ctx, args) // :801-808
	if err != nil {
		return nil, portError("SolicitOffer", err)
	}
	if err := p.upsertSession(contract.OriginCommand, session); err != nil { // :811
		return nil, p.abandonSession(ctx, session.Id, err)
	}
	resp := wpdef.SolicitOfferResponse{WebRtcSessionId: session.Id, DeferredOffer: deferred}
	if req.VideoStreamId != nil { // :826-830
		resp.VideoStreamId = streamIDOf(session.VideoStreamId)
	}
	if req.AudioStreamId != nil { // :832-836
		resp.AudioStreamId = streamIDOf(session.AudioStreamId)
	}
	return resp, nil
}

// streamIDOf is a session's deprecated stream id for a response; null
// when the session names none.
func streamIDOf(id *spec.Nullable[uint16]) *spec.Nullable[uint16] {
	v := spec.NullOf[uint16]()
	if id != nil {
		v = *id
	}
	return &v
}

// provideOffer carries out ProvideOffer. Mirrors chip HandleProvideOffer
// (:843-1133): the constraints; a session id names the invoker's session
// (NOT_FOUND otherwise) and a re-offer keeps its id, taking the stream
// ids (VideoStreamID, AudioStreamID) the request names; a null id is a new session,
// validated as SolicitOffer's but RESOURCE_EXHAUSTED when the camera
// cannot provide the usage; then the port, and the session it returns is
// stored.
func (p *Provider) provideOffer(ctx context.Context, fields any) (any, error) {
	req, err := providerRequest[wpdef.ProvideOfferRequest](fields, "ProvideOffer")
	if err != nil {
		return nil, err
	}
	var usage wpdef.StreamUsageEnum
	if req.StreamUsage != nil {
		usage = *req.StreamUsage
	}
	r := offerRequest{
		usage: uint8(usage), videoID: req.VideoStreamId, audioID: req.AudioStreamId,
		videoStreams: req.VideoStreams, audioStreams: req.AudioStreams, iceServers: req.IceServers, policy: req.IceTransportPolicy,
	}
	if err := checkConstraints(r); err != nil { // :855-901
		return nil, err
	}
	var existing ProviderSession
	if !req.WebRtcSessionId.Null {
		var ok bool
		if existing, ok = p.matchingSession(ctx, req.WebRtcSessionId.Value); !ok { // :916-923
			return nil, spec.Errorf(im.StatusNotFound, "WebRtcTransportProvider: session %d not found", req.WebRtcSessionId.Value)
		}
		if req.StreamUsage == nil {
			usage = existing.StreamUsage
		}
	} else if req.StreamUsage == nil {
		return nil, spec.Errorf(im.StatusInvalidCommand, "WebRtcTransportProvider: a new session needs a StreamUsage")
	}
	node, fabric := peerOf(ctx)
	args := OfferArgs{
		StreamUsage: usage, IceServers: req.IceServers, IceTransportPolicy: req.IceTransportPolicy,
		MetadataEnabled: req.MetadataEnabled, PeerNodeID: node, FabricIndex: fabric, Sdp: req.Sdp,
	}
	if req.OriginatingEndpointId != nil {
		args.OriginatingEndpointID = *req.OriginatingEndpointId
	} else {
		args.OriginatingEndpointID = existing.PeerEndpointId
	}
	if !req.WebRtcSessionId.Null {
		args.SessionID = existing.Id // :926-948
		if req.VideoStreamId != nil {
			args.VideoStreams = nullableList(req.VideoStreamId)
		}
		if req.AudioStreamId != nil {
			args.AudioStreams = nullableList(req.AudioStreamId)
		}
	} else {
		videos, audios, err := p.checkStreams(r) // :955-1016
		if err != nil {
			return nil, err
		}
		if !p.view.autoSelect(r.usage, videos, audios) { // :1020-1026
			return nil, spec.Errorf(im.StatusResourceExhausted, "WebRtcTransportProvider: the camera cannot provide StreamUsage %d", r.usage)
		}
		if err := p.checkTurnsOrStuns(req.IceServers); err != nil { // :1046-1051
			return nil, err
		}
		if p.atSessionBound(fabric) {
			return nil, errSessionBound
		}
		if args.SessionID, err = p.generateSessionID(); err != nil { // :1054-1060
			return nil, err
		}
		args.VideoStreams, args.AudioStreams = videos, audios
	}
	session, err := p.port.ProvideOffer(ctx, args) // :1101-1108
	if err != nil {
		return nil, portError("ProvideOffer", err)
	}
	if err := p.upsertSession(contract.OriginCommand, session); err != nil { // :1111
		if req.WebRtcSessionId.Null {
			return nil, p.abandonSession(ctx, session.Id, err)
		}
		// A re-offer: the table still holds the session as it was, and
		// the controller can end it.
		return nil, spec.Errorf(im.StatusFailure, "%v", err)
	}
	resp := wpdef.ProvideOfferResponse{WebRtcSessionId: session.Id}
	if req.VideoStreamId != nil { // :1117-1121
		resp.VideoStreamId = streamIDOf(session.VideoStreamId)
	}
	if req.AudioStreamId != nil { // :1123-1127
		resp.AudioStreamId = streamIDOf(session.AudioStreamId)
	}
	return resp, nil
}

// nullableList is a re-offer's deprecated stream id as a list: empty for
// null (:931-946).
func nullableList(id *spec.Nullable[uint16]) *[]uint16 {
	out := []uint16{}
	if !id.Null {
		out = append(out, id.Value)
	}
	return &out
}

// provideAnswer carries out ProvideAnswer. Mirrors chip
// HandleProvideAnswer (:1135-1151).
func (p *Provider) provideAnswer(ctx context.Context, fields any) (any, error) {
	req, err := providerRequest[wpdef.ProvideAnswerRequest](fields, "ProvideAnswer")
	if err != nil {
		return nil, err
	}
	if _, ok := p.matchingSession(ctx, req.WebRtcSessionId); !ok { // :1145
		return nil, spec.Errorf(im.StatusNotFound, "WebRtcTransportProvider: session %d not found", req.WebRtcSessionId)
	}
	if err := p.port.ProvideAnswer(ctx, req.WebRtcSessionId, req.Sdp); err != nil { // :1149
		return nil, portError("ProvideAnswer", err)
	}
	return nil, nil
}

// provideIceCandidates carries out ProvideICECandidates. Mirrors chip
// HandleProvideICECandidates (:1153-1193): an empty SDPMid is
// CONSTRAINT_ERROR before the session is looked up.
func (p *Provider) provideIceCandidates(ctx context.Context, fields any) (any, error) {
	req, err := providerRequest[wpdef.ProvideIceCandidatesRequest](fields, "ProvideICECandidates")
	if err != nil {
		return nil, err
	}
	for _, c := range req.IceCandidates {
		if !c.SdpMid.Null && c.SdpMid.Value == "" { // :1166-1171
			return nil, spec.Errorf(im.StatusConstraintError, "WebRtcTransportProvider: an empty SDPMid")
		}
	}
	if _, ok := p.matchingSession(ctx, req.WebRtcSessionId); !ok { // :1187
		return nil, spec.Errorf(im.StatusNotFound, "WebRtcTransportProvider: session %d not found", req.WebRtcSessionId)
	}
	if err := p.port.ProvideIceCandidates(ctx, req.WebRtcSessionId, slices.Clone(req.IceCandidates)); err != nil { // :1191
		return nil, portError("ProvideICECandidates", err)
	}
	return nil, nil
}

// abandonSession ends at the port a new session the port created but
// CurrentSessions refused to store (cause), and returns the command's
// FAILURE. Without it the session would run on with no table entry, and
// a controller's EndSession would answer NOT_FOUND (R2-C2-err-1602). The
// reason is UnknownReason, as a removed fabric's sessions end; the port's
// error, if any, joins the FAILURE's message.
func (p *Provider) abandonSession(ctx context.Context, id uint16, cause error) error {
	if err := p.port.EndSession(ctx, id, endReasonUnknown); err != nil {
		return spec.Errorf(im.StatusFailure, "%v (ending the session at the port: %v)", cause, err)
	}
	return spec.Errorf(im.StatusFailure, "%v", cause)
}

// maxEndReason is the highest WebRTCEndReasonEnum value (UnknownReason).
const maxEndReason = 12

// endSession carries out EndSession. Mirrors chip HandleEndSession
// (:1194-1213): an unknown reason is CONSTRAINT_ERROR; the session leaves
// CurrentSessions whatever the port answers, and the port's error is the
// command's.
func (p *Provider) endSession(ctx context.Context, fields any) (any, error) {
	req, err := providerRequest[wpdef.EndSessionRequest](fields, "EndSession")
	if err != nil {
		return nil, err
	}
	if req.Reason > maxEndReason { // :1201-1205
		return nil, spec.Errorf(im.StatusConstraintError, "WebRtcTransportProvider: end reason %d", req.Reason)
	}
	if _, ok := p.matchingSession(ctx, req.WebRtcSessionId); !ok { // :1211
		return nil, spec.Errorf(im.StatusNotFound, "WebRtcTransportProvider: session %d not found", req.WebRtcSessionId)
	}
	perr := p.port.EndSession(ctx, req.WebRtcSessionId, req.Reason)      // :1215
	serr := p.removeSession(contract.OriginCommand, req.WebRtcSessionId) // :1218
	if perr != nil {
		return nil, portError("EndSession", perr)
	}
	if serr != nil {
		return nil, spec.Errorf(im.StatusFailure, "%v", serr)
	}
	return nil, nil
}

// MatterDataVersion implements [contract.ClusterDataVersion].
func (p *Provider) MatterDataVersion() uint32 { return p.srv.MatterDataVersion() }

// OnAttributeChange implements [contract.AttributeChangeNotifier].
func (p *Provider) OnAttributeChange(cb func(change contract.Change)) (unsubscribe func()) {
	return p.srv.OnAttributeChange(cb)
}

// MatterRead resolves an attribute for a local caller: CurrentSessions
// holds every fabric's sessions.
func (p *Provider) MatterRead(attrID uint32) (any, bool) { return p.srv.MatterRead(attrID) }

// MatterReadFiltered implements [contract.FabricScopedReader]:
// CurrentSessions is fabric-sensitive ("R S M"), so a peer's read sees its
// own fabric's sessions only (matter.js ListManager.ts:461-478, as the
// requestor's).
func (p *Provider) MatterReadFiltered(ctx context.Context, attrID uint32) (any, bool) {
	_, fabric, remote := im.LookupFabricFilter(ctx)
	if attrID != wpdef.AttrCurrentSessions || !remote {
		return p.MatterRead(attrID) //nolint:contextcheck // MatterRead is the unfiltered cluster-interface read
	}
	out := spec.List[ProviderSession]{}
	for _, s := range p.Sessions() {
		if fabric != 0 && s.FabricIndex == fabric {
			out = append(out, s)
		}
	}
	return out, true
}

// MatterWrite answers every write by the definition: the cluster has no
// writable attribute.
func (p *Provider) MatterWrite(ctx context.Context, attrID uint32, value any) error {
	return p.srv.MatterWrite(ctx, attrID, value)
}

// MatterWriteChanged implements [contract.ChangeReportingWriter]: the
// generated server's write and its verdict.
func (p *Provider) MatterWriteChanged(ctx context.Context, attrID uint32, value any) (bool, error) {
	return p.srv.MatterWriteChanged(ctx, attrID, value)
}

// MatterInvoke dispatches an accepted command; a host's handler runs in
// its place ([spec.SelfHeld.Handle]).
func (p *Provider) MatterInvoke(ctx context.Context, cmdID uint32, fields any) (any, error) {
	return spec.InvokeHeld(ctx, &p.SelfHeld, cmdID, fields)
}
