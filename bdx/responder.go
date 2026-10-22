// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bdx

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/clock"
)

// Peer is the peer of a BDX exchange: its node on one of this node's
// fabrics (matter.js PeerAddress), and the secure session the exchange
// runs on.
type Peer struct {
	FabricIndex uint8
	NodeID      uint64
	// SessionID is this node's id of the secure session.
	SessionID uint16
}

// key is the peer address without the session (PeerAddress.is compares
// fabric index and node id).
func (p Peer) key() peerKey { return peerKey{fabricIndex: p.FabricIndex, nodeID: p.NodeID} }

type peerKey struct {
	fabricIndex uint8
	nodeID      uint64
}

// Storage is the files of one storage scope a [Responder] serves: matter.js
// ScopedStorage (ScopedStorage.ts:16-34) with the blob driver behind it.
type Storage interface {
	// Open returns the named file for an outbound transfer; an error
	// wrapping fs.ErrNotExist is answered FileDesignatorUnknown.
	Open(ctx context.Context, name string) (Blob, error)
}

// SinkStorage is the optional [Storage] capability a scope needs to accept
// files a peer sends (a SendInit).
type SinkStorage interface {
	Create(ctx context.Context, name string) (Sink, error)
}

// ScopeConfig is how a [Responder] serves a peer's transfers in one scope:
// matter.js BdxProtocol.Config (BdxProtocol.ts:214-225) — the session
// configuration and the messenger's message timeout.
type ScopeConfig struct {
	// PreferredDriverModes are the modes accepted, in order of preference;
	// empty is matter.js's default [SenderDrive, ReceiverDrive].
	PreferredDriverModes []DriverMode
	// MaxBlockSize, when set, lowers the accepted block size.
	MaxBlockSize uint16
	// MessageTimeout is the messenger's wait for each message of the peer;
	// zero is [TransferIdleTimeout].
	MessageTimeout time.Duration
}

// Responder serves the BDX exchanges peers open — the responder side of
// the protocol, matter.js BdxProtocol (packages/protocol/src/bdx/
// BdxProtocol.ts), registered with the exchange layer for the BDX protocol
// id: a peer is enabled for a storage scope, and a *Init whose file
// designator names that scope (`<scope>/<file>`) starts a session on its
// exchange. It is safe for concurrent use.
type Responder struct {
	logger *slog.Logger

	// mu guards clock, scopes, sessions and started.
	mu       sync.Mutex
	scopes   map[scopeKey]scopeEntry
	sessions map[*ServerSession]struct{}
	started  []*startedListener
	clock    clock.Clock // a session's activity stamps; nil is clock.System
}

type scopeKey struct {
	peer  peerKey
	scope string
}

type scopeEntry struct {
	storage Storage
	cfg     ScopeConfig
}

type startedListener struct {
	fn func(*ServerSession, string)
}

// NewResponder returns a responder with no scope enabled; nil logger is
// slog.Default().
func NewResponder(logger *slog.Logger) *Responder {
	if logger == nil {
		logger = slog.Default()
	}
	return &Responder{logger: logger, scopes: map[scopeKey]scopeEntry{}, sessions: map[*ServerSession]struct{}{}}
}

// SetClock sets the clock that stamps each session's last activity; nil
// is [clock.System].
func (r *Responder) SetClock(c clock.Clock) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.clock = c
}

func (r *Responder) now() time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return clock.Or(r.clock).Now()
}

// ErrActiveSession is a scope disabled without force while a transfer of
// it runs (BdxProtocol.ts:101-103).
var ErrActiveSession = errors.New("bdx: active BDX sessions use this scope")

// EnablePeerForScope lets peer open transfers of scope's files, served from
// storage with cfg. It reports false — and changes nothing — when the peer
// is already enabled for the scope with another storage or configuration
// while one of its transfers runs: only a transfer under way is entitled
// to the configuration it started with. Mirrors BdxProtocol.enablePeerForScope
// (BdxProtocol.ts:57-82).
func (r *Responder) EnablePeerForScope(peer Peer, scope string, storage Storage, cfg ScopeConfig) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := scopeKey{peer: peer.key(), scope: scope}
	if existing, ok := r.scopes[key]; ok {
		if !sameStorage(existing.storage, storage) || !reflect.DeepEqual(existing.cfg, cfg) {
			if r.activeForLocked(key) != nil {
				r.logger.Warn("bdx.enable_refused", slog.String("scope", scope), slog.Uint64("peer", peer.NodeID),
					slog.String("reason", "different storage or config while a transfer is active"))
				return false
			}
		}
	}
	r.scopes[key] = scopeEntry{storage: storage, cfg: cfg}
	return true
}

