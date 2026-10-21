// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package modebase_test

import (
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/modebase"
	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/cluster/spec/rvcrunmode"
	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
	"github.com/SukramJ/go-fabric/contract"
)

// TestTheChangesOrigin is the origin audit of the ModeBase server: a
// host's Set reports OriginHostSet, a ChangeToMode OriginCommand, a
// controller's write of the read-only CurrentMode nothing.
func TestTheChangesOrigin(t *testing.T) {
	t.Parallel()
	spectest.CheckOrigins(t, func(tb testing.TB) contract.ClusterServer {
		tb.Helper()
		return newRun(t, &changer{})
	},
		spectest.OriginCase{Name: "host set", Want: contract.OriginHostSet, Act: func(tb testing.TB, srv contract.ClusterServer) {
			tb.Helper()
			one := uint8(1)
			if err := rvcrunmode.Wrap(srv).Set(rvcrunmode.Patch{CurrentMode: &one}); err != nil {
				tb.Fatal(err)
			}
		}},
		spectest.OriginCase{Name: "command", Want: contract.OriginCommand, Act: func(tb testing.TB, srv contract.ClusterServer) {
			tb.Helper()
			if _, err := srv.MatterInvoke(context.Background(), modebase.CmdChangeToMode, rvcrunmode.ChangeToModeRequest{NewMode: 1}); err != nil {
				tb.Fatal(err)
			}
		}},
		spectest.OriginCase{Name: "controller write", Act: func(tb testing.TB, srv contract.ClusterServer) {
			tb.Helper()
			if err := srv.MatterWrite(context.Background(), modebase.AttrCurrentMode, uint8(1)); err == nil {
				tb.Fatal("a write of the read-only CurrentMode succeeded")
			}
		}},
	)
}

// TestTypedServer drives the server through the generated typed form.
func TestTypedServer(t *testing.T) {
	t.Parallel()
	c := &changer{}
	srv := newRun(t, c)
	typed := rvcrunmode.Wrap(srv)
	if st := typed.Get(); len(st.SupportedModes) != len(runModes) || len(st.SupportedModes[0].ModeTags) != 1 {
		t.Errorf("SupportedModes read back as %+v", st.SupportedModes)
	}
	nine := uint8(9)
	if err := typed.Set(rvcrunmode.Patch{CurrentMode: &nine}); !errors.Is(err, modebase.ErrUnsupportedMode) {
		t.Errorf("Set of an unsupported mode: %v, want ErrUnsupportedMode", err)
	}
	modes := []rvcrunmode.ModeOptionStruct{
		{Label: "x", ModeTags: []rvcrunmode.ModeTagStruct{{Value: rvcrunmode.ModeTagIdle}}},
		{Label: "y", Mode: 1, ModeTags: []rvcrunmode.ModeTagStruct{{Value: rvcrunmode.ModeTagCleaning}}},
	}
	if err := typed.Set(rvcrunmode.Patch{SupportedModes: &modes}); !errors.Is(err, spec.ErrNotHeld) {
		t.Errorf("Set of SupportedModes: %v, want ErrNotHeld", err)
	}
	if err := typed.Set(rvcrunmode.Patch{}); err != nil {
		t.Errorf("an empty Set: %v", err)
	}
	if err := typed.HandleChangeToMode(func(context.Context, rvcrunmode.ChangeToModeRequest) (rvcrunmode.ChangeToModeResponse, error) {
		return rvcrunmode.ChangeToModeResponse{}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.MatterInvoke(context.Background(), modebase.CmdChangeToMode, rvcrunmode.ChangeToModeRequest{NewMode: 2}); err != nil {
		t.Errorf("ChangeToMode through the host handler: %v", err)
	}
	if len(c.asked) != 0 {
		t.Errorf("the changer was asked %v, want nothing", c.asked)
	}
	if err := typed.OnWrite(func(context.Context, rvcrunmode.State, uint32) error { return nil }); err != nil {
		t.Errorf("OnWrite: %v", err)
	}
	if err := spec.EmitOn(srv, 1, 0, nil); !errors.Is(err, spec.ErrNotEmitted) {
		t.Errorf("Emit of an event ModeBase does not define: %v, want ErrNotEmitted", err)
	}
}
