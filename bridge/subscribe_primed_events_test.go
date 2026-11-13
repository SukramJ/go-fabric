// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/im/subscription"
	"github.com/SukramJ/go-fabric/transport/message"
)

// TestEventInThePrimingReportIsNotReportedAgain (review II,
// R2-P1-req2-03): an event fired after the subscription was admitted and
// before its priming report read the log goes out in the priming report;
// the SubscribeResponse hands the report's last event number to the
// subscription, and the first report after the priming does not carry
// the event again.
func TestEventInThePrimingReportIsNotReportedAgain(t *testing.T) {
	t.Parallel()
	b := newACLTestBridge(t, &aclStoreFake{})
	mgr := subscription.NewManager(subscription.Config{}, nil, nil)
	b.AttachSubscriptionManager(mgr)
	attachPASETestPeer(t, b)
	var mu sync.Mutex
	var later []im.EventReport
	mgr.SetEventReporter(func(_ context.Context, _ *subscription.Subscription, events []im.EventReport) {
		mu.Lock()
		later = append(later, events...)
		mu.Unlock()
	})

	_, src := newSubscribeTestPeer(t)
	hdr := &message.Header{SessionID: paseTestSessionID}
	proto := message.ProtocolHeader{ProtocolID: im.InteractionModelProtocolID, ExchangeID: 7, Initiator: true}
	req := im.SubscribeRequest{
		AttributeRequests:  []im.ConcreteAttributePath{accessControlACLPath()},
		EventRequests:      []im.ConcreteEventPath{{}},
		MaxIntervalCeiling: 60,
		KeepSubscriptions:  true,
	}
	var stamp im.ReportData
	subID, err := b.registerSubscription(src, hdr, proto, req, &stamp)
	if err != nil {
		t.Fatal(err)
	}
	// The event fires while the priming data is being collected.
	b.EmitEvent(0, 0x0028, 0x00, nil, contract.EventPriorityCritical)
	subCtx := im.WithAuthModePASE(im.WithFabricFilter(context.Background(), false, 0))
	primed, _ := b.buildInitialReport(subCtx, b.Dispatcher(), req)
	if len(primed.EventReports) != 1 {
		t.Fatalf("the priming report carries %d events, want the one fired", len(primed.EventReports))
	}
	primed.HasSubscription, primed.SubscriptionID = stamp.HasSubscription, stamp.SubscriptionID
	if err := b.sendSubscribeResponse(src, hdr, proto, req, subID, primed); err != nil {
		t.Fatalf("sendSubscribeResponse: %v", err)
	}
	mgr.Tick(context.Background(), b.clk().Now().Add(2*time.Second))
	mu.Lock()
	defer mu.Unlock()
	for _, ev := range later {
		if ev.Number == primed.EventReports[0].Number {
			t.Fatalf("event %d was in the priming report and is reported again after it", ev.Number)
		}
	}
}
