// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package operational

import (
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/secure/channel"
)

// TestAllocateIDAtAFullTableOfLiveSessionsClosesTheLeastRecentlyActive: when
// live sessions hold every id — one fabric's administrator can open them for
// as many node ids as it issues NOCs — a handshake still gets an id: the
// least recently active session is closed and its id reused, as matter.js
// does (packages/protocol/src/session/SessionManager.ts:590-598,
// findOldestInactiveSession :564-572, #leastRecentlyUsedSession :553-562
// at db021da0); review II, R2-P3-admin-05.
func TestAllocateIDAtAFullTableOfLiveSessionsClosesTheLeastRecentlyActive(t *testing.T) {
	t.Parallel()
	m := NewManager(newMinimalFakeStore())
	keys := lifecycleKeys(1)
	sess, err := channel.New(channel.Config{EncryptKey: keys.R2IKey[:], DecryptKey: keys.I2RKey[:], LocalNodeID: 1, PeerNodeID: 2})
	if err != nil {
		t.Fatal(err)
	}
	var (
		mu     sync.Mutex
		closed []uint16
	)
	m.SetOnSessionClose(func(id uint16) {
		mu.Lock()
		closed = append(closed, id)
		mu.Unlock()
	})
	const leastRecent = uint16(42)
	base := time.Now()
	m.mu.Lock()
	for id := uint16(1); id <= SessionIDSpace; id++ {
		mono := 10 * time.Second
		if id == leastRecent {
			mono = time.Second
		}
		m.sessions[id] = &Entry{
			SessionID: id, fabricIndex: 1, Session: sess,
			lastActivity: base, lastActivityMono: mono, createdAt: base,
		}
	}
	m.mu.Unlock()

	id, err := m.AllocateID()
	if err != nil {
		t.Fatalf("AllocateID with every id held by a live session: %v, want the least recently active session's id", err)
	}
	if id != leastRecent {
		t.Fatalf("AllocateID = %d, want %d (the least recently active session)", id, leastRecent)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(closed) != 1 || closed[0] != leastRecent {
		t.Fatalf("sessions closed = %v, want [%d]", closed, leastRecent)
	}
}
