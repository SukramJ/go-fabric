// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"context"
	"errors"
	"slices"
	"sync"

	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/im"
)

// outcome is a result that arrives once: the Go form of the promises
// matterjs-server's registry hands around. Waiters block until it is
// resolved or their context ends.
type outcome[T any] struct {
	done  chan struct{}
	value T
	err   error
}

func newOutcome[T any]() *outcome[T] { return &outcome[T]{done: make(chan struct{})} }

// resolvedOutcome is an outcome that has already arrived.
func resolvedOutcome[T any](value T, err error) *outcome[T] {
	o := newOutcome[T]()
	o.resolve(value, err)
	return o
}

// resolve settles the outcome; it is called once.
func (o *outcome[T]) resolve(value T, err error) {
	o.value, o.err = value, err
	close(o.done)
}

// wait blocks until the outcome arrives or ctx ends.
func (o *outcome[T]) wait(ctx context.Context) (T, error) {
	select {
	case <-o.done:
		return o.value, o.err
	case <-ctx.Done():
		var zero T
		return zero, ctx.Err()
	}
}

// deviceForgotSession reports whether an EndSession failed because the
// camera knows no such session: NotFound for any unknown id. Mirrors
// matterjs-server packages/ws-controller/src/controller/webRtcSessionTracking.ts:35-41
// at 0bd06f2.
func deviceForgotSession(err error) bool {
	var se *interaction.StatusError
	return errors.As(err, &se) && se.Status == im.StatusNotFound
}

// endedOnDevice is true when the device ended the session, false when it
// answered NotFound; any other failure is returned. Mirrors matterjs-server
// packages/ws-controller/src/controller/webRtcSessionTracking.ts:43-52 at
// 0bd06f2.
func endedOnDevice(err error) (bool, error) {
	switch {
	case err == nil:
		return true, nil
	case deviceForgotSession(err):
		return false, nil
	default:
		return false, err
	}
}

// invokeEndSession sends an EndSession and drops the local records on
// success (deviceHeldSession true) and on deviceForgotSession (false); any
// other failure keeps them, so a later stop, disconnect or shutdown still
// ends the session. Mirrors matterjs-server
// packages/ws-controller/src/controller/webRtcSessionTracking.ts:54-72 at
// 0bd06f2.
func invokeEndSession(ctx context.Context, send func(context.Context) error, dropRecords func(deviceHeldSession bool)) error {
	err := send(ctx)
	switch {
	case err == nil:
		dropRecords(true)
	case deviceForgotSession(err):
		dropRecords(false)
	}
	return err
}

// sessionScope names a session's node, endpoint, owner connection and the
// owner's key for it. Mirrors matterjs-server
// packages/ws-controller/src/camera/sessionRegistry.ts:11-16 at 0bd06f2.
type sessionScope struct {
	nodeID           uint64
	endpointID       uint16
	connectionID     string
	clientSessionKey string
}

// scope is a managed session's scope.
func (s *managedSession) scope() sessionScope {
	return sessionScope{nodeID: s.nodeID, endpointID: s.endpointID, connectionID: s.connectionID, clientSessionKey: s.clientSessionKey}
}

// sessionOwner is who receives a session's signalling, and the name that
// connection gave the session ("" for none). Mirrors matterjs-server
// packages/ws-controller/src/camera/sessionRegistry.ts:18-22, :499-504 at
// 0bd06f2.
type sessionOwner struct {
	connectionID     string
	clientSessionKey string
}

// pendingSession is a session being established, identified by its
// pointer, so a release path that runs before finish can see it. Mirrors
// matterjs-server packages/ws-controller/src/camera/sessionRegistry.ts:27-28
// at 0bd06f2.
type pendingSession struct {
	scope sessionScope
}

