// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package discovery

import (
	"context"
	"fmt"
	"testing"

	"github.com/miekg/dns"
)

// TestScanner_ContinuousSearchRetainsBoundedState: a search over DNS-SD
// that a sender feeds ever-new matching instances keeps at most
// maxSeenInstances instance ids in its waiter, however many it delivers
// (review II, R3-bound-3552).
func TestScanner_ContinuousSearchRetainsBoundedState(t *testing.T) {
	t.Parallel()
	n, conn, _ := testNames(t)
	s := NewScanner(n)
	defer s.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var got collector
	done := make(chan error, 1)
	go func() { done <- IPTransport(s).FindContinuously(ctx, ByLongDiscriminator(3840), got.add) }()
	waitFor(t, func() bool { return len(conn.queries(t)) >= 1 })
	const total = maxSeenInstances + 64
	const batch = 64
	for i := 0; i < total; i += batch {
		var answers, extra []dns.RR
		for j := i; j < min(i+batch, total); j++ {
			a, e := announce(fmt.Sprintf("%016X", j+1), fmt.Sprintf("H%015X.local", j+1), "3840", "1", "fd00::10")
			answers, extra = append(answers, a...), append(extra, e...)
		}
		inject(t, conn, 7, answers, extra...)
		waitFor(t, func() bool { return got.len() >= min(i+batch, total) })
	}
	var seen int
	onLoop(t, n, func() {
		for _, w := range s.waiters {
			seen = max(seen, w.seen.len())
		}
	})
	if seen > maxSeenInstances {
		t.Fatalf("the waiter holds %d instance ids after %d were delivered, want at most %d", seen, total, maxSeenInstances)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// TestScanner_FindWithoutKeepReturnsNothing: the transport's search does
// not accumulate what it delivers (review II, R3-bound-3552).
func TestScanner_FindWithoutKeepReturnsNothing(t *testing.T) {
	t.Parallel()
	n, conn, _ := testNames(t)
	s := NewScanner(n)
	defer s.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var got collector
	type res struct {
		devices []Device
		err     error
	}
	done := make(chan res, 1)
	go func() {
		d, err := s.find(ctx, ByLongDiscriminator(3840), got.add, false)
		done <- res{d, err}
	}()
	waitFor(t, func() bool { return len(conn.queries(t)) >= 1 })
	answers, extra := announce(cInst, cHost, "3840", "1", "fd00::10")
	inject(t, conn, 7, answers, extra...)
	waitFor(t, func() bool { return got.len() == 1 })
	cancel()
	if r := <-done; r.err != nil || len(r.devices) != 0 {
		t.Fatalf("result = %d devices, %v; want none kept", len(r.devices), r.err)
	}
}
