// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package exchange

import (
	"context"
	"net"
	"slices"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/internal/exchangeseam"
	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/transport/message"
)

// newEstablishingPair registers, through the seam the CASE initiator
// uses, a secure session still establishing with local id id on fabric 1,
// and returns it with its peer.
func newEstablishingPair(t *testing.T, m *Manager, id uint16) (*Session, *securePeer) {
	t.Helper()
	raw, err := exchangeseam.NewEstablishingSecureSession(m, SecureSessionConfig{
		ID:   id,
		Addr: peerAddr,
		Channel: channel.Config{
			EncryptKey: keyI2R, DecryptKey: keyR2I,
			LocalNodeID: 0x1111, PeerNodeID: 0x2222, PeerSessionID: 0x7000 + id,
		},
		PeerParameters: PeerParameters(nil),
		FabricIndex:    1,
	})
	if err != nil {
		t.Fatal(err)
	}
	ch, err := channel.New(channel.Config{EncryptKey: keyR2I, DecryptKey: keyI2R, LocalNodeID: 0x2222, PeerNodeID: 0x1111, PeerSessionID: id})
	if err != nil {
		t.Fatal(err)
	}
	return raw.(*Session), &securePeer{m: m, ch: ch}
}

// TestEstablishingSessionHoldsUntilActivated: a session registered before
// its handshake finished decrypts what the peer sends but serves nothing
// and is not listed until it is activated; then it delivers what it held,
// at most maxHeldWhileEstablishing messages (owner decision 269,
// BD-Matter-CASEClient-SessionBeforeSigma3).
func TestEstablishingSessionHoldsUntilActivated(t *testing.T) {
	t.Parallel()
	m, _, _ := newTestManager(t, Config{})
	served := make(chan uint16, 2*maxHeldWhileEstablishing)
	m.SetProtocolHandler(im.InteractionModelProtocolID, func(_ *Exchange, first *Message) { served <- first.ExchangeID })
	s, p := newEstablishingPair(t, m, 21)
	peer := PeerAddress{FabricIndex: 1, NodeID: 0x2222}

	if got := m.SessionsFor(peer); slices.Contains(got, s) {
		t.Fatal("SessionsFor lists a session still establishing")
	}
	if m.Session(21) != s {
		t.Fatal("the establishing session is not registered under its id")
	}
	// 100..117 sent, and exchange 100's datagram once more after 114, as
	// the peer's MRP retransmits it: the retransmission takes no place in
	// the hold, so 100..115 are held; 116 and 117 find the hold full and
	// are dropped before they are decrypted, so their retransmissions
	// after the activation are fresh, not duplicates.
	raw := map[int][]byte{}
	for i := range maxHeldWhileEstablishing + 2 {
		raw[i] = p.send(t, 21, message.ProtocolHeader{Initiator: true, ExchangeID: uint16(100 + i), ProtocolID: im.InteractionModelProtocolID, Opcode: im.OpcodeStatusResponse}, []byte{0x15, 0x18})
		if i == maxHeldWhileEstablishing-2 {
			m.HandleDatagram(raw[0], peerAddr)
		}
	}
	select {
	case id := <-served:
		t.Fatalf("exchange %d served before the session was activated", id)
	default:
	}

	exchangeseam.ActivateSecureSession(s)
	if got := m.SessionsFor(peer); !slices.Contains(got, s) {
		t.Fatal("SessionsFor does not list the activated session")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var ids []uint16
	for len(ids) < maxHeldWhileEstablishing {
		select {
		case id := <-served:
			ids = append(ids, id)
		case <-ctx.Done():
			t.Fatalf("served %d of the %d held messages", len(ids), maxHeldWhileEstablishing)
		}
	}
	slices.Sort(ids)
	if ids[0] != 100 || ids[len(ids)-1] != uint16(100+maxHeldWhileEstablishing-1) {
		t.Fatalf("served exchanges %v, want the first %d sent", ids, maxHeldWhileEstablishing)
	}

	// The retransmissions of the two dropped at the cap are served.
	m.HandleDatagram(raw[maxHeldWhileEstablishing], peerAddr)
	m.HandleDatagram(raw[maxHeldWhileEstablishing+1], peerAddr)
	var late []uint16
	for len(late) < 2 {
		select {
		case id := <-served:
			late = append(late, id)
		case <-ctx.Done():
			t.Fatalf("served %v of the retransmitted exchanges 116 and 117: a message dropped at the hold's cap was recorded as received", late)
		}
	}
	slices.Sort(late)
	if late[0] != 116 || late[1] != 117 {
		t.Fatalf("served %v after the retransmissions, want [116 117]", late)
	}

	// Activated, the session serves at once.
	p.send(t, 21, message.ProtocolHeader{Initiator: true, ExchangeID: 300, ProtocolID: im.InteractionModelProtocolID, Opcode: im.OpcodeStatusResponse}, []byte{0x15, 0x18})
	select {
	case id := <-served:
		if id != 300 {
			t.Fatalf("served exchange %d, want 300 (a message beyond the hold was delivered)", id)
		}
	case <-ctx.Done():
		t.Fatal("a message after the activation was not served")
	}
}

// TestEstablishingSessionClosedDeliversNothing: an establishing session
// closed because its handshake failed leaves the table, and what it held
// is never served.
func TestEstablishingSessionClosedDeliversNothing(t *testing.T) {
	t.Parallel()
	m, _, _ := newTestManager(t, Config{})
	served := make(chan uint16, 1)
	m.SetProtocolHandler(im.InteractionModelProtocolID, func(_ *Exchange, first *Message) { served <- first.ExchangeID })
	s, p := newEstablishingPair(t, m, 22)
	p.send(t, 22, message.ProtocolHeader{Initiator: true, ExchangeID: 7, ProtocolID: im.InteractionModelProtocolID, Opcode: im.OpcodeStatusResponse}, []byte{0x15, 0x18})
	s.Close()
	if m.Session(22) != nil {
		t.Fatal("the closed establishing session is still registered")
	}
	select {
	case id := <-served:
		t.Fatalf("exchange %d served on a session whose handshake failed", id)
	default:
	}
}

// gateConn holds every datagram's send until release is closed, and
// reports on entered that a send began.
type gateConn struct {
	entered chan struct{}
	release chan struct{}
}

func (c *gateConn) Send(*net.UDPAddr, []byte) error {
	select {
	case c.entered <- struct{}{}:
	default:
	}
	<-c.release
	return nil
}

// TestCloseSessionMarksClosingBeforeItsCloseSession: a graceful close
// records that the session is closing before its CloseSession goes out, so
// a CASE responder session the close reaches while it still establishes is
// never established in between — not handed over, not counted for the
// per-peer cap, its record not saved. Mirrors matter.js
// packages/protocol/src/session/Session.ts:311 before :319 at b25ed960
// (initiateClose: closing first, then the close) with NodeSession.ts:490-492
// (only the close emits the graceful close that sends CloseSession) and
// :245-247 (completeEstablishment declines from :311 on).
func TestCloseSessionMarksClosingBeforeItsCloseSession(t *testing.T) {
	t.Parallel()
	conn := &gateConn{entered: make(chan struct{}, 1), release: make(chan struct{})}
	m, err := NewManager(conn, Config{Clock: newFakeClock(), Rand: func() float64 { return 0.5 }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	establishing, _ := newResponderSession(t, m, 0x0501, true)
	done := make(chan struct{})
	go func() {
		defer close(done)
		m.CloseSession(context.Background(), establishing)
	}()
	// CloseSession's send reaches the conn; a hang is the test binary's
	// timeout, not a wall-clock wait of its own.
	<-conn.entered
	// The CloseSession is on the wire and the session not yet closed.
	established := establishing.completeEstablishment()
	closing := establishing.IsClosing()
	close(conn.release)
	<-done
	if !closing {
		t.Error("the session was not closing while its CloseSession went out")
	}
	if established {
		t.Error("a session under a graceful close was established before its CloseSession returned")
	}
	if !establishing.IsClosed() {
		t.Error("the session is not closed after CloseSession")
	}
}
