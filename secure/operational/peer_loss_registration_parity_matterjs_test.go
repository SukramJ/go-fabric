// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package operational

// A port of matter.js packages/protocol/test/session/SessionManagerTest.ts
// :1128-1235 at b25ed960, describe "peer loss by registration order", on the
// device side's session table. matter.js's handlePeerShutdown(peer, keep)
// and sessionsRegisteredSince(session) have no device-side subject (the
// device never shuts a peer down by a session it keeps); their cases run by
// meaning on ClosePeerLostBefore with the kept session's registration
// position as the cutoff, the cutoff handlePeerShutdown derives
// (SessionManager.ts:733-734). The controller's port is
// client/exchange:TestSessionManagerPeerLossByRegistrationOrder.

import (
	"testing"
	"time"
)

const registrationPeer uint64 = 0x1234

func TestSessionManagerPeerLossByRegistrationOrder(t *testing.T) {
	t.Parallel()
	t.Run("keeps the session given to peer shutdown and every session registered after it", func(t *testing.T) {
		t.Parallel()
		m, _ := newEstablishingManager(t)
		older := sessionManagerOpen(t, m, 0x0100, registrationPeer, 0x0100, false)
		kept := sessionManagerOpen(t, m, 0x0200, registrationPeer, 0x0200, false)
		sessionManagerOpen(t, m, 0x0300, registrationPeer, 0x0300, false)
		if !older.CreatedAt().Equal(kept.CreatedAt()) {
			t.Fatal("the clock moved")
		}
		if n := m.ClosePeerLostBefore(sessionManagerFabric, registrationPeer, kept.registration); n != 1 {
			t.Fatalf("closed %d, want 1", n)
		}
		if !sessionManagerClosing(m, 0x0100) || sessionManagerClosing(m, 0x0200) || sessionManagerClosing(m, 0x0300) {
			t.Fatalf("closed: older %v kept %v newer %v", sessionManagerClosing(m, 0x0100),
				sessionManagerClosing(m, 0x0200), sessionManagerClosing(m, 0x0300))
		}
	})
	t.Run("keeps a session that registers after the given registration count although it was created before", func(t *testing.T) {
		t.Parallel()
		// A device-side session is built and registered in one call; the
		// case holds by meaning with a session whose creation time reads
		// before the moment the count was taken (the wall clock stepped
		// back), registered after it: its creation time does not decide.
		m, clk := newEstablishingManager(t)
		lost := sessionManagerOpen(t, m, 0x0100, registrationPeer, 0x0100, false)
		registeredBefore := m.RegistrationCount()
		countTakenAt := clk.Now()
		clk.StepWall(-time.Hour)
		clk.Advance(time.Second)
		later := openEstablishing(t, m, 0x0200, registrationPeer)
		if !later.CreatedAt().Before(countTakenAt) || !later.CreatedAt().Before(lost.CreatedAt()) {
			t.Fatal("the later session does not read as created before")
		}
		if n := m.ClosePeerLostBefore(sessionManagerFabric, registrationPeer, registeredBefore); n != 1 {
			t.Fatalf("closed %d, want 1", n)
		}
		if !sessionManagerClosing(m, 0x0100) || sessionManagerClosing(m, 0x0200) {
			t.Fatalf("closed: lost %v later %v", sessionManagerClosing(m, 0x0100), sessionManagerClosing(m, 0x0200))
		}
	})
	t.Run("keeps a session registered while the peer loss runs", func(t *testing.T) {
		t.Parallel()
		m, _ := newEstablishingManager(t)
		var registeredDuringLoss *Entry
		m.SetOnSessionClose(func(uint16) {
			if registeredDuringLoss == nil {
				registeredDuringLoss = sessionManagerOpen(t, m, 0x0200, registrationPeer, 0x0200, false)
			}
		})
		sessionManagerOpen(t, m, 0x0100, registrationPeer, 0x0100, false)
		if n := m.ClosePeerLostBefore(sessionManagerFabric, registrationPeer, m.RegistrationCount()); n != 1 {
			t.Fatalf("closed %d, want 1", n)
		}
		if !sessionManagerClosing(m, 0x0100) || registeredDuringLoss == nil || sessionManagerClosing(m, 0x0200) {
			t.Fatalf("lost %v; registered during the loss %v", sessionManagerClosing(m, 0x0100), registeredDuringLoss)
		}
	})
	t.Run("keeps the sessions created at or after a deprecated timestamp given to peer shutdown", func(t *testing.T) {
		t.Parallel()
		m, clk := newEstablishingManager(t)
		sessionManagerOpen(t, m, 0x0100, registrationPeer, 0x0100, false)
		clk.Advance(time.Second)
		kept := sessionManagerOpen(t, m, 0x0200, registrationPeer, 0x0200, false)
		if n := m.ClosePeerLost(sessionManagerFabric, registrationPeer, kept.CreatedAt()); n != 1 {
			t.Fatalf("closed %d, want 1", n)
		}
		if !sessionManagerClosing(m, 0x0100) || sessionManagerClosing(m, 0x0200) {
			t.Fatalf("closed: older %v kept %v", sessionManagerClosing(m, 0x0100), sessionManagerClosing(m, 0x0200))
		}
	})
	t.Run("returns the sessions of the peer registered at or after a session", func(t *testing.T) {
		t.Parallel()
		// sessionsRegisteredSince by meaning: the sessions a cutoff at a
		// session's position keeps. An establishing session registers at
		// its install, before its establishment.
		for _, tc := range []struct {
			since uint16
			kept  []uint16
		}{
			{0x0200, []uint16{0x0200, 0x0300, 0x0400}},
			{0x0300, []uint16{0x0300, 0x0400}},
			{0x0100, []uint16{0x0100, 0x0200, 0x0300, 0x0400}},
		} {
			m, _ := newEstablishingManager(t)
			entries := map[uint16]*Entry{
				0x0100: sessionManagerOpen(t, m, 0x0100, registrationPeer, 0x0100, false),
				0x0200: sessionManagerOpen(t, m, 0x0200, registrationPeer, 0x0200, false),
				0x0300: openEstablishing(t, m, 0x0300, registrationPeer),
				0x0400: sessionManagerOpen(t, m, 0x0400, registrationPeer, 0x0400, false),
			}
			if !m.completeEstablishment(entries[0x0300]) {
				t.Fatal("the establishing session did not complete")
			}
			m.ClosePeerLostBefore(sessionManagerFabric, registrationPeer, entries[tc.since].registration)
			var kept []uint16
			for _, id := range []uint16{0x0100, 0x0200, 0x0300, 0x0400} {
				if !sessionManagerClosing(m, id) {
					kept = append(kept, id)
				}
			}
			if len(kept) != len(tc.kept) {
				t.Fatalf("since %#x: kept %#x, want %#x", tc.since, kept, tc.kept)
			}
			for i := range kept {
				if kept[i] != tc.kept[i] {
					t.Fatalf("since %#x: kept %#x, want %#x", tc.since, kept, tc.kept)
				}
			}
		}
	})
	t.Run("treats a session that never registered as keeping nothing", func(t *testing.T) {
		t.Parallel()
		// A session that never registered gives no cutoff; the caller's
		// fallback is the registration count now, as handlePeerShutdown's
		// (SessionManager.ts:733-734): every registered session goes. A
		// placeholder the allocator staked is the device side's session
		// that never registered, and the sweep passes it by.
		m, _ := newEstablishingManager(t)
		sessionManagerOpen(t, m, 0x0100, registrationPeer, 0x0100, false)
		stake, err := m.AllocateStake()
		if err != nil {
			t.Fatal(err)
		}
		if n := m.ClosePeerLostBefore(sessionManagerFabric, registrationPeer, m.RegistrationCount()); n != 1 {
			t.Fatalf("closed %d, want 1", n)
		}
		if !sessionManagerClosing(m, 0x0100) {
			t.Fatal("the registered session was kept")
		}
		if !m.stakes(placeholderRef{id: stake.ID, e: stake.e}) {
			t.Fatal("the sweep took the placeholder")
		}
	})
}

