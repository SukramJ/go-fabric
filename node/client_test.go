// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package node_test

// A minimal commissioner over real UDP: PASE (PBKDFParamRequest, Pake1-3)
// against a running node, then Interaction Model requests sealed with the
// PASE session keys. Nothing here reaches into the node: every assertion
// built on it crossed the wire both ways.

import (
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/secure/spake2"
	"github.com/SukramJ/go-fabric/tlv"
	"github.com/SukramJ/go-fabric/transport/message"
)

// Secure Channel opcodes (Matter §4.11.1).
const (
	opPBKDFParamRequest  uint8  = 0x20
	opPBKDFParamResponse uint8  = 0x21
	opPake1              uint8  = 0x22
	opPake2              uint8  = 0x23
	opPake3              uint8  = 0x24
	opStatusReport       uint8  = 0x40
	opStandaloneAck      uint8  = 0x10
	secureChannelProto   uint16 = 0x0000
	clientSessionID      uint16 = 0x0BAD
)

// commissioner is the controller half.
type commissioner struct {
	t        *testing.T
	conn     *net.UDPConn
	nodeID   uint64
	counter  uint32
	exchange uint16
	pending  *uint32 // a received reliable message counter to acknowledge
	session  *channel.Session
}

func dialCommissioner(t *testing.T, addr string) *commissioner {
	t.Helper()
	raddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		t.Fatalf("resolve %s: %v", addr, err)
	}
	conn, err := net.DialUDP("udp", nil, raddr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	var b [8]byte
	_, _ = rand.Read(b[:])
	return &commissioner{t: t, conn: conn, nodeID: binary.LittleEndian.Uint64(b[:]), counter: 1, exchange: 0x4000}
}

// recv reads the next message that is not a standalone ack; for a secured
// one it decrypts. It returns the protocol header and the payload.
func (c *commissioner) recv(timeout time.Duration) (message.ProtocolHeader, []byte, error) {
	buf := make([]byte, 8192)
	deadline := time.Now().Add(timeout)
	for {
		_ = c.conn.SetReadDeadline(deadline)
		n, err := c.conn.Read(buf)
		if err != nil {
			return message.ProtocolHeader{}, nil, err
		}
		hdr, hl, err := message.UnmarshalHeader(buf[:n])
		if err != nil {
			return message.ProtocolHeader{}, nil, err
		}
		body := append([]byte(nil), buf[hl:n]...)
		if hdr.SessionID != 0 {
			if c.session == nil {
				return message.ProtocolHeader{}, nil, errors.New("secured message before a session")
			}
			if body, _, err = c.session.Decrypt(&hdr, hdr.SecurityFlags(), body); err != nil {
				return message.ProtocolHeader{}, nil, err
			}
		}
		proto, pl, err := message.UnmarshalProtocolHeader(body)
		if err != nil {
			return message.ProtocolHeader{}, nil, err
		}
		if proto.NeedsAck {
			ctr := hdr.MessageCounter
			c.pending = &ctr
		}
		if proto.ProtocolID == secureChannelProto && proto.Opcode == opStandaloneAck {
			continue
		}
		return proto, body[pl:], nil
	}
}

// send sends one message on exchange xid; secured once a session exists.
func (c *commissioner) send(protocol uint16, opcode uint8, xid uint16, initiator bool, payload []byte) {
	c.t.Helper()
	proto := message.ProtocolHeader{Initiator: initiator, NeedsAck: true, Opcode: opcode, ExchangeID: xid, ProtocolID: protocol}
	if c.pending != nil {
		proto.HasAck, proto.AckCounter = true, *c.pending
		c.pending = nil
	}
	plain := append(proto.Marshal(), payload...)
	var datagram []byte
	if c.session == nil {
		hdr := message.Header{MessageCounter: c.counter, HasSourceNodeID: true, SourceNodeID: c.nodeID}
		c.counter++
		datagram = append(hdr.Marshal(), plain...)
	} else {
		hdr := message.Header{SessionID: c.session.PeerSessionID()}
		sealed, err := c.session.Encrypt(&hdr, hdr.SecurityFlags(), plain)
		if err != nil {
			c.t.Fatalf("encrypt: %v", err)
		}
		datagram = append(hdr.Marshal(), sealed.Ciphertext...)
	}
	if _, err := c.conn.Write(datagram); err != nil {
		c.t.Fatalf("write: %v", err)
	}
}

// expect receives the next message and wants opcode on xid.
func (c *commissioner) expect(protocol uint16, opcode uint8, xid uint16) []byte {
	c.t.Helper()
	for {
		proto, payload, err := c.recv(5 * time.Second)
		if err != nil {
			c.t.Fatalf("waiting for opcode 0x%02X: %v", opcode, err)
		}
		if proto.ExchangeID != xid || proto.ProtocolID != protocol {
			continue
		}
		if proto.Opcode != opcode {
			c.t.Fatalf("opcode 0x%02X on exchange 0x%04X, want 0x%02X (payload %x)", proto.Opcode, xid, opcode, payload)
		}
		return payload
	}
}

