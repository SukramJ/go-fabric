// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behaviors/door-lock/DoorLockServerTest.ts
// at 862ad75f: describe("ExpiringUser timeout (spec § 5.2.6.18.8)")
// (:1008-1207), on setUpScheduledLock (doorlockserver_harness_test.go).
//
// MockTime keeps two readings: timers run on the monotonic one, and
// stepWallClock moves the wall clock alone. clocktest.Fake has one reading
// — a step (Fake.Set) moves the timers' "now" with it, so a timer whose
// deadline the step passed fires on the next Advance. The two wall-clock
// cases below therefore reach their outcome through that late fire where
// matter.js reaches it through the deadline check of the next access or
// of a timer that fired early; their asserted outcomes are matter.js's.
// Each site is the test's own, so the cases leave the stepped clock as it
// is (matter.js steps back in a finally).

package lock_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/lock"
	lockdef "github.com/SukramJ/go-fabric/cluster/spec/doorlock"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/store"
)

func expiringUserTimeout(minutes uint16) func(*lock.DoorLockConfig) {
	return func(c *lock.DoorLockConfig) { c.ExpiringUserTimeout = &minutes }
}

// startMs is Time.nowMs when a case starts: every site's clock starts at
// fabrictest.DefaultStart.
func startMs(offset time.Duration) *int64 {
	v := fabrictest.DefaultStart.Add(offset).UnixMilli()
	return &v
}

// memoryAuth is an [lock.AuthStore] in memory, matter.js's overridden auth
// store of an external backend (DoorLockServerTest.ts:1171-1179).
type memoryAuth struct {
	mu       sync.Mutex
	settings map[string]string
	saved    map[string]store.DoorLockAuth
}

func newMemoryAuth() *memoryAuth {
	return &memoryAuth{settings: map[string]string{}, saved: map[string]store.DoorLockAuth{}}
}

func (m *memoryAuth) GetSetting(_ context.Context, key string) (value string, ok bool, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	value, ok = m.settings[key]
	return value, ok, nil
}

func (m *memoryAuth) SetSetting(_ context.Context, key, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.settings[key] = value
	return nil
}

func (m *memoryAuth) LoadDoorLockAuth(_ context.Context, key string) (store.DoorLockAuth, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.saved[key]
	return a, ok, nil
}

func (m *memoryAuth) SaveDoorLockAuth(_ context.Context, key string, a store.DoorLockAuth) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.saved[key] = a
	return nil
}

