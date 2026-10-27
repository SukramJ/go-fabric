// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sync"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	wrdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportrequestor"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// RequestorClusterID is the WebRtcTransportRequestor cluster id.
const RequestorClusterID = wrdef.ClusterID

// Generated types the requestor speaks.
type (
	// WebRTCSession is one tracked session, an entry of CurrentSessions.
	WebRTCSession = wrdef.WebRTCSessionStruct
	// ICECandidate is one ICE candidate of an IceCandidates command.
	ICECandidate = wrdef.ICECandidateStruct
	// RequestorPort receives the signalling of the tracked sessions.
	RequestorPort = wrdef.RequestorPort
	// RequestorRefusals is the optional refusal observer of a
	// RequestorPort.
	RequestorRefusals = wrdef.RequestorRefusals
	// Signal names a signalling command.
	Signal = wrdef.Signal
)

// SessionKey identifies a tracked session. The session id alone is not
// unique: each provider node allocates its own, so two peers, or one peer
// on two fabrics, may use the same id at the same time. Mirrors matter.js
// WebRtcTransportRequestorServer.ts:189-196 (SessionKey) and :185-187
// (isSameSession).
type SessionKey struct {
	ID          uint16
	FabricIndex uint8
	PeerNodeID  uint64
}

// KeyOf is the key of a session.
func KeyOf(s WebRTCSession) SessionKey {
	return SessionKey{ID: s.Id, FabricIndex: s.FabricIndex, PeerNodeID: s.PeerNodeId}
}

// RequestorConfig carries the construction parameters of a [Requestor].
type RequestorConfig struct {
	// Port receives the signalling; nil drops it (the commands are still
	// checked and answered).
	Port RequestorPort
	// DataVersion is an optional host-owned tracker.
	DataVersion *cluster.DataVersionTracker
}

// Requestor implements [contract.ClusterServer] for WebRtcTransportRequestor:
// the controller side of a WebRTC stream (a camera controller). The peer
// camera drives signalling by invoking Offer, Answer, IceCandidates and End;
// the server checks each against the session the host registered for the
// peer's fabric and node, tracks the sessions in CurrentSessions, and hands
// the signalling to the [RequestorPort]. Mirrors matter.js
// WebRtcTransportRequestorServer.ts:22-41.
//
// The host registers a session with [Requestor.UpsertSession] when it
// initiates a stream, so the peer's commands for it are accepted, and
// removes it with [Requestor.RemoveSession] when it tears the stream down;
// End removes it on the peer's side. The node installs the Operate entry
// the peer needs on the first fabric (node/webrtc_requestor_acl.go,
// :56-94).
type Requestor struct {
	*spec.Instance
	// SelfHeld is the typed server's command handlers, event path and
	// write veto over srv.
	spec.SelfHeld

	srv  *spec.Server
	port RequestorPort

	// pub serialises the changes of the session table with their
	// publication; mu guards sessions, the table CurrentSessions reports.
	pub      sync.Mutex
	mu       sync.Mutex
	sessions []WebRTCSession
}

// Compile-time assertions.
var (
	_ contract.ClusterServer                  = (*Requestor)(nil)
	_ contract.ClusterDataVersion             = (*Requestor)(nil)
	_ contract.ClusterAttributeLister         = (*Requestor)(nil)
	_ contract.ClusterCommandLister           = (*Requestor)(nil)
	_ contract.ClusterAttributeWritePrivilege = (*Requestor)(nil)
	_ contract.ClusterCommandInvokePrivilege  = (*Requestor)(nil)
	_ contract.AttributeChangeNotifier        = (*Requestor)(nil)
	_ contract.FabricScopedReader             = (*Requestor)(nil)
	_ spec.AttributeSetter                    = (*Requestor)(nil)
	_ spec.CommandRegistrar                   = (*Requestor)(nil)
)