// keyedSession is a connection's session found by its clientSessionKey:
// tracked (session set), or still being established (pending set). Mirrors
// matterjs-server packages/ws-controller/src/camera/sessionRegistry.ts:24-25
// at 0bd06f2.
type keyedSession struct {
	session *managedSession
	pending *pendingSession
}

// cameraEnd is an end the camera told of: a CurrentSessions report no
// longer listing the session, or the peer's End. Mirrors matterjs-server
// packages/ws-controller/src/camera/sessionRegistry.ts:30-31 at 0bd06f2.
type cameraEnd = sessionEnd

// lifecycle is everything learned about one session, from begin until it
// ends; the tracked entry is the registration's own lifecycle, so what was
// learned while establishing still counts. A registration stays pending
// until finish, also after track and after it ended. Mirrors
// matterjs-server packages/ws-controller/src/camera/sessionRegistry.ts:33-61
// at 0bd06f2.
type lifecycle struct {
	nodeID     uint64
	endpointID uint16
	// sessionID is the camera's id, once hasID.
	sessionID uint16
	hasID     bool
	session   *managedSession
	// listed: a report listed the id; only then can a report without it
	// end the session.
	listed bool
	// cameraEnded is acted on once no EndSession for the session is in
	// flight; the first evidence wins.
	cameraEnded  *cameraEnd
	endsInFlight int
	// stopping is the one EndSession that stops and releases of this
	// session share.
	stopping *outcome[bool]
	over     bool
}

// claimState is who took a registration: a release path (closing
// connection, shutdown) or its owner's stop.
type claimState uint8

// The claims of a registration.
const (
	unclaimed claimState = iota
	claimReleased
	claimStopped
)

// pendingRecord is a registration's state. Mirrors matterjs-server
// packages/ws-controller/src/camera/sessionRegistry.ts:63-73 at 0bd06f2.
type pendingRecord struct {
	claimed claimState
	settled chan struct{}
	// superseded: the camera gave the id to another session, so this
	// registration neither receives its signalling nor ends it.
	superseded bool
	life       *lifecycle
	// ending is the EndSession the establishing call sent for the session
	// it gave up.
	ending *outcome[bool]
}

// trackRefusal is why track refused a registration. Mirrors matterjs-server
// packages/ws-controller/src/camera/sessionRegistry.ts:75-79 at 0bd06f2.
type trackRefusal uint8

// The refusals: claimed (a release path is waiting for it), stopped (its
// owner cancelled it), alreadyEnded (it ended between the offer response and
// track) — the caller must end the session — and superseded: the id names
// another session now, so the caller must not end it or drop its local
// tracking.
const (
	trackAccepted trackRefusal = iota
	refusalClaimed
	refusalStopped
	refusalAlreadyEnded
	refusalSuperseded
)

// claimedSessions is what claim took over: tracked sessions, each to be
// stopped with stop, and the starts in flight, whose channels close once
// the establishing call, which ends the session itself, has finished.
// Mirrors matterjs-server packages/ws-controller/src/camera/sessionRegistry.ts:81-87
// at 0bd06f2.
type claimedSessions struct {
	tracked  []managedSession
	starting []<-chan struct{}
}

// sessionKey keys a tracked session: WebRTCSessionID is unique per node
// (§11.4.5.1), and every route names a session by all three. Mirrors
// matterjs-server packages/ws-controller/src/camera/sessionRegistry.ts:116-118
// at 0bd06f2.
type sessionKey struct {
	nodeID     uint64
	endpointID uint16
	sessionID  uint16
}

