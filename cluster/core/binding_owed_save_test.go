// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"errors"
	"testing"
)

// owedBindingStore is a stored Binding list whose next save fails on
// request.
type owedBindingStore struct {
	stored   []TargetStruct
	failNext bool
	saves    int
}

func (s *owedBindingStore) config() BindingConfig {
	return BindingConfig{
		Load: func() ([]TargetStruct, error) { return s.stored, nil },
		Save: func(l []TargetStruct) error {
			s.saves++
			if s.failNext {
				s.failNext = false
				return errors.New("store down")
			}
			s.stored = append([]TargetStruct{}, l...)
			return nil
		},
	}
}

func persistedBinding(t *testing.T, s *owedBindingStore) *Binding {
	t.Helper()
	b := NewBinding()
	if err := b.Persist(s.config()); err != nil {
		t.Fatal(err)
	}
	return b
}

// TestBindingDropOwesAFailedSave (R3-store-3052): a removed fabric's drop
// whose save fails leaves the save owed; the next drop — of any index, also
// one that finds nothing in memory, as the node's drop for an index a new
// fabric was installed at — stores the list, so a restart restores no
// entry of the removed fabric.
func TestBindingDropOwesAFailedSave(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := &owedBindingStore{}
	b := persistedBinding(t, s)
	if err := b.SetAttributes(map[uint32]any{bindingAttrBinding: []TargetStruct{{Node: 0x1234, Endpoint: 1, FabricIndex: 3}}}); err != nil {
		t.Fatal(err)
	}
	s.failNext = true
	if err := b.MatterDropFabric(ctx, 3); err == nil {
		t.Fatal("the drop whose save failed answered nil")
	}
	if held := b.MatterHeldFabrics(); len(held) != 0 {
		t.Fatalf("memory holds fabrics %v after the drop", held)
	}
	if err := b.MatterDropFabric(ctx, 3); err != nil {
		t.Fatalf("the second drop: %v", err)
	}
	if held := persistedBinding(t, s).MatterHeldFabrics(); len(held) != 0 {
		t.Errorf("after a restart the server holds fabrics %v: the owed save was never made", held)
	}
	// Paid: a further drop that finds nothing saves nothing.
	before := s.saves
	if err := b.MatterDropFabric(ctx, 4); err != nil {
		t.Fatal(err)
	}
	if s.saves != before {
		t.Errorf("a drop with nothing owed saved %d times", s.saves-before)
	}
}

// TestBindingWriteSettlesAnOwedSave (R3-store-3052): a write equal to the
// current list commits nothing, but it stores the list when a drop's save
// is owed.
func TestBindingWriteSettlesAnOwedSave(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := &owedBindingStore{}
	b := persistedBinding(t, s)
	kept := TargetStruct{Node: 0x55, Endpoint: 1, FabricIndex: 1}
	if err := b.SetAttributes(map[uint32]any{bindingAttrBinding: []TargetStruct{kept, {Node: 0x1234, Endpoint: 1, FabricIndex: 3}}}); err != nil {
		t.Fatal(err)
	}
	s.failNext = true
	_ = b.MatterDropFabric(ctx, 3)
	if err := b.SetAttributes(map[uint32]any{bindingAttrBinding: []TargetStruct{kept}}); err != nil {
		t.Fatal(err)
	}
	if held := persistedBinding(t, s).MatterHeldFabrics(); len(held) != 1 || held[0] != 1 {
		t.Errorf("after a restart the server holds fabrics %v, want [1]", held)
	}
}
