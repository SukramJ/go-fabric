// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/internal/serialqueue"
)

// TestCallbackQueueWarnsThroughTheHostsLogger (review II, R2-P6-bound-0209):
// a callback the node's queue drops is reported on Config.Logger, not on
// slog.Default().
func TestCallbackQueueWarnsThroughTheHostsLogger(t *testing.T) {
	t.Parallel()
	var logged bytes.Buffer
	q := newCallbackQueue(clock.System{}, slog.New(slog.NewTextHandler(&logged, nil)))
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
	if !strings.Contains(logged.String(), "matter.callbacks.dropped") {
		t.Fatalf("the dropped callback did not reach the host's logger:\n%s", logged.String())
	}
}
