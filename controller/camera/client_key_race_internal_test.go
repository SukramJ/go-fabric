// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"sync"
	"testing"
)

// TestConcurrentStartsWithOneClientSessionKey (review II, R2-P1-cam-a-01,
// R2-P1-cam-b-03): concurrent starts of one connection under one
// client_session_key open one session; the others are refused. The check
// and the registration are one step under the registry's lock. Before,
// they were two, and two starts could both pass the check and both be
// tracked under the key (measured in 1 of 2000 rounds of two starts).
// The race needs no fixed interleaving to fail: every round that lets two
// starts through is a failure, so the rounds and the starts per round are
// what make it likely to show.
func TestConcurrentStartsWithOneClientSessionKey(t *testing.T) {
	t.Parallel()
	const rounds, starts = 2000, 8
	for i := range rounds {
		m, _, _ := managerWith(fixtureState(), sliceBNumbering())
		var wg sync.WaitGroup
		gate := make(chan struct{})
		errs := make([]error, starts)
		for j := range starts {
			wg.Go(func() {
				<-gate
				_, errs[j] = sliceBStartKeyed(sliceBCtx, m, "conn-1", "k")
			})
		}
		close(gate)
		wg.Wait()
		ok := 0
		for _, err := range errs {
			if err == nil {
				ok++
			}
		}
		keyed := 0
		for _, s := range m.sessions.trackedSessions() {
			if s.scope().clientSessionKey == "k" {
				keyed++
			}
		}
		if ok != 1 || keyed != 1 {
			t.Fatalf("round %d: %d of %d starts succeeded, %d tracked sessions keyed k; want 1 and 1 (errors %v)", i, ok, starts, keyed, errs)
		}
	}
}
