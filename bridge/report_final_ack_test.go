// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/im/subscription"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// TestReportStatusResponseAckedAtOnce pins that the peer's StatusResponse
// to an ongoing report — the last message of the bridge's report
// exchange, no chunk loop waiting for it — is acknowledged at once by a
// standalone ack, not left to the ack pump's grace window. matter.js
// closes the exchange after it and MessageExchange.close sends the
// standalone ack of the last received message immediately
// (packages/protocol/src/protocol/MessageExchange.ts:1117-1124 at
// 862ad75f). The tracker here is attached after Start, so no pump runs:
// before the fix the ack never left, and a controller's StatusResponse
// waited on it until the controller's MRP gave up or its teardown bound
// ran out.
func TestReportStatusResponseAckedAtOnce(t *testing.T) {
	t.Parallel()
	const sessionID uint16 = 78
	b := newStartedBridge(t)
	b.AttachAckTracker(mrp.NewAckTracker(time.Hour))
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
		src: peerAddr, hasPeerSourceNodeID: true, peerSourceNodeID: 0xBBBB4444,
		exchangeID: 19, sessionID: sessionID, peerInitiator: true,
	})

	b.reportSubscription(context.Background(), sub, paths)
	proto, _, ok := readOneIMDatagram(t, peerConn, peerSess, time.Second)
	if !ok || !proto.Initiator {
		t.Fatalf("no bridge-initiated report reached the peer (%v, %+v)", ok, proto)
	}
	body, err := EncodeStatusResponse(im.StatusResponse{Status: im.StatusSuccess})
	if err != nil {
		t.Fatal(err)
	}
	datagram := encryptIMFromPeer(t, peerSess, sessionID, message.ProtocolHeader{
		Opcode: im.OpcodeStatusResponse, ExchangeID: proto.ExchangeID, Initiator: false, NeedsAck: true,
	}, body)
	if err := b.dispatch(context.Background(), datagram, peerAddr); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	ack, _, ok := readOneIMDatagram(t, peerConn, peerSess, time.Second)
	if !ok {
		t.Fatal("the StatusResponse ending the report exchange was not acknowledged")
	}
	if ack.ProtocolID != mrp.SecureChannelProtocolID || ack.Opcode != mrp.StandaloneAckOpcode || !ack.HasAck || ack.ExchangeID != proto.ExchangeID {
		t.Fatalf("got %+v, want a standalone ack on exchange %d", ack, proto.ExchangeID)
	}
}
