// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package lock_test

import (
	"context"
	"errors"
	"testing"

	doorlockcluster "github.com/SukramJ/go-fabric/cluster/lock"
	lockdef "github.com/SukramJ/go-fabric/cluster/spec/doorlock"
	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
	"github.com/SukramJ/go-fabric/im"
)

// TestServerMatchesTheGeneratedDefinition holds the server against matter.js
// door-lock-cluster.element.ts (spectest.CheckServer) for the one feature
// it serves, Unbolting.
func TestServerMatchesTheGeneratedDefinition(t *testing.T) {
	t.Parallel()
	srv := doorlockcluster.NewDoorLockServer(doorlockcluster.DoorLockConfig{Source: &stubSource{}})
	spectest.CheckServer(t, srv, lockdef.Definition, uint32(lockdef.FeatureUnbolting))
}

// TestRequestsTakeTheGeneratedStructs pins the shapes the bridge now hands
// over — the three requests decode through the definition — and the write
// statuses the definition answers: UNSUPPORTED_WRITE for a served read-only
// attribute, UNSUPPORTED_ATTRIBUTE for one the server does not serve,
// CONSTRAINT_ERROR for a value outside OperatingModeEnum.
func TestRequestsTakeTheGeneratedStructs(t *testing.T) {
	t.Parallel()
	src := &stubSource{}
	srv := doorlockcluster.NewDoorLockServer(doorlockcluster.DoorLockConfig{Source: src})
	pin := []byte("1234")
	for cmd, req := range map[uint32]any{
		lockdef.CmdLockDoor:   lockdef.LockDoorRequest{PinCode: &pin},
		lockdef.CmdUnlockDoor: lockdef.UnlockDoorRequest{},
		lockdef.CmdUnboltDoor: lockdef.UnboltDoorRequest{},
	} {
		if _, err := srv.MatterInvoke(context.Background(), cmd, req); err != nil {
			t.Errorf("command 0x%02X: %v", cmd, err)
		}
	}
	if len(src.invoked) != 3 {
		t.Errorf("LockInvoke calls %v, want three", src.invoked)
	}
	for _, c := range []struct {
		attr  uint32
		value any
		want  im.StatusCode
	}{
		{lockdef.AttrLockType, uint8(0), im.StatusUnsupportedWrite},
		{lockdef.AttrDoorOpenEvents, uint32(0), im.StatusUnsupportedAttribute},
		{lockdef.AttrOperatingMode, uint8(5), im.StatusConstraintError},
	} {
		err := srv.MatterWrite(context.Background(), c.attr, c.value)
		if sce, ok := errors.AsType[im.StatusCodeError](err); !ok || sce.MatterStatusCode() != c.want {
			t.Errorf("write 0x%04X = %v: %v, want status 0x%02X", c.attr, c.value, err, c.want)
		}
	}
}
