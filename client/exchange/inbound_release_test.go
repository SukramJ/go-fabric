// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package exchange

import (
	"slices"
	"sync"
	"testing"
	"testing/synctest"

	"thingfabric.dev/go-fabric/transport/message"
	"thingfabric.dev/go-fabric/transport/mrp"
)

// TestInitiatingMessageOutlivesTheReleaseOfTheSessionItFound holds
// matter.js ExchangeManager.ts:244-256 and :307-327 at b25ed960: a peer's
// initiating message either lands on the inbound unsecured session of
// the peer's previous message before that session is released, or finds
// no session and gets a new one — it is never dropped. The second
// Check-In finds the first one's session; the first one's handler ends
// and releases that session before the second is delivered on it.
func TestInitiatingMessageOutlivesTheReleaseOfTheSessionItFound(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		m, conn, _ := newTestManager(t, Config{})
		checkIn := func(counter uint32) {
			hdr := message.Header{MessageCounter: counter, HasSourceNodeID: true, SourceNodeID: 0xBBBB}
			proto := message.ProtocolHeader{Initiator: true, Opcode: mrp.SCOpcodeIcdCheckIn, ExchangeID: 0x5000 + uint16(counter), ProtocolID: mrp.SecureChannelProtocolID}
			m.HandleDatagram(append(append(hdr.Marshal(), proto.Marshal()...), byte(counter)), peerAddr)
		}
		release := make(chan struct{})
		var mu sync.Mutex
		var got []byte
		m.SetCheckInReceiver(func(payload []byte) bool {
			if payload[0] == 2 {
				<-release
			}
			mu.Lock()
			got = append(got, payload[0])
			mu.Unlock()
			return true
		})
		var found *Session
		m.afterUnsecuredLookup = func(s *Session) {
			if found != nil {
				return
			}
			found = s
			// The first Check-In's handler ends now, and with it the
			// session: released between the lookup and the delivery.
			close(release)
			synctest.Wait()
		}

		checkIn(2)
		synctest.Wait()
		checkIn(3)
		synctest.Wait()

		if found == nil {
			t.Fatal("the second Check-In did not find the first one's session")
		}
		if !found.IsClosed() {
			t.Fatal("the first Check-In's session was not released")
		}
		mu.Lock()
		defer mu.Unlock()
		if !slices.Equal(got, []byte{2, 3}) {
			t.Fatalf("Check-Ins received %v, want [2 3]", got)
		}
		conn.none(t)
		waitNoInbound(t, m)
	})
}