// NewRequestor builds a WebRtcTransportRequestor server with no session.
func NewRequestor(cfg RequestorConfig) (*Requestor, error) {
	r := &Requestor{port: cfg.Port}
	handlers := map[uint32]spec.CommandHandler{
		wrdef.CmdOffer:         r.offer,
		wrdef.CmdAnswer:        r.answer,
		wrdef.CmdIceCandidates: r.iceCandidates,
		wrdef.CmdEnd:           r.end,
	}
	srv, err := spec.NewServer(wrdef.Definition, spec.Options{}, spec.ServerConfig{Handlers: handlers, DataVersion: cfg.DataVersion})
	if err != nil {
		return nil, fmt.Errorf("camera: %w", err)
	}
	r.srv, r.Instance, r.SelfHeld = srv, srv.Instance, spec.NewSelfHeld(srv)
	if err := srv.SetAttributes(map[uint32]any{wrdef.AttrCurrentSessions: spec.List[WebRTCSession]{}}); err != nil {
		return nil, fmt.Errorf("camera: %w", err)
	}
	return r, nil
}

// Sessions returns the tracked sessions, in the order they were added.
func (r *Requestor) Sessions() []WebRTCSession {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.sessions)
}

// UpsertSession registers or updates a session the host initiated with a
// peer: a session must be registered before the peer's signalling for it
// is accepted, and it appears in CurrentSessions. A session with the same
// [SessionKey] is replaced in place. The deprecated VideoStreamId and
// AudioStreamId carry the first entry of VideoStreams and AudioStreams,
// null without one. The table is held to the attribute's schema; a
// session it refuses is an error and changes nothing. Mirrors matter.js
// WebRtcTransportRequestorServer.ts:96-114.
//
// It must not be called from the server's own change notification.
func (r *Requestor) UpsertSession(session WebRTCSession) error {
	enriched := session
	enriched.VideoStreamId = firstStream(session.VideoStreams) // :104
	enriched.AudioStreamId = firstStream(session.AudioStreams) // :105
	return r.change(contract.OriginHostSet, func(sessions []WebRTCSession) ([]WebRTCSession, bool) {
		idx := slices.IndexFunc(sessions, func(s WebRTCSession) bool { return KeyOf(s) == KeyOf(session) })
		if idx == -1 { // :109-110
			return append(sessions, enriched), true
		}
		sessions[idx] = enriched // :111-112
		return sessions, true
	})
}

// RemoveSession stops tracking a session, after the host tore its stream
// down; a key no session has changes nothing. Mirrors matter.js
// WebRtcTransportRequestorServer.ts:116-122.
func (r *Requestor) RemoveSession(key SessionKey) {
	_ = r.change(contract.OriginHostSet, func(sessions []WebRTCSession) ([]WebRTCSession, bool) {
		return removeKey(sessions, key)
	})
}

// removeKey drops the session of key; false when no session has it.
func removeKey(sessions []WebRTCSession, key SessionKey) ([]WebRTCSession, bool) {
	if !slices.ContainsFunc(sessions, func(s WebRTCSession) bool { return KeyOf(s) == key }) {
		return sessions, false
	}
	return slices.DeleteFunc(sessions, func(s WebRTCSession) bool { return KeyOf(s) == key }), true
}

// firstStream is a stream list's first id, null for none (matter.js
// `session.videoStreams?.[0] ?? null`).
func firstStream(streams *[]uint16) *spec.Nullable[uint16] {
	v := spec.NullOf[uint16]()
	if streams != nil && len(*streams) > 0 {
		v = spec.ValueOf((*streams)[0])
	}
	return &v
}

// change applies edit to a copy of the table and publishes the result when
// edit reports a change and the schema admits it.
func (r *Requestor) change(origin contract.Origin, edit func([]WebRTCSession) ([]WebRTCSession, bool)) error {
	r.pub.Lock()
	defer r.pub.Unlock()
	next, changed := edit(r.Sessions())
	if !changed {
		return nil
	}
	if next == nil {
		next = []WebRTCSession{}
	}
	if err := r.srv.SetAttributesFrom(origin, map[uint32]any{wrdef.AttrCurrentSessions: spec.List[WebRTCSession](next)}); err != nil {
		return fmt.Errorf("camera: CurrentSessions: %w", err)
	}
	r.mu.Lock()
	r.sessions = next
	r.mu.Unlock()
	return nil
}

