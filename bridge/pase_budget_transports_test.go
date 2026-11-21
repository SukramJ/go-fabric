// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/controller/ble/bletest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// failingPaseHandler refuses every PASE message, as a wrong passcode or a
// malformed request does, and counts the calls.
type failingPaseHandler struct{ calls *atomic.Int32 }

func (h failingPaseHandler) fail() (opcode uint8, reply []byte, err error) {
	h.calls.Add(1)
	return 0, nil, errors.New("refused by the test")
}

func (h failingPaseHandler) ProcessPBKDFParamRequest([]byte) (opcode uint8, reply []byte, err error) {
	return h.fail()
}

func (h failingPaseHandler) ProcessPake1([]byte) (opcode uint8, reply []byte, err error) {
	return h.fail()
}

func (h failingPaseHandler) ProcessPake3([]byte) (opcode uint8, reply []byte, err error) {
	return h.fail()
}

// pbkdfParamRequest is an unsecured PBKDFParamRequest from source node src
// on exchange xid; its body does not matter to a handler that refuses it.
func pbkdfParamRequest(src uint64, xid uint16) []byte {
	hdr := message.Header{MessageCounter: uint32(xid), HasSourceNodeID: true, SourceNodeID: src}
	proto := message.ProtocolHeader{Initiator: true, Opcode: mrp.SCOpcodePBKDFParamRequest, ExchangeID: xid, ProtocolID: mrp.SecureChannelProtocolID}
	return append(hdr.Marshal(), proto.Marshal()...)
}

// unsecuredOpcode is the secure-channel opcode of an unsecured reply.
func unsecuredOpcode(t *testing.T, msg []byte) message.ProtocolHeader {
	t.Helper()
	hdr, n, err := message.UnmarshalHeader(msg)
	if err != nil {
		t.Fatal(err)
	}
	if hdr.SessionID != 0 {
		t.Fatalf("reply on session %d, want an unsecured one", hdr.SessionID)
	}
	proto, _, err := message.UnmarshalProtocolHeader(msg[n:])
	if err != nil {
		t.Fatal(err)
	}
	return proto
}

