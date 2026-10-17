// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"context"
	"slices"
	"testing"

	levelcontroldef "github.com/SukramJ/go-fabric/cluster/spec/levelcontrol"
)

// TestDefaultServerCommandsUnimplementedUntilHandled: the generated
// default server is matter.js's generated behavior, whose commands are
// Behavior.unimplemented — non-strict validation drops them from
// AcceptedCommandList (ValidatedElements.ts:341-343, :361-368;
// ServerBehaviorBacking.ts:127-148 at 0ceb202d). A handler the host
// registers lists the command again.
func TestDefaultServerCommandsUnimplementedUntilHandled(t *testing.T) {
	t.Parallel()
	d, err := NewDimmableLight(testOptions("d"))
	if err != nil {
		t.Fatal(err)
	}
	lc := d.LevelControl()
	if got := lc.MatterAcceptedCommands(); len(got) != 0 {
		t.Fatalf("AcceptedCommandList %v before any handler", got)
	}
	lc.Handle(levelcontroldef.CmdMoveToLevel, func(context.Context, any) (any, error) { return nil, nil })
	if got := lc.MatterAcceptedCommands(); !slices.Equal(got, []uint32{levelcontroldef.CmdMoveToLevel}) {
		t.Fatalf("AcceptedCommandList %v after Handle", got)
	}
}
