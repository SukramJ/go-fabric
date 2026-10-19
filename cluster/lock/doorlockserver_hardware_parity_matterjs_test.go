// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behaviors/door-lock/DoorLockServerTest.ts
// at 862ad75f: describe("lock hardware") (:1474-1683) — the hardware
// handler ([lock.LockOperationHandler], matter.js handleLockOperation):
// when it moves, what it is told, how a failure is reported and that
// operations never overlap, on setUpScheduledLock with the test hardware
// (doorlockserver_harness_test.go).

package lock_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/lock"
	"github.com/SukramJ/go-fabric/cluster/spec"
	lockdef "github.com/SukramJ/go-fabric/cluster/spec/doorlock"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/store"
)

// call is the part of a hardware call a case compares.
type call struct {
	actuation lock.Actuation
	source    lockdef.OperationSourceEnum
	userIndex *uint16
	lockState any
}

func (h *testHardware) summary() []call {
	calls := h.snapshot()
	out := make([]call, 0, len(calls))
	for _, c := range calls {
		out = append(out, call{actuation: c.op.Actuation, source: c.op.Source, userIndex: c.op.UserIndex, lockState: c.lockState})
	}
	return out
}

func sameCall(a, b call) bool {
	if a.actuation != b.actuation || a.source != b.source || a.lockState != b.lockState {
		return false
	}
	if (a.userIndex == nil) != (b.userIndex == nil) {
		return false
	}
	return a.userIndex == nil || *a.userIndex == *b.userIndex
}

func actuationsOf(calls []call) []lock.Actuation {
	out := make([]lock.Actuation, 0, len(calls))
	for _, c := range calls {
		out = append(out, c.actuation)
	}
	return out
}

