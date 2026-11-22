// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera_test

import (
	"context"
	"testing"

	csdef "thingfabric.dev/go-fabric/cluster/spec/cameraavstreammanagement"
	"thingfabric.dev/go-fabric/im"
)

// TestAvStreamViewportRuleSurvivesHostVeto: the server's own Viewport rule
// (spec.ServerConfig.WriteRule) refuses a 4:3 viewport on a 16:9 sensor
// before a host installs a veto, with a host veto that lets everything
// through, and after the host removes its veto — the host's OnWrite
// replaces only the host's veto (R2-P5-apispec-01).
func TestAvStreamViewportRuleSurvivesHostVeto(t *testing.T) {
	a, _ := newChipAvStream(t)
	bad := csdef.ViewportStruct{X2: 1280, Y2: 960}
	wantCode(t, "before OnWrite", writeStream(a, csdef.AttrViewport, bad), im.StatusConstraintError)
	calls := 0
	typed := csdef.Wrap(a)
	if err := typed.OnWrite(func(context.Context, csdef.State, uint32) error { calls++; return nil }); err != nil {
		t.Fatal(err)
	}
	wantCode(t, "after OnWrite", writeStream(a, csdef.AttrViewport, bad), im.StatusConstraintError)
	if calls != 0 {
		t.Fatalf("the host veto heard a write the server's rule refused: %d calls", calls)
	}
	if err := typed.OnWrite(nil); err != nil {
		t.Fatal(err)
	}
	wantCode(t, "after OnWrite(nil)", writeStream(a, csdef.AttrViewport, bad), im.StatusConstraintError)
}
