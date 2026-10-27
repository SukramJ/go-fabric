// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
	bindingdef "github.com/SukramJ/go-fabric/cluster/spec/binding"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// bindingChange is one OnChange call.
type bindingChange struct {
	fabric         uint8
	added, removed []string
}

type changeRecorder struct {
	mu  sync.Mutex
	got []bindingChange
}

func (r *changeRecorder) fn(fabric uint8, added, removed []bindingdef.TargetStruct) {
	keys := func(l []bindingdef.TargetStruct) []string {
		out := make([]string, 0, len(l))
		for _, t := range l {
			out = append(out, core.TargetFromDef(t).Key())
		}
		return out
	}
	r.mu.Lock()
	r.got = append(r.got, bindingChange{fabric, keys(added), keys(removed)})
	r.mu.Unlock()
}

func (r *changeRecorder) take() []bindingChange {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.got
	r.got = nil
	return out
}

// fabricCtx is the context the bridge gives a write on a session of
// fabric (bridge/receive_dispatch.go writeFabricIndex).
func fabricCtx(fabric uint8) context.Context {
	return im.WithFabricFilter(context.Background(), false, fabric)
}

// TestBindingKey pins TargetStruct.Key to matter.js BindingManager.entryKey
// (BindingManager.ts:714-718): fabric/node/group/endpoint/cluster, an
// absent field empty.
func TestBindingKey(t *testing.T) {
	t.Parallel()
	cases := []struct {
		t    core.TargetStruct
		want string
	}{
		{core.TargetStruct{FabricIndex: 1, Node: 2, Endpoint: 1, Cluster: 6}, "1/2//1/6"},
		{core.TargetStruct{FabricIndex: 3, Group: 0x10}, "3//16//"},
		{core.TargetStruct{FabricIndex: 1, Node: 2, Endpoint: 1, Cluster: 0xFFFFFFFF}, "1/2//1/"},
	}
	for _, c := range cases {
		if got := c.t.Key(); got != c.want {
			t.Errorf("%+v: %q, want %q", c.t, got, c.want)
		}
	}
}

// TestBindingOnChange: a committed write reports, per fabric, the entries
// added and removed by key; a changed entry is one removed and one added
// (BindingServer.ts:127-157). A refused write reports nothing; a stopped
// observer hears nothing.
func TestBindingOnChange(t *testing.T) {
	t.Parallel()
	b := core.NewBinding()
	r := &changeRecorder{}
	stop := b.OnChange(r.fn)
	ctx := fabricCtx(1)
	e1 := core.TargetStruct{Node: 2, Endpoint: 1, Cluster: 6}
	e2 := core.TargetStruct{Node: 3, Endpoint: 2, Cluster: 6}

	// Add two.
	if err := b.MatterWrite(ctx, 0, []core.TargetStruct{e1, e2}); err != nil {
		t.Fatal(err)
	}
	if got := r.take(); !slices.EqualFunc(got, []bindingChange{{1, []string{"1/2//1/6", "1/3//2/6"}, nil}}, eqChange) {
		t.Fatalf("add: %+v", got)
	}
	// The same list again: nothing changed.
	if err := b.MatterWrite(ctx, 0, []core.TargetStruct{e1, e2}); err != nil {
		t.Fatal(err)
	}
	if got := r.take(); len(got) != 0 {
		t.Fatalf("unchanged: %+v", got)
	}
	// Remove one, change the other (its endpoint): remove + add.
	e1b := e1
	e1b.Endpoint = 3
	if err := b.MatterWrite(ctx, 0, []core.TargetStruct{e1b}); err != nil {
		t.Fatal(err)
	}
	if got := r.take(); !slices.EqualFunc(got, []bindingChange{{1, []string{"1/2//3/6"}, []string{"1/2//1/6", "1/3//2/6"}}}, eqChange) {
		t.Fatalf("change: %+v", got)
	}
	// A refused write (a session without a fabric, a value of the wrong
	// type) leaves the list and tells nobody.
	if err := b.MatterWrite(fabricCtx(0), 0, []core.TargetStruct{e2}); err == nil {
		t.Fatal("a write without a fabric succeeded")
	}
	if err := b.MatterWrite(ctx, 0, "nope"); err == nil {
		t.Fatal("a write of the wrong type succeeded")
	}
	if got := r.take(); len(got) != 0 {
		t.Fatalf("a refused write reported %+v", got)
	}
	if got := coreEntries(b); len(got) != 1 || got[0].Key() != "1/2//3/6" {
		t.Fatalf("entries after refused writes %+v", got)
	}
	stop()
	if err := b.MatterWrite(ctx, 0, []core.TargetStruct{}); err != nil {
		t.Fatal(err)
	}
	if got := r.take(); len(got) != 0 {
		t.Fatalf("a stopped observer heard %+v", got)
	}
}

