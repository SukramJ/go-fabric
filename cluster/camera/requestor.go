// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sync"
	"time"

	"thingfabric.dev/go-fabric/internal/deepcopy"

	"thingfabric.dev/go-fabric/clock"
	"thingfabric.dev/go-fabric/cluster"
	"thingfabric.dev/go-fabric/cluster/spec"
	wrdef "thingfabric.dev/go-fabric/cluster/spec/webrtctransportrequestor"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/internal/specseam"
)

// RequestorClusterID is the WebRtcTransportRequestor cluster id.
const RequestorClusterID = wrdef.ClusterID

// EarlySignalHoldTime is how long a signal for an untracked session waits,
// from its arrival, while a hold for its peer exists
// ([Requestor.HoldUnknownSignals]). Mirrors matter.js
// WebRtcTransportRequestorServer.ts:24 (EARLY_SIGNAL_HOLD_TIME).
const EarlySignalHoldTime = 5 * time.Second

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

// RequestorConfig carries the construction parameters of a [Requestor].
type RequestorConfig struct {
	// Port receives the signalling; nil drops it (the commands are still
	// checked and answered).
	Port RequestorPort
	// DataVersion is an optional host-owned tracker.
	//
	// Deprecated: a host-owned tracker adds nothing — the wire carries the
	// endpoint's DataVersion for the cluster, or the server's own where it
	// serves one (decision 165, F-db021da0-46); leave it nil. Removable in
	// v0.25.0.
	DataVersion *cluster.DataVersionTracker
	// Clock times the wait of a held signal ([EarlySignalHoldTime]); nil is
	// the system clock.
	Clock clock.Clock
}

// Requestor implements [contract.ClusterServer] for WebRtcTransportRequestor:
// the controller side of a WebRTC stream (a camera controller). The peer
// camera drives signalling by invoking Offer, Answer, IceCandidates and End;
// the server checks each against the session the host registered for the
// peer's fabric and node, tracks the sessions in CurrentSessions, and hands
// the signalling to the [RequestorPort]. Mirrors matter.js
// WebRtcTransportRequestorServer.ts:26-48.
//
// The host registers a session with [Requestor.UpsertSession] when it
// knows its id, so the peer's commands for it are accepted, and removes it
// with [Requestor.RemoveSession] when it tears the stream down; End
// removes it on the peer's side. While the host asks a provider for a new
// session, it keeps a [Requestor.HoldUnknownSignals] hold for that
// provider, so the signalling that arrives before the provider's response
// waits for the registration instead of being refused. An End's removal
// waits for the Offer, Answer and IceCandidates calls at the port, so the
// port sees no signal for a session after its End; a port that blocks in
// one of them holds back every End meanwhile, and may itself call
// UpsertSession or RemoveSession. The node installs
// the Operate entry the peer needs in the fabrics its host grants
// (node.WebRTCRequestorConfig.GrantFabric, node/webrtc_requestor_acl.go;
// matter.js WebRtcTransportRequestorServer.ts:118-129 and :143-181 write
// it into the first fabric its own controller administers).
type Requestor struct {
	*spec.Instance
	// SelfHeld is the typed server's command handlers, event path and
	// write veto over srv.
	spec.SelfHeld

	srv  *spec.Server
	port RequestorPort

	clock clock.Clock

	// deliver orders a signal's delivery against End's removal: Offer,
	// Answer and IceCandidates hold it shared from the session's lookup
	// through the port call, End holds it exclusively while it removes the
	// session, so the port never sees a signal for a session after its
	// End (matter.js holds the behaviour lock from the lookup through the
	// event, #lockSession WebRtcTransportRequestorServer.ts:317-318, and
	// End removes under the same lock, :294-296). It is not pub: a port
	// may call UpsertSession or RemoveSession from inside a signal. The
	// lock order is deliver, pub, the spec server's, mu.
	deliver sync.RWMutex
	// pub serialises the changes of the session table with their
	// publication; mu guards sessions, the table CurrentSessions reports,
	// the holds, the waiters and recheck.
	pub      sync.Mutex
	mu       sync.Mutex
	sessions []WebRTCSession
	// holds are the [Requestor.HoldUnknownSignals] holds; two holds for
	// one peer are two entries (matter.js's Set of PeerAddress objects,
	// WebRtcTransportRequestorServer.ts:107-108).
	holds map[*signalHold]struct{}
	// waiters are the held signals, each with the session key it waits
	// for; a committed change that tracks a waiter's key marks it, so the
	// waiter leaves its wait even when a later change removed the session
	// before the waiter looked again.
	waiters map[*signalWaiter]struct{}
	// recheck is closed, and replaced, when a held signal's wait may have
	// ended: a committed change of the table, a released hold, a quiesce
	// (matter.js internal.recheck, :418-422).
	recheck chan struct{}
}

