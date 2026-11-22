// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"encoding/json"
	"slices"
	"sync"
	"testing"

	mattercore "thingfabric.dev/go-fabric/cluster/core"
)

// memScenesStore is a [ScenesStore] holding one table per key.
type memScenesStore struct {
	mu     sync.Mutex
	tables map[string][]byte
}

func (s *memScenesStore) LoadScenes(key SourceKey) []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tables[key.String()]
}

func (s *memScenesStore) SaveScenes(key SourceKey, table []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tables[key.String()] = table
}

// fabricsIn lists the fabric indexes of the scene entries in a saved table.
func (s *memScenesStore) fabricsIn(t *testing.T, key SourceKey) []uint8 {
	t.Helper()
	var table []mattercore.SceneEntry
	if err := json.Unmarshal(s.LoadScenes(key), &table); err != nil {
		t.Fatalf("saved scene table: %v", err)
	}
	var out []uint8
	for _, e := range table {
		if !slices.Contains(out, e.FabricIndex) {
			out = append(out, e.FabricIndex)
		}
	}
	return out
}

// TestForgetFabricScenesReachesEveryTable pins Endpoint.ForgetFabricScenes'
// two ways in: a scene table persisted but not yet loaded by any dispatch,
// and a table loaded in memory with no store attached any more. In both
// the removed fabric's scenes go and the other fabric's stay, so a later
// fabric that is given the same index does not inherit them
// (R2-P5-tmut-18).
func TestForgetFabricScenesReachesEveryTable(t *testing.T) {
	t.Parallel()
	seeded := func(t *testing.T) (*memScenesStore, SourceKey) {
		t.Helper()
		key := StringKey("lamp")
		raw, err := json.Marshal([]mattercore.SceneEntry{
			{FabricIndex: 1, GroupID: 1, SceneID: 1},
			{FabricIndex: 2, GroupID: 1, SceneID: 1},
		})
		if err != nil {
			t.Fatal(err)
		}
		return &memScenesStore{tables: map[string][]byte{key.String(): raw}}, key
	}

	t.Run("persisted, not loaded", func(t *testing.T) {
		t.Parallel()
		store, key := seeded(t)
		e := &Endpoint{ID: 7, SourceKey: key, persistence: hookPersistence{scenes: store}}
		e.ForgetFabricScenes(2)
		if got := store.fabricsIn(t, key); !slices.Equal(got, []uint8{1}) {
			t.Fatalf("persisted scene table holds fabrics %v after fabric 2 was forgotten, want [1]", got)
		}
	})

	t.Run("loaded, store detached", func(t *testing.T) {
		t.Parallel()
		store, key := seeded(t)
		e := &Endpoint{ID: 7, SourceKey: key, persistence: hookPersistence{scenes: store}}
		e.scenesState() // a dispatch loaded the table
		e.persistence = nil
		e.ForgetFabricScenes(2)
		if got := store.fabricsIn(t, key); !slices.Equal(got, []uint8{1}) {
			t.Fatalf("loaded scene table holds fabrics %v after fabric 2 was forgotten, want [1]", got)
		}
	})
}
