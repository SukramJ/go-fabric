// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"runtime"
	"strings"
	"testing"
)

// goroutineHeaderA2 is the calling goroutine's stack header, "goroutine N ".
func goroutineHeaderA2() string {
	buf := make([]byte, 64)
	buf = buf[:runtime.Stack(buf, false)]
	head, _, _ := strings.Cut(string(buf), "[")
	return head
}

// waitLockedOrDoneA2 yields until the goroutine named by header waits for
// a mutex inside fn — read from the goroutines' stacks, no wall-clock
// wait — or until done is closed (the goroutine got through without
// waiting).
func waitLockedOrDoneA2(t *testing.T, header, fn string, done <-chan struct{}) {
	t.Helper()
	waitParkedOrDoneA2(t, header, fn, "sync.(*Mutex).Lock", done)
}

// waitParkedOrDoneA2 yields until the goroutine named by header waits in
// frame inside fn, or until done is closed.
func waitParkedOrDoneA2(t *testing.T, header, fn, frame string, done <-chan struct{}) {
	t.Helper()
	buf := make([]byte, 1<<20)
	for range 1_000_000 {
		select {
		case <-done:
			return
		default:
		}
		stacks := string(buf[:runtime.Stack(buf, true)])
		for _, g := range strings.Split(stacks, "\n\n") {
			if strings.HasPrefix(g, header+"[") && strings.Contains(g, frame) && strings.Contains(g, fn) {
				return
			}
		}
		runtime.Gosched()
	}
	t.Fatalf("%sneither waits in %s inside %s nor finished", header, frame, fn)
}