func TestDoorLockServerExpiringUserTimeoutParity(t *testing.T) {
	t.Run("keeps granting access before the timeout elapses", func(t *testing.T) {
		f := setUpScheduledLock(t, expiringUserTimeout(5), scheduledUser(lockdef.UserTypeExpiringUser, nil))
		if err := f.unlockScheduled(); err != nil {
			t.Fatal(err)
		}
		f.advance(60 * time.Second)
		if err := f.unlockScheduled(); err != nil {
			t.Fatal(err)
		}
		if got := f.userStatus(); got != lockdef.UserStatusOccupiedEnabled {
			t.Errorf("user status %d, want OccupiedEnabled", got)
		}
	})

	t.Run("disables the user when the timeout elapses after first use, without a further access attempt", func(t *testing.T) {
		f := setUpScheduledLock(t, expiringUserTimeout(1), scheduledUser(lockdef.UserTypeExpiringUser, nil))
		if err := f.unlockScheduled(); err != nil {
			t.Fatal(err)
		}
		if got := f.userStatus(); got != lockdef.UserStatusOccupiedEnabled {
			t.Fatalf("user status %d after the first use, want OccupiedEnabled", got)
		}
		f.markEvents()
		f.advance(70 * time.Second)
		f.settle()
		if got := f.userStatus(); got != lockdef.UserStatusOccupiedDisabled {
			t.Fatalf("user status %d, want OccupiedDisabled", got)
		}
		changes := f.events().userChanges
		want := lockdef.LockUserChangeEvent{
			LockDataType: lockdef.LockDataTypeUserIndex, DataOperationType: lockdef.DataOperationTypeModify,
			OperationSource: lockdef.OperationSourceUnspecified, UserIndex: user1,
			FabricIndex: nullFabric(), SourceNode: nullNode(), DataIndex: user1,
		}
		if len(changes) != 1 || changes[0] != want {
			t.Errorf("user changes %+v, want [%+v]", changes, want)
		}
		f.expectScheduledDenial(lockdef.OperationErrorDisabledUserDenied)
	})

	t.Run("does not arm the timeout before the first successful use", func(t *testing.T) {
		f := setUpScheduledLock(t, expiringUserTimeout(1), scheduledUser(lockdef.UserTypeExpiringUser, nil))
		f.advance(70 * time.Second)
		f.settle()
		if got := f.userStatus(); got != lockdef.UserStatusOccupiedEnabled {
			t.Fatalf("user status %d, want OccupiedEnabled", got)
		}
		if err := f.unlockScheduled(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("keeps the deadline when a modification changes neither status nor type", func(t *testing.T) {
		f := setUpScheduledLock(t, expiringUserTimeout(1), scheduledUser(lockdef.UserTypeExpiringUser, nil))
		if err := f.unlockScheduled(); err != nil {
			t.Fatal(err)
		}
		f.advance(30 * time.Second)
		renamed := "renamed"
		f.modifyUser(&renamed, nil)
		f.advance(40 * time.Second)
		f.settle()
		if got := f.userStatus(); got != lockdef.UserStatusOccupiedDisabled {
			t.Errorf("user status %d, want OccupiedDisabled", got)
		}
	})

	t.Run("starts a new cycle when an expired user is enabled again", func(t *testing.T) {
		f := setUpScheduledLock(t, expiringUserTimeout(1), scheduledUser(lockdef.UserTypeExpiringUser, nil))
		if err := f.unlockScheduled(); err != nil {
			t.Fatal(err)
		}
		f.advance(70 * time.Second)
		f.settle()
		if got := f.userStatus(); got != lockdef.UserStatusOccupiedDisabled {
			t.Fatalf("user status %d, want OccupiedDisabled", got)
		}
		enabled := lockdef.UserStatusOccupiedEnabled
		f.modifyUser(nil, &enabled)
		if err := f.unlockScheduled(); err != nil {
			t.Fatal(err)
		}
		f.advance(30 * time.Second)
		f.settle()
		if got := f.userStatus(); got != lockdef.UserStatusOccupiedEnabled {
			t.Fatalf("user status %d 30 s into the new cycle, want OccupiedEnabled", got)
		}
		f.advance(40 * time.Second)
		f.settle()
		if got := f.userStatus(); got != lockdef.UserStatusOccupiedDisabled {
			t.Errorf("user status %d after the new cycle, want OccupiedDisabled", got)
		}
	})

	t.Run("denies and disables once the wall clock passes the deadline, before the timer fires", func(t *testing.T) {
		f := setUpScheduledLock(t, expiringUserTimeout(1), scheduledUser(lockdef.UserTypeExpiringUser, nil))
		if err := f.unlockScheduled(); err != nil {
			t.Fatal(err)
		}
		f.markEvents()
		f.stepWallClock(2 * time.Minute)

		if err := f.unlockScheduled(); err == nil {
			t.Fatal("unlock past the deadline was granted")
		}
		f.advance(0)
		f.settle()
		if got := f.userStatus(); got != lockdef.UserStatusOccupiedDisabled {
			t.Fatalf("user status %d, want OccupiedDisabled", got)
		}
		ev := f.events()
		if len(ev.errors) != 1 || ev.errors[0].OperationError != lockdef.OperationErrorDisabledUserDenied || ev.errors[0].UserIndex != user1 {
			t.Errorf("error events %+v, want one DisabledUserDenied for user 1", ev.errors)
		}
		if len(ev.userChanges) != 1 || ev.userChanges[0].UserIndex != user1 || ev.userChanges[0].OperationSource != lockdef.OperationSourceUnspecified {
			t.Errorf("user changes %+v, want one for user 1 from source Unspecified", ev.userChanges)
		}
	})

	t.Run("re-arms for the remaining time when the timer fires early after the wall clock stepped back", func(t *testing.T) {
		f := setUpScheduledLock(t, expiringUserTimeout(1), scheduledUser(lockdef.UserTypeExpiringUser, nil))
		if err := f.unlockScheduled(); err != nil {
			t.Fatal(err)
		}
		f.stepWallClock(-30 * time.Second)
		f.advance(65 * time.Second)
		f.settle()
		if got := f.userStatus(); got != lockdef.UserStatusOccupiedEnabled {
			t.Fatalf("user status %d 65 s after the step back, want OccupiedEnabled", got)
		}
		f.advance(30 * time.Second)
		f.settle()
		if got := f.userStatus(); got != lockdef.UserStatusOccupiedDisabled {
			t.Errorf("user status %d, want OccupiedDisabled", got)
		}
	})

	t.Run("denies an ExpiringUser while ExpiringUserTimeout is not set", func(t *testing.T) {
		f := setUpScheduledLock(t, nil, scheduledUser(lockdef.UserTypeExpiringUser, nil))
		f.expectScheduledDenial(lockdef.OperationErrorRestricted)
	})

	t.Run("disables at startup a user whose stored deadline has passed", func(t *testing.T) {
		f := setUpScheduledLock(t, expiringUserTimeout(1), scheduledUser(lockdef.UserTypeExpiringUser, startMs(-time.Minute)))
		f.advance(0)
		f.settle()
		if got := f.userStatus(); got != lockdef.UserStatusOccupiedDisabled {
			t.Errorf("user status %d, want OccupiedDisabled", got)
		}
	})

	t.Run("starts with a deadline beyond the timer range and still disables once it passes", func(t *testing.T) {
		f := setUpScheduledLock(t, expiringUserTimeout(1), scheduledUser(lockdef.UserTypeExpiringUser, startMs(60*24*time.Hour)))
		if got := f.userStatus(); got != lockdef.UserStatusOccupiedEnabled {
			t.Fatalf("user status %d at the start, want OccupiedEnabled", got)
		}
		f.stepWallClock(60 * 24 * time.Hour)
		f.advance(24 * time.Hour)
		f.settle()
		if got := f.userStatus(); got != lockdef.UserStatusOccupiedDisabled {
			t.Errorf("user status %d, want OccupiedDisabled", got)
		}
	})

	t.Run("restores deadlines at startup from an overridden auth store", func(t *testing.T) {
		// The external store holds the user, the lock's own state none
		// (users: [], :1184); the Go lock reads its records from its
		// AuthStore and writes the disabled user back to it.
		external := newMemoryAuth()
		external.saved["lock"] = store.DoorLockAuth{
			Users: []store.DoorLockUser{scheduledUser(lockdef.UserTypeExpiringUser, startMs(-time.Minute))},
		}
		f := createLock(t, func(c *lock.DoorLockConfig) {
			c.Initial = store.DoorLockAuth{}
			c.ExpiringUserTimeout = ptr[uint16](1)
			c.Auth, c.AuthKey = external, "lock"
		})
		f.advance(0)
		f.settle()
		external.mu.Lock()
		users := external.saved["lock"].Users
		external.mu.Unlock()
		if len(users) != 1 || lockdef.UserStatusEnum(users[0].UserStatus) != lockdef.UserStatusOccupiedDisabled {
			t.Errorf("external users %+v, want the user OccupiedDisabled", users)
		}
	})

	t.Run("disables a user when a deadline that was still pending at startup passes", func(t *testing.T) {
		f := setUpScheduledLock(t, expiringUserTimeout(1), scheduledUser(lockdef.UserTypeExpiringUser, startMs(time.Hour)))
		if got := f.userStatus(); got != lockdef.UserStatusOccupiedEnabled {
			t.Fatalf("user status %d at the start, want OccupiedEnabled", got)
		}
		f.advance(3_610_000 * time.Millisecond)
		f.settle()
		if got := f.userStatus(); got != lockdef.UserStatusOccupiedDisabled {
			t.Errorf("user status %d, want OccupiedDisabled", got)
		}
	})
}

func ptr[T any](v T) *T { return &v }
