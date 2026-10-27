// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package thread_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/thread"
)

// Attached is the completion an activation waits for: an attached role,
// neither disabled nor detached (GenericThreadBorderRouterDelegate.cpp:
// 145-157 at connectedhomeip 6170af84).
func TestDeviceRoleAttached(t *testing.T) {
	t.Parallel()
	for role, want := range map[thread.DeviceRole]bool{
		thread.RoleDisabled: false, thread.RoleDetached: false, "": false, "unknown": false,
		thread.RoleChild: true, thread.RoleRouter: true, thread.RoleLeader: true,
	} {
		if got := role.Attached(); got != want {
			t.Errorf("%q.Attached() = %v, want %v", role, got, want)
		}
	}
}
