// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"strings"
	"testing"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/diagevent"
)

// The DiagnosticLogs provider serves the diagnostic event ring for the
// support and network intents, one line per event, and no crash log.
func TestRingLogs(t *testing.T) {
	ring := diagevent.NewRing(4)
	logs := ringLogs{ring}
	if b, err := logs.Logs(context.Background(), mattercore.IntentEndUserSupport); err != nil || !strings.Contains(string(b), "no diagnostic events") {
		t.Errorf("empty ring: %q, %v", b, err)
	}
	ring.Record(diagevent.Event{Message: "a commissioner opened PASE", Detail: map[string]string{"z": "1", "a": "2"}})
	b, err := logs.Logs(context.Background(), mattercore.IntentNetworkDiag)
	if err != nil || !strings.Contains(string(b), "a commissioner opened PASE a=2 z=1") {
		t.Errorf("one event: %q, %v", b, err)
	}
	if b, err := logs.Logs(context.Background(), mattercore.IntentCrashLogs); err != nil || b != nil {
		t.Errorf("crash logs: %q, %v, want none", b, err)
	}
}
