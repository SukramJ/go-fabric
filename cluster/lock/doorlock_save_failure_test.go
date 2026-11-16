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
