// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/secure/operational"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// deferredRevokeRig is a PASE device with the retransmit table wired, a
// UDP commissioner on the PASE session and a sink that takes what the
// retransmit table re-sends of the messages the rig tracks itself.
type deferredRevokeRig struct {
	dev           *resumeDevice
	w             *CommissioningWindow
	sid, peerSID  uint16
	peer          revokePeer
	peerSess      *channel.Session
	sink          net.Addr
	outbound      *outboundReliableTracker
	peerAddr      *net.UDPAddr
	responseExch  uint16
	responseCount uint32
}

func newDeferredRevokeRig(t *testing.T) *deferredRevokeRig {
	t.Helper()
	dev, w, sid, peerSID, peer := revokePASEDeviceWith(t, func(br *Bridge, _ *operational.Manager) {
		br.AttachAckTracker(mrp.NewAckTracker(0))
	})
	sink, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sink.Close() })
	dev.br.mu.RLock()
	outbound := dev.br.outboundReliable
	dev.br.mu.RUnlock()
	if outbound == nil {
		t.Fatal("no outbound-reliable tracker")
	}
	peerAddr, ok := peer.conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		t.Fatalf("peer address %T", peer.conn.LocalAddr())
	}
	return &deferredRevokeRig{
		dev: dev, w: w, sid: sid, peerSID: peerSID, peer: peer, peerSess: peer.peers[peerSID],
		sink: sink.LocalAddr(), outbound: outbound, peerAddr: peerAddr,
		responseExch: 0x0077, responseCount: 0x5000,
	}
}

// revokeOverPASE runs RevokeWindow as the RevokeCommissioning handler on
// the PASE session does: inside a handler turn on the session, its
// response tracked by the retransmit table before the turn ends (when
// tracked), the turn ending after the response.
func (r *deferredRevokeRig) revokeOverPASE(t *testing.T, tracked bool) {
	t.Helper()
	b := r.dev.br
	b.enterSessionTurn(r.sid)
	after := &im.AfterResponse{}
	ctx := im.WithAfterResponse(im.WithAuthModePASE(core.WithInvokeSessionID(context.Background(), r.sid)), after)
	if err := r.w.RevokeWindow(ctx); err != nil {
		t.Fatalf("RevokeWindow: %v", err)
	}
	if tracked {
		r.trackResponse(t)
	}
	after.Run()
	b.exitSessionTurn(r.sid)
}

// trackResponse puts a message of this node on the PASE session's
// responder exchange in the retransmit table, unacknowledged.
func (r *deferredRevokeRig) trackResponse(t *testing.T) {
	t.Helper()
	if err := r.outbound.TrackOnExchange(r.responseCount, r.sid, r.responseExch, false, []byte{0}, r.sink, r.dev.br.clk().Now()); err != nil {
		t.Fatal(err)
	}
}

// fromPeer seals a message of the commissioner on the PASE session.
func (r *deferredRevokeRig) fromPeer(t *testing.T, proto message.ProtocolHeader, payload []byte) []byte {
	t.Helper()
	body := append(proto.Marshal(), payload...)
	hdr := message.Header{SessionID: r.sid}
	enc, err := r.peerSess.Encrypt(&hdr, securityFlagsByte(&hdr), body)
	if err != nil {
		t.Fatalf("peer Encrypt: %v", err)
	}
	return append(hdr.Marshal(), enc.Ciphertext...)
}

// ackResponse has the commissioner acknowledge the tracked response with
// a standalone ack on its exchange.
func (r *deferredRevokeRig) ackResponse(t *testing.T) {
	t.Helper()
	d := r.fromPeer(t, message.ProtocolHeader{
		Initiator: true, HasAck: true, AckCounter: r.responseCount,
		Opcode: mrp.StandaloneAckOpcode, ExchangeID: r.responseExch, ProtocolID: mrp.SecureChannelProtocolID,
	}, nil)
	if err := r.dev.br.dispatch(context.Background(), d, r.peerAddr); err != nil {
		t.Fatalf("dispatch ack: %v", err)
	}
}

func (r *deferredRevokeRig) assertOpenAndQuiet(t *testing.T, when string) {
	t.Helper()
	if got := r.peer.read(t); len(got) != 0 {
		t.Fatalf("%s: datagrams reached the commissioner: %+v, want none", when, got)
	}
	if _, err := r.dev.sessions.Get(r.sid); err != nil {
		t.Fatalf("%s: PASE session %v, want open", when, err)
	}
}

