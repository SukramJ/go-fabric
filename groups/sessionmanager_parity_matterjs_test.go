// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package groups

// A port of matter.js packages/protocol/test/session/SessionManagerTest.ts at
// 862ad75f: describe "group data message counter" — the node-global group
// data counter and the group message reception state a key set's removal
// or rewrite forgets. The unicast session cases live in
// secure/operational/sessionmanager_parity_matterjs_test.go, the local
// parameter cases in controller/exchange/sessionmanager_parity_matterjs_test.go.

import (
	"context"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/store"
)

// sessionManagerEpochA / B are the epoch keys of SessionManagerTest.ts:607, :670-671.
var (
	sessionManagerEpochA = []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f}
	sessionManagerEpochB = []byte{0x0f, 0x0e, 0x0d, 0x0c, 0x0b, 0x0a, 0x09, 0x08, 0x07, 0x06, 0x05, 0x04, 0x03, 0x02, 0x01, 0x00}
)

// sessionManagerKeySet is keySet() of SessionManagerTest.ts:592-602.
func sessionManagerKeySet(id uint16, epochKey []byte) store.GroupKeySet {
	return store.GroupKeySet{GroupKeySetID: id, EpochKey0: epochKey, EpochStart0: 1}
}

// sessionManagerRemoveKeySet is fabric.groups.removeGroupKeySet(id): the
// row goes and the manager reloads the fabric.
func sessionManagerRemoveKeySet(t *testing.T, m *Manager, st *memStore, id uint16) {
	t.Helper()
	st.mu.Lock()
	st.sets[1] = slices.DeleteFunc(st.sets[1], func(ks store.GroupKeySet) bool { return ks.GroupKeySetID == id })
	st.mu.Unlock()
	sessionManagerReload(t, m)
}

func sessionManagerReload(t *testing.T, m *Manager) {
	t.Helper()
	if err := m.Reload(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
}

// sessionManagerOperationalKey is keySets.get(…).operationalEpochKey0.
func sessionManagerOperationalKey(t *testing.T, epochKey []byte) []byte {
	t.Helper()
	compressed := [8]byte{}
	copy(compressed[:], mustHex(t, "87e1b004e235a130"))
	k, err := OperationalKey(epochKey, compressed)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// sessionManagerGroups is groupFabric() of SessionManagerTest.ts:472-490 on
// a manager with a counter store and a transport: fabric 1, node id 1.
func sessionManagerGroups(t *testing.T) (*Manager, *memStore) {
	t.Helper()
	st := newMemStore()
	st.mu.Lock()
	compressed := [8]byte{}
	copy(compressed[:], mustHex(t, "87e1b004e235a130"))
	st.fabrics[1] = store.FabricRecord{FabricIndex: 1, FabricID: 0x456789ABCDEF1234, NodeID: 1, CompressedID: compressed}
	st.mu.Unlock()
	m, err := NewManager(st, nil)
	if err != nil {
		t.Fatal(err)
	}
	m.counterRandom = fixedStart(0x0010_0000)
	sessionManagerReload(t, m)
	return m, st
}

func TestSessionManagerGroupDataMessageCounter(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const source uint64 = 2 // SessionManagerTest.ts:612

	next := func(t *testing.T, m *Manager) uint32 {
		t.Helper()
		c, err := m.groupDataCounter(ctx)
		if err != nil {
			t.Fatal(err)
		}
		v, err := c.next(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	// updateMessageCounter throws DuplicateMessageError where Accept is false.
	accepts := func(m *Manager, key []byte, counter uint32) bool {
		return m.receptionWindow(key, source).Accept(counter)
	}

	t.Run("exposes one node-global counter shared across group sessions", func(t *testing.T) {
		t.Parallel()
		m, _ := sessionManagerGroups(t)
		first := next(t, m)
		if second := next(t, m); second != first+1 {
			t.Fatalf("second = %d, want %d", second, first+1)
		}
		// One counter for the node, not one per session.
		a, _ := m.groupDataCounter(ctx)
		b, _ := m.groupDataCounter(ctx)
		if a != b {
			t.Fatal("two group data counters")
		}
	})

	t.Run("does not roll the counter back when a group key set is removed (Q-02)", func(t *testing.T) {
		t.Parallel()
		m, st := sessionManagerGroups(t)
		st.putKeySet(1, sessionManagerKeySet(1, sessionManagerEpochA))
		sessionManagerReload(t, m)
		before := next(t, m)
		sessionManagerRemoveKeySet(t, m, st, 1)
		if after := next(t, m); after <= before {
			t.Fatalf("after = %d, want > %d", after, before)
		}
	})

	t.Run("forgets group message reception state when its key set is removed so a reused key re-syncs", func(t *testing.T) {
		t.Parallel()
		m, st := sessionManagerGroups(t)
		st.putKeySet(1, sessionManagerKeySet(1, sessionManagerEpochA))
		sessionManagerReload(t, m)
		key := sessionManagerOperationalKey(t, sessionManagerEpochA)
		if !accepts(m, key, 1000) || accepts(m, key, 500) {
			t.Fatal("the window does not treat 500 after 1000 as a replay")
		}
		sessionManagerRemoveKeySet(t, m, st, 1)
		st.putKeySet(1, sessionManagerKeySet(1, sessionManagerEpochA))
		sessionManagerReload(t, m)
		if !accepts(m, sessionManagerOperationalKey(t, sessionManagerEpochA), 500) {
			t.Fatal("the reused key did not re-sync on 500")
		}
	})

	t.Run("keeps reception state until the last key set sharing an operational key is removed", func(t *testing.T) {
		t.Parallel()
		m, st := sessionManagerGroups(t)
		st.putKeySet(1, sessionManagerKeySet(1, sessionManagerEpochA))
		st.putKeySet(1, sessionManagerKeySet(2, sessionManagerEpochA))
		sessionManagerReload(t, m)
		key := sessionManagerOperationalKey(t, sessionManagerEpochA)
		if !accepts(m, key, 1000) {
			t.Fatal("first counter refused")
		}
		sessionManagerRemoveKeySet(t, m, st, 1)
		if accepts(m, key, 500) {
			t.Fatal("reception state dropped while key set 2 still shares the key")
		}
		sessionManagerRemoveKeySet(t, m, st, 2)
		if !accepts(m, key, 500) {
			t.Fatal("reception state kept after the last key set went")
		}
	})

	t.Run("forgets the previous operational key when a key set is rewritten with a new epoch key", func(t *testing.T) {
		t.Parallel()
		m, st := sessionManagerGroups(t)
		st.putKeySet(1, sessionManagerKeySet(1, sessionManagerEpochA))
		sessionManagerReload(t, m)
		keyA := sessionManagerOperationalKey(t, sessionManagerEpochA)
		if !accepts(m, keyA, 1000) {
			t.Fatal("first counter refused")
		}
		st.putKeySet(1, sessionManagerKeySet(1, sessionManagerEpochB))
		sessionManagerReload(t, m)
		if !accepts(m, keyA, 500) {
			t.Fatal("key A's reception state survived the rewrite")
		}
	})
}
