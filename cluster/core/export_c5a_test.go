// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"
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
//
// The wait is bounded: when neither happened within ten seconds — the
// goroutine blocks somewhere else, an RWMutex or a channel — the test
// fails naming fn rather than hanging to the go test timeout. The
// goroutine dump, which stops the world, is taken every 64th yield, not
// on each (PR 1 review, Opus L3, decision 684).
func WaitParkedOnMutexA1(tb testing.TB, done <-chan struct{}, fn string) bool {
	tb.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	buf := make([]byte, 1<<20)
	for turn := 0; ; turn++ {
		select {
		case <-done:
			return false
		default:
		}
		if ctx.Err() != nil {
			tb.Fatalf("no goroutine in %s parked on a sync.Mutex, and done not closed, within 10s", fn)
		}
		if turn%64 != 0 {
			runtime.Gosched()
			continue
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
