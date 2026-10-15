// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/bdx"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// BDXResponder serves the BDX exchanges a peer opens on a secure session —
// matter.js's BdxProtocol, the protocol handler ExchangeManager hands the
// first message of a new BDX exchange (BdxProtocol.ts:110-173,
// onNewExchange; ExchangeManager.ts:407-440). *bdx.Responder implements it.
//
// HandleExchange runs on a goroutine of its own, one per exchange, with the
// first message already taken off the exchange; the exchange's later
// messages arrive through ex. It closes ex when it is done.
type BDXResponder interface {
	HandleExchange(ctx context.Context, peer bdx.Peer, first bdx.Message, ex bdx.Exchange)
}

// bdxRouting is the attached responder and the exchanges peers opened on
// it, keyed by session and exchange id.
type bdxRouting struct {
	mu        sync.RWMutex
	responder BDXResponder
	exchanges sync.Map // secureExchangeKey → *initiatedSecureExchange (responder role)
}

// AttachBDXResponder installs the responder for the BDX exchanges peers
// open — an OTA requestor's ReceiveInit, say, for the provider's image.
// Without one the bridge refuses them as it refuses any protocol it has no
// handler for: the message is acknowledged when it asks for it and dropped
// with [ErrUnknownProtocol], matter.js ExchangeManager.ts:441-455 for a
// protocol without a handler (the acknowledgement for a message that
// requires one, MessageFlowError "Unsupported protocol" otherwise). nil
// detaches it. Attach before or after Start.
func (b *Bridge) AttachBDXResponder(r BDXResponder) {
	b.bdx.mu.Lock()
	b.bdx.responder = r
	b.bdx.mu.Unlock()
}

func (b *Bridge) bdxResponder() BDXResponder {
	b.bdx.mu.RLock()
	defer b.bdx.mu.RUnlock()
	return b.bdx.responder
}

// deliverRespondedBDX takes a message the peer sends as the initiator on a
// secure session: one of a BDX exchange it opened earlier goes to that
// exchange, and the first message of a new BDX exchange opens one and
// starts the responder on it. It returns false — leaving the message to the
// ordinary dispatch — for every other message, and for a new BDX exchange
// while no responder is attached. The responder runs under ctx, the
// bridge's handler context the datagram is dispatched on.
func (b *Bridge) deliverRespondedBDX(ctx context.Context, hdr *message.Header, proto message.ProtocolHeader, payload []byte) bool {
	if hdr.SessionID == 0 || !proto.Initiator {
		return false
	}
	if proto.ProtocolID == mrp.SecureChannelProtocolID && proto.Opcode == mrp.StandaloneAckOpcode {
		return false
	}
	key := secureExchangeKey{sessionID: hdr.SessionID, exchangeID: proto.ExchangeID}
	msg := bdx.Message{ProtocolID: proto.ProtocolID, Opcode: proto.Opcode, Payload: append([]byte(nil), payload...)}
	if raw, ok := b.bdx.exchanges.Load(key); ok {
		if ex, ok := raw.(*initiatedSecureExchange); ok {
			k := ex.turn.deliver()
			select {
			case ex.in <- msg:
				ex.turn.await(k, ex.done)
			case <-ex.done:
			default:
				b.logger.Debug("matter.exchange.bdx.overflow",
					slog.Int("session_id", int(hdr.SessionID)), slog.Int("exchange_id", int(proto.ExchangeID)))
			}
			return true
		}
	}
	if proto.ProtocolID != bdx.ProtocolID || (proto.HasVendorID && proto.VendorID != 0) {
		return false
	}
	responder := b.bdxResponder()
	if responder == nil {
		return false
	}
	peer, ok := b.bdxPeer(hdr.SessionID)
	if !ok {
		return false
	}
	ex := &initiatedSecureExchange{
		b: b, sessionID: hdr.SessionID, addr: b.currentPeerAddr(hdr.SessionID, nil), exchangeID: proto.ExchangeID,
		createdAt: time.Now(), responder: true, turn: newResponderTurn(), in: make(chan bdx.Message, 16), done: make(chan struct{}),
	}
	if _, loaded := b.bdx.exchanges.LoadOrStore(key, ex); loaded {
		// A concurrent first message of the same exchange won the race;
		// this one is a duplicate the replay window did not see yet.
		return true
	}
	go func() {
		defer func() { _ = ex.Close() }()
		responder.HandleExchange(ctx, peer, msg, ex)
	}()
	// The *Init is message 0: the responder has answered it once it waits
	// for the next.
	ex.turn.await(0, ex.done)
	return true
}

