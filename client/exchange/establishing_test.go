// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package exchange

import (
	"context"
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
	sent := maxHeldWhileEstablishing + 2
	for i := range sent {
		p.send(t, 21, message.ProtocolHeader{Initiator: true, ExchangeID: uint16(100 + i), ProtocolID: im.InteractionModelProtocolID, Opcode: im.OpcodeStatusResponse}, []byte{0x15, 0x18})
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