// pase runs the PASE handshake with passcode and installs the session.
func (c *commissioner) pase(passcode uint32) {
	c.t.Helper()
	c.exchange++
	xid := c.exchange
	initRandom := make([]byte, spake2.PBKDFRandomSize)
	_, _ = rand.Read(initRandom)
	enc := tlv.NewEncoder()
	enc.StartStruct(tlv.AnonymousTag())
	enc.PutOctets(tlv.ContextTag(1), initRandom)
	enc.PutUint(tlv.ContextTag(2), uint64(clientSessionID))
	enc.PutUint(tlv.ContextTag(3), 0)
	enc.PutBool(tlv.ContextTag(4), false)
	_ = enc.EndContainer()
	req, err := enc.Bytes()
	if err != nil {
		c.t.Fatal(err)
	}
	c.send(secureChannelProto, opPBKDFParamRequest, xid, true, req)
	respBytes := c.expect(secureChannelProto, opPBKDFParamResponse, xid)
	resp, err := spake2.DecodePBKDFParamResponse(respBytes)
	if err != nil {
		c.t.Fatalf("PBKDFParamResponse: %v", err)
	}
	if resp.Parameters == nil {
		c.t.Fatal("PBKDFParamResponse without PBKDF parameters")
	}
	h := sha256.New()
	h.Write([]byte("CHIP PAKE V1 Commissioning"))
	h.Write(req)
	h.Write(respBytes)
	prover, err := spake2.NewProver(passcode, resp.Parameters.Salt, int(resp.Parameters.Iterations), nil, nil, h.Sum(nil))
	if err != nil {
		c.t.Fatalf("NewProver: %v", err)
	}
	pA, err := prover.GeneratePake1()
	if err != nil {
		c.t.Fatal(err)
	}
	c.send(secureChannelProto, opPake1, xid, true, spake2.EncodePake1(pA))
	pB, cB, err := spake2.DecodePake2(c.expect(secureChannelProto, opPake2, xid))
	if err != nil {
		c.t.Fatalf("Pake2: %v", err)
	}
	cA, err := prover.ProcessPake2(pB, cB)
	if err != nil {
		c.t.Fatalf("ProcessPake2: %v", err)
	}
	c.send(secureChannelProto, opPake3, xid, true, spake2.EncodePake3(cA))
	status := c.expect(secureChannelProto, opStatusReport, xid)
	if len(status) < 2 || binary.LittleEndian.Uint16(status) != 0 {
		c.t.Fatalf("PASE StatusReport %x, want GeneralCode SUCCESS", status)
	}
	keys, err := hkdf.Key(sha256.New, prover.SharedSecret(), nil, "SessionKeys", 48)
	if err != nil {
		c.t.Fatal(err)
	}
	c.session, err = channel.New(channel.Config{
		EncryptKey: keys[0:16], DecryptKey: keys[16:32], PeerSessionID: resp.ResponderSessionID,
	})
	if err != nil {
		c.t.Fatal(err)
	}
}

// attrPath is one concrete attribute path.
type attrPath struct {
	endpoint  uint16
	cluster   uint32
	attribute uint32
}

func encodePaths(enc *tlv.Encoder, paths []attrPath) {
	enc.StartArray(tlv.ContextTag(0))
	for _, p := range paths {
		enc.StartList(tlv.AnonymousTag())
		enc.PutUint(tlv.ContextTag(2), uint64(p.endpoint))
		enc.PutUint(tlv.ContextTag(3), uint64(p.cluster))
		enc.PutUint(tlv.ContextTag(4), uint64(p.attribute))
		_ = enc.EndContainer()
	}
	_ = enc.EndContainer()
}

// read sends a ReadRequest and returns the ReportData payload.
func (c *commissioner) read(paths ...attrPath) []byte {
	c.t.Helper()
	enc := tlv.NewEncoder()
	enc.StartStruct(tlv.AnonymousTag())
	encodePaths(enc, paths)
	enc.PutBool(tlv.ContextTag(3), true) // FabricFiltered
	enc.PutUint(tlv.ContextTag(0xFF), 12)
	_ = enc.EndContainer()
	body, err := enc.Bytes()
	if err != nil {
		c.t.Fatal(err)
	}
	c.exchange++
	xid := c.exchange
	c.send(im.InteractionModelProtocolID, im.OpcodeReadRequest, xid, true, body)
	report := c.expect(im.InteractionModelProtocolID, im.OpcodeReportData, xid)
	c.ack()
	return report
}

