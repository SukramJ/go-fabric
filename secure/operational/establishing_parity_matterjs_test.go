// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package operational

// Ports of matter.js at db91886c for the CASE responder's establishing
// session on the device side (#4882; findings F-b25ed960-23): the
// establishing cases of packages/protocol/test/session/SessionManagerTest.ts
// (describe "session supersession", the rows without supersession, which is
// P2b-B's) and the device-side rows of
// packages/node/test/node/SessionViewsTest.ts that this table answers. The
// controller's table has its own ports in client/exchange.

import (
	"slices"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/clock"
	"thingfabric.dev/go-fabric/clock/clocktest"
	"thingfabric.dev/go-fabric/secure/channel"
)

const establishingPeer uint64 = 0x1234

// openEstablishing is createSecureSession with `establishing: true`: a CASE
// session the peer established, its handshake still running.
func openEstablishing(t *testing.T, m *Manager, id uint16, peer uint64) *Entry {
	t.Helper()
	e, err := m.openFromSigma(id, nil, sessionManagerFabric, sessionManagerLocal, peer, id, nil, closeParityKeys(byte(id)), false, true)
	if err != nil {
		t.Fatalf("open establishing session %#x: %v", id, err)
	}
	return e
}

// newestOf is maybeSessionFor: the session a picker hands out for peer.
func newestOf(m *Manager, peer uint64) (uint16, bool) {
	return m.NewestSessionForPeer(sessionManagerFabric, peer, nil)
}

// listed reports whether the session listing — getActiveSessionInformation —
// holds id.
func listed(m *Manager, id uint16) bool {
	return slices.ContainsFunc(m.Sessions(), func(s SessionInfo) bool { return s.SessionID == id })
}

func newEstablishingManager(t *testing.T) (*Manager, *clocktest.Fake) {
	t.Helper()
	m := NewManager(newMinimalFakeStore())
	clk := clocktest.New(time.UnixMilli(1_700_000_000_000))
	m.SetClock(clk)
	return m, clk
}

