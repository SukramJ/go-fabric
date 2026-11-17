// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/im"
)

// bindingTableSize is connectedhomeip's Binding::Table::kMaxBindingEntries:
// CHIP_CONFIG_MAX_BINDING_ENTRIES_PER_FABRIC (4) times CHIP_CONFIG_MAX_FABRICS
// (16) (src/app/clusters/bindings/binding-table.h:134-135,
// src/lib/core/CHIPConfig.h:1149 and :799 at 6170af84).
const bindingTableSize = 4 * 16

// groupTargets is n group entries, groups first to first+n-1.
func groupTargets(first, n int) []core.TargetStruct {
	out := make([]core.TargetStruct, 0, n)
	for i := range n {
		out = append(out, core.TargetStruct{Group: uint16(first + i)}) //nolint:gosec // the test's groups stay far below 65536
	}
	return out
}

// bindingWriteStatus is the IM status of a Binding write's error.
func bindingWriteStatus(err error) im.StatusCode {
	if err == nil {
		return im.StatusSuccess
	}
	var s interface{ MatterStatusCode() im.StatusCode }
	if errors.As(err, &s) {
		return s.MatterStatusCode()
	}
	return im.StatusFailure
}

// bindingFabricShare is a fabric's share of the table
// (bindingMaxEntriesPerFabric): a quarter.
const bindingFabricShare = bindingTableSize / 4

// TestBinding_ListIsBounded is R2-P3-admin-04: the Binding list takes at
// most the table size of entries, every fabric's together, and a write
// that would pass it is answered RESOURCE_EXHAUSTED and changes nothing —
// connectedhomeip's check of a write (src/app/clusters/bindings/BindingCluster.cpp:79-80
// at 6170af84: the table's size, less the accessing fabric's entries on the
// endpoint, plus the written ones, within kMaxBindingEntries). A write that
// does not grow the list is taken. Four fabrics at their full share fill
// the table (R2-P6-bound-1008).
func TestBinding_ListIsBounded(t *testing.T) {
	t.Parallel()
	b := sourcedBinding()
	fabric := func(idx uint8) context.Context {
		return im.WithFabricFilter(context.Background(), true, idx)
	}
	for f := uint8(1); f <= 4; f++ {
		if err := b.MatterWrite(fabric(f), 0x0000, groupTargets(1, bindingFabricShare)); err != nil {
			t.Fatalf("fabric %d writes its %d entries: %v", f, bindingFabricShare, err)
		}
	}
	if st := bindingWriteStatus(b.MatterWrite(fabric(5), 0x0000, groupTargets(1, 1))); st != im.StatusResourceExhausted {
		t.Fatalf("fabric 5 writes one entry past the table: status %v, want RESOURCE_EXHAUSTED", st)
	}
	if v, _ := b.MatterRead(0x0000); len(v.([]core.TargetStruct)) != bindingTableSize {
		t.Fatalf("the refused write changed the list: %d entries", len(v.([]core.TargetStruct)))
	}
	if err := b.MatterWrite(fabric(1), 0x0000, groupTargets(1, bindingFabricShare-1)); err != nil {
		t.Fatalf("fabric 1 shortens its list by one: %v", err)
	}
	if err := b.MatterWrite(fabric(5), 0x0000, groupTargets(1, 1)); err != nil {
		t.Fatalf("fabric 5 writes the one entry the table has room for: %v", err)
	}
	if st := bindingWriteStatus(b.MatterWrite(context.Background(), 0x0000, groupTargets(1, bindingTableSize+1))); st != im.StatusResourceExhausted {
		t.Fatalf("a local writer's list past the table: status %v, want RESOURCE_EXHAUSTED", st)
	}
}

// One fabric cannot fill the table: its write past its share is
// RESOURCE_EXHAUSTED, a list ADD past it too, and another fabric still
// writes its own share (review II, R2-P6-bound-1008).
func TestBinding_AFabricIsHeldToItsShare(t *testing.T) {
	t.Parallel()
	b := sourcedBinding()
	fabric := func(idx uint8) context.Context {
		return im.WithFabricFilter(context.Background(), true, idx)
	}
	if st := bindingWriteStatus(b.MatterWrite(fabric(1), 0x0000, groupTargets(1, bindingTableSize))); st != im.StatusResourceExhausted {
		t.Fatalf("fabric 1 writes the whole table: status %v, want RESOURCE_EXHAUSTED", st)
	}
	if err := b.MatterWrite(fabric(1), 0x0000, groupTargets(1, bindingFabricShare)); err != nil {
		t.Fatalf("fabric 1 writes its share: %v", err)
	}
	if st := bindingWriteStatus(b.MatterWrite(im.WithListAppendWrite(fabric(1)), 0x0000, groupTargets(1, bindingFabricShare+1))); st != im.StatusResourceExhausted {
		t.Fatalf("fabric 1 appends past its share: status %v, want RESOURCE_EXHAUSTED", st)
	}
	if err := b.MatterWrite(fabric(2), 0x0000, groupTargets(1, bindingFabricShare)); err != nil {
		t.Fatalf("fabric 2 writes its share: %v", err)
	}
}