func eqChange(a, b bindingChange) bool {
	return a.fabric == b.fabric && slices.Equal(a.added, b.added) && slices.Equal(a.removed, b.removed)
}

// TestBindingFabricScopedWrite: a write on a session replaces that
// fabric's entries only, each stamped with the accessing fabric whatever it
// carried (AttributeWriteResponse.ts:461-467); a session without a fabric
// is UNSUPPORTED_ACCESS (:246-249); a fabric-filtered read lists the
// accessing fabric's entries, an unfiltered one every fabric's.
func TestBindingFabricScopedWrite(t *testing.T) {
	t.Parallel()
	b := core.NewBinding()
	r := &changeRecorder{}
	b.OnChange(r.fn)
	if err := b.MatterWrite(fabricCtx(1), 0, []core.TargetStruct{{Node: 2, Endpoint: 1, FabricIndex: 9}}); err != nil {
		t.Fatal(err)
	}
	if err := b.MatterWrite(fabricCtx(2), 0, []core.TargetStruct{{Group: 7}}); err != nil {
		t.Fatal(err)
	}
	if err := b.MatterWrite(fabricCtx(1), 0, []core.TargetStruct{{Node: 4, Endpoint: 1}}); err != nil {
		t.Fatal(err)
	}
	want := []string{"2//7//", "1/4//1/"}
	entries := coreEntries(b)
	got := make([]string, 0, len(entries))
	for _, e := range entries {
		got = append(got, e.Key())
	}
	if !slices.Equal(got, want) {
		t.Fatalf("entries %v, want %v", got, want)
	}
	if ch := r.take(); !slices.EqualFunc(ch, []bindingChange{
		{1, []string{"1/2//1/"}, nil},
		{2, []string{"2//7//"}, nil},
		{1, []string{"1/4//1/"}, []string{"1/2//1/"}},
	}, eqChange) {
		t.Fatalf("changes %+v", ch)
	}
	var sce im.StatusCodeError
	if err := b.MatterWrite(fabricCtx(0), 0, []core.TargetStruct{}); !errors.As(err, &sce) || sce.MatterStatusCode() != im.StatusUnsupportedAccess {
		t.Fatalf("no-fabric write: %v", err)
	}
	v, _ := b.MatterReadFiltered(im.WithFabricFilter(context.Background(), true, 2), 0)
	if l := v.([]core.TargetStruct); len(l) != 1 || l[0].Group != 7 {
		t.Fatalf("filtered read %+v", l)
	}
	v, _ = b.MatterReadFiltered(im.WithFabricFilter(context.Background(), false, 2), 0)
	if l := v.([]core.TargetStruct); len(l) != 2 {
		t.Fatalf("unfiltered read %+v", l)
	}
	// A local write (no request) replaces every fabric's entries.
	if err := b.MatterWrite(context.Background(), 0, []core.TargetStruct{{Node: 5, Endpoint: 1, FabricIndex: 3}}); err != nil {
		t.Fatal(err)
	}
	if ch := r.take(); !slices.EqualFunc(ch, []bindingChange{
		{1, nil, []string{"1/4//1/"}},
		{2, nil, []string{"2//7//"}},
		{3, []string{"3/5//1/"}, nil},
	}, eqChange) {
		t.Fatalf("local write changes %+v", ch)
	}
}

