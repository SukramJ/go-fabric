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
