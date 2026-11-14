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
// are taken at the same moment from different goroutines (the reporter
// or the ACK pump, and the inbound acknowledgement). Either order is a
// count the two steps make one after the other — the delivered update
// last leaves nothing counted, the failed one last leaves one — and no
// other count is ever left.
func TestFailedUpdateCountIsOneStep(t *testing.T) {
	t.Parallel()
	const (
		subID     = uint32(77)
		sessionID = uint16(5)
		rounds    = 2000
	)
	errSend := errors.New("send failed")
	for round := range rounds {
		b := &Bridge{}
		b.routing.subTargets.Store(subID, subTarget{})
		b.subSendErrorCount.Store(subID, 1)
		counter := uint32(round + 1)
		b.reportCounterOwner.Store(reportCounterKey(sessionID, counter), &owedReport{subID: subID})

		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Go(func() {
			<-start
			b.noteSubscriptionSendError(subID, 0, "report", errSend)
		})
		wg.Go(func() {
			<-start
			b.releaseReportCounter(sessionID, counter)
		})
		close(start)
		wg.Wait()

		raw, counted := b.subSendErrorCount.Load(subID)
		if counted && raw != 1 {
			t.Fatalf("round %d: %v failed updates counted after a failed and a delivered update, want none or 1", round, raw)
		}
	}
}

// TestReportNumberDoesNotOutliveItsSubscription: no report number and no
// count of failed updates is kept for a subscription whose routing is
// released — not for a report begun after the release, whose send then
// fails, and not for a failure taken at the same moment as the one that
// reaches the cap and reaps the subscription.
func TestReportNumberDoesNotOutliveItsSubscription(t *testing.T) {
	t.Parallel()
	const subID = uint32(91)
	errSend := errors.New("send failed")
	kept := func(b *Bridge) (number, count bool) {
		b.subUpdateMu.Lock()
		_, number = b.subUpdateSeq[subID]
		b.subUpdateMu.Unlock()
		_, count = b.subSendErrorCount.Load(subID)
		return number, count
	}

	t.Run("a report begun after the release", func(t *testing.T) {
		t.Parallel()
		b := &Bridge{}
		b.routing.subTargets.Store(subID, subTarget{})
		b.beginUpdate(subID)
		b.releaseSubscriptionRouting(subID)
		seq := b.beginUpdate(subID)
		b.noteSubscriptionSendError(subID, seq, "report", errSend)
		if number, count := kept(b); number || count {
			t.Errorf("after the release: report number kept %v, failure count kept %v; want neither", number, count)
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
					b.noteSubscriptionSendError(subID, 0, "report", errSend)
				})
			}
			close(start)
			wg.Wait()
			if _, live := b.routing.subTargets.Load(subID); live {
				t.Fatalf("round %d: the subscription was not reaped", round)
			}
			if number, count := kept(b); number || count {
				t.Fatalf("round %d: after the reap: report number kept %v, failure count kept %v; want neither", round, number, count)
			}
		}
	})
}
