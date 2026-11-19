// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest

import (
	"context"
	"testing"
	"time"
)

// slotTB is a test of the given name whose cleanups run on end.
type slotTB struct {
	testing.TB
	name     string
	cleanups []func()
}

func (s *slotTB) Name() string        { return s.name }
func (s *slotTB) Helper()             {}
func (s *slotTB) Logf(string, ...any) {}
func (s *slotTB) Cleanup(fn func())   { s.cleanups = append(s.cleanups, fn) }

func (s *slotTB) end() {
	for i := len(s.cleanups) - 1; i >= 0; i-- {
		s.cleanups[i]()
	}
}

// TestLoopbackSlotRunsOneTreeAtATime: the slot's holder and its subtests go
// ahead; a sibling, and a test whose name only starts with the holder's,
// wait until the holder ends. Not parallel, so no loopback site of the
// package's parallel tests holds the slot meanwhile.
func TestLoopbackSlotRunsOneTreeAtATime(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	holder := &slotTB{TB: t, name: "TestX/a"}
	holdLoopbackSlot(holder)
	holdLoopbackSlot(&slotTB{TB: t, name: "TestX/a/sub"}) // in the tree: returns at once

	waiting := map[string]chan struct{}{}
	others := []*slotTB{{TB: t, name: "TestX/b"}, {TB: t, name: "TestX/ab"}}
	for _, o := range others {
		done := make(chan struct{})
		waiting[o.name] = done
		go func() {
			holdLoopbackSlot(o)
			// Released before done closes: the check after the loop
			// reads the slot once every done is closed.
			o.end()
			close(done)
		}()
	}
	for name, done := range waiting {
		select {
		case <-done:
			t.Fatalf("%s took the slot %s holds", name, holder.name)
		default:
		}
	}
	holder.end()
	for name, done := range waiting {
		select {
		case <-done:
		case <-ctx.Done():
			t.Fatalf("%s did not take the slot after its holder ended", name)
		}
	}
	loopbackSlot.mu.Lock()
	defer loopbackSlot.mu.Unlock()
	if loopbackSlot.holder != "" {
		t.Fatalf("the slot is held by %q after every holder ended", loopbackSlot.holder)
	}
}
