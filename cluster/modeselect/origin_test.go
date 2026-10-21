// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package modeselect_test

import (
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/modeselect"
	"github.com/SukramJ/go-fabric/cluster/spec"
	msdef "github.com/SukramJ/go-fabric/cluster/spec/modeselect"
)

// TestModeSelectOrigins is the origin audit of the pull ModeSelect
// server. The mode lives in the host's ModeSource, so the server holds
// nothing of its own and reports no attribute change itself (it is no
// contract.AttributeChangeNotifier, which spectest.CheckOrigins needs;
// the source's ChangeNotifier is the route a change takes): the host's
// Set is refused (ErrNotHeld) and changes nothing, ChangeToMode reaches
// the source, and the cluster serves no writable attribute — a write of
// CurrentMode is refused.
func TestModeSelectOrigins(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	src := coffeeSource()
	srv := modeselect.NewServer(modeselect.Config{Source: src})

	mode := uint8(1)
	if err := msdef.Wrap(srv).Set(msdef.Patch{CurrentMode: &mode}); !errors.Is(err, spec.ErrNotHeld) {
		t.Errorf("host set: %v, want ErrNotHeld", err)
	}
	if _, err := srv.MatterInvoke(ctx, modeselect.CmdChangeToMode, msdef.ChangeToModeRequest{NewMode: 1}); err != nil || len(src.changed) != 1 {
		t.Errorf("command: %v, the source saw %v, want one ChangeToMode", err, src.changed)
	}
	if err := srv.MatterWrite(ctx, modeselect.AttrCurrentMode, uint8(1)); err == nil {
		t.Error("a write of the read-only CurrentMode succeeded")
	}
}
