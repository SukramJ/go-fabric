// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package operational

// The peer index leaves a session with the session (F-b25ed960-41): every
// removal from the table takes the entry out of byPeer at once, with no
// establishment and no reaper run in between. matter.js keeps no peer
// index; a closed session leaves its sets at the end of its close
// (packages/protocol/src/session/NodeSession.ts:485-504 at b25ed960,
// registeredSessions.delete :503; SessionManager.ts:333-335 drops it from
// the established set).

import (
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
)

const peerIndexPeer uint64 = 0x4141

// indexed is the number of sessions byPeer holds for peer, read without
// the pruning livePeerBucketLocked does.
func indexed(m *Manager, peer uint64) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.byPeer[peer])
}

func TestPeerIndexDropsSessionAtRemoval(t *testing.T) {
	t.Parallel()
	open := func(t *testing.T, m *Manager, id uint16) *Entry {
		t.Helper()
		e, err := m.openFromSigma(id, nil, sessionManagerFabric, sessionManagerLocal, peerIndexPeer, id, nil, closeParityKeys(byte(id)), false, false)
		if err != nil {
			t.Fatalf("open %#x: %v", id, err)
		}
		return e
	}
	cases := []struct {
		name string
		// close removes some of the two sessions a and b and returns how
		// many stay indexed.
		close func(t *testing.T, m *Manager, a, b *Entry) int
	}{
		{"Close", func(t *testing.T, m *Manager, a, _ *Entry) int {
			t.Helper()
			if err := m.Close(a.SessionID); err != nil {
				t.Fatal(err)
			}
			return 1
		}},
		{"closeEntry", func(_ *testing.T, m *Manager, a, _ *Entry) int {
			m.closeEntry(a)
			return 1
		}},
		{"CloseFabric", func(_ *testing.T, m *Manager, _, _ *Entry) int {
			m.CloseFabric(sessionManagerFabric)
			return 0
		}},
		{"CloseFabricExcept", func(_ *testing.T, m *Manager, _, b *Entry) int {
			m.CloseFabricExcept(sessionManagerFabric, b.SessionID)
			return 1
		}},
		{"ClosePeer", func(_ *testing.T, m *Manager, _, _ *Entry) int {
			m.ClosePeer(sessionManagerFabric, peerIndexPeer)
			return 0
		}},
		{"ClosePeerLostBefore", func(_ *testing.T, m *Manager, _, b *Entry) int {
			m.ClosePeerLostBefore(sessionManagerFabric, peerIndexPeer, b.registration)
			return 1
		}},
		{"CloseAllGraceful", func(_ *testing.T, m *Manager, _, _ *Entry) int {
			m.CloseAllGraceful(time.Time{})
			return 0
		}},
		{"reapIdle", func(_ *testing.T, m *Manager, _, _ *Entry) int {
			m.clk().(*clocktest.Fake).Advance(2 * time.Hour)
			m.reapIdle(time.Hour)
			return 0
		}},
		{"occupant replacement", func(t *testing.T, m *Manager, a, _ *Entry) int {
			t.Helper()
			// Installed establishing: no supersession check reads (and
			// prunes) the peer's bucket at the install.
			openEstablishing(t, m, a.SessionID, peerIndexPeer)
			return 2
		}},
		{"per-peer cap eviction", func(t *testing.T, m *Manager, _, _ *Entry) int {
			t.Helper()
			for id := uint16(3); id <= MaxSessionsPerPeer+1; id++ {
				open(t, m, id)
			}
			return MaxSessionsPerPeer
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			m, _ := newEstablishingManager(t)
			a, b := open(t, m, 1), open(t, m, 2)
			want := c.close(t, m, a, b)
			if got := indexed(m, peerIndexPeer); got != want {
				t.Fatalf("byPeer holds %d sessions of the peer; want %d", got, want)
			}
			if want == 0 {
				m.mu.RLock()
				_, ok := m.byPeer[peerIndexPeer]
				m.mu.RUnlock()
				if ok {
					t.Fatal("byPeer keeps an empty bucket for the peer")
				}
			}
		})
	}
}
