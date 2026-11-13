// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/im/subscription"
	"github.com/SukramJ/go-fabric/store"
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

// TestReestablishedSubscriptionEventInThePrimingReportIsNotReportedAgain
// (review II, R2-P1-dr-req-06): the re-establishment of a former
// subscription after a restart ends its priming as a SubscribeRequest's
// does — an event fired after the subscription was restored and before its
// priming report read the log goes out in the priming report and is not
// reported again by the engine afterwards. matter.js runs the same
// activation on this path (InteractionServer.ts:906 sendInitialReport,
// then :911 subscription.activate()).
func TestReestablishedSubscriptionEventInThePrimingReportIsNotReportedAgain(t *testing.T) {
	t.Parallel()
	const fabric, subject = 1, 0x1234 // caseOnFabricLookup's subject
	b := newStartedBridge(t)
	srv := &changingOnRead{}
	// The event fires while the priming report reads the attribute, so
	// after the restore and before the report reads the event log.
	srv.onChange = func() { b.EmitEvent(0, 0x0006, 0x00, nil, contract.EventPriorityCritical) }
	acl := &aclStoreFake{entries: []store.ACLEntry{{FabricIndex: fabric, Privilege: store.PrivilegeView, AuthMode: store.AuthModeCASE}}}
	ac, err := core.NewAccessControl(acl)
	if err != nil {
		t.Fatal(err)
	}
	b.AttachRootClusters([]contract.ClusterServer{ac, srv})
	b.AttachACLLister(acl)
	if err := b.Reassemble(context.Background()); err != nil {
		t.Fatalf("Reassemble: %v", err)
	}
	mgr := subscription.NewManager(subscription.Config{}, nil, nil)
	b.AttachSubscriptionManager(mgr)
	lookup, peerSess := newPASETestSessions(t)
	b.AttachSessionLookup(caseOnFabricLookup{SessionLookup: lookup, id: paseTestSessionID, fabric: fabric})
	var mu sync.Mutex
	var later []im.EventReport
	mgr.SetEventReporter(func(_ context.Context, _ *subscription.Subscription, events []im.EventReport) {
		mu.Lock()
		later = append(later, events...)
		mu.Unlock()
	})

	// The peer answers every chunk of the priming report.
	peerConn, peerAddr := newSubscribeTestPeer(t)
	go func() {
		rbuf := make([]byte, 1500)
		for {
			_ = peerConn.SetReadDeadline(time.Now().Add(5 * time.Second))
			n, _, err := peerConn.ReadFromUDP(rbuf)
			if err != nil {
				return
			}
			hdr, hdrLen, err := message.UnmarshalHeader(rbuf[:n])
			if err != nil {
				continue
			}
			plain, _, err := peerSess.Decrypt(&hdr, securityFlagsByte(&hdr), rbuf[hdrLen:n])
			if err != nil {
				continue
			}
			rproto, _, err := message.UnmarshalProtocolHeader(plain)
			if err == nil && rproto.Opcode == im.OpcodeReportData {
				b.signalStatusResponseRX(hdr.SessionID, rproto.ExchangeID, rproto.Initiator, im.StatusSuccess)
			}
		}
	}()
	rec := subscription.PeerSubscription{
		SubscriptionID: 0x5150, FabricIndex: fabric, PeerNodeID: subject,
		AttributeRequests:  []im.ConcreteAttributePath{changingPath()},
		EventRequests:      []im.ConcreteEventPath{{}},
		MaxIntervalCeiling: 60, MaxInterval: 60,
	}
	if err := b.establishFormerSubscription(context.Background(), rec, peerLink{sessionID: paseTestSessionID, addr: peerAddr}); err != nil {
		t.Fatalf("establishFormerSubscription: %v", err)
	}
	// The priming report read the log after the event fired, under the
	// subject the re-establishment reads for: the same read now returns it.
	readCtx := im.WithSubject(im.WithFabricFilter(context.Background(), false, fabric), subject, nil)
	primed, _ := b.buildInitialReport(readCtx, b.Dispatcher(), im.SubscribeRequest{EventRequests: rec.EventRequests})
	if len(primed.EventReports) != 1 {
		t.Fatalf("the event log holds %d events for the subscriber, want the one fired during the priming read", len(primed.EventReports))
	}
	sub, err := mgr.Get(rec.SubscriptionID)
	if err != nil {
		t.Fatal(err)
	}
	// Past the send interval the engine reports whatever is owed.
	mgr.Tick(context.Background(), b.clk().Now().Add(sub.SendInterval()+time.Second))
	mu.Lock()
	defer mu.Unlock()
	for _, ev := range later {
		if ev.Number == primed.EventReports[0].Number {
			t.Fatalf("event %d was in the priming report and is reported again after it", ev.Number)
		}
	}
}