// signalWaiter is one held signal: the key it waits for, and whether a
// committed change tracked that key since the signal started waiting
// (matter.js re-checks #isHeld in the continuation of the committed
// change, :374-384 with recheck from #sessionsChanged :232).
type signalWaiter struct {
	key        wrdef.SessionKey
	registered bool
}

// signalHold is one hold: the provider's fabric and node, the session key
// without its id (matter.js PeerAddress, :107 — signals name no provider
// endpoint).
type signalHold struct {
	fabricIndex uint8
	peerNodeID  uint64
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
	_ contract.ClusterQuiescer                = (*Requestor)(nil)
	_ spec.AttributeSetter                    = (*Requestor)(nil)
	_ spec.CommandRegistrar                   = (*Requestor)(nil)
)

// NewRequestor builds a WebRtcTransportRequestor server with no session.
func NewRequestor(cfg RequestorConfig) (*Requestor, error) {
	r := &Requestor{port: cfg.Port, clock: clock.Or(cfg.Clock), holds: map[*signalHold]struct{}{}, waiters: map[*signalWaiter]struct{}{}, recheck: make(chan struct{})}
	handlers := map[uint32]spec.CommandHandler{
		wrdef.CmdOffer:         r.offer,
		wrdef.CmdAnswer:        r.answer,
		wrdef.CmdIceCandidates: r.iceCandidates,
		wrdef.CmdEnd:           r.end,
	}
	srv, err := spec.NewServer(wrdef.Definition, spec.Options{}, spec.ServerConfig{Handlers: handlers, DataVersion: cfg.DataVersion}) //nolint:staticcheck // the deprecated tracker is passed on for its window (decision 165)
	if err != nil {
		return nil, fmt.Errorf("camera: %w", err)
	}
	r.srv, r.Instance, r.SelfHeld = srv, srv.Instance, spec.NewSelfHeld(srv)
	if err := srv.SetAttributes(map[uint32]any{wrdef.AttrCurrentSessions: spec.List[WebRTCSession]{}}); err != nil {
		return nil, fmt.Errorf("camera: %w", err)
	}
	return r, nil
}

// Sessions returns the tracked sessions, in the order they were added: a
// deep copy, which the caller may change.
func (r *Requestor) Sessions() []WebRTCSession {
	r.mu.Lock()
	defer r.mu.Unlock()
	return deepcopy.Each(r.sessions)
}

// UpsertSession registers or updates a session the host initiated with a
// peer: a session must be registered before the peer's signalling for it
// is accepted, and it appears in CurrentSessions. A session with the same
// [wrdef.SessionKey] is replaced in place. The deprecated VideoStreamId and
// AudioStreamId carry the first entry of VideoStreams and AudioStreams,
// null without one. The table is held to the attribute's schema; a
// session it refuses is an error and changes nothing. A signal held for
// the session ([Requestor.HoldUnknownSignals]) is delivered once the
// registration is stored. Mirrors matter.js
// WebRtcTransportRequestorServer.ts:193-207.
//
// It must not be called from the server's own change notification.
func (r *Requestor) UpsertSession(session WebRTCSession) error {
	enriched := session.Clone()
	enriched.VideoStreamId = firstStream(session.VideoStreams) // :197
	enriched.AudioStreamId = firstStream(session.AudioStreams) // :198
	return r.change(contract.OriginHostSet, func(sessions []WebRTCSession) ([]WebRTCSession, bool) {
		idx := slices.IndexFunc(sessions, func(s WebRTCSession) bool { return wrdef.KeyOf(s) == wrdef.KeyOf(session) })
		if idx == -1 { // :202-203
			return append(sessions, enriched), true
		}
		sessions[idx] = enriched // :204-205
		return sessions, true
	})
}

