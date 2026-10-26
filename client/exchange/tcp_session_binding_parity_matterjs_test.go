// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package exchange

// A port of matter.js packages/protocol/test/protocol/TcpSessionBindingTest.ts
// at 862ad75f: sessions bound to TCP connections (ExchangeManager.ts:600-710)
// — a dropped connection evicts its sessions, a connection outlives a
// session while another uses it and closes with its last, the transport of
// a session. A fake Channel stands in for TcpChannel. The TCP session
// selection cases are client/peer's (TestPeerTCPSessionSelectionParity).

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// fakeChannel is a connected TCP channel to remote.
type fakeChannel struct {
	remote *net.TCPAddr
	mu     sync.Mutex
	sent   [][]byte
	closed chan struct{}
	once   sync.Once
}

func newFakeChannel(port int) *fakeChannel {
	return &fakeChannel{remote: &net.TCPAddr{IP: net.ParseIP("fd00::9"), Port: port}, closed: make(chan struct{})}
}

func (c *fakeChannel) Send(msg []byte) error {
	select {
	case <-c.closed:
		return errors.New("closed")
	default:
	}
	c.mu.Lock()
	c.sent = append(c.sent, msg)
	c.mu.Unlock()
	return nil
}
func (c *fakeChannel) Reliable() bool          { return true }
func (c *fakeChannel) MaxMessageSize() int     { return 64000 }
func (c *fakeChannel) Remote() net.Addr        { return c.remote }
func (c *fakeChannel) Close() error            { c.once.Do(func() { close(c.closed) }); return nil }
func (c *fakeChannel) Closed() <-chan struct{} { return c.closed }
func (c *fakeChannel) isClosed() bool {
	select {
	case <-c.closed:
		return true
	default:
		return false
	}
}

