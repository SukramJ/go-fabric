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

// sqlCallWatchers are the creators of the goroutines database/sql and the
// SQLite driver start for one call and that end with it — they watch the
// call's context to abort it and return once the call is over. They
// inherit the label of the goroutine that made the call, and one can still
// be live, or not yet have run at all, just after that call returned: it
// is the end of a call New made, not a goroutine New left running. A full
// run of the package counted two such (review II, phase 1 rework): a
// modernc.org/sqlite.interruptOnDone.func1 and one that had not run yet,
// which the debug=1 profile showed without a frame. Each ends with its
// call, at the versions go.mod pins:
//   - modernc.org/sqlite.interruptOnDone (sqlite.go, v1.61.1): its
//     goroutine selects on the call's context and on donech, which the
//     function it returns — deferred by every caller — closes;
//   - database/sql.(*DB).beginDC (go tx.awaitDone()): awaitDone waits for
//     the transaction's context, which Commit and Rollback cancel;
//   - database/sql.(*Rows).initContextClose (go rs.awaitDone(…)): it
//     waits for closectx, which Rows.Close cancels.
//
// A watcher of a transaction or rows New left open would be left out as
// well: that is a held pool connection, not a goroutine New starts, and
// this test does not look for it.
var sqlCallWatchers = []string{
	"created by modernc.org/sqlite.interruptOnDone ",
	"created by database/sql.(*DB).beginDC ",
	"created by database/sql.(*Rows).initContextClose ",
}

// labelledGoroutines is the number of live goroutines whose label
// goroutineLabel is value, read from the goroutine profile (debug=2: one
// block per goroutine, a "goroutine <id> [<state>] {<labels>}:" header,
// its frames — a not yet started goroutine shows its function too — and a
// "created by <function> in goroutine <id>" line). Left out are a
// goroutine whose every frame is the runtime's — the runtime starts its GC
// mark workers from whichever goroutine begins the first collection, and
// they inherit that goroutine's labels — and the per-call watchers of
// database/sql and the SQLite driver (sqlCallWatchers). It returns the
// blocks it counted too, for a failure to show.
func labelledGoroutines(t *testing.T, value string) (total int, counted []string) {
	t.Helper()
	var buf bytes.Buffer
	if err := pprof.Lookup("goroutine").WriteTo(&buf, 2); err != nil {
		t.Fatal(err)
	}
	// The header prints a key or value quoted when it is not a plain word
	// ({"gofabric-node-test": TestNewStartsNoGoroutine}).
	wants := make([]string, 0, 16)
	for _, k := range []string{goroutineLabel, strconv.Quote(goroutineLabel)} {
		for _, v := range []string{value, strconv.Quote(value)} {
			wants = append(wants, "{"+k+": "+v+"}", "{"+k+": "+v+", ", ", "+k+": "+v+"}", ", "+k+": "+v+", ")
		}
	}
	for _, block := range strings.Split(buf.String(), "\n\n") {
		header, _, _ := strings.Cut(block, "\n")
		if !strings.HasPrefix(header, "goroutine ") || !containsAny(header, wants) {
			continue
		}
		if runtimeOnly(block) || sqlCallWatcher(block) {
			continue
		}
		total++
		counted = append(counted, block)
	}
	return total, counted
}

// containsAny reports whether s contains one of subs.
func containsAny(s string, subs []string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// runtimeOnly reports whether every frame of a goroutine's block is in
// package runtime.
func runtimeOnly(block string) bool {
	frames := 0
	for _, line := range strings.Split(block, "\n")[1:] {
		if line == "" || strings.HasPrefix(line, "\t") || strings.HasPrefix(line, "created by ") {
			continue
		}
		frames++
		if !strings.HasPrefix(line, "runtime.") {
			return false
		}
	}
	return frames > 0
}

// sqlCallWatcher reports whether a goroutine's block is one of
// sqlCallWatchers by the function that created it.
func sqlCallWatcher(block string) bool {
	for _, line := range strings.Split(block, "\n") {
		for _, creator := range sqlCallWatchers {
			if strings.HasPrefix(line, creator) {
				return true
			}
		}
	}
	return false
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
		before, beforeRecords := labelledGoroutines(t, value)
		if before < 1 {
			t.Fatalf("the goroutine profile names %d goroutines labelled %q, want the test's own", before, value)
		}
		n, err := node.New(ctx, minimalConfig(db))
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = n.Stop(context.Background()) }()
		if after, afterRecords := labelledGoroutines(t, value); after != before {
			t.Errorf("New started %d goroutines (%d before, %d after); want none before Start\nbefore:\n%s\nafter:\n%s",
				after-before, before, after, strings.Join(beforeRecords, "\n\n"), strings.Join(afterRecords, "\n\n"))
		}
		if err := n.Start(ctx); err != nil {
			t.Fatal(err)
		}
		if started, _ := labelledGoroutines(t, value); started <= before {
			t.Errorf("Start left %d goroutines, %d before New; want the engine and reapers running", started, before)
		}
	})
}
