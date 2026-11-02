// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package exchange

import (
	"bytes"
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/transport/message"
)

// fakeClock is the test clock, a [clocktest.Fake] (timers fire, in order,
// inside Advance), that also records the duration of every timer started.
type fakeClock struct {
	*clocktest.Fake
	mu      sync.Mutex
	started []time.Duration
}

func newFakeClock() *fakeClock { return &fakeClock{Fake: clocktest.New(time.Unix(1_000_000, 0))} }

// AfterFunc arms the timer on the fake and then records d: a test that
// waits for the record ([armedSince], TestMessageExchangePaddedBDXCap) and
// advances the fake must find the timer armed, or the advance runs past
// a deadline that is set only afterwards and the timer never fires — the
// stress job's "send did not return" of TestNextTimeoutIsMaxPeerResponseTime
// (#306, nightly 2026-10-31), recorded before the arm until then.
func (c *fakeClock) AfterFunc(d time.Duration, f func()) Timer {
	t := c.Fake.AfterFunc(d, f)
	c.mu.Lock()
	c.started = append(c.started, d)
	c.mu.Unlock()
	return t
}

// durations returns the durations of every timer started so far.
func (c *fakeClock) durations() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]time.Duration(nil), c.started...)
}

// pendingTimers counts the timers that may still fire.
func (c *fakeClock) pendingTimers() int { return c.Pending() }

// sent is one datagram the manager sent.
type sent struct {
	to    *net.UDPAddr
	bytes []byte
}

// fakeConn records every datagram.
type fakeConn struct {
	mu   sync.Mutex
	out  []sent
	ch   chan sent
	fail error
}

func newFakeConn() *fakeConn { return &fakeConn{ch: make(chan sent, 1024)} }

func (c *fakeConn) Send(dst *net.UDPAddr, payload []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.fail != nil {
		return c.fail
	}
	d := sent{to: dst, bytes: append([]byte(nil), payload...)}
	c.out = append(c.out, d)
	c.ch <- d
	return nil
}

func (c *fakeConn) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.out)
}

// next returns the next datagram sent, failing the test after a second.
func (c *fakeConn) next(t *testing.T) sent {
	t.Helper()
	select {
	case d := <-c.ch:
		return d
	case <-time.After(5 * time.Second):
		t.Fatal("no datagram sent")
		return sent{}
	}
}

// nextOn is next for the first transmission of a send on x: it returns
// once x armed its retransmission timer, which happens under x's lock
// right after the datagram left.
func (c *fakeConn) nextOn(t *testing.T, x *Exchange) sent {
	t.Helper()
	d := c.next(t)
	_ = x.HasUnackedMessage() // a barrier: transmitLocked holds x's lock until the timer is armed
	return d
}

// none fails the test when a datagram is queued.
func (c *fakeConn) none(t *testing.T) {
	t.Helper()
	select {
	case d := <-c.ch:
		t.Fatalf("unexpected datagram %x", d.bytes)
	default:
	}
}

var peerAddr = &net.UDPAddr{IP: net.IPv6loopback, Port: 5540}

