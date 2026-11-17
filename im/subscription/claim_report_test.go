// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package subscription

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/im"
)

// TestClaimReportWakesWhenTheReportEnds (review II, R2-P6-bound-0517): a
// flush waiting for the report in flight takes the flag when that report
// ends and gives up when the subscription closes — woken by the event, not
// by a timer on the manager's clock, which the fake clock here never
// fires.
func TestClaimReportWakesWhenTheReportEnds(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		m := newTestManager(Config{Clock: clocktest.New(time.Unix(1_700_000_000, 0))})
		subscribe := func() *Subscription {
			sub, err := m.Subscribe(SubscribeArgs{FabricIndex: 1, PeerNodeID: 1, SessionID: 1, MaxIntervalCeiling: 60, AttributePaths: []im.ConcreteAttributePath{{}}})
			if err != nil {
				t.Fatal(err)
			}
			sub.reporting.Store(true) // a report is in flight
			return sub
		}
		// claim starts the flush's claim and returns once it is blocked.
		claim := func(sub *Subscription) <-chan bool {
			got := make(chan bool, 1)
			go func() { ok, _ := m.claimReport(context.Background(), sub); got <- ok }()
			synctest.Wait()
			select {
			case <-got:
				t.Fatal("the flush took the flag of a report in flight")
			default:
			}
			return got
		}
		result := func(got <-chan bool, what string) bool {
			synctest.Wait()
			select {
			case ok := <-got:
				return ok
			default:
				t.Fatalf("the flush still waits after %s", what)
				return false
			}
		}

		ending := subscribe()
		got := claim(ending)
		ending.endReport()
		if !result(got, "the report ended") {
			t.Fatal("the flush did not take the flag the ended report cleared")
		}

		closing := subscribe()
		got = claim(closing)
		if err := m.Close(closing.ID); err != nil {
			t.Fatal(err)
		}
		if result(got, "the subscription closed") {
			t.Fatal("the flush took the flag of a closed subscription")
		}
	})
}
