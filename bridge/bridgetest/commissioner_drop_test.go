// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridgetest

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"thingfabric.dev/go-fabric/client/exchange"
)

// logRecorder is a testing.TB that keeps what is logged through it.
type logRecorder struct {
	testing.TB
	mu   sync.Mutex
	logs []string
}

func (r *logRecorder) Logf(format string, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.logs = append(r.logs, fmt.Sprintf(format, args...))
}

// TestCommissionerLogsWhatItDrops: a session the device opened beyond the
// unconsumed ones Accept holds, and a report beyond the unconsumed ones of
// its session, are dropped with a log line — before, both went silently
// and the test waited out Accept's or NextReport's timeout with no hint
// (review II, R2-P6-test-0613).
func TestCommissionerLogsWhatItDrops(t *testing.T) {
	t.Parallel()
	rec := &logRecorder{TB: t}
	s := &exchange.Session{}
	c := &Commissioner{
		tb:       rec,
		accepted: make(chan *Session),
		sessions: map[*exchange.Session]*Session{},
	}
	c.sessions[s] = &Session{c: c, s: s, reports: make(chan *Report)}
	c.onSession(s, 0)
	c.route(s, &Report{SubscriptionID: 7})
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.logs) != 2 {
		t.Fatalf("log lines = %q, want one per drop", rec.logs)
	}
	if !strings.Contains(rec.logs[0], "session") || !strings.Contains(rec.logs[1], "subscription 7") {
		t.Errorf("log lines %q do not name what was dropped", rec.logs)
	}
}
