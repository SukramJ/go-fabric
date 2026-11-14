// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"crypto/rand"
	"net"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/bridge/bridgetest"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// While a PASE session exists that is not closing, the node accepts no new
// PASE exchange: matter.js ignores it ("Pairing already in progress (PASE
// session exists), ignoring new exchange",
// packages/protocol/src/session/pase/PaseServer.ts:80-82 at db021da0), and
// connectedhomeip stops listening for PBKDFParamRequest once the PASE
// session is established (src/app/server/CommissioningWindowManager.cpp:211,
// :224 and :560-561 at 6170af84). A second PBKDFParamRequest earns no
// PBKDFParamResponse (review II, R2-P3-pase-01, R2-P3-lan-dev-04).
func TestNoNewPASEExchangeWhileAPASESessionExists(t *testing.T) {
	t.Parallel()
	n := newNode(t, minimalConfig(openDB(t, t.TempDir()+"/node.db")))
	if err := n.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	c := bridgetest.Dial(t, n.Bridge().LocalAddr(), commissionerOptions())
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	if _, err := c.PASE(ctx); err != nil {
		t.Fatalf("the commissioner's PASE: %v", err)
	}
	if pbkdfParamRequestAnswered(t, n.Bridge().LocalAddr()) {
		t.Fatal("a second PBKDFParamRequest was answered with a PBKDFParamResponse while the commissioner's PASE session exists")
	}
}

// TestPBKDFParamRequestAnsweredWithoutAPASESession is the control: on a node
// without a PASE session the same datagram is answered.
func TestPBKDFParamRequestAnsweredWithoutAPASESession(t *testing.T) {
	t.Parallel()
	n := newNode(t, minimalConfig(openDB(t, t.TempDir()+"/node.db")))
	if err := n.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !pbkdfParamRequestAnswered(t, n.Bridge().LocalAddr()) {
		t.Fatal("a PBKDFParamRequest on a node without a PASE session was not answered")
	}
}

// pbkdfParamRequestAnswered sends one PBKDFParamRequest from a socket of its
// own and a source node id of its own, and reports whether a
// PBKDFParamResponse comes back before the test's read deadline.
func pbkdfParamRequestAnswered(t *testing.T, addr string) bool {
	t.Helper()
	device, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: device.IP})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	initRand := make([]byte, 32)
	_, _ = rand.Read(initRand)
	req := make([]byte, 0, 4+len(initRand)+11)
	req = append(req, 0x15, 0x30, 0x01, 0x20)
	req = append(req, initRand...)
	req = append(req, 0x25, 0x02, 0x2A, 0x00, 0x25, 0x03, 0x00, 0x00, 0x28, 0x04, 0x18)
	hdr := message.Header{MessageCounter: 77, HasSourceNodeID: true, SourceNodeID: 0xBAD0BAD0}
	proto := message.ProtocolHeader{
		Initiator: true, NeedsAck: true, Opcode: mrp.SCOpcodePBKDFParamRequest,
		ExchangeID: 0x7777, ProtocolID: mrp.SecureChannelProtocolID,
	}
	datagram := append(hdr.Marshal(), proto.Marshal()...)
	datagram = append(datagram, req...)
	if _, err := conn.WriteToUDP(datagram, device); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	buf := make([]byte, 1500)
	_ = conn.SetReadDeadline(deadline)
	for {
		k, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			return false // nothing but (at most) acknowledgements: the exchange was ignored
		}
		_, hl, err := message.UnmarshalHeader(buf[:k])
		if err != nil {
			continue
		}
		p, _, err := message.UnmarshalProtocolHeader(buf[hl:k])
		if err != nil {
			continue
		}
		if p.Opcode == mrp.SCOpcodePBKDFParamResponse {
			return true
		}
	}
}
