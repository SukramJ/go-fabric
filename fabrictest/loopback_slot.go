// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest

import (
	"strings"
	"sync"
	"testing"
)

// loopbackSlot runs the process's loopback sites on a fake clock one test
// tree at a time. Such a site cannot see its sockets' work, so it judges
// quiet by the process's goroutines (busyHolds): a working goroutine holds
// each clock step for up to holdBudget. Two of them in parallel read each
// other's work as their own and hold every step for it — the clock crawls
// (a subscription's 60 s keep-alive not reached within a two-minute wait)
// — and past holdBudget either one moves on and may overtake a handshake
// of its own a sibling starved (a 35 s CASE timeout fired). One at a time,
// with no other loopback site's work in the process, the check reads this
// site's work. The slot belongs to the test that opened the first site and
// to its subtests: a test that builds several sites, or a parent whose
// subtests build their own, takes it once — as the node package's test
// helper holdSite serialises that package's sites, for the same reason.
var loopbackSlot = struct {
	mu     sync.Mutex
	free   *sync.Cond
	holder string
}{}

func init() { loopbackSlot.free = sync.NewCond(&loopbackSlot.mu) }

// holdLoopbackSlot takes the slot until tb ends, waiting while a test
// outside tb's tree holds it; a test inside the holder's tree goes ahead.
// The caller registers its site's Close after this, so the slot is
// released only once the site closed (cleanups run last-registered first).
func holdLoopbackSlot(tb testing.TB) {
	tb.Helper()
	name := tb.Name()
	inTree := func() bool {
		h := loopbackSlot.holder
		return name == h || strings.HasPrefix(name, h+"/")
	}
	loopbackSlot.mu.Lock()
	defer loopbackSlot.mu.Unlock()
	if loopbackSlot.holder != "" && !inTree() {
		tb.Logf("fabrictest: this loopback site waits for %s's: a loopback site on a fake clock runs alone among the process's loopback sites", loopbackSlot.holder)
	}
	for loopbackSlot.holder != "" && !inTree() {
		loopbackSlot.free.Wait()
	}
	if loopbackSlot.holder != "" {
		return
	}
	loopbackSlot.holder = name
	tb.Cleanup(func() {
		loopbackSlot.mu.Lock()
		loopbackSlot.holder = ""
		loopbackSlot.mu.Unlock()
		loopbackSlot.free.Broadcast()
	})
}