// RemoveSession stops tracking a session, after the host tore its stream
// down; a key no session has changes nothing. Mirrors matter.js
// WebRtcTransportRequestorServer.ts:216-222.
func (r *Requestor) RemoveSession(key wrdef.SessionKey) {
	_ = r.change(contract.OriginHostSet, func(sessions []WebRTCSession) ([]WebRTCSession, bool) {
		return removeKey(sessions, key)
	})
}

// removeKey drops the session of key; false when no session has it.
func removeKey(sessions []WebRTCSession, key wrdef.SessionKey) ([]WebRTCSession, bool) {
	if !slices.ContainsFunc(sessions, func(s WebRTCSession) bool { return wrdef.KeyOf(s) == key }) {
		return sessions, false
	}
	return slices.DeleteFunc(sessions, func(s WebRTCSession) bool { return wrdef.KeyOf(s) == key }), true
}

// HoldUnknownSignals holds the signalling of a provider — fabricIndex's
// node peerNodeID — for session ids the requestor does not track yet,
// while the host asks that provider for a new session: the provider
// allocates the id and may invoke Offer, Answer, IceCandidates or End
// before its SolicitOffer or ProvideOffer response arrives.
//
// While a hold for the peer exists, every signal from it that names a
// session the requestor does not track waits instead of being refused —
// a late signal for a session that already ended too. A waiting signal is
// delivered once its session's registration is stored; if its session is
// still not tracked, it is refused (NOT_FOUND, and the [RequestorRefusals]
// report) once no hold for the peer exists, or [EarlySignalHoldTime] after
// the signal arrived. A waiting signal releases its CASE session's turn,
// so the peer's other messages on that session are handled meanwhile.
//
// Take the hold before the request is sent; call release after the
// registration was tried and before any cleanup that ends the session at
// the provider, so a held signal for a failed session is refused at once.
// Each call is its own hold; release is idempotent, and only its first
// call can end a wait. [Requestor.MatterQuiesce] ends every hold taken
// before it; a hold taken afterwards holds again.
//
// Mirrors matter.js WebRtcTransportRequestorServer.ts:102-116
// (holdUnknownSignals; its doc :59-101), with the holds ended by
// #endHolds :138-141. A method of the server rather than a function over
// an endpoint, so matter.js's ImplementationError for an endpoint without
// the behavior (:103-105) has no Go counterpart.
func (r *Requestor) HoldUnknownSignals(fabricIndex uint8, peerNodeID uint64) (release func()) {
	h := &signalHold{fabricIndex: fabricIndex, peerNodeID: peerNodeID}
	r.mu.Lock()
	r.holds[h] = struct{}{}
	r.mu.Unlock()
	return func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		if _, ok := r.holds[h]; ok { // :110-113
			delete(r.holds, h)
			r.recheckLocked()
		}
	}
}

// MatterQuiesce implements [contract.ClusterQuiescer]: the bridge calls it
// when the server leaves the topology or stops. Every hold ends and every
// held signal whose session is still not tracked is refused, with the
// [RequestorRefusals] report; the server stays usable, and a hold taken
// afterwards holds again. Mirrors matter.js
// WebRtcTransportRequestorServer.ts:138-141 (#endHolds), which runs when
// the node goes offline (:124) and when the behavior is disposed
// (:131-135).
func (r *Requestor) MatterQuiesce() {
	r.mu.Lock()
	defer r.mu.Unlock()
	clear(r.holds)
	r.recheckLocked()
}

