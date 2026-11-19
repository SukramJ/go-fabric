// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"bytes"
	"context"
	"errors"
	"runtime"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/bdx"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/secure/operational"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// endingResponder takes an exchange and reports what its next Receive
// returned: a message, or the error the exchange ended with.
type endingResponder struct {
	got   chan bdx.Message
	ended chan error
}

func newEndingResponder() *endingResponder {
	return &endingResponder{got: make(chan bdx.Message, 4), ended: make(chan error, 4)}
}

func (r *endingResponder) HandleExchange(ctx context.Context, _ bdx.Peer, _ bdx.Message, ex bdx.Exchange) {
	msg, err := ex.Receive(ctx)
	if err != nil {
		r.ended <- err
		return
	}
	r.got <- msg
}

// exchangeSessionRig is a device with a CASE session to a fake peer, a
// BDX exchange the peer opened on it and one the device opened.
type exchangeSessionRig struct {
	dev       *resumeDevice
	peer      *fakeController
	responder *endingResponder
	sid       uint16
	peerXID   uint16
	nodeEx    bdx.Exchange
}

func newExchangeSessionRig(t *testing.T) *exchangeSessionRig {
	t.Helper()
	fab := newResumeFabric(t)
	peer := newFakeController(t, fab)
	responder := newEndingResponder()
	dev := startResumeDevice(t, newMemSubscriptionStore(), func(br *Bridge, sessions *operational.Manager) {
		br.AttachCaseInitiatorProvider(resumeInitiatorProvider(fab, sessions))
		br.AttachOperationalResolver(staticResolver{addr: peer.addr})
		br.AttachBDXResponder(responder)
	})
	peer.setBridge(dev.addr(t))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s, err := dev.br.ConnectPeer(ctx, resumeFabricIndex, resumeCtrlNode)
	if err != nil {
		t.Fatalf("CASE to the peer: %v", err)
	}
	r := &exchangeSessionRig{dev: dev, peer: peer, responder: responder, sid: s.link.sessionID, peerXID: 0x4242}
	sendBDX(peer, r.peerXID, bdx.MessageReceiveInit, []byte{0x00})
	key := secureExchangeKey{sessionID: r.sid, exchangeID: r.peerXID}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, ok := dev.br.bdx.exchanges.Load(key); ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the peer's BDX exchange was not opened")
		}
		runtime.Gosched()
	}
	r.nodeEx, err = dev.br.OpenSessionExchange(context.Background(), r.sid)
	if err != nil {
		t.Fatalf("OpenSessionExchange: %v", err)
	}
	return r
}

// reuseSessionID installs a new session under the closed session's id, as
// the id allocator hands it out again once it walked the id space.
func (r *exchangeSessionRig) reuseSessionID(t *testing.T) {
	t.Helper()
	secret := make([]byte, 32)
	for i := range secret {
		secret[i] = byte(i + 1)
	}
	if _, err := r.dev.sessions.OpenFromPaseWithID(r.sid, 0, 0x9999, 0x0777, secret); err != nil { //nolint:staticcheck // SA1019: the session reuses a chosen id, which no stake install takes (decision 159)
		t.Fatalf("a new session under id %d: %v", r.sid, err)
	}
}

// readOnPeerExchange is an IM ReadRequest of whoever holds the session id
// now, on the exchange id the peer's BDX exchange had.
func (r *exchangeSessionRig) readOnPeerExchange() bool {
	return r.dev.br.deliverRespondedBDX(context.Background(),
		&message.Header{SessionID: r.sid, MessageCounter: 99},
		message.ProtocolHeader{Initiator: true, ExchangeID: r.peerXID, ProtocolID: im.InteractionModelProtocolID, Opcode: im.OpcodeReadRequest},
		[]byte{0x15, 0x18})
}

func (r *exchangeSessionRig) responderEnded(t *testing.T) {
	t.Helper()
	select {
	case err := <-r.responder.ended:
		if !errors.Is(err, ErrExchangeClosed) {
			t.Fatalf("the responder's Receive returned %v, want ErrExchangeClosed", err)
		}
	case msg := <-r.responder.got:
		t.Fatalf("the old exchange's responder received protocol %#x opcode %#x", msg.ProtocolID, msg.Opcode)
	}
}

