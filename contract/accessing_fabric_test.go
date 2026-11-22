// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package contract_test

import (
	"context"
	"testing"

	"thingfabric.dev/go-fabric/contract"
)

// TestAccessingFabric: a context without a request reports ok false — a
// local call — and a stamped one returns the fabric and the flag, with
// fabric 0 (a session that has no fabric yet) still ok.
func TestAccessingFabric(t *testing.T) {
	t.Parallel()
	if idx, filtered, ok := contract.AccessingFabric(context.Background()); ok || idx != 0 || filtered {
		t.Errorf("unstamped: (%d, %t, %t), want (0, false, false)", idx, filtered, ok)
	}
	for _, c := range []struct {
		idx      uint8
		filtered bool
	}{{0, false}, {0, true}, {3, true}, {254, false}} {
		ctx := contract.WithAccessingFabric(context.Background(), c.idx, c.filtered)
		idx, filtered, ok := contract.AccessingFabric(ctx)
		if !ok || idx != c.idx || filtered != c.filtered {
			t.Errorf("stamped (%d, %t): got (%d, %t, %t)", c.idx, c.filtered, idx, filtered, ok)
		}
	}
}

// TestEligibilityStateString: every state has its own stable token, and a
// value outside the set reads as unknown.
func TestEligibilityStateString(t *testing.T) {
	t.Parallel()
	for s, want := range map[contract.EligibilityState]string{
		contract.EligibilityMappable:   "mappable",
		contract.EligibilityPartial:    "partially_mappable",
		contract.EligibilityUnmappable: "unmappable",
		contract.EligibilityState(99):  "unknown",
	} {
		if got := s.String(); got != want {
			t.Errorf("%d.String() = %q, want %q", s, got, want)
		}
	}
}

// TestMetricNames: the named set lists each name once, none empty.
func TestMetricNames(t *testing.T) {
	t.Parallel()
	names := contract.MetricNames()
	if len(names) == 0 {
		t.Fatal("MetricNames is empty")
	}
	seen := map[string]bool{}
	for _, n := range names {
		if n == "" || seen[n] {
			t.Errorf("name %q empty or listed twice", n)
		}
		seen[n] = true
	}
	if !seen[contract.MetricSessionsOpen] || !seen[contract.MetricICDCheckInsMissed] {
		t.Errorf("MetricNames %v leaves out a named metric", names)
	}
}
