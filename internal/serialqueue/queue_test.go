// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package serialqueue_test

import (
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/internal/serialqueue"
)

// TestQueue: a stopped queue drops, a running one runs in order, Stop
// waits for the queued callbacks, and Start and Stop are idempotent.
func TestQueue(t *testing.T) {
	q := serialqueue.New()
	q.Enqueue(func() { t.Error("ran on a stopped queue") })
	q.Enqueue(nil)
	q.Stop() // not running: a no-op
	q.Start()
	q.Start() // running: a no-op
	var got []int
	for i := range 5 {
		q.Enqueue(func() { got = append(got, i) })
	}
	q.Stop()
	if !slices.Equal(got, []int{0, 1, 2, 3, 4}) {
		t.Errorf("ran %v", got)
	}
	q.Start()
	done := make(chan struct{})
	q.Enqueue(func() { close(done) })
	q.Stop()
	<-done
}