// recheckLocked wakes the held signals to look again (matter.js
// recheck.emit()). The caller holds mu.
func (r *Requestor) recheckLocked() {
	close(r.recheck)
	r.recheck = make(chan struct{})
}

// isHeldLocked reports whether a signal for key waits: a hold for its peer
// exists and its session is not tracked. Mirrors matter.js
// WebRtcTransportRequestorServer.ts:345-347 (#isHeld). The caller holds
// mu.
func (r *Requestor) isHeldLocked(key wrdef.SessionKey) bool {
	if slices.ContainsFunc(r.sessions, func(s WebRTCSession) bool { return wrdef.KeyOf(s) == key }) {
		return false
	}
	for h := range r.holds {
		if h.fabricIndex == key.FabricIndex && h.peerNodeID == key.PeerNodeID {
			return true
		}
	}
	return false
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
//
// The table is kept between the store and the notification
// (internal/specseam.SetAttributesStored): a change listener, and whatever
// it wakes, reads through Sessions() the table it is told about (PR 1
// review M1, decision 682). keep runs under the spec server's lock
// (decision 689), so it takes only mu, and no holder of mu calls the
// spec server: the lock order is pub, then the server's, then mu.
// Mirrors matter.js Datasource.ts at db91886c:
// commit2 makes the committed values canonical (:1475-1489, the
// assignment :1488) before postCommit emits the change events (:1520),
// and a refused commit's values never become canonical (rollback,
// :1539-1540).
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
	keep := func() {
		r.mu.Lock()
		r.sessions = next
		// The committed change marks the held signals whose session it
		// tracks and wakes them all (matter.js #sessionsChanged,
		// WebRtcTransportRequestorServer.ts:231-232).
		for w := range r.waiters {
			if !w.registered && slices.ContainsFunc(next, func(s WebRTCSession) bool { return wrdef.KeyOf(s) == w.key }) {
				w.registered = true
			}
		}
		r.recheckLocked()
		r.mu.Unlock()
	}
	if err := specseam.SetAttributesStored(r.srv, origin, map[uint32]any{wrdef.AttrCurrentSessions: spec.List[WebRTCSession](next)}, keep); err != nil {
		return fmt.Errorf("camera: CurrentSessions: %w", err)
	}
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

// MatterWriteChanged implements [contract.ChangeReportingWriter]: the
// generated server's write and its verdict.
func (r *Requestor) MatterWriteChanged(ctx context.Context, attrID uint32, value any) (bool, error) {
	return r.srv.MatterWriteChanged(ctx, attrID, value)
}

// MatterInvoke dispatches a signalling command; a host's handler runs in
// its place ([spec.SelfHeld.Handle]).
func (r *Requestor) MatterInvoke(ctx context.Context, cmdID uint32, fields any) (any, error) {
	return spec.InvokeHeld(ctx, &r.SelfHeld, cmdID, fields)
}

// offer carries out Offer: the session the peer names is surfaced with the
// request. Mirrors matter.js WebRtcTransportRequestorServer.ts:257-261.
func (r *Requestor) offer(ctx context.Context, fields any) (any, error) {
	req, ok := spec.RequestOf[wrdef.OfferRequest](fields)
	if !ok {
		return nil, spec.Errorf(im.StatusInvalidCommand, "WebRtcTransportRequestor: Offer payload %T", fields)
	}
	key, err := r.awaitSession(ctx, wrdef.SignalOffer, req.WebRtcSessionId)
	if err != nil {
		return nil, err
	}
	r.deliver.RLock()
	defer r.deliver.RUnlock()
	session, err := r.findSessionStrict(ctx, wrdef.SignalOffer, key)
	if err != nil {
		return nil, err
	}
	if r.port != nil {
		r.port.Offer(ctx, session, req)
	}
	return nil, nil
}

// answer carries out Answer: the session is surfaced with the SDP. Mirrors
// matter.js WebRtcTransportRequestorServer.ts:267-271.
func (r *Requestor) answer(ctx context.Context, fields any) (any, error) {
	req, ok := spec.RequestOf[wrdef.AnswerRequest](fields)
	if !ok {
		return nil, spec.Errorf(im.StatusInvalidCommand, "WebRtcTransportRequestor: Answer payload %T", fields)
	}
	key, err := r.awaitSession(ctx, wrdef.SignalAnswer, req.WebRtcSessionId)
	if err != nil {
		return nil, err
	}
	r.deliver.RLock()
	defer r.deliver.RUnlock()
	session, err := r.findSessionStrict(ctx, wrdef.SignalAnswer, key)
	if err != nil {
		return nil, err
	}
	if r.port != nil {
		r.port.Answer(ctx, session, req.Sdp)
	}
	return nil, nil
}

// iceCandidates carries out IceCandidates: an empty list is
// CONSTRAINT_ERROR before any session is looked up or waited for (a peer's
// empty list is refused by the request's schema already, "min 1"); the
// session is surfaced with the candidates. Mirrors matter.js
// WebRtcTransportRequestorServer.ts:276-287.
func (r *Requestor) iceCandidates(ctx context.Context, fields any) (any, error) {
	req, ok := spec.RequestOf[wrdef.IceCandidatesRequest](fields)
	if !ok {
		return nil, spec.Errorf(im.StatusInvalidCommand, "WebRtcTransportRequestor: IceCandidates payload %T", fields)
	}
	if len(req.IceCandidates) == 0 { // :280-284
		return nil, spec.Errorf(im.StatusConstraintError, "WebRtcTransportRequestor: ICE candidates list must not be empty")
	}
	key, err := r.awaitSession(ctx, wrdef.SignalIceCandidates, req.WebRtcSessionId)
	if err != nil {
		return nil, err
	}
	r.deliver.RLock()
	defer r.deliver.RUnlock()
	session, err := r.findSessionStrict(ctx, wrdef.SignalIceCandidates, key)
	if err != nil {
		return nil, err
	}
	if r.port != nil {
		r.port.IceCandidates(ctx, session, req.IceCandidates)
	}
	return nil, nil
}

// end carries out End: the session leaves the table, then it is surfaced
// with the reason. The lookup and the removal are one step under pub, so
// of two Ends for one session one removes it and is surfaced, and the
// other is refused NOT_FOUND, as is an End for a session the host removed
// while the End waited. The removal waits for every Offer, Answer and
// IceCandidates already at the port (deliver), so none reaches the port
// after the End for its session. Mirrors matter.js
// WebRtcTransportRequestorServer.ts:292-297 (removeSession under the lock
// #lockSession took, :294-296).
func (r *Requestor) end(ctx context.Context, fields any) (any, error) {
	req, ok := spec.RequestOf[wrdef.EndRequest](fields)
	if !ok {
		return nil, spec.Errorf(im.StatusInvalidCommand, "WebRtcTransportRequestor: End payload %T", fields)
	}
	key, err := r.awaitSession(ctx, wrdef.SignalEnd, req.WebRtcSessionId)
	if err != nil {
		return nil, err
	}
	var session WebRTCSession
	found := false
	r.deliver.Lock()
	err = r.change(contract.OriginCommand, func(sessions []WebRTCSession) ([]WebRTCSession, bool) {
		idx := slices.IndexFunc(sessions, func(s WebRTCSession) bool { return wrdef.KeyOf(s) == key })
		if idx < 0 {
			return sessions, false
		}
		session, found = sessions[idx], true
		return slices.Delete(sessions, idx, idx+1), true
	})
	r.deliver.Unlock()
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, r.refuse(ctx, wrdef.SignalEnd, key)
	}
	if r.port != nil {
		r.port.End(ctx, session, req.Reason)
	}
	return nil, nil
}

