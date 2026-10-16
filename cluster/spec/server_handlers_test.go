// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	provdef "github.com/SukramJ/go-fabric/cluster/spec/otasoftwareupdateprovider"
)

func noop(context.Context, any) (any, error) { return nil, nil }

// TestNewServer_MandatoryCommandWithoutHandler: a command the
// conformance requires and the configuration gives no handler is fatal at
// construction — matter.js ValidatedElements.ts:357-370 at 0ceb202d
// ("Implementation missing").
func TestNewServer_MandatoryCommandWithoutHandler(t *testing.T) {
	t.Parallel()
	_, err := spec.NewServer(provdef.Definition, spec.Options{}, spec.ServerConfig{
		Handlers: map[uint32]spec.CommandHandler{provdef.CmdQueryImage: noop, provdef.CmdApplyUpdateRequest: noop},
	})
	if !errors.Is(err, spec.ErrMissingHandler) {
		t.Fatalf("NewServer without NotifyUpdateApplied: %v, want ErrMissingHandler", err)
	}
	srv, err := spec.NewServer(provdef.Definition, spec.Options{}, spec.ServerConfig{Handlers: map[uint32]spec.CommandHandler{
		provdef.CmdQueryImage: noop, provdef.CmdApplyUpdateRequest: noop, provdef.CmdNotifyUpdateApplied: noop,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !srv.Handles(provdef.CmdQueryImage) || !srv.Accepts(provdef.CmdNotifyUpdateApplied) {
		t.Fatal("a handled command is not accepted")
	}
}

// TestNewServer_UnimplementedStub: a stub's commands are matter.js's
// Behavior.unimplemented under non-strict validation — the server
// constructs, the commands stay out of AcceptedCommandList and their
// responses out of GeneratedCommandList (ValidatedElements.ts:341-343,
// :361-368; ServerBehaviorBacking.ts:127-148 at 0ceb202d) until a handler
// is registered.
func TestNewServer_UnimplementedStub(t *testing.T) {
	t.Parallel()
	srv, err := spec.NewServer(provdef.Definition, spec.Options{}, spec.ServerConfig{Unimplemented: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := srv.MatterAcceptedCommands(); len(got) != 0 {
		t.Fatalf("AcceptedCommandList %v, want empty", got)
	}
	if got := srv.MatterGeneratedCommands(); len(got) != 0 {
		t.Fatalf("GeneratedCommandList %v, want empty", got)
	}
	if srv.Accepts(provdef.CmdQueryImage) {
		t.Fatal("an unimplemented command is accepted")
	}
	srv.Handle(provdef.CmdQueryImage, noop)
	if got := srv.MatterAcceptedCommands(); !slices.Equal(got, []uint32{provdef.CmdQueryImage}) {
		t.Fatalf("AcceptedCommandList %v after Handle", got)
	}
	if got := srv.MatterGeneratedCommands(); !slices.Equal(got, []uint32{provdef.CmdQueryImageResponse}) {
		t.Fatalf("GeneratedCommandList %v after Handle", got)
	}
	for _, id := range []uint32{provdef.CmdApplyUpdateRequest, provdef.CmdNotifyUpdateApplied} {
		srv.Handle(id, noop)
	}
	if got := srv.MatterAcceptedCommands(); len(got) != 3 || !srv.Handles(provdef.CmdNotifyUpdateApplied) {
		t.Fatalf("AcceptedCommandList %v with every handler", got)
	}
	// A server without stubs answers the instance's lists.
	full, err := spec.NewServer(provdef.Definition, spec.Options{}, spec.ServerConfig{Handlers: map[uint32]spec.CommandHandler{
		provdef.CmdQueryImage: noop, provdef.CmdApplyUpdateRequest: noop, provdef.CmdNotifyUpdateApplied: noop,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(full.MatterGeneratedCommands(), full.Instance.MatterGeneratedCommands()) {
		t.Fatal("GeneratedCommandList differs from the instance's")
	}
}
