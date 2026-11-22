// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

// Ports matter.js packages/node/test/node/ServerSubscriptionTest.ts
// describe("keep-alive status") at db021da0 by meaning: a keep-alive — an
// ongoing report without data — asks the peer for its status
// (suppressResponse false, ServerSubscription.ts:909-920, :913-914), and
// the peer's INVALID_SUBSCRIPTION answer ends the subscription
// (:938-950), while a SUCCESS answer keeps it.

import (
	"context"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/im/subscription"
	"thingfabric.dev/go-fabric/transport/message"
)

// TestKeepAliveStatusParity: "requests a status on an MRP session and ends
// the subscription on INVALID_SUBSCRIPTION", and "waits for the status and
// keeps the subscription when the peer answers SUCCESS" — the keep-alive's
// reporter returns only once the peer answered it
// ([Bridge.deliverOngoingReport]; F-db021da0-36), and the subscription
// stays.
func TestKeepAliveStatusParity(t *testing.T) {
	t.Parallel()
	const sessionID uint16 = 83

	b := newStartedBridge(t)
	mgr := subscription.NewManager(subscription.Config{}, nil, nil)
	b.AttachSubscriptionManager(mgr)
	peerConn, peerSess, peerAddr := chunkTestPeer(t, b, sessionID)
	paths := []im.ConcreteAttributePath{
		{Endpoint: 0, Cluster: 0x001D, Attribute: 0x0000, HasEndpoint: true, HasCluster: true, HasAttribute: true},
	}
	sub, err := mgr.Subscribe(subscription.SubscribeArgs{
		SessionID: sessionID, MinIntervalFloor: 1, MaxIntervalCeiling: 60, AttributePaths: paths,
	})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	b.routing.subTargets.Store(sub.ID, subTarget{
		src:                 peerAddr,
		hasPeerSourceNodeID: true,
		peerSourceNodeID:    0xBBBB4444,
		exchangeID:          21,
		sessionID:           sessionID,
		peerInitiator:       true,
	})

	keepAliveAnswered := func(status im.StatusCode) {
		t.Helper()
		// No path is due: the report is the keep-alive. Its reporter
		// waits for the answer.
		reported := make(chan struct{})
		go func() {
			defer close(reported)
			b.reportSubscription(context.Background(), sub, nil)
		}()
		proto, body, ok := readOneIMDatagram(t, peerConn, peerSess, time.Second)
		if !ok {
			t.Fatal("no keep-alive reached the peer")
		}
		if proto.Opcode != im.OpcodeReportData || !proto.Initiator {
			t.Fatalf("keep-alive: opcode %#x initiator %v, want a ReportData opening a bridge-initiated exchange", proto.Opcode, proto.Initiator)
		}
		suppress, present, err := decodeReportDataSuppressResponse(body)
		if err != nil {
			t.Fatalf("keep-alive body: %v", err)
		}
		if !present || suppress {
			t.Fatalf("keep-alive SuppressResponse present=%v value=%v, want present and false: the keep-alive asks the peer for its status", present, suppress)
		}
		answer, err := EncodeStatusResponse(im.StatusResponse{Status: status})
		if err != nil {
			t.Fatalf("EncodeStatusResponse: %v", err)
		}
		datagram := encryptIMFromPeer(t, peerSess, sessionID, message.ProtocolHeader{
			Opcode: im.OpcodeStatusResponse, ExchangeID: proto.ExchangeID, Initiator: false,
		}, answer)
		select {
		case <-reported:
			t.Fatal("the keep-alive's reporter returned before the peer answered it")
		default:
		}
		if err := b.dispatch(context.Background(), datagram, peerAddr); err != nil {
			t.Fatalf("dispatch: %v", err)
		}
		<-reported // a reporter that never returns fails the test by its timeout
	}

	keepAliveAnswered(im.StatusSuccess)
	if mgr.Active() != 1 {
		t.Fatalf("subscriptions after a SUCCESS answer to the keep-alive: %d, want 1", mgr.Active())
	}

	keepAliveAnswered(im.StatusInvalidSubscription)
	if mgr.Active() != 0 {
		t.Errorf("subscriptions after an INVALID_SUBSCRIPTION answer to the keep-alive: %d, want 0", mgr.Active())
	}
	if _, ok := b.routing.subTargets.Load(sub.ID); ok {
		t.Error("the subscription is still routed after the peer answered its keep-alive INVALID_SUBSCRIPTION")
	}
}
