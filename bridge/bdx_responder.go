// SPDX-License-Identifier: MIT
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
			select {
			case ex.in <- msg:
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
		createdAt: time.Now(), responder: true, in: make(chan bdx.Message, 16), done: make(chan struct{}),
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
	return true
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
