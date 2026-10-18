// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package operational

// A port of matter.js packages/protocol/test/session/SessionManagerTest.ts at
// 862ad75f: the device-side session table — session-id allocation, the
// per-peer cap and who it spares, and the reported peer activity. The group
// counter cases live in groups/sessionmanager_parity_matterjs_test.go, the
// local parameter cases in controller/exchange/sessionmanager_parity_matterjs_test.go.

import (
	"errors"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
)

const (
	sessionManagerFabric uint8  = 1
	sessionManagerLocal  uint64 = 0xB0B
)

// sessionManagerOpen is createSecureSession for a CASE session with peer:
// isInitiator picks who established it.
func sessionManagerOpen(t *testing.T, m *Manager, id uint16, peer uint64, peerSessionID uint16, isInitiator bool) *Entry {
	t.Helper()
	open := m.OpenFromSigmaWithID
	if isInitiator {
		open = m.OpenFromSigmaAsInitiatorWithID
	}
	e, err := open(id, sessionManagerFabric, sessionManagerLocal, peer, peerSessionID, nil, closeParityKeys(byte(id)))
	if err != nil {
		t.Fatalf("open session %#x: %v", id, err)
	}
	return e
}

// sessionManagerStamp is `session.timestamp = Timestamp(…)`: the session's
// last traffic at the fake's reading, one millisecond after the previous one.
func sessionManagerStamp(clk *clocktest.Fake, e *Entry) {
	clk.Advance(time.Millisecond)
	e.MarkActiveTx()
}

func sessionManagerClosing(m *Manager, id uint16) bool {
	_, err := m.Get(id)
	return errors.Is(err, ErrSessionNotFound)
}

func TestSessionManagerGetNextAvailableSessionID(t *testing.T) {
	t.Parallel()
	t.Run("verify that existing session ids are skipped", func(t *testing.T) {
		t.Parallel()
		m := NewManager(newMinimalFakeStore())
		first, err := m.AllocateID()
		if err != nil {
			t.Fatal(err)
		}
		// Keep the test simple, as matter.js does: step over the wrap.
		for first >= SessionIDSpace-1 {
			if first, err = m.AllocateID(); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := m.OpenFromPaseWithID(first+1, 0, 0, 0x8d4b, []byte{1}); err != nil {
			t.Fatal(err)
		}
		if next, err := m.AllocateID(); err != nil || next != first+2 {
			t.Fatalf("next = %d, %v; want %d", next, err, first+2)
		}
	})

	t.Run("never allocates 0 when the random seed is 0", func(t *testing.T) {
		t.Parallel()
		m := NewManager(newMinimalFakeStore())
		m.nextID = 0 // nextSessionId = crypto.randomUint16 = 0
		for _, want := range []uint16{1, 2} {
			if got, err := m.AllocateID(); err != nil || got != want {
				t.Fatalf("AllocateID = %d, %v; want %d", got, err, want)
			}
		}
	})
}

func TestSessionManagerMaybeSessionFor(t *testing.T) {
	t.Parallel()
	t.Run("never evicts a session we initiated, even when it is the least recently used", func(t *testing.T) {
		t.Parallel()
		const peer uint64 = 0x9abc
		m := NewManager(newMinimalFakeStore())
		clk := clocktest.New(time.UnixMilli(1_700_000_000_000))
		m.SetClock(clk)
		ours := sessionManagerOpen(t, m, 0x0300, peer, 0x0021, true)
		sessionManagerStamp(clk, ours) // the oldest
		var theirs []uint16
		for i := range uint16(6) {
			e := sessionManagerOpen(t, m, 0x0301+i, peer, 0x0031+i, false)
			sessionManagerStamp(clk, e)
			theirs = append(theirs, e.SessionID)
		}
		if sessionManagerClosing(m, ours.SessionID) {
			t.Fatal("the session we initiated was evicted")
		}
		if !sessionManagerClosing(m, theirs[0]) {
			t.Fatal("the peer's least recently used session survived")
		}
		for _, id := range theirs[1:] {
			if sessionManagerClosing(m, id) {
				t.Fatalf("session %#x evicted", id)
			}
		}
	})

	t.Run("leaves sessions we initiated alone; peer loss reclaims those", func(t *testing.T) {
		t.Parallel()
		const peer uint64 = 0x5678
		m := NewManager(newMinimalFakeStore())
		clk := clocktest.New(time.UnixMilli(1_700_000_000_000))
		m.SetClock(clk)
		for i := range uint16(6) {
			sessionManagerStamp(clk, sessionManagerOpen(t, m, 0x0200+i, peer, 0x0011+i, true))
		}
		for i := range uint16(6) {
			if sessionManagerClosing(m, 0x0200+i) {
				t.Fatalf("session %#x evicted", 0x0200+i)
			}
		}
	})
}

// TestSessionManagerEvictsOnlyForSessionsThePeerEstablished: a session we
// initiate does not run the per-peer check, so it cannot push one of the
// peer's sessions out (SessionManager.ts:272-277, only !isInitiator calls
// #evictExcessSessionsFor). Beside the ported cases; it proves the fix.
func TestSessionManagerEvictsOnlyForSessionsThePeerEstablished(t *testing.T) {
	t.Parallel()
	const peer uint64 = 0x1234
	m := NewManager(newMinimalFakeStore())
	clk := clocktest.New(time.UnixMilli(1_700_000_000_000))
	m.SetClock(clk)
	for i := range uint16(MaxSessionsPerPeer) {
		sessionManagerStamp(clk, sessionManagerOpen(t, m, 0x0100+i, peer, 0x0001+i, false))
	}
	sessionManagerOpen(t, m, 0x0200, peer, 0x0020, true)
	for i := range uint16(MaxSessionsPerPeer) {
		if sessionManagerClosing(m, 0x0100+i) {
			t.Fatalf("adding our own session evicted the peer's %#x", 0x0100+i)
		}
	}
	// The peer's next session still finds the cap full and evicts its oldest.
	sessionManagerOpen(t, m, 0x0300, peer, 0x0030, false)
	if !sessionManagerClosing(m, 0x0100) {
		t.Fatal("the peer's sixth session evicted nothing")
	}
}

func TestSessionManagerWallClockSteps(t *testing.T) {
	t.Parallel()
	t.Run("reports a peer never heard from with a last active time of 0", func(t *testing.T) {
		t.Parallel()
		m := NewManager(newMinimalFakeStore())
		clk := clocktest.New(time.UnixMilli(1_700_000_000_000))
		m.SetClock(clk)
		sessionManagerOpen(t, m, 0x0100, 0x1234, 0x0100, false)
		clk.Set(clk.Now().Add(time.Hour)) // stepWallClock(Hours(1))
		infos := m.Sessions()
		if len(infos) != 1 || !infos[0].LastPeerActivity.IsZero() {
			t.Fatalf("sessions = %+v, want one with a zero last peer activity", infos)
		}
	})
}
