// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"thingfabric.dev/go-fabric/diagevent"
	"thingfabric.dev/go-fabric/mdns"
)

// TestAsyncDiagnosticCallbackPanicIsContained (review II, R2-P6-err-2410):
// a panicking OnDiagnosticEventAsync callback runs through queued, which
// logs the panic: the callback goroutine lives on, the next callback
// runs, and Stop returns.
func TestAsyncDiagnosticCallbackPanicIsContained(t *testing.T) {
	t.Parallel()
	n, _, _ := commissionedKeeperNode(t, mdns.NewNoop())
	ran := make(chan struct{}, 1)
	calls := 0
	n.OnDiagnosticEventAsync(func(DiagnosticEvent) {
		calls++
		if calls == 1 {
			panic("host callback")
		}
		ran <- struct{}{}
	})
	n.diagRing.Record(diagevent.Event{})
	n.diagRing.Record(diagevent.Event{})
	<-ran
	if err := n.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
}

// TestEveryQueuedHostCallbackIsContained (review II, R2-P6-err-2410): the
// node hands a host callback to its callback queue only through queued
// (listeners.go), which contains a panic; a bare Enqueue elsewhere in the
// package would let a host's panic end the process.
func TestEveryQueuedHostCallbackIsContained(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || f == "listeners.go" {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(src), "\n") {
			if strings.Contains(line, ".Enqueue(") {
				t.Errorf("%s:%d enqueues on the callback queue without queued: %s", f, i+1, strings.TrimSpace(line))
			}
		}
	}
}