// sessionOn registers a secure session with id on ch.
func sessionOn(t *testing.T, m *Manager, id uint16, ch Channel) *Session {
	t.Helper()
	s, err := m.NewSecureSession(SecureSessionConfig{
		ID: id, Addr: m.AttachChannel(ch), FabricIndex: 1,
		Channel:        channel.Config{EncryptKey: keyI2R, DecryptKey: keyR2I, LocalNodeID: 0x1111, PeerNodeID: 0x2222, PeerSessionID: 0x7000 + id},
		PeerParameters: PeerParameters(nil),
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func bindingManager(t *testing.T) *Manager {
	t.Helper()
	m, err := NewManager(&mxConn{sent: make(chan struct{}, 64)}, Config{Clock: newFakeClock()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	return m
}

// eventuallyClosed waits for the session to close (the eviction runs on
// the manager's goroutine).
func eventuallyClosed(t *testing.T, s *Session) {
	t.Helper()
	<-s.Closing()
}

func TestTCPSessionBindingParity(t *testing.T) {
	t.Parallel()
	t.Run("Connection drop evicts sessions", func(t *testing.T) {
		t.Parallel()
		t.Run("finds sessions by channel identity", func(t *testing.T) {
			m := bindingManager(t)
			ch := newFakeChannel(5540)
			s := sessionOn(t, m, 1, ch)
			if s.channel != ch || s.Transport() != TransportTCP {
				t.Error("the session is not bound to its connection")
			}
		})
		t.Run("sessions on different connections are distinguishable", func(t *testing.T) {
			m := bindingManager(t)
			a, b := newFakeChannel(5540), newFakeChannel(5541)
			sa, sb := sessionOn(t, m, 1, a), sessionOn(t, m, 2, b)
			if sa.channel == sb.channel || sa.Addr() == sb.Addr() {
				t.Error("two connections, one binding")
			}
		})
		t.Run("session force-close marks session as closing", func(t *testing.T) {
			m := bindingManager(t)
			s := sessionOn(t, m, 1, newFakeChannel(5540))
			s.closeWith(ErrTransportClosed)
			if !s.IsClosed() {
				t.Error("not closing")
			}
		})
		t.Run("exchange can be closed with error", func(t *testing.T) {
			m := bindingManager(t)
			s := sessionOn(t, m, 1, newFakeChannel(5540))
			x, err := m.Initiate(s, mrp.SecureChannelProtocolID, Options{})
			if err != nil {
				t.Fatal(err)
			}
			x.Abort(ErrTransportClosed)
			<-x.Done()
		})
	})
	t.Run("the manager's own reaction to a dropped connection", func(t *testing.T) {
		t.Parallel()
		t.Run("closes the sessions bound to the connection that dropped", func(t *testing.T) {
			m := bindingManager(t)
			ch := newFakeChannel(5540)
			s1, s2 := sessionOn(t, m, 1, ch), sessionOn(t, m, 2, ch)
			_ = ch.Close()
			eventuallyClosed(t, s1)
			eventuallyClosed(t, s2)
		})
		t.Run("leaves a session on another connection alone", func(t *testing.T) {
			m := bindingManager(t)
			a, b := newFakeChannel(5540), newFakeChannel(5541)
			sa, sb := sessionOn(t, m, 1, a), sessionOn(t, m, 2, b)
			_ = a.Close()
			eventuallyClosed(t, sa)
			if sb.IsClosed() || b.isClosed() {
				t.Error("the other connection's session closed")
			}
		})
		t.Run("gives the closed exchange a cause that claims nothing about the peer", func(t *testing.T) {
			m := bindingManager(t)
			ch := newFakeChannel(5540)
			s := sessionOn(t, m, 1, ch)
			x, err := m.Initiate(s, imProtocolID, Options{})
			if err != nil {
				t.Fatal(err)
			}
			_ = ch.Close()
			<-x.Done()
			x.mu.Lock()
			cause := x.closeCause
			x.mu.Unlock()
			if !errors.Is(cause, ErrTransportClosed) || s.IsPeerLost() {
				t.Errorf("cause %v, peer lost %v", cause, s.IsPeerLost())
			}
		})
	})
	t.Run("TCP connection lifecycle", func(t *testing.T) {
		t.Parallel()
		t.Run("TCP connection is NOT closed by MessageChannel.close()", func(t *testing.T) {
			// A session closing leaves the connection to the other session
			// on it (ExchangeManager.ts:668-680).
			m := bindingManager(t)
			ch := newFakeChannel(5540)
			s1, _ := sessionOn(t, m, 1, ch), sessionOn(t, m, 2, ch)
			s1.Close()
			if ch.isClosed() {
				t.Error("the connection closed with one of its sessions")
			}
		})
		t.Run("multiple sessions can share the same TCP connection", func(t *testing.T) {
			m := bindingManager(t)
			ch := newFakeChannel(5540)
			s1, s2 := sessionOn(t, m, 1, ch), sessionOn(t, m, 2, ch)
			if s1.channel != ch || s2.channel != ch {
				t.Error("not shared")
			}
		})
		t.Run("closing one session does not affect sessions on other connections", func(t *testing.T) {
			m := bindingManager(t)
			a, b := newFakeChannel(5540), newFakeChannel(5541)
			sa, sb := sessionOn(t, m, 1, a), sessionOn(t, m, 2, b)
			sa.Close()
			if sb.IsClosed() || b.isClosed() {
				t.Error("affected")
			}
		})
		t.Run("closes the connection with its last session", func(t *testing.T) {
			m := bindingManager(t)
			ch := newFakeChannel(5540)
			s1, s2 := sessionOn(t, m, 1, ch), sessionOn(t, m, 2, ch)
			s1.Close()
			s2.Close()
			if !ch.isClosed() {
				t.Error("the connection outlived its last session")
			}
		})
	})
	t.Run("Channel type detection", func(t *testing.T) {
		t.Parallel()
		t.Run("TCP channels have type TCP", func(t *testing.T) {
			m := bindingManager(t)
			if s := sessionOn(t, m, 1, newFakeChannel(5540)); s.Transport() != TransportTCP || s.UsesMRP() {
				t.Errorf("transport %v, MRP %v", s.Transport(), s.UsesMRP())
			}
		})
		t.Run("UDP channels have type UDP", func(t *testing.T) {
			m := bindingManager(t)
			s, _ := newSecurePair(t, m, 1, false)
			if s.Transport() != TransportUDP || !s.UsesMRP() {
				t.Errorf("transport %v, MRP %v", s.Transport(), s.UsesMRP())
			}
		})
	})
	t.Run("sends on the connection, not the socket", func(t *testing.T) {
		t.Parallel()
		m := bindingManager(t)
		ch := newFakeChannel(5540)
		s := sessionOn(t, m, 1, ch)
		x, err := m.Initiate(s, mrp.SecureChannelProtocolID, Options{})
		if err != nil {
			t.Fatal(err)
		}
		if err := x.Send(context.Background(), 0, nil, SendOptions{SuppressAck: true}); err != nil {
			t.Fatal(err)
		}
		ch.mu.Lock()
		n := len(ch.sent)
		ch.mu.Unlock()
		if n != 1 {
			t.Errorf("%d messages on the connection, want 1", n)
		}
	})
}
