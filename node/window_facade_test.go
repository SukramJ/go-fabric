// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"
)

// The facade opens and closes the node's window without the bridge's
// window type (W5-api-01): a commissioned node opens its own window,
// CloseWindow ends it whoever opened it, and OpenOwnWindow reopens it —
// matter.js DeviceCommissioner.allowBasicCommissioning / endCommissioning
// (DeviceCommissioner.ts:130-137, :315-325).
func TestNodeOpenCloseReopenOwnWindow(t *testing.T) {
	t.Parallel()
	site := quietSite(t)
	p := site.CommissionedPair(fabrictest.PairOptions{})
	n := p.Device.Node()
	ctx := parityCtx(t)
	if w := n.Window(); w.Open {
		t.Fatalf("a commissioned node starts with window %+v, want none", w)
	}

	if err := n.OpenOwnWindow(ctx, 3*time.Minute); err != nil {
		t.Fatal(err)
	}
	if w := n.Window(); !w.Open || w.Kind != node.WindowOwn || w.Timeout != 3*time.Minute {
		t.Fatalf("after OpenOwnWindow: %+v, want the own window for 3 min", w)
	}

	if err := n.CloseWindow(ctx); err != nil {
		t.Fatal(err)
	}
	if w := n.Window(); w.Open {
		t.Fatalf("after CloseWindow: %+v, want none", w)
	}
	if err := n.CloseWindow(ctx); err != nil {
		t.Fatalf("CloseWindow without a window: %v, want nil", err)
	}

	if err := n.OpenOwnWindow(ctx, 0); err != nil {
		t.Fatal(err)
	}
	if w := n.Window(); !w.Open || w.Kind != node.WindowOwn || w.Timeout != 15*time.Minute {
		t.Fatalf("after the reopen: %+v, want the own window for the commissioned default 15 min", w)
	}
	if err := n.CloseWindow(ctx); err != nil {
		t.Fatal(err)
	}
}
