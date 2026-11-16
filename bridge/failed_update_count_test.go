// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"errors"
	"sync"
	"testing"
)

// TestFailedUpdateCountIsOneStep: the count of a subscription's failed
// updates moves in whole steps when a failed update and a delivered one
// are taken at the same moment from different goroutines (the ACK pump,
// and the reporter taking the peer's Success StatusResponse). Either order
// is a count the two steps make one after the other — the delivered update
// last leaves nothing counted, the failed one last leaves one — and no
// other count is ever left.
func TestFailedUpdateCountIsOneStep(t *testing.T) {
	t.Parallel()
	const (
		subID  = uint32(77)
		rounds = 2000
	)
	errSend := errors.New("send failed")
	for round := range rounds {
		b := &Bridge{}
		b.routing.subTargets.Store(subID, subTarget{})
		b.subSendErrorCount.Store(subID, 1)

		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Go(func() {
			<-start
			b.noteSubscriptionSendError(subID, "report", errSend)
		})
		wg.Go(func() {
			<-start
			b.noteUpdateDelivered(subID)
		})
		close(start)
		wg.Wait()

		raw, counted := b.subSendErrorCount.Load(subID)
		if counted && raw != 1 {
			t.Fatalf("round %d: %v failed updates counted after a failed and a delivered update, want none or 1", round, raw)
		}
	}
}

// TestFailedUpdateCountDoesNotOutliveItsSubscription: no count of failed
// updates is kept for a subscription whose routing is released — not for
// a report whose send fails after the release, and not for a failure taken
// at the same moment as the one that reaches the cap and reaps the
// subscription.
func TestFailedUpdateCountDoesNotOutliveItsSubscription(t *testing.T) {
	t.Parallel()
	const subID = uint32(91)
	errSend := errors.New("send failed")
	kept := func(b *Bridge) bool {
		_, count := b.subSendErrorCount.Load(subID)
		return count
	}

	t.Run("a failure after the release", func(t *testing.T) {
		t.Parallel()
		b := &Bridge{}
		b.routing.subTargets.Store(subID, subTarget{})
		b.releaseSubscriptionRouting(subID)
		b.noteSubscriptionSendError(subID, "report", errSend)
		if kept(b) {
			t.Error("after the release: failure count kept")
		}
	})

	t.Run("a failure beside the one that reaps", func(t *testing.T) {
		t.Parallel()
		for round := range 2000 {
			b := &Bridge{}
			b.routing.subTargets.Store(subID, subTarget{})
			b.subSendErrorCount.Store(subID, sendErrorRetryLimit)
			start := make(chan struct{})
			var wg sync.WaitGroup
			for range 2 {
				wg.Go(func() {
					<-start
					b.noteSubscriptionSendError(subID, "report", errSend)
				})
			}
			close(start)
			wg.Wait()
			if _, live := b.routing.subTargets.Load(subID); live {
				t.Fatalf("round %d: the subscription was not reaped", round)
			}
			if kept(b) {
				t.Fatalf("round %d: after the reap: failure count kept", round)
			}
		}
	})
}
