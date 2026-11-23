// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"errors"
	"testing"

	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/store"
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

// owedFabrics is a fabric source holding the listed indices.
type owedFabrics []uint8

func (p owedFabrics) ListFabrics(context.Context) ([]store.FabricRecord, error) {
	out := make([]store.FabricRecord, len(p))
	for i, f := range p {
		out[i].FabricIndex = f
	}
	return out, nil
}

// TestBindingUnchangedWritesSettleAnOwedSave (R4-store-4001) holds every
// write route to the promise of the saveOwed field: a write that changes
// nothing still stores the list when a drop's save is owed. A local
// MatterWriteChanged and a controller's request write compare the held
// entries before replace and returned there without the settle.
func TestBindingUnchangedWritesSettleAnOwedSave(t *testing.T) {
	t.Parallel()
	kept := TargetStruct{Node: 0x55, Endpoint: 1, FabricIndex: 1}
	routes := map[string]func(ctx context.Context, b *Binding) error{
		"SetAttributes": func(_ context.Context, b *Binding) error {
			return b.SetAttributes(map[uint32]any{bindingAttrBinding: []TargetStruct{kept}})
		},
		"local MatterWriteChanged": func(ctx context.Context, b *Binding) error {
			_, err := b.MatterWriteChanged(ctx, bindingAttrBinding, []TargetStruct{kept})
			return err
		},
		"request write of the remaining fabric": func(ctx context.Context, b *Binding) error {
			_, err := b.MatterWriteChanged(im.WithFabricFilter(ctx, true, 1), bindingAttrBinding, []TargetStruct{kept})
			return err
		},
	}
	for name, write := range routes {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			s := &owedBindingStore{}
			cfg := s.config()
			cfg.Fabrics = owedFabrics{1}
			b := NewBinding()
			if err := b.Persist(cfg); err != nil {
				t.Fatal(err)
			}
			if err := b.SetAttributes(map[uint32]any{bindingAttrBinding: []TargetStruct{kept, {Node: 0x1234, Endpoint: 1, FabricIndex: 3}}}); err != nil {
				t.Fatal(err)
			}
			s.failNext = true
			if err := b.MatterDropFabric(ctx, 3); err == nil {
				t.Fatal("the drop whose save failed answered nil")
			}
			if err := write(ctx, b); err != nil {
				t.Fatal(err)
			}
			if held := persistedBinding(t, s).MatterHeldFabrics(); len(held) != 1 || held[0] != 1 {
				t.Errorf("after a restart the server holds fabrics %v, want [1]", held)
			}
			// Paid: the same write again saves nothing.
			before := s.saves
			if err := write(ctx, b); err != nil {
				t.Fatal(err)
			}
			if s.saves != before {
				t.Errorf("an unchanged write with nothing owed saved %d times", s.saves-before)
			}
		})
	}
}

// TestBindingIndexReuseAfterTwoFailedDrops (R4-store-4001): the removal's
// drop and the install's drop at the reused index both fail to save; the
// new fabric's write of the empty list it holds stores the list, so a
// restart does not give it the removed fabric's entry.
func TestBindingIndexReuseAfterTwoFailedDrops(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := &owedBindingStore{}
	fail := 0
	cfg := s.config()
	inner := cfg.Save
	cfg.Save = func(l []TargetStruct) error {
		if fail > 0 {
			fail--
			return errors.New("store down")
		}
		return inner(l)
	}
	cfg.Fabrics = owedFabrics{3}
	b := NewBinding()
	if err := b.Persist(cfg); err != nil {
		t.Fatal(err)
	}
	if err := b.SetAttributes(map[uint32]any{bindingAttrBinding: []TargetStruct{{Node: 0x1234, Endpoint: 1, FabricIndex: 3}}}); err != nil {
		t.Fatal(err)
	}
	fail = 2
	if err := b.MatterDropFabric(ctx, 3); err == nil {
		t.Fatal("the removal's drop answered nil")
	}
	if err := b.MatterDropFabric(ctx, 3); err == nil {
		t.Fatal("the install's drop answered nil")
	}
	if _, err := b.MatterWriteChanged(im.WithFabricFilter(ctx, true, 3), bindingAttrBinding, []TargetStruct{}); err != nil {
		t.Fatal(err)
	}
	if held := persistedBinding(t, s).MatterHeldFabrics(); len(held) != 0 {
		t.Errorf("after a restart the server holds fabrics %v, want none", held)
	}
}