// SetAttributes implements [spec.AttributeSetter]: CurrentSessions
// replaces the table, each session as [Requestor.UpsertSession] stores it.
func (r *Requestor) SetAttributes(values map[uint32]any) error {
	for _, id := range slices.Sorted(maps.Keys(values)) {
		if id != wrdef.AttrCurrentSessions {
			return fmt.Errorf("%w: camera: WebRtcTransportRequestor attribute 0x%04X", spec.ErrNotHeld, id)
		}
	}
	v, ok := values[wrdef.AttrCurrentSessions]
	if !ok {
		return nil
	}
	var list []WebRTCSession
	switch l := v.(type) {
	case []WebRTCSession:
		list = l
	case spec.List[WebRTCSession]:
		list = l
	default:
		return fmt.Errorf("%w: camera: CurrentSessions %T", spec.ErrInvalidValue, v)
	}
	next := make([]WebRTCSession, 0, len(list))
	for _, s := range list {
		s.VideoStreamId = firstStream(s.VideoStreams)
		s.AudioStreamId = firstStream(s.AudioStreams)
		next = append(next, s)
	}
	return r.change(contract.OriginHostSet, func([]WebRTCSession) ([]WebRTCSession, bool) { return next, true })
}

// MatterDataVersion implements [contract.ClusterDataVersion].
func (r *Requestor) MatterDataVersion() uint32 { return r.srv.MatterDataVersion() }

// OnAttributeChange implements [contract.AttributeChangeNotifier].
func (r *Requestor) OnAttributeChange(cb func(change contract.Change)) (unsubscribe func()) {
	return r.srv.OnAttributeChange(cb)
}

// MatterRead resolves an attribute for a local caller: CurrentSessions
// holds every fabric's sessions.
func (r *Requestor) MatterRead(attrID uint32) (any, bool) { return r.srv.MatterRead(attrID) }

// MatterReadFiltered implements [contract.FabricScopedReader].
// CurrentSessions is a fabric-sensitive list ("R S A"): a peer's read sees
// the sessions of its own fabric only, whether or not it filters, and a
// session without a fabric sees none. Mirrors matter.js
// packages/node/src/behavior/state/managed/values/ListManager.ts:461-478
// (createProxy: a fabric-sensitive list is filtered for every read of a
// remote actor).
func (r *Requestor) MatterReadFiltered(ctx context.Context, attrID uint32) (any, bool) {
	_, fabric, request := im.LookupFabricFilter(ctx)
	if attrID != wrdef.AttrCurrentSessions || !request {
		return r.MatterRead(attrID) //nolint:contextcheck // MatterRead is the unfiltered cluster-interface read
	}
	out := spec.List[WebRTCSession]{}
	for _, s := range r.Sessions() {
		if fabric != 0 && s.FabricIndex == fabric {
			out = append(out, s)
		}
	}
	return out, true
}

// MatterWrite answers every write by the definition: the cluster has no
// writable attribute.
func (r *Requestor) MatterWrite(ctx context.Context, attrID uint32, value any) error {
	return r.srv.MatterWrite(ctx, attrID, value)
}

// MatterInvoke dispatches a signalling command; a host's handler runs in
// its place ([spec.SelfHeld.Handle]).
func (r *Requestor) MatterInvoke(ctx context.Context, cmdID uint32, fields any) (any, error) {
	return spec.InvokeHeld(ctx, &r.SelfHeld, cmdID, fields)
}

// offer carries out Offer: the session the peer names is surfaced with the
// request. Mirrors matter.js WebRtcTransportRequestorServer.ts:124-132.
func (r *Requestor) offer(ctx context.Context, fields any) (any, error) {
	req, ok := spec.RequestOf[wrdef.OfferRequest](fields)
	if !ok {
		return nil, spec.Errorf(im.StatusInvalidCommand, "WebRtcTransportRequestor: Offer payload %T", fields)
	}
	session, err := r.findSessionStrict(ctx, wrdef.SignalOffer, req.WebRtcSessionId)
	if err != nil {
		return nil, err
	}
	if r.port != nil {
		r.port.Offer(ctx, session, req)
	}
	return nil, nil
}

