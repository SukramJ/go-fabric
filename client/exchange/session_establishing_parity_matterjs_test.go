// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package exchange

// Ports of matter.js at db91886c (#4882) against the controller's Manager:
// the CASE responder session's establishing state and peer loss by
// registration order (packages/protocol/test/session/SessionManagerTest.ts,
// describes "session supersession" and "peer loss by registration order"),
// which consumer of the session table sees an establishing session
// (packages/node/test/node/SessionViewsTest.ts) and the establishing
// session a fabric's teardown closes (packages/protocol/test/fabric/FabricTest.ts).
//
// matter.js's established view (SessionManager.sessions) has no list on the
// Go Manager: the per-peer cap is its one consumer here, and the peer set's
// adoption is the CASE server's OnSession after the establishment
// (client/handshake). Supersession is not modelled on the Manager.

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/secure/channel"
	"thingfabric.dev/go-fabric/transport/message"
	"thingfabric.dev/go-fabric/transport/mrp"
)

// responderPeer is the peer of the responder sessions below: node 0x2222
// on fabric index 1.
var responderPeer = PeerAddress{FabricIndex: 1, NodeID: 0x2222}

// newResponderSession registers a CASE responder session with the peer,
// establishing (as the CASE server creates it) or established, and returns
// it with the peer's end.
func newResponderSession(t *testing.T, m *Manager, id uint16, establishing bool) (*Session, *securePeer) {
	t.Helper()
	s, err := m.newSecureSession(SecureSessionConfig{
		ID:   id,
		Addr: peerAddr,
		Channel: channel.Config{
			EncryptKey: keyI2R, DecryptKey: keyR2I,
			LocalNodeID: 0x1111, PeerNodeID: 0x2222, PeerSessionID: 0x7000 + id,
		},
		PeerParameters: PeerParameters(nil),
		FabricIndex:    1,
		Responder:      true,
	}, establishing)
	if err != nil {
		t.Fatal(err)
	}
	ch, err := channel.New(channel.Config{EncryptKey: keyR2I, DecryptKey: keyI2R, LocalNodeID: 0x2222, PeerNodeID: 0x1111, PeerSessionID: id})
	if err != nil {
		t.Fatal(err)
	}
	return s, &securePeer{m: m, ch: ch}
}

// holdClosing makes s closing without closing it: a peer loss that spares
// an open exchange on s defers the close until that exchange ends
// (NodeSession.ts deferredClose), as matter.js's holdClosing parks a
// session in its close. release ends the exchange, and with it the session.
func holdClosing(t *testing.T, m *Manager, s *Session) (release func()) {
	t.Helper()
	x, err := m.Initiate(s, im.InteractionModelProtocolID, Options{})
	if err != nil {
		t.Fatal(err)
	}
	s.handlePeerLoss(errors.New("test: closing"), x)
	if !s.IsClosing() || s.IsClosed() {
		t.Fatalf("closing %v, closed %v; want closing and open", s.IsClosing(), s.IsClosed())
	}
	return x.Close
}

