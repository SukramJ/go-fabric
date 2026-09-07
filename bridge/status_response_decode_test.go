// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

// White-box tests for what the bridge does with the STATUS carried by
// an inbound IM StatusResponse: an error status answering an ongoing
// report ends the subscription, and an error status answering a chunk
// aborts the chunk loop. Lives in package bridge to reach the reporters,
// the receive pipeline and the routing tables directly.

import (
	"context"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/im/subscription"
	"github.com/SukramJ/go-fabric/tlv"
	"github.com/SukramJ/go-fabric/transport/message"
)

// TestStatusResponseWithErrorStatusEndsTheOngoingSubscription pins that
// a peer answering an ongoing report with InvalidSubscription (0x7d)
// ends the subscription — and that a Success answer does not. matter.js
// throws on any non-Success StatusResponse after a DataReport
// (InteractionMessenger.ts:183-196 throwIfErrorStatusMessage, reached
// through waitForSuccess at :721) and ServerSubscription.ts:866-876
// marks the subscription terminated and closes it. Without this the
// engine keeps ticking a subscription the peer has forgotten, the
// per-fabric quota slot stays occupied, and the peer's own reply keeps
// the session's idle reaper from ever firing.
func TestStatusResponseWithErrorStatusEndsTheOngoingSubscription(t *testing.T) {
	t.Parallel()
	const sessionID uint16 = 77
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
		exchangeID:          19,
		sessionID:           sessionID,
		peerInitiator:       true,
	})

	answer := func(status im.StatusCode) {
		t.Helper()
		b.reportSubscription(context.Background(), sub, paths)
		proto, _, ok := readOneIMDatagram(t, peerConn, peerSess, time.Second)
		if !ok {
			t.Fatal("no report reached the peer")
		}
		if !proto.Initiator {
			t.Fatal("report did not open a bridge-initiated exchange")
		}
		body, err := EncodeStatusResponse(im.StatusResponse{Status: status})
		if err != nil {
			t.Fatalf("EncodeStatusResponse: %v", err)
		}
		// The peer answers on the bridge's exchange as the responder.
		datagram := encryptIMFromPeer(t, peerSess, sessionID, message.ProtocolHeader{
			Opcode: im.OpcodeStatusResponse, ExchangeID: proto.ExchangeID, Initiator: false,
		}, body)
		if err := b.dispatch(context.Background(), datagram, peerAddr); err != nil {
			t.Fatalf("dispatch: %v", err)
		}
	}

	// Negative control: a Success answer leaves the subscription alone.
	answer(im.StatusSuccess)
	if mgr.Active() != 1 {
		t.Fatalf("manager.Active() = %d after a Success StatusResponse, want 1", mgr.Active())
	}
	if _, ok := b.routing.subTargets.Load(sub.ID); !ok {
		t.Fatal("subTarget dropped after a Success StatusResponse")
	}

	answer(im.StatusInvalidSubscription)
	if mgr.Active() != 0 {
		t.Errorf("manager.Active() = %d after an InvalidSubscription StatusResponse, want 0 (the peer reported the subscription invalid)", mgr.Active())
	}
	if _, ok := b.routing.subTargets.Load(sub.ID); ok {
		t.Error("subTarget still routed after the peer reported the subscription invalid")
	}
}

// buildWildcardReadRequestPayload returns a ReadRequest naming every
// attribute of every cluster on every endpoint.
func buildWildcardReadRequestPayload(t *testing.T) []byte {
	t.Helper()
	enc := tlv.NewEncoder()
	im.ReadRequest{AttributeRequests: []im.ConcreteAttributePath{{}}}.MarshalTLV(enc)
	out, err := enc.Bytes()
	if err != nil {
		t.Fatalf("ReadRequest.MarshalTLV: %v", err)
	}
	return out
}

// TestReadChunkLoopAbortsOnErrorStatusResponse pins that an error
// StatusResponse answering a ReportData chunk aborts the interaction
// instead of releasing the next chunk. matter.js's per-chunk
// waitForSuccess (InteractionMessenger.ts:721) throws on any
// non-Success status (:183-196) and the read is abandoned.
func TestReadChunkLoopAbortsOnErrorStatusResponse(t *testing.T) {
	t.Parallel()
	const sessionID uint16 = 78
	b := newStartedBridgeWithSnapshotter(t, manyTempSensorsSnapshotterForTest())
	peerConn, peerSess, peerAddr := chunkTestPeer(t, b, sessionID)

	const readExchange uint16 = 0x2001
	request := encryptIMFromPeer(t, peerSess, sessionID, message.ProtocolHeader{
		Opcode: im.OpcodeReadRequest, ExchangeID: readExchange, Initiator: true,
	}, buildWildcardReadRequestPayload(t))

	done := make(chan error, 1)
	go func() { done <- b.dispatch(context.Background(), request, peerAddr) }()

	proto, _, ok := readOneIMDatagram(t, peerConn, peerSess, 2*time.Second)
	if !ok {
		t.Fatal("no ReportData chunk reached the peer")
	}
	if proto.ExchangeID != readExchange {
		t.Fatalf("chunk exchange = %d, want the read exchange %d", proto.ExchangeID, readExchange)
	}
	// The peer rejects the first chunk.
	body, err := EncodeStatusResponse(im.StatusResponse{Status: im.StatusFailure})
	if err != nil {
		t.Fatalf("EncodeStatusResponse: %v", err)
	}
	reject := encryptIMFromPeer(t, peerSess, sessionID, message.ProtocolHeader{
		Opcode: im.OpcodeStatusResponse, ExchangeID: readExchange, Initiator: true,
	}, body)
	if err := b.dispatch(context.Background(), reject, peerAddr); err != nil {
		t.Fatalf("dispatch(StatusResponse): %v", err)
	}

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("read dispatch still running 3 s after the peer rejected the first chunk")
	}
	if extra, _, got := readOneIMDatagram(t, peerConn, peerSess, 300*time.Millisecond); got {
		t.Errorf("a further datagram (opcode %#x, exchange %d) followed the peer's error StatusResponse; the read must be aborted", extra.Opcode, extra.ExchangeID)
	}
}