// sessionRegistry ends and announces every session at most once, whatever
// order reports, stops, releases, peer Ends, reissued ids and node removal
// arrive in. Ends the camera tells of are recorded and applied by settle
// once no EndSession is in flight, so a finished EndSession names who asked
// for it, and a failed one leaves the recorded end to apply. Mirrors
// matterjs-server packages/ws-controller/src/camera/sessionRegistry.ts:89-497
// at 0bd06f2.
//
// It is safe for concurrent use. announce and vanished run outside the
// registry's lock, in the order the registry decided them, on the
// goroutine of the call that decided them or of a call delivering at the
// same time; they may call into the registry.
type sessionRegistry struct {
	announce func(cameraSessionEnded)
	// vanished is called for a tracked session a report no longer lists,
	// after it was announced: the local requestor still holds it, since no
	// EndSession or End went through it.
	vanished func(managedSession, sessionEndCause)

	mu sync.Mutex
	// tracked and trackedOrder are matterjs-server's #tracked Map, in
	// insertion order.
	tracked      map[sessionKey]*lifecycle
	trackedOrder []sessionKey
	// pending and pendingOrder are its #pending Map, in insertion order.
	pending      map[*pendingSession]*pendingRecord
	pendingOrder []*pendingSession
	queue        []func()
	delivering   bool
}

// newSessionRegistry is a registry calling announce for every end and
// vanished for a tracked session a report dropped. Mirrors matterjs-server
// packages/ws-controller/src/camera/sessionRegistry.ts:104-114 at 0bd06f2.
func newSessionRegistry(announce func(cameraSessionEnded), vanished func(managedSession, sessionEndCause)) *sessionRegistry {
	return &sessionRegistry{
		announce: announce,
		vanished: vanished,
		tracked:  map[sessionKey]*lifecycle{},
		pending:  map[*pendingSession]*pendingRecord{},
	}
}

// unlock releases the lock and delivers the queued callbacks, unless a
// call already delivers, which then delivers these too.
func (r *sessionRegistry) unlock() {
	if r.delivering {
		r.mu.Unlock()
		return
	}
	r.delivering = true
	for len(r.queue) > 0 {
		calls := r.queue
		r.queue = nil
		r.mu.Unlock()
		for _, call := range calls {
			call()
		}
		r.mu.Lock()
	}
	r.delivering = false
	r.mu.Unlock()
}

// trackedSessions is the tracked sessions in insertion order. Mirrors
// matterjs-server packages/ws-controller/src/camera/sessionRegistry.ts:120-122
// at 0bd06f2.
func (r *sessionRegistry) trackedSessions() []*managedSession {
	var out []*managedSession
	for _, k := range r.trackedOrder {
		if life := r.tracked[k]; life.session != nil {
			out = append(out, life.session)
		}
	}
	return out
}

func (r *sessionRegistry) setTracked(k sessionKey, life *lifecycle) {
	if _, ok := r.tracked[k]; !ok {
		r.trackedOrder = append(r.trackedOrder, k)
	}
	r.tracked[k] = life
}

func (r *sessionRegistry) deleteTracked(k sessionKey) {
	delete(r.tracked, k)
	r.trackedOrder = slices.DeleteFunc(r.trackedOrder, func(o sessionKey) bool { return o == k })
}

// begin registers a session being established. Every exit of the
// establishing call must finish the returned registration; the caller makes
// sure clientSessionKey is not clientSessionKeyInUse. Mirrors
// matterjs-server packages/ws-controller/src/camera/sessionRegistry.ts:124-146
// at 0bd06f2.
func (r *sessionRegistry) begin(nodeID uint64, endpointID uint16, connectionID, clientSessionKey string) *pendingSession {
	p := &pendingSession{scope: sessionScope{nodeID: nodeID, endpointID: endpointID, connectionID: connectionID, clientSessionKey: clientSessionKey}}
	r.mu.Lock()
	defer r.unlock()
	r.pending[p] = &pendingRecord{
		settled: make(chan struct{}),
		life:    &lifecycle{nodeID: nodeID, endpointID: endpointID},
	}
	r.pendingOrder = append(r.pendingOrder, p)
	return p
}

