// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

// Ports by meaning of matter.js
// packages/node/test/node/ServerSubscriptionTest.ts at db91886c:
// "terminates when the peer refuses a report sent during a shutdown close"
// (:279-305). matter.js marks the subscription terminated when the peer
// answers a report with an error status, "even during a shutdown close"
// (packages/node/src/node/server/ServerSubscription.ts:976-989 at
// b25ed960), and as the closing subscription awaits its flush before it
// emits cancelled (:861-864, :872), the record of a subscription whose
// flush report was refused is dropped
// (packages/node/src/behavior/system/subscriptions/SubscriptionsServer.ts
// :168-187). A flush report left unanswered for 500 ms does not terminate
// (ServerSubscription.ts:980, :564-567). The matter.js case refuses a
// report in flight and asserts isTerminated; here the refused report is
// the flush's own, and the observable is the persisted record.

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/transport/mrp"
)

func TestServerSubscriptionShutdownFlushRefused(t *testing.T) {
	t.Parallel()
	// flushing starts the shutdown flush of the rig's subscription, its
	// record persisted, and returns the flush report's exchange and the
	// channel that closes when the flush returned.
	flushing := func(t *testing.T, base uint16) (*undeliveredReportRig, *memSubscriptionStore, uint16, <-chan struct{}) {
		t.Helper()
		r := newUndeliveredReportRig(t, base)
		r.dev.br.resumption.mu.Lock()
		st, ok := r.dev.br.resumption.storeLocked().(*memSubscriptionStore)
		r.dev.br.resumption.mu.Unlock()
		if !ok {
			t.Fatal("no in-memory subscription store")
		}
		if err := st.SaveServerSubscription(context.Background(), r.sub.ID, resumeFabricIndex, resumeCtrlNode, []byte{1}); err != nil {
			t.Fatal(err)
		}
		// The shutdown flush runs after the engine stopped, so no ongoing
		// report takes the change first.
		r.dev.subs.Stop()
		r.dev.subs.OnAttributeChanged(undeliveredReportPath)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		t.Cleanup(cancel)
		flushed := make(chan struct{})
		go func() {
			defer close(flushed)
			r.dev.subs.FlushFabric(ctx, resumeFabricIndex, 0)
		}()
		var exch uint16
		waitFor(t, "the flush report waiting for its StatusResponse", func() bool {
			found := false
			r.dev.br.routing.statusResponseWaits.Range(func(k, _ any) bool {
				if ek, ok := k.(mrp.ExchangeKey); ok && ek.SessionID == r.reportSID && ek.Initiator {
					exch, found = ek.ExchangeID, true
				}
				return !found
			})
			return found
		})
		return r, st, exch, flushed
	}
	reannounced := func(r *undeliveredReportRig) bool {
		for _, ev := range r.dev.br.DiagnosticEvents() {
			if strings.Contains(ev.Message, "re-announced itself") {
				return true
			}
		}
		return false
	}

	t.Run("terminates when the peer refuses a report sent during a shutdown close", func(t *testing.T) {
		t.Parallel()
		r, st, exch, flushed := flushing(t, 0x1801)
		if !r.dev.br.signalStatusResponseRX(r.reportSID, exch, true, im.StatusInvalidSubscription) {
			t.Fatal("the refusal found no waiting flush report")
		}
		<-flushed
		if !r.sub.IsClosed() {
			t.Fatal("the subscription whose flush report was refused lives on")
		}
		if slices.Contains(st.ids(), r.sub.ID) {
			t.Fatalf("the refused subscription's record %d stays persisted (%v)", r.sub.ID, st.ids())
		}
		// The shutdown closed the advertiser before the flush in matter.js
		// (ServerNetworkRuntime.ts:449-459, DeviceAdvertiser.ts:331-332,
		// :356-357 at b25ed960): a refused flush announces nothing.
		if reannounced(r) {
			t.Fatal("a refused flush report re-announced the fabric")
		}
	})
	t.Run("keeps the subscription whose flush report is unanswered", func(t *testing.T) {
		t.Parallel()
		r, st, _, flushed := flushing(t, 0x1803)
		<-flushed // after flushStatusResponseTimeout on the bridge's clock
		if r.sub.IsClosed() {
			t.Fatal("an unanswered flush report ended the subscription")
		}
		if !slices.Contains(st.ids(), r.sub.ID) {
			t.Fatalf("the record of subscription %d went with an unanswered flush report", r.sub.ID)
		}
	})
	t.Run("keeps the subscription whose flush report is answered with success", func(t *testing.T) {
		t.Parallel()
		r, st, exch, flushed := flushing(t, 0x1805)
		if !r.dev.br.signalStatusResponseRX(r.reportSID, exch, true, im.StatusSuccess) {
			t.Fatal("the answer found no waiting flush report")
		}
		<-flushed
		if r.sub.IsClosed() || !slices.Contains(st.ids(), r.sub.ID) {
			t.Fatalf("closed %v, records %v; want open and persisted", r.sub.IsClosed(), st.ids())
		}
	})
}
