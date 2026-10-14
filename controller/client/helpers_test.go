// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package client_test

import (
	"bytes"
	"context"
	"net"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/controller/exchange"
	"github.com/SukramJ/go-fabric/controller/interaction"
	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/transport/message"
)

// fakeClock is a manual exchange.Clock: timers fire, in order, inside
// Advance.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*fakeTimer
}

type fakeTimer struct {
	c       *fakeClock
	at      time.Time
	d       time.Duration
	f       func()
	stopped bool
	fired   bool
}

func newFakeClock() *fakeClock { return &fakeClock{now: time.Unix(3_000_000, 0)} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) AfterFunc(d time.Duration, f func()) exchange.Timer {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &fakeTimer{c: c, at: c.now.Add(d), d: d, f: f}
	c.timers = append(c.timers, t)
	return t
}

func (t *fakeTimer) Stop() bool {
	t.c.mu.Lock()
	defer t.c.mu.Unlock()
	was := !t.stopped && !t.fired
	t.stopped = true
	return was
}

// Advance moves the clock by d, firing every due timer in order.
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	target := c.now.Add(d)
	for {
		sort.SliceStable(c.timers, func(i, j int) bool { return c.timers[i].at.Before(c.timers[j].at) })
		var next *fakeTimer
		for _, t := range c.timers {
			if !t.stopped && !t.fired && !t.at.After(target) {
				next = t
				break
			}
		}
		if next == nil {
			break
		}
		next.fired = true
		if next.at.After(c.now) {
			c.now = next.at
		}
		c.mu.Unlock()
		next.f()
		c.mu.Lock()
	}
	c.now = target
	c.mu.Unlock()
}

// fakeConn records the datagrams the manager sends.
type fakeConn struct{ ch chan []byte }

func (c *fakeConn) Send(_ *net.UDPAddr, payload []byte) error {
	c.ch <- append([]byte(nil), payload...)
	return nil
}

var peerAddr = &net.UDPAddr{IP: net.ParseIP("fd00::1"), Port: 5540}

// The scripted harness below is copied from controller/interaction's
// helpers_test.go (package interaction_test, which another package's tests
// cannot import): a manager with one secure session, a fake clock, and the
// peer's half of the session to script the node's answers.

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

// scriptedNode is a client node over a scripted session.
type scriptedNode struct {
	*scripted
	reg  *interaction.Subscriptions
	c    *interaction.Interaction
	node *client.Node
}

// newScriptedNode builds a node over a scripted session; cfg's Interaction
// is set, its Clock and Rand default to the harness's.
func newScriptedNode(t *testing.T, cfg client.Config) *scriptedNode {
	t.Helper()
	s := newScripted(t)
	reg := interaction.NewSubscriptions(interaction.SubscriptionsConfig{Clock: s.clock})
	t.Cleanup(func() { _ = reg.Close(context.Background()) })
	c := interaction.Bind(interaction.SessionProvider(s.mgr, s.session), interaction.Config{Subscriptions: reg, Clock: s.clock, Rand: func() float64 { return 0 }})
	cfg.Interaction = c
	if cfg.Clock == nil {
		cfg.Clock = s.clock
	}
	if cfg.Rand == nil {
		cfg.Rand = func() float64 { return 0 }
	}
	n, err := client.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = n.Close(context.Background()) })
	return &scriptedNode{scripted: s, reg: reg, c: c, node: n}
}