func TestSessionManagerEstablishing(t *testing.T) {
	t.Parallel()
	t.Run("does not establish a session that is closing", func(t *testing.T) {
		t.Parallel()
		m, _, _ := newTestManager(t, Config{})
		establishing, _ := newResponderSession(t, m, 0x0101, true)
		release := holdClosing(t, m, establishing)
		defer release()
		if establishing.completeEstablishment() {
			t.Fatal("a closing session was established")
		}
		if !establishing.establishing.Load() {
			t.Fatal("the closing session left the establishing state")
		}
	})
	t.Run("completes establishment once and shows observers an established session", func(t *testing.T) {
		t.Parallel()
		// The Manager's observer of an establishment is the per-peer cap:
		// four established sessions and the new one are five, the cap; a
		// fifth established one makes the new one the sixth, so its
		// establishment evicts exactly one, and a second completion no
		// further one.
		m, _, clock := newTestManager(t, Config{})
		older := make([]*Session, 0, maxSessionsPerPeer)
		for i := range maxSessionsPerPeer {
			s, _ := newResponderSession(t, m, 0x0110+uint16(i), false)
			older = append(older, s)
			clock.Advance(time.Second)
		}
		establishing, _ := newResponderSession(t, m, 0x011F, true)
		if closing := closingOf(older); len(closing) != 0 {
			t.Fatalf("closing before the establishment: %v", closing)
		}
		first := establishing.completeEstablishment()
		second := establishing.completeEstablishment()
		if !first || !second {
			t.Fatalf("completeEstablishment = %v, %v on an open session; want true twice", first, second)
		}
		if establishing.establishing.Load() {
			t.Fatal("still establishing")
		}
		if closing := closingOf(older); len(closing) != 1 || closing[0] != older[0] {
			t.Fatalf("evicted %v, want only the least recently used %v", closing, older[0])
		}
	})
	t.Run("lists an establishing session in sessions only once it is established", func(t *testing.T) {
		t.Parallel()
		m, _, _ := newTestManager(t, Config{})
		establishing, _ := newResponderSession(t, m, 0x0121, true)
		if !establishing.establishing.Load() {
			t.Fatal("not establishing")
		}
		if m.Session(0x0121) != establishing || !slices.Contains(m.SessionsFor(responderPeer), establishing) {
			t.Fatal("the establishing session is not registered")
		}
		establishing.completeEstablishment()
		if establishing.establishing.Load() || m.Session(0x0121) != establishing {
			t.Fatal("not established, or no longer registered")
		}
	})
	t.Run("never lists a session whose handshake fails", func(t *testing.T) {
		t.Parallel()
		m, _, _ := newTestManager(t, Config{})
		establishing, _ := newResponderSession(t, m, 0x0131, true)
		establishing.Close()
		if establishing.completeEstablishment() {
			t.Fatal("a closed session was established")
		}
		if m.Session(0x0131) != nil || slices.Contains(m.SessionsFor(responderPeer), establishing) {
			t.Fatal("the failed session is still registered")
		}
	})
	t.Run("reports an established session that is closing as established", func(t *testing.T) {
		t.Parallel()
		m, _, _ := newTestManager(t, Config{})
		established, _ := newResponderSession(t, m, 0x0141, false)
		release := holdClosing(t, m, established)
		defer release()
		if !established.completeEstablishment() {
			t.Fatal("an established session that is closing reported not established")
		}
	})
	t.Run("neither counts nor evicts an establishing session for the per-peer cap", func(t *testing.T) {
		t.Parallel()
		m, _, clock := newTestManager(t, Config{})
		sessions := make([]*Session, 0, 6)
		for i := range 3 {
			clock.Advance(time.Second)
			s, _ := newResponderSession(t, m, 0x0150+uint16(i), false)
			sessions = append(sessions, s)
		}
		// Registered at the clock's earliest reading so far: the least
		// recently used of all, if it were counted.
		establishing, _ := newResponderSession(t, m, 0x015A, true)
		establishing.mu.Lock()
		establishing.timestamp = time.Time{}
		establishing.mu.Unlock()
		for i := range 2 {
			clock.Advance(time.Second)
			s, _ := newResponderSession(t, m, 0x0160+uint16(i), false)
			sessions = append(sessions, s)
		}
		if closing := closingOf(append(sessions, establishing)); len(closing) != 0 {
			t.Fatalf("closing: %v", closing)
		}
	})
}

// closingOf is the sessions of ss that are closing.
func closingOf(ss []*Session) []*Session {
	var out []*Session
	for _, s := range ss {
		if s.IsClosing() {
			out = append(out, s)
		}
	}
	return out
}