// clientSessionKeyInUse reports whether the connection names a session by
// the key, on any node: a key names one session of its connection, so
// events and commands are unambiguous. Mirrors matterjs-server
// packages/ws-controller/src/camera/sessionRegistry.ts:148-155 at 0bd06f2.
func (r *sessionRegistry) clientSessionKeyInUse(connectionID, clientSessionKey string) bool {
	r.mu.Lock()
	defer r.unlock()
	owns := func(s sessionScope) bool {
		return s.connectionID == connectionID && s.clientSessionKey == clientSessionKey
	}
	for _, s := range r.trackedSessions() {
		if owns(s.scope()) {
			return true
		}
	}
	for _, p := range r.pendingOrder {
		if owns(p.scope) {
			return true
		}
	}
	return false
}

// byClientKey finds the connection's session of the key on the node and
// endpoint. A tracked entry wins over its own registration, which stays
// until finish; never another connection's session: keys are scoped to the
// connection. Mirrors matterjs-server
// packages/ws-controller/src/camera/sessionRegistry.ts:157-175 at 0bd06f2.
func (r *sessionRegistry) byClientKey(nodeID uint64, endpointID uint16, connectionID, clientSessionKey string) (keyedSession, bool) {
	r.mu.Lock()
	defer r.unlock()
	want := sessionScope{nodeID: nodeID, endpointID: endpointID, connectionID: connectionID, clientSessionKey: clientSessionKey}
	for _, s := range r.trackedSessions() {
		if s.scope() == want {
			session := *s
			return keyedSession{session: &session}, true
		}
	}
	for _, p := range r.pendingOrder {
		if p.scope == want {
			return keyedSession{pending: p}, true
		}
	}
	return keyedSession{}, false
}

// cancel is the owner stopping a session it is still establishing: track
// refuses it with refusalStopped, so the establishing call ends it. The
// returned wait answers once that call finished, with the outcome of its
// EndSession (see ending), or false when it ended nothing: it gave up
// before the camera opened a session, or the session's id names another
// session now. Mirrors matterjs-server
// packages/ws-controller/src/camera/sessionRegistry.ts:177-188 at 0bd06f2.
func (r *sessionRegistry) cancel(p *pendingSession) func(context.Context) (bool, error) {
	r.mu.Lock()
	defer r.unlock()
	record, ok := r.pending[p]
	if !ok {
		return func(context.Context) (bool, error) { return false, nil }
	}
	if record.claimed == unclaimed {
		record.claimed = claimStopped
	}
	return func(ctx context.Context) (bool, error) {
		select {
		case <-record.settled:
		case <-ctx.Done():
			return false, ctx.Err()
		}
		// record.ending is written under the lock before finish closes
		// settled, and never after.
		if record.ending == nil {
			return false, nil
		}
		return record.ending.wait(ctx)
	}
}

// ending records the EndSession the establishing call sent for the session
// track refused; a cancel answers with it. Mirrors matterjs-server
// packages/ws-controller/src/camera/sessionRegistry.ts:190-194 at 0bd06f2.
func (r *sessionRegistry) ending(p *pendingSession, ending *outcome[bool]) {
	r.mu.Lock()
	defer r.unlock()
	if record, ok := r.pending[p]; ok {
		record.ending = ending
	}
}

// claimRefusal is what track would refuse for a claim, asked before the
// offer so a claimed start sends none; trackAccepted for none. Mirrors
// matterjs-server packages/ws-controller/src/camera/sessionRegistry.ts:196-201
// at 0bd06f2.
func (r *sessionRegistry) claimRefusal(p *pendingSession) trackRefusal {
	r.mu.Lock()
	defer r.unlock()
	return r.claimRefusalLocked(p)
}

func (r *sessionRegistry) claimRefusalLocked(p *pendingSession) trackRefusal {
	record, ok := r.pending[p]
	switch {
	case !ok || record.claimed == claimReleased:
		return refusalClaimed
	case record.claimed == claimStopped:
		return refusalStopped
	default:
		return trackAccepted
	}
}

