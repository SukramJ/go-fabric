// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/node"
)

// TestNewStartsNoGoroutine: New allocates and Start runs (W5-api-03, ADR
// 0026 principle 5) — a node built and dropped without Start leaves no
// goroutine behind; the subscription engine, the reapers and the subtype
// responder start in Start, which the second half checks by count.
//
//nolint:paralleltest // counts the process's goroutines
func TestNewStartsNoGoroutine(t *testing.T) {
	db := openDB(t, filepath.Join(t.TempDir(), "n.db"))
	if err := db.PingContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	settle := func() int {
		var n int
		for range 50 {
			runtime.Gosched()
			time.Sleep(2 * time.Millisecond)
			n = runtime.NumGoroutine()
		}
		return n
	}
	before := settle()
	n, err := node.New(context.Background(), minimalConfig(db))
	if err != nil {
		t.Fatal(err)
	}
	if after := settle(); after > before {
		t.Errorf("New started %d goroutines (%d before, %d after); want none before Start", after-before, before, after)
	}
	if err := n.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if started := settle(); started <= before {
		t.Errorf("Start left %d goroutines, %d before New; want the engine and reapers running", started, before)
	}
	if err := n.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}