func TestSessionManagerPeerLossByRegistrationOrder(t *testing.T) {
	t.Parallel()
	cause := errors.New("test: unresponsive")
	t.Run("keeps the session given to peer shutdown and every session registered after it", func(t *testing.T) {
		t.Parallel()
		m, _, _ := newTestManager(t, Config{})
		older := newFabricSession(t, m, 0x0201)
		kept := newFabricSession(t, m, 0x0202)
		newer := newFabricSession(t, m, 0x0203)
		if !older.CreatedAt().Equal(kept.CreatedAt()) {
			t.Fatal("the clock moved")
		}
		m.HandlePeerLossBefore(responderPeer, cause, kept)
		if !older.IsPeerLost() || kept.IsPeerLost() || newer.IsPeerLost() {
			t.Fatalf("lost: older %v kept %v newer %v", older.IsPeerLost(), kept.IsPeerLost(), newer.IsPeerLost())
		}
	})
	t.Run("keeps a session that registers after the given registration count although it was created before", func(t *testing.T) {
		t.Parallel()
		// A Go session is created and registered in one step; the case
		// holds by meaning as a session registered after the count at the
		// same instant: its creation time does not decide.
		m, _, _ := newTestManager(t, Config{})
		lost := newFabricSession(t, m, 0x0211)
		registeredBefore := m.registrations.Load()
		later := newFabricSession(t, m, 0x0212)
		if !later.CreatedAt().Equal(lost.CreatedAt()) {
			t.Fatal("the clock moved")
		}
		m.handlePeerLoss(responderPeer, cause, registeredBefore, time.Time{}, nil)
		if !lost.IsPeerLost() || later.IsPeerLost() {
			t.Fatalf("lost: first %v later %v", lost.IsPeerLost(), later.IsPeerLost())
		}
	})
	t.Run("keeps a session registered while the peer loss runs", func(t *testing.T) {
		t.Parallel()
		var m *Manager
		var registeredDuringLoss *Session
		m, _, _ = newTestManager(t, Config{PeerLost: func(*Session, error) {
			if registeredDuringLoss == nil {
				registeredDuringLoss = newFabricSession(t, m, 0x0222)
			}
		}})
		lost := newFabricSession(t, m, 0x0221)
		m.HandlePeerLossBefore(responderPeer, cause, nil)
		if !lost.IsPeerLost() || registeredDuringLoss == nil || registeredDuringLoss.IsPeerLost() {
			t.Fatalf("lost %v; registered during the loss %v", lost.IsPeerLost(), registeredDuringLoss)
		}
	})
	t.Run("keeps the sessions created at or after a deprecated timestamp given to peer shutdown", func(t *testing.T) {
		t.Parallel()
		m, _, clock := newTestManager(t, Config{})
		older := newFabricSession(t, m, 0x0231)
		clock.Advance(time.Second)
		kept := newFabricSession(t, m, 0x0232)
		m.HandlePeerLoss(responderPeer, cause, kept.CreatedAt())
		if !older.IsPeerLost() || kept.IsPeerLost() {
			t.Fatalf("lost: older %v kept %v", older.IsPeerLost(), kept.IsPeerLost())
		}
	})
	t.Run("treats a session that never registered as keeping nothing", func(t *testing.T) {
		t.Parallel()
		m, _, _ := newTestManager(t, Config{})
		registered := newFabricSession(t, m, 0x0241)
		other, _, _ := newTestManager(t, Config{})
		unregistered := newFabricSession(t, other, 0x0242)
		m.HandlePeerLossBefore(responderPeer, cause, unregistered)
		if !registered.IsPeerLost() {
			t.Fatal("a session that never registered with the manager kept the peer's sessions")
		}
	})
}

// sessionViews is SessionViewsTest's fixture on the Manager: one peer's
// established responder session and its establishing one.
func sessionViews(t *testing.T) (m *Manager, conn *fakeConn, established, establishing *Session, p *securePeer) {
	t.Helper()
	m, conn, _ = newTestManager(t, Config{})
	establishing, p = newResponderSession(t, m, 0x0301, true)
	established, _ = newResponderSession(t, m, 0x0302, false)
	return m, conn, established, establishing, p
}

// seen is the members of the fixture's pair, established first as
// SessionViewsTest's expectation lists them, that are in got.
func seen(established, establishing *Session, got []*Session) []*Session {
	var out []*Session
	for _, s := range []*Session{establishing, established} {
		if slices.Contains(got, s) {
			out = append(out, s)
		}
	}
	return out
}

func closedOf(ss ...*Session) []*Session {
	var out []*Session
	for _, s := range ss {
		if s.IsClosed() {
			out = append(out, s)
		}
	}
	return out
}

