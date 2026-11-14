// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/im/subscription"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/transport/message"
)

// TestSubscribeAdmittedAtTheQuotaLogsNoRefusal: a SubscribeRequest the
// bridge admits at its fabric's quota, in the place of the fabric's oldest
// subscription (review II, R2-P3-admin-03), is not logged as a refusal of
// the subscription manager — the first, refused attempt is the step before
// the eviction, not an outcome.
func TestSubscribeAdmittedAtTheQuotaLogsNoRefusal(t *testing.T) {
	t.Parallel()
	var logs syncBuffer
	b, err := New(wbEmptySnapshotter, mdns.NewNoop(), Config{Listen: ":0"},
		slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := b.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer stopCancel()
		_ = b.Stop(stopCtx)
	})
	b.AttachRootClusters([]contract.ClusterServer{&panickingOnRead{}})
	if err := b.Reassemble(ctx); err != nil {
		t.Fatalf("Reassemble: %v", err)
	}
	mgr := subscription.NewManager(subscription.Config{MaxSubscriptionsPerFabric: 1}, nil, nil)
	b.AttachSubscriptionManager(mgr)
	attachPASETestPeer(t, b)

	path := im.ConcreteAttributePath{Endpoint: 0, HasEndpoint: true, Cluster: 0x0006, HasCluster: true, Attribute: 0, HasAttribute: true}
	req := im.SubscribeRequest{AttributeRequests: []im.ConcreteAttributePath{path}, KeepSubscriptions: true, MaxIntervalCeiling: 60}
	peerConn, peerAddr := newSubscribeTestPeer(t)
	opcodes := answerReportChunks(b, peerConn)
	for i := range 2 {
		hdr := &message.Header{SessionID: paseTestSessionID, MessageCounter: uint32(200 + i)}
		proto := message.ProtocolHeader{ProtocolID: im.InteractionModelProtocolID, Opcode: im.OpcodeSubscribeRequest, ExchangeID: uint16(10 + i)}
		done := make(chan error, 1)
		go func() { done <- b.handleSubscribeRequest(ctx, peerAddr, hdr, proto, req) }()
		if op := nextSubscribeOpcode(t, opcodes); op != im.OpcodeReportData {
			t.Fatalf("subscription %d: first reply opcode = 0x%02X, want ReportData", i+1, op)
		}
		if op := nextSubscribeOpcode(t, opcodes); op != im.OpcodeSubscribeResponse {
			t.Fatalf("subscription %d: second reply opcode = 0x%02X, want SubscribeResponse", i+1, op)
		}
		if err := <-done; err != nil {
			t.Fatalf("subscription %d: handleSubscribeRequest: %v", i+1, err)
		}
	}
	if n := mgr.Active(); n != 1 {
		t.Fatalf("mgr.Active() = %d, want 1 (the second admitted in the first's place)", n)
	}
	for line := range strings.SplitSeq(logs.String(), "\n") {
		if strings.Contains(line, "matter.rx.im.subscribe.manager") && strings.Contains(line, "level=WARN") {
			t.Errorf("a subscription admitted at the quota logged a refusal: %s", line)
		}
	}
}
