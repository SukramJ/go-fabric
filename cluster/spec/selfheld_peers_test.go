// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec_test

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/im"
)

type peerReq struct {
	Level uint8
}

// TestInvokeHeldResolvesSiblingAttributes: the request gate in front of a
// self-held server ([spec.AdmitRequest] with the server's attributes as
// peers, as endpoint.TopologyDispatcher runs it) admits a request
// whose field constraint names a sibling attribute ("max Limit") against
// the attribute's current value — the server's peers — before the host's
// handler and before the server's own logic. A level at the limit is
// admitted, one above it refused without a handler call, and a raised
// limit admits it (review II, N6a-test-3, R2-P6-test-3113).
func TestInvokeHeldResolvesSiblingAttributes(t *testing.T) {
	t.Parallel()
	def := &spec.Cluster{
		ID: 0xFFF1_FC3A, Name: "Peers", Revision: 1,
		Attributes: []spec.Attribute{{
			ID: 0, Name: "Limit", Type: spec.Type{Kind: spec.KindUint, Bits: 8}, Conformance: op(spec.ConfMandatory),
		}},
		Commands: []spec.Command{{
			ID: 0, Name: "Op", Direction: spec.Request, Response: "status", Conformance: op(spec.ConfMandatory),
			Access: spec.Access{Write: spec.PrivilegeOperate}, Check: spec.RequestCheck{Refusal: im.StatusInvalidCommand},
			Fields: []spec.Field{{
				ID: 0, Name: "Level", Type: spec.Type{Kind: spec.KindUint, Bits: 8}, Conformance: op(spec.ConfMandatory),
				Constraint: spec.Constraint{Text: "max Limit", Max: &spec.Bound{Ref: "Limit"}},
			}},
		}},
	}
	for _, path := range []string{"host handler", "server logic"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			calls := 0
			count := func(context.Context, any) (any, error) { calls++; return nil, nil }
			cfg := spec.ServerConfig{Handlers: map[uint32]spec.CommandHandler{0: noop}}
			if path == "server logic" {
				cfg.Handlers[0] = count
			}
			srv, err := spec.NewServer(def, spec.Options{}, cfg)
			if err != nil {
				t.Fatal(err)
			}
			held := spec.NewSelfHeld(srv)
			if path == "host handler" {
				if err := held.Handle(0, count); err != nil {
					t.Fatal(err)
				}
			}
			ctx := context.Background()
			if err := held.SetAttributes(map[uint32]any{0: uint8(5)}); err != nil {
				t.Fatal(err)
			}
			if _, err := wireInvokeHeld(ctx, srv, &held, 0, peerReq{Level: 5}); err != nil || calls != 1 {
				t.Fatalf("level 5 under Limit 5: %v, %d calls; want admitted", err, calls)
			}
			if _, err := wireInvokeHeld(ctx, srv, &held, 0, peerReq{Level: 6}); statusOf(err) != im.StatusInvalidCommand || calls != 1 {
				t.Fatalf("level 6 over Limit 5: %v, %d calls; want INVALID_COMMAND before the handler", err, calls)
			}
			if err := held.SetAttributes(map[uint32]any{0: uint8(6)}); err != nil {
				t.Fatal(err)
			}
			if _, err := wireInvokeHeld(ctx, srv, &held, 0, peerReq{Level: 6}); err != nil || calls != 2 {
				t.Fatalf("level 6 under Limit 6: %v, %d calls; want admitted", err, calls)
			}
		})
	}
}
