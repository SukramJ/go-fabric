// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"thingfabric.dev/go-fabric/internal/serialqueue"
)

// TestQueueWarnsThroughTheDevicesLogger (review II, R2-P6-bound-0209): a
// callback the device's queue drops is reported on the logger the device
// was given, not on slog.Default().
func TestQueueWarnsThroughTheDevicesLogger(t *testing.T) {
	t.Parallel()
	var logged bytes.Buffer
	q := newQueue(&deviceClock{}, slog.New(slog.NewTextHandler(&logged, nil)))
	overflowOnce(q)
	if !strings.Contains(logged.String(), "matter.callbacks.dropped") {
		t.Fatalf("the dropped callback did not reach the device's logger:\n%s", logged.String())
	}
}

// overflowOnce holds the queue's goroutine, queues one callback more than
// the bound and lets the queue drain.
func overflowOnce(q *serialqueue.Queue) {
	q.Start()
	release, running := make(chan struct{}), make(chan struct{})
	q.Enqueue(func() {
		close(running)
		<-release
	})
	<-running
	for range serialqueue.Bound + 1 {
		q.Enqueue(func() {})
	}
	close(release)
	q.Stop()
}
