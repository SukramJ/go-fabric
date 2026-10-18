// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package groups

// A port of matter.js packages/protocol/test/groups/GroupsTest.ts at
// 862ad75f: a fabric's group state — the per-group multicast address, the
// replacement of the GroupKeyMap, the subject a group message is evaluated
// under and the current key of a group.
//
// The Go fabric's group state is the Manager's fabricGroups, loaded from the
// store: the idMap setter is a store change and Reload, subjectForGroup is
// the HasValidMapping and Endpoints Decode reports, and currentKeyForId is
// fabricGroups.currentKey. The store holds epoch keys, not the operational
// keys matter.js puts in OperationalKeySet; identical epoch keys derive
// identical operational keys, which is what the collision case needs.

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/store"
)

const groupsTestFabricID uint64 = 0x1122334455667788 // GroupsTest.ts:15

// groupsTestFabric returns the loaded group state of fabric 1.
func groupsTestFabric(t *testing.T, m *Manager) *fabricGroups {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	f, err := m.fabricLocked(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestGroupsMulticastAddress(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newMemStore()
	st.addFabric(1, groupsTestFabricID, [8]byte{})
	m, err := NewManager(st, nil)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("derives a stable per-group multicast address for the fabric", func(t *testing.T) {
		t.Parallel()
		a, err := m.MulticastAddressFor(ctx, 1, 5)
		if err != nil {
			t.Fatal(err)
		}
		if got, want := a.String(), "ff35:40:fd11:2233:4455:6677:8800:5"; got != want { // GroupsTest.ts:37
			t.Fatalf("multicastAddress(5) = %s, want %s", got, want)
		}
		b, err := m.MulticastAddressFor(ctx, 1, 5)
		if err != nil || !a.Equal(b) {
			t.Fatalf("second multicastAddress(5) = %s, %v; want %s", b, err, a)
		}
	})

	t.Run("rejects an invalid group id", func(t *testing.T) {
		t.Parallel()
		if a, err := m.MulticastAddressFor(ctx, 1, 0); !errors.Is(err, ErrInvalidGroupID) {
			t.Fatalf("multicastAddress(0) = %s, %v; want ErrInvalidGroupID", a, err)
		}
	})
}

func TestGroupsIDMap(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	newFabric := func(t *testing.T) (*Manager, *memStore) {
		t.Helper()
		st := newMemStore()
		st.addFabric(1, groupsTestFabricID, [8]byte{})
		m, err := NewManager(st, nil)
		if err != nil {
			t.Fatal(err)
		}
		return m, st
	}
	// replace is the idMap setter: the store's GroupKeyMap becomes the
	// replacement map and the fabric reloads it.
	replace := func(t *testing.T, m *Manager, st *memStore, entries map[uint16]uint16) {
		t.Helper()
		st.mu.Lock()
		st.maps[1] = nil
		for gid, ks := range entries {
			st.maps[1] = append(st.maps[1], store.GroupKeyMapping{FabricIndex: 1, GroupID: gid, GroupKeySetID: ks})
		}
		st.mu.Unlock()
		if err := m.Reload(ctx, 1); err != nil {
			t.Fatal(err)
		}
	}
	idMap := func(t *testing.T, m *Manager, gid uint16) (uint16, bool) {
		t.Helper()
		f := groupsTestFabric(t, m)
		m.mu.RLock()
		defer m.mu.RUnlock()
		ks, ok := f.idMap[gid]
		return ks, ok
	}

	t.Run("applies additions from a replacement map", func(t *testing.T) {
		t.Parallel()
		m, st := newFabric(t)
		replace(t, m, st, map[uint16]uint16{1: 10, 2: 20})
		if ks, ok := idMap(t, m, 1); !ok || ks != 10 {
			t.Errorf("idMap[1] = %d, %v; want 10", ks, ok)
		}
		if ks, ok := idMap(t, m, 2); !ok || ks != 20 {
			t.Errorf("idMap[2] = %d, %v; want 20", ks, ok)
		}
	})

	t.Run("removes entries absent from a replacement map", func(t *testing.T) {
		t.Parallel()
		m, st := newFabric(t)
		replace(t, m, st, map[uint16]uint16{1: 10, 2: 20})
		replace(t, m, st, map[uint16]uint16{1: 10})
		if ks, ok := idMap(t, m, 1); !ok || ks != 10 {
			t.Errorf("idMap[1] = %d, %v; want 10", ks, ok)
		}
		if ks, ok := idMap(t, m, 2); ok {
			t.Errorf("idMap[2] = %d; want undefined", ks)
		}
	})
}

func TestGroupsSubjectForGroup(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	shared := make([]byte, 16) // GroupsTest.ts:75
	for i := range shared {
		shared[i] = byte(i)
	}
	other := slices.Repeat([]byte{0xaa}, 16) // GroupsTest.ts:76
	compressed := [8]byte{1, 2, 3, 4, 5, 6, 7, 8}

	// mapped is GroupsTest.ts:78-91: 0x01a3 and 0x01a4 carry identical key
	// material, group 0x0101 maps to 0x01a4 with endpoint 1, group 0x0300
	// to 0x0055.
	mapped := func(t *testing.T) *Manager {
		t.Helper()
		st := newMemStore()
		st.addFabric(1, groupsTestFabricID, compressed)
		st.putKeySet(1, store.GroupKeySet{GroupKeySetID: 0x01a3, EpochKey0: slices.Clone(shared), EpochStart0: 1_000})
		st.putKeySet(1, store.GroupKeySet{GroupKeySetID: 0x01a4, EpochKey0: slices.Clone(shared), EpochStart0: 1_000})
		st.putKeySet(1, store.GroupKeySet{GroupKeySetID: 0x0055, EpochKey0: slices.Clone(other), EpochStart0: 1_000})
		st.mapGroup(1, 0x0101, 0x01a4)
		st.mapGroup(1, 0x0300, 0x0055)
		m, err := NewManager(st, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := m.AddEndpointForGroup(ctx, 1, 0x0101, 1, ""); err != nil {
			t.Fatal(err)
		}
		return m
	}
	// subjectFor decodes a message to group gid authenticated under the
	// shared key.
	subjectFor := func(t *testing.T, gid uint16) *Message {
		t.Helper()
		opKey, err := OperationalKey(shared, compressed)
		if err != nil {
			t.Fatal(err)
		}
		sid, err := SessionID(opKey)
		if err != nil {
			t.Fatal(err)
		}
		msg, err := mapped(t).Decode(ctx, sealGroup(t, opKey, sid, gid, 0x1234, 1, false, []byte{0x01}))
		if err != nil {
			t.Fatalf("Decode: %v", err)
		}
		return msg
	}

	t.Run("accepts a session-id collision peer that carries the authenticating key", func(t *testing.T) {
		t.Parallel()
		msg := subjectFor(t, 0x0101)
		if !msg.HasValidMapping {
			t.Error("hasValidMapping = false, want true")
		}
		if !slices.Equal(msg.Endpoints, []uint16{1}) {
			t.Errorf("endpoints = %v, want [1]", msg.Endpoints)
		}
	})

	t.Run("rejects when the mapped key set does not carry the authenticating key", func(t *testing.T) {
		t.Parallel()
		if subjectFor(t, 0x0300).HasValidMapping {
			t.Error("hasValidMapping = true, want false")
		}
	})

	t.Run("rejects an unmapped group", func(t *testing.T) {
		t.Parallel()
		msg := subjectFor(t, 0x0999)
		if msg.HasValidMapping {
			t.Error("hasValidMapping = true, want false")
		}
		if len(msg.Endpoints) != 0 {
			t.Errorf("endpoints = %v, want []", msg.Endpoints)
		}
	})
}

func TestGroupsCurrentKeyForID(t *testing.T) {
	t.Parallel()
	const nowMs = 1_700_000_000_000
	// withKeySets is GroupsTest.ts:116-122 on fabric id 1.
	withKeySets := func(t *testing.T, mappings map[uint16]uint16, sets ...store.GroupKeySet) *fabricGroups {
		t.Helper()
		st := newMemStore()
		st.addFabric(1, 1, [8]byte{})
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
		return groupsTestFabric(t, m)
	}
	wantMissing := func(t *testing.T, err error, text string) {
		t.Helper()
		if !errors.Is(err, ErrNoKey) || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(text)) {
			t.Fatalf("currentKeyForId(5) = %v, want GroupKeySetMissingError %q", err, text)
		}
	}

	t.Run("refuses a group the fabric maps no key set to", func(t *testing.T) {
		t.Parallel()
		_, _, err := withKeySets(t, nil).currentKey(5, nowMs)
		wantMissing(t, err, "No group key set found for groupId 5") // GroupsTest.ts:125-128
	})

	t.Run("refuses a group mapped to a key set the fabric does not hold", func(t *testing.T) {
		t.Parallel()
		_, _, err := withKeySets(t, map[uint16]uint16{5: 0x1a1}).currentKey(5, nowMs)
		wantMissing(t, err, "GroupId 5 is mapped to group key set 417, which the fabric does not hold") // GroupsTest.ts:131-138
	})

	t.Run("returns the key of the key set the group is mapped to", func(t *testing.T) {
		t.Parallel()
		f := withKeySets(t, map[uint16]uint16{5: 0x1a1},
			store.GroupKeySet{GroupKeySetID: 0x1a1, EpochKey0: make([]byte, 16), EpochStart0: 1_000})
		ks, _, err := f.currentKey(5, nowMs)
		if err != nil || ks != 0x1a1 {
			t.Fatalf("currentKeyForId(5) = %#x, %v; want key set 0x1a1", ks, err)
		}
	})
}