// sameStorage is matter.js's identity test of two storages
// (`existingStorage !== storage`, BdxProtocol.ts:62): the same comparable
// value, or the same map, slice, function or pointer behind an
// uncomparable one.
func sameStorage(a, b Storage) bool {
	ta, tb := reflect.TypeOf(a), reflect.TypeOf(b)
	if ta != tb {
		return false
	}
	if ta == nil {
		return true
	}
	va, vb := reflect.ValueOf(a), reflect.ValueOf(b)
	switch va.Kind() { //nolint:exhaustive // the reference kinds compare by identity, the rest by value
	case reflect.Map, reflect.Slice, reflect.Func, reflect.Pointer, reflect.Chan:
		return va.Pointer() == vb.Pointer()
	}
	if ta.Comparable() {
		return va.Equal(vb)
	}
	return false
}

// DisablePeerForScope withdraws peer's access to scope. A transfer of it
// that still runs is closed when force is set, else the call fails with
// [ErrActiveSession] and changes nothing. Mirrors
// BdxProtocol.disablePeerForScope (BdxProtocol.ts:93-108).
func (r *Responder) DisablePeerForScope(peer Peer, scope string, force bool) error {
	r.mu.Lock()
	key := scopeKey{peer: peer.key(), scope: scope}
	active := r.activeForLocked(key)
	if active != nil && !force {
		r.mu.Unlock()
		return fmt.Errorf("%w: peer %d (scope: %s)", ErrActiveSession, peer.NodeID, scope)
	}
	delete(r.scopes, key)
	r.mu.Unlock()
	if active != nil {
		active.Close(errors.New("bdx: BDX protocol scope disabled"))
	}
	return nil
}

// SessionFor returns the running transfer of peer in scope, nil when none
// runs (BdxProtocol.ts:204-211).
func (r *Responder) SessionFor(peer Peer, scope string) *ServerSession {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.activeForLocked(scopeKey{peer: peer.key(), scope: scope})
}

func (r *Responder) activeForLocked(key scopeKey) *ServerSession {
	for s := range r.sessions {
		if s.scope == key.scope && s.peer.key() == key.peer {
			return s
		}
	}
	return nil
}

// OnSessionStarted registers fn for every session that starts, before it
// negotiates, and returns the function that unregisters it
// (BdxProtocol.ts:32, sessionStarted). fn runs on the session's goroutine
// and may register the session's own observers.
func (r *Responder) OnSessionStarted(fn func(s *ServerSession, scope string)) (off func()) {
	l := &startedListener{fn: fn}
	r.mu.Lock()
	r.started = append(r.started, l)
	r.mu.Unlock()
	return func() {
		r.mu.Lock()
		r.started = slices.DeleteFunc(r.started, func(x *startedListener) bool { return x == l })
		r.mu.Unlock()
	}
}

// Close closes every running transfer (BdxProtocol.ts:196-202).
func (r *Responder) Close() {
	r.mu.Lock()
	sessions := make([]*ServerSession, 0, len(r.sessions))
	for s := range r.sessions {
		sessions = append(sessions, s)
	}
	r.mu.Unlock()
	for _, s := range sessions {
		s.Close(errors.New("bdx: BDX protocol handler closed"))
	}
}

