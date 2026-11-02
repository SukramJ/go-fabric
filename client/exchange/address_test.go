// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package exchange

import (
	"context"
	"math"
	"net"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
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

// TestNewExchangeMovesSessionToSource (W4-ctlnet-02): a message that opens
// a new exchange on a secure session from another address moves the
// session there — the report is acked at the new address and the observer
// learns of it; a message inside an existing exchange moves nothing.
// Mirrors matter.js ExchangeManager.ts:425-431.
func TestNewExchangeMovesSessionToSource(t *testing.T) {
	t.Parallel()
	m, conn, _ := newTestManager(t, Config{})
	s, p := newSecurePair(t, m, 0x0323, false)
	moved := make(chan *net.UDPAddr, 4)
	s.OnAddressChanged(func(a *net.UDPAddr) { moved <- a })
	h, calls := recordingHandler(nil)
	m.SetProtocolHandler(im.InteractionModelProtocolID, h)
	other := &net.UDPAddr{IP: net.ParseIP("fd00::7"), Port: 5540}

	// Inside an existing exchange: no move.
	x, _ := m.Initiate(s, im.InteractionModelProtocolID, Options{})
	p.sendFrom(t, other, s.ID(), message.ProtocolHeader{Opcode: im.OpcodeReportData, ExchangeID: x.ID(), ProtocolID: im.InteractionModelProtocolID}, nil)
	if s.Addr() != peerAddr {
		t.Fatalf("a message inside an existing exchange moved the session to %v", s.Addr())
	}

	p.sendFrom(t, other, s.ID(), peerOpens(0x6101, im.InteractionModelProtocolID, im.OpcodeReportData, true), []byte{0x15, 0x18})
	waitHandled(t, calls)
	if got := s.Addr(); !sameAddr(got, other) {
		t.Fatalf("session at %v, want %v", got, other)
	}
	select {
	case a := <-moved:
		if !sameAddr(a, other) {
			t.Fatalf("observer told %v", a)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("observer not told of the move")
	}
	d := conn.next(t)
	if ack := p.decode(t, d.bytes); ack.proto.Opcode != mrp.StandaloneAckOpcode || ack.proto.ExchangeID != 0x6101 {
		t.Fatalf("ack %+v", ack.proto)
	}
	if !sameAddr(d.to, other) {
		t.Fatalf("ack went to %v, want the new address %v", d.to, other)
	}
}

// TestAddressHelpers: satAdd saturates at both Duration bounds; the
// address observer can be removed; sameAddr compares nil and zone; an
// unsecured session or a TCP-carried source moves nothing (W4-ctlnet-01,
// W4-ctlnet-02).
func TestAddressHelpers(t *testing.T) {
	t.Parallel()
	if satAdd(math.MaxInt64, time.Second) != math.MaxInt64 || satAdd(math.MinInt64, -time.Second) != math.MinInt64 || satAdd(time.Second, time.Second) != 2*time.Second {
		t.Fatal("satAdd")
	}
	a := &net.UDPAddr{IP: net.ParseIP("fe80::1"), Port: 5540, Zone: "eth0"}
	if !sameAddr(nil, nil) || sameAddr(a, nil) || sameAddr(a, &net.UDPAddr{IP: a.IP, Port: a.Port, Zone: "eth1"}) {
		t.Fatal("sameAddr")
	}
	m, _, _ := newTestManager(t, Config{})
	s, _ := newSecurePair(t, m, 0x0324, false)
	called := false
	s.OnAddressChanged(func(*net.UDPAddr) { called = true })
	s.OnAddressChanged(nil)
	s.SetAddr(a)
	if called || !sameAddr(s.Addr(), a) {
		t.Fatal("a removed observer was called, or SetAddr did not move")
	}
	u, _ := newUnsecured(t, m)
	before := u.Addr()
	u.adoptSource(&net.UDPAddr{IP: net.ParseIP("fd00::5"), Port: 5540})
	if u.Addr() != before {
		t.Fatal("an unsecured session moved")
	}
}
