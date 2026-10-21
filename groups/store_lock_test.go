// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package groups

import (
	"context"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/store"
)

// slowGroupStore holds its group-table write until released, as SQLite
// under a busy disk does, and reads the manager from inside it.
type slowGroupStore struct {
	*memStore
	m       *Manager
	entered chan struct{}
	release chan struct{}
}

func (s *slowGroupStore) UpsertGroupTableEntry(ctx context.Context, e store.GroupTableEntry) error {
	if s.m != nil {
		if _, err := s.m.HasKeyMapping(ctx, e.FabricIndex, e.GroupID); err != nil {
			return err
		}
		close(s.entered)
		<-s.release
	}
	return s.memStore.UpsertGroupTableEntry(ctx, e)
}

// TestGroupsStoreWriteWithoutTheLock pins G1-16 (ADR 0026 P6): a group
// table write reaches the store with the manager's state lock released —
// the store itself may read the manager, and the CASE identity lookup
// (IPKKeys) does not wait for the disk — while the write still lands in
// the cache once the store holds it.
func TestGroupsStoreWriteWithoutTheLock(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := &slowGroupStore{memStore: newMemStore(), entered: make(chan struct{}), release: make(chan struct{})}
	st.addFabric(1, groupsTestFabricID, [8]byte{})
	m, err := NewManager(st, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.GroupTable(ctx, 1); err != nil { // loads fabric 1
		t.Fatal(err)
	}
	st.m = m
	added := make(chan error, 1)
	go func() { added <- m.AddEndpointForGroup(ctx, 1, 7, 2, "kitchen") }()
	select {
	case <-st.entered:
	case err := <-added:
		t.Fatalf("AddEndpointForGroup returned %v before the store write", err)
	case <-time.After(10 * time.Second): // a held lock never releases: this bound only names the failure
		t.Fatal("the store's read of the manager waited for the manager's lock")
	}
	read := make(chan error, 1)
	go func() {
		_, err := m.IPKKeys(ctx, 1)
		read <- err
	}()
	select {
	case err := <-read:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("IPKKeys waited for the group table write")
	}
	close(st.release)
	if err := <-added; err != nil {
		t.Fatal(err)
	}
	table, err := m.GroupTable(ctx, 1)
	if err != nil || len(table) != 1 || table[0].GroupID != 7 {
		t.Fatalf("group table %+v, %v; want group 7", table, err)
	}
}
