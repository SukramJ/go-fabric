// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec_test

import (
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/im"
)

// TestHostHandlerResponseCheckedEverywhere: the same host handler, whose
// response leaves out a field the request makes mandatory, answers
// FAILURE on the generated server (Handle), on a self-held server over a
// Server (SelfHeld, InvokeHeld), on one over Hooks and through
// HostHandlers called directly; a correct response passes on each
// (R2-P5-apispec-06).
func TestHostHandlerResponseCheckedEverywhere(t *testing.T) {
	t.Parallel()
	def := &spec.Cluster{
		ID: 0xFFF1FC0A, Name: "HostResp", Revision: 1,
		Commands: []spec.Command{
			{
				ID: 1, Name: "Req", Direction: spec.Request, Response: "Resp", Conformance: op(spec.ConfMandatory),
				Fields: []spec.Field{{ID: 0, Name: "Enable", Type: spec.Type{Kind: spec.KindBool}, Conformance: op(spec.ConfOptional)}},
			},
			{
				ID: 1, Name: "Resp", Direction: spec.Response, Conformance: op(spec.ConfMandatory),
				Fields: []spec.Field{{ID: 0, Name: "Result", Type: spec.Type{Kind: spec.KindUint, Bits: 8}, Conformance: spec.Conformance{
					Text: "Req.Enable", Op: spec.ConfDot, Args: []spec.Conformance{name("Req"), name("Enable")},
				}}},
			},
		},
	}
	var resp any
	host := func(context.Context, any) (any, error) { return resp, nil }
	own := func(context.Context, any) (any, error) { return nil, errors.New("the server's own logic ran") }
	newServer := func() *spec.Server {
		srv, err := spec.NewServer(def, spec.Options{}, spec.ServerConfig{Handlers: map[uint32]spec.CommandHandler{1: own}})
		if err != nil {
			t.Fatal(err)
		}
		return srv
	}
	generated := newServer()
	if err := generated.Handle(1, host); err != nil {
		t.Fatal(err)
	}
	held := spec.NewSelfHeld(newServer())
	if err := held.Handle(1, host); err != nil {
		t.Fatal(err)
	}
	var hooks spec.Hooks
	inst, err := spec.New(def, spec.Options{})
	if err != nil {
		t.Fatal(err)
	}
	hooks.Init(inst)
	if err := hooks.Handle(1, host); err != nil {
		t.Fatal(err)
	}
	var direct spec.HostHandlers
	if err := direct.Register(inst, 1, host); err != nil {
		t.Fatal(err)
	}
	paths := map[string]func(ctx context.Context, req any) error{
		"generated": func(ctx context.Context, req any) error { _, err := generated.MatterInvoke(ctx, 1, req); return err },
		"SelfHeld":  func(ctx context.Context, req any) error { _, err := spec.InvokeHeld(ctx, &held, 1, req); return err },
		"Hooks":     func(ctx context.Context, req any) error { _, _, err := hooks.Invoke(ctx, 1, req); return err },
		"HostHandlers": func(ctx context.Context, req any) error {
			_, _, err := direct.Invoke(ctx, 1, req)
			return err
		},
	}
	v, yes := uint8(1), true
	for name, call := range paths {
		resp = ctResp{}
		if st := statusOf(call(context.Background(), ctReq{Enable: &yes})); st != im.StatusFailure {
			t.Errorf("%s: a response without the mandatory Result answered %v, want FAILURE", name, st)
		}
		resp = &ctResp{Result: &v}
		if err := call(context.Background(), ctReq{Enable: &yes}); err != nil {
			t.Errorf("%s: a correct response: %v", name, err)
		}
	}
}
