// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package exchange

import (
	"testing"
	"testing/synctest"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// TestSessionClosedWhenItsCounterNearsExhaustion — a send that takes a
// secure session's outbound counter to within 100 000 of the maximum ends
// the session with a CloseSession, as matter.js winds it down
// (packages/protocol/src/session/NodeSession.ts:398-412, the bridge's
// endSessionAtRollover for the device side); a session whose counter is
// used up is ended by the send that fails. Either closes once the exchange
// that sent ended (TestRolloverCloseWaitsForTheExchangesInFlight). A
// closed session is never chosen again, so the peer's next interaction
// establishes a new one.
func TestSessionClosedWhenItsCounterNearsExhaustion(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		initial   uint32
		farewells int
	}{
		{name: "near exhaustion", initial: 0xFFFFFFFF - 100_000, farewells: 1},
		{name: "used up", initial: 0xFFFFFFFF},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				m, conn, _ := newTestManager(t, Config{})
				s, peer := rolloverSession(t, m, tc.initial)
				x, err := m.Initiate(s, im.InteractionModelProtocolID, Options{})
				if err != nil {
					t.Fatal(err)
				}
				_ = x.Send(t.Context(), im.OpcodeReadRequest, nil, SendOptions{SuppressAck: true})
				x.Close()
				synctest.Wait()
				if !s.IsClosed() {
					t.Fatal("the session whose counter nears exhaustion was not closed")
				}
				// The farewell leaves before the session closes (Manager.CloseSession).
				if n := farewells(t, conn, peer); n != tc.farewells {
					t.Fatalf("%d CloseSession reports went out, want %d", n, tc.farewells)
				}
			})
		})
	}
}

// farewells drains the datagrams sent and counts the StatusReports among
// them — the CloseSession reports.
func farewells(t *testing.T, conn *fakeConn, peer *securePeer) int {
	t.Helper()
	n := 0
	for {
		select {
		case sent := <-conn.ch:
			d := peer.decode(t, sent.bytes)
			if d.proto.ProtocolID == mrp.SecureChannelProtocolID && d.proto.Opcode == mrp.SCOpcodeStatusReport {
				n++
			}
		default:
			return n
		}
	}
}

// rolloverSession registers a secure session whose outbound counter starts
// at initial and returns it with its peer.
func rolloverSession(t *testing.T, m *Manager, initial uint32) (*Session, *securePeer) {
	t.Helper()
	const id = 0x0371
	s, err := m.NewSecureSession(SecureSessionConfig{
		ID: id, Addr: peerAddr, FabricIndex: 1, PeerParameters: PeerParameters(nil),
		Channel: channel.Config{
			EncryptKey: keyI2R, DecryptKey: keyR2I,
			LocalNodeID: 0x1111, PeerNodeID: 0x2222, PeerSessionID: 0x7000 + id,
			InitialCounter: initial,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ch, err := channel.New(channel.Config{EncryptKey: keyR2I, DecryptKey: keyI2R, LocalNodeID: 0x2222, PeerNodeID: 0x1111, PeerSessionID: id})
	if err != nil {
		t.Fatal(err)
	}
	return s, &securePeer{m: m, ch: ch}
}

// TestRolloverCloseWaitsForTheExchangesInFlight — the send that takes the
// counter to within 100 000 of the maximum leaves the session closing: it
// declines an exchange the peer opens, but the exchange in flight, the one
// that sent, still takes its response. The CloseSession goes out, and the
// session closes, once that exchange ended. Mirrors matter.js
// packages/protocol/src/session/NodeSession.ts:367-376 (initiateClose
// defers the close while exchanges are active), :384-391 (the last
// exchange's end closes it) and :419-424 (close emits the graceful close),
// with Session.ts:304-320 at db021da0 (review II, R2-P2-rr-ctl-02).
func TestRolloverCloseWaitsForTheExchangesInFlight(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		m, conn, _ := newTestManager(t, Config{})
		s, peer := rolloverSession(t, m, 0xFFFFFFFF-100_000)
		opened := make(chan struct{}, 1)
		m.SetProtocolHandler(im.InteractionModelProtocolID, func(*Exchange, *Message) { opened <- struct{}{} })
		x, err := m.Initiate(s, im.InteractionModelProtocolID, Options{})
		if err != nil {
			t.Fatal(err)
		}
		if err := x.Send(t.Context(), im.OpcodeInvokeRequest, []byte{0x15, 0x18}, SendOptions{SuppressAck: true}); err != nil {
			t.Fatalf("the invoke that crossed the threshold: %v", err)
		}
		synctest.Wait()
		if s.IsClosed() {
			t.Fatal("the session closed while the exchange that crossed the threshold was in flight")
		}
		if !s.IsClosing() {
			t.Fatal("the session is not closing after its counter crossed the threshold")
		}
		peer.send(t, s.ID(), peerOpens(0x6201, im.InteractionModelProtocolID, im.OpcodeReadRequest, false), []byte{0x15, 0x18})
		synctest.Wait()
		select {
		case <-opened:
			t.Fatal("the closing session took an exchange the peer opened")
		default:
		}
		peer.send(t, s.ID(), message.ProtocolHeader{Opcode: im.OpcodeInvokeResponse, ExchangeID: x.ID(), ProtocolID: im.InteractionModelProtocolID}, []byte{0x15, 0x18})
		msg, err := x.Next(t.Context(), ReceiveOptions{})
		if err != nil || msg.Opcode != im.OpcodeInvokeResponse {
			t.Fatalf("the response to the exchange in flight: %v, %v", msg, err)
		}
		if n := farewells(t, conn, peer); n != 0 {
			t.Fatalf("%d CloseSession reports went out while the exchange was in flight, want 0", n)
		}
		x.Close()
		synctest.Wait()
		if !s.IsClosed() {
			t.Fatal("the session did not close once its last exchange ended")
		}
		if n := farewells(t, conn, peer); n != 1 {
			t.Fatalf("%d CloseSession reports went out once the exchange ended, want 1", n)
		}
	})
}
