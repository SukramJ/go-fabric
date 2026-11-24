// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
)

// TestScenesPendingRemovalsAreKeyed: while the table does not load, a
// repeated fabric-wide removal (Groups RemoveAllGroups, a fabric's drop)
// and a repeated group removal each keep one pending consequence, and a
// fabric-wide removal takes the fabric's earlier group removals and global
// scene with it; the load applies them (review II, R4-bound-4153).
func TestScenesPendingRemovalsAreKeyed(t *testing.T) {
	var fail atomic.Bool
	fail.Store(true)
	stored := []SceneEntry{
		{FabricIndex: 1, GroupID: 0, SceneID: 1},
		{FabricIndex: 1, GroupID: 5, SceneID: 2},
		{FabricIndex: 2, GroupID: 5, SceneID: 3},
		{FabricIndex: 2, GroupID: 6, SceneID: 4},
	}
	data, err := json.Marshal(stored)
	if err != nil {
		t.Fatal(err)
	}
	var saved []byte
	st := NewPersistedScenesState(
		func(context.Context) ([]byte, error) {
			if fail.Load() {
				return nil, errors.New("load refused")
			}
			return data, nil
		},
		func(_ context.Context, b []byte) error { saved = b; return nil },
	)
	for range 1000 {
		st.RemoveScenesForFabric(1)
	}
	for range 1000 {
		st.RemoveScenesForGroup(2, 5)
	}
	st.mu.Lock()
	n := len(st.pending)
	st.mu.Unlock()
	if n != 2 {
		t.Errorf("pending after 1000 removals of fabric 1 and 1000 of fabric 2's group 5 = %d, want 2", n)
	}
	st.RemoveScenesForGroup(1, 5)
	st.RemoveScenesForFabric(1)
	st.mu.Lock()
	n = len(st.pending)
	st.mu.Unlock()
	if n != 2 {
		t.Errorf("pending after a group removal subsumed by a later fabric removal = %d, want 2", n)
	}
	fail.Store(false)
	if !st.ready(context.Background()) {
		t.Fatal("the table did not load")
	}
	var got []SceneEntry
	if err := json.Unmarshal(saved, &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].FabricIndex != 2 || got[0].GroupID != 6 {
		t.Errorf("stored after the load = %+v, want only fabric 2's group 6 scene", got)
	}
}
