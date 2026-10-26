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

// changingOnRead is a root cluster server whose first read — the one the
// Subscribe's priming report makes — changes the attribute it is being
// read for, the way a server's state can move while a priming report is
// collected.
type changingOnRead struct {
	once     sync.Once
	onChange func()
}

func (*changingOnRead) MatterClusterID() uint32 { return 0x0006 }

func (c *changingOnRead) MatterRead(attrID uint32) (any, bool) {
	if attrID != 0 {
		return nil, false
	}
	c.once.Do(c.onChange)
	return false, true
}

func (*changingOnRead) MatterReportable() []uint32                             { return []uint32{0} }
func (*changingOnRead) MatterWrite(context.Context, uint32, any) error         { return nil }
func (*changingOnRead) MatterInvoke(context.Context, uint32, any) (any, error) { return nil, nil }

func changingPath() im.ConcreteAttributePath {
	return im.ConcreteAttributePath{Endpoint: 0, HasEndpoint: true, Cluster: 0x0006, HasCluster: true, Attribute: 0, HasAttribute: true}
}

// TestHandleSubscribeRequest_ChangeDuringPrimingReadIsReported pins the
// order of admission and priming read: a change made while the priming
// data is collected reaches the subscriber in its first report after the
// SubscribeResponse. matter.js attaches the subscription's change
// handlers before it reads the initial report
// (packages/node/src/node/server/ServerSubscription.ts:731-739 at
// 862ad75f); a bridge that read first and admitted after lost the change
// (the controller-side CI flake "flips requiresAwait when a registered
// DSLS peer switches SIT→LIT at runtime": the device's OperatingMode
// flipped inside the recreated subscription's priming read).
func TestHandleSubscribeRequest_ChangeDuringPrimingReadIsReported(t *testing.T) {
	t.Parallel()
	var (
		mgr      *subscription.Manager
		reported = make(chan []im.ConcreteAttributePath, 4)
	)
	srv := &changingOnRead{}
	srv.onChange = func() { mgr.OnAttributeChanged(changingPath()) }
	b := newStartedBridge(t)
	b.AttachRootClusters([]contract.ClusterServer{srv})
	if err := b.Reassemble(context.Background()); err != nil {
		t.Fatalf("Reassemble: %v", err)
	}
	mgr = subscription.NewManager(subscription.Config{}, func(_ context.Context, _ *subscription.Subscription, paths []im.ConcreteAttributePath) {
		reported <- paths
	}, nil)
	b.AttachSubscriptionManager(mgr)

	peerConn, peerAddr := newSubscribeTestPeer(t)
	hdr := &message.Header{SessionID: 0, MessageCounter: 103}
	proto := message.ProtocolHeader{ProtocolID: im.InteractionModelProtocolID, Opcode: im.OpcodeSubscribeRequest, ExchangeID: 1}
	req := im.SubscribeRequest{
		AttributeRequests:  []im.ConcreteAttributePath{changingPath()},
		MinIntervalFloor:   0,
		MaxIntervalCeiling: 60,
	}

	opcodes := answerReportChunks(b, peerConn)
	done := make(chan error, 1)
	go func() { done <- b.handleSubscribeRequest(context.Background(), peerAddr, hdr, proto, req) }()
	if op := nextSubscribeOpcode(t, opcodes); op != im.OpcodeReportData {
		t.Fatalf("first reply opcode = 0x%02X, want ReportData", op)
	}
	if op := nextSubscribeOpcode(t, opcodes); op != im.OpcodeSubscribeResponse {
		t.Fatalf("second reply opcode = 0x%02X, want SubscribeResponse", op)
	}
	if err := <-done; err != nil {
		t.Fatalf("handleSubscribeRequest: %v", err)
	}
	if n := mgr.Active(); n != 1 {
		t.Fatalf("mgr.Active() = %d, want 1", n)
	}

	// The change marked the priming subscription dirty; the first tick
	// past MinInterval reports it.
	mgr.Tick(context.Background(), time.Now().Add(2*time.Second))
	select {
	case paths := <-reported:
		if len(paths) != 1 || paths[0] != changingPath() {
			t.Fatalf("reported %v, want the changed path alone", paths)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the change made during the priming read was not reported: the subscription was admitted after the read")
	}
}
