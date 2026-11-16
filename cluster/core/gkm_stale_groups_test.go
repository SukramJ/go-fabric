// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/spec"
	gkmdef "github.com/SukramJ/go-fabric/cluster/spec/groupkeymanagement"
	"github.com/SukramJ/go-fabric/groups"
	"github.com/SukramJ/go-fabric/im"
	mstore "github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/store/memstore"
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

// KeySetWrite refuses a null EpochKey1 / EpochKey2 whose start time is
// not null — a start time of 0 included — with InvalidCommand, as matter.js
// does; the generated request carries the null (review II, R2-P6-par-1013).
func TestGKMKeySetWriteNullKeyWithAStartTimeIsInvalidCommand(t *testing.T) {
	t.Parallel()
	base := func() gkmdef.KeySetWriteRequest {
		return gkmdef.KeySetWriteRequest{GroupKeySet: gkmdef.GroupKeySetStruct{
			GroupKeySetId:          7,
			GroupKeySecurityPolicy: gkmdef.GroupKeySecurityPolicyTrustFirst,
			EpochKey0:              spec.Nullable[[]byte]{Value: make([]byte, 16)},
			EpochStartTime0:        spec.Nullable[uint64]{Value: 1000},
			EpochKey1:              spec.Nullable[[]byte]{Null: true},
			EpochStartTime1:        spec.Nullable[uint64]{Null: true},
			EpochKey2:              spec.Nullable[[]byte]{Null: true},
			EpochStartTime2:        spec.Nullable[uint64]{Null: true},
		}}
	}
	gkm, err := core.NewGroupKeyManagement(newFakeStore(), core.GroupKeyMgmtConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gkm.MatterInvoke(fabricCtx(1), 0x00, base()); err != nil {
		t.Fatalf("KeySetWrite with both later epochs null: %v", err)
	}
	slot1 := base()
	slot1.GroupKeySet.EpochStartTime1 = spec.Nullable[uint64]{Value: 0}
	slot2 := base()
	slot2.GroupKeySet.EpochStartTime2 = spec.Nullable[uint64]{Value: 0}
	for name, req := range map[string]gkmdef.KeySetWriteRequest{"EpochKey1 null, EpochStartTime1 0": slot1, "EpochKey2 null, EpochStartTime2 0": slot2} {
		if _, err := gkm.MatterInvoke(fabricCtx(1), 0x00, req); spec.StatusOf(err) != im.StatusInvalidCommand {
			t.Errorf("%s: %v, want InvalidCommand", name, err)
		}
	}
}

// gatedListStore holds the first ListGroupKeySets until a second one
// enters or a short wait passes.
type gatedListStore struct {
	*fakeStore
	calls   atomic.Int32
	entered chan struct{}
}

func (s *gatedListStore) ListGroupKeySets(ctx context.Context, fabricIndex uint8) ([]mstore.GroupKeySet, error) {
	switch s.calls.Add(1) {
	case 1:
		select {
		case <-s.entered:
		case <-time.After(300 * time.Millisecond):
		}
	case 2:
		close(s.entered)
	}
	return s.fakeStore.ListGroupKeySets(ctx, fabricIndex)
}

// Two KeySetWrites of new ids one below the cap: the budget check and the
// store are one step, so only one is taken (review II, R2-P6-bound-1015).
func TestGKMConcurrentKeySetWritesKeepTheBudget(t *testing.T) {
	t.Parallel()
	st := &gatedListStore{fakeStore: newFakeStore(), entered: make(chan struct{})}
	gkm, err := core.NewGroupKeyManagement(st, core.GroupKeyMgmtConfig{MaxGroupKeysPerFabric: 2})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var ok atomic.Int32
	for _, id := range []uint16{7, 8} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := gkm.MatterInvoke(fabricCtx(1), 0x00, validKeySet(id)); err == nil {
				ok.Add(1)
			}
		}()
	}
	wg.Wait()
	if n := ok.Load(); n != 1 {
		t.Errorf("%d new key sets taken at one below the cap, want 1", n)
	}
}

