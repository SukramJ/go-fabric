// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec_test

import (
	"context"
	"errors"
	"testing"

	"thingfabric.dev/go-fabric/cluster/spec"
	bsdef "thingfabric.dev/go-fabric/cluster/spec/booleanstate"
	"thingfabric.dev/go-fabric/cluster/spec/powersource"
	"thingfabric.dev/go-fabric/im"
)

type listReq struct {
	Op    uint8
	Level *uint8
}

// TestCheckRequestShapes: the payloads CheckRequest does not judge (a nil
// pointer, no struct, a type without the definition's fields, a command
// the definition does not state), a nil payload judged as a request
// without fields (ProtocolService.ts:624-625 at b25ed960), a pointer
// payload, an absent
// optional field, the enum-member-list constraint, and the gate of
// InvokeHeld refusing before the host's handler.
func TestCheckRequestShapes(t *testing.T) {
	t.Parallel()
	opEnum := &spec.Enum{Name: "OpEnum", Bits: 8, Values: []spec.EnumValue{{Value: 0, Name: "Add"}, {Value: 1, Name: "Modify"}, {Value: 2, Name: "Clear"}}}
	def := &spec.Cluster{
		ID: 0xFFF1_FC0C, Name: "Shapes", Revision: 1,
		Commands: []spec.Command{{
			ID: 0, Name: "Op", Direction: spec.Request, Decode: decodesAs[listReq](), Response: "status", Conformance: op(spec.ConfMandatory),
			Access: spec.Access{Write: spec.PrivilegeOperate}, Check: spec.RequestCheck{Refusal: im.StatusInvalidCommand},
			Fields: []spec.Field{
				{
					ID: 0, Name: "Op", Type: spec.Type{Kind: spec.KindEnum, Bits: 8, Enum: opEnum}, Conformance: op(spec.ConfMandatory),
					Constraint: spec.Constraint{Text: "add, modify", Parts: []spec.Constraint{{Value: &spec.Bound{Ref: "add"}}, {Value: &spec.Bound{Ref: "modify"}}}},
				},
				{ID: 1, Name: "Level", Type: spec.Type{Kind: spec.KindUint, Bits: 8}, Conformance: op(spec.ConfOptional), Quality: spec.Quality{Nullable: true}},
			},
		}},
	}
	calls := 0
	srv, err := spec.NewServer(def, spec.Options{}, spec.ServerConfig{Handlers: map[uint32]spec.CommandHandler{0: noop}})
	if err != nil {
		t.Fatal(err)
	}
	for name, fields := range map[string]any{
		"nil pointer": (*listReq)(nil), "no struct": 7, "other type": struct{ X int }{},
	} {
		if err := srv.CheckRequest(0, fields, nil); err != nil {
			t.Errorf("%s: %v, want not judged", name, err)
		}
	}
	if err := srv.CheckRequest(0, nil, nil); !errors.Is(err, spec.ErrConformance) {
		t.Errorf("nil: %v, want the mandatory Op missing", err)
	}
	if err := srv.CheckRequest(9, listReq{}, nil); err != nil {
		t.Errorf("an unstated command: %v", err)
	}
	level := uint8(3)
	if err := srv.CheckRequest(0, &listReq{Op: 1, Level: &level}, nil); err != nil {
		t.Errorf("modify with a level: %v", err)
	}
	if err := srv.CheckRequest(0, listReq{Op: 2}, nil); statusOf(err) != im.StatusConstraintError {
		t.Errorf("clear, outside \"add, modify\": %v", err)
	}
	if err := srv.CheckRequest(0, listReq{Op: 7}, nil); err == nil {
		t.Error("an undefined enum value passed")
	}

	held := spec.NewSelfHeld(srv)
	if err := held.Handle(0, func(context.Context, any) (any, error) { calls++; return nil, nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := wireInvokeHeld(context.Background(), srv, &held, 0, listReq{Op: 2}); statusOf(err) != im.StatusInvalidCommand || calls != 0 {
		t.Errorf("InvokeHeld of a refused request: %v, %d handler calls", err, calls)
	}

	b, err := spec.NewServer(bsdef.Definition, spec.Options{Features: uint32(bsdef.FeatureChangeEvent)}, spec.ServerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	em := &emitter{}
	b.SetMatterEventEmitter(em)
	bh := spec.NewSelfHeld(b)
	bh.SetEndpoint(9)
	if err := bh.EmitEvent(bsdef.EventStateChange, bsdef.StateChangeEvent{}); err != nil || len(em.events) != 1 || em.events[0].endpoint != 9 {
		t.Errorf("SelfHeld.SetEndpoint: %v, %+v", err, em.events)
	}

	p, err := spec.NewServer(powersource.Definition, spec.Options{Features: uint32(powersource.FeatureBattery)}, spec.ServerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.MarkQuietChanged(powersource.AttrBatPercentRemaining); !errors.Is(err, spec.ErrNotServed) {
		t.Errorf("MarkQuietChanged of an unserved attribute: %v", err)
	}
}