// establishing gives a registration the camera's id; it must be called
// before the local requestor is given the session, so a later End can be
// attributed. Any other record still holding the id is a session the
// camera lost (it numbers sessions from 0 again after a reboot), handled as
// issued does. Mirrors matterjs-server
// packages/ws-controller/src/camera/sessionRegistry.ts:203-212 at 0bd06f2.
func (r *sessionRegistry) establishing(p *pendingSession, sessionID uint16) {
	r.mu.Lock()
	defer r.unlock()
	r.issuedLocked(p.scope.nodeID, p.scope.endpointID, sessionID, p)
	if record, ok := r.pending[p]; ok {
		record.life.sessionID, record.life.hasID = sessionID, true
	}
}

// issued records that the camera handed the id to a session another route
// established: a session still recorded under it is one the camera lost,
// so an established entry is dropped and announced as camera_restarted,
// and a registration waiting for it is marked superseded. Mirrors
// matterjs-server packages/ws-controller/src/camera/sessionRegistry.ts:214-229
// at 0bd06f2.
func (r *sessionRegistry) issued(nodeID uint64, endpointID, sessionID uint16) {
	r.mu.Lock()
	defer r.unlock()
	r.issuedLocked(nodeID, endpointID, sessionID, nil)
}

func (r *sessionRegistry) issuedLocked(nodeID uint64, endpointID, sessionID uint16, to *pendingSession) {
	if lost, ok := r.tracked[sessionKey{nodeID, endpointID, sessionID}]; ok {
		r.close(lost, sessionEnd{cause: endCameraRestarted})
	}
	for _, p := range r.holding(nodeID, endpointID, sessionID) {
		if p != to {
			r.pending[p].superseded = true
		}
	}
}

// holding is the registrations on the endpoint the camera gave the id, in
// insertion order. Mirrors matterjs-server
// packages/ws-controller/src/camera/sessionRegistry.ts:231-245 at 0bd06f2.
func (r *sessionRegistry) holding(nodeID uint64, endpointID, sessionID uint16) []*pendingSession {
	var out []*pendingSession
	for _, p := range r.pendingOrder {
		life := r.pending[p].life
		if p.scope.nodeID == nodeID && p.scope.endpointID == endpointID && life.hasID && life.sessionID == sessionID {
			out = append(out, p)
		}
	}
	return out
}

// named is the lifecycle of the live session known under the id: tracked,
// or still being established. Mirrors matterjs-server
// packages/ws-controller/src/camera/sessionRegistry.ts:247-255 at 0bd06f2.
func (r *sessionRegistry) named(nodeID uint64, endpointID, sessionID uint16) *lifecycle {
	if life, ok := r.tracked[sessionKey{nodeID, endpointID, sessionID}]; ok {
		return life
	}
	for _, p := range r.holding(nodeID, endpointID, sessionID) {
		if record := r.pending[p]; !record.superseded {
			return record.life
		}
	}
	return nil
}

// track turns a registration into a tracked session, or says why not:
// superseded wins over a claim, because ending the id would end another
// session. Mirrors matterjs-server
// packages/ws-controller/src/camera/sessionRegistry.ts:257-275 at 0bd06f2.
func (r *sessionRegistry) track(p *pendingSession, session managedSession) trackRefusal {
	r.mu.Lock()
	defer r.unlock()
	record, ok := r.pending[p]
	if !ok {
		return refusalClaimed
	}
	if record.superseded {
		return refusalSuperseded
	}
	if refusal := r.claimRefusalLocked(p); refusal != trackAccepted {
		return refusal
	}
	life := record.life
	if life.over || life.cameraEnded != nil {
		return refusalAlreadyEnded
	}
	life.sessionID, life.hasID = session.webRTCSessionID, true
	life.session = &session
	r.setTracked(sessionKey{session.nodeID, session.endpointID, session.webRTCSessionID}, life)
	return trackAccepted
}

