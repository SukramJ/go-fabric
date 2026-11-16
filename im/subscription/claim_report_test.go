// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package subscription

import (
	"context"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/im"
)

// TestClaimReportWakesWhenTheReportEnds (review II, R2-P6-bound-0517): a
// flush waiting for the report in flight takes the flag when that report
// ends and gives up when the subscription closes — woken by the event, not
// by a timer on the manager's clock, which a fake clock never fires here.
func TestClaimReportWakesWhenTheReportEnds(t *testing.T) {
	t.Parallel()
	m := NewManager(Config{Clock: clocktest.New(time.Unix(1_700_000_000, 0))}, nil, nil)
	subscribe := func() *Subscription {
		sub, err := m.Subscribe(SubscribeArgs{FabricIndex: 1, PeerNodeID: 1, SessionID: 1, MaxIntervalCeiling: 60, AttributePaths: []im.ConcreteAttributePath{{}}})
		if err != nil {
			t.Fatal(err)
		}
		sub.reporting.Store(true) // a report is in flight
		return sub
	}
	// claim starts the flush's claim and returns once it waits on the
	// report's end.
	claim := func(sub *Subscription) <-chan bool {
		got := make(chan bool, 1)
		go func() { got <- m.claimReport(context.Background(), sub) }()
		deadline := time.Now().Add(2 * time.Second)
		for {
			sub.mu.Lock()
			waiting := sub.reportEnded != nil
			sub.mu.Unlock()
			if waiting {
				return got
			}
			if time.Now().After(deadline) {
				t.Fatal("the flush does not wait on the report's end")
			}
			time.Sleep(time.Millisecond)
		}
	}
	wait := func(got <-chan bool, what string) bool {
		select {
		case ok := <-got:
			return ok
		case <-time.After(2 * time.Second):
			t.Fatalf("the flush still waits 2 s after %s", what)
			return false
		}
	}

	ending := subscribe()
	got := claim(ending)
	ending.endReport()
	if !wait(got, "the report ended") {
		t.Fatal("the flush did not take the flag the ended report cleared")
	}

	closing := subscribe()
	got = claim(closing)
	if err := m.Close(closing.ID); err != nil {
		t.Fatal(err)
	}
	if wait(got, "the subscription closed") {
		t.Fatal("the flush took the flag of a closed subscription")
	}
}