// HandleExchange serves the exchange peer opened with first, which it has
// already received: a SendInit or ReceiveInit whose designator names an
// enabled scope runs a session to its end; anything else is answered with
// a failure StatusReport. The exchange is closed when it returns. Mirrors
// BdxProtocol.onNewExchange (BdxProtocol.ts:110-173) and #registerSession
// (:176-194).
func (r *Responder) HandleExchange(ctx context.Context, peer Peer, first Message, ex Exchange) {
	defer func() { _ = ex.Close() }()
	t := MessageType(first.Opcode)
	if first.ProtocolID != ProtocolID || (t != MessageSendInit && t != MessageReceiveInit) {
		// :167-171.
		r.logger.Warn("bdx.unexpected_new_exchange", slog.String("type", t.String()))
		_ = NewMessenger(ex, 0).SendError(ctx, StatusUnexpectedMessage)
		return
	}
	init, err := DecodeInit(first.Payload)
	if err != nil {
		r.refuse(ctx, ex, nil, err)
		return
	}
	fd := FileDesignator(init.FileDesignator)
	// :129: the designator's first two path elements are the scope and the
	// file.
	parts := strings.Split(fd.Text(), "/")
	scope := parts[0]
	r.mu.Lock()
	entry, ok := r.scopes[scopeKey{peer: peer.key(), scope: scope}]
	r.mu.Unlock()
	if !ok || len(parts) < 2 {
		r.refuse(ctx, ex, nil, newError(StatusFileDesignatorUnknown, "No storage context found for BDX file designator %q", fd.Text()))
		return
	}
	file := parts[1]
	m := NewMessenger(ex, entry.cfg.MessageTimeout)
	sessCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	s := &ServerSession{peer: peer, scope: scope, file: file, initType: t, init: init, cancel: cancel, lastActivity: r.now(), clock: clock.Or(r.clock)}
	opts := ServeOptions{
		InitType: t, Init: init,
		// PersistedFileDesignator with a ScopedStorage names the file
		// `<scope>/<file>` (PersistedFileDesignator.ts:35-37), which the
		// Init must name exactly (bdx-session-initiator.ts:59-65).
		TransferFileDesignator: FileDesignator(scope + "/" + file),
		PreferredDriverModes:   entry.cfg.PreferredDriverModes,
		MaxBlockSize:           entry.cfg.MaxBlockSize,
		Open: func(ctx context.Context) (Blob, error) {
			return entry.storage.Open(ctx, file)
		},
		Progress:   s.progress,
		Finished:   s.finished,
		Cancelled:  s.cancelled,
		Negotiated: s.negotiated,
	}
	if t == MessageSendInit {
		creator, ok := entry.storage.(SinkStorage)
		if !ok {
			r.refuse(ctx, ex, m, newError(StatusFileDesignatorUnknown, "No storage context found for BDX file designator %q", fd.Text()))
			return
		}
		sink, err := creator.Create(ctx, file)
		if err != nil {
			r.refuse(ctx, ex, m, err)
			return
		}
		opts.Sink = sink
	}

	r.register(s)
	_, err = Serve(sessCtx, m, opts)
	if err != nil {
		r.logger.Warn("bdx.transfer_failed", slog.String("scope", scope), slog.Uint64("peer", peer.NodeID), slog.String("err", err.Error()))
	}
	r.unregister(s)
}

// refuse is onNewExchange's catch (BdxProtocol.ts:159-164): a BDX error is
// reported to the peer with its code; any other failure is logged and the
// exchange closed without a report, as matter.js lets it reach the
// exchange manager's error handling (ExchangeManager.ts:438-439).
func (r *Responder) refuse(ctx context.Context, ex Exchange, m *Messenger, err error) {
	if be, ok := AsError(err); ok {
		if m == nil {
			m = NewMessenger(ex, 0)
		}
		_ = m.SendError(ctx, be.Code)
	}
	r.logger.Warn("bdx.session_failed", slog.String("err", err.Error()))
}

func (r *Responder) register(s *ServerSession) {
	r.mu.Lock()
	r.sessions[s] = struct{}{}
	listeners := slices.Clone(r.started)
	r.mu.Unlock()
	for _, l := range listeners {
		l.fn(s, s.scope)
	}
}

func (r *Responder) unregister(s *ServerSession) {
	r.mu.Lock()
	delete(r.sessions, s)
	r.mu.Unlock()
	s.closed()
}

// ServerSession is one transfer a peer opened on a [Responder] — matter.js
// BdxSession as BdxProtocol announces it (BdxSession.ts): its peer, its
// scope and file, and the observers a listener of
// [Responder.OnSessionStarted] registers.
type ServerSession struct {
	peer     Peer
	scope    string
	file     string
	initType MessageType
	init     Init
	cancel   context.CancelCauseFunc

	// mu guards the transfer's progress and its observers: every field
	// below it. The observer lists are copied under it and called after it
	// is released.
	mu           sync.Mutex
	transferred  uint64
	total        *uint64
	result       *Result
	lastActivity time.Time
	clock        clock.Clock
	isClosed     bool
	onProgress   []func(uint64, *uint64)
	onFinished   []func(uint64)
	onCancelled  []func()
	onClosed     []func()
}