func TestSessionManagerEstablishing(t *testing.T) {
	t.Parallel()
	t.Run("does not establish a session that is closing", func(t *testing.T) {
		t.Parallel()
		m, _ := newEstablishingManager(t)
		establishing := openEstablishing(t, m, 0x0101, establishingPeer)
		if err := m.Close(establishing.SessionID); err != nil {
			t.Fatal(err)
		}
		if m.completeEstablishment(establishing) {
			t.Fatal("a closed session completed its establishment")
		}
		m.mu.RLock()
		still := establishing.establishing
		m.mu.RUnlock()
		if !still {
			t.Fatal("the closed session is no longer establishing")
		}
	})
	t.Run("completes establishment once and shows observers an established session", func(t *testing.T) {
		t.Parallel()
		m, _ := newEstablishingManager(t)
		establishing := openEstablishing(t, m, 0x0101, establishingPeer)
		first := m.completeEstablishment(establishing)
		second := m.completeEstablishment(establishing)
		if !first || !second {
			t.Fatalf("completeEstablishment = %v, %v; want true twice", first, second)
		}
		// The observers here are the established views.
		if id, ok := newestOf(m, establishingPeer); !ok || id != establishing.SessionID {
			t.Fatalf("NewestSessionForPeer = %#x, %v; want the established session", id, ok)
		}
		if !listed(m, establishing.SessionID) {
			t.Fatal("the established session is not listed")
		}
	})
	t.Run("finds an establishing session only once it is established", func(t *testing.T) {
		t.Parallel()
		m, clk := newEstablishingManager(t)
		older := sessionManagerOpen(t, m, 0x0100, establishingPeer, 0x0100, false)
		clk.Advance(time.Millisecond)
		establishing := openEstablishing(t, m, 0x0101, establishingPeer)
		establishing.MarkActiveRx() // heard from after older
		if id, ok := newestOf(m, establishingPeer); !ok || id != older.SessionID {
			t.Fatalf("NewestSessionForPeer = %#x, %v; want the older established session", id, ok)
		}
		m.completeEstablishment(establishing)
		if id, ok := newestOf(m, establishingPeer); !ok || id != establishing.SessionID {
			t.Fatalf("NewestSessionForPeer = %#x, %v; want the session now established", id, ok)
		}
	})
	t.Run("lists an establishing session in sessions only once it is established", func(t *testing.T) {
		t.Parallel()
		m, _ := newEstablishingManager(t)
		establishing := openEstablishing(t, m, 0x0101, establishingPeer)
		if listed(m, establishing.SessionID) {
			t.Fatal("the establishing session is listed")
		}
		if got, err := m.Get(establishing.SessionID); err != nil || got != establishing {
			t.Fatalf("Get = %v, %v; want the establishing session (routing sees it)", got, err)
		}
		m.completeEstablishment(establishing)
		if !listed(m, establishing.SessionID) {
			t.Fatal("the established session is not listed")
		}
	})
	t.Run("never lists a session whose handshake fails", func(t *testing.T) {
		t.Parallel()
		m, _ := newEstablishingManager(t)
		var reannounced int
		m.SetReannounceTrigger(func(uint8) { reannounced++ })
		establishing := openEstablishing(t, m, 0x0101, establishingPeer)
		if err := m.Close(establishing.SessionID); err != nil {
			t.Fatal(err)
		}
		if m.completeEstablishment(establishing) {
			t.Fatal("the failed handshake's session completed its establishment")
		}
		if _, err := m.Get(establishing.SessionID); err == nil {
			t.Fatal("the closed session is still in the table")
		}
		if listed(m, establishing.SessionID) {
			t.Fatal("the session whose handshake failed is listed")
		}
		// sessions.deleted never fires for it: DeviceAdvertiser does not
		// react (DeviceAdvertiser.ts:132-138 at db91886c).
		if reannounced != 0 {
			t.Fatalf("the close of a session that never established reannounced %d times", reannounced)
		}
	})
	t.Run("reports an established session that is closing as established", func(t *testing.T) {
		t.Parallel()
		m, _ := newEstablishingManager(t)
		established := sessionManagerOpen(t, m, 0x0100, establishingPeer, 0x0100, false)
		if err := m.Close(established.SessionID); err != nil {
			t.Fatal(err)
		}
		if !m.completeEstablishment(established) {
			t.Fatal("completeEstablishment of an established, closed session = false")
		}
	})
	t.Run("neither counts nor evicts an establishing session for the per-peer cap", func(t *testing.T) {
		t.Parallel()
		m, clk := newEstablishingManager(t)
		ids := make([]uint16, 0, 5)
		for i := range uint16(3) {
			ids = append(ids, sessionManagerStamp2(clk, sessionManagerOpen(t, m, 0x0100+i, establishingPeer, 0x0100+i, false)))
		}
		// establishing.timestamp = 0: the least recently used of all.
		establishing := openEstablishing(t, m, 0x0110, establishingPeer)
		for i := range uint16(2) {
			ids = append(ids, sessionManagerStamp2(clk, sessionManagerOpen(t, m, 0x0103+i, establishingPeer, 0x0103+i, false)))
		}
		for _, id := range append(ids, establishing.SessionID) {
			if sessionManagerClosing(m, id) {
				t.Fatalf("session %#x was evicted", id)
			}
		}
	})
	// The per-peer cap runs when a session becomes established, not when it
	// is registered (SessionManager.ts:757-769 at db91886c).
	t.Run("evicts the least recently used session when an establishing session is established", func(t *testing.T) {
		t.Parallel()
		m, clk := newEstablishingManager(t)
		for i := range uint16(MaxSessionsPerPeer) {
			sessionManagerStamp(clk, sessionManagerOpen(t, m, 0x0100+i, establishingPeer, 0x0100+i, false))
		}
		establishing := openEstablishing(t, m, 0x0110, establishingPeer)
		if sessionManagerClosing(m, 0x0100) {
			t.Fatal("the registration of an establishing session evicted a session")
		}
		m.completeEstablishment(establishing)
		if !sessionManagerClosing(m, 0x0100) {
			t.Fatal("the establishment evicted nothing")
		}
		for i := range uint16(MaxSessionsPerPeer - 1) {
			if sessionManagerClosing(m, 0x0101+i) {
				t.Fatalf("session %#x was evicted too", 0x0101+i)
			}
		}
		if sessionManagerClosing(m, establishing.SessionID) {
			t.Fatal("the session that prompted the check was evicted")
		}
	})
}

// sessionManagerStamp2 is [sessionManagerStamp] returning the session's id.
func sessionManagerStamp2(clk *clocktest.Fake, e *Entry) uint16 {
	sessionManagerStamp(clk, e)
	return e.SessionID
}

// sessionViews is SessionViewsTest.ts's fixture on this table: two CASE
// sessions of one peer we respond to — establishing, whose handshake still
// runs, created first and least recently used, and established.
func sessionViews(t *testing.T) (m *Manager, clk *clocktest.Fake, establishing, established *Entry) {
	t.Helper()
	m, clk = newEstablishingManager(t)
	establishing = openEstablishing(t, m, 0x7000, establishingPeer)
	clk.Advance(time.Millisecond)
	established = sessionManagerOpen(t, m, 0x7001, establishingPeer, 0x7001, false)
	sessionManagerStamp(clk, established)
	return m, clk, establishing, established
}

