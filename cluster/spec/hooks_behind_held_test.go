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

// TestHooksInvokeBehindInvokeHeldAdmitsOnce (decision 177): a server that
// keeps the deprecated Hooks field for its window runs the field's
// Invoke from its own logic, behind [spec.InvokeHeld], so that a handler
// registered on the field still answers. The request InvokeHeld admitted
// is not held a second time there — a misfit of a command that states no
// refusal is logged once — while Hooks.Invoke called on its own still
// holds the request first, as before.
func TestHooksInvokeBehindInvokeHeldAdmitsOnce(t *testing.T) {
	var hooks spec.Hooks
	own := 0
	// inner is what the server's own logic of command 1 runs first: the
	// field's Invoke, as a server keeping the field does.
	inner := func(ctx context.Context, fields any) (any, bool, error) { return hooks.Invoke(ctx, 1, fields) }
	srv, err := spec.NewServer(handCluster(), spec.Options{}, spec.ServerConfig{Handlers: map[uint32]spec.CommandHandler{
		0: func(context.Context, any) (any, error) { return nil, nil },
		1: func(ctx context.Context, fields any) (any, error) {
			if resp, handled, err := inner(ctx, fields); handled {
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

	misfit := handReq{Level: 101}
	if _, err := spec.InvokeHeld(context.Background(), &held, 1, misfit); err != nil || field != 1 || own != 0 {
		t.Errorf("behind InvokeHeld: %v, field handler %d, own logic %d; want the field's handler alone", err, field, own)
	}
	if n := strings.Count(logged.String(), "command=Logged"); n != 1 {
		t.Errorf("the misfit was logged %d times behind InvokeHeld, want once:\n%s", n, logged.String())
	}

	// On its own, Hooks.Invoke holds the request first, as before.
	if _, handled, err := hooks.Invoke(context.Background(), 1, misfit); !handled || err != nil || field != 2 {
		t.Errorf("Hooks.Invoke on its own: handled %t, %v, field handler %d", handled, err, field)
	}
	if n := strings.Count(logged.String(), "command=Logged"); n != 2 {
		t.Errorf("Hooks.Invoke on its own logged the misfit %d times in all, want 2", n)
	}
	if _, handled, err := hooks.Invoke(context.Background(), 0, misfit); !handled || statusOf(err) != im.StatusConstraintError {
		t.Errorf("Hooks.Invoke on its own, a stated refusal: handled %t, %v; want CONSTRAINT_ERROR", handled, err)
	}
	// The context InvokeHeld hands the own logic excuses that request
	// alone: another command of the instance, or a request to another
	// instance, is held as before.
	inner = func(ctx context.Context, _ any) (any, bool, error) { return hooks.Invoke(ctx, 0, misfit) }
	if _, err := spec.InvokeHeld(context.Background(), &held, 1, handReq{Level: 1}); statusOf(err) != im.StatusConstraintError {
		t.Errorf("another command behind InvokeHeld: %v, want CONSTRAINT_ERROR", err)
	}
	other, err := spec.New(handCluster(), spec.Options{})
	if err != nil {
		t.Fatal(err)
	}
	var otherHooks spec.Hooks
	otherHooks.Init(other)
	inner = func(ctx context.Context, fields any) (any, bool, error) { return otherHooks.Invoke(ctx, 1, fields) }
	if _, err := spec.InvokeHeld(context.Background(), &held, 1, misfit); err != nil || own != 1 {
		t.Errorf("another instance behind InvokeHeld: %v, own logic %d; want it to run once", err, own)
	}
	if n := strings.Count(logged.String(), "command=Logged"); n != 4 {
		t.Errorf("logged the misfit %d times in all, want 4 (InvokeHeld and the other instance's own gate)", n)
	}
}
