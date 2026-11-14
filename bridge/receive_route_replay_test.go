// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"net"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// sealHarnessStatusResponse seals one StatusResponse on the harness session
// with the controller half's next message counter and returns the datagram.
func sealHarnessStatusResponse(t *testing.T, h *secureHarness, xid uint16) []byte {
	t.Helper()
	proto := message.ProtocolHeader{
		Initiator: true, NeedsAck: true, Opcode: im.OpcodeStatusResponse,
		ExchangeID: xid, ProtocolID: im.InteractionModelProtocolID,
	}
	plain := append(proto.Marshal(), 0x15, 0x24, 0x00, 0x00, 0x24, 0xFF, 0x0C, 0x18)
	hdr := message.Header{SessionID: harnessLocalSessionID}
	sealed, err := h.peer.Encrypt(&hdr, securityFlagsByte(&hdr), plain)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	return append(hdr.Marshal(), sealed.Ciphertext...)
}

// assertHarnessRoute fails unless the harness session's outbound route is want.
func assertHarnessRoute(t *testing.T, h *secureHarness, step string, want net.Addr) {
	t.Helper()
	got := h.bridge.currentPeerAddr(harnessLocalSessionID, nil)
	if got == nil || got.String() != want.String() {
		t.Fatalf("%s: session route = %v, want %v", step, got, want)
	}
}

// TestReceive_ReplayDoesNotMoveSessionRoute: a byte-identical copy of an
// authentic datagram, sent from another address, is a duplicate — anyone
// who saw the original can send it — so it must not move the address the
// session's reports, BDX and OTA exchanges and the CloseSession farewell go
// to. connectedhomeip src/transport/SessionManager.cpp:1054-1068 at
// 6170af84 updates the peer address only inside `if (isDuplicate ==
// SessionMessageDelegate::DuplicateMessage::No)`; matter.js
// packages/protocol/src/protocol/ExchangeManager.ts:462-467 at db021da0
// adopts it only in a branch that requires `!isDuplicate`.
func TestReceive_ReplayDoesNotMoveSessionRoute(t *testing.T) {
	t.Parallel()
	h := newSecureHarness(t, nil, nil)
	datagram := sealHarnessStatusResponse(t, h, 0x321)
	peer, _ := h.conn.LocalAddr().(*net.UDPAddr)

	h.bridge.handleDatagram(append([]byte(nil), datagram...), peer)
	assertHarnessRoute(t, h, "after the peer's datagram", peer)

	replayer := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 9}
	h.bridge.handleDatagram(append([]byte(nil), datagram...), replayer)
	assertHarnessRoute(t, h, "after the same datagram replayed from another address", peer)
}

// TestReceive_NewMessageFromNewAddressMovesSessionRoute: a peer that changed
// its address keeps working — its next datagram with a new counter rebinds
// the route.
func TestReceive_NewMessageFromNewAddressMovesSessionRoute(t *testing.T) {
	t.Parallel()
	h := newSecureHarness(t, nil, nil)
	peer, _ := h.conn.LocalAddr().(*net.UDPAddr)

	h.bridge.handleDatagram(sealHarnessStatusResponse(t, h, 0x321), peer)
	assertHarnessRoute(t, h, "after the peer's first datagram", peer)

	moved := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 9}
	h.bridge.handleDatagram(sealHarnessStatusResponse(t, h, 0x322), moved)
	assertHarnessRoute(t, h, "after a new datagram from the peer's new address", moved)
}

// TestReceive_ReplayDoesNotMoveExchangeAckRoute: the same rule for the
// route an exchange's standalone acknowledgements take — a replayed
// duplicate is acknowledged at the address the exchange (or the session)
// already has, not at the replayer's, the rule the session route follows
// (by_design BD-chip-SessionRouteByNewCounter).
func TestReceive_ReplayDoesNotMoveExchangeAckRoute(t *testing.T) {
	t.Parallel()
	h := newSecureHarness(t, nil, nil)
	// A grace window longer than the test: the original's obligation waits,
	// so the one acknowledgement sent is the duplicate's, expedited.
	h.bridge.AttachAckTracker(mrp.NewAckTracker(time.Minute))
	datagram := sealHarnessStatusResponse(t, h, 0x321)
	rhdr, _, err := message.UnmarshalHeader(datagram)
	if err != nil {
		t.Fatal(err)
	}
	peer, _ := h.conn.LocalAddr().(*net.UDPAddr)
	replayer, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = replayer.Close() })

	h.bridge.handleDatagram(append([]byte(nil), datagram...), peer)
	replayerAddr, _ := replayer.LocalAddr().(*net.UDPAddr)
	h.bridge.handleDatagram(append([]byte(nil), datagram...), replayerAddr)

	if !receivesAckFor(t, h, h.conn, rhdr.MessageCounter) {
		t.Fatalf("no acknowledgement of counter %d reached the peer; the replay moved the exchange's ack route", rhdr.MessageCounter)
	}
}

// receivesAckFor reads conn until a standalone acknowledgement of counter
// arrives (true) or the read deadline passes (false).
func receivesAckFor(t *testing.T, h *secureHarness, conn *net.UDPConn, counter uint32) bool {
	t.Helper()
	buf := make([]byte, 1500)
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	for {
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			return false
		}
		hdr, hl, err := message.UnmarshalHeader(buf[:n])
		if err != nil {
			continue
		}
		body, _, err := h.peer.Decrypt(&hdr, securityFlagsByte(&hdr), append([]byte(nil), buf[hl:n]...))
		if err != nil {
			continue
		}
		proto, _, err := message.UnmarshalProtocolHeader(body)
		if err != nil {
			continue
		}
		if proto.Opcode == mrp.StandaloneAckOpcode && proto.HasAck && proto.AckCounter == counter {
			return true
		}
	}
}
