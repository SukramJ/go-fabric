// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package exchange

import (
	"context"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// TestSessionClosedWhenItsCounterNearsExhaustion — a send that takes a
// secure session's outbound counter to within 100 000 of the maximum ends
// the session with a CloseSession, as matter.js winds it down
// (packages/protocol/src/session/NodeSession.ts:398-412, the bridge's
// endSessionAtRollover for the device side); a session whose counter is
// used up is ended by the send that fails. A closed session is never
// chosen again, so the peer's next interaction establishes a new one.
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
			m, conn, _ := newTestManager(t, Config{})
			const id = 0x0371
			s, err := m.NewSecureSession(SecureSessionConfig{
				ID: id, Addr: peerAddr, FabricIndex: 1, PeerParameters: PeerParameters(nil),
				Channel: channel.Config{
					EncryptKey: keyI2R, DecryptKey: keyR2I,
					LocalNodeID: 0x1111, PeerNodeID: 0x2222, PeerSessionID: 0x7000 + id,
					InitialCounter: tc.initial,
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			ch, err := channel.New(channel.Config{EncryptKey: keyR2I, DecryptKey: keyI2R, LocalNodeID: 0x2222, PeerNodeID: 0x1111, PeerSessionID: id})
			if err != nil {
				t.Fatal(err)
			}
			peer := &securePeer{m: m, ch: ch}
			x, err := m.Initiate(s, im.InteractionModelProtocolID, Options{})
			if err != nil {
				t.Fatal(err)
			}
			_ = x.Send(t.Context(), im.OpcodeReadRequest, nil, SendOptions{SuppressAck: true})
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			select {
			case <-s.Closing():
			case <-ctx.Done():
				t.Fatal("the session whose counter nears exhaustion was not closed")
			}
			// The farewell leaves before the session closes (Manager.CloseSession).
			farewells := 0
			for drained := false; !drained; {
				select {
				case sent := <-conn.ch:
					d := peer.decode(t, sent.bytes)
					if d.proto.ProtocolID == mrp.SecureChannelProtocolID && d.proto.Opcode == mrp.SCOpcodeStatusReport {
						farewells++
					}
				default:
					drained = true
				}
			}
			if farewells != tc.farewells {
				t.Fatalf("%d CloseSession reports went out, want %d", farewells, tc.farewells)
			}
		})
	}
}
