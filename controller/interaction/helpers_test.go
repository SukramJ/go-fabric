// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package interaction_test

import (
	"bytes"
	"net"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/controller/exchange"
	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/transport/message"
)

// fakeClock is the test clock, a [clocktest.Fake]: timers fire, in order,
// inside Advance.
type fakeClock struct{ *clocktest.Fake }

func newFakeClock() *fakeClock { return &fakeClock{clocktest.New(time.Unix(3_000_000, 0))} }

// pending lists the time left on each armed timer.
func (c *fakeClock) pending() []time.Duration { return c.Armed() }

// waitTimer waits until a timer of duration d is armed.
func (c *fakeClock) waitTimer(t *testing.T, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		for _, p := range c.pending() {
			if p == d {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("no timer of %v armed (pending %v)", d, c.pending())
		}
		time.Sleep(time.Millisecond)
	}
}

// fakeConn records the datagrams the manager sends.
type fakeConn struct{ ch chan []byte }

func (c *fakeConn) Send(_ *net.UDPAddr, payload []byte) error {
	c.ch <- append([]byte(nil), payload...)
	return nil
}

var peerAddr = &net.UDPAddr{IP: net.ParseIP("fd00::1"), Port: 5540}

// scripted is a manager with one secure session and its peer.
type scripted struct {
	mgr     *exchange.Manager
	clock   *fakeClock
	conn    *fakeConn
	session *exchange.Session
	peer    *channel.Session
	counter uint32
}

func newScripted(t *testing.T) *scripted {
	t.Helper()
	conn := &fakeConn{ch: make(chan []byte, 1024)}
	clock := newFakeClock()
	m, err := exchange.NewManager(conn, exchange.Config{Clock: clock, Rand: func() float64 { return 0.5 }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	i2r, r2i := bytes.Repeat([]byte{0x11}, 16), bytes.Repeat([]byte{0x22}, 16)
	s, err := m.NewSecureSession(exchange.SecureSessionConfig{
		ID: 0x0101, Addr: peerAddr, FabricIndex: 1,
		Channel:        channel.Config{EncryptKey: i2r, DecryptKey: r2i, LocalNodeID: 0x1111, PeerNodeID: 0x2222, PeerSessionID: 0x7101},
		PeerParameters: exchange.PeerParameters(nil),
	})
	if err != nil {
		t.Fatal(err)
	}
	peer, err := channel.New(channel.Config{EncryptKey: r2i, DecryptKey: i2r, LocalNodeID: 0x2222, PeerNodeID: 0x1111, PeerSessionID: 0x0101})
	if err != nil {
		t.Fatal(err)
	}
	return &scripted{mgr: m, clock: clock, conn: conn, session: s, peer: peer}
}

// msg is one message the manager sent.
type msg struct {
	hdr     message.Header
	proto   message.ProtocolHeader
	payload []byte
}

// next returns the next message the manager sent, skipping standalone
// acknowledgements.
func (s *scripted) next(t *testing.T) msg {
	t.Helper()
	for {
		select {
		case b := <-s.conn.ch:
			hdr, hl, err := message.UnmarshalHeader(b)
			if err != nil {
				t.Fatal(err)
			}
			plain, _, err := s.peer.Decrypt(&hdr, hdr.NonceSecurityFlags(), b[hl:])
			if err != nil {
				t.Fatal(err)
			}
			proto, pl, err := message.UnmarshalProtocolHeader(plain)
			if err != nil {
				t.Fatal(err)
			}
			if proto.ProtocolID == 0 && proto.Opcode == 0x10 { // standalone ack
				continue
			}
			return msg{hdr: hdr, proto: proto, payload: plain[pl:]}
		case <-time.After(5 * time.Second):
			t.Fatal("the manager sent nothing")
			return msg{}
		}
	}
}

// reply answers to with a reliable message of the peer on its exchange,
// acknowledging it.
func (s *scripted) reply(t *testing.T, to msg, protocol uint16, opcode uint8, payload []byte) {
	t.Helper()
	s.send(t, message.ProtocolHeader{NeedsAck: true, HasAck: true, AckCounter: to.hdr.MessageCounter, Opcode: opcode, ExchangeID: to.proto.ExchangeID, ProtocolID: protocol}, payload)
}

// ack sends a standalone acknowledgement of to.
func (s *scripted) ack(t *testing.T, to msg) {
	t.Helper()
	s.send(t, message.ProtocolHeader{HasAck: true, AckCounter: to.hdr.MessageCounter, Opcode: 0x10, ExchangeID: to.proto.ExchangeID}, nil)
}

func (s *scripted) send(t *testing.T, proto message.ProtocolHeader, payload []byte) {
	t.Helper()
	s.counter++
	hdr := message.Header{SessionID: 0x0101, MessageCounter: 0x100 + s.counter}
	sealed, err := s.peer.Encrypt(&hdr, hdr.SecurityFlags(), append(proto.Marshal(), payload...))
	if err != nil {
		t.Fatal(err)
	}
	s.mgr.HandleDatagram(append(hdr.Marshal(), sealed.Ciphertext...), peerAddr)
}