// finish ends a registration once the establishing call has dealt with the
// session, even by ending it: a claiming release waits for this. Mirrors
// matterjs-server packages/ws-controller/src/camera/sessionRegistry.ts:277-282
// at 0bd06f2.
func (r *sessionRegistry) finish(p *pendingSession) {
	r.mu.Lock()
	defer r.unlock()
	record, ok := r.pending[p]
	if !ok {
		return
	}
	delete(r.pending, p)
	r.pendingOrder = slices.DeleteFunc(r.pendingOrder, func(o *pendingSession) bool { return o == p })
	close(record.settled)
}

// signallingOwner is who receives the session's signalling, ok false when
// no entry names the session (raw provider route, or already dropped). A
// registration counts once establishing gave it the id: the local requestor
// accepts the camera's signalling from then on, before track runs. A
// superseded registration does not. Mirrors matterjs-server
// packages/ws-controller/src/camera/sessionRegistry.ts:284-296 at 0bd06f2.
func (r *sessionRegistry) signallingOwner(nodeID uint64, endpointID, sessionID uint16) (sessionOwner, bool) {
	r.mu.Lock()
	defer r.unlock()
	if life, ok := r.tracked[sessionKey{nodeID, endpointID, sessionID}]; ok && life.session != nil {
		return sessionOwner{connectionID: life.session.connectionID, clientSessionKey: life.session.clientSessionKey}, true
	}
	for _, p := range r.holding(nodeID, endpointID, sessionID) {
		if !r.pending[p].superseded {
			return sessionOwner{connectionID: p.scope.connectionID, clientSessionKey: p.scope.clientSessionKey}, true
		}
	}
	return sessionOwner{}, false
}

// get is the tracked session of the id. Mirrors matterjs-server
// packages/ws-controller/src/camera/sessionRegistry.ts:298-300 at 0bd06f2.
func (r *sessionRegistry) get(nodeID uint64, endpointID, sessionID uint16) (managedSession, bool) {
	r.mu.Lock()
	defer r.unlock()
	if life, ok := r.tracked[sessionKey{nodeID, endpointID, sessionID}]; ok && life.session != nil {
		return *life.session, true
	}
	return managedSession{}, false
}

// stop sends EndSession for a stop or a release (stopping a stream, a
// closing owner, shutdown). Concurrent calls for one session share one
// EndSession and its answer: true when the camera ended the session, false
// on NotFound; any other failure is returned and leaves the session to a
// later attempt. An id nothing names is sent all the same: the camera's
// CurrentSessions is the record of what exists. Mirrors matterjs-server
// packages/ws-controller/src/camera/sessionRegistry.ts:302-324 at 0bd06f2.
func (r *sessionRegistry) stop(ctx context.Context, nodeID uint64, endpointID, sessionID uint16, end sessionEnd, send func(context.Context) error) (bool, error) {
	r.mu.Lock()
	life := r.named(nodeID, endpointID, sessionID)
	if life == nil {
		r.unlock()
		return endedOnDevice(r.end(ctx, nil, sessionKey{nodeID, endpointID, sessionID}, end, send))
	}
	if shared := life.stopping; shared != nil {
		r.unlock()
		return shared.wait(ctx)
	}
	stopping := newOutcome[bool]()
	life.stopping = stopping
	r.unlock()

	ended, err := endedOnDevice(r.end(ctx, life, sessionKey{nodeID, endpointID, sessionID}, end, send))
	r.mu.Lock()
	if life.stopping == stopping {
		life.stopping = nil
	}
	r.unlock()
	stopping.resolve(ended, err)
	return ended, err
}

