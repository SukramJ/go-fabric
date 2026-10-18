// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package exchange_test

// A port of matter.js
// packages/protocol/test/securechannel/SecureChannelMessengerTest.ts at
// 862ad75f: the status report that must not be reliable — CloseSession —
// as the controller's exchange manager sends it.

import (
	"bytes"
	"context"
	"net"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/controller/exchange"
	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// secureChannelMessengerConn records what reached the wire.
type secureChannelMessengerConn struct {
	mu   sync.Mutex
	sent [][]byte
}

func (c *secureChannelMessengerConn) Send(_ *net.UDPAddr, d []byte) error {
	c.mu.Lock()
	c.sent = append(c.sent, bytes.Clone(d))
	c.mu.Unlock()
	return nil
}

func TestSecureChannelMessengerStatusReportsThatMustNotBeReliable(t *testing.T) {
	t.Parallel()
	t.Run("sends CloseSession with the R flag clear", func(t *testing.T) {
		t.Parallel()
		i2r, r2i := bytes.Repeat([]byte{0x11}, 16), bytes.Repeat([]byte{0x22}, 16)
		conn := &secureChannelMessengerConn{}
		m, err := exchange.NewManager(conn, exchange.Config{})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(m.Close)
		s, err := m.NewSecureSession(exchange.SecureSessionConfig{
			ID: 0x0100, Addr: &net.UDPAddr{IP: net.IPv6loopback, Port: 5540},
			Channel: channel.Config{EncryptKey: i2r, DecryptKey: r2i, LocalNodeID: 1, PeerNodeID: 2, PeerSessionID: 0x0200},
		})
		if err != nil {
			t.Fatal(err)
		}
		m.CloseSession(context.Background(), s)

		conn.mu.Lock()
		sent := conn.sent
		conn.mu.Unlock()
		if len(sent) != 1 {
			t.Fatalf("%d datagrams sent, want 1", len(sent))
		}
		// The peer's side of the session opens it.
		peer, err := channel.New(channel.Config{EncryptKey: r2i, DecryptKey: i2r, LocalNodeID: 2, PeerNodeID: 1})
		if err != nil {
			t.Fatal(err)
		}
		hdr, n, err := message.UnmarshalHeader(sent[0])
		if err != nil {
			t.Fatal(err)
		}
		plain, _, err := peer.Decrypt(&hdr, hdr.NonceSecurityFlags(), sent[0][n:])
		if err != nil {
			t.Fatal(err)
		}
		ph, _, err := message.UnmarshalProtocolHeader(plain)
		if err != nil {
			t.Fatal(err)
		}
		if ph.Opcode != mrp.SCOpcodeStatusReport || ph.NeedsAck {
			t.Fatalf("opcode %#x NeedsAck=%v, want a StatusReport with the R flag clear", ph.Opcode, ph.NeedsAck)
		}
	})
}
