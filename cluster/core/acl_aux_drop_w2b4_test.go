// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"

	"thingfabric.dev/go-fabric/cluster"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/store"
)

var errAuxDownW2B4 = errors.New("auxiliary source down")

// auxSourceW2B4 is an auxiliary source that counts its reads, fails while
// failing is set, and — while hold is armed — parks its next read until
// release is closed, after closing entered.
type auxSourceW2B4 struct {
	reads   atomic.Int32
	failing atomic.Bool

	mu      sync.Mutex
	hold    bool
	entered chan struct{}
	release chan struct{}
}

func (s *auxSourceW2B4) AuxiliaryACL(context.Context, uint8) ([]store.ACLEntry, error) {
	s.reads.Add(1)
	s.mu.Lock()
	hold := s.hold
	s.hold = false
	s.mu.Unlock()
	if hold {
		close(s.entered)
		<-s.release
	}
	if s.failing.Load() {
		return nil, errAuxDownW2B4
	}
	return nil, nil
}

func (s *auxSourceW2B4) Fabrics(context.Context) ([]uint8, error) { return nil, nil }

func (s *auxSourceW2B4) OnGroupcastChanged(func(context.Context, uint8)) {}

func (s *auxSourceW2B4) armHold() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hold = true
	s.entered = make(chan struct{})
	s.release = make(chan struct{})
}

// levelCountW2B4 counts the records of one message per level.
type levelCountW2B4 struct {
	msg string

	mu  sync.Mutex
	got map[slog.Level]int
}

func (h *levelCountW2B4) Enabled(context.Context, slog.Level) bool { return true }

func (h *levelCountW2B4) Handle(_ context.Context, r slog.Record) error {
	if r.Message != h.msg {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.got == nil {
		h.got = map[slog.Level]int{}
	}
	h.got[r.Level]++
	return nil
}

func (h *levelCountW2B4) WithAttrs([]slog.Attr) slog.Handler { return h }

func (h *levelCountW2B4) WithGroup(string) slog.Handler { return h }

func (h *levelCountW2B4) count(l slog.Level) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.got[l]
}

func newAuxACLW2B4(t *testing.T, st ACLStoreFacade) (*AccessControl, *auxSourceW2B4, *levelCountW2B4) {
	t.Helper()
	ctx := context.Background()
	a, err := NewAccessControlWith(ctx, AccessControlConfig{Store: st})
	if err != nil {
		t.Fatal(err)
	}
	h := &levelCountW2B4{msg: "matter.access_control.auxiliary_sync"}
	a.SetLogger(slog.New(h))
	src := &auxSourceW2B4{}
	a.registerAuxiliaryProvider(ctx, src)
	src.reads.Store(0)
	return a, src, h
}

// TestAccessControlFailedSyncKeepsNoDroppedFabricContextW2B4 (#619,
// P8-PR1-fable-L2): a synchronisation takes the owed contexts out of the
// field before it reads the source; a fabric dropped while that read is in
// flight and fails gets its context back from the failure's write-back,
// and an event of a later fabric at the index would name the dropped
// fabric's administrator. After the failed read no owed context of the
// dropped fabric remains; another fabric's stays owed.
func TestAccessControlFailedSyncKeepsNoDroppedFabricContextW2B4(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const f, other uint8 = 3, 1
	st := &dropACLStore{acl: map[uint8][]store.ACLEntry{f: {dropAdmin(f)}}, fabrics: []uint8{f}}
	a, src, _ := newAuxACLW2B4(t, st)

	a.mu.Lock()
	a.auxOwed, a.auxOwedEmit = true, true
	a.auxOwedCtx = map[uint8]context.Context{
		f:     im.WithFabricFilter(ctx, true, f),
		other: im.WithFabricFilter(ctx, true, other),
	}
	a.mu.Unlock()

	src.failing.Store(true)
	src.armHold()
	done := make(chan struct{})
	go func() {
		defer close(done)
		a.MatterRead(cluster.AttrGlobalClusterRevision) // settles: takes the owed contexts
	}()
	<-src.entered
	st.removeFabric(f)
	if err := a.MatterDropFabric(ctx, f); err != nil {
		t.Fatal(err)
	}
	close(src.release)
	<-done

	a.mu.RLock()
	defer a.mu.RUnlock()
	if !a.auxOwed {
		t.Fatal("the failed synchronisation owes nothing")
	}
	if _, ok := a.auxOwedCtx[f]; ok {
		t.Error("the failed synchronisation put the dropped fabric's owed context back")
	}
	if _, ok := a.auxOwedCtx[other]; !ok {
		t.Error("the failed synchronisation lost fabric 1's owed context")
	}
}

