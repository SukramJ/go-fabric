// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"sync"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/transport/mrp"
)

// doneSpyContext reports the first call of Done: the flush calls it only
// once it waits for the report in flight (subscription.Manager.claimReport),
// after it noted the subscription's failed updates.
type doneSpyContext struct {
	context.Context
	once   sync.Once
	called chan struct{}
}

func (c *doneSpyContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.called) })
	return c.Context.Done()
}

// TestFlushAfterAnAbortedOngoingReportSends (review II, R2-P7-req-06,
// decision 274): an ongoing report aborted by the engine's stop owes its
// data again without counting a failed update, so the flush that waited
// for it — the shutdown flush runs after the engine stopped — sends what
// the subscription owes instead of skipping it as after a failed update.
func TestFlushAfterAnAbortedOngoingReportSends(t *testing.T) {
	t.Parallel()
	r := newUndeliveredReportRig(t, 0x1701)
	// The engine starts the ongoing report; it waits for the answer the
	// silent peer never sends.
	r.dev.subs.OnAttributeChanged(undeliveredReportPath)
	var reportExch uint16
	waitFor(t, "the ongoing report waiting for its StatusResponse", func() bool {
		found := false
		r.dev.br.routing.statusResponseWaits.Range(func(k, _ any) bool {
			if ek, ok := k.(mrp.ExchangeKey); ok && ek.SessionID == r.reportSID && ek.Initiator {
				reportExch, found = ek.ExchangeID, true
			}
			return !found
		})
		return found
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	spy := &doneSpyContext{Context: ctx, called: make(chan struct{})}
	flushed := make(chan struct{})
	go func() {
		defer close(flushed)
		r.dev.subs.FlushFabric(spy, resumeFabricIndex, 0)
	}()
	select {
	case <-spy.called:
	case <-flushed:
		t.Fatal("the flush returned without waiting for the report in flight")
	}
	// The engine's stop aborts the report's wait.
	r.dev.subs.Stop()

	var flushExch uint16
	waitFor(t, "the flush's report, or the flush's end", func() bool {
		select {
		case <-flushed:
			return true
		default:
		}
		found := false
		r.dev.br.routing.statusResponseWaits.Range(func(k, _ any) bool {
			if ek, ok := k.(mrp.ExchangeKey); ok && ek.SessionID == r.reportSID && ek.Initiator && ek.ExchangeID != reportExch {
				flushExch, found = ek.ExchangeID, true
			}
			return !found
		})
		return found
	})
	if flushExch == 0 {
		t.Fatal("the flush that waited for an aborted report sent nothing, as after a failed update")
	}
	if !r.dev.br.signalStatusResponseRX(r.reportSID, flushExch, true, im.StatusSuccess) {
		t.Fatal("the StatusResponse found no waiting flush report")
	}
	<-flushed
	if _, counted := r.dev.br.subSendErrorCount.Load(r.sub.ID); counted {
		t.Error("the aborted report was counted as a failed update")
	}
}
