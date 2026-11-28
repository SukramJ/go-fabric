// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package subscription_test

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/im/subscription"
)

// reportPanicLogP10 runs one report whose sender panics with what panics
// and returns the log at level.
func reportPanicLogP10(t *testing.T, level slog.Level, panics func()) string {
	t.Helper()
	logs := &lockedBufferW2B10{}
	logger := slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: level}))
	m := subscription.NewManager(subscription.Config{}, nil, logger)
	m.SetReportSender(func(context.Context, *subscription.Subscription, subscription.Report) subscription.ReportOutcome {
		panics()
		return subscription.Sent
	})
	if _, err := m.Subscribe(defaultArgs()); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	m.OnAttributeChanged(mkPath(1, 0x0006, 0x0000))
	m.Tick(context.Background(), time.Now().Add(2*time.Second))
	m.Stop()
	return logs.String()
}

// TestReportPanicLogWithholdsTheValueP10 (P10-sec-6403): the report
// backstop logs a panic's type and, for a runtime error, its bounded
// message at ERROR — any other value is withheld — and the stack only at
// DEBUG. Red at d1ab4e970: the ERROR record carried the panic value and
// the stack.
func TestReportPanicLogWithholdsTheValueP10(t *testing.T) {
	t.Parallel()
	const secret = "host secret in the panic value"
	index := 3
	for name, tc := range map[string]struct {
		panics func()
		want   string
	}{
		"string": {func() { panic(secret) }, `panic="string: (redacted)"`},
		"runtime error": {
			func() { _ = []int{}[index] },
			`panic="runtime.boundsError: runtime error: index out of range [3] with length 0"`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			line := reportPanicLogP10(t, slog.LevelError, tc.panics)
			if !strings.Contains(line, "level=ERROR msg=im.subscription.report_panic") ||
				!strings.Contains(line, tc.want) {
				t.Fatalf("ERROR log = %q, want %s", line, tc.want)
			}
			if strings.Contains(line, secret) || strings.Contains(line, "stack=") {
				t.Fatalf("ERROR log = %q, want neither the panic value nor the stack", line)
			}
			debug := reportPanicLogP10(t, slog.LevelDebug, tc.panics)
			if !strings.Contains(debug, "level=DEBUG msg=im.subscription.report_panic") ||
				!strings.Contains(debug, "stack=") {
				t.Fatalf("DEBUG log = %q, want a DEBUG im.subscription.report_panic with the stack", debug)
			}
		})
	}
}
