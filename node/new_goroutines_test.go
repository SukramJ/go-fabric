// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"bytes"
	"context"
	"path/filepath"
	"runtime/pprof"
	"strconv"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/node"
)

// goroutineLabel is the profiler label key TestNewStartsNoGoroutine marks
// its own goroutine with: every goroutine it starts, and each one those
// start, inherits the label, so the goroutine profile names exactly the
// test's goroutines — none of another test's that is still winding down.
const goroutineLabel = "gofabric-node-test"

// labelledGoroutines is the number of live goroutines whose label
// goroutineLabel is value, read from the goroutine profile (debug=1: one
// record per stack and label set, its count first, its labels on a
// "# labels:" line, then one "#\t<pc>\t<function>+<off>\t<file>" line
// per frame). A goroutine whose every frame is the runtime's is left out:
// the runtime starts its GC mark workers from whichever goroutine begins
// the first collection, and they inherit that goroutine's labels.
func labelledGoroutines(t *testing.T, value string) int {
	t.Helper()
	var buf bytes.Buffer
	if err := pprof.Lookup("goroutine").WriteTo(&buf, 1); err != nil {
		t.Fatal(err)
	}
	want := strconv.Quote(goroutineLabel) + ":" + strconv.Quote(value)
	total := 0
	for _, record := range strings.Split(buf.String(), "\n\n") {
		if !strings.Contains(record, "# labels: ") || !strings.Contains(record, want) || runtimeOnly(record) {
			continue
		}
		count, _, ok := strings.Cut(record, " @ ")
		if !ok {
			t.Fatalf("goroutine profile record without a count: %q", record)
		}
		n, err := strconv.Atoi(strings.TrimSpace(count[strings.LastIndex(count, "\n")+1:]))
		if err != nil {
			t.Fatalf("goroutine profile record count %q: %v", count, err)
		}
		total += n
	}
	return total
}

// runtimeOnly reports whether every frame of a goroutine profile record
// is in package runtime.
func runtimeOnly(record string) bool {
	frames := 0
	for _, line := range strings.Split(record, "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) < 3 || fields[0] != "#" {
			continue
		}
		frames++
		if !strings.HasPrefix(fields[2], "runtime.") {
			return false
		}
	}
	return frames > 0
}

// TestNewStartsNoGoroutine: New allocates and Start runs (W5-api-03, ADR
// 0026 principle 5) — a node built without Start has started no
// goroutine; the subscription engine, the reapers and the subtype
// responder start in Start, which the second half checks. The goroutines
// counted are the test's own and their descendants (a profiler label they
// inherit), so another test's goroutines do not enter the count, and a
// goroutine is counted from its creation on: nothing is waited for.
func TestNewStartsNoGoroutine(t *testing.T) {
	t.Parallel()
	db := openDB(t, filepath.Join(t.TempDir(), "n.db"))
	if err := db.PingContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	value := t.Name()
	pprof.Do(t.Context(), pprof.Labels(goroutineLabel, value), func(ctx context.Context) {
		before := labelledGoroutines(t, value)
		if before < 1 {
			t.Fatalf("the goroutine profile names %d goroutines labelled %q, want the test's own", before, value)
		}
		n, err := node.New(ctx, minimalConfig(db))
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = n.Stop(context.Background()) }()
		if after := labelledGoroutines(t, value); after != before {
			t.Errorf("New started %d goroutines (%d before, %d after); want none before Start", after-before, before, after)
		}
		if err := n.Start(ctx); err != nil {
			t.Fatal(err)
		}
		if started := labelledGoroutines(t, value); started <= before {
			t.Errorf("Start left %d goroutines, %d before New; want the engine and reapers running", started, before)
		}
	})
}
