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
// (16) (src/app/clusters/bindings/binding-table.h:133-134,
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

// TestBinding_ListIsBounded is R2-P3-admin-04: the Binding list takes at
// most the table size of entries, every fabric's together, and a write
// that would pass it is answered RESOURCE_EXHAUSTED and changes nothing —
// connectedhomeip's check of a write (src/app/clusters/bindings/BindingCluster.cpp:79-80
// at 6170af84: the table's size, less the accessing fabric's entries on the
// endpoint, plus the written ones, within kMaxBindingEntries). A write that
// does not grow the list is taken.
func TestBinding_ListIsBounded(t *testing.T) {
	t.Parallel()
	b := core.NewBinding()
	fabric := func(idx uint8) context.Context {
		return im.WithFabricFilter(context.Background(), true, idx)
	}
	if err := b.MatterWrite(fabric(1), 0x0000, groupTargets(1, bindingTableSize)); err != nil {
		t.Fatalf("fabric 1 writes %d entries: %v", bindingTableSize, err)
	}
	if st := bindingWriteStatus(b.MatterWrite(fabric(2), 0x0000, groupTargets(1, 1))); st != im.StatusResourceExhausted {
		t.Fatalf("fabric 2 writes one entry past the table: status %v, want RESOURCE_EXHAUSTED", st)
	}
	if v, _ := b.MatterRead(0x0000); len(v.([]core.TargetStruct)) != bindingTableSize {
		t.Fatalf("the refused write changed the list: %d entries", len(v.([]core.TargetStruct)))
	}
	// A list ADD arrives as the whole list with one entry appended.
	if st := bindingWriteStatus(b.MatterWrite(im.WithListAppendWrite(fabric(1)), 0x0000, groupTargets(1, bindingTableSize+1))); st != im.StatusResourceExhausted {
		t.Fatalf("fabric 1 appends past the table: status %v, want RESOURCE_EXHAUSTED", st)
	}
	if err := b.MatterWrite(fabric(1), 0x0000, groupTargets(1, bindingTableSize-1)); err != nil {
		t.Fatalf("fabric 1 shortens its list by one: %v", err)
	}
	if err := b.MatterWrite(fabric(2), 0x0000, groupTargets(1, 1)); err != nil {
		t.Fatalf("fabric 2 writes the one entry the table has room for: %v", err)
	}
	if st := bindingWriteStatus(b.MatterWrite(context.Background(), 0x0000, groupTargets(1, bindingTableSize+1))); st != im.StatusResourceExhausted {
		t.Fatalf("a local writer's list past the table: status %v, want RESOURCE_EXHAUSTED", st)
	}
}