// answer carries out Answer: the session is surfaced with the SDP. Mirrors
// matter.js WebRtcTransportRequestorServer.ts:134-142.
func (r *Requestor) answer(ctx context.Context, fields any) (any, error) {
	req, ok := spec.RequestOf[wrdef.AnswerRequest](fields)
	if !ok {
		return nil, spec.Errorf(im.StatusInvalidCommand, "WebRtcTransportRequestor: Answer payload %T", fields)
	}
	session, err := r.findSessionStrict(ctx, wrdef.SignalAnswer, req.WebRtcSessionId)
	if err != nil {
		return nil, err
	}
	if r.port != nil {
		r.port.Answer(ctx, session, req.Sdp)
	}
	return nil, nil
}

// iceCandidates carries out IceCandidates: an empty list is
// CONSTRAINT_ERROR before any session is looked up (a peer's empty list is
// refused by the request's schema already, "min 1"); the session is
// surfaced with the candidates. Mirrors matter.js
// WebRtcTransportRequestorServer.ts:144-158.
func (r *Requestor) iceCandidates(ctx context.Context, fields any) (any, error) {
	req, ok := spec.RequestOf[wrdef.IceCandidatesRequest](fields)
	if !ok {
		return nil, spec.Errorf(im.StatusInvalidCommand, "WebRtcTransportRequestor: IceCandidates payload %T", fields)
	}
	if len(req.IceCandidates) == 0 { // :151-155
		return nil, spec.Errorf(im.StatusConstraintError, "WebRtcTransportRequestor: ICE candidates list must not be empty")
	}
	session, err := r.findSessionStrict(ctx, wrdef.SignalIceCandidates, req.WebRtcSessionId)
	if err != nil {
		return nil, err
	}
	if r.port != nil {
		r.port.IceCandidates(ctx, session, req.IceCandidates)
	}
	return nil, nil
}

// end carries out End: the session leaves the table, then it is surfaced
// with the reason. Mirrors matter.js WebRtcTransportRequestorServer.ts:
// 160-168.
func (r *Requestor) end(ctx context.Context, fields any) (any, error) {
	req, ok := spec.RequestOf[wrdef.EndRequest](fields)
	if !ok {
		return nil, spec.Errorf(im.StatusInvalidCommand, "WebRtcTransportRequestor: End payload %T", fields)
	}
	session, err := r.findSessionStrict(ctx, wrdef.SignalEnd, req.WebRtcSessionId)
	if err != nil {
		return nil, err
	}
	key := KeyOf(session)
	_ = r.change(contract.OriginCommand, func(sessions []WebRTCSession) ([]WebRTCSession, bool) {
		return removeKey(sessions, key)
	})
	if r.port != nil {
		r.port.End(ctx, session, req.Reason)
	}
	return nil, nil
}

// findSessionStrict finds the session id names for the invoking peer: the
// same id on the peer's fabric from the peer's node. A command without a
// remote node session behind it (a local call, a group message) fails, as
// matter.js's assertRemoteActor and NodeSession.assert throw; an unknown
// session is reported to the port's [RequestorRefusals] and answered
// NOT_FOUND. Mirrors matter.js WebRtcTransportRequestorServer.ts:170-182.
func (r *Requestor) findSessionStrict(ctx context.Context, signal Signal, id uint16) (WebRTCSession, error) {
	_, fabric, remote := im.LookupFabricFilter(ctx)
	if _, group := im.GroupSubjectFromContext(ctx); !remote || group { // :171-172
		return WebRTCSession{}, spec.Errorf(im.StatusFailure, "WebRtcTransportRequestor: %s needs a peer's node session", signal)
	}
	node, _ := im.SubjectFromContext(ctx)
	key := SessionKey{ID: id, FabricIndex: fabric, PeerNodeID: node}
	r.mu.Lock()
	idx := slices.IndexFunc(r.sessions, func(s WebRTCSession) bool { return KeyOf(s) == key })
	var session WebRTCSession
	if idx >= 0 {
		session = r.sessions[idx]
	}
	r.mu.Unlock()
	if idx < 0 { // :177-180
		if obs, ok := r.port.(RequestorRefusals); ok {
			obs.Refused(ctx, signal, id, fabric, node)
		}
		return WebRTCSession{}, spec.Errorf(im.StatusNotFound, "WebRTC session %d not found", id)
	}
	return session, nil
}