// TestRevokeClosesThePASESessionWhenItIsIdle: F-b25ed960-56 (iv)(v) — a
// revoked PASE session closes, with its CloseSession, once it has no
// exchange in flight: revoked over the PASE session itself, after the
// RevokeCommissioning's response is acknowledged or its retransmissions
// are spent; revoked over CASE while the PASE session waits for an
// acknowledgement, after that acknowledgement. Mirrors matter.js
// packages/protocol/src/session/Session.ts:304-320 at b25ed960
// (initiateClose), packages/protocol/src/session/NodeSession.ts:438-462 at b25ed960
// (deferredClose, the close when the last exchange closes) and
// packages/protocol/src/protocol/MessageExchange.ts:1187-1216 at b25ed960
// (an exchange whose message is unacknowledged closes on the ack, :549-558,
// or on the spent budget, :995-1006). No matter.js it( covers the
// deferral (AdministratorCommissioningServerTest.ts has none).
func TestRevokeClosesThePASESessionWhenItIsIdle(t *testing.T) {
	t.Parallel()
	t.Run("over PASE, after the response's acknowledgement", func(t *testing.T) {
		t.Parallel()
		r := newDeferredRevokeRig(t)
		r.revokeOverPASE(t, true)
		r.assertOpenAndQuiet(t, "after the response, before its ack")
		r.ackResponse(t)
		assertOneCloseSession(t, r.peer.read(t), r.peerSID)
		assertPASEClosed(t, r.dev.sessions, r.sid)
	})
	t.Run("over PASE, after the response's retransmissions are spent", func(t *testing.T) {
		t.Parallel()
		r := newDeferredRevokeRig(t)
		r.revokeOverPASE(t, true)
		r.assertOpenAndQuiet(t, "after the response, before its give-up")
		now := r.dev.br.clk().Now()
		for i := range 2 * mrp.MaxRetransmissions {
			r.dev.br.tickOutboundReliable(r.outbound, now.Add(time.Duration(i+1)*time.Minute))
		}
		assertOneCloseSession(t, r.peer.read(t), r.peerSID)
		assertPASEClosed(t, r.dev.sessions, r.sid)
	})
	t.Run("over PASE, nothing awaiting an ack: after the response", func(t *testing.T) {
		t.Parallel()
		// A connection carries no MRP (matter.js
		// MessageExchange.ts:1182-1190 at b25ed960): the exchange closes
		// with the response, and the session with it.
		r := newDeferredRevokeRig(t)
		r.revokeOverPASE(t, false)
		assertOneCloseSession(t, r.peer.read(t), r.peerSID)
		assertPASEClosed(t, r.dev.sessions, r.sid)
	})
	t.Run("over CASE, the PASE session awaiting an ack", func(t *testing.T) {
		t.Parallel()
		r := newDeferredRevokeRig(t)
		r.trackResponse(t)
		if err := r.w.RevokeWindow(context.Background()); err != nil {
			t.Fatalf("RevokeWindow: %v", err)
		}
		r.assertOpenAndQuiet(t, "before the ack")
		r.ackResponse(t)
		assertOneCloseSession(t, r.peer.read(t), r.peerSID)
		assertPASEClosed(t, r.dev.sessions, r.sid)
	})
	t.Run("over CASE, the PASE session idle: at once", func(t *testing.T) {
		t.Parallel()
		r := newDeferredRevokeRig(t)
		if err := r.w.RevokeWindow(context.Background()); err != nil {
			t.Fatalf("RevokeWindow: %v", err)
		}
		if _, err := r.dev.sessions.Get(r.sid); err == nil {
			t.Fatal("the idle PASE session outlived the revoke's own call")
		}
		assertOneCloseSession(t, r.peer.read(t), r.peerSID)
	})
	t.Run("the fail-safe's expiry leaves a deferred close alone", func(t *testing.T) {
		t.Parallel()
		// N1: the revoke expires a fail-safe armed over PASE right after
		// its close was deferred; that rollback closes the PASE session it
		// finds, and a closing one it does not find (matter.js
		// packages/protocol/src/session/SessionManager.ts:660-664 at
		// b25ed960, getPaseSession skips a closing session).
		r := newDeferredRevokeRig(t)
		r.revokeOverPASE(t, true)
		r.dev.br.FailSafeExpired(context.Background())
		r.assertOpenAndQuiet(t, "after the fail-safe's expiry")
		r.ackResponse(t)
		assertOneCloseSession(t, r.peer.read(t), r.peerSID)
		assertPASEClosed(t, r.dev.sessions, r.sid)
	})
	t.Run("the close runs at the deferral's cap", func(t *testing.T) {
		t.Parallel()
		// Not matter.js's (it has no bound): a deferral is held to
		// paseCloseDeferralCap on the bridge's clock, past which the
		// session closes, CloseSession sent — here behind a handler turn
		// that never ends, the leak the cap guards against.
		fake := clocktest.New(time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC))
		dev, w, sid, peerSID, peer := revokePASEDeviceWith(t, func(br *Bridge, _ *operational.Manager) {
			br.clock = fake
		})
		dev.br.enterSessionTurn(sid)
		if err := w.RevokeWindow(context.Background()); err != nil {
			t.Fatalf("RevokeWindow: %v", err)
		}
		fake.Advance(paseCloseDeferralCap - time.Millisecond)
		if got := peer.read(t); len(got) != 0 {
			t.Fatalf("datagrams before the cap: %+v", got)
		}
		if _, err := dev.sessions.Get(sid); err != nil {
			t.Fatalf("PASE session before the cap: %v, want open", err)
		}
		fake.Advance(time.Millisecond)
		assertOneCloseSession(t, peer.read(t), peerSID)
		assertPASEClosed(t, dev.sessions, sid)
	})
	t.Run("a timed window holds the close until it elapses", func(t *testing.T) {
		t.Parallel()
		// (c): a TimedRequest's window is an exchange in flight until its
		// Write or Invoke comes or the window elapses.
		fake := clocktest.New(time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC))
		dev, w, sid, peerSID, peer := revokePASEDeviceWith(t, func(br *Bridge, _ *operational.Manager) {
			br.clock = fake
		})
		dev.br.routing.timedDeadlines.Store(timedKey{sessionID: sid, exchangeID: 0x33}, fake.Now().Add(2*time.Second))
		if err := w.RevokeWindow(context.Background()); err != nil {
			t.Fatalf("RevokeWindow: %v", err)
		}
		if _, err := dev.sessions.Get(sid); err != nil {
			t.Fatalf("PASE session with a timed window open: %v, want open", err)
		}
		fake.Advance(2*time.Second + time.Millisecond)
		assertOneCloseSession(t, peer.read(t), peerSID)
		assertPASEClosed(t, dev.sessions, sid)
	})
}

