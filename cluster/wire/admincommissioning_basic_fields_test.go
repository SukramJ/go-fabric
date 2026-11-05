// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package wire_test

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	admdef "github.com/SukramJ/go-fabric/cluster/spec/administratorcommissioning"
	"github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/im"
)

// TestAdmComm_Basic_OpenBasicWindow_FieldShapes: OpenBasicCommissioningWindow
// takes its one field from every shape the dispatcher or a host hands in —
// the generated request by value and by pointer, and the window parameters
// by value and by pointer — and a nil pointer is malformed (InvalidCommand).
func TestAdmComm_Basic_OpenBasicWindow_FieldShapes(t *testing.T) {
	t.Parallel()
	ctx := im.WithFabricFilter(context.Background(), true, 1)
	for name, fields := range map[string]any{
		"request":         admdef.OpenBasicCommissioningWindowRequest{CommissioningTimeout: 200},
		"request pointer": &admdef.OpenBasicCommissioningWindowRequest{CommissioningTimeout: 200},
		"params":          wire.OpenWindowParams{CommissioningTimeoutSeconds: 200},
		"params pointer":  &wire.OpenWindowParams{CommissioningTimeoutSeconds: 200},
	} {
		ac := newBasicAdmComm()
		ctrl := &fakeWindowController{}
		ac.SetController(ctrl)
		if _, err := ac.MatterInvoke(ctx, admCommCmdOpenBasicWindow, fields); err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if got := ctrl.capturedParams; !got.IsBasicWindow || got.CommissioningTimeoutSeconds != 200 {
			t.Errorf("%s: window %+v, want a basic window of 200 s", name, got)
		}
	}
	for name, fields := range map[string]any{
		"nil request pointer": (*admdef.OpenBasicCommissioningWindowRequest)(nil),
		"nil params pointer":  (*wire.OpenWindowParams)(nil),
	} {
		ac := newBasicAdmComm()
		ac.SetController(&fakeWindowController{})
		if _, err := ac.MatterInvoke(ctx, admCommCmdOpenBasicWindow, fields); spec.StatusOf(err) != im.StatusInvalidCommand {
			t.Errorf("%s: %v, want InvalidCommand", name, err)
		}
	}
}