// endSession is a client's own EndSession (a raw provider command): its own
// invoke, whose error is passed on, ending the session as stop does.
// Mirrors matterjs-server packages/ws-controller/src/camera/sessionRegistry.ts:326-345
// at 0bd06f2.
func (r *sessionRegistry) endSession(ctx context.Context, nodeID uint64, endpointID, sessionID uint16, end sessionEnd, send func(context.Context) error) error {
	r.mu.Lock()
	life := r.named(nodeID, endpointID, sessionID)
	r.unlock()
	return r.end(ctx, life, sessionKey{nodeID, endpointID, sessionID}, end, send)
}

// end sends the EndSession, counted on life from before send runs until it
// settled, so settle waits for it. Mirrors matterjs-server
// packages/ws-controller/src/camera/sessionRegistry.ts:347-370 at 0bd06f2.
func (r *sessionRegistry) end(ctx context.Context, life *lifecycle, k sessionKey, end sessionEnd, send func(context.Context) error) error {
	if life == nil {
		return invokeEndSession(ctx, send, func(deviceHeldSession bool) {
			if deviceHeldSession {
				r.mu.Lock()
				r.announceEnd(k, end, nil)
				r.unlock()
			}
		})
	}
	r.mu.Lock()
	life.endsInFlight++
	r.unlock()
	err := invokeEndSession(ctx, send, func(deviceHeldSession bool) {
		r.mu.Lock()
		r.endSessionAnswered(life, k, end, deviceHeldSession)
		r.unlock()
	})
	r.mu.Lock()
	life.endsInFlight--
	r.settle(life)
	r.unlock()
	return err
}

// endSessionAnswered applies an answered EndSession: a tracked session is
// announced with the EndSession's requester, on NotFound with the end the
// camera reported, if any. A session still being established is not
// announced to its owner (its start fails instead); everyone else is told,
// as for an id nothing names, unless the controller gave the session up.
// Mirrors matterjs-server packages/ws-controller/src/camera/sessionRegistry.ts:372-394
// at 0bd06f2.
func (r *sessionRegistry) endSessionAnswered(life *lifecycle, k sessionKey, end sessionEnd, deviceHeldSession bool) {
	if life.over {
		return
	}
	if life.session != nil {
		if !deviceHeldSession && life.cameraEnded != nil {
			end = *life.cameraEnded
		}
		r.close(life, end)
		return
	}
	life.over = true
	if deviceHeldSession && end.cause != endReleased {
		r.announceEnd(k, end, nil)
	}
}

// peerEnded records that the peer ended the session and reports whether a
// tracked session had the id. With an EndSession in flight the end is
// announced once that finished, with the EndSession's requester if it
// succeeded. hasReason says whether the camera stated reason. Mirrors
// matterjs-server packages/ws-controller/src/camera/sessionRegistry.ts:396-407
// at 0bd06f2.
func (r *sessionRegistry) peerEnded(nodeID uint64, endpointID, sessionID uint16, reason uint8, hasReason bool) bool {
	r.mu.Lock()
	defer r.unlock()
	life := r.named(nodeID, endpointID, sessionID)
	if life == nil {
		return false
	}
	if life.cameraEnded == nil {
		life.cameraEnded = &cameraEnd{cause: endPeer, reason: reason, hasReason: hasReason}
	}
	tracked := life.session != nil
	r.settle(life)
	return tracked
}

// reconcile applies one CurrentSessions report of an endpoint (present: the
// ids it lists for this controller), to tracked sessions and to
// registrations the camera gave an id. A session ends by a report only
// after an earlier report listed it: a report built before a new session's
// offer does not list it. Mirrors matterjs-server
// packages/ws-controller/src/camera/sessionRegistry.ts:409-432 at 0bd06f2.
func (r *sessionRegistry) reconcile(nodeID uint64, endpointID uint16, present map[uint16]bool, cause sessionEndCause) {
	r.mu.Lock()
	defer r.unlock()
	onEndpoint := func(life *lifecycle) bool {
		return life.nodeID == nodeID && life.endpointID == endpointID && life.hasID
	}
	var reported []*lifecycle
	add := func(life *lifecycle) {
		if onEndpoint(life) && !slices.Contains(reported, life) {
			reported = append(reported, life)
		}
	}
	for _, k := range r.trackedOrder {
		add(r.tracked[k])
	}
	for _, p := range r.pendingOrder {
		add(r.pending[p].life)
	}
	for _, life := range reported {
		switch {
		case present[life.sessionID]:
			life.listed = true
		case life.listed && life.cameraEnded == nil:
			life.cameraEnded = &cameraEnd{cause: cause}
		}
	}
	for _, life := range reported {
		r.settle(life)
	}
}

