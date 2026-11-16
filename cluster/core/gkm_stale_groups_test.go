// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/groups"
	mstore "github.com/SukramJ/go-fabric/store"
)

// reloadFailStore fails the group state's read of a fabric's key sets
// while fail is set: a Reload that cannot refresh.
type reloadFailStore struct {
	*fakeStore
	fail atomic.Bool
}

func (s *reloadFailStore) ListGroupKeySets(ctx context.Context, fabricIndex uint8) ([]mstore.GroupKeySet, error) {
	if s.fail.Load() {
		return nil, errors.New("injected reload failure")
	}
	return s.fakeStore.ListGroupKeySets(ctx, fabricIndex)
}

// A change the store took answers success when the reload of the
// operational group state fails; the state is marked stale and reloaded by
// the server's next command (review II, R2-P6-err-1701, R2-P6-err-1014).
func TestGKMStoredChangeSucceedsAndTheStaleGroupStateIsReloaded(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fs := newFakeStore()
	if _, err := fs.AddFabric(ctx, mstore.FabricRecord{FabricID: 0xA}); err != nil {
		t.Fatal(err)
	}
	mgrStore := &reloadFailStore{fakeStore: fs}
	mgr, err := groups.NewManager(mgrStore, nil)
	if err != nil {
		t.Fatal(err)
	}
	gkm, err := core.NewGroupKeyManagement(fs, core.GroupKeyMgmtConfig{Groups: mgr})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gkm.MatterInvoke(fabricCtx(1), 0x00, validKeySet(7)); err != nil {
		t.Fatalf("KeySetWrite: %v", err)
	}
	mgrStore.fail.Store(true)
	if err := gkm.MatterWrite(fabricCtx(1), 0x0000, []core.GroupKeyMapStruct{{GroupID: 5, GroupKeySetID: 7}}); err != nil {
		t.Fatalf("GroupKeyMap write the store took, its reload failing: %v, want success", err)
	}
	if list, _ := fs.ListGroupKeyMappings(ctx, 1); len(list) != 1 {
		t.Fatalf("stored mappings = %d, want 1", len(list))
	}
	if ok, _ := mgr.HasKeyMapping(ctx, 1, 5); ok {
		t.Fatal("the group state took the mapping although its reload failed")
	}
	mgrStore.fail.Store(false)
	if _, err := gkm.MatterInvoke(fabricCtx(1), 0x04, nil); err != nil {
		t.Fatalf("KeySetReadAllIndices: %v", err)
	}
	if ok, err := mgr.HasKeyMapping(ctx, 1, 5); !ok || err != nil {
		t.Errorf("group state after the next command: mapping held %v (%v), want the stored mapping reloaded", ok, err)
	}
}