// A GroupKeyMap write that only reorders the held bindings is a change: it
// is stored and advances the DataVersion; the held list written again in
// its order is not (owner decision 148, F-38 server half). Over memstore
// and over the in-memory fake, which keeps the written order.
func TestGKMGroupKeyMapReorderIsAChange(t *testing.T) {
	t.Parallel()
	for name, st := range map[string]core.GroupStoreFacade{"memstore": memstore.New(), "fake": newFakeStore()} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			if _, err := st.(interface {
				AddFabric(context.Context, mstore.FabricRecord) (uint8, error)
			}).AddFabric(ctx, mstore.FabricRecord{FabricID: 0xA, RootPublicKey: []byte{4, 1}}); err != nil {
				t.Fatal(err)
			}
			gkm, err := core.NewGroupKeyManagement(st, core.GroupKeyMgmtConfig{})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := gkm.MatterInvoke(fabricCtx(1), 0x00, validKeySet(7)); err != nil {
				t.Fatal(err)
			}
			held, err := st.ListGroupKeyMappings(ctx, 1)
			if err != nil || len(held) != 0 {
				t.Fatalf("held mappings = %v, %v", held, err)
			}
			first := []core.GroupKeyMapStruct{{GroupID: 3, GroupKeySetID: 7}, {GroupID: 5, GroupKeySetID: 7}}
			if changed, err := gkm.MatterWriteChanged(fabricCtx(1), 0x0000, first); err != nil || !changed {
				t.Fatalf("first write: changed %v, %v", changed, err)
			}
			v := gkm.MatterDataVersion()
			if changed, err := gkm.MatterWriteChanged(fabricCtx(1), 0x0000, first); err != nil || changed {
				t.Errorf("the held list again, in its order: changed %v, %v; want unchanged", changed, err)
			}
			reordered := []core.GroupKeyMapStruct{{GroupID: 5, GroupKeySetID: 7}, {GroupID: 3, GroupKeySetID: 7}}
			if changed, err := gkm.MatterWriteChanged(fabricCtx(1), 0x0000, reordered); err != nil || !changed {
				t.Errorf("a reorder-only write: changed %v, %v; want stored", changed, err)
			}
			if gkm.MatterDataVersion() == v {
				t.Error("a reorder-only write left the DataVersion")
			}
		})
	}
}

// A KeySetRemove the store took, whose reload of the group state fails,
// answers success and fails closed: the removed key set no longer
// authenticates group messages before any reload succeeded; the reload
// that succeeds re-announces the memberships; a stale fabric that is
// removed is unmarked (review II, R2-P6-err-1701 R-1).
func TestGKMFailedReloadAfterARemovalFailsClosed(t *testing.T) {
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
	var announced atomic.Int32
	mgr.OnMembershipChanged(func() { announced.Add(1) })
	gkm, err := core.NewGroupKeyManagement(fs, core.GroupKeyMgmtConfig{Groups: mgr})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gkm.MatterInvoke(fabricCtx(1), 0x00, validKeySet(7)); err != nil {
		t.Fatal(err)
	}
	if err := gkm.MatterWrite(fabricCtx(1), 0x0000, []core.GroupKeyMapStruct{{GroupID: 5, GroupKeySetID: 7}}); err != nil {
		t.Fatal(err)
	}
	if ok, err := mgr.HasUsableKey(ctx, 1, 5); !ok || err != nil {
		t.Fatalf("group 5 before the removal: usable key %v, %v", ok, err)
	}
	mgrStore.fail.Store(true)
	if _, err := gkm.MatterInvoke(fabricCtx(1), 0x03, core.KeySetRemoveRequest{GroupKeySetID: 7}); err != nil {
		t.Fatalf("KeySetRemove the store took: %v, want success", err)
	}
	if ok, _ := mgr.HasUsableKey(ctx, 1, 5); ok {
		t.Fatal("the removed key set still serves group 5 while the group state could not reload")
	}
	mgrStore.fail.Store(false)
	before := announced.Load()
	if _, err := gkm.MatterInvoke(fabricCtx(1), 0x04, nil); err != nil {
		t.Fatal(err)
	}
	if ok, _ := mgr.HasUsableKey(ctx, 1, 5); ok {
		t.Error("group 5 has a usable key after the reload; key set 7 was removed")
	}
	if len(gkm.StaleGroupFabrics()) != 0 {
		t.Errorf("stale fabrics after the reload succeeded: %v", gkm.StaleGroupFabrics())
	}
	if announced.Load() == before {
		t.Error("the recovered reload did not re-announce the memberships")
	}
	// A stale fabric whose row goes is unmarked at the next command.
	mgrStore.fail.Store(true)
	if _, err := gkm.MatterInvoke(fabricCtx(1), 0x00, validKeySet(8)); err != nil {
		t.Fatal(err)
	}
	mgrStore.fail.Store(false)
	if err := fs.RemoveFabric(ctx, 1); err != nil {
		t.Fatal(err)
	}
	_, _ = gkm.MatterInvoke(fabricCtx(2), 0x04, nil)
	if got := gkm.StaleGroupFabrics(); len(got) != 0 {
		t.Errorf("stale fabrics after the fabric's removal: %v, want none", got)
	}
}
