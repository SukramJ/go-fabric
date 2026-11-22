// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package operational

import (
	"sync"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/secure/channel"
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

// TestAllocateIDAtAFullTableEvictsInsideTheFabricHoldingTheMost holds
// R2-P6-bound-0107: when live sessions hold every id, the session closed
// for the new handshake is taken from the fabric that holds the most
// sessions — the least recently active of that fabric — so one fabric's
// administrator who fills the id space with busy sessions evicts its own,
// not another fabric's idle ones. Fabric 2 holds a few sessions, one of
// them the least recently active of the whole table; fabric 1 holds the
// rest. The id comes from fabric 1, and fabric 2 loses nothing.
func TestAllocateIDAtAFullTableEvictsInsideTheFabricHoldingTheMost(t *testing.T) {
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
	const (
		victimIdle      = uint16(7)   // fabric 2, idle the longest of all
		fabricOneOldest = uint16(500) // fabric 1, its least recently active
	)
	base := time.Now()
	m.mu.Lock()
	for id := uint16(1); id <= SessionIDSpace; id++ {
		fabric, mono := uint8(1), 10*time.Second
		if id <= 10 {
			fabric = 2
		}
		switch id {
		case victimIdle:
			mono = time.Second
		case fabricOneOldest:
			mono = 5 * time.Second
		}
		m.sessions[id] = &Entry{
			SessionID: id, fabricIndex: fabric, Session: sess,
			lastActivity: base, lastActivityMono: mono, createdAt: base,
		}
	}
	m.mu.Unlock()

	id, err := m.AllocateID()
	if err != nil {
		t.Fatalf("AllocateID at a full table: %v", err)
	}
	if id != fabricOneOldest {
		t.Fatalf("AllocateID = %d, want %d (fabric 1's least recently active); fabric 2's idle session %d must stay", id, fabricOneOldest, victimIdle)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(closed) != 1 || closed[0] != fabricOneOldest {
		t.Fatalf("sessions closed = %v, want [%d]", closed, fabricOneOldest)
	}
	if e, err := m.Get(victimIdle); err != nil || e.Session == nil {
		t.Fatalf("fabric 2's session %d was taken", victimIdle)
	}
}