// Peer is the transfer's peer and the secure session it runs on
// (BdxSession.peerAddress, BdxSession.session).
func (s *ServerSession) Peer() Peer { return s.peer }

// Scope and File are the two parts of the designator the peer named.
func (s *ServerSession) Scope() string { return s.scope }

// File is the file in the scope.
func (s *ServerSession) File() string { return s.file }

// InitType is the message that opened the transfer.
func (s *ServerSession) InitType() MessageType { return s.initType }

// Init returns a copy of the *Init that opened the transfer
// (BdxSession.initMessage, BdxSession.ts:118-133).
func (s *ServerSession) Init() Init {
	in := s.init
	in.FileDesignator = slices.Clone(in.FileDesignator)
	in.MetaData = slices.Clone(in.MetaData)
	return in
}

// Negotiated returns what the transfer settled on, false until the *Accept
// went out (BdxSession.transferParameters, BdxSession.ts:141-143).
func (s *ServerSession) Negotiated() (Result, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.result == nil {
		return Result{}, false
	}
	return *s.result, true
}

// Transferred is the bytes moved so far (BdxSession.transferredBytes).
func (s *ServerSession) Transferred() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.transferred
}

// LastActivity is when the transfer last reported progress, the session's
// start before it did.
func (s *ServerSession) LastActivity() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastActivity
}

// OnProgress registers fn for progressInfo (BdxSession.ts:94-96).
func (s *ServerSession) OnProgress(fn func(transferred uint64, total *uint64)) {
	s.mu.Lock()
	s.onProgress = append(s.onProgress, fn)
	s.mu.Unlock()
}

// OnFinished registers fn for progressFinished (BdxSession.ts:145-147).
func (s *ServerSession) OnFinished(fn func(total uint64)) {
	s.mu.Lock()
	s.onFinished = append(s.onFinished, fn)
	s.mu.Unlock()
}

// OnCancelled registers fn for progressCancelled (BdxSession.ts:149-151).
func (s *ServerSession) OnCancelled(fn func()) {
	s.mu.Lock()
	s.onCancelled = append(s.onCancelled, fn)
	s.mu.Unlock()
}

// OnClosed registers fn for the session's end, success or not
// (BdxSession.closed, BdxSession.ts:235-237).
func (s *ServerSession) OnClosed(fn func()) {
	s.mu.Lock()
	s.onClosed = append(s.onClosed, fn)
	s.mu.Unlock()
}

// Close ends the transfer with cause (BdxSession.close, BdxSession.ts:
// 249-256): its context is cancelled, the flow stops at its next wait.
func (s *ServerSession) Close(cause error) {
	s.cancel(cause)
}

func (s *ServerSession) negotiated(res Result) {
	s.mu.Lock()
	s.result = &res
	s.lastActivity = s.clock.Now()
	s.mu.Unlock()
}

func (s *ServerSession) progress(transferred uint64, total *uint64) {
	s.mu.Lock()
	s.transferred = transferred
	s.total = total
	s.lastActivity = s.clock.Now()
	fns := slices.Clone(s.onProgress)
	s.mu.Unlock()
	for _, fn := range fns {
		fn(transferred, total)
	}
}

func (s *ServerSession) finished(total uint64) {
	s.mu.Lock()
	fns := slices.Clone(s.onFinished)
	s.mu.Unlock()
	for _, fn := range fns {
		fn(total)
	}
}

func (s *ServerSession) cancelled() {
	s.mu.Lock()
	fns := slices.Clone(s.onCancelled)
	s.mu.Unlock()
	for _, fn := range fns {
		fn()
	}
}

func (s *ServerSession) closed() {
	s.mu.Lock()
	if s.isClosed {
		s.mu.Unlock()
		return
	}
	s.isClosed = true
	fns := slices.Clone(s.onClosed)
	s.mu.Unlock()
	for _, fn := range fns {
		fn()
	}
}
