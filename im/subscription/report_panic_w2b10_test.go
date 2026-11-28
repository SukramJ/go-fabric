// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package subscription_test

import (
	"bytes"
	"context"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/im/subscription"
)

// lockedBufferW2B10 is a log sink the report goroutine and the test share.
type lockedBufferW2B10 struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBufferW2B10) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBufferW2B10) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// TestReportSenderPanicDropsTheReportNotTheProcessW2B10: a panic while a
// subscription's report is built or sent — a host value's EncodeTLV runs
// on this goroutine — is logged at ERROR with the subscription id and the
// panic value's type, and the report is dropped. The process lives, the
// subscription stays and reports again on its next change, and the
// engine's bookkeeping is released: Stop returns. Red before the
// backstop: the test binary dies of the panic.
func TestReportSenderPanicDropsTheReportNotTheProcessW2B10(t *testing.T) {
	t.Parallel()
	logs := &lockedBufferW2B10{}
	logger := slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	m := subscription.NewManager(subscription.Config{}, nil, logger)

	var mu sync.Mutex
	calls := 0
	m.SetReportSender(func(_ context.Context, _ *subscription.Subscription, _ subscription.Report) subscription.ReportOutcome {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		if n == 1 {
			panic("host encoder left a container open")
		}
		return subscription.Sent
	})
	sub, err := m.Subscribe(defaultArgs())
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	ctx := context.Background()
	t0 := time.Now()

	m.OnAttributeChanged(mkPath(1, 0x0006, 0x0000))
	m.Tick(ctx, t0.Add(2*time.Second))

	line := logs.String()
	if !strings.Contains(line, "level=ERROR") ||
		!strings.Contains(line, "msg=im.subscription.report_panic") ||
		!strings.Contains(line, "subscription_id="+strconv.FormatUint(uint64(sub.ID), 10)) ||
		!strings.Contains(line, `panic="string: (redacted)"`) {
		t.Fatalf("log after the panic = %q, want one ERROR im.subscription.report_panic with subscription_id and panic", line)
	}
	if sub.IsClosed() {
		t.Fatal("the subscription was closed by the panic, want it kept")
	}

	m.OnAttributeChanged(mkPath(1, 0x0006, 0x0000))
	m.Tick(ctx, t0.Add(4*time.Second))
	mu.Lock()
	got := calls
	mu.Unlock()
	if got != 2 {
		t.Fatalf("sender calls = %d, want 2: the subscription reports again after the dropped report", got)
	}

	// The goroutine's wg / reporting flag were released: Stop returns.
	m.Stop()
}

// TestFlushSenderPanicDropsTheFlushNotTheProcessW2B10: the flush of a
// session's subscriptions runs the same sender on a goroutine of its own;
// a panic there is the same backstop's — logged at ERROR, the flush
// report dropped, the subscription kept and reporting on its next change,
// Stop returning. Red before the backstop: the test binary dies.
func TestFlushSenderPanicDropsTheFlushNotTheProcessW2B10(t *testing.T) {
	t.Parallel()
	logs := &lockedBufferW2B10{}
	logger := slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	m := subscription.NewManager(subscription.Config{}, nil, logger)

	var mu sync.Mutex
	var flushes, reports int
	m.SetReportSender(func(_ context.Context, _ *subscription.Subscription, r subscription.Report) subscription.ReportOutcome {
		mu.Lock()
		defer mu.Unlock()
		if r.Flush {
			flushes++
			panic("host encoder left a container open")
		}
		reports++
		return subscription.Sent
	})
	args := defaultArgs()
	sub, err := m.Subscribe(args)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	ctx := context.Background()

	m.OnAttributeChanged(mkPath(1, 0x0006, 0x0000))
	m.FlushSession(ctx, args.SessionID)

	line := logs.String()
	if !strings.Contains(line, "level=ERROR") ||
		!strings.Contains(line, "msg=im.subscription.report_panic") ||
		!strings.Contains(line, "subscription_id="+strconv.FormatUint(uint64(sub.ID), 10)) ||
		!strings.Contains(line, `panic="string: (redacted)"`) {
		t.Fatalf("log after the flush panic = %q, want one ERROR im.subscription.report_panic with subscription_id and panic", line)
	}
	if sub.IsClosed() {
		t.Fatal("the subscription was closed by the flush panic, want it kept")
	}

	m.OnAttributeChanged(mkPath(1, 0x0006, 0x0000))
	m.Tick(ctx, time.Now().Add(2*time.Second))
	mu.Lock()
	gotFlushes, gotReports := flushes, reports
	mu.Unlock()
	if gotFlushes != 1 || gotReports != 1 {
		t.Fatalf("flushes = %d, reports = %d; want 1 and 1: the subscription reports again after the dropped flush", gotFlushes, gotReports)
	}

	m.Stop()
}