// TestSessionViews ports the device-side rows of SessionViewsTest.ts at
// db91886c that this table answers: which consumer sees the establishing
// session, the established one or both.
func TestSessionViews(t *testing.T) {
	t.Parallel()
	// gone reports which of the pair left the table.
	gone := func(m *Manager, pair ...*Entry) []*Entry {
		var out []*Entry
		for _, e := range pair {
			if sessionManagerClosing(m, e.SessionID) {
				out = append(out, e)
			}
		}
		return out
	}
	expect := func(t *testing.T, got []*Entry, want ...*Entry) {
		t.Helper()
		if !slices.Equal(got, want) {
			t.Fatalf("seen %v, want %v", ids(got), ids(want))
		}
	}
	t.Run("SessionManager.sessionsForFabricIndex() sees both sessions", func(t *testing.T) {
		t.Parallel()
		// Fabric teardown reads the registered sessions.
		m, _, establishing, established := sessionViews(t)
		m.CloseFabric(sessionManagerFabric)
		expect(t, gone(m, establishing, established), establishing, established)
	})
	t.Run("SessionManager.getSession(), which routes received messages sees both sessions", func(t *testing.T) {
		t.Parallel()
		m, _, establishing, established := sessionViews(t)
		var seen []*Entry
		for _, e := range []*Entry{establishing, established} {
			if got, err := m.Get(e.SessionID); err == nil && got == e {
				seen = append(seen, e)
			}
		}
		expect(t, seen, establishing, established)
	})
	t.Run("SessionManager.closeAllSessions() sees both sessions", func(t *testing.T) {
		t.Parallel()
		m, _, establishing, established := sessionViews(t)
		m.CloseAllGraceful(time.Time{})
		expect(t, gone(m, establishing, established), establishing, established)
	})
	t.Run("SessionManager.findOldestInactiveSession() picks the oldest of both sessions, which is the establishing one", func(t *testing.T) {
		t.Parallel()
		// The id-space eviction's choice (AllocateStake at a full table).
		m, _, establishing, _ := sessionViews(t)
		m.mu.Lock()
		oldest := m.leastRecentlyActiveLocked()
		m.mu.Unlock()
		if oldest != establishing {
			t.Fatalf("picked %v, want the establishing session", ids([]*Entry{oldest}))
		}
	})
	t.Run("SessionManager.handlePeerLoss() sees both sessions", func(t *testing.T) {
		t.Parallel()
		m, clk, establishing, established := sessionViews(t)
		clk.Advance(time.Millisecond)
		m.ClosePeerLostSince(sessionManagerFabric, establishingPeer, clock.MonotonicOf(clk))
		expect(t, gone(m, establishing, established), establishing, established)
	})
	t.Run("SessionManager.getActiveSessionInformation() sees the established session", func(t *testing.T) {
		t.Parallel()
		m, _, establishing, established := sessionViews(t)
		var seen []*Entry
		for _, e := range []*Entry{establishing, established} {
			if listed(m, e.SessionID) {
				seen = append(seen, e)
			}
		}
		expect(t, seen, established)
	})
	t.Run("eviction of the least recently used session beyond the per-peer cap sees the established session", func(t *testing.T) {
		t.Parallel()
		m, clk, establishing, established := sessionViews(t)
		for i := range uint16(5) {
			sessionManagerStamp(clk, sessionManagerOpen(t, m, 0x7100+i, establishingPeer, 0x7100+i, false))
		}
		expect(t, gone(m, establishing, established), established)
	})
	t.Run("ExchangeManager sending CloseSession on a graceful close sees both sessions", func(t *testing.T) {
		t.Parallel()
		m, _, establishing, established := sessionViews(t)
		var told []uint16
		m.SetGracefulCloseNotifier(func(sessionID uint16, _ *channel.Session) { told = append(told, sessionID) })
		m.CloseAllGraceful(time.Time{})
		slices.Sort(told)
		if !slices.Equal(told, []uint16{establishing.SessionID, established.SessionID}) {
			t.Fatalf("CloseSession for %#x, want both sessions", told)
		}
	})
}

func ids(entries []*Entry) []uint16 {
	out := make([]uint16, 0, len(entries))
	for _, e := range entries {
		if e == nil {
			out = append(out, 0)
			continue
		}
		out = append(out, e.SessionID)
	}
	return out
}
