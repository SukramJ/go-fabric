// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioner

import (
	"bytes"
	"crypto/rand"
	"log/slog"
	"net"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/controller/exchange"
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

func newFakeClock() *fakeClock { return &fakeClock{now: time.Unix(4_000_000, 0)} }

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

// live counts the timers neither fired nor stopped.
func (c *fakeClock) live() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, t := range c.timers {
		if !t.stopped && !t.fired {
			n++
		}
	}
	return n
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

// eventually polls cond until it holds or ten seconds pass.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

func discard() *slog.Logger { return slog.New(slog.DiscardHandler) }

// deviceConn is the manager's socket in a test: every datagram goes to the
// scripted device.
type deviceConn struct{ dev *scriptedDevice }

func (c *deviceConn) Send(_ *net.UDPAddr, payload []byte) error {
	c.dev.receive(append([]byte(nil), payload...))
	return nil
}

var deviceAddr = &net.UDPAddr{IP: net.ParseIP("fd00::2"), Port: 5540}

// imRequest is one IM request the scripted device received.
type imRequest struct {
	opcode  uint8
	payload []byte
}

// scriptedDevice is the far side of one secure session: it acknowledges
// every reliable message and answers IM requests through handle.
type scriptedDevice struct {
	mgr     *exchange.Manager
	ch      *channel.Session
	session uint16
	handle  func(req imRequest) (opcode uint8, payload []byte, ok bool)

	mu      sync.Mutex
	counter uint32
	inbox   chan []byte
}

// newScriptedSession registers a secure session on mgr whose peer is a
// scripted device answering through handle.
func newScriptedSession(t *testing.T, mgr *exchange.Manager, dev *scriptedDevice) *exchange.Session {
	t.Helper()
	i2r, r2i := make([]byte, 16), make([]byte, 16)
	_, _ = rand.Read(i2r)
	_, _ = rand.Read(r2i)
	s, err := mgr.NewSecureSession(exchange.SecureSessionConfig{
		ID: dev.session, Addr: deviceAddr,
		Channel:              channel.Config{EncryptKey: i2r, DecryptKey: r2i, LocalNodeID: 0, PeerNodeID: 0, PeerSessionID: 0x7000 + dev.session},
		PeerParameters:       exchange.PeerParameters(nil),
		AttestationChallenge: bytes.Repeat([]byte{0xCC}, 16),
		SuppressPeerLoss:     true,
	})
	if err != nil {
		t.Fatal(err)
	}
	dev.ch, err = channel.New(channel.Config{EncryptKey: r2i, DecryptKey: i2r, PeerSessionID: dev.session})
	if err != nil {
		t.Fatal(err)
	}
	dev.mgr = mgr
	return s
}

// receive takes one datagram of the manager, synchronously from its send.
func (d *scriptedDevice) receive(b []byte) {
	d.inbox <- b
}

// run serves the inbox until it closes.
func (d *scriptedDevice) run() {
	for b := range d.inbox {
		hdr, hl, err := message.UnmarshalHeader(b)
		if err != nil {
			continue
		}
		plain, _, err := d.ch.Decrypt(&hdr, hdr.NonceSecurityFlags(), b[hl:])
		if err != nil {
			continue
		}
		proto, pl, err := message.UnmarshalProtocolHeader(plain)
		if err != nil {
			continue
		}
		if proto.ProtocolID == 0 && proto.Opcode == 0x10 { // standalone ack
			continue
		}
		req := imRequest{opcode: proto.Opcode, payload: plain[pl:]}
		op, payload, ok := d.handle(req)
		reply := message.ProtocolHeader{HasAck: true, AckCounter: hdr.MessageCounter, ExchangeID: proto.ExchangeID, ProtocolID: proto.ProtocolID}
		if ok {
			reply.NeedsAck, reply.Opcode = true, op
		} else {
			if !proto.NeedsAck {
				continue
			}
			reply.ProtocolID, reply.Opcode = 0, 0x10
		}
		d.send(reply, payload)
	}
}

func (d *scriptedDevice) send(proto message.ProtocolHeader, payload []byte) {
	d.mu.Lock()
	d.counter++
	hdr := message.Header{SessionID: d.session, MessageCounter: 0x1000 + d.counter}
	d.mu.Unlock()
	sealed, err := d.ch.Encrypt(&hdr, hdr.SecurityFlags(), append(proto.Marshal(), payload...))
	if err != nil {
		return
	}
	go d.mgr.HandleDatagram(append(hdr.Marshal(), sealed.Ciphertext...), deviceAddr)
}
