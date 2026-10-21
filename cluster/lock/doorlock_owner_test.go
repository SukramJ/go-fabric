// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package lock_test

import (
	"testing"
	"time"

	lockdef "github.com/SukramJ/go-fabric/cluster/spec/doorlock"
)

// TestDoorLockQuiesceWaitsForTheTimerAction pins the server as the owner
// of its timer actions (G1-14, ADR 0026 P5): the auto-relock runs on a
// goroutine of its own because it waits for the operation lock, and
// MatterQuiesce returns only after a running one returned.
func TestDoorLockQuiesceWaitsForTheTimerAction(t *testing.T) {
	f := setUpScheduledLock(t, autoRelockTime(5), scheduledUser(lockdef.UserTypeUnrestrictedUser, nil))
	if err := f.unlockScheduled(); err != nil {
		t.Fatal(err)
	}
	release := f.hw.hold()
	defer release()
	called := f.hw.nextCall()
	f.advance(5 * time.Second) // the auto-relock starts and holds at the hardware
	<-called

	quiesced := make(chan struct{})
	go func() {
		f.srv.MatterQuiesce()
		close(quiesced)
	}()
	// A quiesce that does not wait returns at once; one that waits never
	// returns before the release, however long this wait is.
	select {
	case <-quiesced:
		t.Fatal("MatterQuiesce returned while the auto-relock still moved the hardware")
	case <-time.After(50 * time.Millisecond):
	}
	release()
	<-quiesced
	if got := f.lockStateNow(); got != stateLocked {
		t.Fatalf("lockState %v after the relock, want Locked", got)
	}
}
