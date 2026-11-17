// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package lock_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/cluster/lock"
	lockdef "github.com/SukramJ/go-fabric/cluster/spec/doorlock"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/store"
)

// failingAuth is a memoryAuth whose saves fail while fail is set.
type failingAuth struct {
	*memoryAuth
	mu   sync.Mutex
	fail bool
}

func (a *failingAuth) SaveDoorLockAuth(ctx context.Context, key string, d store.DoorLockAuth) error {
	a.mu.Lock()
	fail := a.fail
	a.mu.Unlock()
	if fail {
		return errors.New("store unavailable")
	}
	return a.memoryAuth.SaveDoorLockAuth(ctx, key, d)
}

// TestUnlockWhoseRecordsAreNotSavedKeepsThePhysicalOutcome
// (R2-P6-err-1504): the hardware unlocked, then the AuthStore refused the
// records. The command answers FAILURE, as matter.js answers a failed
// commit; the physical outcome stands — the DisposableUser that opened the
// door stays disabled, and the auto-relock locks the door again.
func TestUnlockWhoseRecordsAreNotSavedKeepsThePhysicalOutcome(t *testing.T) {
	auth := &failingAuth{memoryAuth: newMemoryAuth()}
	f := setUpScheduledLock(t, func(c *lock.DoorLockConfig) {
		autoRelockTime(5)(c)
		c.Auth, c.AuthKey = auth, "lock"
	}, scheduledUser(lockdef.UserTypeDisposableUser, nil))
	auth.mu.Lock()
	auth.fail = true
	auth.mu.Unlock()

	err := f.unlockScheduled()
	var se *interaction.StatusError
	if !errors.As(err, &se) || se.Status != im.StatusFailure {
		t.Fatalf("unlock with records not saved: %v, want FAILURE", err)
	}
	if got := f.userStatus(); got != lockdef.UserStatusOccupiedDisabled {
		t.Errorf("user status %d: the DisposableUser that opened the door is enabled again", got)
	}
	if got := f.lockStateNow(); got != stateUnlocked {
		t.Fatalf("lockState %v, want Unlocked: the hardware moved", got)
	}
	f.advance(6 * time.Second)
	f.settle()
	if got := f.lockStateNow(); got != stateLocked {
		t.Errorf("lockState %v 6 s after the unlock, want Locked: the auto-relock was not armed", got)
	}
}

// TestRecordsNotSavedAreSavedByTheNextCommit (R2-P6-err-1504, round 2):
// records the store refused stay dirty, so the next commit that saves —
// here a LockDoor that changes no record — writes the disabled
// DisposableUser, and a restart cannot enable it again.
func TestRecordsNotSavedAreSavedByTheNextCommit(t *testing.T) {
	auth := &failingAuth{memoryAuth: newMemoryAuth()}
	f := setUpScheduledLock(t, func(c *lock.DoorLockConfig) {
		c.Auth, c.AuthKey = auth, "lock"
	}, scheduledUser(lockdef.UserTypeDisposableUser, nil))
	auth.mu.Lock()
	auth.fail = true
	auth.mu.Unlock()
	if err := f.unlockScheduled(); err == nil {
		t.Fatal("unlock with records not saved answered SUCCESS")
	}
	auth.mu.Lock()
	auth.fail = false
	auth.mu.Unlock()
	f.must(lockdef.CmdLockDoor, lockdef.LockDoorRequest{})
	saved, ok, _ := auth.LoadDoorLockAuth(context.Background(), "lock")
	if !ok || len(saved.Users) == 0 || lockdef.UserStatusEnum(saved.Users[0].UserStatus) != lockdef.UserStatusOccupiedDisabled {
		t.Errorf("the store holds %+v after the next commit, want the DisposableUser disabled", saved.Users)
	}
}
