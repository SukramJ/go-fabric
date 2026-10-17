// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridgetest_test

import (
	"errors"
	"net"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/bridge/bridgetest"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// pipeConn is a reliable connection that records what the bridge sends.
type pipeConn struct {
	mu   sync.Mutex
	sent [][]byte
}

func (c *pipeConn) Send(b []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sent = append(c.sent, append([]byte(nil), b...))
	return nil
}
func (c *pipeConn) Remote() net.Addr { return &net.UnixAddr{Name: "test", Net: "unix"} }
func (c *pipeConn) Reliable() bool   { return true }

func TestServeConn(t *testing.T) {
	if _, err := bridgetest.ServeConn(nil, &pipeConn{}); !errors.Is(err, bridgetest.ErrNoBridge) {
		t.Fatalf("nil bridge: %v", err)
	}
	b := newBridge(t)
	if _, err := bridgetest.ServeConn(b, nil); !errors.Is(err, bridgetest.ErrNoConn) {
		t.Fatalf("nil conn: %v", err)
	}
	conn := &pipeConn{}
	srv, err := bridgetest.ServeConn(b, conn)
	if err != nil {
		t.Fatal(err)
	}
	// An unsolicited message of the unsecured session, in order, and
	// garbage the bridge drops: both reach the receive path without MRP.
	hdr := message.Header{MessageCounter: 1, HasSourceNodeID: true, SourceNodeID: 0x42}
	proto := message.ProtocolHeader{Initiator: true, Opcode: mrp.SCOpcodeStatusReport, ExchangeID: 7, ProtocolID: mrp.SecureChannelProtocolID}
	srv.Deliver(append(hdr.Marshal(), proto.Marshal()...))
	srv.Deliver([]byte{0xff})
	srv.Wait()
	srv.Close()
	srv.Deliver([]byte{0x00}) // after Close: dropped
	srv.Wait()
	conn.mu.Lock()
	defer conn.mu.Unlock()
	for _, s := range conn.sent {
		h, hl, err := message.UnmarshalHeader(s)
		if err != nil {
			continue
		}
		if p, _, err := message.UnmarshalProtocolHeader(s[hl:]); err == nil && p.NeedsAck {
			t.Fatalf("a reply over a reliable connection asks for MRP: %+v %+v", h, p)
		}
	}
}