// TestExchangesEndWithTheirSession: an exchange a peer opened and one this
// node opened end when their session closes — the session's close reaches
// the bridge through SessionClosed — and a later session that is given the
// same id inherits neither: its message on the exchange id goes to the
// ordinary dispatch, and the old exchange sends nothing on it (review II,
// R2-P6-sec-0302).
func TestExchangesEndWithTheirSession(t *testing.T) {
	r := newExchangeSessionRig(t)
	_ = r.dev.sessions.Close(r.sid)
	r.dev.br.SessionClosed(r.sid)

	if _, held := r.dev.br.bdx.exchanges.Load(secureExchangeKey{sessionID: r.sid, exchangeID: r.peerXID}); held {
		t.Fatal("the peer's BDX exchange outlived its session")
	}
	held := 0
	r.dev.br.resumption.initiated.Range(func(k, _ any) bool {
		if key, ok := k.(secureExchangeKey); ok && key.sessionID == r.sid {
			held++
		}
		return true
	})
	if held != 0 {
		t.Fatalf("%d exchanges this node opened outlived their session", held)
	}
	r.responderEnded(t)

	r.reuseSessionID(t)
	if r.readOnPeerExchange() {
		t.Fatal("a message of the session that took the id over went to the closed session's exchange")
	}
	if err := r.nodeEx.Send(context.Background(), bdx.ProtocolID, uint8(bdx.MessageSendInit), []byte{0x01}); !errors.Is(err, ErrExchangeClosed) {
		t.Fatalf("Send on an exchange of a closed session: %v, want ErrExchangeClosed", err)
	}
}

// TestExchangesRefuseASessionTheyWereNotOpenedOn: a host that does not call
// SessionClosed from its registry's close hook still keeps an exchange from
// passing to the session that takes its id over — the exchange is bound to
// the session object it was opened on (review II, R2-P6-sec-0302).
func TestExchangesRefuseASessionTheyWereNotOpenedOn(t *testing.T) {
	r := newExchangeSessionRig(t)
	_ = r.dev.sessions.Close(r.sid) // the registry only: no SessionClosed
	r.reuseSessionID(t)

	if r.readOnPeerExchange() {
		t.Fatal("a message of the session that took the id over went to the closed session's exchange")
	}
	r.responderEnded(t)
	if err := r.nodeEx.Send(context.Background(), bdx.ProtocolID, uint8(bdx.MessageSendInit), []byte{0x01}); !errors.Is(err, ErrExchangeClosed) {
		t.Fatalf("Send on an exchange whose session id another session holds: %v, want ErrExchangeClosed", err)
	}
	if _, held := r.dev.br.resumption.initiated.Load(secureExchangeKey{sessionID: r.sid, exchangeID: exchangeIDOf(t, r.nodeEx)}); held {
		t.Fatal("the refused exchange stayed in the table")
	}
}

func exchangeIDOf(t *testing.T, ex bdx.Exchange) uint16 {
	t.Helper()
	e, ok := ex.(*initiatedSecureExchange)
	if !ok {
		t.Fatalf("exchange of type %T", ex)
	}
	return e.exchangeID
}

// TestOpenedExchangeSkipsAnIDALiveExchangeHolds: the id of an exchange this
// node opens on a session skips one an open exchange of that session holds,
// so a counter that came round never hands one exchange's id to another,
// and the older one's Close leaves the newer one in place (review II,
// R2-P2-ctr-03).
func TestOpenedExchangeSkipsAnIDALiveExchangeHolds(t *testing.T) {
	r := newExchangeSessionRig(t)
	held := exchangeIDOf(t, r.nodeEx)
	r.dev.br.outboundExchangeID.Store(uint32(held - 1))
	next, err := r.dev.br.OpenSessionExchange(context.Background(), r.sid)
	if err != nil {
		t.Fatal(err)
	}
	if got := exchangeIDOf(t, next); got == held {
		t.Fatalf("the new exchange took id %#x, which an open exchange holds", got)
	}
	_ = r.nodeEx.Close()
	if _, ok := r.dev.br.resumption.initiated.Load(secureExchangeKey{sessionID: r.sid, exchangeID: exchangeIDOf(t, next)}); !ok {
		t.Fatal("the older exchange's Close removed the newer one")
	}
}