func equalActuations(a []lock.Actuation, b ...lock.Actuation) bool {
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

// async runs an invocation in its own goroutine.
func (f *lockFixture) async(cmd uint32, req spec.Encodable) <-chan error {
	done := make(chan error, 1)
	go func() {
		_, err := f.invoke(cmd, req)
		done <- err
	}()
	return done
}

func TestDoorLockServerLockHardwareParity(t *testing.T) {
	remote, auto := lockdef.OperationSourceRemote, lockdef.OperationSourceAuto
	one := uint16(1)

	t.Run("moves the hardware after authorization and before the lock state changes", func(t *testing.T) {
		f := setUpScheduledLock(t, withLockState(lockdef.LockStateLocked), scheduledUser(lockdef.UserTypeUnrestrictedUser, nil))
		if err := f.unlock("9999"); err == nil {
			t.Fatal("a wrong PIN unlocked")
		}
		if err := f.unlockScheduled(); err != nil {
			t.Fatal(err)
		}
		f.must(lockdef.CmdLockDoor, lockdef.LockDoorRequest{})
		f.must(lockdef.CmdUnboltDoor, lockdef.UnboltDoorRequest{})

		want := []call{
			{lock.ActuationUnlock, remote, &one, stateLocked},
			{lock.ActuationLock, remote, nil, stateUnlocked},
			{lock.ActuationUnbolt, remote, nil, stateLocked},
		}
		got := f.hw.summary()
		if len(got) != len(want) {
			t.Fatalf("hardware calls %+v, want %+v", got, want)
		}
		for i := range want {
			if !sameCall(got[i], want[i]) {
				t.Errorf("hardware call %d = %+v, want %+v", i, got[i], want[i])
			}
		}
		ops := f.events().operations
		if len(ops) != 3 {
			t.Fatalf("operations %+v, want three", ops)
		}
		for _, op := range ops {
			if op.FabricIndex.Null || op.FabricIndex.Value != pairFabric || op.SourceNode.Null {
				t.Errorf("operation %+v, want fabric 1 and a source node", op)
			}
		}
	})

	t.Run("refuses a lock command invoked locally before the hardware moves", func(t *testing.T) {
		f := setUpScheduledLock(t, withLockState(lockdef.LockStateLocked), scheduledUser(lockdef.UserTypeUnrestrictedUser, nil))
		// matter.js's local agent carries no fabric (lock.act, :1506); a Go
		// request without an Interaction Model context has none either.
		_, err := f.srv.MatterInvoke(context.Background(), lockdef.CmdUnlockDoor, lockdef.UnlockDoorRequest{})
		var sc im.StatusCodeError
		if !errors.As(err, &sc) || sc.MatterStatusCode() != im.StatusUnsupportedAccess {
			t.Errorf("local unlock: %v, want UNSUPPORTED_ACCESS", err)
		}
		if calls := f.hw.snapshot(); len(calls) != 0 {
			t.Errorf("hardware calls %+v, want none", calls)
		}
		if got := f.lockStateNow(); got != stateLocked {
			t.Errorf("lockState %v, want Locked", got)
		}
	})

	t.Run("starts the attempt count again only after the hardware succeeds", func(t *testing.T) {
		f := setUpScheduledLock(t, withLockState(lockdef.LockStateLocked), scheduledUser(lockdef.UserTypeUnrestrictedUser, nil))
		for range 2 {
			if err := f.unlock("9999"); err == nil {
				t.Fatal("a wrong PIN unlocked")
			}
		}
		f.hw.fail(&lock.LockOperationFailedError{Reason: lockdef.OperationErrorUnspecified})
		if err := f.unlockScheduled(); err == nil {
			t.Fatal("the failing hardware unlocked")
		}
		if err := f.unlock("9999"); err == nil {
			t.Fatal("a wrong PIN unlocked")
		}
		if got := f.alarmsSoFar(); !equalAlarms(got, []lockdef.AlarmCodeEnum{lockdef.AlarmCodeWrongCodeEntryLimit}) {
			t.Errorf("alarms %v, want [WrongCodeEntryLimit]", got)
		}
	})

	t.Run("keeps the attempt count through an operation without PIN", func(t *testing.T) {
		f := setUpScheduledLock(t, withLockState(lockdef.LockStateLocked), scheduledUser(lockdef.UserTypeUnrestrictedUser, nil))
		for range 2 {
			if err := f.unlock("9999"); err == nil {
				t.Fatal("a wrong PIN unlocked")
			}
		}
		if err := f.unlock(""); err != nil {
			t.Fatalf("unlock without PIN: %v", err)
		}
		if err := f.unlock("9999"); err == nil {
			t.Fatal("a wrong PIN unlocked")
		}
		if got := f.alarmsSoFar(); !equalAlarms(got, []lockdef.AlarmCodeEnum{lockdef.AlarmCodeWrongCodeEntryLimit}) {
			t.Errorf("alarms %v, want [WrongCodeEntryLimit]", got)
		}
	})

	t.Run("refuses a PIN that belongs to no user as a wrong code", func(t *testing.T) {
		f := setUpScheduledLock(t, func(c *lock.DoorLockConfig) {
			c.Initial.Credentials = []store.DoorLockCredential{{
				Type: uint8(lockdef.CredentialTypePin), Index: 2, Data: pin("5555"),
				CreatorFabricIndex: pairFabric, LastModifiedFabricIndex: pairFabric,
			}}
		}, scheduledUser(lockdef.UserTypeUnrestrictedUser, nil))
		if err := f.unlock("5555"); err == nil {
			t.Fatal("a PIN of no user unlocked")
		}
		if got := f.lockStateNow(); got != stateLocked {
			t.Errorf("lockState %v, want Locked", got)
		}
		if calls := f.hw.snapshot(); len(calls) != 0 {
			t.Errorf("hardware calls %+v, want none", calls)
		}
		if got := f.events().operationErrors(); len(got) != 1 || got[0] != lockdef.OperationErrorInvalidCredential {
			t.Errorf("operation errors %v, want [InvalidCredential]", got)
		}
	})

	t.Run("runs one lock operation at a time", func(t *testing.T) {
		f := setUpScheduledLock(t, withLockState(lockdef.LockStateLocked), scheduledUser(lockdef.UserTypeUnrestrictedUser, nil))
		release := f.hw.hold()
		defer release()
		called := f.hw.nextCall()
		unlock := f.async(lockdef.CmdUnlockDoor, lockdef.UnlockDoorRequest{})
		<-called
		// matter.js sends both at once and the unlock reaches the hardware
		// first; the port sends the lock once the unlock holds the hardware,
		// which keeps that order.
		lockDone := f.async(lockdef.CmdLockDoor, lockdef.LockDoorRequest{})
		f.advance(time.Second)
		f.settle()
		if got := actuationsOf(f.hw.summary()); !equalActuations(got, lock.ActuationUnlock) {
			t.Fatalf("actuations while the unlock is held %v, want [unlock]", got)
		}
		release()
		if err := <-unlock; err != nil {
			t.Errorf("unlock: %v", err)
		}
		if err := <-lockDone; err != nil {
			t.Errorf("lock: %v", err)
		}
		got := f.hw.summary()
		if len(got) != 2 || got[0].actuation != lock.ActuationUnlock || got[0].lockState != stateLocked ||
			got[1].actuation != lock.ActuationLock || got[1].lockState != stateUnlocked {
			t.Errorf("hardware calls %+v, want unlock while Locked, then lock while Unlocked", got)
		}
		if got := f.lockStateNow(); got != stateLocked {
			t.Errorf("lockState %v, want Locked", got)
		}
	})

	t.Run("does not relock for a timer an unlock replaced while the relock waited", func(t *testing.T) {
		f := setUpScheduledLock(t, func(c *lock.DoorLockConfig) {
			withLockState(lockdef.LockStateLocked)(c)
			autoRelockTime(5)(c)
		}, scheduledUser(lockdef.UserTypeUnrestrictedUser, nil))
		f.must(lockdef.CmdUnlockDoor, lockdef.UnlockDoorRequest{})

		release := f.hw.hold()
		defer release()
		called := f.hw.nextCall()
		unlock := f.async(lockdef.CmdUnlockDoor, lockdef.UnlockDoorRequest{})
		<-called
		f.advance(6 * time.Second)

		release()
		if err := <-unlock; err != nil {
			t.Errorf("unlock: %v", err)
		}
		f.settle()
		if got := actuationsOf(f.hw.summary()); !equalActuations(got, lock.ActuationUnlock, lock.ActuationUnlock) {
			t.Errorf("actuations %v, want [unlock unlock]", got)
		}
		if got := f.lockStateNow(); got != stateUnlocked {
			t.Errorf("lockState %v, want Unlocked", got)
		}
	})

	t.Run("fails the operation when the hardware fails", func(t *testing.T) {
		f := setUpScheduledLock(t, withLockState(lockdef.LockStateLocked), scheduledUser(lockdef.UserTypeUnrestrictedUser, nil))
		f.hw.fail(errors.New("motor jammed"))
		if err := f.unlockScheduled(); err == nil {
			t.Fatal("the failing hardware unlocked")
		}
		if got := f.lockStateNow(); got != stateLocked {
			t.Errorf("lockState %v, want Locked", got)
		}
		ev := f.events()
		if len(ev.operations) != 0 {
			t.Errorf("operations %+v, want none", ev.operations)
		}
		if len(ev.errors) != 1 || ev.errors[0].LockOperationType != lockdef.LockOperationTypeUnlock ||
			ev.errors[0].OperationError != lockdef.OperationErrorUnspecified || ev.errors[0].UserIndex != user1 {
			t.Errorf("error events %+v, want one Unlock Unspecified of user 1", ev.errors)
		}
	})

	t.Run("reports the reason the hardware gives for a failure", func(t *testing.T) {
		f := setUpScheduledLock(t, withLockState(lockdef.LockStateLocked), scheduledUser(lockdef.UserTypeUnrestrictedUser, nil))
		f.hw.fail(&lock.LockOperationFailedError{Reason: lockdef.OperationErrorInsufficientBattery})
		if err := f.unlockScheduled(); err == nil {
			t.Fatal("the failing hardware unlocked")
		}
		if got := f.lockStateNow(); got != stateLocked {
			t.Errorf("lockState %v, want Locked", got)
		}
		if got := f.events().operationErrors(); len(got) != 1 || got[0] != lockdef.OperationErrorInsufficientBattery {
			t.Errorf("operation errors %v, want [InsufficientBattery]", got)
		}
	})

	t.Run("relocks through the hardware and keeps the state when it fails", func(t *testing.T) {
		f := setUpScheduledLock(t, func(c *lock.DoorLockConfig) {
			withLockState(lockdef.LockStateLocked)(c)
			autoRelockTime(5)(c)
		}, scheduledUser(lockdef.UserTypeUnrestrictedUser, nil))
		if err := f.unlockScheduled(); err != nil {
			t.Fatal(err)
		}
		f.hw.fail(&lock.LockOperationFailedError{Reason: lockdef.OperationErrorInsufficientBattery})
		f.advance(5 * time.Second)
		f.settle()

		got := f.hw.summary()
		if len(got) != 2 || got[0].actuation != lock.ActuationUnlock || got[0].source != remote ||
			got[1].actuation != lock.ActuationLock || got[1].source != auto {
			t.Errorf("hardware calls %+v, want a remote unlock and an auto lock", got)
		}
		if got := f.lockStateNow(); got != stateUnlocked {
			t.Errorf("lockState %v, want Unlocked", got)
		}
		errs := f.allEvents().errors
		if len(errs) != 1 || errs[0].LockOperationType != lockdef.LockOperationTypeLock ||
			errs[0].OperationSource != auto || errs[0].OperationError != lockdef.OperationErrorInsufficientBattery {
			t.Errorf("error events %+v, want one Lock from Auto with InsufficientBattery", errs)
		}

		f.hw.fail(nil)
		f.must(lockdef.CmdUnlockDoor, lockdef.UnlockDoorRequest{})
		f.advance(5 * time.Second)
		f.settle()
		if got := f.lockStateNow(); got != stateLocked {
			t.Errorf("lockState %v after the next relock, want Locked", got)
		}
	})
}
