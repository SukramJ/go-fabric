// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/im/subscription"
	"thingfabric.dev/go-fabric/tlv"
	"thingfabric.dev/go-fabric/transport/message"
)

// openOneContainerW2B10 is a host attribute value whose EncodeTLV leaves
// one structure open: the report's encoder then still stands inside the
// AttributeReports array when the report's context-tagged
// SuppressResponse follows, and tlv.Encoder panics (w2-b2b's measurement).
type openOneContainerW2B10 struct{}

func (openOneContainerW2B10) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) {
	enc.StartStruct(tag)
}

// turningBadW2B10 is a root cluster server whose attribute 0 is a plain
// bool until bad is set, and an [openOneContainerW2B10] after.
type turningBadW2B10 struct {
	bad atomic.Bool
}

func (*turningBadW2B10) MatterClusterID() uint32 { return 0x0006 }

func (s *turningBadW2B10) MatterRead(attrID uint32) (any, bool) {
	if attrID != 0 {
		return nil, false
	}
	if s.bad.Load() {
		return openOneContainerW2B10{}, true
	}
	return false, true
}

func (*turningBadW2B10) MatterReportable() []uint32                             { return []uint32{0} }
func (*turningBadW2B10) MatterWrite(context.Context, uint32, any) error         { return nil }
func (*turningBadW2B10) MatterInvoke(context.Context, uint32, any) (any, error) { return nil, nil }

// logSinkW2B10 is a log sink safe for the bridge's goroutines.
type logSinkW2B10 struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *logSinkW2B10) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *logSinkW2B10) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

// TestHostEncoderPanicInOngoingReportKeepsTheNodeW2B10: a host value
// whose EncodeTLV leaves one container open in an active subscription's
// ongoing report panicked in the report goroutine (w2-b2b: the process
// exited); wave 2's engine backstop then logged it and dropped the report.
// Since #625 the value is encoded in isolation when the chunker admits
// it: the report goes out with FAILURE for the path, the backstop is not
// reached, the node and the subscription live, and the next change
// reports again once the value encodes. Red before the backstop: the test
// binary dies; red before #625: the backstop's ERROR is logged and no
// report goes out.
func TestHostEncoderPanicInOngoingReportKeepsTheNodeW2B10(t *testing.T) {
	t.Parallel()
	srv := &turningBadW2B10{}
	b := newStartedBridge(t)
	b.AttachRootClusters([]contract.ClusterServer{srv})
	if err := b.Reassemble(context.Background()); err != nil {
		t.Fatalf("Reassemble: %v", err)
	}
	logs := &logSinkW2B10{}
	mgr := subscription.NewManager(subscription.Config{}, nil,
		slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	b.AttachSubscriptionManager(mgr)
	attachPASETestPeer(t, b)

	path := im.ConcreteAttributePath{Endpoint: 0, HasEndpoint: true, Cluster: 0x0006, HasCluster: true, Attribute: 0, HasAttribute: true}
	peerConn, peerAddr := newSubscribeTestPeer(t)
	hdr := &message.Header{SessionID: paseTestSessionID, MessageCounter: 110}
	proto := message.ProtocolHeader{ProtocolID: im.InteractionModelProtocolID, Opcode: im.OpcodeSubscribeRequest, ExchangeID: 1}
	req := im.SubscribeRequest{AttributeRequests: []im.ConcreteAttributePath{path}, MaxIntervalCeiling: 60}

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

	// The value turns bad: the ongoing report goes out with FAILURE for
	// the path. Tick waits for the report goroutine.
	t0 := time.Now()
	srv.bad.Store(true)
	mgr.OnAttributeChanged(path)
	mgr.Tick(context.Background(), t0.Add(2*time.Second))
	if strings.Contains(logs.String(), "im.subscription.report_panic") {
		t.Fatalf("the report panicked; log = %q", logs.String())
	}
	if op := nextSubscribeOpcode(t, opcodes); op != im.OpcodeReportData {
		t.Fatalf("reply for the failing value = 0x%02X, want ReportData", op)
	}
	if n := mgr.Active(); n != 1 {
		t.Fatalf("mgr.Active() = %d after the failing report, want the subscription kept", n)
	}

	// The value encodes again: the next change reports.
	srv.bad.Store(false)
	mgr.OnAttributeChanged(path)
	mgr.Tick(context.Background(), t0.Add(4*time.Second))
	if op := nextSubscribeOpcode(t, opcodes); op != im.OpcodeReportData {
		t.Fatalf("reply after the failing report = 0x%02X, want ReportData", op)
	}
}