// newTestManager is a manager on a fake socket and clock, jitter fixed.
func newTestManager(t *testing.T, cfg Config) (*Manager, *fakeConn, *fakeClock) {
	t.Helper()
	conn := newFakeConn()
	clock := newFakeClock()
	cfg.Clock = clock
	if cfg.Rand == nil {
		cfg.Rand = func() float64 { return 0.5 }
	}
	m, err := NewManager(conn, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	return m, conn, clock
}

// decoded is a datagram split into its headers.
type decoded struct {
	hdr     message.Header
	proto   message.ProtocolHeader
	payload []byte
}

// decodeUnsecured splits an unsecured datagram.
func decodeUnsecured(t *testing.T, b []byte) decoded {
	t.Helper()
	hdr, hl, err := message.UnmarshalHeader(b)
	if err != nil {
		t.Fatal(err)
	}
	proto, pl, err := message.UnmarshalProtocolHeader(b[hl:])
	if err != nil {
		t.Fatal(err)
	}
	return decoded{hdr: hdr, proto: proto, payload: b[hl+pl:]}
}

// peer is the far side of an unsecured session in a test.
type peer struct {
	m       *Manager
	s       *Session
	counter uint32
}

// reply sends the peer's message on exchange xid as the responder.
func (p *peer) reply(xid, protocol uint16, opcode uint8, needsAck bool, ack *uint32, payload []byte) uint32 {
	p.counter++
	return p.replyWith(p.counter, xid, protocol, opcode, needsAck, ack, payload)
}

func (p *peer) replyWith(counter uint32, xid, protocol uint16, opcode uint8, needsAck bool, ack *uint32, payload []byte) uint32 {
	hdr := message.Header{MessageCounter: counter, DestSize: message.DestNodeID, DestNodeID: p.s.NodeID()}
	proto := message.ProtocolHeader{NeedsAck: needsAck, Opcode: opcode, ExchangeID: xid, ProtocolID: protocol}
	if ack != nil {
		proto.HasAck, proto.AckCounter = true, *ack
	}
	b := append(hdr.Marshal(), proto.Marshal()...)
	p.m.HandleDatagram(append(b, payload...), peerAddr)
	return counter
}

// securePeer is the far side of a secure session.
type securePeer struct {
	m  *Manager
	ch *channel.Session
}

var (
	keyI2R = bytes.Repeat([]byte{0x11}, 16)
	keyR2I = bytes.Repeat([]byte{0x22}, 16)
)

// newSecureConfig is a valid channel configuration.
func newSecureConfig() channel.Config {
	return channel.Config{EncryptKey: keyI2R, DecryptKey: keyR2I, LocalNodeID: 0x1111, PeerNodeID: 0x2222}
}

// newSecurePair registers a secure session with local id id and returns
// it with its peer.
func newSecurePair(t *testing.T, m *Manager, id uint16, suppress bool) (*Session, *securePeer) {
	t.Helper()
	s, err := m.NewSecureSession(SecureSessionConfig{
		ID:   id,
		Addr: peerAddr,
		Channel: channel.Config{
			EncryptKey: keyI2R, DecryptKey: keyR2I,
			LocalNodeID: 0x1111, PeerNodeID: 0x2222, PeerSessionID: 0x7000 + id,
		},
		PeerParameters:   PeerParameters(nil),
		SuppressPeerLoss: suppress,
	})
	if err != nil {
		t.Fatal(err)
	}
	ch, err := channel.New(channel.Config{EncryptKey: keyR2I, DecryptKey: keyI2R, LocalNodeID: 0x2222, PeerNodeID: 0x1111, PeerSessionID: id})
	if err != nil {
		t.Fatal(err)
	}
	return s, &securePeer{m: m, ch: ch}
}

// decode opens a datagram the manager sent on the secure session.
func (p *securePeer) decode(t *testing.T, b []byte) decoded {
	t.Helper()
	hdr, hl, err := message.UnmarshalHeader(b)
	if err != nil {
		t.Fatal(err)
	}
	plain, _, err := p.ch.Decrypt(&hdr, hdr.NonceSecurityFlags(), b[hl:])
	if err != nil {
		t.Fatal(err)
	}
	proto, pl, err := message.UnmarshalProtocolHeader(plain)
	if err != nil {
		t.Fatal(err)
	}
	return decoded{hdr: hdr, proto: proto, payload: plain[pl:]}
}

// send delivers the peer's message to the manager and returns its bytes.
func (p *securePeer) send(t *testing.T, sessionID uint16, proto message.ProtocolHeader, payload []byte) []byte {
	t.Helper()
	return p.sendFrom(t, peerAddr, sessionID, proto, payload)
}

// sendFrom is send from the source address src.
func (p *securePeer) sendFrom(t *testing.T, src *net.UDPAddr, sessionID uint16, proto message.ProtocolHeader, payload []byte) []byte {
	t.Helper()
	hdr := message.Header{SessionID: sessionID}
	sealed, err := p.ch.Encrypt(&hdr, hdr.SecurityFlags(), append(proto.Marshal(), payload...))
	if err != nil {
		t.Fatal(err)
	}
	b := append(hdr.Marshal(), sealed.Ciphertext...)
	p.m.HandleDatagram(b, src)
	return b
}

// sendAsync runs x.Send in a goroutine and returns its result channel.
func sendAsync(x *Exchange, opcode uint8, payload []byte, opts SendOptions) <-chan error {
	done := make(chan error, 1)
	go func() { done <- x.Send(context.Background(), opcode, payload, opts) }()
	return done
}

func waitErr(t *testing.T, ch <-chan error) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("send did not return")
		return errors.New("unreachable")
	}
}

func u32(v uint32) *uint32 { return &v }