// TestAccessControlDropClearsTheBatchDeferredContextW2B4 (#619, batch
// part; fable-diff-b4 4b): a batch records fabric 3's change context, the
// fabric is dropped while the batch is open, and the batch's closing sync
// fails to read the source: the dropped fabric's context is not owed after
// it.
func TestAccessControlDropClearsTheBatchDeferredContextW2B4(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const f uint8 = 3
	st := &dropACLStore{acl: map[uint8][]store.ACLEntry{f: {dropAdmin(f)}}, fabrics: []uint8{f}}
	a, src, _ := newAuxACLW2B4(t, st)

	a.beginAuxiliaryBatch()
	a.mu.Lock()
	a.auxDeferred = map[uint8]context.Context{f: im.WithFabricFilter(ctx, true, f)}
	a.mu.Unlock()
	st.removeFabric(f)
	if err := a.MatterDropFabric(ctx, f); err != nil {
		t.Fatal(err)
	}
	src.failing.Store(true)
	a.endAuxiliaryBatch(ctx)

	a.mu.RLock()
	defer a.mu.RUnlock()
	if !a.auxOwed {
		t.Fatal("the failed synchronisation owes nothing")
	}
	if _, ok := a.auxOwedCtx[f]; ok {
		t.Error("the batch's failed synchronisation owes the dropped fabric's context")
	}
}

// TestAccessControlDropBeforeTheSyncsTakeIsSeenW2B4 (#619, batch part): the
// batch's close takes its recorded contexts and then waits for auxSyncMu;
// a drop of fabric 3 in that wait is seen by the epoch the close noted, so
// the failed synchronisation owes no context of fabric 3. Not parallel:
// WaitParkedOnMutexA1 reads every goroutine's stack.
func TestAccessControlDropBeforeTheSyncsTakeIsSeenW2B4(t *testing.T) {
	ctx := context.Background()
	const f uint8 = 3
	st := &dropACLStore{acl: map[uint8][]store.ACLEntry{f: {dropAdmin(f)}}, fabrics: []uint8{f}}
	a, src, _ := newAuxACLW2B4(t, st)

	a.beginAuxiliaryBatch()
	a.mu.Lock()
	a.auxDeferred = map[uint8]context.Context{f: im.WithFabricFilter(ctx, true, f)}
	a.mu.Unlock()
	src.failing.Store(true)

	a.auxSyncMu.Lock()
	done := make(chan struct{})
	go func() {
		defer close(done)
		a.endAuxiliaryBatch(ctx)
	}()
	if !WaitParkedOnMutexA1(t, done, "syncAuxiliaryAs") {
		a.auxSyncMu.Unlock()
		t.Fatal("the batch's close did not wait for auxSyncMu")
	}
	st.removeFabric(f)
	if err := a.MatterDropFabric(ctx, f); err != nil {
		t.Fatal(err)
	}
	a.auxSyncMu.Unlock()
	<-done

	a.mu.RLock()
	defer a.mu.RUnlock()
	if !a.auxOwed {
		t.Fatal("the failed synchronisation owes nothing")
	}
	if _, ok := a.auxOwedCtx[f]; ok {
		t.Error("a drop between the batch's take and the synchronisation's take left the fabric's context owed")
	}
}
