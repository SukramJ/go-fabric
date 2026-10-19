// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behaviors/door-lock/DoorLockServerTest.ts
// at 862ad75f: describe("auto relock (spec § 5.2.9.22)") (:971-991) and
// describe("auto relock with the largest AutoRelockTime") (:993-1006), on
// setUpScheduledLock (doorlockserver_harness_test.go).

package lock_test

import (
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/lock"
	lockdef "github.com/SukramJ/go-fabric/cluster/spec/doorlock"
)

var (
	stateLocked   = uint8(lockdef.LockStateLocked)
	stateUnlocked = uint8(lockdef.LockStateUnlocked)
)

func autoRelockTime(seconds uint32) func(*lock.DoorLockConfig) {
	return func(c *lock.DoorLockConfig) { c.AutoRelockTime = &seconds }
}

func TestDoorLockServerAutoRelockParity(t *testing.T) {
	t.Run("relocks once AutoRelockTime has passed since the latest unlock", func(t *testing.T) {
		f := setUpScheduledLock(t, autoRelockTime(5), scheduledUser(lockdef.UserTypeUnrestrictedUser, nil))
		if err := f.unlockScheduled(); err != nil {
			t.Fatal(err)
		}
		if got := f.lockStateNow(); got != stateUnlocked {
			t.Fatalf("lockState %v, want Unlocked", got)
		}

		f.advance(3 * time.Second)
		if err := f.unlockScheduled(); err != nil {
			t.Fatal(err)
		}

		f.advance(3 * time.Second)
		f.settle()
		if got := f.lockStateNow(); got != stateUnlocked {
			t.Fatalf("lockState %v 3 s after the second unlock, want Unlocked", got)
		}

		f.advance(3 * time.Second)
		f.settle()
		if got := f.lockStateNow(); got != stateLocked {
			t.Fatalf("lockState %v 6 s after the second unlock, want Locked", got)
		}
	})
}

func TestDoorLockServerAutoRelockLargestTimeParity(t *testing.T) {
	t.Run("unlocks and stays unlocked", func(t *testing.T) {
		f := setUpScheduledLock(t, autoRelockTime(0xFFFFFFFF), scheduledUser(lockdef.UserTypeUnrestrictedUser, nil))
		if err := f.unlockScheduled(); err != nil {
			t.Fatal(err)
		}
		if got := f.lockStateNow(); got != stateUnlocked {
			t.Fatalf("lockState %v, want Unlocked", got)
		}
		f.advance(time.Hour)
		f.settle()
		if got := f.lockStateNow(); got != stateUnlocked {
			t.Fatalf("lockState %v an hour later, want Unlocked", got)
		}
	})
}