// responderTurnBound is the longest a session's next message waits for the
// BDX responder to finish with the previous one ([responderTurn]).
const responderTurnBound = 2 * time.Second

// responderTurn keeps a session's messages in order across the responder's
// goroutine. matter.js handles a BDX message on its event loop: the flow
// resumes on it and runs to its next await before the session's next
// message is looked at (ExchangeManager #receiveMessage, the flow's
// `await this.messenger.read…`). So an OTA requestor's BlockAckEof
// completes the transfer — WaitForApply — before its ApplyUpdateRequest,
// sent right after on the same session, is handled
// (OtaSoftwareUpdateProviderServer.ts:332-339, :418-424). Here the
// responder runs on a goroutine of its own; the dispatch of a message to it
// waits, bounded, until the responder has taken it and is waiting for the
// next one or has finished.
type responderTurn struct {
	mu        sync.Mutex
	delivered int64
	taken     int64
	// idleAt is how many messages the responder had taken when it last
	// waited for one; -1 before it first waited.
	idleAt  int64
	changed chan struct{}
}

func newResponderTurn() *responderTurn {
	return &responderTurn{idleAt: -1, changed: make(chan struct{})}
}

// waiting records that the responder waits for its next message, having
// taken all it took.
func (t *responderTurn) waiting() {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.idleAt = t.taken
	close(t.changed)
	t.changed = make(chan struct{})
	t.mu.Unlock()
}

// took records that the responder took a message.
func (t *responderTurn) took() {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.taken++
	t.mu.Unlock()
}

// deliver numbers the next message handed to the responder; the first,
// handed over with the exchange, is message 0.
func (t *responderTurn) deliver() int64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.delivered++
	return t.delivered
}

// await waits until the responder took message k and waits again, the
// exchange ended, or [responderTurnBound] passed.
func (t *responderTurn) await(k int64, done <-chan struct{}) {
	timer := time.NewTimer(responderTurnBound)
	defer timer.Stop()
	for {
		t.mu.Lock()
		reached, ch := t.idleAt >= k, t.changed
		t.mu.Unlock()
		if reached {
			return
		}
		select {
		case <-ch:
		case <-done:
			return
		case <-timer.C:
			return
		}
	}
}

// bdxPeer resolves the peer of a secure session: its node id and the
// fabric the session runs on (matter.js exchange.session.peerAddress,
// BdxProtocol.ts:131).
func (b *Bridge) bdxPeer(sessionID uint16) (bdx.Peer, bool) {
	b.mu.RLock()
	sessions := b.sessions
	b.mu.RUnlock()
	if sessions == nil {
		return bdx.Peer{}, false
	}
	sess, ok := sessions.Lookup(sessionID)
	if !ok {
		return bdx.Peer{}, false
	}
	peer := bdx.Peer{NodeID: sess.PeerNodeID(), SessionID: sessionID}
	if resolver, ok := sessions.(SessionFabricResolver); ok {
		peer.FabricIndex, _ = resolver.FabricFor(sessionID)
	}
	return peer, true
}

// OpenSessionExchange implements core.BDXExchangeOpener: a new exchange
// this node initiates on the secure session sessionID — the session a
// command arrived on, for the BDX transfer chip's DiagnosticLogs server
// opens there (connectedhomeip BDXDiagnosticLogsProvider.cpp:67-70 at the
// harness pin, exchangeMgr->NewContext(sessionHandle)). Called from that
// command's handler, it hands the session's turn on (session_order.go):
// the handler goes on to wait for the peer's SendAccept on this session.
func (b *Bridge) OpenSessionExchange(ctx context.Context, sessionID uint16) (bdx.Exchange, error) {
	addr := b.currentPeerAddr(sessionID, nil)
	if addr == nil {
		return nil, ErrUnsolicitedSessionMissing
	}
	ex, err := b.openInitiatedExchange(peerLink{sessionID: sessionID, addr: addr})
	if err != nil {
		return nil, err
	}
	leaveSessionOrder(ctx)
	return ex, nil
}
