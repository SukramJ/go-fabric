// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec_test

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	hepa "github.com/SukramJ/go-fabric/cluster/spec/hepafiltermonitoring"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// hepaState and hepaPatch have the shape script/clustergen generates for
// a State and a Patch.
type hepaState struct {
	Condition              uint8                           `matter:"0x0000"`
	ChangeIndication       hepa.ChangeIndicationEnum       `matter:"0x0002"`
	InPlaceIndicator       bool                            `matter:"0x0003"`
	LastChangedTime        *uint32                         `matter:"0x0004"`
	ReplacementProductList []hepa.ReplacementProductStruct `matter:"0x0005"`
	Note                   string                          // untagged: no attribute
}

type hepaPatch struct {
	Condition              *uint8                           `matter:"0x0000"`
	ChangeIndication       *hepa.ChangeIndicationEnum       `matter:"0x0002"`
	LastChangedTime        *uint32                          `matter:"0x0004"`
	ReplacementProductList *[]hepa.ReplacementProductStruct `matter:"0x0005"`
	Null                   []uint32
}

func ptr[T any](v T) *T { return &v }

func TestTypedStateAndPatch(t *testing.T) {
	t.Parallel()
	srv := hepaServer(t, spec.ServerConfig{})
	st := spec.StateOf[hepaState](srv)
	if st.Condition != 40 || st.ChangeIndication != hepa.ChangeIndicationCritical || st.InPlaceIndicator || st.LastChangedTime != nil ||
		!reflect.DeepEqual(st.ReplacementProductList, []hepa.ReplacementProductStruct(products)) {
		t.Errorf("state %+v", st)
	}

	p := hepaPatch{Condition: ptr(uint8(50)), LastChangedTime: ptr(uint32(1000))}
	if got := spec.PatchValues(p); !reflect.DeepEqual(got, map[uint32]any{hepa.AttrCondition: uint8(50), hepa.AttrLastChangedTime: uint32(1000)}) {
		t.Errorf("patch values %#v", got)
	}
	if err := srv.SetAttributes(spec.PatchValues(&p)); err != nil {
		t.Fatal(err)
	}
	st = spec.StateOf[hepaState](srv)
	if st.Condition != 50 || st.LastChangedTime == nil || *st.LastChangedTime != 1000 {
		t.Errorf("after the patch: %+v", st)
	}
	if err := srv.SetAttributes(spec.PatchValues(hepaPatch{Null: []uint32{hepa.AttrLastChangedTime}})); err != nil {
		t.Fatal(err)
	}
	if st = spec.StateOf[hepaState](srv); st.LastChangedTime != nil {
		t.Errorf("null patch: %v", *st.LastChangedTime)
	}
	// All or nothing: one value outside its constraint stores none.
	bad := hepaPatch{Condition: ptr(uint8(60)), ChangeIndication: ptr(hepa.ChangeIndicationEnum(9))}
	if err := srv.SetAttributes(spec.PatchValues(bad)); !errors.Is(err, spec.ErrInvalidValue) {
		t.Errorf("bad patch: %v", err)
	}
	if st = spec.StateOf[hepaState](srv); st.Condition != 50 {
		t.Errorf("a refused patch stored %d", st.Condition)
	}
	if got := spec.PatchValues(42); len(got) != 0 {
		t.Errorf("a non-struct patch: %v", got)
	}
	if got := spec.StateOf[int](srv); got != 0 {
		t.Errorf("a non-struct state: %v", got)
	}
}

func TestTypedOnChanged(t *testing.T) {
	t.Parallel()
	srv := hepaServer(t, spec.ServerConfig{})
	var states []hepaState
	var ids [][]uint32
	unsubscribe := spec.OnChangedOf(srv, func(s hepaState, changed []uint32) {
		states = append(states, s)
		ids = append(ids, changed)
	})
	if err := srv.SetAttributes(spec.PatchValues(hepaPatch{Condition: ptr(uint8(7)), LastChangedTime: ptr(uint32(3))})); err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 || states[0].Condition != 7 || !slices.Equal(ids[0], []uint32{hepa.AttrCondition, hepa.AttrLastChangedTime}) {
		t.Errorf("notified %+v %v", states, ids)
	}
	unsubscribe()
	if err := srv.Set(hepa.AttrCondition, uint8(8)); err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 {
		t.Errorf("notified after unsubscribe: %d", len(states))
	}
	spec.OnChangedOf[hepaState](srv, nil)() // nil: a no-op subscription
}

