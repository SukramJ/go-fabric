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
// a State and a Patch, their methods written by hand as the generator
// writes them (State.FromAttributeValues, Patch.AttributeValues).
type hepaState struct {
	Condition              uint8
	ChangeIndication       hepa.ChangeIndicationEnum
	InPlaceIndicator       bool
	LastChangedTime        *uint32
	ReplacementProductList []hepa.ReplacementProductStruct
	Note                   string // no attribute
}

func (st *hepaState) FromAttributeValues(read func(attrID uint32) (any, bool)) {
	if v, ok := read(hepa.AttrCondition); ok {
		spec.Assign(&st.Condition, v)
	}
	if v, ok := read(hepa.AttrChangeIndication); ok {
		spec.Assign(&st.ChangeIndication, v)
	}
	if v, ok := read(hepa.AttrInPlaceIndicator); ok {
		spec.Assign(&st.InPlaceIndicator, v)
	}
	if v, ok := read(hepa.AttrLastChangedTime); ok {
		spec.Assign(&st.LastChangedTime, v)
	}
	if v, ok := read(hepa.AttrReplacementProductList); ok {
		spec.Assign(&st.ReplacementProductList, v)
	}
}

type hepaPatch struct {
	Condition              *uint8
	ChangeIndication       *hepa.ChangeIndicationEnum
	LastChangedTime        *uint32
	ReplacementProductList *[]hepa.ReplacementProductStruct
	Null                   []uint32
}

func (p hepaPatch) AttributeValues() map[uint32]any {
	v := map[uint32]any{}
	if p.Condition != nil {
		v[hepa.AttrCondition] = *p.Condition
	}
	if p.ChangeIndication != nil {
		v[hepa.AttrChangeIndication] = *p.ChangeIndication
	}
	if p.LastChangedTime != nil {
		v[hepa.AttrLastChangedTime] = *p.LastChangedTime
	}
	if p.ReplacementProductList != nil {
		v[hepa.AttrReplacementProductList] = spec.List[hepa.ReplacementProductStruct](*p.ReplacementProductList)
	}
	for _, id := range p.Null {
		v[id] = nil
	}
	return v
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
	if got := p.AttributeValues(); !reflect.DeepEqual(got, map[uint32]any{hepa.AttrCondition: uint8(50), hepa.AttrLastChangedTime: uint32(1000)}) {
		t.Errorf("patch values %#v", got)
	}
	if err := spec.SetPatch(srv, p); err != nil {
		t.Fatal(err)
	}
	st = spec.StateOf[hepaState](srv)
	if st.Condition != 50 || st.LastChangedTime == nil || *st.LastChangedTime != 1000 {
		t.Errorf("after the patch: %+v", st)
	}
	if err := spec.SetPatch(srv, hepaPatch{Null: []uint32{hepa.AttrLastChangedTime}}); err != nil {
		t.Fatal(err)
	}
	if st = spec.StateOf[hepaState](srv); st.LastChangedTime != nil {
		t.Errorf("null patch: %v", *st.LastChangedTime)
	}
	// All or nothing: one value outside its constraint stores none.
	bad := hepaPatch{Condition: ptr(uint8(60)), ChangeIndication: ptr(hepa.ChangeIndicationEnum(9))}
	if err := spec.SetPatch(srv, bad); !errors.Is(err, spec.ErrInvalidValue) {
		t.Errorf("bad patch: %v", err)
	}
	if st = spec.StateOf[hepaState](srv); st.Condition != 50 {
		t.Errorf("a refused patch stored %d", st.Condition)
	}
}

