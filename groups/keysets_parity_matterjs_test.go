// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package groups

// A port of matter.js packages/protocol/test/groups/KeySetsTest.ts at
// 862ad75f: a fabric's operational group key sets — lookup and replacement,
// their keys, the current key and the operational-key match.
//
// The Go KeySets is the keySets map of the Manager's fabricGroups, loaded
// from the store's epoch keys: add is a store write and Reload, forId and
// allKeysForId read the map, currentKeyForId is fabricGroups.currentKey of a
// group mapped to the key set, and containsOperationalKey is the
// HasValidMapping Decode reports. Session ids are derived from the keys in
// Go, not set as matter.js's 100/101/102, so a slot is identified by its
// start time. Key set 0 is the IPK, which the manager does not hold
// (loadFabric); the forId and allKeysForId cases use key set 1.

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/store"
)

const keySetsHourUs = 60 * 60 * 1000 * 1000 // KeySetsTest.ts:11

var keySetsCompressed = [8]byte{8, 7, 6, 5, 4, 3, 2, 1}

// keySetsManager is a manager over fabric 1 holding sets, with the group
// key map mappings.
func keySetsManager(t *testing.T, mappings map[uint16]uint16, sets ...store.GroupKeySet) (*Manager, *memStore) {
	t.Helper()
	st := newMemStore()
	st.addFabric(1, 1, keySetsCompressed)
	for _, s := range sets {
		st.putKeySet(1, s)
	}
	for gid, ks := range mappings {
		st.mapGroup(1, gid, ks)
	}
	m, err := NewManager(st, nil)
	if err != nil {
		t.Fatal(err)
	}
	return m, st
}

// keySetsDefault is keySet(id): one zero key from start 1000 µs
// (KeySetsTest.ts:33-47).
func keySetsDefault(id uint16) store.GroupKeySet {
	return store.GroupKeySet{GroupKeySetID: id, EpochKey0: make([]byte, 16), EpochStart0: 1_000}
}

// keySetsThree is threeKeySet (KeySetsTest.ts:14-31): slots of key 0x00,
// 0x01 and 0x02 with the given start times in µs.
func keySetsThree(id uint16, s0, s1, s2 uint64) store.GroupKeySet {
	return store.GroupKeySet{
		GroupKeySetID: id,
		EpochKey0:     slices.Repeat([]byte{0}, 16), EpochStart0: s0,
		EpochKey1: slices.Repeat([]byte{1}, 16), EpochStart1: s1,
		EpochKey2: slices.Repeat([]byte{2}, 16), EpochStart2: s2,
	}
}

func keySetsForID(t *testing.T, m *Manager, id uint16) *keySet {
	t.Helper()
	f := groupsTestFabric(t, m)
	m.mu.RLock()
	defer m.mu.RUnlock()
	return f.keySets[id]
}

