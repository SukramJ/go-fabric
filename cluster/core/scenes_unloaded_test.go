// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	"thingfabric.dev/go-fabric/cluster/core"
	"thingfabric.dev/go-fabric/contract"
)

// sceneStore is a scene table store whose load can be made to fail.
type sceneStore struct {
	mu     sync.Mutex
	data   []byte
	refuse bool
	saves  int
}

func (s *sceneStore) load(context.Context) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.refuse {
		return nil, errors.New("the store refuses")
	}
	return slices.Clone(s.data), nil
}

func (s *sceneStore) save(_ context.Context, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data = slices.Clone(data)
	s.saves++
	return nil
}

func (s *sceneStore) table(t *testing.T) []core.SceneEntry {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []core.SceneEntry
	if err := json.Unmarshal(s.data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func persistedScenes(t *testing.T, store *sceneStore, light *sceneOnOff) (*core.ScenesManagement, *core.ScenesState) {
	t.Helper()
	st := core.NewPersistedScenesState(store.load, store.save)
	s, err := core.NewScenesManagement(core.ScenesConfig{
		State:      st,
		Siblings:   func() []contract.ClusterServer { return []contract.ClusterServer{light} },
		GroupKnown: func(context.Context, uint8, uint16) bool { return true },
		Fabrics:    func(context.Context) []uint8 { return []uint8{1} },
	})
	if err != nil {
		t.Fatal(err)
	}
	return s, st
}

// globalScenes counts fabric's global scene entries (group 0, scene 0).
func globalScenes(table []core.SceneEntry, fabric uint8) int {
	n := 0
	for _, e := range table {
		if e.FabricIndex == fabric && e.GroupID == 0 && e.SceneID == 0 {
			n++
		}
	}
	return n
}

// storedGlobalScene is a stored table holding fabric 1's global scene with
// OnOff on.
func storedGlobalScene(t *testing.T) []byte {
	t.Helper()
	var out []byte
	light := &sceneOnOff{on: true}
	s, _ := newScenes(t, light, func(b []byte) { out = slices.Clone(b) })
	s.StoreGlobalScene(fabric1, 1)
	if len(out) == 0 {
		t.Fatal("no global scene stored")
	}
	return out
}

// TestScenesStoreGlobalSceneLoadsFirst (review II, R2-P6-store-1202): on a
// persisted table nothing has loaded yet, OffWithEffect's global scene
// replaces the stored one instead of appending a second (fabric, 0, 0)
// entry; while the load fails, repeated OffWithEffects leave one entry
// once it succeeds, and the pending changes do not grow with them.
func TestScenesStoreGlobalSceneLoadsFirst(t *testing.T) {
	t.Parallel()
	store := &sceneStore{data: storedGlobalScene(t)}
	s, _ := persistedScenes(t, store, &sceneOnOff{})
	s.StoreGlobalScene(fabric1, 1)
	if n := globalScenes(store.table(t), 1); n != 1 {
		t.Fatalf("global scenes of fabric 1 after StoreGlobalScene on an unloaded table: %d, want 1", n)
	}
	m := invoke(t, s, 0x06, core.SceneGroupRequest{GroupID: 0}).(core.GetSceneMembershipResponse)
	if !slices.Equal(m.SceneList, []uint8{0}) {
		t.Errorf("GetSceneMembership(0) %v, want [0]", m.SceneList)
	}

	failing := &sceneStore{data: storedGlobalScene(t), refuse: true}
	s2, st2 := persistedScenes(t, failing, &sceneOnOff{})
	for range 50 {
		s2.StoreGlobalScene(fabric1, 1)
	}
	if n := core.PendingSceneChanges(st2); n > 1 {
		t.Errorf("%d pending changes after 50 OffWithEffects on a table that does not load, want at most 1", n)
	}
	failing.mu.Lock()
	failing.refuse = false
	failing.mu.Unlock()
	if _, ok := s2.MatterReadFiltered(fabric1, 0x0002); !ok {
		t.Fatal("FabricSceneInfo once the store loads")
	}
	if n := globalScenes(failing.table(t), 1); n != 1 {
		t.Errorf("global scenes of fabric 1 once the load succeeds: %d, want 1", n)
	}
}

// TestScenesRecallGlobalSceneLoadsFirst (review II, R2-P6-store-1203): on a
// persisted table nothing has loaded yet, OnWithRecallGlobalScene applies
// the stored global scene and makes it current.
func TestScenesRecallGlobalSceneLoadsFirst(t *testing.T) {
	t.Parallel()
	store := &sceneStore{data: storedGlobalScene(t)}
	light := &sceneOnOff{}
	s, _ := persistedScenes(t, store, light)
	s.RecallGlobalScene(fabric1, 1)
	if !light.on {
		t.Fatal("RecallGlobalScene on an unloaded table applied nothing")
	}
	v, _ := s.MatterReadFiltered(fabric1, 0x0002)
	si := v.([]core.SceneInfoStruct)
	if len(si) != 1 || si[0].CurrentScene != 0 || si[0].CurrentGroup != 0 || !si[0].SceneValid {
		t.Errorf("FabricSceneInfo after the recall: %+v", si)
	}
}

// TestScenesLoadedTableIsValidated (review II, R2-P6-store-1212): a stored
// table is held to the server's rules when it loads — a duplicate key is
// dropped, the entries beyond SceneTableSize too, and an over-long name is
// cut — and MatterHeldFabrics names the stored fabrics before any dispatch
// loaded the table, so the node's reconcile can drop a removed one.
func TestScenesLoadedTableIsValidated(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	b.WriteString(`[{"f":3,"g":1,"s":1,"name":"` + strings.Repeat("x", 40) + `"},{"f":3,"g":1,"s":1}`)
	for i := range 130 {
		fmt.Fprintf(&b, `,{"f":%d,"g":2,"s":%d}`, 4+i%2, i/2)
	}
	b.WriteString(`]`)
	store := &sceneStore{data: []byte(b.String())}
	s, st := persistedScenes(t, store, &sceneOnOff{})
	if got := s.MatterHeldFabrics(); !slices.Equal(got, []uint8{3, 4, 5}) {
		t.Errorf("MatterHeldFabrics before any dispatch %v, want [3 4 5]", got)
	}
	table := core.SceneTable(st)
	if len(table) != 128 {
		t.Errorf("%d entries loaded, want SceneTableSize 128", len(table))
	}
	seen := map[[3]int]bool{}
	for _, e := range table {
		k := [3]int{int(e.FabricIndex), int(e.GroupID), int(e.SceneID)}
		if seen[k] {
			t.Errorf("duplicate entry %v", k)
		}
		seen[k] = true
		if len(e.Name) > 16 {
			t.Errorf("name %q longer than 16 bytes", e.Name)
		}
	}
}
