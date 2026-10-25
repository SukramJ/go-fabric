// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

// A port of matter.js packages/protocol/test/securechannel/SecureChannelProtocolTest.ts
// at 862ad75f. matter.js hands a Check-In to SecureChannelProtocol.onNewExchange,
// which sends nothing, closes the exchange and offers the payload to every
// fabric's FabricIcd. Go splits the path: the exchange manager's half —
// nothing sent, the exchange and the inbound session closed — is held by
// client/exchange's TestCheckInReachesTheReceiver; this port drives the
// registry's half, receiveCheckIn over the ICD peer tables of its fabrics.

import (
	"log/slog"
	"reflect"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/icd"
	"github.com/SukramJ/go-fabric/icd/checkin"
)

func TestSecureChannelCheckInRoutingParity(t *testing.T) {
	t.Parallel()
	key := []byte{0xd9, 0x0e, 0x13, 0x18, 0x0d, 0x00, 0xba, 0xad, 0xd2, 0x0c, 0xf5, 0xed, 0x49, 0x13, 0xd3, 0xff}
	registry := func(tables map[uint8]*icd.PeerTable) *Registry {
		return &Registry{log: slog.New(slog.DiscardHandler), icdTables: tables}
	}
	t.Run("SecureChannelProtocol — ICD Check-In routing", func(t *testing.T) {
		t.Parallel()
		t.Run("routes a check-in to the owning fabric peer", func(t *testing.T) {
			t.Parallel()
			table := icd.NewPeerTable(clocktest.New(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)), nil)
			var received []icd.ReceivedCheckIn
			table.AddPeer(icd.Peer{PeerNodeID: 11, Key: key, CounterStart: 10}, func(c icd.ReceivedCheckIn) { received = append(received, c) })
			payload, err := checkin.EncodeICD(key, 12, 5000)
			if err != nil {
				t.Fatal(err)
			}
			if !registry(map[uint8]*icd.PeerTable{1: table}).receiveCheckIn(payload) {
				t.Fatal("not routed")
			}
			if want := []icd.ReceivedCheckIn{{PeerNodeID: 11, Counter: 12, ActiveModeThreshold: 5000}}; !reflect.DeepEqual(received, want) {
				t.Fatalf("received %+v", received)
			}
		})
		t.Run("silently drops a check-in matching no peer", func(t *testing.T) {
			t.Parallel()
			payload, err := checkin.EncodeICD(key, 12, 5000)
			if err != nil {
				t.Fatal(err)
			}
			// A fabric whose ICD state was never touched.
			r := registry(map[uint8]*icd.PeerTable{1: icd.NewPeerTable(clocktest.New(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)), nil)})
			if r.receiveCheckIn(payload) {
				t.Fatal("routed")
			}
		})
	})
}
