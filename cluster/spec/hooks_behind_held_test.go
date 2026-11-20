// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/im"
)

// TestHooksInvokeBehindInvokeHeldAdmitsOnce (decision 177, superseded by
// decision 339): a server that keeps the deprecated Hooks field for its
// window runs the field's Invoke from its own logic, behind
// [spec.InvokeHeld], so that a handler registered on the field still
// answers. The request is held once, by the gate in front of the server
// ([spec.AdmitRequest], endpoint.TopologyDispatcher): a misfit of a command
// that states no refusal is logged once, a stated refusal is answered
// there; InvokeHeld, Hooks.Invoke and MatterInvoke themselves are local
// calls and neither log nor refuse.
func TestHooksInvokeBehindInvokeHeldAdmitsOnce(t *testing.T) {
	var hooks spec.Hooks
	own := 0
	srv, err := spec.NewServer(handCluster(), spec.Options{}, spec.ServerConfig{Handlers: map[uint32]spec.CommandHandler{
		0: func(context.Context, any) (any, error) { return nil, nil },
		1: func(ctx context.Context, fields any) (any, error) {
			if resp, handled, err := hooks.Invoke(ctx, 1, fields); handled {
				return resp, err
			}
			own++
			return nil, nil
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	hooks.Init(srv.Instance)
	field := 0
	if err := hooks.Handle(1, func(context.Context, any) (any, error) { field++; return nil, nil }); err != nil {
		t.Fatal(err)
	}
	held := spec.NewSelfHeld(srv)

	var logged bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, nil)))
	defer slog.SetDefault(prev)

	ctx := context.Background()
	misfit := handReq{Level: 101}
	if _, err := wireInvokeHeld(ctx, srv, &held, 1, misfit); err != nil || field != 1 || own != 0 {
		t.Errorf("behind InvokeHeld: %v, field handler %d, own logic %d; want the field's handler alone", err, field, own)
	}
	if n := strings.Count(logged.String(), "command=Logged"); n != 1 {
		t.Errorf("the misfit was logged %d times over the wire, want once:\n%s", n, logged.String())
	}
	// Local calls: nothing is held, nothing logged.
	if _, err := spec.InvokeHeld(ctx, &held, 1, misfit); err != nil || field != 2 {
		t.Errorf("InvokeHeld, local: %v, field handler %d", err, field)
	}
	if _, handled, err := hooks.Invoke(ctx, 1, misfit); !handled || err != nil || field != 3 {
		t.Errorf("Hooks.Invoke, local: handled %t, %v, field handler %d", handled, err, field)
	}
	if _, err := srv.MatterInvoke(ctx, 0, misfit); err != nil {
		t.Errorf("MatterInvoke of a stated refusal, local: %v; want the handler", err)
	}
	if n := strings.Count(logged.String(), "command=Logged"); n != 1 {
		t.Errorf("local calls logged the misfit: %d in all, want 1", n)
	}
	// Over the wire, the stated refusal is answered before the server.
	if _, err := wireInvokeHeld(ctx, srv, &held, 0, misfit); statusOf(err) != im.StatusConstraintError {
		t.Errorf("a stated refusal over the wire: %v, want CONSTRAINT_ERROR", err)
	}
}
