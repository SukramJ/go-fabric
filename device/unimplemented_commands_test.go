// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"context"
	"slices"
	"testing"

	"thingfabric.dev/go-fabric/cluster/spec"

	modeselectdef "thingfabric.dev/go-fabric/cluster/spec/modeselect"
)

// TestDefaultServerCommandsUnimplementedUntilHandled: the generated
// default server — the default of a cluster without a self-held one, here
// ModeSelect, whose server needs the host's mode port — is matter.js's
// generated behavior, whose commands are Behavior.unimplemented: non-strict
// validation drops them from AcceptedCommandList (ValidatedElements.ts:341-343,
// :361-368; ServerBehaviorBacking.ts:119-136 at db021da0). A handler the
// host registers lists the command again.
func TestDefaultServerCommandsUnimplementedUntilHandled(t *testing.T) {
	t.Parallel()
	d, err := NewModeSelect(testOptions("m"), req("ModeSelect")...)
	if err != nil {
		t.Fatal(err)
	}
	ms, ok := d.ModeSelect().Underlying().(*spec.Server)
	if !ok {
		t.Fatalf("ModeSelect's default is %T, want the generated server", d.ModeSelect().Underlying())
	}
	if got := ms.MatterAcceptedCommands(); len(got) != 0 {
		t.Fatalf("AcceptedCommandList %v before any handler", got)
	}
	if err := ms.Handle(modeselectdef.CmdChangeToMode, func(context.Context, any) (any, error) { return nil, nil }); err != nil {
		t.Fatal(err)
	}
	if got := ms.MatterAcceptedCommands(); !slices.Equal(got, []uint32{modeselectdef.CmdChangeToMode}) {
		t.Fatalf("AcceptedCommandList %v after Handle", got)
	}
}
