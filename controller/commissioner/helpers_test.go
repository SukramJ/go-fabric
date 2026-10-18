// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioner

import (
	"bytes"
	"crypto/rand"
	"log/slog"
	"net"
	"sync"
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

func newFakeClock() *fakeClock { return &fakeClock{clocktest.New(time.Unix(4_000_000, 0))} }

// live counts the timers neither fired nor stopped.
func (c *fakeClock) live() int { return c.Pending() }

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
