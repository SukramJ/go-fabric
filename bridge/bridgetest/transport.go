// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridgetest

// The commissioner's message layer: one UDP socket, one goroutine reading
// it. Every reliable message is acknowledged at once with a standalone ack;
// a reply is routed to the request that waits on its exchange; a message
// the device initiates is handled here — Sigma1 / Sigma3 of a CASE
// handshake the device opens, and the reports of its subscriptions.
//
// The commissioner sends without retransmission: over loopback a datagram
// is not lost, and a test that loses one fails on its timeout.

import (
	"context"
	"encoding/binary"
	"net"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// inbound is one decoded message.
type inbound struct {
	proto   message.ProtocolHeader
	payload []byte
	counter uint32
	srcNode uint64 // the unsecured sender's node id, 0 when absent
	from    *net.UDPAddr
}

// exchangeKey names an exchange: the local session id (0 for the
// unsecured session) and the exchange id.
type exchangeKey struct {
	session  uint16
	exchange uint16
}

// readLoop reads the socket until it is closed.
func (c *Commissioner) readLoop() {
	defer c.wg.Done()
	buf := make([]byte, 65535)
	for {
		n, from, err := c.conn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		c.handle(append([]byte(nil), buf[:n]...), from)
	}
}

// handle decodes one datagram and routes it.
func (c *Commissioner) handle(datagram []byte, from *net.UDPAddr) {
	hdr, hl, err := message.UnmarshalHeader(datagram)
	if err != nil {
		return
	}
	body := datagram[hl:]
	var sess *Session
	duplicate := false
	if hdr.SessionID != 0 {
		if sess = c.session(hdr.SessionID); sess == nil {
			return
		}
		if body, duplicate, err = sess.ch.Decrypt(&hdr, hdr.SecurityFlags(), body); err != nil {
			return
		}
	}
	proto, pl, err := message.UnmarshalProtocolHeader(body)
	if err != nil {
		return
	}
	if proto.NeedsAck {
		c.sendAck(sess, &hdr, proto, from)
	}
	if duplicate || (proto.ProtocolID == mrp.SecureChannelProtocolID && proto.Opcode == mrp.StandaloneAckOpcode) {
		return
	}
	msg := inbound{proto: proto, payload: body[pl:], counter: hdr.MessageCounter, from: from}
	if hdr.HasSourceNodeID {
		msg.srcNode = hdr.SourceNodeID
	}
	local := uint16(0)
	if sess != nil {
		local = sess.localID
	}
	if !proto.Initiator {
		c.deliver(exchangeKey{session: local, exchange: proto.ExchangeID}, msg)
		return
	}
	if sess == nil {
		c.respondCASE(msg)
		return
	}
	sess.unsolicited(msg)
}

// deliver hands msg to the request waiting on key; nothing waits for a
// late or stray reply, which is dropped.
func (c *Commissioner) deliver(key exchangeKey, msg inbound) {
	c.mu.Lock()
	ch := c.waiters[key]
	c.mu.Unlock()
	if ch != nil {
		select {
		case ch <- msg:
		default:
		}
	}
}

// openExchange allocates an exchange on the session with local id local
// and registers its waiter. The returned func releases it.
func (c *Commissioner) openExchange(local uint16) (xid uint16, ch chan inbound, release func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nextExchange++
	key := exchangeKey{session: local, exchange: c.nextExchange}
	ch = make(chan inbound, 16)
	c.waiters[key] = ch
	return key.exchange, ch, func() {
		c.mu.Lock()
		delete(c.waiters, key)
		c.mu.Unlock()
	}
}

// await returns the next message on ch, or ctx's error.
func await(ctx context.Context, ch <-chan inbound) (inbound, error) {
	select {
	case <-ctx.Done():
		return inbound{}, ctx.Err()
	case msg := <-ch:
		return msg, nil
	}
}

// sendAck acknowledges the reliable message hdr/proto with a standalone
// ack on its exchange, from the other side of the exchange.
func (c *Commissioner) sendAck(sess *Session, hdr *message.Header, proto message.ProtocolHeader, to *net.UDPAddr) {
	ack := message.ProtocolHeader{
		Initiator:  !proto.Initiator,
		Opcode:     mrp.StandaloneAckOpcode,
		ExchangeID: proto.ExchangeID,
		ProtocolID: mrp.SecureChannelProtocolID,
		HasAck:     true,
		AckCounter: hdr.MessageCounter,
	}
	if sess != nil {
		_ = sess.sendProto(ack, nil)
		return
	}
	c.sendUnsecured(to, ack, nil, hdr.SourceNodeID)
}

// sendUnsecured sends one message on the unsecured session. As the
// initiator of the exchange the commissioner names its ephemeral node id
// as the source; as the responder it names the peer's as the destination
// (dest, Matter §4.13.2.1).
func (c *Commissioner) sendUnsecured(to *net.UDPAddr, proto message.ProtocolHeader, payload []byte, dest uint64) {
	c.mu.Lock()
	c.counter++
	hdr := message.Header{MessageCounter: c.counter}
	c.mu.Unlock()
	if proto.Initiator {
		hdr.HasSourceNodeID, hdr.SourceNodeID = true, c.ephemeral
	} else if dest != 0 {
		hdr.DestSize, hdr.DestNodeID = message.DestNodeID, dest
	}
	datagram := append(hdr.Marshal(), proto.Marshal()...) //nolint:gocritic // one datagram
	_, _ = c.conn.WriteToUDP(append(datagram, payload...), to)
}

// unsolicited handles a message the device initiates on s: a report of a
// subscription (answered with a StatusResponse unless it suppresses one,
// queued once its last chunk arrived) and a CloseSession StatusReport.
func (s *Session) unsolicited(msg inbound) {
	switch {
	case msg.proto.ProtocolID == im.InteractionModelProtocolID && msg.proto.Opcode == im.OpcodeReportData:
		chunk, err := decodeReport(msg.payload)
		if err != nil {
			return
		}
		if !chunk.suppress {
			_ = s.reply(msg.proto.ExchangeID, im.OpcodeStatusResponse, statusResponse(im.StatusSuccess))
		}
		s.mu.Lock()
		report := s.partial[msg.proto.ExchangeID].merge(chunk)
		if chunk.more {
			s.partial[msg.proto.ExchangeID] = report
			s.mu.Unlock()
			return
		}
		delete(s.partial, msg.proto.ExchangeID)
		s.mu.Unlock()
		select {
		case s.reports <- report:
		default:
		}
	case msg.proto.ProtocolID == mrp.SecureChannelProtocolID && msg.proto.Opcode == mrp.SCOpcodeStatusReport:
		if _, protocolID, code, ok := decodeStatusReport(msg.payload); ok &&
			protocolID == uint32(mrp.SecureChannelProtocolID) && code == statusCloseSession {
			s.markClosed()
		}
	}
}

// statusCloseSession is the Secure Channel CloseSession protocol code
// (bridge/securechannel.go:905-909).
const statusCloseSession uint16 = 0x0003

// decodeStatusReport splits a Secure Channel StatusReport body: general
// code, protocol id, protocol code, little-endian — the layout
// mrp.EncodeStatusReport writes (transport/mrp/status_report.go:31-48).
func decodeStatusReport(payload []byte) (general uint16, protocolID uint32, code uint16, ok bool) {
	if len(payload) < 8 {
		return 0, 0, 0, false
	}
	return binary.LittleEndian.Uint16(payload[0:2]), binary.LittleEndian.Uint32(payload[2:6]), binary.LittleEndian.Uint16(payload[6:8]), true
}
