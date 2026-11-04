// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"log/slog"
	"runtime"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/fabrictest"
)

// watchers counts the goroutines parked in GeneralCommissioning's
// fail-safe expiry watcher.
func watchers() int {
	buf := make([]byte, 1<<22)
	buf = buf[:runtime.Stack(buf, true)]
	return strings.Count(string(buf), "watchFailSafeExpiry")
}

// TestClosedSiteLeavesNoFailSafeWatcher: the fail-safe a PASE session
// arms on its own runs under the node's lifetime, so a commissioned
// site's Close ends its watcher — the fake clock never fires it again
// (W5-ex-06).
//
//nolint:paralleltest // counts the process's goroutines
func TestClosedSiteLeavesNoFailSafeWatcher(t *testing.T) {
	before := watchers()
	site := fabrictest.NewSite(t, fabrictest.Options{Logger: slog.New(slog.DiscardHandler)})
	site.CommissionedPair(fabrictest.PairOptions{})
	site.Close()
	// The watcher's goroutine leaves after Close returns; the process's
	// goroutine count is the subject, which no site or bubble paces.
	waitLive(t, "no fail-safe watcher after the site closed", func() bool { return watchers() <= before })
}
