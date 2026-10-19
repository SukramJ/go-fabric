// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behaviors/door-lock/DoorLockServerTest.ts
// at 862ad75f: describe("lock operation events") (:1685-1702) — UnboltDoor
// reports an Unlock, on setUpScheduledLock (doorlockserver_harness_test.go).

package lock_test

import (
	"testing"

	lockdef "github.com/SukramJ/go-fabric/cluster/spec/doorlock"
)

func TestDoorLockServerLockOperationEventsParity(t *testing.T) {
	t.Run("reports UnboltDoor as Unlock and leaves the latch alone", func(t *testing.T) {
		f := setUpScheduledLock(t, withLockState(lockdef.LockStateLocked), scheduledUser(lockdef.UserTypeUnrestrictedUser, nil))
		f.must(lockdef.CmdUnboltDoor, lockdef.UnboltDoorRequest{PinCode: pinPtr("1234")})
		if got := f.lockStateNow(); got != stateUnlocked {
			t.Errorf("lockState %v, want Unlocked", got)
		}
		if ops := f.events().operations; len(ops) != 1 || ops[0].LockOperationType != lockdef.LockOperationTypeUnlock {
			t.Errorf("operations %+v, want [Unlock]", ops)
		}

		if _, err := f.invoke(lockdef.CmdUnboltDoor, lockdef.UnboltDoorRequest{PinCode: pinPtr("9999")}); err == nil {
			t.Fatal("UnboltDoor with a wrong PIN was granted")
		}
		if errs := f.events().errors; len(errs) != 1 || errs[0].LockOperationType != lockdef.LockOperationTypeUnlock {
			t.Errorf("error events %+v, want [Unlock]", errs)
		}
	})
}
