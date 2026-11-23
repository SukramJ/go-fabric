// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"net"
	"testing"
	"time"
)

// activityRegistry orders a peer's sessions as the operational manager's
// NewestSessionForPeer does: newest peer activity first.
type activityRegistry struct {
	newestFirst []uint16
}

func (activityRegistry) Close(uint16) error             { return nil }
func (activityRegistry) CloseAllGraceful(time.Time) int { return 0 }
func (r activityRegistry) NewestSessionForPeer(_ uint8, _ uint64, usable func(uint16) bool) (uint16, bool) {
	for _, id := range r.newestFirst {
		if usable(id) {
			return id, true
		}
	}
	return 0, false
}

// TestPeerSessionAddressIsTheNewestSessions: when the peer holds several
// sessions, the Check-In address is the UDP address of the session it was
// active on last, whatever the order of the address table; a newer session
// without a UDP address (a TCP route) hides no older UDP one (review II,
// R2-C2-par-0353).
func TestPeerSessionAddressIsTheNewestSessions(t *testing.T) {
	t.Parallel()
	const node = 0x1122_3344
	newest := &net.UDPAddr{IP: net.IPv6loopback, Port: 6000}
	for range 8 {
		table := sessionTable{}
		b := &Bridge{}
		order := []uint16{40} // newest: a TCP route
		table[40] = tableSession{1, node}
		b.sessionPeerAddrs.Store(uint16(40), &net.TCPAddr{IP: net.IPv6loopback, Port: 5540})
		order = append(order, 41)
		table[41] = tableSession{1, node}
		b.sessionPeerAddrs.Store(uint16(41), newest)
		for id := uint16(20); id < 36; id++ { // older sessions, addresses the peer left
			table[id] = tableSession{1, node}
			b.sessionPeerAddrs.Store(id, &net.UDPAddr{IP: net.IPv6loopback, Port: 7000 + int(id)})
			order = append(order, id)
		}
		b.sessions = table
		b.sessionRegistry = activityRegistry{newestFirst: order}
		if got := b.PeerSessionAddress(1, node); got != newest {
			t.Fatalf("Check-In address %v, want the newest session's %v", got, newest)
		}
	}
}
