// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package peer

import (
	"encoding/binary"
	"fmt"
	"testing"
	"time"

	"github.com/miekg/dns"

	"github.com/SukramJ/go-fabric/controller/exchange"
	"github.com/SukramJ/go-fabric/controller/handshake"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// pending are the remaining durations of the live timers, sorted.
func (c *fakeClock) pending() []time.Duration { return c.Armed() }

// hasTimer reports whether a live timer is due in exactly d.
func (c *fakeClock) hasTimer(d time.Duration) bool {
	for _, p := range c.pending() {
		if p == d {
			return true
		}
	}
	return false
}

// waitTimer waits until a live timer is due in exactly d.
func (c *fakeClock) waitTimer(t *testing.T, d time.Duration) {
	t.Helper()
	eventually(t, fmt.Sprintf("a %v timer", d), func() bool { return c.hasTimer(d) })
}

// nextTo skips datagrams until one goes to addr.
func (c *fakeUDP) nextTo(t *testing.T, addr string) datagram {
	t.Helper()
	for {
		if d := c.next(t); d.to.String() == addr {
			return d
		}
	}
}

// unsecured splits an unsecured datagram.
type unsecured struct {
	hdr     message.Header
	proto   message.ProtocolHeader
	payload []byte
}

func decodeUnsecured(t *testing.T, b []byte) unsecured {
	t.Helper()
	hdr, hl, err := message.UnmarshalHeader(b)
	if err != nil {
		t.Fatal(err)
	}
	proto, pl, err := message.UnmarshalProtocolHeader(b[hl:])
	if err != nil {
		t.Fatal(err)
	}
	return unsecured{hdr: hdr, proto: proto, payload: b[hl+pl:]}
}

// announceAddrs publishes the test node's operational record with the
// given IPv6 addresses on port 5540.
func (h *harness) announceAddrs(t *testing.T, ips ...string) {
	t.Helper()
	rrs := make([]dns.RR, 0, 2+len(ips))
	rrs = append(rrs, srvRR(h.qname, h.hostQ, 5540, 120), txtRR(h.qname, 120, "SII=800", "SAI=300"))
	for _, ip := range ips {
		rrs = append(rrs, aaaaRR(h.hostQ, ip, 120))
	}
	h.mdns.announce(t, rrs...)
}

// expire sends goodbyes for the given addresses.
func (h *harness) expire(t *testing.T, ips ...string) {
	t.Helper()
	rrs := make([]dns.RR, 0, len(ips))
	for _, ip := range ips {
		rrs = append(rrs, aaaaRR(h.hostQ, ip, 0))
	}
	h.mdns.announce(t, rrs...)
}

// sigma1 returns the next first transmission of a Sigma1 to addr: a
// Sigma1 whose message counter was not seen before.
func (h *harness) sigma1(t *testing.T, addr string) unsecured {
	t.Helper()
	for {
		d := h.udp.nextTo(t, addr)
		u := decodeUnsecured(t, d.bytes)
		if u.proto.Opcode != mrp.SCOpcodeSigma1 {
			continue
		}
		h.mu.Lock()
		seen := h.seen[u.hdr.MessageCounter]
		if h.seen == nil {
			h.seen = map[uint32]bool{}
		}
		h.seen[u.hdr.MessageCounter] = true
		h.mu.Unlock()
		if !seen {
			return u
		}
	}
}

// noSigma1 fails when a first Sigma1 transmission to addr is queued.
func (h *harness) noSigma1(t *testing.T, addr string) {
	t.Helper()
	time.Sleep(20 * time.Millisecond) // let a Sigma1 that is on its way arrive
	for {
		select {
		case d := <-h.udp.ch:
			u := decodeUnsecured(t, d.bytes)
			h.mu.Lock()
			seen := h.seen[u.hdr.MessageCounter]
			h.mu.Unlock()
			if d.to.String() == addr && u.proto.Opcode == mrp.SCOpcodeSigma1 && !seen {
				t.Fatalf("unexpected Sigma1 to %s", addr)
			}
		default:
			return
		}
	}
}

// reply answers a Sigma1 on its exchange, acknowledging it.
func (h *harness) reply(to unsecured, from string, opcode uint8, payload []byte) {
	h.mu.Lock()
	h.counter++
	c := h.counter
	h.mu.Unlock()
	hdr := message.Header{MessageCounter: c, DestSize: message.DestNodeID, DestNodeID: to.hdr.SourceNodeID}
	proto := message.ProtocolHeader{Opcode: opcode, ExchangeID: to.proto.ExchangeID, ProtocolID: mrp.SecureChannelProtocolID, HasAck: true, AckCounter: to.hdr.MessageCounter}
	b := append(hdr.Marshal(), proto.Marshal()...)
	h.mgr.HandleDatagram(append(b, payload...), udpAddr(from))
}

// status answers a Sigma1 with a StatusReport.
func (h *harness) status(to unsecured, from string, general, code uint16, data []byte) {
	r := exchange.StatusReport{GeneralCode: general, ProtocolID: uint32(mrp.SecureChannelProtocolID), ProtocolCode: code, ProtocolData: data}
	h.reply(to, from, mrp.SCOpcodeStatusReport, r.Encode())
}

func busyData(ms uint16) []byte { return binary.LittleEndian.AppendUint16(nil, ms) }

// resumptionRecordFor is a record for the test node.
func resumptionRecordFor(t *testing.T, h *harness) handshake.ResumptionRecord {
	t.Helper()
	return handshake.ResumptionRecord{Peer: testAddr, SharedSecret: make([]byte, 32), ResumptionID: make([]byte, 16)}
}

// currentExchange is the CASE exchange of the process's attempt at
// addr1; c.mu is held.
func currentExchange(c *connection) *exchange.Exchange {
	if att := c.attempts[addressOf(udpAddr(addr1)).AddrPort()]; att != nil {
		return att.exchange.Load()
	}
	return nil
}
