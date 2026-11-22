// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package operational

// A port of matter.js packages/protocol/test/fabric/FabricTest.ts at
// b25ed960, the rows of Fabric.hasSessionForPeer with a session still
// establishing (:283-305). Go has no Fabric session set: the question is
// the session table's (hasSessionForPeer, behind the reannounce trigger;
// Fabric.ts:422-429 skips a session still establishing).

import "testing"

func TestFabricHasSessionForPeer(t *testing.T) {
	t.Parallel()
	t.Run("has no session for the peer when its established session closes while another one is establishing", func(t *testing.T) {
		t.Parallel()
		m, _ := newEstablishingManager(t)
		established := sessionManagerOpen(t, m, 0x0100, establishingPeer, 0x0100, false)
		establishing := openEstablishing(t, m, 0x0101, establishingPeer)
		if !m.hasSessionForPeer(sessionManagerFabric, establishingPeer) {
			t.Fatal("no session for the peer while one is established")
		}
		if err := m.Close(established.SessionID); err != nil {
			t.Fatal(err)
		}
		if m.hasSessionForPeer(sessionManagerFabric, establishingPeer) {
			t.Fatal("a session for the peer while its only other session is establishing")
		}
		m.completeEstablishment(establishing)
		if !m.hasSessionForPeer(sessionManagerFabric, establishingPeer) {
			t.Fatal("no session for the peer once the establishing one is established")
		}
	})
	t.Run("has no session for the peer while its only session is establishing", func(t *testing.T) {
		t.Parallel()
		m, _ := newEstablishingManager(t)
		establishing := openEstablishing(t, m, 0x0101, establishingPeer)
		if m.hasSessionForPeer(sessionManagerFabric, establishingPeer) {
			t.Fatal("a session for the peer while its only session is establishing")
		}
		if err := m.Close(establishing.SessionID); err != nil {
			t.Fatal(err)
		}
		if m.hasSessionForPeer(sessionManagerFabric, establishingPeer) {
			t.Fatal("a session for the peer after its establishing session closed")
		}
	})
}

// TestPeerIndexDropsClosedSessions: the supersession check's peer index
// forgets a session as it leaves the table (F-b25ed960-41).
func TestPeerIndexDropsClosedSessions(t *testing.T) {
	t.Parallel()
	m, _ := newEstablishingManager(t)
	e := sessionManagerOpen(t, m, 0x0100, establishingPeer, 0x0100, false)
	if err := m.Close(e.SessionID); err != nil {
		t.Fatal(err)
	}
	m.mu.RLock()
	left := len(m.byPeer)
	m.mu.RUnlock()
	if left != 0 {
		t.Fatalf("peer index holds %d peers after their only session closed", left)
	}
}