func TestSessionViews(t *testing.T) {
	t.Parallel()
	both := func(t *testing.T, established, establishing *Session, got []*Session) {
		t.Helper()
		if v := seen(established, establishing, got); len(v) != 2 {
			t.Fatalf("seen %v, want both sessions", v)
		}
	}
	t.Run("SessionManager.sessionsFor() sees both sessions", func(t *testing.T) {
		t.Parallel()
		m, _, established, establishing, _ := sessionViews(t)
		both(t, established, establishing, m.SessionsFor(responderPeer))
	})
	t.Run("SessionManager.getSession(), which routes received messages sees both sessions", func(t *testing.T) {
		t.Parallel()
		m, _, established, establishing, p := sessionViews(t)
		both(t, established, establishing, []*Session{m.Session(establishing.ID()), m.Session(established.ID())})
		// The establishing session's messages are processed, not held
		// (NodeSession.ts:223-225 at db91886c).
		served := make(chan uint16, 1)
		m.SetProtocolHandler(im.InteractionModelProtocolID, func(_ *Exchange, first *Message) { served <- first.ExchangeID })
		p.send(t, establishing.ID(), message.ProtocolHeader{Initiator: true, ExchangeID: 0x0333, ProtocolID: im.InteractionModelProtocolID, Opcode: im.OpcodeStatusResponse}, []byte{0x15, 0x18})
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		select {
		case id := <-served:
			if id != 0x0333 {
				t.Fatalf("served exchange %#x", id)
			}
		case <-ctx.Done():
			t.Fatal("a message on the establishing session was not served")
		}
		if !establishing.establishing.Load() {
			t.Fatal("a message ended the establishment")
		}
	})
	t.Run("SessionManager.closeAllSessions() sees both sessions", func(t *testing.T) {
		t.Parallel()
		m, _, established, establishing, _ := sessionViews(t)
		m.Close()
		if got := closedOf(establishing, established); len(got) != 2 {
			t.Fatalf("closed %v, want both sessions", got)
		}
	})
	t.Run("SessionManager.handlePeerLoss() sees both sessions", func(t *testing.T) {
		t.Parallel()
		m, _, established, establishing, _ := sessionViews(t)
		m.HandlePeerLossBefore(responderPeer, errors.New("test"), nil)
		if m.Session(establishing.ID()) != nil || m.Session(established.ID()) != nil {
			t.Fatal("a lost session is still registered")
		}
		if got := closedOf(establishing, established); len(got) != 2 {
			t.Fatalf("closed %v, want both sessions", got)
		}
	})
	t.Run("eviction of the least recently used session beyond the per-peer cap sees the established session", func(t *testing.T) {
		t.Parallel()
		m, _, established, establishing, _ := sessionViews(t)
		for i := range maxSessionsPerPeer {
			newResponderSession(t, m, 0x0310+uint16(i), false)
		}
		if got := closingOf([]*Session{establishing, established}); len(got) != 1 || got[0] != established {
			t.Fatalf("evicted %v, want the established session only", got)
		}
	})
	t.Run("ExchangeManager sending CloseSession on a graceful close sees both sessions", func(t *testing.T) {
		t.Parallel()
		m, conn, established, establishing, p := sessionViews(t)
		for _, s := range []*Session{establishing, established} {
			m.CloseSession(context.Background(), s)
			d := p.decode(t, conn.next(t).bytes)
			if d.hdr.SessionID != s.PeerSessionID() || d.proto.Opcode != mrp.SCOpcodeStatusReport {
				t.Fatalf("session %#x: sent opcode %#x on session %#x, want a CloseSession on it", s.ID(), d.proto.Opcode, d.hdr.SessionID)
			}
		}
	})
}

func TestFabricSessionsForPeer(t *testing.T) {
	t.Parallel()
	t.Run("closes a session that is still establishing when the fabric is removed (characterization)", func(t *testing.T) {
		t.Parallel()
		// The controller's teardown of a fabric's sessions walks
		// SessionsFor of each peer (node.closeClientFabricSessions,
		// client/peer.Peer.Close); an establishing responder session is
		// among them, as it is in matter.js's fabric session set
		// (NodeSession.ts:166, Fabric.ts:493-496 at db91886c), and it is
		// then never established.
		m, _, _ := newTestManager(t, Config{})
		establishing, _ := newResponderSession(t, m, 0x0401, true)
		for _, s := range m.SessionsFor(responderPeer) {
			m.CloseSession(context.Background(), s)
		}
		if !establishing.IsClosed() {
			t.Fatal("the establishing session survived its fabric's teardown")
		}
		if establishing.completeEstablishment() {
			t.Fatal("a session closed with its fabric was established")
		}
	})
}
