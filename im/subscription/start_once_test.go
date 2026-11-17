// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package subscription_test

import (
	"runtime"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/SukramJ/go-fabric/im/subscription"
)

// TestStart_SecondCallStartsNoSecondEngine pins the "Idempotent" the
// doc comment on Start promised: a second Start must not launch a
// second engine goroutine, which would double every report tick and
// heartbeat for the Manager's lifetime. The count is taken once every
// goroutine of the bubble is blocked, so a second engine scheduled late
// is counted too (review II, R2-P5-tassert-12: the poll before returned
// at the first count of one). Not parallel: the count is the process's.
func TestStart_SecondCallStartsNoSecondEngine(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := newManager(subscription.Config{TickInterval: time.Hour}, nil)
		t.Cleanup(m.Stop)
		m.Start(t.Context())
		m.Start(t.Context())
		synctest.Wait()
		if n := engineGoroutines(); n != 1 {
			t.Fatalf("%d engine goroutines running after two Start calls, want 1", n)
		}
	})
}

// engineGoroutines counts goroutines parked inside (*Manager).run.
func engineGoroutines() int {
	buf := make([]byte, 1<<20)
	n := runtime.Stack(buf, true)
	return strings.Count(string(buf[:n]), "subscription.(*Manager).run(")
}
