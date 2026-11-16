// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package closure

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	cd "github.com/SukramJ/go-fabric/cluster/spec/closuredimension"
)

// TestHeldServerReadsTheServersState: the spec.Server a self-held server
// routes its commands through answers its attributes — the peers a
// request's or response's conformance names — from the server's own
// read, not from defaults of its own (decision 154).
func TestHeldServerReadsTheServersState(t *testing.T) {
	t.Parallel()
	sentinel := struct{ held bool }{true}
	srv, err := newHeld(cd.Definition, spec.Options{Features: uint32(cd.FeaturePositioning | cd.FeatureTranslation)}, func(uint32) (any, bool) { return sentinel, true },
		func(context.Context, uint32, any) (any, error) { return nil, nil })
	if err != nil {
		t.Fatal(err)
	}
	read := 0
	for _, id := range srv.MatterAttributes() {
		if id >= 0xFFF8 {
			continue // the globals are the instance's
		}
		if v, _ := srv.MatterRead(id); v != any(sentinel) {
			t.Errorf("attribute 0x%04X reads %v, want the server's own value", id, v)
		}
		read++
	}
	if read == 0 {
		t.Fatal("no attribute read")
	}
}
