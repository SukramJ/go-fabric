// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package closure_test

import (
	"context"
	"errors"
	"testing"

	closuredef "thingfabric.dev/go-fabric/cluster/spec/closurecontrol"
	"thingfabric.dev/go-fabric/cluster/wire"
)

// TestDeprecatedHooksHandlerAnswersACommand (decision 177): a handler
// registered through the deprecated Hooks field answers the command when
// no handler is registered with Handle, as it did before decision 154;
// the server's own logic does not run, and the handler's error is the
// command's.
func TestDeprecatedHooksHandlerAnswersACommand(t *testing.T) {
	t.Parallel()
	srv, _, h := newEmittingServer(t, wire.ClosureControlFeaturePositioning)
	srv.SetMainState(wire.ClosureMainStateMoving) // so the server's own Stop reaches the mover
	refused := errors.New("refused by the field's handler")
	calls := 0
	if err := srv.Hooks.Handle(closuredef.CmdStop, func(context.Context, any) (any, error) { //nolint:staticcheck // the deprecated field is the subject
		calls++
		return nil, refused
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.MatterInvoke(context.Background(), closuredef.CmdStop, nil); !errors.Is(err, refused) || calls != 1 || h.stopped != 0 {
		t.Errorf("Stop with a handler on the deprecated field: %v, handler calls %d, mover stops %d; want the handler's error, 1, 0", err, calls, h.stopped)
	}
	if err := srv.Hooks.Handle(closuredef.CmdStop, nil); err != nil { //nolint:staticcheck // the deprecated field is the subject
		t.Fatal(err)
	}
	if _, err := srv.MatterInvoke(context.Background(), closuredef.CmdStop, nil); err != nil || calls != 1 || h.stopped != 1 {
		t.Errorf("Stop with the field's handler removed: %v, handler calls %d, mover stops %d; want the server's own Stop", err, calls, h.stopped)
	}
}

// TestHandleTakesPrecedenceOverTheDeprecatedHooks (decision 177): with a
// handler registered both with Handle and on the deprecated field, Handle's
// runs; Handle is mirrored into the field, so Handle(nil) hands the
// command back to the server's own logic on both paths.
func TestHandleTakesPrecedenceOverTheDeprecatedHooks(t *testing.T) {
	t.Parallel()
	srv, _, h := newEmittingServer(t, wire.ClosureControlFeaturePositioning)
	srv.SetMainState(wire.ClosureMainStateMoving) // so the server's own Stop reaches the mover
	var ran []string
	if err := srv.Handle(closuredef.CmdStop, func(context.Context, any) (any, error) { ran = append(ran, "Handle"); return nil, nil }); err != nil {
		t.Fatal(err)
	}
	if err := srv.Hooks.Handle(closuredef.CmdStop, func(context.Context, any) (any, error) { ran = append(ran, "Hooks"); return nil, nil }); err != nil { //nolint:staticcheck // the deprecated field is the subject
		t.Fatal(err)
	}
	if _, err := srv.MatterInvoke(context.Background(), closuredef.CmdStop, nil); err != nil || len(ran) != 1 || ran[0] != "Handle" {
		t.Errorf("Stop with both handlers: %v, ran %v; want Handle's alone", err, ran)
	}
	if err := srv.Handle(closuredef.CmdStop, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.MatterInvoke(context.Background(), closuredef.CmdStop, nil); err != nil || len(ran) != 1 || h.stopped != 1 {
		t.Errorf("Stop after Handle(nil): %v, ran %v, mover stops %d; want the server's own Stop", err, ran, h.stopped)
	}
}

// TestDeprecatedPromotedMethodsSeeTheNewSetters (decision 177): the
// methods the deprecated field promotes onto the server — Invoke and
// CheckWrite — see the handler Handle registered and the veto
// SetBeforeWrite installed, as they did when Handle and SetBeforeWrite
// were the field's own.
func TestDeprecatedPromotedMethodsSeeTheNewSetters(t *testing.T) {
	t.Parallel()
	srv, _, _ := newEmittingServer(t, wire.ClosureControlFeaturePositioning)
	calls := 0
	if err := srv.Handle(closuredef.CmdStop, func(context.Context, any) (any, error) { calls++; return nil, nil }); err != nil {
		t.Fatal(err)
	}
	if _, handled, err := srv.Invoke(context.Background(), closuredef.CmdStop, nil); !handled || err != nil || calls != 1 { //nolint:staticcheck // the deprecated promoted method is the subject
		t.Errorf("Invoke after Handle: handled %t, %v, handler calls %d; want the host's handler", handled, err, calls)
	}
	veto := errors.New("vetoed")
	srv.SetBeforeWrite(func(context.Context, uint32, any) error { return veto })
	if err := srv.CheckWrite(context.Background(), wire.ClosureControlAttrMainState, uint8(1)); !errors.Is(err, veto) { //nolint:staticcheck // the deprecated promoted method is the subject
		t.Errorf("CheckWrite after SetBeforeWrite = %v, want the veto", err)
	}
}
