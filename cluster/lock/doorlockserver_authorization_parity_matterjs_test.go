// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behaviors/door-lock/DoorLockServerTest.ts
// at 862ad75f: describe("authorization of lock operations") (:1209-1472)
// without its nested "wrong-code lockout" and "OperatingMode" — the
// operating modes that refuse remote operations and the user types the
// authorization treats apart, on setUpScheduledLock
// (doorlockserver_harness_test.go).

package lock_test

import (
	"fmt"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/lock"
	"github.com/SukramJ/go-fabric/cluster/spec"
	lockdef "github.com/SukramJ/go-fabric/cluster/spec/doorlock"
)

// pinOne is the seeded user's credential, [{ Pin, 1 }].
var pinOne = []lockdef.CredentialStruct{credential(lockdef.CredentialTypePin, 1)}

// credentialsOf reads an event's Credentials field, nil for null or absent.
func credentialsOf(c *spec.Nullable[[]lockdef.CredentialStruct]) []lockdef.CredentialStruct {
	if c == nil || c.Null {
		return nil
	}
	return c.Value
}

func withLockState(state lockdef.LockStateEnum) func(*lock.DoorLockConfig) {
	return func(c *lock.DoorLockConfig) { c.LockState = &state }
}

func TestDoorLockServerAuthorizationParity(t *testing.T) {
	// privacySupported is DoorLockServerTest.ts:1210.
	privacySupported := lockdef.OperatingModesAlwaysSet | lockdef.OperatingModesVacation | lockdef.OperatingModesPassage

	actuations := []struct {
		name string
		cmd  uint32
		req  spec.Encodable
	}{
		{"lockDoor", lockdef.CmdLockDoor, lockdef.LockDoorRequest{}},
		{"unlockDoor", lockdef.CmdUnlockDoor, lockdef.UnlockDoorRequest{}},
		{"unlockWithTimeout", lockdef.CmdUnlockWithTimeout, lockdef.UnlockWithTimeoutRequest{Timeout: 10}},
		{"unboltDoor", lockdef.CmdUnboltDoor, lockdef.UnboltDoorRequest{}},
	}
	for _, mode := range []lockdef.OperatingModeEnum{lockdef.OperatingModePrivacy, lockdef.OperatingModeNoRemoteLockUnlock} {
		for _, a := range actuations {
			t.Run(fmt.Sprintf("refuses ${name} in ${OperatingMode[mode]}/%s in %s", a.name, operatingModeName(mode)), func(t *testing.T) {
				initial := lockdef.LockStateLocked
				if a.name == "lockDoor" {
					initial = lockdef.LockStateUnlocked
				}
				f := setUpScheduledLock(t, func(c *lock.DoorLockConfig) {
					c.LockState = &initial
					c.OperatingMode = mode
					c.SupportedOperatingModes = privacySupported
				}, scheduledUser(lockdef.UserTypeUnrestrictedUser, nil))
				if _, err := f.invoke(a.cmd, a.req); err == nil {
					t.Fatal("the operation was granted")
				}
				if got := f.lockStateNow(); got != uint8(initial) {
					t.Errorf("lockState %v, want %d", got, initial)
				}
				if calls := f.hw.snapshot(); len(calls) != 0 {
					t.Errorf("hardware calls %+v, want none", calls)
				}
				ev := f.events()
				if got := ev.operationErrors(); len(got) != 1 || got[0] != lockdef.OperationErrorUnspecified {
					t.Errorf("operation errors %v, want [Unspecified]", got)
				}
				if len(ev.operations) != 0 {
					t.Errorf("operations %+v, want none", ev.operations)
				}
			})
		}
	}

	t.Run("refuses a NonAccessUser and reports the access attempt", func(t *testing.T) {
		f := setUpScheduledLock(t, withLockState(lockdef.LockStateLocked), scheduledUser(lockdef.UserTypeNonAccessUser, nil))
		if err := f.unlockScheduled(); err == nil {
			t.Fatal("the NonAccessUser unlocked")
		}
		if got := f.lockStateNow(); got != stateLocked {
			t.Errorf("lockState %v, want Locked", got)
		}
		if calls := f.hw.snapshot(); len(calls) != 0 {
			t.Errorf("hardware calls %+v, want none", calls)
		}
		ops := f.events().operations
		if len(ops) != 1 || ops[0].LockOperationType != lockdef.LockOperationTypeNonAccessUserEvent ||
			ops[0].UserIndex != user1 || !equalCredentials(credentialsOf(ops[0].Credentials), pinOne) {
			t.Errorf("operations %+v, want one NonAccessUserEvent of user 1 with PIN 1", ops)
		}
	})

	t.Run("keeps a DisposableUser enabled when it locks the door", func(t *testing.T) {
		f := setUpScheduledLock(t, withLockState(lockdef.LockStateUnlocked), scheduledUser(lockdef.UserTypeDisposableUser, nil))
		f.must(lockdef.CmdLockDoor, lockdef.LockDoorRequest{PinCode: pinPtr("1234")})
		if got := f.userStatus(); got != lockdef.UserStatusOccupiedEnabled {
			t.Fatalf("user status %d after locking, want OccupiedEnabled", got)
		}
		if err := f.unlockScheduled(); err != nil {
			t.Fatal(err)
		}
		if got := f.userStatus(); got != lockdef.UserStatusOccupiedDisabled {
			t.Errorf("user status %d after unlocking, want OccupiedDisabled", got)
		}
	})

	t.Run("disables a DisposableUser after one use", func(t *testing.T) {
		f := setUpScheduledLock(t, nil, scheduledUser(lockdef.UserTypeDisposableUser, nil))
		if err := f.unlockScheduled(); err != nil {
			t.Fatal(err)
		}
		if got := f.userStatus(); got != lockdef.UserStatusOccupiedDisabled {
			t.Fatalf("user status %d, want OccupiedDisabled", got)
		}
		changes := f.events().userChanges
		if len(changes) != 1 || changes[0].LockDataType != lockdef.LockDataTypeUserIndex ||
			changes[0].DataOperationType != lockdef.DataOperationTypeModify || changes[0].UserIndex != user1 || changes[0].DataIndex != user1 {
			t.Errorf("user changes %+v, want one UserIndex Modify of user 1", changes)
		}
		f.expectScheduledDenial(lockdef.OperationErrorDisabledUserDenied)
	})

	t.Run("reports a ForcedUser operation and raises the silent alarm", func(t *testing.T) {
		f := setUpScheduledLock(t, nil, scheduledUser(lockdef.UserTypeForcedUser, nil))
		if err := f.unlockScheduled(); err != nil {
			t.Fatal(err)
		}
		if got := f.lockStateNow(); got != stateUnlocked {
			t.Errorf("lockState %v, want Unlocked", got)
		}
		ev := f.events()
		if len(ev.operations) != 1 || ev.operations[0].LockOperationType != lockdef.LockOperationTypeForcedUserEvent {
			t.Errorf("operations %+v, want [ForcedUserEvent]", ev.operations)
		}
		if len(ev.alarms) != 1 || ev.alarms[0] != lockdef.AlarmCodeForcedUser {
			t.Errorf("alarms %v, want [ForcedUser]", ev.alarms)
		}
	})

	t.Run("reports the user and credential when refusing a disabled user", func(t *testing.T) {
		disabled := scheduledUser(lockdef.UserTypeUnrestrictedUser, nil)
		disabled.UserStatus = uint8(lockdef.UserStatusOccupiedDisabled)
		f := setUpScheduledLock(t, nil, disabled)
		if err := f.unlockScheduled(); err == nil {
			t.Fatal("the disabled user unlocked")
		}
		errs := f.events().errors
		if len(errs) != 1 || errs[0].OperationError != lockdef.OperationErrorDisabledUserDenied ||
			errs[0].UserIndex != user1 || !equalCredentials(credentialsOf(errs[0].Credentials), pinOne) {
			t.Errorf("error events %+v, want one DisabledUserDenied of user 1 with PIN 1", errs)
		}
	})
}

func operatingModeName(m lockdef.OperatingModeEnum) string {
	for i := range lockdef.OperatingModeEnumDef.Values {
		if v := &lockdef.OperatingModeEnumDef.Values[i]; v.Value == uint64(m) {
			return v.Name
		}
	}
	return "unknown"
}
