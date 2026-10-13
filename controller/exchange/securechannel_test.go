// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package exchange

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// peerOpens is the header of a message the peer sends to open exchange xid.
func peerOpens(xid, protocol uint16, opcode uint8, needsAck bool) message.ProtocolHeader {
	return message.ProtocolHeader{Initiator: true, NeedsAck: needsAck, Opcode: opcode, ExchangeID: xid, ProtocolID: protocol}
}

func statusBody(general, code uint16) []byte {
	return StatusReport{GeneralCode: general, ProtocolID: uint32(mrp.SecureChannelProtocolID), ProtocolCode: code}.Encode()
}

// TestPeerCloseSession: an initial StatusReport Success / CloseSession on a
// secure session loses the session with PeerInitiatedCloseError; the
// exchange that carried it is acknowledged at once and closed
// (SecureChannelProtocol.ts:54-83, NodeSession.ts:305-309).
func TestPeerCloseSession(t *testing.T) {
	t.Parallel()
	var rec lossRecorder
	m, conn, _ := newTestManager(t, Config{PeerLost: rec.hook})
	s, p := newSecurePair(t, m, 0x0361, false)
	x, _ := m.Initiate(s, im.InteractionModelProtocolID, Options{})
	next := make(chan error, 1)
	go func() {
		_, err := x.Next(context.Background(), ReceiveOptions{Timeout: time.Hour})
		next <- err
	}()
	sent := p.send(t, s.ID(), peerOpens(0x5000, mrp.SecureChannelProtocolID, mrp.SCOpcodeStatusReport, true), statusBody(mrp.SCStatusGeneralSuccess, statusCloseSession))
	ack := p.decode(t, conn.next(t).bytes)
	if ack.proto.Opcode != mrp.StandaloneAckOpcode || ack.proto.Initiator || ack.proto.ExchangeID != 0x5000 || !ack.proto.HasAck {
		t.Fatalf("ack of the CloseSession %+v", ack.proto)
	}
	err := waitErr(t, next)
	var closed *PeerInitiatedCloseError
	if !errors.As(err, &closed) || !errors.Is(err, ErrTransient) {
		t.Fatalf("Next = %v, want PeerInitiatedCloseError", err)
	}
	if !s.IsPeerLost() || !s.IsClosed() || rec.count(s) != 1 {
		t.Fatalf("lost %v closed %v hook %d", s.IsPeerLost(), s.IsClosed(), rec.count(s))
	}
	// A message on the lost session opens nothing and is not acknowledged.
	m.HandleDatagram(sent, peerAddr)
	conn.none(t)
}

// TestPeerSecureChannelOthers: any other initial StatusReport and any other
// Secure Channel message are acknowledged and leave the session alone; a
// standalone ack without R opens nothing (SecureChannelProtocol.ts:36-52,
// :69-77; ExchangeManager.ts:413-420); another protocol's message is
// acknowledged (:441-452).
func TestPeerSecureChannelOthers(t *testing.T) {
	t.Parallel()
	m, conn, _ := newTestManager(t, Config{})
	s, p := newSecurePair(t, m, 0x0362, false)
	cases := []struct {
		name     string
		proto    message.ProtocolHeader
		body     []byte
		wantsAck bool
	}{
		{"busy status", peerOpens(0x5001, mrp.SecureChannelProtocolID, mrp.SCOpcodeStatusReport, true), statusBody(8, 4), true},
		{"short status", peerOpens(0x5002, mrp.SecureChannelProtocolID, mrp.SCOpcodeStatusReport, true), []byte{1}, true},
		{"sigma1", peerOpens(0x5003, mrp.SecureChannelProtocolID, mrp.SCOpcodeSigma1, true), nil, true},
		// A standalone ack never takes an ack, R or not (MessageExchange.ts:519-522).
		{"ack with R", peerOpens(0x5004, mrp.SecureChannelProtocolID, mrp.StandaloneAckOpcode, true), nil, false},
		{"bare ack", peerOpens(0x5005, mrp.SecureChannelProtocolID, mrp.StandaloneAckOpcode, false), nil, false},
		{"im report", peerOpens(0x5006, im.InteractionModelProtocolID, im.OpcodeReportData, true), nil, true},
		{"im report without R", peerOpens(0x5007, im.InteractionModelProtocolID, im.OpcodeReportData, false), nil, false},
	}
	for _, c := range cases {
		p.send(t, s.ID(), c.proto, c.body)
		if !c.wantsAck {
			conn.none(t)
			continue
		}
		ack := p.decode(t, conn.next(t).bytes)
		if ack.proto.Opcode != mrp.StandaloneAckOpcode || ack.proto.ExchangeID != c.proto.ExchangeID || ack.proto.Initiator {
			t.Fatalf("%s: ack %+v", c.name, ack.proto)
		}
	}
	if s.IsPeerLost() || s.IsClosed() {
		t.Fatal("a message other than CloseSession ended the session")
	}
}