// forgetNode drops every entry of a removed node, announced as
// node_removed; its registrations are marked ended. Mirrors matterjs-server
// packages/ws-controller/src/camera/sessionRegistry.ts:434-442 at 0bd06f2.
func (r *sessionRegistry) forgetNode(nodeID uint64) {
	r.mu.Lock()
	defer r.unlock()
	for _, k := range slices.Clone(r.trackedOrder) {
		if life := r.tracked[k]; life.nodeID == nodeID {
			r.close(life, sessionEnd{cause: endNodeRemoved})
		}
	}
	for _, p := range r.pendingOrder {
		if p.scope.nodeID == nodeID {
			r.pending[p].life.over = true
		}
	}
}

// claim takes responsibility for every session in scope, established or
// being established; a claimed registration ends itself instead of being
// tracked. Mirrors matterjs-server
// packages/ws-controller/src/camera/sessionRegistry.ts:444-460 at 0bd06f2.
func (r *sessionRegistry) claim(matches func(sessionScope) bool) claimedSessions {
	r.mu.Lock()
	defer r.unlock()
	var out claimedSessions
	for _, s := range r.trackedSessions() {
		if matches(s.scope()) {
			out.tracked = append(out.tracked, *s)
		}
	}
	for _, p := range r.pendingOrder {
		if !matches(p.scope) {
			continue
		}
		record := r.pending[p]
		record.claimed = claimReleased
		out.starting = append(out.starting, record.settled)
	}
	return out
}

// settle is the one decision point for an end the camera reported, applied
// once no EndSession is in flight. Mirrors matterjs-server
// packages/ws-controller/src/camera/sessionRegistry.ts:462-468 at 0bd06f2.
func (r *sessionRegistry) settle(life *lifecycle) {
	session, ended := life.session, life.cameraEnded
	if life.over || life.endsInFlight > 0 || session == nil || ended == nil {
		return
	}
	r.close(life, *ended)
	if ended.cause != endPeer {
		vanished, cause := *session, ended.cause
		r.queue = append(r.queue, func() { r.vanished(vanished, cause) })
	}
}

// close is the one place a tracked session ends and is announced. Mirrors
// matterjs-server packages/ws-controller/src/camera/sessionRegistry.ts:470-478
// at 0bd06f2.
func (r *sessionRegistry) close(life *lifecycle, end sessionEnd) {
	session := life.session
	if life.over || session == nil {
		return
	}
	life.over = true
	k := sessionKey{session.nodeID, session.endpointID, session.webRTCSessionID}
	if r.tracked[k] == life {
		r.deleteTracked(k)
	}
	r.announceEnd(k, end, session)
}

// announceEnd queues the announcement of an end; without an owner the
// route tells every camera-aware connection. Mirrors matterjs-server
// packages/ws-controller/src/camera/sessionRegistry.ts:480-496 at 0bd06f2.
func (r *sessionRegistry) announceEnd(k sessionKey, end sessionEnd, owner *managedSession) {
	ended := cameraSessionEnded{sessionEnd: end, nodeID: k.nodeID, endpointID: k.endpointID, webRTCSessionID: k.sessionID}
	if owner != nil {
		ended.ownerID, ended.clientSessionKey = owner.connectionID, owner.clientSessionKey
	}
	r.queue = append(r.queue, func() { r.announce(ended) })
}
