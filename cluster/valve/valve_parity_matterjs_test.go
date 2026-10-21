// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package valve_test

import (
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
	valvedef "github.com/SukramJ/go-fabric/cluster/spec/valveconfigurationandcontrol"
	"github.com/SukramJ/go-fabric/cluster/valve"
	"github.com/SukramJ/go-fabric/im"
)

// TestServerMatchesTheGeneratedDefinition holds the server against
// matter.js valve-configuration-and-control.element.ts for the one feature
// selection it serves (none): the mandatory elements listed, nothing
// disallowed, matter.js's privileges, UNSUPPORTED_WRITE on every read-only
// attribute.
func TestServerMatchesTheGeneratedDefinition(t *testing.T) {
	t.Parallel()
	spectest.CheckServer(t, valve.NewServer(valve.Config{Source: fullyObservedHost()}), valvedef.Definition, 0)
}

// TestServer_OpenTakesTheGeneratedRequest pins the shape the bridge hands
// over once the definition is registered: the generated OpenRequest, by
// value or pointer, with an absent OpenDuration kept apart from a null one
// and a TargetLevel refused without LVL.
func TestServer_OpenTakesTheGeneratedRequest(t *testing.T) {
	t.Parallel()
	sixty := spec.ValueOf[uint32](60)
	null := spec.NullOf[uint32]()
	cases := []struct {
		name     string
		fields   any
		has      bool
		duration uint32 // 0: nil
	}{
		{"absent", valvedef.OpenRequest{}, false, 0},
		{"null", valvedef.OpenRequest{OpenDuration: &null}, true, 0},
		{"value", valvedef.OpenRequest{OpenDuration: &sixty}, true, 60},
		{"pointer", &valvedef.OpenRequest{OpenDuration: &sixty}, true, 60},
		{"nil pointer", (*valvedef.OpenRequest)(nil), false, 0},
	}
	for _, tc := range cases {
		host := fullyObservedHost()
		srv := valve.NewServer(valve.Config{Source: host})
		if _, err := srv.MatterInvoke(context.Background(), valve.CmdOpen, tc.fields); err != nil {
			t.Fatalf("%s: Open: %v", tc.name, err)
		}
		got := host.openCalls[0]
		if has := got.OpenDuration != nil; has != tc.has {
			t.Errorf("%s: OpenDuration present = %v, want %v", tc.name, has, tc.has)
		}
		switch d := got.OpenDuration; {
		case tc.duration == 0 && d != nil && !d.Null:
			t.Errorf("%s: OpenDuration = %d, want absent or null", tc.name, d.Value)
		case tc.duration != 0 && (d == nil || d.Null || d.Value != tc.duration):
			t.Errorf("%s: OpenDuration = %+v, want %d", tc.name, d, tc.duration)
		}
	}

	level := uint8(50)
	host := fullyObservedHost()
	srv := valve.NewServer(valve.Config{Source: host})
	_, err := srv.MatterInvoke(context.Background(), valve.CmdOpen, valvedef.OpenRequest{TargetLevel: &level})
	var sce im.StatusCodeError
	if !errors.As(err, &sce) || sce.MatterStatusCode() != im.StatusConstraintError {
		t.Errorf("Open with TargetLevel = %v, want CONSTRAINT_ERROR", err)
	}
	if len(host.openCalls) != 0 {
		t.Error("an Open with TargetLevel reached the host")
	}
}
