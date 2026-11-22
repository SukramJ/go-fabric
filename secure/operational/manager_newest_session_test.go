// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package operational

import (
	"testing"
	"time"

	"thingfabric.dev/go-fabric/clock/clocktest"
)

// TestNewestSessionForPeer_OrdersByLastPeerActivity pins the order a
// peer's sessions are offered for reuse in (review II, R2-P5-port-07,
// F-db021da0-34): the session the peer sent on last wins, not the highest
// id, and this node's own sends do not move it. matter.js newestSession
// (Peer.ts:582-602 at db021da0) takes the highest activeTimestamp, which
// moves on a received message only (Session.ts:142-147).
func TestNewestSessionForPeer_OrdersByLastPeerActivity(t *testing.T) {
	t.Parallel()
	clk := clocktest.New(time.UnixMilli(1_700_000_000_000))
	m := NewManager(newMinimalFakeStore())
	m.SetClock(clk)
	const (
		fabric = uint8(1)
		local  = uint64(0xB0B)
		peer   = uint64(0xA11CE)
	)
	older, err := m.OpenFromSigma(fabric, local, peer, lifecycleKeys(0x10))
	if err != nil {
		t.Fatalf("OpenFromSigma: %v", err)
	}
	clk.Advance(time.Second)
	newer, err := m.OpenFromSigma(fabric, local, peer, lifecycleKeys(0x30))
	if err != nil {
		t.Fatalf("OpenFromSigma: %v", err)
	}
	if older.SessionID == newer.SessionID {
		t.Fatal("two sessions share an id")
	}
	// Opened last, newer is the newest so far.
	if id, ok := m.NewestSessionForPeer(fabric, peer, nil); !ok || id != newer.SessionID {
		t.Fatalf("NewestSessionForPeer = %d, %v; want the session opened last (%d)", id, ok, newer.SessionID)
	}
	// The peer sends on the older session: that one is now the newest,
	// whatever its id.
	clk.Advance(time.Second)
	older.MarkActiveRx()
	if id, ok := m.NewestSessionForPeer(fabric, peer, nil); !ok || id != older.SessionID {
		t.Fatalf("NewestSessionForPeer = %d, %v; want the session the peer sent on last (%d)", id, ok, older.SessionID)
	}
	if got, want := older.LastPeerActivity(), clk.Now(); !got.Equal(want) {
		t.Errorf("LastPeerActivity = %v, want %v", got, want)
	}
	// Our own send on newer does not move the peer's activity.
	clk.Advance(time.Second)
	newer.MarkActiveTx()
	if id, _ := m.NewestSessionForPeer(fabric, peer, nil); id != older.SessionID {
		t.Fatalf("a send of ours moved the order: NewestSessionForPeer = %d, want %d", id, older.SessionID)
	}
	// usable filters: refusing the newest yields the next.
	id, ok := m.NewestSessionForPeer(fabric, peer, func(id uint16) bool { return id != older.SessionID })
	if !ok || id != newer.SessionID {
		t.Fatalf("NewestSessionForPeer with the newest refused = %d, %v; want %d", id, ok, newer.SessionID)
	}
	if _, ok := m.NewestSessionForPeer(fabric, peer, func(uint16) bool { return false }); ok {
		t.Error("NewestSessionForPeer reported a session usable refused")
	}
	if _, ok := m.NewestSessionForPeer(fabric+1, peer, nil); ok {
		t.Error("NewestSessionForPeer crossed fabrics")
	}
	// usable is asked newest first and not again after its first true,
	// whose id is the one returned (bridge livePeerLink keeps what that
	// call found; review II, phase 5b).
	var asked []uint16
	id, ok = m.NewestSessionForPeer(fabric, peer, func(id uint16) bool {
		asked = append(asked, id)
		return true
	})
	if !ok || id != older.SessionID || len(asked) != 1 || asked[0] != older.SessionID {
		t.Fatalf("NewestSessionForPeer = %d, %v after asking %v; want %d after asking it alone", id, ok, asked, older.SessionID)
	}
}
