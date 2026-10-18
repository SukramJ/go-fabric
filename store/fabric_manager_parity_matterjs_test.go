// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package store_test

// A port of matter.js packages/protocol/test/fabric/FabricManagerTest.ts at
// 862ad75f: the next fabric index the fabric table allocates.
//
// matter.js FabricManager.allocateFabricIndex hands out the index and
// advances its counter; the Go fabric table allocates inside AddFabric with
// index 0, so "the next index" is the index the next auto-assigned AddFabric
// receives. TestFabric({ fabrics }) is an AddFabric, fabric.delete a
// RemoveFabric.

import (
	"context"
	"errors"
	"testing"

	store "github.com/SukramJ/go-fabric/store"
)

// fabricManagerTable is a fabric table whose fabrics get distinct keys.
type fabricManagerTable struct {
	t    *testing.T
	s    *store.Store
	seed byte
}

func newFabricManagerTable(t *testing.T) *fabricManagerTable {
	t.Helper()
	return &fabricManagerTable{t: t, s: store.New(openTestDB(t))}
}

// add is TestFabric({ fabrics }): it allocates the next index and adds a
// fabric there.
func (f *fabricManagerTable) add() (uint8, error) {
	f.t.Helper()
	f.seed++
	return f.s.AddFabric(context.Background(), newFabricRecord(0, f.seed))
}

func (f *fabricManagerTable) mustAdd() uint8 {
	f.t.Helper()
	idx, err := f.add()
	if err != nil {
		f.t.Fatalf("AddFabric: %v", err)
	}
	return idx
}

func (f *fabricManagerTable) remove(idx uint8) {
	f.t.Helper()
	if err := f.s.RemoveFabric(context.Background(), idx); err != nil {
		f.t.Fatalf("RemoveFabric(%d): %v", idx, err)
	}
}

// fill adds fabrics 1..254 (FabricManagerTest.ts:254-256).
func (f *fabricManagerTable) fill() {
	f.t.Helper()
	for range 254 {
		f.mustAdd()
	}
}

func (f *fabricManagerTable) wantNext(want uint8) {
	f.t.Helper()
	if got := f.mustAdd(); got != want {
		f.t.Fatalf("allocateFabricIndex() = %d, want %d", got, want)
	}
}

func TestFabricManagerDetermineNextFabricIndex(t *testing.T) {
	t.Parallel()

	t.Run("get next fabric index initially", func(t *testing.T) {
		t.Parallel()
		newFabricManagerTable(t).wantNext(1)
	})

	t.Run("get next fabric index after adding fabric", func(t *testing.T) {
		t.Parallel()
		f := newFabricManagerTable(t)
		f.mustAdd()
		f.wantNext(2)
	})

	t.Run("get next fabric index after adding fabric and removing it", func(t *testing.T) {
		t.Parallel()
		f := newFabricManagerTable(t)
		f.remove(f.mustAdd())
		f.wantNext(2)
	})

	t.Run("get next fabric index after adding fabric and revoking it", func(t *testing.T) {
		t.Parallel()
		f := newFabricManagerTable(t)
		f.remove(f.mustAdd())
		f.wantNext(2)
	})

	t.Run("get jumps over one fabric index after adding fabric and revoking it", func(t *testing.T) {
		t.Parallel()
		f := newFabricManagerTable(t)
		first := f.mustAdd() // index 1
		f.mustAdd()          // index 2
		f.remove(first)
		f.wantNext(3)
	})

	t.Run("it find first open slot for next index", func(t *testing.T) {
		t.Parallel()
		f := newFabricManagerTable(t)
		f.fill()
		f.remove(100)
		f.wantNext(100)
	})

	t.Run("it find first open slot for next index id including overflow", func(t *testing.T) {
		t.Parallel()
		f := newFabricManagerTable(t)
		f.fill()
		f.remove(1)
		f.wantNext(1)
	})

	t.Run("throws when table is full", func(t *testing.T) {
		t.Parallel()
		f := newFabricManagerTable(t)
		f.fill()
		if idx, err := f.add(); !errors.Is(err, store.ErrFabricExhausted) {
			t.Fatalf("allocateFabricIndex() = %d, %v; want FabricTableFullError (ErrFabricExhausted)", idx, err)
		}
	})
}
