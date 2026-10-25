// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package peer

import (
	"context"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/tlv"
	"github.com/SukramJ/go-fabric/transport/message"
)

// nodeSide is the node's end of a secure session in a unit test.
type nodeSide struct {
	h  *harness
	s  *exchange.Session
	ch *channel.Session
}

// read opens a datagram the manager sent on the session.
func (n nodeSide) read(t *testing.T, d datagram) (message.Header, message.ProtocolHeader, []byte) {
	t.Helper()
	hdr, hl, err := message.UnmarshalHeader(d.bytes)
	if err != nil {
		t.Fatal(err)
	}
	plain, _, err := n.ch.Decrypt(&hdr, hdr.NonceSecurityFlags(), d.bytes[hl:])
	if err != nil {
		t.Fatal(err)
	}
	proto, pl, err := message.UnmarshalProtocolHeader(plain)
	if err != nil {
		t.Fatal(err)
	}
	return hdr, proto, plain[pl:]
}

// send delivers the node's message on the session.
func (n nodeSide) send(t *testing.T, proto message.ProtocolHeader, payload []byte) {
	t.Helper()
	hdr := message.Header{SessionID: n.s.ID()}
	sealed, err := n.ch.Encrypt(&hdr, hdr.SecurityFlags(), append(proto.Marshal(), payload...))
	if err != nil {
		t.Fatal(err)
	}
	n.h.mgr.HandleDatagram(append(hdr.Marshal(), sealed.Ciphertext...), n.s.Addr())
}

func reportData(more, suppress bool) []byte {
	enc := tlv.NewEncoder()
	im.ReportData{MoreChunkedMessages: more, SuppressResponse: suppress}.MarshalTLV(enc, nil)
	b, _ := enc.Bytes()
	return b
}

func probeAsync(h *harness, s *exchange.Session) <-chan bool { return probeVia(h, s, nil) }

func probeVia(h *harness, s *exchange.Session, override *net.UDPAddr) <-chan bool {
	out := make(chan bool, 1)
	go func() {
		out <- probe(context.Background(), h.mgr, s, override, slog.New(slog.DiscardHandler), nil, nil)
	}()
	return out
}

func waitBool(t *testing.T, ch <-chan bool) bool {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(10 * time.Second):
		t.Fatal("probe did not return")
		return false
	}
}

// TestProbeReadsTheReports: an empty, unfiltered ReadRequest; a chunk with
// more to come is answered with StatusResponse Success, and so is a final
// report that does not suppress the response (ClientInteraction.ts:330-344,
// InteractionMessenger.ts:938-972).
func TestProbeReadsTheReports(t *testing.T) {
	t.Parallel()
	h := newHarness(t, nil)
	s, ch := h.securePair(t, 0x40, udpAddr(addr1))
	n := nodeSide{h: h, s: s, ch: ch}
	res := probeAsync(h, s)
	hdr, proto, payload := n.read(t, h.udp.next(t))
	if proto.Opcode != im.OpcodeReadRequest || proto.ProtocolID != im.InteractionModelProtocolID {
		t.Fatalf("probe sent %+v", proto)
	}
	req, err := im.UnmarshalReadRequestTLV(tlv.NewDecoder(payload))
	if err != nil || req.FabricFiltered || len(req.AttributeRequests) != 0 {
		t.Fatalf("probe request %+v, %v", req, err)
	}
	reply := message.ProtocolHeader{NeedsAck: true, HasAck: true, AckCounter: hdr.MessageCounter, Opcode: im.OpcodeReportData, ExchangeID: proto.ExchangeID, ProtocolID: im.InteractionModelProtocolID}
	n.send(t, reply, reportData(true, false))
	statusHdr, status, body := n.read(t, h.udp.next(t))
	sr, err := im.UnmarshalStatusResponseTLV(tlv.NewDecoder(body))
	if status.Opcode != im.OpcodeStatusResponse || err != nil || sr.Status != im.StatusSuccess {
		t.Fatalf("after a chunk: %+v %v", status, err)
	}
	last := message.ProtocolHeader{NeedsAck: true, HasAck: true, AckCounter: statusHdr.MessageCounter, Opcode: im.OpcodeReportData, ExchangeID: proto.ExchangeID, ProtocolID: im.InteractionModelProtocolID}
	n.send(t, last, reportData(false, false))
	if !waitBool(t, res) {
		t.Fatal("probe failed")
	}
	if _, final, _ := n.read(t, h.udp.next(t)); final.Opcode != im.OpcodeStatusResponse {
		t.Fatalf("after the last report: %+v", final)
	}
	// An override sends the probe elsewhere; the session stays where it is.
	res = probeVia(h, s, udpAddr(addr2))
	d := h.udp.next(t)
	if d.to.String() != addr2 {
		t.Fatalf("override probe went to %v", d.to)
	}
	hdr, proto, _ = n.read(t, d)
	n.send(t, message.ProtocolHeader{HasAck: true, AckCounter: hdr.MessageCounter, Opcode: im.OpcodeReportData, ExchangeID: proto.ExchangeID, ProtocolID: im.InteractionModelProtocolID}, reportData(false, true))
	if !waitBool(t, res) || s.Addr().String() != addr1 {
		t.Fatal("the override probe failed or moved the session")
	}
}

// TestProbeFailures: a closed session is not probed (requireExistingSession);
// an unanswered probe gives up after two transmissions and the final wait
// without losing the session (suppressPeerLoss, PROBE_MAX_RETRANSMISSIONS);
// a reply that is no report fails it.
func TestProbeFailures(t *testing.T) {
	t.Parallel()
	h := newHarness(t, nil)
	closed := h.secureSession(t, 0x41, udpAddr(addr1))
	closed.Close()
	if probe(context.Background(), h.mgr, closed, nil, slog.New(slog.DiscardHandler), nil, nil) {
		t.Fatal("probed a closed session")
	}
	s, ch := h.securePair(t, 0x42, udpAddr(addr1))
	n := nodeSide{h: h, s: s, ch: ch}
	res := probeAsync(h, s)
	h.udp.next(t)
	for range 20 {
		h.clock.Advance(5 * time.Second)
		select {
		case ok := <-res:
			if ok {
				t.Fatal("an unanswered probe succeeded")
			}
			if s.IsPeerLost() {
				t.Fatal("the probe reported the peer lost")
			}
			sent := 1
			for {
				select {
				case <-h.udp.ch:
					sent++
					continue
				default:
				}
				break
			}
			if sent != probeMaxRetransmissions {
				t.Fatalf("%d transmissions, want %d", sent, probeMaxRetransmissions)
			}
			goto answered
		default:
			time.Sleep(time.Millisecond)
		}
	}
	t.Fatal("the unanswered probe never gave up")
answered:
	res = probeAsync(h, s)
	hdr, proto, _ := n.read(t, h.udp.next(t))
	n.send(t, message.ProtocolHeader{HasAck: true, AckCounter: hdr.MessageCounter, Opcode: im.OpcodeStatusResponse, ExchangeID: proto.ExchangeID, ProtocolID: im.InteractionModelProtocolID}, statusSuccess())
	if waitBool(t, res) {
		t.Fatal("a StatusResponse passed as a report")
	}
	if _, _, err := reportFlags([]byte{0x15}); err == nil {
		t.Fatal("a truncated report parsed")
	}
	if _, _, err := reportFlags([]byte{0x24, 0x00, 0x01}); err == nil {
		t.Fatal("a non-structure parsed")
	}
}
