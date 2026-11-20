// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package operational

// A port of matter.js packages/protocol/test/session/SessionManagerTest.ts at
// b25ed960, describe "session supersession" (:435-660): which established
// sessions a newly established one supersedes, and that supersession comes
// before the per-peer eviction. The establishing rows without supersession
// are in establishing_parity_matterjs_test.go.

import (
	"slices"
	"sync"
	"testing"
)

const (
	catA uint32 = 0x0001_0001
	catB uint32 = 0x0002_0001
)

// supersessionLog records the hook's calls: for each older session id the
// newer session ids that superseded it, in order, and every event (as
// "superseded <id>" / "closing <id>") in the order it fired.
type supersessionLog struct {
	mu     sync.Mutex
	newer  map[uint16][]uint16
	events []string
}

func recordSupersession(m *Manager) *supersessionLog {
	l := &supersessionLog{newer: map[uint16][]uint16{}}
	m.SetOnSessionSuperseded(func(olderID, newerID uint16) {
		l.mu.Lock()
		l.newer[olderID] = append(l.newer[olderID], newerID)
		l.events = append(l.events, "superseded")
		l.mu.Unlock()
	})
	return l
}

func (l *supersessionLog) of(older *Entry) []uint16 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.newer[older.SessionID])
}

type supersessionOpts struct {
	peer         uint64
	cats         []uint32
	initiator    bool
	establishing bool
}

// supersessionOpen is the test's session(): createSecureSession of a CASE
// session with the next id.
func supersessionOpen(t *testing.T, m *Manager, next *uint16, o supersessionOpts) *Entry {
	t.Helper()
	id := *next
	*next++
	peer := o.peer
	if peer == 0 {
		peer = establishingPeer
	}
	e, err := m.openFromSigma(id, nil, sessionManagerFabric, sessionManagerLocal, peer, id, o.cats, closeParityKeys(byte(id)), o.initiator, o.establishing)
	if err != nil {
		t.Fatalf("open session %#x: %v", id, err)
	}
	return e
}

func expectSupersededBy(t *testing.T, got []uint16, want ...*Entry) {
	t.Helper()
	ids := make([]uint16, 0, len(want))
	for _, e := range want {
		ids = append(ids, e.SessionID)
	}
	if !slices.Equal(got, ids) {
		t.Fatalf("superseded by %#x; want %#x", got, ids)
	}
}

