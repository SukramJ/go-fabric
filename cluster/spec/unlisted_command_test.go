// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"thingfabric.dev/go-fabric/cluster/spec"
	"thingfabric.dev/go-fabric/im"
)

// unlistedCluster has a mandatory command (0, status) and an optional one
// (1) answered by a response command (0x10).
var unlistedCluster = &spec.Cluster{
	ID: 0xFFF1_FC05, Name: "Unlisted", Revision: 1,
	Commands: []spec.Command{
		{ID: 0, Name: "Mandatory", Direction: spec.Request, Response: "status", Conformance: op(spec.ConfMandatory), Access: spec.Access{Write: spec.PrivilegeOperate}},
		{ID: 1, Name: "Optional", Direction: spec.Request, Response: "OptionalResponse", Conformance: op(spec.ConfOptional), Access: spec.Access{Write: spec.PrivilegeOperate}},
		{ID: 0x10, Name: "OptionalResponse", Direction: spec.Response, Conformance: op(spec.ConfMandatory)},
	},
}

// TestOptionalCommandListedOnlyWithHandler: an optional command the server
// declares (spec.Options.Commands) without a handler is refused at
// construction, naming the command, unless the server is the generated
// behavior (ServerConfig.Unimplemented) — before, it dropped out of
// AcceptedCommandList with no word (review II, phase 5b). With
// Unimplemented it is in neither AcceptedCommandList nor — its response —
// GeneratedCommandList and answers UNSUPPORTED_COMMAND, until Handle
// registers it; nil takes it out again. It is no unimplemented mandatory
// command (R2-P5-apispec-04).
func TestOptionalCommandListedOnlyWithHandler(t *testing.T) {
	_, err := spec.NewServer(unlistedCluster, spec.Options{Commands: []uint32{1}}, spec.ServerConfig{
		Handlers: map[uint32]spec.CommandHandler{0: noop},
	})
	if !errors.Is(err, spec.ErrMissingHandler) || !strings.Contains(err.Error(), "Optional (0x01)") {
		t.Errorf("an optional command selected without a handler = %v, want ErrMissingHandler naming Optional (0x01)", err)
	}
	if _, err := spec.NewServer(unlistedCluster, spec.Options{Commands: []uint32{1}}, spec.ServerConfig{
		Handlers: map[uint32]spec.CommandHandler{0: noop, 1: noop},
	}); err != nil {
		t.Errorf("an optional command selected with its handler: %v", err)
	}
	srv, err := spec.NewServer(unlistedCluster, spec.Options{Commands: []uint32{1}}, spec.ServerConfig{
		Handlers:      map[uint32]spec.CommandHandler{0: noop},
		Unimplemented: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	lists := func(wantAccepted, wantGenerated []uint32) {
		t.Helper()
		if got := srv.MatterAcceptedCommands(); !slices.Equal(got, wantAccepted) {
			t.Errorf("AcceptedCommandList %v, want %v", got, wantAccepted)
		}
		if got := srv.MatterGeneratedCommands(); !slices.Equal(got, wantGenerated) {
			t.Errorf("GeneratedCommandList %v, want %v", got, wantGenerated)
		}
	}
	lists([]uint32{0}, []uint32{})
	if srv.Accepts(1) {
		t.Error("Accepts(1) without a handler")
	}
	if _, err := srv.MatterInvoke(context.Background(), 1, nil); statusOf(err) != im.StatusUnsupportedCommand {
		t.Errorf("invoke without a handler: %v", err)
	}
	if got := srv.UnimplementedMandatoryCommands(); len(got) != 0 {
		t.Errorf("UnimplementedMandatoryCommands %v", got)
	}
	if err := srv.Handle(1, noop); err != nil {
		t.Fatal(err)
	}
	lists([]uint32{0, 1}, []uint32{0x10})
	if err := srv.Handle(1, nil); err != nil {
		t.Fatal(err)
	}
	lists([]uint32{0}, []uint32{})
}
