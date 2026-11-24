// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"runtime"
	"strings"
	"testing"
)

// WaitParkedOnMutexA1 waits until a goroutine whose stack names fn is
// parked on a sync.Mutex — it waits for a lock another goroutine holds —
// and reports true, or until done is closed first and reports false.
//
// It is the observation point of the check-then-write tests of review II
// C5 (#595): a second command that the fix makes wait for the first one's
// lock parks there, and one that runs past the check without the lock
// finishes. Either outcome ends the wait, so the test decides on what
// happened, not on a timeout. The goroutine dump's header names a parked
// waiter "[sync.Mutex.Lock"; a sync.RWMutex waiter is named
// differently and does not count.
func WaitParkedOnMutexA1(t testing.TB, done <-chan struct{}, fn string) bool {
	t.Helper()
	buf := make([]byte, 1<<20)
	for {
		select {
		case <-done:
			return false
		default:
		}
		n := runtime.Stack(buf, true)
		if n == len(buf) {
			buf = make([]byte, 2*len(buf))
			continue
		}
		for _, g := range strings.Split(string(buf[:n]), "\n\n") {
			header, _, _ := strings.Cut(g, "\n")
			if strings.Contains(header, "[sync.Mutex.Lock") && strings.Contains(g, fn) {
				return true
			}
		}
		runtime.Gosched()
	}
}
