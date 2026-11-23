// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package lock_test

import (
	"context"
	"testing"

	"thingfabric.dev/go-fabric/cluster/lock"
	"thingfabric.dev/go-fabric/cluster/spec"
	lockdef "thingfabric.dev/go-fabric/cluster/spec/doorlock"
)

// TestRecordsNotSavedSurviveTheNextTransaction (R2-C2-store-1502): records
// an unlock left unsaved stay pending across a transaction that does not
// complete — a command its own checks refuse, or one whose own save the
// store refuses — so the disabled DisposableUser still reaches the store
// and a restart cannot enable it again.
func TestRecordsNotSavedSurviveTheNextTransaction(t *testing.T) {
	refusedClear := func(f *lockFixture) {
		f.refused(lockdef.CmdClearCredential, lockdef.ClearCredentialRequest{
			Credential: spec.ValueOf(lockdef.CredentialStruct{CredentialType: lockdef.CredentialTypePin, CredentialIndex: 5}),
		})
	}
	addUser := func(f *lockFixture) {
		_, _ = f.invoke(lockdef.CmdSetUser, lockdef.SetUserRequest{
			OperationType:  lockdef.DataOperationTypeAdd,
			UserIndex:      2,
			UserName:       spec.NullOf[string](),
			UserUniqueId:   spec.NullOf[uint32](),
			UserStatus:     spec.NullOf[lockdef.UserStatusEnum](),
			UserType:       spec.NullOf[lockdef.UserTypeEnum](),
			CredentialRule: spec.NullOf[lockdef.CredentialRuleEnum](),
		})
	}
	for _, tc := range []struct {
		name string
		// storeFails is whether the store still refuses during step.
		storeFails bool
		step       func(*lockFixture)
	}{
		{"a refused command", false, refusedClear},
		{"a refused command while the store still fails", true, refusedClear},
		{"a command whose own save fails", true, addUser},
	} {
		t.Run(tc.name, func(t *testing.T) {
			auth := &failingAuth{memoryAuth: newMemoryAuth()}
			f := setUpScheduledLock(t, func(c *lock.DoorLockConfig) {
				c.Auth, c.AuthKey = auth, "lock"
			}, scheduledUser(lockdef.UserTypeDisposableUser, nil))
			setFail := func(v bool) {
				auth.mu.Lock()
				auth.fail = v
				auth.mu.Unlock()
			}
			setFail(true)
			if err := f.unlockScheduled(); err == nil {
				t.Fatal("unlock with records not saved answered SUCCESS")
			}
			setFail(tc.storeFails)
			tc.step(f)
			setFail(false)
			f.must(lockdef.CmdLockDoor, lockdef.LockDoorRequest{})
			saved, ok, _ := auth.LoadDoorLockAuth(context.Background(), "lock")
			if !ok || len(saved.Users) == 0 || lockdef.UserStatusEnum(saved.Users[0].UserStatus) != lockdef.UserStatusOccupiedDisabled {
				t.Errorf("the store holds ok=%v users=%+v, want the DisposableUser disabled", ok, saved.Users)
			}
		})
	}
}
