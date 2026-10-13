// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package exchange

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/transport/message"
)

// TestAddressOverrideAndSetAddr: an exchange with an address override
// sends — first transmission and retransmissions — to the override while
// the session and its other exchanges keep the session's address
// (MessageExchange.ts:297, :771, :953); SetAddr moves every later send
// (PeerAddressMonitor.ts:243).
func TestAddressOverrideAndSetAddr(t *testing.T) {
	t.Parallel()
	m, conn, clock := newTestManager(t, Config{})
	s, p := newSecurePair(t, m, 0x0321, false)
	other := &net.UDPAddr{IP: net.ParseIP("fd00::2"), Port: 5540}
	x, _ := m.Initiate(s, im.InteractionModelProtocolID, Options{AddressOverride: other})
	done := sendAsync(x, im.OpcodeReadRequest, nil, SendOptions{})
	d := conn.nextOn(t, x)
	if d.to != other {
		t.Fatalf("override send went to %v", d.to)
	}
	first := p.decode(t, d.bytes)
	iv, active := s.timing()
	clock.Advance(RetransmissionInterval(0, iv, active, 0.5))
	if d := conn.next(t); d.to != other {
		t.Fatalf("override retransmission went to %v", d.to)
	}
	p.send(t, s.ID(), ackFor(x.ID(), first.hdr.MessageCounter), nil)
	if err := waitErr(t, done); err != nil {
		t.Fatal(err)
	}
	if s.Addr() != peerAddr {
		t.Fatalf("the override moved the session to %v", s.Addr())
	}
	y, _ := m.Initiate(s, im.InteractionModelProtocolID, Options{})
	if err := y.Send(context.Background(), im.OpcodeReadRequest, nil, SendOptions{DisableMRP: true}); err != nil {
		t.Fatal(err)
	}
	if d := conn.next(t); d.to != peerAddr {
		t.Fatalf("plain exchange sent to %v", d.to)
	}
	s.SetAddr(other)
	s.SetAddr(nil)
	if err := y.Send(context.Background(), im.OpcodeReadRequest, nil, SendOptions{DisableMRP: true}); err != nil {
		t.Fatal(err)
	}
	if d := conn.next(t); d.to != other || s.Addr() != other {
		t.Fatalf("after SetAddr the send went to %v, session at %v", d.to, s.Addr())
	}
}

// TestActiveTimestamp: a secure session starts active at creation
// (NodeSession.ts:119); a receipt moves the stamp (Session.ts:141-147); the
// unsecured session is zero until the peer's first message.
func TestActiveTimestamp(t *testing.T) {
	t.Parallel()
	m, _, clock := newTestManager(t, Config{})
	s, p := newSecurePair(t, m, 0x0322, false)
	created := s.ActiveTimestamp()
	if created.IsZero() {
		t.Fatal("secure session without an active timestamp")
	}
	clock.Advance(time.Minute)
	x, _ := m.Initiate(s, im.InteractionModelProtocolID, Options{})
	p.send(t, s.ID(), message.ProtocolHeader{Opcode: im.OpcodeReportData, ExchangeID: x.ID(), ProtocolID: im.InteractionModelProtocolID}, nil)
	if got := s.ActiveTimestamp(); !got.Equal(created.Add(time.Minute)) {
		t.Fatalf("active timestamp %v, want %v", got, created.Add(time.Minute))
	}
	u, up := newUnsecured(t, m)
	if !u.ActiveTimestamp().IsZero() {
		t.Fatal("unsecured session active before any receipt")
	}
	up.reply(0x4242, 0, 0x40, false, nil, nil)
	if u.ActiveTimestamp().IsZero() {
		t.Fatal("receipt did not stamp the unsecured session")
	}
}