func TestSessionManagerSupersession(t *testing.T) {
	t.Parallel()
	setup := func(t *testing.T) (*Manager, *supersessionLog, *uint16) {
		t.Helper()
		m, _ := newEstablishingManager(t)
		next := uint16(0x0100)
		return m, recordSupersession(m), &next
	}
	t.Run("supersedes an older session with the same peer and CATs in any order", func(t *testing.T) {
		t.Parallel()
		m, log, next := setup(t)
		older := supersessionOpen(t, m, next, supersessionOpts{cats: []uint32{catA, catB}})
		newer := supersessionOpen(t, m, next, supersessionOpts{cats: []uint32{catB, catA, catB}})
		expectSupersededBy(t, log.of(older), newer)
	})
	t.Run("does not supersede a session with other CATs", func(t *testing.T) {
		t.Parallel()
		m, log, next := setup(t)
		older := supersessionOpen(t, m, next, supersessionOpts{cats: []uint32{catA}})
		supersessionOpen(t, m, next, supersessionOpts{cats: []uint32{catA, catB}})
		supersessionOpen(t, m, next, supersessionOpts{cats: []uint32{catB}})
		supersessionOpen(t, m, next, supersessionOpts{})
		expectSupersededBy(t, log.of(older))
	})
	t.Run("supersedes a session of the other role with equal CATs", func(t *testing.T) {
		t.Parallel()
		m, log, next := setup(t)
		initiated := supersessionOpen(t, m, next, supersessionOpts{initiator: true, cats: []uint32{catA}})
		responded := supersessionOpen(t, m, next, supersessionOpts{cats: []uint32{catA}})
		initiatedAgain := supersessionOpen(t, m, next, supersessionOpts{initiator: true, cats: []uint32{catA}})
		expectSupersededBy(t, log.of(initiated), responded, initiatedAgain)
		expectSupersededBy(t, log.of(responded), initiatedAgain)
	})
	t.Run("does not supersede a session of the other role with other CATs", func(t *testing.T) {
		t.Parallel()
		m, log, next := setup(t)
		initiated := supersessionOpen(t, m, next, supersessionOpts{initiator: true, cats: []uint32{catA}})
		responded := supersessionOpen(t, m, next, supersessionOpts{cats: []uint32{catB}})
		supersessionOpen(t, m, next, supersessionOpts{initiator: true, cats: []uint32{catA, catB}})
		expectSupersededBy(t, log.of(initiated))
		expectSupersededBy(t, log.of(responded))
	})
	t.Run("does not supersede a session with another peer", func(t *testing.T) {
		t.Parallel()
		m, log, next := setup(t)
		older := supersessionOpen(t, m, next, supersessionOpts{})
		supersessionOpen(t, m, next, supersessionOpts{peer: 0x5678})
		expectSupersededBy(t, log.of(older))
	})
	t.Run("does not supersede a session with the same peer on another fabric", func(t *testing.T) {
		t.Parallel()
		// Not a matter.js row: peerIs compares the PeerAddress, fabric
		// index and node id (SessionManager.ts:1242-1244 at b25ed960); the
		// test above varies only the node id.
		m, log, _ := setup(t)
		older, err := m.openFromSigma(0x0100, nil, sessionManagerFabric, sessionManagerLocal, establishingPeer, 1, nil, closeParityKeys(1), false, false)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := m.openFromSigma(0x0101, nil, sessionManagerFabric+1, sessionManagerLocal, establishingPeer, 2, nil, closeParityKeys(2), false, false); err != nil {
			t.Fatal(err)
		}
		expectSupersededBy(t, log.of(older))
	})
	t.Run("does not supersede a PASE session", func(t *testing.T) {
		t.Parallel()
		// matter.js marks PASE by the unspecified peer node id; here a PASE
		// session is one opened from the PASE handshake. Neither a newer
		// PASE session nor a newer CASE session with the PASE session's
		// peer node id supersedes it.
		m, log, next := setup(t)
		older, err := m.OpenFromPase(sessionManagerLocal, establishingPeer, 1, []byte("shared-secret-1"))
		if err != nil {
			t.Fatal(err)
		}
		m.AdoptFabricIndex(older.SessionID, sessionManagerFabric)
		if _, err := m.OpenFromPase(sessionManagerLocal, establishingPeer, 2, []byte("shared-secret-2")); err != nil {
			t.Fatal(err)
		}
		supersessionOpen(t, m, next, supersessionOpts{})
		expectSupersededBy(t, log.of(older))
	})
	t.Run("does not supersede a closing session", func(t *testing.T) {
		t.Parallel()
		// This table has no closing state: a session that is closing is one
		// that no longer holds its id.
		m, log, next := setup(t)
		older := supersessionOpen(t, m, next, supersessionOpts{})
		if err := m.Close(older.SessionID); err != nil {
			t.Fatal(err)
		}
		supersessionOpen(t, m, next, supersessionOpts{})
		expectSupersededBy(t, log.of(older))
	})
	t.Run("does not supersede a session that is still establishing", func(t *testing.T) {
		t.Parallel()
		m, log, next := setup(t)
		establishing := supersessionOpen(t, m, next, supersessionOpts{establishing: true})
		supersessionOpen(t, m, next, supersessionOpts{})
		expectSupersededBy(t, log.of(establishing))
	})
	t.Run("supersedes nothing while establishing and the older sessions once established", func(t *testing.T) {
		t.Parallel()
		m, log, next := setup(t)
		older := supersessionOpen(t, m, next, supersessionOpts{})
		newer := supersessionOpen(t, m, next, supersessionOpts{establishing: true})
		expectSupersededBy(t, log.of(older))
		m.completeEstablishment(newer)
		expectSupersededBy(t, log.of(older), newer)
		expectSupersededBy(t, log.of(newer))
	})
	t.Run("orders supersession by completion, not by registration", func(t *testing.T) {
		t.Parallel()
		m, log, next := setup(t)
		older := supersessionOpen(t, m, next, supersessionOpts{})
		resumed := supersessionOpen(t, m, next, supersessionOpts{establishing: true})
		newer := supersessionOpen(t, m, next, supersessionOpts{})
		m.completeEstablishment(resumed)
		expectSupersededBy(t, log.of(older), newer, resumed)
		expectSupersededBy(t, log.of(newer), resumed)
		expectSupersededBy(t, log.of(resumed))
	})
	t.Run("supersedes the least recently used session before it evicts it", func(t *testing.T) {
		t.Parallel()
		m, log, next := setup(t)
		older := supersessionOpen(t, m, next, supersessionOpts{})
		// older never carried traffic and has the lowest id: the least
		// recently used session (lessRecentlyActive).
		m.SetOnSessionClose(func(id uint16) {
			if id != older.SessionID {
				return
			}
			log.mu.Lock()
			log.events = append(log.events, "closing")
			log.mu.Unlock()
		})
		for range 4 {
			supersessionOpen(t, m, next, supersessionOpts{cats: []uint32{catA}})
		}
		log.mu.Lock()
		log.events = nil // the CAT_A sessions supersede one another
		log.mu.Unlock()
		newer := supersessionOpen(t, m, next, supersessionOpts{establishing: true})
		if _, err := m.Get(older.SessionID); err != nil {
			t.Fatal("the older session closed before the newer one was established")
		}
		m.completeEstablishment(newer)
		log.mu.Lock()
		events := slices.Clone(log.events)
		log.mu.Unlock()
		if !slices.Equal(events, []string{"superseded", "closing"}) {
			t.Fatalf("events = %q; want [superseded closing]", events)
		}
		expectSupersededBy(t, log.of(older), newer)
	})
}