// subscribe sends a SubscribeRequest, answers the priming report and
// returns it and the SubscribeResponse.
func (c *commissioner) subscribe(maxInterval uint16, paths ...attrPath) (priming, response []byte) {
	c.t.Helper()
	enc := tlv.NewEncoder()
	enc.StartStruct(tlv.AnonymousTag())
	enc.PutBool(tlv.ContextTag(0), false) // KeepSubscriptions
	enc.PutUint(tlv.ContextTag(1), 0)     // MinIntervalFloor
	enc.PutUint(tlv.ContextTag(2), uint64(maxInterval))
	enc.StartArray(tlv.ContextTag(3))
	for _, p := range paths {
		enc.StartList(tlv.AnonymousTag())
		enc.PutUint(tlv.ContextTag(2), uint64(p.endpoint))
		enc.PutUint(tlv.ContextTag(3), uint64(p.cluster))
		enc.PutUint(tlv.ContextTag(4), uint64(p.attribute))
		_ = enc.EndContainer()
	}
	_ = enc.EndContainer()
	enc.PutBool(tlv.ContextTag(7), true) // FabricFiltered
	enc.PutUint(tlv.ContextTag(0xFF), 12)
	_ = enc.EndContainer()
	body, err := enc.Bytes()
	if err != nil {
		c.t.Fatal(err)
	}
	c.exchange++
	xid := c.exchange
	c.send(im.InteractionModelProtocolID, im.OpcodeSubscribeRequest, xid, true, body)
	priming = c.expect(im.InteractionModelProtocolID, im.OpcodeReportData, xid)
	c.send(im.InteractionModelProtocolID, im.OpcodeStatusResponse, xid, true, statusResponse())
	response = c.expect(im.InteractionModelProtocolID, im.OpcodeSubscribeResponse, xid)
	c.ack()
	return priming, response
}

// nextReport waits for a ReportData the node initiates and answers it.
func (c *commissioner) nextReport(timeout time.Duration) ([]byte, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		proto, payload, err := c.recv(time.Until(deadline))
		if err != nil {
			return nil, err
		}
		if proto.ProtocolID != im.InteractionModelProtocolID || proto.Opcode != im.OpcodeReportData || !proto.Initiator {
			continue
		}
		c.send(im.InteractionModelProtocolID, im.OpcodeStatusResponse, proto.ExchangeID, false, statusResponse())
		return payload, nil
	}
	return nil, errors.New("no report before the deadline")
}

// ack acknowledges a pending reliable message with a standalone ack.
func (c *commissioner) ack() {
	if c.pending == nil {
		return
	}
	proto := message.ProtocolHeader{Opcode: opStandaloneAck, ExchangeID: c.exchange, ProtocolID: secureChannelProto, HasAck: true, AckCounter: *c.pending, Initiator: true}
	c.pending = nil
	hdr := message.Header{SessionID: c.session.PeerSessionID()}
	sealed, err := c.session.Encrypt(&hdr, hdr.SecurityFlags(), proto.Marshal())
	if err != nil {
		c.t.Fatalf("encrypt ack: %v", err)
	}
	_, _ = c.conn.Write(append(hdr.Marshal(), sealed.Ciphertext...))
}

func statusResponse() []byte {
	enc := tlv.NewEncoder()
	enc.StartStruct(tlv.AnonymousTag())
	enc.PutUint(tlv.ContextTag(0), 0)
	enc.PutUint(tlv.ContextTag(0xFF), 12)
	_ = enc.EndContainer()
	b, _ := enc.Bytes()
	return b
}

// tlvTree is a decoded TLV element and its children.
type tlvTree struct {
	El       tlv.Element
	Children []tlvTree
}

func decodeTree(t *testing.T, raw []byte) tlvTree {
	t.Helper()
	dec := tlv.NewDecoder(raw)
	el, err := dec.Next()
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return decodeNode(t, dec, el)
}

func decodeNode(t *testing.T, dec *tlv.Decoder, el tlv.Element) tlvTree {
	t.Helper()
	n := tlvTree{El: el}
	if !el.IsContainer {
		return n
	}
	for {
		c, err := dec.Next()
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if c.IsEndContainer {
			return n
		}
		n.Children = append(n.Children, decodeNode(t, dec, c))
	}
}

func (n tlvTree) child(tag uint64) (tlvTree, bool) {
	for _, c := range n.Children {
		if c.El.Tag.Kind == tlv.TagKindContext && uint64(c.El.Tag.Number) == tag {
			return c, true
		}
	}
	return tlvTree{}, false
}

// attributeData returns the AttributeDataIB values of a ReportData by
// attribute id (AttributeReports [1] → AttributeReportIB → [1] data →
// [1] path, [2] value).
func attributeData(t *testing.T, report []byte) map[uint32]tlvTree {
	t.Helper()
	out := map[uint32]tlvTree{}
	reports, ok := decodeTree(t, report).child(1)
	if !ok {
		return out
	}
	for _, r := range reports.Children {
		data, ok := r.child(1)
		if !ok {
			continue
		}
		path, _ := data.child(1)
		attr, _ := path.child(4)
		value, _ := data.child(2)
		out[uint32(attr.El.Uint)] = value
	}
	return out
}