// TestExchangeHooksTakeOnlyTheirDirection: each hook that hands a message
// to an exchange this node opened or answers takes only a message of that
// exchange's direction — a peer-initiated message whose exchange id equals
// a node-opened exchange's is a new exchange of the peer, and a message
// sent as the responder never reaches an exchange the peer opened
// (review II, R2-P6-test-0801).
func TestExchangeHooksTakeOnlyTheirDirection(t *testing.T) {
	t.Parallel()
	b, err := New(wbEmptySnapshotter, mdns.NewNoop(), Config{Listen: "127.0.0.1:0"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	const sid, xid = uint16(0x0101), uint16(0x0202)
	newEx := func(responder bool) *initiatedSecureExchange {
		return &initiatedSecureExchange{b: b, sessionID: sid, exchangeID: xid, responder: responder, turn: newResponderTurn(nil), in: make(chan bdx.Message, 4), done: make(chan struct{})}
	}
	opened, answered := newEx(false), newEx(true)
	b.resumption.initiated.Store(secureExchangeKey{sessionID: sid, exchangeID: xid}, opened)
	b.bdx.exchanges.Store(secureExchangeKey{sessionID: sid, exchangeID: xid}, answered)
	handshake := &initiatedExchange{ch: make(chan initiatedMessage, 4)}
	b.resumption.initiated.Store(HandshakeKey{InitiatorNodeID: 0x77, ExchangeID: xid}, handshake)

	read := message.ProtocolHeader{ExchangeID: xid, ProtocolID: im.InteractionModelProtocolID, Opcode: im.OpcodeReadRequest}
	asInitiator, asResponder := read, read
	asInitiator.Initiator = true
	if b.deliverInitiatedSecure(&message.Header{SessionID: sid}, asInitiator, nil) || len(opened.in) != 0 {
		t.Error("a peer-initiated message reached the exchange this node opened")
	}
	if b.deliverRespondedBDX(context.Background(), &message.Header{SessionID: sid}, asResponder, nil) || len(answered.in) != 0 {
		t.Error("a message sent as the responder reached the exchange the peer opened")
	}
	if b.deliverRespondedBDX(context.Background(), &message.Header{SessionID: 0}, asInitiator, nil) || len(answered.in) != 0 {
		t.Error("an unsecured message reached a secure session's exchange")
	}
	unsecured := &message.Header{SessionID: 0, DestSize: message.DestNodeID, DestNodeID: 0x77}
	if b.deliverInitiatedSecureChannel(unsecured, asInitiator, nil) || len(handshake.ch) != 0 {
		t.Error("a peer-initiated unsecured message reached the CASE handshake this node opened")
	}
	secured := &message.Header{SessionID: sid, DestSize: message.DestNodeID, DestNodeID: 0x77}
	if b.deliverInitiatedSecureChannel(secured, asResponder, nil) || len(handshake.ch) != 0 {
		t.Error("a secure-session message reached the unsecured CASE handshake this node opened")
	}
}

// TestSessionExchangesAreBounded: a session holds at most
// maxExchangesPerSession open exchanges in the bridge's tables; the one
// least recently active ends when a newer one would exceed it (review II,
// R2-P6-bound-0307).
func TestSessionExchangesAreBounded(t *testing.T) {
	r := newExchangeSessionRig(t)
	for range maxExchangesPerSession {
		if _, err := r.dev.br.OpenSessionExchange(context.Background(), r.sid); err != nil {
			t.Fatal(err)
		}
	}
	open := 0
	count := func(k, v any) bool {
		if key, ok := k.(secureExchangeKey); ok && key.sessionID == r.sid && !v.(*initiatedSecureExchange).ended() {
			open++
		}
		return true
	}
	r.dev.br.bdx.exchanges.Range(count)
	r.dev.br.resumption.initiated.Range(count)
	if open != maxExchangesPerSession {
		t.Fatalf("the session holds %d open exchanges, want %d", open, maxExchangesPerSession)
	}
	// The peer's BDX exchange was the least recently active.
	r.responderEnded(t)
	if !r.nodeEx.(*initiatedSecureExchange).ended() {
		t.Fatal("the second-oldest exchange ended too; one place was needed")
	}
}

// TestRespondedExchangeEndsWhenItsMessageIsGivenUp: a reply on an exchange
// the peer opened whose retransmissions ran out ends that exchange — its
// responder's Receive returns ErrExchangeClosed — as one on an exchange this
// node opened does (review II, R2-P6-bound-0307).
func TestRespondedExchangeEndsWhenItsMessageIsGivenUp(t *testing.T) {
	r := newExchangeSessionRig(t)
	now := time.Now()
	tr := newOutboundReliableTracker(nil, nil)
	tr.Track(7, r.sid, r.peerXID, []byte{0x00}, r.peer.addr, now)
	tr.mu.Lock()
	for _, e := range tr.pending {
		e.retries = mrp.MaxRetransmissions
	}
	tr.mu.Unlock()
	r.dev.br.tickOutboundReliable(tr, now.Add(time.Hour))
	r.responderEnded(t)
}

// TestGivenUpInitiatedMessageLeavesTheRespondedExchange (review II,
// phase 7, R2-P7-req-04): exchange ids are the initiator's, so a message
// of an exchange this node opened that shares its id with the peer's BDX
// exchange is given up without ending the peer's exchange.
func TestGivenUpInitiatedMessageLeavesTheRespondedExchange(t *testing.T) {
	r := newExchangeSessionRig(t)
	now := time.Now()
	tr := newOutboundReliableTracker(nil, nil)
	_ = tr.trackExchangeMessage(7, r.sid, r.peerXID, true, []byte{0x00}, r.peer.addr, now, 0)
	tr.mu.Lock()
	for _, e := range tr.pending {
		e.retries = mrp.MaxRetransmissions
	}
	tr.mu.Unlock()
	r.dev.br.tickOutboundReliable(tr, now.Add(time.Hour))
	// The peer's next message on its exchange reaches the responder only
	// while the exchange is open: whichever of the two arrives first is
	// the verdict, with no wall-clock window.
	sendBDX(r.peer, r.peerXID, bdx.MessageBlockQuery, []byte{0x00})
	select {
	case err := <-r.responder.ended:
		t.Fatalf("the peer's exchange ended (%v) with a message of this node's exchange of the same id", err)
	case <-r.responder.got:
	}
}

// TestRespondedExchangeIsNeverStoredUnbound: a peer's first BDX message on a
// session id the lookup does not hold at the delivery's first look opens no
// exchange, even when a later look — the peer's resolution — finds a
// session under the id: an exchange without the session object it belongs
// to would take any later session's messages (review II, R2-P6-sec-0302,
// round 2).
func TestRespondedExchangeIsNeverStoredUnbound(t *testing.T) {
	t.Parallel()
	b, err := New(wbEmptySnapshotter, mdns.NewNoop(), Config{Listen: "127.0.0.1:0"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := channel.New(channel.Config{EncryptKey: bytes.Repeat([]byte{0xAA}, 16), DecryptKey: bytes.Repeat([]byte{0xBB}, 16), LocalNodeID: 1, PeerNodeID: 2})
	if err != nil {
		t.Fatal(err)
	}
	looks := 0
	b.AttachSessionLookup(sessionLookupFunc(func(uint16) (*channel.Session, bool) {
		looks++
		if looks == 1 {
			return nil, false // the session is not installed yet at the first look
		}
		return sess, true
	}))
	b.AttachBDXResponder(newEndingResponder())
	const sid, xid = uint16(0x0303), uint16(0x0404)
	init := message.ProtocolHeader{Initiator: true, ExchangeID: xid, ProtocolID: bdx.ProtocolID, Opcode: uint8(bdx.MessageReceiveInit)}
	if b.deliverRespondedBDX(context.Background(), &message.Header{SessionID: sid}, init, []byte{0x00}) {
		t.Error("a BDX init on a session the delivery did not find was taken")
	}
	if _, held := b.bdx.exchanges.Load(secureExchangeKey{sessionID: sid, exchangeID: xid}); held {
		t.Fatal("an exchange was stored without its session")
	}
}

// TestSendRacingTheSessionCloseLeavesNoEntry: a Send whose session closes
// between its check and the tracking of its message — the purge's drop of
// the session's entries ran in between — takes its entry back and ends the
// exchange, so no entry of the closed session stays under the bare id to be
// retransmitted and to take a place of the next session given the id
// (review II, R2-P6-sec-0302 round 2, Fable read row 5).
func TestSendRacingTheSessionCloseLeavesNoEntry(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	b.AttachAckTracker(mrp.NewAckTracker(time.Hour))
	sess, err := channel.New(channel.Config{EncryptKey: bytes.Repeat([]byte{0xAA}, 16), DecryptKey: bytes.Repeat([]byte{0xBB}, 16), LocalNodeID: 1, PeerNodeID: 2})
	if err != nil {
		t.Fatal(err)
	}
	looks := 0
	b.AttachSessionLookup(sessionLookupFunc(func(uint16) (*channel.Session, bool) {
		looks++
		if looks == 1 {
			return sess, true // Send's check
		}
		return nil, false // closed right after it
	}))
	const sid = uint16(0x0505)
	ex := &initiatedSecureExchange{b: b, sessionID: sid, addr: loopbackSrc(), session: sess, exchangeID: 0x0606, in: make(chan bdx.Message, 1), done: make(chan struct{})}
	if err := ex.Send(context.Background(), bdx.ProtocolID, uint8(bdx.MessageSendInit), []byte{0x01}); !errors.Is(err, ErrExchangeClosed) {
		t.Fatalf("Send across its session's close: %v, want ErrExchangeClosed", err)
	}
	if n := b.outboundReliable.Pending(); n != 0 {
		t.Fatalf("%d entries of the closed session left in the retransmit table", n)
	}
}
