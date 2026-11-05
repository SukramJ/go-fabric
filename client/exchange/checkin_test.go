// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package exchange

import (
	"bytes"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// TestCheckInReachesTheReceiver holds SecureChannelProtocol.ts:149-180: an
// unsecured ICD Check-In goes to the receiver, nothing is sent back, the
// exchange and the inbound session close; without a receiver it is
// dropped.
func TestCheckInReachesTheReceiver(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		m, conn, _ := newTestManager(t, Config{})
		checkIn := func(counter uint32) {
			hdr := message.Header{MessageCounter: counter, HasSourceNodeID: true, SourceNodeID: 0xBBBB}
			proto := message.ProtocolHeader{Initiator: true, Opcode: mrp.SCOpcodeIcdCheckIn, ExchangeID: 0x5000 + uint16(counter), ProtocolID: mrp.SecureChannelProtocolID}
			m.HandleDatagram(append(append(hdr.Marshal(), proto.Marshal()...), 0x01, 0x02, 0x03), peerAddr)
		}

		checkIn(1) // no receiver: dropped
		conn.none(t)
		waitNoInbound(t, m)

		var mu sync.Mutex
		var got [][]byte
		m.SetCheckInReceiver(func(payload []byte) bool {
			mu.Lock()
			defer mu.Unlock()
			got = append(got, bytes.Clone(payload))
			return len(got) == 1
		})
		checkIn(2)
		checkIn(3)
		synctest.Wait()
		mu.Lock()
		n := len(got)
		mu.Unlock()
		if n != 2 {
			t.Fatalf("%d Check-Ins received", n)
		}
		mu.Lock()
		first := got[0]
		mu.Unlock()
		if !bytes.Equal(first, []byte{1, 2, 3}) {
			t.Fatalf("payload %x", first)
		}
		conn.none(t)
		waitNoInbound(t, m)
		m.SetCheckInReceiver(nil)
		checkIn(4)
		conn.none(t)
	})
}