// findSessionStrict finds the session of key — the id a signal names on
// the peer's fabric from the peer's node, once a held signal's wait ended
// ([Requestor.awaitSession]). An unknown session is reported to the
// port's [RequestorRefusals] and answered NOT_FOUND. The caller holds
// deliver shared. Mirrors matter.js WebRtcTransportRequestorServer.ts:306-337
// (#lockSession and #sessionOrRefuse: the lookup after the wait :317-318,
// :327, the refusal :328-335).
func (r *Requestor) findSessionStrict(ctx context.Context, signal Signal, key wrdef.SessionKey) (WebRTCSession, error) {
	r.mu.Lock()
	idx := slices.IndexFunc(r.sessions, func(s WebRTCSession) bool { return wrdef.KeyOf(s) == key })
	var session WebRTCSession
	if idx >= 0 {
		session = r.sessions[idx]
	}
	r.mu.Unlock()
	if idx < 0 {
		return WebRTCSession{}, r.refuse(ctx, signal, key)
	}
	return session, nil
}

// refuse reports a signal for an unknown session to the port's
// [RequestorRefusals] and returns its NOT_FOUND. Mirrors matter.js
// WebRtcTransportRequestorServer.ts:328-335 (refused.emit :333; the
// debug line's wait outcome :329-332 has no Go counterpart, see
// notes/parity/by_design.md).
func (r *Requestor) refuse(ctx context.Context, signal Signal, key wrdef.SessionKey) error {
	if obs, ok := r.port.(RequestorRefusals); ok {
		obs.Refused(ctx, signal, key.ID, key.FabricIndex, key.PeerNodeID)
	}
	return spec.Errorf(im.StatusNotFound, "WebRTC session %d not found", key.ID)
}