// TestClosingPASESessionTakesNoNewExchange: N2 — while a revoked PASE
// session's close is deferred, a message that opens a new exchange on it
// is dropped: no ack, no reply, the commissioning channel's implicit
// Administer serves nothing more; a duplicate of a message handled before
// is still acknowledged. Mirrors matter.js
// packages/protocol/src/protocol/ExchangeManager.ts:433-438 at b25ed960.
func TestClosingPASESessionTakesNoNewExchange(t *testing.T) {
	t.Parallel()
	r := newDeferredRevokeRig(t)
	// A message of the commissioner handled before the revoke, needing an
	// ack: an unsolicited StatusResponse on an exchange of its own.
	before := r.fromPeer(t, message.ProtocolHeader{
		Initiator: true, NeedsAck: true, Opcode: im.OpcodeStatusResponse, ExchangeID: 0x0101, ProtocolID: im.InteractionModelProtocolID,
	}, []byte{0x15, 0x24, 0x00, 0x00, 0x24, 0xFF, 0x0C, 0x18})
	if err := r.dev.br.dispatch(context.Background(), before, r.peerAddr); err != nil {
		t.Logf("dispatch before the revoke: %v", err)
	}
	_ = r.peer.read(t) // its ack
	r.revokeOverPASE(t, true)
	// A Read on a new exchange.
	read := r.fromPeer(t, message.ProtocolHeader{
		Initiator: true, NeedsAck: true, Opcode: im.OpcodeReadRequest, ExchangeID: 0x0202, ProtocolID: im.InteractionModelProtocolID,
	}, []byte{0x15, 0x36, 0x00, 0x17, 0x24, 0x02, 0x28, 0x18, 0x18, 0x28, 0x03, 0x24, 0xFF, 0x0C, 0x18})
	if err := r.dev.br.dispatch(context.Background(), read, r.peerAddr); !errors.Is(err, errNewExchangeOnClosingSession) {
		t.Fatalf("dispatch Read: %v, want %v", err, errNewExchangeOnClosingSession)
	}
	r.assertOpenAndQuiet(t, "after a Read on a new exchange")
	if r.dev.br.ackTracker.Owes(r.sid, 0x0202, false) || r.dev.br.ackTracker.Owes(r.sid, 0x0202, true) {
		t.Fatal("an ack is owed for a message on a closing session's new exchange")
	}
	// The duplicate of the message handled before is acknowledged.
	if err := r.dev.br.dispatch(context.Background(), before, r.peerAddr); err != nil {
		t.Logf("dispatch duplicate: %v", err)
	}
	got := r.peer.read(t)
	if len(got) != 1 || got[0].proto.Opcode != mrp.StandaloneAckOpcode || got[0].proto.ExchangeID != 0x0101 {
		t.Fatalf("the duplicate got %+v, want one standalone ack on exchange 0x0101", got)
	}
	r.ackResponse(t)
	assertOneCloseSession(t, r.peer.read(t), r.peerSID)
}