// TestBindingGeneratedValue: the generated TargetStruct the definition
// decodes is accepted, stamped with the accessing fabric and judged by the
// definition: a Group below its "min 1" is CONSTRAINT_ERROR.
func TestBindingGeneratedValue(t *testing.T) {
	t.Parallel()
	b := core.NewBinding()
	node, ep, cl := uint64(2), uint16(1), uint32(6)
	if err := b.MatterWrite(fabricCtx(4), 0, []bindingdef.TargetStruct{{Node: &node, Endpoint: &ep, Cluster: &cl}}); err != nil {
		t.Fatal(err)
	}
	if got := coreEntries(b); len(got) != 1 || got[0] != (core.TargetStruct{Node: 2, Endpoint: 1, Cluster: 6, FabricIndex: 4}) {
		t.Fatalf("entries %+v", got)
	}
	zero := uint16(0)
	var sce im.StatusCodeError
	err := b.MatterWrite(fabricCtx(4), 0, []bindingdef.TargetStruct{{Group: &zero}})
	if !errors.As(err, &sce) || sce.MatterStatusCode() != im.StatusConstraintError {
		t.Fatalf("group 0: %v", err)
	}
}

// TestBindingRemoveFabric: a removed fabric's entries go and the observers
// hear it (FabricScopedDataHandler.ts:109-122); another fabric's stay.
func TestBindingRemoveFabric(t *testing.T) {
	t.Parallel()
	b := core.NewBinding()
	if err := b.MatterWrite(fabricCtx(1), 0, []core.TargetStruct{{Node: 2, Endpoint: 1}}); err != nil {
		t.Fatal(err)
	}
	if err := b.MatterWrite(fabricCtx(2), 0, []core.TargetStruct{{Group: 3}}); err != nil {
		t.Fatal(err)
	}
	r := &changeRecorder{}
	b.OnChange(r.fn)
	b.RemoveFabric(1)
	b.RemoveFabric(7) // none: no call
	if got := r.take(); !slices.EqualFunc(got, []bindingChange{{1, nil, []string{"1/2//1/"}}}, eqChange) {
		t.Fatalf("changes %+v", got)
	}
	if got := coreEntries(b); len(got) != 1 || got[0].FabricIndex != 2 {
		t.Fatalf("entries %+v", got)
	}
}

// TestBindingOrigins is the origin audit of the Binding server (BY-1 Left
// out, BY-2 §6): a controller's write of the list reaches the attribute
// listeners — device.Device.OnChanged and the bridge — from
// OriginControllerWrite, the host's SetAttributes from OriginHostSet, a
// removed fabric's entries from OriginUnknown; a write of the same list
// reports nothing.
func TestBindingOrigins(t *testing.T) {
	t.Parallel()
	b := core.NewBinding()
	var mu sync.Mutex
	var got []contract.Change
	b.OnAttributeChange(func(c contract.Change) {
		mu.Lock()
		got = append(got, c)
		mu.Unlock()
	})
	take := func() []contract.Change {
		mu.Lock()
		defer mu.Unlock()
		out := got
		got = nil
		return out
	}
	e1 := core.TargetStruct{Node: 2, Endpoint: 1, Cluster: 6}
	ctx := contract.WithOrigin(fabricCtx(1), contract.OriginControllerWrite)
	if err := b.MatterWrite(ctx, 0, []core.TargetStruct{e1}); err != nil {
		t.Fatal(err)
	}
	want := func(step string, origin contract.Origin) {
		t.Helper()
		if c := take(); len(c) != 1 || c[0].Origin != origin || !slices.Equal(c[0].Attributes, []uint32{bindingdef.AttrBinding}) {
			t.Errorf("%s: changes %+v, want one of Binding from %v", step, c, origin)
		}
	}
	want("controller write", contract.OriginControllerWrite)
	if err := b.MatterWrite(ctx, 0, []core.TargetStruct{e1}); err != nil {
		t.Fatal(err)
	}
	if c := take(); len(c) != 0 {
		t.Errorf("the same list again: %+v", c)
	}
	e2 := core.TargetStruct{Node: 3, Endpoint: 2, Cluster: 6, FabricIndex: 2}
	if err := b.SetAttributes(map[uint32]any{bindingdef.AttrBinding: []core.TargetStruct{{Node: 2, Endpoint: 1, Cluster: 6, FabricIndex: 1}, e2}}); err != nil {
		t.Fatal(err)
	}
	want("host set", contract.OriginHostSet)
	b.RemoveFabric(2)
	want("fabric removed", contract.OriginUnknown)
}

// coreEntries is b's entries in core's presence-flag form, for comparing
// with core.TargetStruct literals.
func coreEntries(b interface {
	Entries() []bindingdef.TargetStruct
}) []core.TargetStruct {
	var out []core.TargetStruct
	for _, t := range b.Entries() {
		out = append(out, core.TargetFromDef(t))
	}
	return out
}
