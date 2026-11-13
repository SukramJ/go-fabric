// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package groups

// The reception state is bounded per operational key, as connectedhomeip
// bounds its group peer table (CHIP_CONFIG_MAX_GROUP_DATA_PEERS,
// src/lib/core/CHIPConfig.h:833; the least recently used sender goes,
// src/transport/GroupPeerMessageCounter.cpp:79-93, at 6170af84). matter.js
// keeps one window per source node id for as long as the key lives
// (packages/protocol/src/groups/MessagingState.ts:80-90 at db021da0), so
// these cases have no matter.js test to port (review II, R2-P0-bound-A07).

import (
	"context"
	"encoding/hex"
	"slices"
	"testing"
)

// trackedSources returns the source node ids key holds a window for, most
// recently accepted first.
func trackedSources(m *Manager, key []byte) []uint64 {
	m.receptionMu.Lock()
	defer m.receptionMu.Unlock()
	windows := m.reception[hex.EncodeToString(key)]
	out := make([]uint64, 0, len(windows))
	for _, p := range windows {
		out = append(out, p.source)
	}
	return out
}

func TestReceptionStateIsBoundedPerKey(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	op, sid := opKeyOf(t, 0xA1)
	duplicate := func(t *testing.T, m *Manager, source uint64, counter uint32) bool {
		t.Helper()
		msg, err := m.Decode(ctx, sealGroup(t, op, sid, 0x0101, source, counter, true, []byte("x")))
		if err != nil {
			t.Fatal(err)
		}
		return msg.Duplicate
	}
	windows := func(m *Manager) int {
		m.receptionMu.Lock()
		defer m.receptionMu.Unlock()
		return len(m.reception[hex.EncodeToString(op)])
	}

	t.Run("a key holder's claimed source node ids allocate at most maxReceptionPeers windows", func(t *testing.T) {
		t.Parallel()
		m, _ := newTestManager(t)
		for source := uint64(1); source <= 1000; source++ {
			if duplicate(t, m, source, 7) {
				t.Fatalf("the first message of source %d was taken for a replay", source)
			}
		}
		if n := windows(m); n != maxReceptionPeers {
			t.Fatalf("%d reception windows for one key after 1000 claimed sources, want %d", n, maxReceptionPeers)
		}
	})

	t.Run("a replay is refused for as long as its sender is tracked", func(t *testing.T) {
		t.Parallel()
		m, _ := newTestManager(t)
		if duplicate(t, m, 1, 1000) {
			t.Fatal("precondition: the first message was taken for a replay")
		}
		// maxReceptionPeers-1 other senders fill the table; source 1 stays.
		for source := uint64(2); source <= maxReceptionPeers; source++ {
			duplicate(t, m, source, 1)
		}
		if !duplicate(t, m, 1, 1000) || !duplicate(t, m, 1, 500) {
			t.Fatal("a replayed counter of a tracked sender was accepted")
		}
		if duplicate(t, m, 1, 1001) {
			t.Fatal("the tracked sender's next counter was refused")
		}
	})

	t.Run("an evicted sender re-enters trust-first, and no message of a real sender is lost", func(t *testing.T) {
		t.Parallel()
		m, _ := newTestManager(t)
		duplicate(t, m, 1, 1000)
		for source := uint64(2); source <= maxReceptionPeers+1; source++ {
			duplicate(t, m, source, 1)
		}
		if slices.Contains(trackedSources(m, op), 1) {
			t.Fatal("precondition: source 1, the least recently used, was not evicted")
		}
		// The window went with the eviction: the sender's next message is
		// accepted, and so — the price of the bound, as in connectedhomeip
		// (VerifyOrTrustFirstGroup, src/transport/PeerMessageCounter.h:102-108)
		// — would be an old one presented first.
		if duplicate(t, m, 1, 500) {
			t.Fatal("the first message of a re-entering sender was refused")
		}
		// From there on the window holds again.
		if !duplicate(t, m, 1, 500) || !duplicate(t, m, 1, 499) {
			t.Fatal("a replay was accepted after the sender re-entered")
		}
		if duplicate(t, m, 1, 1001) {
			t.Fatal("the re-entered sender's next counter was refused")
		}
	})

	t.Run("one key's senders are not evicted by another key's", func(t *testing.T) {
		t.Parallel()
		m, _ := newTestManager(t)
		other := []byte{9, 9, 9}
		if !m.acceptCounter(other, 1, 1000) {
			t.Fatal("precondition")
		}
		for source := uint64(1); source <= 100; source++ {
			duplicate(t, m, source, 1)
		}
		if m.acceptCounter(other, 1, 1000) {
			t.Fatal("senders of one key evicted a sender of another: its replay was accepted")
		}
	})
}

// TestReceptionStateEvictionOrder: the sender whose last accepted message
// is the oldest goes first; an accepted message refreshes its sender, a
// refused one does not — so replaying recorded messages of tracked senders
// cannot choose who is evicted next.
func TestReceptionStateEvictionOrder(t *testing.T) {
	t.Parallel()
	key := []byte{1, 2, 3}
	fill := func(m *Manager) {
		for source := uint64(1); source <= maxReceptionPeers; source++ {
			if !m.acceptCounter(key, source, 100) {
				t.Fatalf("first counter of source %d refused", source)
			}
		}
	}

	t.Run("most recently accepted first, the least recently used evicted", func(t *testing.T) {
		t.Parallel()
		m, _ := newTestManager(t)
		fill(m)
		want := make([]uint64, 0, maxReceptionPeers)
		for source := uint64(maxReceptionPeers); source >= 1; source-- {
			want = append(want, source)
		}
		if got := trackedSources(m, key); !slices.Equal(got, want) {
			t.Fatalf("order after filling = %v, want %v", got, want)
		}
		if !m.acceptCounter(key, 1, 101) { // source 1 is used again
			t.Fatal("next counter of source 1 refused")
		}
		m.acceptCounter(key, 1000, 1) // a new sender: source 2 is now the oldest
		got := trackedSources(m, key)
		if len(got) != maxReceptionPeers || got[0] != 1000 || got[1] != 1 || slices.Contains(got, 2) || !slices.Contains(got, 3) {
			t.Fatalf("after a refresh of 1 and a new sender: %v, want 1000 first, 1 kept, 2 evicted", got)
		}
	})

	t.Run("a refused counter does not refresh its sender", func(t *testing.T) {
		t.Parallel()
		m, _ := newTestManager(t)
		fill(m)
		if m.acceptCounter(key, 1, 100) { // a replay of source 1's message
			t.Fatal("the replay was accepted")
		}
		m.acceptCounter(key, 1000, 1)
		if got := trackedSources(m, key); slices.Contains(got, 1) || !slices.Contains(got, 2) {
			t.Fatalf("after a replay of 1 and a new sender: %v, want 1 evicted and 2 kept", got)
		}
	})
}