func keySetsOpKey(t *testing.T, epoch []byte) []byte {
	t.Helper()
	k, err := OperationalKey(epoch, keySetsCompressed)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestKeySetsForIDAdd(t *testing.T) {
	t.Parallel()

	t.Run("retrieves a key set by id", func(t *testing.T) {
		t.Parallel()
		m, _ := keySetsManager(t, nil, keySetsDefault(1))
		ks := keySetsForID(t, m, 1)
		if ks == nil || ks.id != 1 {
			t.Fatalf("forId(1) = %+v, want key set 1", ks)
		}
		if ks := keySetsForID(t, m, 99); ks != nil {
			t.Fatalf("forId(99) = %+v, want undefined", ks)
		}
	})

	t.Run("replaces an existing key set with the same id", func(t *testing.T) {
		t.Parallel()
		m, st := keySetsManager(t, nil, keySetsDefault(1))
		replacement := keySetsDefault(1)
		replacement.EpochKey0 = slices.Repeat([]byte{0x5a}, 16)
		st.putKeySet(1, replacement)
		if err := m.Reload(context.Background(), 1); err != nil {
			t.Fatal(err)
		}
		ks := keySetsForID(t, m, 1)
		if ks == nil || len(ks.epochs) != 1 || !slices.Equal(ks.epochs[0].key, keySetsOpKey(t, replacement.EpochKey0)) {
			t.Fatalf("forId(1) = %+v, want the replacement", ks)
		}
		f := groupsTestFabric(t, m)
		m.mu.RLock()
		size := len(f.keySets)
		m.mu.RUnlock()
		if size != 1 {
			t.Fatalf("size = %d, want 1", size)
		}
	})
}

func TestKeySetsAllKeysForID(t *testing.T) {
	t.Parallel()

	t.Run("throws for an unknown key set", func(t *testing.T) {
		t.Parallel()
		// Through the Go surface: a group mapped to key set 5, which the
		// fabric does not hold, has no key.
		m, _ := keySetsManager(t, map[uint16]uint16{1: 5})
		if ks := keySetsForID(t, m, 5); ks != nil {
			t.Fatalf("key set 5 = %+v, want none", ks)
		}
		f := groupsTestFabric(t, m)
		m.mu.Lock()
		_, _, err := f.currentKey(1, 1_700_000_000_000)
		m.mu.Unlock()
		if !errors.Is(err, ErrNoKey) || !strings.Contains(err.Error(), "group key set 5") {
			t.Fatalf("keys of key set 5: %v, want not found", err)
		}
	})

	t.Run("returns the operational keys present", func(t *testing.T) {
		t.Parallel()
		m, _ := keySetsManager(t, nil, keySetsDefault(1))
		ks := keySetsForID(t, m, 1)
		if ks == nil || len(ks.epochs) != 1 {
			t.Fatalf("allKeysForId(1) = %+v, want one key", ks)
		}
		sid, err := SessionID(keySetsOpKey(t, make([]byte, 16)))
		if err != nil {
			t.Fatal(err)
		}
		if ks.epochs[0].sessionID != sid {
			t.Fatalf("session id = %d, want %d", ks.epochs[0].sessionID, sid)
		}
	})
}

func TestKeySetsCurrentKeyForID(t *testing.T) {
	t.Parallel()
	const nowMs = 1_700_000_000_000
	const nowUs = uint64(nowMs) * 1000
	// current returns the start time of the current key of key set 5,
	// reached through group 1 which is mapped to it.
	current := func(t *testing.T, set store.GroupKeySet) (uint64, error) {
		t.Helper()
		m, _ := keySetsManager(t, map[uint16]uint16{1: set.GroupKeySetID}, set)
		f := groupsTestFabric(t, m)
		m.mu.Lock()
		defer m.mu.Unlock()
		_, ek, err := f.currentKey(1, nowMs)
		return ek.start, err
	}

	t.Run("returns the newest non-future key when a future-dated key is installed", func(t *testing.T) {
		t.Parallel()
		s := keySetsThree(5, nowUs-2*keySetsHourUs, nowUs-keySetsHourUs, nowUs+keySetsHourUs)
		if got, err := current(t, s); err != nil || got != s.EpochStart1 { // session id 101: slot 1
			t.Fatalf("current key starts at %d, %v; want slot 1 (%d)", got, err, s.EpochStart1)
		}
	})

	t.Run("returns the newest key when all keys are in the past", func(t *testing.T) {
		t.Parallel()
		s := keySetsThree(5, nowUs-3*keySetsHourUs, nowUs-2*keySetsHourUs, nowUs-keySetsHourUs)
		if got, err := current(t, s); err != nil || got != s.EpochStart2 { // session id 102: slot 2
			t.Fatalf("current key starts at %d, %v; want slot 2 (%d)", got, err, s.EpochStart2)
		}
	})

	t.Run("throws when every key is in the future", func(t *testing.T) {
		t.Parallel()
		s := keySetsThree(5, nowUs+keySetsHourUs, nowUs+2*keySetsHourUs, nowUs+3*keySetsHourUs)
		_, err := current(t, s)
		if !errors.Is(err, ErrKeyNotStarted) || !strings.Contains(err.Error(), "group key set 5") {
			t.Fatalf("current key: %v, want GroupKeyNotStartedError for key set 5", err)
		}
	})
}

func TestKeySetsContainsOperationalKey(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	// contains decodes a message to group gid sealed under the operational
	// key of epoch and reports whether gid's mapped key set carries it.
	contains := func(t *testing.T, m *Manager, gid uint16, epoch []byte) bool {
		t.Helper()
		op := keySetsOpKey(t, epoch)
		sid, err := SessionID(op)
		if err != nil {
			t.Fatal(err)
		}
		msg, err := m.Decode(ctx, sealGroup(t, op, sid, gid, 0x99, 1, false, []byte{0x01}))
		if err != nil {
			t.Fatalf("Decode: %v", err)
		}
		return msg.HasValidMapping
	}
	seq := make([]byte, 16) // Uint8Array.from({ length: 16 }, (_, i) => i)
	for i := range seq {
		seq[i] = byte(i)
	}

	t.Run("returns false for an unknown key set", func(t *testing.T) {
		t.Parallel()
		// Group 5 maps to key set 5, which is not held; key set 1 (group
		// 1) authenticates the message.
		m, _ := keySetsManager(t, map[uint16]uint16{1: 1, 5: 5}, keySetsDefault(1))
		if contains(t, m, 5, make([]byte, 16)) {
			t.Fatal("containsOperationalKey(5) = true, want false")
		}
	})

	t.Run("matches an operational key present in the key set", func(t *testing.T) {
		t.Parallel()
		m, _ := keySetsManager(t, map[uint16]uint16{1: 1},
			store.GroupKeySet{GroupKeySetID: 1, EpochKey0: slices.Clone(seq), EpochStart0: 1_000})
		if !contains(t, m, 1, seq) {
			t.Fatal("containsOperationalKey(1) = false, want true")
		}
	})

	t.Run("does not match a key absent from the key set", func(t *testing.T) {
		t.Parallel()
		m, _ := keySetsManager(t, map[uint16]uint16{1: 1, 2: 2},
			store.GroupKeySet{GroupKeySetID: 1, EpochKey0: slices.Repeat([]byte{1}, 16), EpochStart0: 1_000},
			store.GroupKeySet{GroupKeySetID: 2, EpochKey0: slices.Repeat([]byte{2}, 16), EpochStart0: 1_000})
		if contains(t, m, 1, slices.Repeat([]byte{2}, 16)) {
			t.Fatal("containsOperationalKey(1, key 0x02) = true, want false")
		}
	})

	t.Run("matches keys stored in a secondary slot", func(t *testing.T) {
		t.Parallel()
		set := keySetsDefault(1)
		set.EpochKey1, set.EpochStart1 = slices.Repeat([]byte{7}, 16), 1_000
		m, _ := keySetsManager(t, map[uint16]uint16{1: 1}, set)
		if !contains(t, m, 1, set.EpochKey1) {
			t.Fatal("containsOperationalKey(1, slot 1) = false, want true")
		}
	})

	t.Run("matches across key sets that share identical key material (session id collision)", func(t *testing.T) {
		t.Parallel()
		m, _ := keySetsManager(t, map[uint16]uint16{0x0101: 0x01a3, 0x0102: 0x01a4},
			store.GroupKeySet{GroupKeySetID: 0x01a3, EpochKey0: slices.Clone(seq), EpochStart0: 1_000},
			store.GroupKeySet{GroupKeySetID: 0x01a4, EpochKey0: slices.Clone(seq), EpochStart0: 1_000})
		if !contains(t, m, 0x0101, seq) {
			t.Error("containsOperationalKey(0x01a3) = false, want true")
		}
		if !contains(t, m, 0x0102, seq) {
			t.Error("containsOperationalKey(0x01a4) = false, want true")
		}
	})
}
