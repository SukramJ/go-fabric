// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package im

import (
	"context"
	"testing"
)

// TestAfterResponse pins the deferral contract: without an AfterResponse
// nothing is registered, with one the work runs once, in order, on Run.
func TestAfterResponse(t *testing.T) {
	t.Parallel()
	if DeferAfterResponse(context.Background(), func() {}) {
		t.Fatal("registered without an AfterResponse")
	}
	a := &AfterResponse{}
	ctx := WithAfterResponse(context.Background(), a)
	var got []int
	for i := range 3 {
		if !DeferAfterResponse(ctx, func() { got = append(got, i) }) {
			t.Fatal("not registered")
		}
	}
	if len(got) != 0 {
		t.Fatal("deferred work ran before Run")
	}
	a.Run()
	a.Run()
	if len(got) != 3 || got[0] != 0 || got[2] != 2 {
		t.Fatalf("ran %v, want [0 1 2] once", got)
	}
}