func TestTypedOnChanged(t *testing.T) {
	t.Parallel()
	srv := hepaServer(t, spec.ServerConfig{})
	var states []hepaState
	var ids [][]uint32
	unsubscribe := spec.OnChangedOf(srv, func(s hepaState, change contract.Change) {
		changed := change.Attributes
		states = append(states, s)
		ids = append(ids, changed)
	})
	if err := spec.SetPatch(srv, hepaPatch{Condition: ptr(uint8(7)), LastChangedTime: ptr(uint32(3))}); err != nil {
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
	refusal := errors.New("the relay says constraint") // wording is no status: the dispatcher answers FAILURE
	var veto error
	var seen []hepaState
	var attrs []uint32
	srv := hepaServer(t, spec.ServerConfig{Sink: sk})
	if err := spec.OnWriteOf(srv, func(_ context.Context, next hepaState, attrID uint32) error {
		seen = append(seen, next)
		attrs = append(attrs, attrID)
		return veto
	}); err != nil {
		t.Fatal(err)
	}

	veto = refusal
	err := srv.MatterWrite(ctx, hepa.AttrLastChangedTime, uint64(1000))
	// The refusal comes back as given, without a status of its own: the
	// dispatcher answers an untyped error with FAILURE (classifyError).
	if !errors.Is(err, refusal) || statusOf(err) != im.StatusSuccess || err.Error() != refusal.Error() {
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

	if err := spec.OnWriteOf[hepaState](srv, nil); err != nil {
		t.Fatal(err)
	}
	veto = refusal
	if err := srv.MatterWrite(ctx, hepa.AttrLastChangedTime, uint64(6)); err != nil {
		t.Errorf("a removed veto still refuses: %v", err)
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

// bare is a server with none of the optional interfaces.
type bare struct{ values map[uint32]any }

func (b bare) MatterClusterID() uint32 { return 0xFFF1FC7E }

func (b bare) MatterRead(attrID uint32) (any, bool) { v, ok := b.values[attrID]; return v, ok }

func (bare) MatterWrite(context.Context, uint32, any) error { return nil }

func (bare) MatterInvoke(context.Context, uint32, any) (any, error) { return nil, nil }

func (bare) MatterReportable() []uint32 { return []uint32{0, 2} }

// valueNotifier reports its changes through contract.ChangeNotifier only.
type valueNotifier struct {
	bare
	cb func()
}

func (n *valueNotifier) OnMatterValueChanged(cb func()) func() {
	n.cb = cb
	return func() { n.cb = nil }
}

// TestWrapper: the wrapper holds its server and is no
// contract.ClusterServer, so a typed server cannot be mounted.
func TestWrapper(t *testing.T) {
	t.Parallel()
	b := &bare{}
	if spec.NewWrapper(b).Underlying() != contract.ClusterServer(b) {
		t.Error("the wrapper does not hold its server")
	}
	server := reflect.TypeFor[contract.ClusterServer]()
	for _, typ := range []reflect.Type{reflect.TypeFor[spec.Wrapper](), reflect.TypeFor[*spec.Wrapper]()} {
		if typ.Implements(server) {
			t.Errorf("%v implements contract.ClusterServer", typ)
		}
	}
}

// TestTypedOverAServerWithoutInterfaces: the typed helpers over a server
// that is no AttributeSetter, CommandRegistrar, EventEmitting or
// BeforeWriteSetter refuse with their error; Get and OnChanged read and
// subscribe through MatterRead and the ChangeNotifier.
func TestTypedOverAServerWithoutInterfaces(t *testing.T) {
	t.Parallel()
	vn := &valueNotifier{bare: bare{values: map[uint32]any{hepa.AttrCondition: uint64(30), hepa.AttrChangeIndication: uint8(2)}}}
	if err := spec.SetPatch(vn, hepaPatch{Condition: ptr(uint8(1))}); !errors.Is(err, spec.ErrNotSettable) {
		t.Errorf("SetPatch: %v", err)
	}
	if err := spec.HandleOn(vn, 0, func(context.Context, hepa.ResetConditionRequest) (struct{}, error) { return struct{}{}, nil }); !errors.Is(err, spec.ErrNoHandlers) {
		t.Errorf("HandleOn: %v", err)
	}
	if err := spec.EmitOn(vn, 1, 0, nil); !errors.Is(err, spec.ErrNoEvents) {
		t.Errorf("EmitOn: %v", err)
	}
	if err := spec.OnWriteOf[hepaState](vn, nil); !errors.Is(err, spec.ErrNoWriteVeto) {
		t.Errorf("OnWriteOf: %v", err)
	}
	if st := spec.StateOf[hepaState](vn); st.Condition != 30 || st.ChangeIndication != hepa.ChangeIndicationCritical {
		t.Errorf("StateOf over MatterRead: %+v", st)
	}
	var got [][]uint32
	unsubscribe := spec.OnChangedOf(vn, func(st hepaState, change contract.Change) { changed := change.Attributes; got = append(got, changed) })
	vn.cb()
	unsubscribe()
	if len(got) != 1 || !slices.Equal(got[0], []uint32{0, 2}) {
		t.Errorf("OnChangedOf over the ChangeNotifier: %v", got)
	}
	spec.OnChangedOf(bare{}, func(hepaState, contract.Change) { t.Error("a silent server reported") })()
}

func TestTypedEmitAndHandleOn(t *testing.T) {
	t.Parallel()
	srv := hepaServer(t, spec.ServerConfig{})
	if err := spec.HandleOn(srv, hepa.CmdResetCondition, func(context.Context, hepa.ResetConditionRequest) (struct{}, error) { return struct{}{}, nil }); err != nil {
		t.Fatal(err)
	}
	if resp, err := srv.MatterInvoke(context.Background(), hepa.CmdResetCondition, hepa.ResetConditionRequest{}); resp != nil || err != nil {
		t.Errorf("HandleOn: %v %v", resp, err)
	}
	if err := spec.EmitOn(srv, 1, 0x7F, nil); !errors.Is(err, spec.ErrNotEmitted) {
		t.Errorf("EmitOn of an event outside EventList: %v", err)
	}
}
