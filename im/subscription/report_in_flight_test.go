// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package subscription_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/im/subscription"
)

// TestChangesDuringAReportInFlightLeaveInOneNextReport: while a
// subscription's reporter runs — the bridge's waits for the peer's
// StatusResponse — a tick starts no second report of it, and the paths
// that change meanwhile, one of them twice, leave together in one next
// report once the reporter returned. matter.js keeps one outstanding
// attribute set while an update is in flight and sends it whole as the
// next report (packages/node/src/node/server/ServerSubscription.ts:346-357,
// :520-529, :537-543 at db021da0).
func TestChangesDuringAReportInFlightLeaveInOneNextReport(t *testing.T) {
	t.Parallel()
	entered := make(chan []im.ConcreteAttributePath, 4)
	release := make(chan struct{})
	reporter := func(_ context.Context, _ *subscription.Subscription, paths []im.ConcreteAttributePath) {
		entered <- slices.Clone(paths)
		<-release
	}
	m := newManager(subscription.Config{}, reporter)
	a, b := mkPath(1, 0x0006, 0x0000), mkPath(1, 0x0006, 0x4000)
	args := defaultArgs()
	args.AttributePaths = []im.ConcreteAttributePath{a, b}
	if _, err := m.Subscribe(args); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	ctx := context.Background()
	t0 := time.Now()

	m.OnAttributeChanged(a)
	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		m.Tick(ctx, t0.Add(2*time.Second))
	}()
	first := <-entered
	if !slices.Equal(first, []im.ConcreteAttributePath{a}) {
		t.Fatalf("first report %v, want %v", first, a)
	}

	m.OnAttributeChanged(b)
	m.OnAttributeChanged(a)
	m.Tick(ctx, t0.Add(4*time.Second))
	select {
	case p := <-entered:
		t.Fatalf("a second report %v started while the first was in flight", p)
	default:
	}

	close(release)
	<-firstDone
	m.Tick(ctx, t0.Add(6*time.Second))
	select {
	case next := <-entered:
		if len(next) != 2 || !slices.Contains(next, a) || !slices.Contains(next, b) {
			t.Fatalf("next report %v, want both changed paths in one", next)
		}
	default:
		t.Fatal("no report after the first returned")
	}
	select {
	case p := <-entered:
		t.Fatalf("a further report %v, want the changes in one", p)
	default:
	}
}