// TestRegistrationAtEveryKeyedInsert pins the registration point: each of
// the four keyed inserts (OpenFromSigma, OpenFromPase and the two through
// installEntryLocked) takes the next position, a placeholder does not.
func TestRegistrationAtEveryKeyedInsert(t *testing.T) {
	t.Parallel()
	m, _ := newEstablishingManager(t)
	if c := m.RegistrationCount(); c != 0 {
		t.Fatalf("count = %d before any session", c)
	}
	sigma, err := m.OpenFromSigma(sessionManagerFabric, sessionManagerLocal, registrationPeer, closeParityKeys(1))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.AllocateStake(); err != nil {
		t.Fatal(err)
	}
	pase, err := m.OpenFromPase(0, 0, 0x10, []byte{1})
	if err != nil {
		t.Fatal(err)
	}
	sigmaID := sessionManagerOpen(t, m, 0x0300, registrationPeer, 0x0300, false)
	paseID, err := m.OpenFromPaseWithID(0x0400, 0, 0, 0x11, []byte{2})
	if err != nil {
		t.Fatal(err)
	}
	for i, e := range []*Entry{sigma, pase, sigmaID, paseID} {
		if e.registration != uint64(i) {
			t.Fatalf("session %d registered at %d", i, e.registration)
		}
	}
	if c := m.RegistrationCount(); c != 4 {
		t.Fatalf("count = %d, want 4", c)
	}
	if n := m.ClosePeerLostBefore(sessionManagerFabric, registrationPeer, 4); n != 2 {
		t.Fatalf("closed %d, want the two CASE sessions", n)
	}
	if sigma.registration != 0 || sigmaID.registration != 2 {
		t.Fatal("a position moved on removal")
	}
}
