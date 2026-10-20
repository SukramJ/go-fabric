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
	if err := spec.OnWriteOf(srv, func(_ context.Context, next hepaState, attrID uint32) error {
		seen = append(seen, next)
		attrs = append(attrs, attrID)
		return veto
	}); err != nil {
		t.Fatal(err)
	}

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

// full implements every optional interface the hull forwards, each
// answering a value of its own.
type full struct {
	bare
	quiesced bool
	emitter  contract.EventEmitter
	attrCB   func([]uint32)
}

func (*full) MatterDataVersion() uint32                              { return 7 }
func (*full) MatterAttributes() []uint32                             { return []uint32{1} }
func (*full) MatterAcceptedCommands() []uint32                       { return []uint32{2} }
func (*full) MatterGeneratedCommands() []uint32                      { return []uint32{3} }
func (*full) MatterEvents() []uint32                                 { return []uint32{4} }
func (*full) MinReadPrivilege(uint32) uint8                          { return 3 }
func (*full) MinWritePrivilege(uint32) uint8                         { return 4 }
func (*full) MinInvokePrivilege(uint32) uint8                        { return 5 }
func (*full) IsTimedCommand(uint32) bool                             { return true }
func (*full) MatterReadFiltered(context.Context, uint32) (any, bool) { return "filtered", true }
func (*full) MatterSelfReportedAttributes() []uint32                 { return []uint32{6} }
func (f *full) MatterQuiesce()                                       { f.quiesced = true }
func (f *full) SetMatterEventEmitter(e contract.EventEmitter)        { f.emitter = e }
func (f *full) OnMatterAttributesChanged(cb func([]uint32)) func()   { f.attrCB = cb; return func() {} }

type nopEmitter struct{}

func (nopEmitter) MatterEmitEvent(uint16, uint32, uint32, any, contract.EventPriority) {}

// TestHullForwards holds spec.Hull to every interface of its forwarding
// list: over a server implementing it, the server's answer; over one
// without it, the dispatcher's value for an absent interface.
func TestHullForwards(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := &full{bare: bare{values: map[uint32]any{0: uint8(1)}}}
	h := spec.NewHull(f)
	if h.Underlying() != contract.ClusterServer(f) || h.MatterClusterID() != 0xFFF1FC7E {
		t.Fatal("the hull does not hold its server")
	}
	if v, ok := h.MatterRead(0); !ok || v != uint8(1) {
		t.Errorf("MatterRead %v %v", v, ok)
	}
	if err := h.MatterWrite(ctx, 0, nil); err != nil {
		t.Error(err)
	}
	if _, err := h.MatterInvoke(ctx, 0, nil); err != nil {
		t.Error(err)
	}
	var calls int
	h.OnMatterAttributesChanged(func([]uint32) { calls++ })()
	h.OnMatterValueChanged(func() { calls++ })()
	f.attrCB(nil)
	h.MatterQuiesce()
	h.SetMatterEventEmitter(nopEmitter{})
	if v, ok := h.MatterReadFiltered(ctx, 0); v != "filtered" || !ok {
		t.Errorf("MatterReadFiltered %v", v)
	}
	if h.MatterDataVersion() != 7 || !slices.Equal(h.MatterAttributes(), []uint32{1}) || !slices.Equal(h.MatterAcceptedCommands(), []uint32{2}) ||
		!slices.Equal(h.MatterGeneratedCommands(), []uint32{3}) || !slices.Equal(h.MatterEvents(), []uint32{4}) ||
		h.MinReadPrivilege(0) != 3 || h.MinWritePrivilege(0) != 4 || h.MinInvokePrivilege(0) != 5 || !h.IsTimedCommand(0) ||
		!slices.Equal(h.MatterSelfReportedAttributes(), []uint32{6}) || !slices.Equal(h.MatterReportable(), []uint32{0, 2}) ||
		!f.quiesced || f.emitter == nil || calls != 1 {
		t.Errorf("forwarding over a full server: %+v, %d calls", f, calls)
	}

	b := spec.NewHull(bare{values: map[uint32]any{0: uint8(9)}})
	if v, _ := b.MatterReadFiltered(ctx, 0); v != uint8(9) {
		t.Errorf("the unfiltered read: %v", v)
	}
	b.OnMatterAttributesChanged(func([]uint32) {})()
	b.OnMatterValueChanged(func() {})()
	b.MatterQuiesce()
	b.SetMatterEventEmitter(nopEmitter{})
	if b.MatterDataVersion() != 0 || !slices.Equal(b.MatterAttributes(), []uint32{0, 2}) || len(b.MatterAcceptedCommands()) != 0 ||
		len(b.MatterGeneratedCommands()) != 0 || len(b.MatterEvents()) != 0 || b.MinReadPrivilege(0) != 1 ||
		b.MinWritePrivilege(0) != 0 || b.MinInvokePrivilege(0) != 0 || b.IsTimedCommand(0) || b.MatterSelfReportedAttributes() != nil {
		t.Error("the absent-interface values")
	}

	// A ChangeNotifier-only server reports through both notifiers.
	vn := &valueNotifier{}
	var ids [][]uint32
	unsubscribe := spec.NewHull(vn).OnMatterAttributesChanged(func(a []uint32) { ids = append(ids, a) })
	vn.cb()
	unsubscribe()
	spec.NewHull(vn).OnMatterValueChanged(func() { ids = append(ids, nil) })
	vn.cb()
	if len(ids) != 2 || !slices.Equal(ids[0], []uint32{0, 2}) {
		t.Errorf("ChangeNotifier adapted: %v", ids)
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
	unsubscribe := spec.OnChangedOf(vn, func(st hepaState, changed []uint32) { got = append(got, changed) })
	vn.cb()
	unsubscribe()
	if len(got) != 1 || !slices.Equal(got[0], []uint32{0, 2}) {
		t.Errorf("OnChangedOf over the ChangeNotifier: %v", got)
	}
	spec.OnChangedOf(bare{}, func(hepaState, []uint32) { t.Error("a silent server reported") })()
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