func TestTypedBeforeWrite(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	sk := &sink{}
	refusal := errors.New("the relay says constraint") // text the dispatcher's heuristic would read as CONSTRAINT_ERROR
	var veto error
	var seen []hepaState
	var attrs []uint32
	srv := hepaServer(t, spec.ServerConfig{Sink: sk})
	srv.SetBeforeWrite(spec.BeforeWriteOf(srv, func(_ context.Context, next hepaState, attrID uint32) error {
		seen = append(seen, next)
		attrs = append(attrs, attrID)
		return veto
	}))

	veto = refusal
	err := srv.MatterWrite(ctx, hepa.AttrLastChangedTime, uint64(1000))
	if !errors.Is(err, refusal) || statusOf(err) != im.StatusFailure || err.Error() != refusal.Error() {
		t.Errorf("plain refusal: %v (status %v)", err, statusOf(err))
	}
	if len(seen) != 1 || seen[0].LastChangedTime == nil || *seen[0].LastChangedTime != 1000 || seen[0].Condition != 40 || attrs[0] != hepa.AttrLastChangedTime {
		t.Errorf("the veto saw %+v %v", seen, attrs)
	}
	if v, _ := srv.Value(hepa.AttrLastChangedTime); v != nil || len(sk.writes) != 0 {
		t.Errorf("a vetoed write reached the store (%v) or the sink (%v)", v, sk.writes)
	}

	veto = spec.Errorf(im.StatusConstraintError, "out of range")
	if err := srv.MatterWrite(ctx, hepa.AttrLastChangedTime, uint64(1)); statusOf(err) != im.StatusConstraintError {
		t.Errorf("a status refusal: %v", err)
	}
	// A refusal that checks before it vetoes: the definition's own check
	// answers first, the veto never sees the write.
	if err := srv.MatterWrite(ctx, hepa.AttrLastChangedTime, "x"); statusOf(err) != im.StatusConstraintError || len(seen) != 2 {
		t.Errorf("a write the definition refuses: %v, %d vetoes", err, len(seen))
	}

	veto = nil
	if err := srv.MatterWrite(ctx, hepa.AttrLastChangedTime, uint64(5)); err != nil {
		t.Fatal(err)
	}
	if v, _ := srv.Value(hepa.AttrLastChangedTime); v != uint32(5) || !slices.Equal(sk.writes, []any{uint32(5)}) {
		t.Errorf("an accepted write: %v, sink %v", v, sk.writes)
	}

	srv.SetBeforeWrite(nil)
	veto = refusal
	if err := srv.MatterWrite(ctx, hepa.AttrLastChangedTime, uint64(6)); err != nil {
		t.Errorf("a removed veto still refuses: %v", err)
	}
	if spec.BeforeWriteOf[hepaState](srv, nil) != nil {
		t.Error("BeforeWriteOf(nil) is not nil")
	}

	// The veto set at construction.
	cfg := hepaServer(t, spec.ServerConfig{BeforeWrite: func(context.Context, uint32, any) error { return refusal }})
	if err := cfg.MatterWrite(ctx, hepa.AttrLastChangedTime, uint64(1)); !errors.Is(err, refusal) {
		t.Errorf("ServerConfig.BeforeWrite: %v", err)
	}
}

func TestTypedHandler(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var got []hepa.ResetConditionRequest
	srv := hepaServer(t, spec.ServerConfig{})
	srv.Handle(hepa.CmdResetCondition, spec.HandlerOf(func(_ context.Context, req hepa.ResetConditionRequest) (struct{}, error) {
		got = append(got, req)
		return struct{}{}, nil
	}))
	for _, fields := range []any{hepa.ResetConditionRequest{}, &hepa.ResetConditionRequest{}, nil} {
		if resp, err := srv.MatterInvoke(ctx, hepa.CmdResetCondition, fields); resp != nil || err != nil {
			t.Errorf("invoke with %#v: %#v, %v", fields, resp, err)
		}
	}
	if len(got) != 3 {
		t.Errorf("handler ran %d times", len(got))
	}
	if _, err := srv.MatterInvoke(ctx, hepa.CmdResetCondition, uint8(1)); statusOf(err) != im.StatusInvalidCommand {
		t.Errorf("a payload of another type: %v", err)
	}

	boom := errors.New("boom")
	product := products[0]
	h := spec.HandlerOf(func(_ context.Context, req hepa.ReplacementProductStruct) (hepa.ReplacementProductStruct, error) {
		if req.ProductIdentifierValue == "" {
			return req, boom
		}
		return req, nil
	})
	if resp, err := h(ctx, product); err != nil || resp != product {
		t.Errorf("a response payload: %#v, %v", resp, err)
	}
	if resp, err := h(ctx, hepa.ReplacementProductStruct{}); resp != nil || !errors.Is(err, boom) {
		t.Errorf("a handler error: %#v, %v", resp, err)
	}
	if _, err := h(ctx, nil); statusOf(err) != im.StatusInvalidCommand {
		t.Errorf("no payload for a request with fields: %v", err)
	}
	if spec.HandlerOf[hepa.ResetConditionRequest, struct{}](nil) != nil {
		t.Error("HandlerOf(nil) is not nil")
	}
}

// typedHull is the shape of a generated typed server.
type typedHull struct{ *spec.Server }

func TestTypedServerRegistry(t *testing.T) {
	t.Parallel()
	def := &spec.Cluster{ID: 0xFFF1FC7E, Name: "TypedTest", Revision: 1}
	srv, err := spec.NewServer(def, spec.Options{}, spec.ServerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if got := spec.TypedServer(srv); got != contract.ClusterServer(srv) {
		t.Errorf("unregistered: %T", got)
	}
	spec.RegisterTyped(def.ID, func(s *spec.Server) *typedHull { return &typedHull{s} })
	if got, ok := spec.TypedServer(srv).(*typedHull); !ok || got.Server != srv {
		t.Errorf("registered: %#v", spec.TypedServer(srv))
	}
	if spec.TypedServer(nil) != nil {
		t.Error("TypedServer(nil) is not nil")
	}
	zero := new(spec.Server)
	if got := spec.TypedServer(zero); got != contract.ClusterServer(zero) {
		t.Errorf("a server without a definition: %T", got)
	}
}
