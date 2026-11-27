// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package interaction

// A port of matter.js
// packages/protocol/test/action/client/subscription/PeerSubscriptionTest.ts
// at db91886c (#4943, #5046): reportInterrupted of a peer subscription
// (PeerSubscription.ts:60-126 at db91886c). matter.js captures the logger's
// lines; the Go subscription logs through its slog.Logger, captured here.

import (
	"bytes"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/client/exchange"
)

// logBuffer is a slog destination tests read back.
type logBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// lossLogs are the lines that report a subscription lost to an
// interrupted report — lossLogs of PeerSubscriptionTest.ts:41 ("lost:").
func (b *logBuffer) lossLogs() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []string
	for line := range strings.SplitSeq(b.buf.String(), "\n") {
		if strings.Contains(line, "subscription_lost_report_") {
			out = append(out, line)
		}
	}
	return out
}

// aPeerSubscription is aSubscription of PeerSubscriptionTest.ts:13-23: a
// peer subscription with id 1 and max interval 60 s whose closed callback
// is onClosed.
func aPeerSubscription(log *logBuffer, compactPriming bool, onClosed func()) *Subscription {
	r := NewSubscriptions(SubscriptionsConfig{})
	logger := slog.New(slog.NewTextHandler(log, &slog.HandlerOptions{Level: slog.LevelDebug}))
	s := r.newSubscription(exchange.PeerAddress{FabricIndex: 1, NodeID: 1}, SubscribeRequest{}, logger)
	s.id, s.maxInterval, s.maxPeerResponseTime = 1, 60, 10*time.Second
	s.compactPriming = compactPriming
	s.closed = onClosed
	return s
}

func isSubscriptionClosed(s *Subscription) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.isClosed
}

func TestPeerSubscriptionReportInterruptedParity(t *testing.T) {
	t.Parallel()
	t.Run("closes the subscription and logs the loss", func(t *testing.T) {
		t.Parallel()
		var log logBuffer
		closed := 0
		s := aPeerSubscription(&log, true, func() { closed++ })

		s.reportInterrupted(s.nextReportNumber(), reportFinalUnacknowledged)

		if closed != 1 || !isSubscriptionClosed(s) {
			t.Fatalf("closed %d times, isClosed %v", closed, isSubscriptionClosed(s))
		}
		if n := len(log.lossLogs()); n != 1 {
			t.Fatalf("%d loss logs, want 1", n)
		}
	})
	t.Run("keeps a subscription without compact priming after an unacknowledged final status", func(t *testing.T) {
		t.Parallel()
		var log logBuffer
		s := aPeerSubscription(&log, false, nil)

		s.reportInterrupted(s.nextReportNumber(), reportFinalUnacknowledged)

		if isSubscriptionClosed(s) {
			t.Fatal("the subscription closed")
		}
	})
	t.Run("keeps a subscription created without a compact priming flag after an unacknowledged final status", func(t *testing.T) {
		t.Parallel()
		var log logBuffer
		r := NewSubscriptions(SubscriptionsConfig{})
		s := r.newSubscription(exchange.PeerAddress{FabricIndex: 1, NodeID: 1}, SubscribeRequest{},
			slog.New(slog.NewTextHandler(&log, nil)))
		s.id, s.maxInterval, s.maxPeerResponseTime = 1, 60, 10*time.Second

		s.reportInterrupted(s.nextReportNumber(), reportFinalUnacknowledged)

		if isSubscriptionClosed(s) {
			t.Fatal("the subscription closed")
		}
	})
	t.Run("closes a subscription without compact priming after an unacknowledged chunk status", func(t *testing.T) {
		t.Parallel()
		var log logBuffer
		s := aPeerSubscription(&log, false, nil)

		s.reportInterrupted(s.nextReportNumber(), reportChunkUnacknowledged)

		if !isSubscriptionClosed(s) {
			t.Fatal("the subscription stayed open")
		}
	})
	t.Run("closes a subscription without compact priming after a missing chunk and logs the loss", func(t *testing.T) {
		t.Parallel()
		var log logBuffer
		s := aPeerSubscription(&log, false, nil)

		s.reportInterrupted(s.nextReportNumber(), reportChunkMissing)

		if !isSubscriptionClosed(s) {
			t.Fatal("the subscription stayed open")
		}
		missing := 0
		for _, line := range log.lossLogs() {
			if strings.Contains(line, "subscription_lost_report_chunk_missing") {
				missing++
			}
		}
		if missing != 1 {
			t.Fatalf("%d chunk-missing loss logs, want 1: %v", missing, log.lossLogs())
		}
	})
	t.Run("keeps a subscription on which a newer report started", func(t *testing.T) {
		t.Parallel()
		var log logBuffer
		s := aPeerSubscription(&log, true, nil)
		unacknowledged := s.nextReportNumber()
		s.nextReportNumber()

		s.reportInterrupted(unacknowledged, reportChunkUnacknowledged)

		if isSubscriptionClosed(s) {
			t.Fatal("the subscription closed")
		}
	})
	t.Run("logs nothing for a subscription that is already closed", func(t *testing.T) {
		t.Parallel()
		var log logBuffer
		s := aPeerSubscription(&log, true, nil)
		s.Close()

		s.reportInterrupted(s.nextReportNumber(), reportFinalUnacknowledged)

		if lines := log.lossLogs(); len(lines) != 0 {
			t.Fatalf("loss logs %v", lines)
		}
	})
}