// TestRevokeDeferredCloseClosesOnlyTheNamedSession: the revoke closes the
// PASE sessions it named, by id, and nothing else when a deferred close
// finally runs — a PASE session a new commissioning established meanwhile
// survives it, and the host's PaseSessionCloser runs at the revoke, not
// at the deferred close. matter.js closes the one session getPaseSession()
// returned (packages/node/src/behaviors/administrator-commissioning/AdministratorCommissioningServer.ts:143-147
// at b25ed960; packages/protocol/src/session/SessionManager.ts:660-664).
func TestRevokeDeferredCloseClosesOnlyTheNamedSession(t *testing.T) {
	t.Parallel()
	r := newDeferredRevokeRig(t)
	host := &countingPaseCloser{}
	r.w.SetPaseSessionCloser(host)
	r.revokeOverPASE(t, true)
	if host.calls != 1 {
		t.Fatalf("the host's closer ran %d times at the revoke, want 1", host.calls)
	}
	r.assertOpenAndQuiet(t, "before the ack")
	// A new commissioning's PASE session, established during the deferral.
	newer, err := r.dev.sessions.OpenFromPase(1, 3, 0x0E08, []byte("another-pase-secret"))
	if err != nil {
		t.Fatalf("OpenFromPase: %v", err)
	}
	r.ackResponse(t)
	assertOneCloseSession(t, r.peer.read(t), r.peerSID)
	assertPASEClosed(t, r.dev.sessions, r.sid)
	if _, err := r.dev.sessions.Get(newer.SessionID); err != nil {
		t.Fatalf("the PASE session established during the deferral: %v, want open", err)
	}
	if host.calls != 1 {
		t.Fatalf("the host's closer ran %d times, want once, at the revoke", host.calls)
	}
}

// TestRevokeMarksASessionOnce: two revokes that both name a PASE session —
// a RevokeCommissioning and the attempt cap, listed before either marked
// it — defer its close once: one deferred close, one count, one cap timer.
func TestRevokeMarksASessionOnce(t *testing.T) {
	t.Parallel()
	r := newDeferredRevokeRig(t)
	b := r.dev.br
	if got := b.markPASECloses([]uint16{r.sid}); len(got) != 1 {
		t.Fatalf("first mark %v, want [%d]", got, r.sid)
	}
	if got := b.markPASECloses([]uint16{r.sid}); len(got) != 0 {
		t.Fatalf("second mark %v, want none: the first revoke's", got)
	}
	if n := b.paseDeferredN.Load(); n != 1 {
		t.Fatalf("deferred count %d, want 1", n)
	}
	b.finishPASEClose(r.sid, "idle")
	if n := b.paseDeferredN.Load(); n != 0 {
		t.Fatalf("deferred count %d after the close, want 0", n)
	}
	assertOneCloseSession(t, r.peer.read(t), r.peerSID)
}

// TestStopEndsDeferredPASECloses: Bridge.Stop forgets the deferred closes
// and stops their timers; none runs on the stopped bridge afterwards.
func TestStopEndsDeferredPASECloses(t *testing.T) {
	t.Parallel()
	fake := clocktest.New(time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC))
	dev, w, sid, _, _ := revokePASEDeviceWith(t, func(br *Bridge, _ *operational.Manager) {
		br.clock = fake
	})
	dev.br.enterSessionTurn(sid)
	if err := w.RevokeWindow(context.Background()); err != nil {
		t.Fatalf("RevokeWindow: %v", err)
	}
	if !dev.br.paseCloseDeferred(sid) {
		t.Fatal("the busy PASE session's close is not deferred")
	}
	if err := dev.br.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	if dev.br.paseCloseDeferred(sid) || dev.br.paseDeferredN.Load() != 0 {
		t.Fatal("a deferred close outlived Stop")
	}
	for _, d := range fake.Armed() {
		if d == paseCloseDeferralCap {
			t.Fatal("the deferral's cap timer is still armed after Stop")
		}
	}
}
