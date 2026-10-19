// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behaviors/door-lock/DoorLockServerTest.ts
// at 862ad75f: describe("wrong-code lockout") (:1325-1417) inside
// "authorization of lock operations" — WrongCodeEntryLimit,
// UserCodeTemporaryDisableTime and the attempt count, on
// setUpScheduledLock (doorlockserver_harness_test.go).
//
// "measures the lockout on the monotonic clock" (:1366-1380) is not
// ported: it steps MockTime's wall clock alone and expects the lockout to
// hold, but clocktest.Fake has one reading, so a step moves the lockout's
// deadline reading as well. The server compares time.Time values, which
// clock.System carries with a monotonic reading — the matter.js behaviour
// in production, which no Go test clock can step apart.

package lock_test

import (
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/lock"
	lockdef "github.com/SukramJ/go-fabric/cluster/spec/doorlock"
)

// enterLockout is DoorLockServerTest.ts:1326-1333.
func (f *lockFixture) enterLockout() {
	f.t.Helper()
	for range 3 {
		if err := f.unlock("9999"); err == nil {
			f.t.Fatal("a wrong PIN unlocked")
		}
	}
	ev := f.allEvents()
	if len(ev.alarms) != 1 || ev.alarms[0] != lockdef.AlarmCodeWrongCodeEntryLimit {
		f.t.Fatalf("alarms %v, want [WrongCodeEntryLimit]", ev.alarms)
	}
	if got := ev.operationErrors(); len(got) != 3 || got[0] != lockdef.OperationErrorInvalidCredential ||
		got[1] != lockdef.OperationErrorInvalidCredential || got[2] != lockdef.OperationErrorInvalidCredential {
		f.t.Fatalf("operation errors %v, want three InvalidCredential", got)
	}
	f.markEvents()
}

// alarmsSoFar are the alarms since the case started.
func (f *lockFixture) alarmsSoFar() []lockdef.AlarmCodeEnum {
	f.t.Helper()
	return f.allEvents().alarms
}

func TestDoorLockServerWrongCodeLockoutParity(t *testing.T) {
	wrongCodeOnce := []lockdef.AlarmCodeEnum{lockdef.AlarmCodeWrongCodeEntryLimit}

	t.Run("refuses every lock operation during the lockout, with or without PIN, and reports none", func(t *testing.T) {
		f := setUpScheduledLock(t, withLockState(lockdef.LockStateLocked), scheduledUser(lockdef.UserTypeUnrestrictedUser, nil))
		f.enterLockout()
		for _, code := range []string{"", "1234", "9999"} {
			if err := f.unlock(code); err == nil {
				t.Fatalf("unlock with %q during the lockout was granted", code)
			}
		}
		if got := f.lockStateNow(); got != stateLocked {
			t.Errorf("lockState %v, want Locked", got)
		}
		if calls := f.hw.snapshot(); len(calls) != 0 {
			t.Errorf("hardware calls %+v, want none", calls)
		}
		if got := f.events().operationErrors(); len(got) != 0 {
			t.Errorf("operation errors %v, want none", got)
		}
		if got := f.alarmsSoFar(); !equalAlarms(got, wrongCodeOnce) {
			t.Errorf("alarms %v, want %v", got, wrongCodeOnce)
		}
	})

	t.Run("ends the lockout after UserCodeTemporaryDisableTime with a fresh attempt count", func(t *testing.T) {
		f := setUpScheduledLock(t, nil, scheduledUser(lockdef.UserTypeUnrestrictedUser, nil))
		f.enterLockout()
		f.advance(9 * time.Second)
		if err := f.unlockScheduled(); err == nil {
			t.Fatal("unlock 9 s into the lockout was granted")
		}
		f.advance(1 * time.Second)
		for range 2 {
			if err := f.unlock("9999"); err == nil {
				t.Fatal("a wrong PIN unlocked")
			}
		}
		if err := f.unlockScheduled(); err != nil {
			t.Fatalf("unlock after the lockout: %v", err)
		}
		if got := f.alarmsSoFar(); !equalAlarms(got, wrongCodeOnce) {
			t.Errorf("alarms %v, want %v", got, wrongCodeOnce)
		}
	})

	t.Run("does not count PINs refused by the operating mode", func(t *testing.T) {
		f := setUpScheduledLock(t, func(c *lock.DoorLockConfig) { c.OperatingMode = lockdef.OperatingModeNoRemoteLockUnlock },
			scheduledUser(lockdef.UserTypeUnrestrictedUser, nil))
		for range 3 {
			if err := f.unlock("9999"); err == nil {
				t.Fatal("an unlock in NoRemoteLockUnlock was granted")
			}
		}
		if got := f.alarmsSoFar(); len(got) != 0 {
			t.Errorf("alarms %v, want none", got)
		}
	})

	t.Run("starts the attempt count again after a correct code", func(t *testing.T) {
		f := setUpScheduledLock(t, nil, scheduledUser(lockdef.UserTypeUnrestrictedUser, nil))
		for _, code := range []string{"9999", "9999", "1234", "9999", "9999"} {
			err := f.unlock(code)
			if code == "1234" && err != nil {
				t.Fatalf("the correct code: %v", err)
			}
			if code != "1234" && err == nil {
				t.Fatal("a wrong PIN unlocked")
			}
		}
		if got := f.alarmsSoFar(); len(got) != 0 {
			t.Errorf("alarms %v, want none", got)
		}
	})

	t.Run("counts an omitted PIN as a wrong code when a PIN is required", func(t *testing.T) {
		f := setUpScheduledLock(t, func(c *lock.DoorLockConfig) { c.RequirePinForRemoteOperation = true },
			scheduledUser(lockdef.UserTypeUnrestrictedUser, nil))
		for range 3 {
			if err := f.unlock(""); err == nil {
				t.Fatal("an unlock without the required PIN was granted")
			}
		}
		if got := f.alarmsSoFar(); !equalAlarms(got, wrongCodeOnce) {
			t.Errorf("alarms %v, want %v", got, wrongCodeOnce)
		}
		if err := f.unlockScheduled(); err == nil {
			t.Error("unlock during the lockout was granted")
		}
	})
}

func equalAlarms(a, b []lockdef.AlarmCodeEnum) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