// TestPASEFailuresShareOneBudgetAcrossTransports: a PASE failure counts
// against the one budget of [paseMaxErrors] whichever transport carried it
// — UDP, TCP or BLE all enter Bridge.receive — and the lockout a failure on
// one transport engages refuses PASE on the others (matter.js counts every
// error of its one PaseServer, PaseServer.ts:94-110 at db021da0, whatever
// channel the exchange came over; docs/security.md states the shared budget;
// review II, docA-22).
func TestPASEFailuresShareOneBudgetAcrossTransports(t *testing.T) {
	p := &bletest.Peripheral{Address: "C0:FF:EE:00:00:22", Discriminator: 3840, VendorID: 0xFFF1, ProductID: 0x8001}
	h := newConnHarness(t, Config{Listen: "127.0.0.1:0", TCP: TCPConfig{Incoming: true}, BLE: p.Radio()})
	b := h.bridge
	if err := b.StartBLE(context.Background(), bleWindow); err != nil {
		t.Fatalf("StartBLE: %v", err)
	}
	waitFor(t, "the GATT service served", func() bool { s, _ := p.Radio().Serving(); return s })
	var calls atomic.Int32
	b.AttachPaseHandler(failingPaseHandler{calls: &calls})

	// Each failure is answered with a StatusReport on the transport it
	// came over, and moves the one counter.
	udp, err := net.Dial("udp", b.LocalAddr())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = udp.Close() })
	if _, err := udp.Write(pbkdfParamRequest(0xA001, 0x101)); err != nil {
		t.Fatal(err)
	}
	if err := udp.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 1500)
	n, err := udp.Read(buf)
	if err != nil {
		t.Fatalf("no reply over UDP: %v", err)
	}
	if proto := unsecuredOpcode(t, buf[:n]); proto.Opcode != mrp.SCOpcodeStatusReport || proto.ExchangeID != 0x101 {
		t.Fatalf("UDP reply opcode 0x%02X on exchange 0x%X, want the StatusReport of 0x101", proto.Opcode, proto.ExchangeID)
	}
	// The StatusReport goes out before the failure is counted
	// (handlePase), so the count is awaited.
	waitFor(t, "failure 1 counted after UDP", func() bool { return b.failedAttempts.Load() == 1 })

	tcpConn, tcpReplies := h.dial()
	if err := tcpConn.Send(pbkdfParamRequest(0xA002, 0x102)); err != nil {
		t.Fatal(err)
	}
	if proto := unsecuredOpcode(t, waitReply(t, tcpReplies)); proto.Opcode != mrp.SCOpcodeStatusReport || proto.ExchangeID != 0x102 {
		t.Fatalf("TCP reply opcode 0x%02X on exchange 0x%X, want the StatusReport of 0x102", proto.Opcode, proto.ExchangeID)
	}
	waitFor(t, "failure 2 counted after TCP", func() bool { return b.failedAttempts.Load() == 2 })

	central := dialRadio(t, p)
	if err := central.s.Send(pbkdfParamRequest(0xA003, 0x103)); err != nil {
		t.Fatal(err)
	}
	if proto := unsecuredOpcode(t, waitReply(t, central.replies)); proto.Opcode != mrp.SCOpcodeStatusReport || proto.ExchangeID != 0x103 {
		t.Fatalf("BLE reply opcode 0x%02X on exchange 0x%X, want the StatusReport of 0x103", proto.Opcode, proto.ExchangeID)
	}
	waitFor(t, "failure 3 counted after BLE", func() bool { return b.failedAttempts.Load() == 3 })

	// The last failure of the budget arrives over UDP and engages the
	// lockout, which starts the counter over (engagePaseLockout).
	for b.failedAttempts.Load() < paseMaxErrors-1 {
		b.recordFailedAttempt(attemptPASEPairing)
	}
	if _, err := udp.Write(pbkdfParamRequest(0xA001, 0x104)); err != nil {
		t.Fatal(err)
	}
	if _, err = udp.Read(buf); err != nil {
		t.Fatalf("no reply over UDP: %v", err)
	}
	waitFor(t, "the lockout", b.paseLockedOut)

	// TCP and BLE refuse PASE now: the PBKDFParamRequest reaches no
	// handler and gets no reply. A connection hands its messages to the
	// bridge in order, so the reply to the invoke sent after it is the
	// first reply, and by then the request was dispatched.
	before := calls.Load()
	for _, leg := range []struct {
		name    string
		send    func([]byte) error
		replies <-chan []byte
		xid     uint16
	}{
		{"TCP", tcpConn.Send, tcpReplies, 0x105},
		{"BLE", central.s.Send, central.replies, 0x106},
	} {
		if err := leg.send(pbkdfParamRequest(0xA002, leg.xid)); err != nil {
			t.Fatal(err)
		}
		if err := leg.send(h.sealedInvoke(leg.xid + 0x100)); err != nil {
			t.Fatal(err)
		}
		reply := waitReply(t, leg.replies)
		if hdr, _, err := message.UnmarshalHeader(reply); err == nil && hdr.SessionID == 0 {
			t.Fatalf("%s: the PBKDFParamRequest was answered under the lockout (opcode 0x%02X)", leg.name, unsecuredOpcode(t, reply).Opcode)
		}
		if proto := h.open(reply); proto.Opcode != im.OpcodeInvokeResponse || proto.ExchangeID != leg.xid+0x100 {
			t.Fatalf("%s: first reply opcode 0x%02X on exchange 0x%X, want the InvokeResponse of 0x%X", leg.name, proto.Opcode, proto.ExchangeID, leg.xid+0x100)
		}
		if got := calls.Load(); got != before {
			t.Fatalf("%s: the PASE handler ran %d times under the lockout", leg.name, got-before)
		}
		if got := b.failedAttempts.Load(); got != 0 {
			t.Fatalf("%s: failures = %d under the lockout, want 0", leg.name, got)
		}
	}
}
