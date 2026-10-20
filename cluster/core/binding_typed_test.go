// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/spec"
	bindingdef "github.com/SukramJ/go-fabric/cluster/spec/binding"
	"github.com/SukramJ/go-fabric/im"
)

// TestTypedBindingServer drives core.Binding through its typed server and
// its optional interfaces: the host sets the list (SetAttributes — the
// typed Patch has no field for the fabric-scoped list) and the observers
// hear it, a handler and an event are refused (the cluster has neither),
// and OnWrite vetoes a controller write before it is stored.
func TestTypedBindingServer(t *testing.T) {
	t.Parallel()
	b := core.NewBinding()
	s := bindingdef.Wrap(b)
	var changes []string
	stop := b.OnChange(func(fabric uint8, added, removed []core.TargetStruct) {
		for _, a := range added {
			changes = append(changes, "+"+a.Key())
		}
		for _, r := range removed {
			changes = append(changes, "-"+r.Key())
		}
	})
	defer stop()

	// The host's own change: stored and told, every fabric's entries as
	// given.
	set := []core.TargetStruct{{Node: 7, Endpoint: 1, FabricIndex: 1}, {Group: 3, FabricIndex: 2}}
	if err := b.SetAttributes(map[uint32]any{bindingdef.AttrBinding: set}); err != nil {
		t.Fatal(err)
	}
	if got := b.Entries(); !slices.Equal(got, set) {
		t.Errorf("entries %+v", got)
	}
	if !slices.Equal(changes, []string{"+1/7//1/", "+2//3//"}) {
		t.Errorf("changes %v", changes)
	}
	// The generated form is checked against the definition: a group 0 is
	// refused and nothing changes.
	bad := []bindingdef.TargetStruct{{Group: ptrU16(0), FabricIndex: 1}}
	if err := b.SetAttributes(map[uint32]any{bindingdef.AttrBinding: bad}); !errors.Is(err, spec.ErrInvalidValue) {
		t.Errorf("an invalid list: %v", err)
	}
	if got := b.Entries(); len(got) != 2 {
		t.Errorf("a refused list changed the entries: %+v", got)
	}
	if err := s.Set(bindingdef.Patch{}); err != nil {
		t.Errorf("an empty patch: %v", err)
	}
	if err := b.SetAttributes(map[uint32]any{0xFFFC: uint32(0)}); !errors.Is(err, spec.ErrNotServed) {
		t.Errorf("FeatureMap: %v, want ErrNotServed", err)
	}

	if err := b.Handle(0x00, nil); !errors.Is(err, spec.ErrNotAccepted) {
		t.Errorf("Handle: %v, want ErrNotAccepted", err)
	}
	if err := b.Emit(1, 0, nil); !errors.Is(err, spec.ErrNotEmitted) {
		t.Errorf("Emit: %v, want ErrNotEmitted", err)
	}

	// A controller write on fabric 1: the veto sees the entries as they
	// would be stored, the fabric stamped, and refuses one.
	refusal := spec.Errorf(im.StatusConstraintError, "the host refuses node 9")
	var seen [][]core.TargetStruct
	b.SetBeforeWrite(func(_ context.Context, attrID uint32, value any) error {
		list, _ := value.([]core.TargetStruct)
		seen = append(seen, list)
		if attrID == bindingdef.AttrBinding && len(list) > 0 && list[0].Node == 9 {
			return refusal
		}
		return nil
	})
	ctx := im.WithFabricFilter(context.Background(), true, 1)
	write := []bindingdef.TargetStruct{{Node: ptrU64(9), Endpoint: ptrU16(2), FabricIndex: 5}}
	if err := b.MatterWrite(ctx, bindingdef.AttrBinding, write); !errors.Is(err, refusal) {
		t.Errorf("a vetoed write: %v", err)
	}
	if got := b.Entries(); !slices.Equal(got, set) {
		t.Errorf("a vetoed write stored %+v", got)
	}
	write[0].Node = ptrU64(8)
	if err := b.MatterWrite(ctx, bindingdef.AttrBinding, write); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 || seen[0][0].FabricIndex != 1 || seen[1][0].Node != 8 {
		t.Errorf("the veto saw %+v", seen)
	}
	want := []core.TargetStruct{{Group: 3, FabricIndex: 2}, {Node: 8, Endpoint: 2, FabricIndex: 1}}
	if got := b.Entries(); !slices.Equal(got, want) {
		t.Errorf("entries after the write %+v, want %+v", got, want)
	}
}

func ptrU16(v uint16) *uint16 { return &v }

func ptrU64(v uint64) *uint64 { return &v }