// awaitSession returns the session key a signal names for the invoking
// peer, after the signal's wait for its registration: a command without a
// remote node session behind it (a local call, a group message) fails, as
// matter.js's assertRemoteActor and NodeSession.assert throw (#peer,
// WebRtcTransportRequestorServer.ts:339-343). A signal for an untracked
// session of a held peer waits — at most [EarlySignalHoldTime] from its
// arrival, a timer started once and not restarted — until its session is
// tracked (a committed change tracked it, even if a later one removed it
// again before the signal looked), no hold for its peer is left, or ctx
// ends. It waits holding
// neither pub, nor mu, nor the spec server's lock (matter.js
// lockOnInvoke = false, :57), and it hands its CASE session's turn to the
// session's next message first, so the peer's other signals on that
// session are not queued behind it. Mirrors matter.js
// WebRtcTransportRequestorServer.ts:362-389 (#awaitRegistration).
func (r *Requestor) awaitSession(ctx context.Context, signal Signal, id uint16) (wrdef.SessionKey, error) {
	_, fabric, remote := im.LookupFabricFilter(ctx)
	if _, group := im.GroupSubjectFromContext(ctx); !remote || group {
		return wrdef.SessionKey{}, spec.Errorf(im.StatusFailure, "WebRtcTransportRequestor: %s needs a peer's node session", signal)
	}
	node, _ := im.SubjectFromContext(ctx)
	key := wrdef.SessionKey{ID: id, FabricIndex: fabric, PeerNodeID: node}
	w := &signalWaiter{key: key}
	r.mu.Lock()
	held := r.isHeldLocked(key)
	if held {
		r.waiters[w] = struct{}{}
	}
	r.mu.Unlock()
	if !held { // :366-368
		return key, nil
	}
	defer func() {
		r.mu.Lock()
		delete(r.waiters, w)
		r.mu.Unlock()
	}()
	timeout := r.clock.NewTimer(EarlySignalHoldTime) // :373, from the arrival
	defer timeout.Stop()
	if leave := specseam.LeaveSessionTurn; leave != nil {
		leave(ctx)
	}
	for { // :374-384
		r.mu.Lock()
		held, wake := !w.registered && r.isHeldLocked(key), r.recheck
		r.mu.Unlock()
		if !held {
			return key, nil
		}
		select {
		case <-wake:
		case <-timeout.C():
			return key, nil
		case <-ctx.Done():
			return key, nil
		}
	}
}

// The host methods the typed server's Host returns (brief CK-2).
var _ wrdef.Host = (*Requestor)(nil)
