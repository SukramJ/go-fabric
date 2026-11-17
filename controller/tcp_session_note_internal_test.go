// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"net"
	"os"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/tcp"
)

type discardConn struct{}

func (discardConn) Send(*net.UDPAddr, []byte) error { return nil }

func framed(msg []byte) []byte {
	f := make([]byte, 4+len(msg))
	binary.LittleEndian.PutUint32(f, uint32(len(msg))) //nolint:gosec // test sizes are small
	copy(f[4:], msg)
	return f
}

// TestInboundConnectionWithASessionIsNotReclaimedAsSessionless: the
// controller's TCP handler notes the messages of a secure session bound to
// the connection they arrive on (tcp.Conn.NoteSessionTraffic), as the
// bridge does, so at its inbound bound the listener measures that
// connection by its session: past sessionlessGrace, within the session idle
// grace, a newcomer does not take its place (review II, N3-finnet-4,
// round 3).
func TestInboundConnectionWithASessionIsNotReclaimedAsSessionless(t *testing.T) {
	t.Parallel()
	fake := clocktest.New(time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC))
	l, err := tcp.Listen(tcp.Config{LocalAddr: "127.0.0.1:0", Clock: fake, MaxInboundConnections: 1})
	if err != nil {
		t.Fatal(err)
	}
	mgr, err := exchange.NewManager(discardConn{}, exchange.Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mgr.Close)
	const sid = uint16(0x0101)
	handle := inboundTCPHandler(mgr)
	handled := make(chan struct{}, 4)
	ctx, cancel := context.WithCancel(t.Context())
	served := make(chan struct{})
	go func() {
		defer close(served)
		_ = l.Serve(ctx, func(conn *tcp.Conn, msg []byte, release func()) {
			if mgr.Session(sid) == nil { // the session established on this connection
				if _, err := mgr.NewSecureSession(exchange.SecureSessionConfig{
					ID: sid, Addr: mgr.AttachChannel(conn), FabricIndex: 1,
					Channel: channel.Config{EncryptKey: bytes.Repeat([]byte{0xAA}, 16), DecryptKey: bytes.Repeat([]byte{0xBB}, 16), LocalNodeID: 1, PeerNodeID: 2, PeerSessionID: 0x0202},
				}); err != nil {
					t.Error(err)
				}
			}
			handle(conn, msg, release)
			handled <- struct{}{}
		}, nil)
	}()
	t.Cleanup(func() { cancel(); _ = l.Close(); <-served })

	a, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	msg := append((&message.Header{SessionID: sid, MessageCounter: 1}).Marshal(), bytes.Repeat([]byte{0xCC}, 24)...)
	if _, err := a.Write(framed(msg)); err != nil {
		t.Fatal(err)
	}
	<-handled
	fake.Advance(2 * time.Minute) // past sessionlessGrace, within the session's idle grace

	b, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close() })
	_ = b.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := b.Read(make([]byte, 1)); errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatal("the newcomer was held: the session-carrying connection lost its place")
	}
	_ = a.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	if _, err := a.Read(make([]byte, 1)); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("the session-carrying connection was closed: %v", err)
	}
}
