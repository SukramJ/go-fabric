// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package clocktest

import (
	"testing"
	"time"
)

// TestTimerStopInFireWindowLeavesNoValue holds clock.Timer's contract
// (clock/clock.go: after Stop or Reset returns, no stale value is
// received from C) at the instant Advance has taken a due timer off but
// has not yet sent: a Stop or Reset that lands there must not be
// followed by a value on C. R2-C2-test-0251.
func TestTimerStopInFireWindowLeavesNoValue(t *testing.T) {
	for _, tc := range []struct {
		name string
		act  func(tm *fakeTimer)
	}{
		{"Stop", func(tm *fakeTimer) { tm.Stop() }},
		{"Reset", func(tm *fakeTimer) { tm.Reset(time.Hour) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := New(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
			tm := f.NewTimer(time.Second).(*fakeTimer)
			testHookFired = func() { tc.act(tm) }
			t.Cleanup(func() { testHookFired = nil })

			f.Advance(time.Second)
			testHookFired = nil
			select {
			case v := <-tm.C():
				t.Fatalf("value %v received after %s returned", v, tc.name)
			default:
			}
		})
	}
}
