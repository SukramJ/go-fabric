// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package subscription_test

import (
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/im/subscription"
)

// TestStart_SecondCallStartsNoSecondEngine pins the "Idempotent" the
// doc comment on Start promised: a second Start must not launch a
// second engine goroutine, which would double every report tick and
// heartbeat for the Manager's lifetime.
func TestStart_SecondCallStartsNoSecondEngine(t *testing.T) {
	t.Parallel()
	m := subscription.NewManager(subscription.Config{TickInterval: time.Hour}, nil, nil)
	ctx := t.Context()
	m.Start(ctx)
	m.Start(ctx)
	t.Cleanup(m.Stop)

	deadline := time.Now().Add(2 * time.Second)
	for {
		if n := engineGoroutines(); n == 1 {
			return
		} else if time.Now().After(deadline) {
			t.Fatalf("%d engine goroutines running after two Start calls, want 1", n)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// engineGoroutines counts goroutines parked inside (*Manager).run.
func engineGoroutines() int {
	buf := make([]byte, 1<<20)
	n := runtime.Stack(buf, true)
	return strings.Count(string(buf[:n]), "subscription.(*Manager).run(")
}
