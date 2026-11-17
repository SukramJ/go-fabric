// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"errors"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
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

// gatedListStore reads the first ListGroupKeySets and then holds its
// answer until a second one enters or the other writer has had gateYields
// turns of the scheduler: a window counted in scheduling turns, not on the
// wall clock. The answer is read before the hold, so a second writer that
// is not kept out stores its key set while the first holds a list without
// it.
type gatedListStore struct {
	*fakeStore
	calls   atomic.Int32
	entered chan struct{}
}

// gateYields bounds gatedListStore's hold.
const gateYields = 10_000

func (s *gatedListStore) ListGroupKeySets(ctx context.Context, fabricIndex uint8) ([]mstore.GroupKeySet, error) {
	switch s.calls.Add(1) {
	case 1:
		sets, err := s.fakeStore.ListGroupKeySets(ctx, fabricIndex)
		for range gateYields {
			select {
			case <-s.entered:
				return sets, err
			default:
				runtime.Gosched()
			}
		}
		return sets, err
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
	var ok, exhausted atomic.Int32
	for _, id := range []uint16{7, 8} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := gkm.MatterInvoke(fabricCtx(1), 0x00, validKeySet(id))
			switch spec.StatusOf(err) {
			case im.StatusSuccess:
				ok.Add(1)
			case im.StatusResourceExhausted:
				exhausted.Add(1)
			default:
				t.Errorf("KeySetWrite %d = %v, want SUCCESS or RESOURCE_EXHAUSTED", id, err)
			}
		}()
	}
	wg.Wait()
	if n, m := ok.Load(), exhausted.Load(); n != 1 || m != 1 {
		t.Errorf("%d new key sets taken and %d refused RESOURCE_EXHAUSTED at one below the cap, want 1 and 1", n, m)
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

// countingReloadStore fails the group state's key-set read while fail is
// set and counts the failed reads.
type countingReloadStore struct {
	*fakeStore
	fail   atomic.Bool
	failed atomic.Int32
}

func (s *countingReloadStore) ListGroupKeySets(ctx context.Context, fabricIndex uint8) ([]mstore.GroupKeySet, error) {
	if s.fail.Load() {
		s.failed.Add(1)
		return nil, errors.New("injected reload failure")
	}
	return s.fakeStore.ListGroupKeySets(ctx, fabricIndex)
}

// A stale group state is reloaded on the clock, not only by the next
// GroupKeyManagement command: after 1 s, then with the wait doubled per
// failed retry; the reload that succeeds unmarks the fabric and the group
// state serves the stored keys; MatterQuiesce disarms the retry (review
// II, R2-P6-err-1701 R2-1).
func TestGKMStaleGroupStateIsRetriedOnTheClock(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	clk := clocktest.New(time.Unix(1_700_000_000, 0))
	fs := newFakeStore()
	if _, err := fs.AddFabric(ctx, mstore.FabricRecord{FabricID: 0xA}); err != nil {
		t.Fatal(err)
	}
	mgrStore := &countingReloadStore{fakeStore: fs}
	mgr, err := groups.NewManager(mgrStore, nil)
	if err != nil {
		t.Fatal(err)
	}
	gkm, err := core.NewGroupKeyManagement(fs, core.GroupKeyMgmtConfig{Groups: mgr, Clock: clk})
	if err != nil {
		t.Fatal(err)
	}
	if err := gkm.MatterWrite(fabricCtx(1), 0x0000, []core.GroupKeyMapStruct{{GroupID: 5, GroupKeySetID: 7}}); err != nil {
		t.Fatal(err)
	}
	if err := mgr.AddEndpointForGroup(ctx, 1, 5, 1, ""); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Load(ctx); err != nil {
		t.Fatal(err)
	}
	mgrStore.fail.Store(true)
	if _, err := gkm.MatterInvoke(fabricCtx(1), 0x00, validKeySet(7)); err != nil {
		t.Fatalf("KeySetWrite the store took: %v", err)
	}
	attempts := mgrStore.failed.Load()
	clk.Advance(time.Second)
	if got := mgrStore.failed.Load(); got <= attempts {
		t.Fatal("no retry after 1 s")
	}
	attempts = mgrStore.failed.Load()
	clk.Advance(time.Second)
	if got := mgrStore.failed.Load(); got != attempts {
		t.Errorf("a retry 1 s after the first; want the wait doubled to 2 s")
	}
	mgrStore.fail.Store(false)
	clk.Advance(time.Second)
	if got := gkm.StaleGroupFabrics(); len(got) != 0 {
		t.Fatalf("stale fabrics after the retry that could read: %v", got)
	}
	if ok, err := mgr.HasUsableKey(ctx, 1, 5); !ok || err != nil {
		t.Errorf("group 5 after the retry: usable key %v, %v; want the stored key set 7", ok, err)
	}
	// The memberships the forget withdrew are back once the retry
	// succeeded (the re-announcement's Load; joined with net's a417899a,
	// which makes Load after ForgetFabric reload the fabric).
	ms, err := mgr.Memberships(ctx)
	if err != nil || !slices.ContainsFunc(ms, func(m groups.Membership) bool { return m.FabricIndex == 1 && m.GroupID == 5 }) {
		t.Errorf("memberships after the retry = %+v, %v; want fabric 1's group 5", ms, err)
	}
	// Quiesced: no retry runs.
	mgrStore.fail.Store(true)
	if _, err := gkm.MatterInvoke(fabricCtx(1), 0x00, validKeySet(8)); err != nil {
		t.Fatal(err)
	}
	gkm.MatterQuiesce()
	attempts = mgrStore.failed.Load()
	clk.Advance(time.Hour)
	if got := mgrStore.failed.Load(); got != attempts {
		t.Errorf("%d reloads after MatterQuiesce, want none", got-attempts)
	}
}

// gatedReloadStore fails the group state's key-set read while fail is set;
// while gate is set the read first says it entered and waits for release.
type gatedReloadStore struct {
	*fakeStore
	fail     atomic.Bool
	gate     atomic.Bool
	failed   atomic.Int32
	entered  chan struct{}
	release  chan struct{}
	failList atomic.Bool
	onList   func() // set before use: counts the re-announcement's listing
}

func (s *gatedReloadStore) ListGroupKeySets(ctx context.Context, fabricIndex uint8) ([]mstore.GroupKeySet, error) {
	if s.gate.CompareAndSwap(true, false) {
		s.entered <- struct{}{}
		<-s.release
	}
	if s.fail.Load() {
		s.failed.Add(1)
		return nil, errors.New("injected reload failure")
	}
	return s.fakeStore.ListGroupKeySets(ctx, fabricIndex)
}

func (s *gatedReloadStore) ListFabrics(ctx context.Context) ([]mstore.FabricRecord, error) {
	if s.onList != nil {
		s.onList()
	}
	if s.failList.Load() {
		return nil, errors.New("injected fabric listing failure")
	}
	return s.fakeStore.ListFabrics(ctx)
}

func newGatedGKM(t *testing.T, clk *clocktest.Fake) (*core.GroupKeyManagement, *gatedReloadStore, *groups.Manager) {
	t.Helper()
	fs := newFakeStore()
	if _, err := fs.AddFabric(context.Background(), mstore.FabricRecord{FabricID: 0xA}); err != nil {
		t.Fatal(err)
	}
	st := &gatedReloadStore{fakeStore: fs, entered: make(chan struct{}), release: make(chan struct{})}
	mgr, err := groups.NewManager(st, nil)
	if err != nil {
		t.Fatal(err)
	}
	gkm, err := core.NewGroupKeyManagement(fs, core.GroupKeyMgmtConfig{Groups: mgr, Clock: clk})
	if err != nil {
		t.Fatal(err)
	}
	return gkm, st, mgr
}

// MatterQuiesce while a retry's reload runs: the failing reload arms no
// next retry (review II, R2-P6-err-1701 R3-a).
func TestGKMRetryRunningAtQuiesceArmsNoNext(t *testing.T) {
	t.Parallel()
	clk := clocktest.New(time.Unix(1_700_000_000, 0))
	gkm, st, _ := newGatedGKM(t, clk)
	st.fail.Store(true)
	if _, err := gkm.MatterInvoke(fabricCtx(1), 0x00, validKeySet(7)); err != nil {
		t.Fatal(err)
	}
	st.gate.Store(true)
	advanced := make(chan struct{})
	go func() {
		defer close(advanced)
		clk.Advance(time.Second)
	}()
	<-st.entered
	gkm.MatterQuiesce()
	close(st.release)
	<-advanced
	before := st.failed.Load()
	clk.Advance(time.Hour)
	if got := st.failed.Load(); got != before {
		t.Errorf("%d reloads after MatterQuiesce, want none: the running retry armed another", got-before)
	}
	if len(gkm.StaleGroupFabrics()) != 1 {
		t.Errorf("stale fabrics = %v, want fabric 1 still marked", gkm.StaleGroupFabrics())
	}
}

// A reload that succeeds whose re-announcement of the memberships fails
// keeps the fabric marked and the retry owed; the retry that announces
// unmarks it (review II, R2-P6-err-1701 R3-2).
func TestGKMFailedReannounceKeepsTheFabricStale(t *testing.T) {
	t.Parallel()
	clk := clocktest.New(time.Unix(1_700_000_000, 0))
	gkm, st, _ := newGatedGKM(t, clk)
	st.fail.Store(true)
	if _, err := gkm.MatterInvoke(fabricCtx(1), 0x00, validKeySet(7)); err != nil {
		t.Fatal(err)
	}
	st.fail.Store(false)
	st.failList.Store(true) // Load's listing of the fabrics fails
	if _, err := gkm.MatterInvoke(fabricCtx(1), 0x04, nil); err != nil {
		t.Fatal(err)
	}
	if got := gkm.StaleGroupFabrics(); len(got) != 1 {
		t.Fatalf("stale fabrics after a failed re-announcement = %v, want fabric 1", got)
	}
	st.failList.Store(false)
	clk.Advance(staleRetryWindow)
	if got := gkm.StaleGroupFabrics(); len(got) != 0 {
		t.Errorf("stale fabrics after the retry = %v, want none", got)
	}
}

// staleRetryWindow covers the first retries' waits (1 s, 2 s).
const staleRetryWindow = 3 * time.Second

// A sole stale fabric whose reload succeeds and whose re-announcement keeps
// failing is retried with the backoff going on — after 1 s, 2 s, 4 s —
// not every second (review II, R2-P6-err-1701 R4-1).
func TestGKMFailedReannounceKeepsTheBackoff(t *testing.T) {
	t.Parallel()
	clk := clocktest.New(time.Unix(1_700_000_000, 0))
	gkm, st, _ := newGatedGKM(t, clk)
	var loads atomic.Int32
	st.onList = func() { loads.Add(1) }
	st.fail.Store(true)
	if _, err := gkm.MatterInvoke(fabricCtx(1), 0x00, validKeySet(7)); err != nil {
		t.Fatal(err)
	}
	st.fail.Store(false)
	st.failList.Store(true) // every re-announcement fails
	for i, wait := range []time.Duration{time.Second, 2 * time.Second, 4 * time.Second} {
		before := loads.Load()
		clk.Advance(wait - time.Millisecond)
		if got := loads.Load(); got != before {
			t.Fatalf("retry %d ran before its wait of %v", i+1, wait)
		}
		clk.Advance(time.Millisecond)
		if got := loads.Load(); got == before {
			t.Fatalf("retry %d did not run after %v", i+1, wait)
		}
	}
	if got := gkm.StaleGroupFabrics(); len(got) != 1 {
		t.Errorf("stale fabrics = %v, want fabric 1 still marked", got)
	}
}
