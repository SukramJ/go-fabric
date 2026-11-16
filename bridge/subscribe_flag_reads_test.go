// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"slices"
	"sync/atomic"
	"testing"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/im/subscription"
)

// flagReadCounter counts a dispatcher's reads.
type flagReadCounter struct {
	im.Dispatcher
	reads atomic.Int32
}

func (c *flagReadCounter) Read(ctx context.Context, path im.ConcreteAttributePath) []im.ReadResult {
	c.reads.Add(1)
	return c.Dispatcher.Read(ctx, path)
}

// TestFlaggedReportReadsTheConfigurationVersionOnce (review II,
// R2-P6-bound-0565): a report of n changes on a subscription with m paths
// whose WildcardPathFlags carry a WildcardFilterConfigurationVersion reads
// the node's ConfigurationVersion once, not once per (change × covering
// path).
func TestFlaggedReportReadsTheConfigurationVersionOnce(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	d := &flagReadCounter{Dispatcher: b.Dispatcher()}
	flagged := func(ep uint16) im.ConcreteAttributePath {
		return im.ConcreteAttributePath{
			Endpoint: ep, HasEndpoint: true,
			WildcardPathFlags: im.WildcardSkipDiagnosticsClusters, WildcardFilterConfigurationVersion: 1, HasWildcardFilterConfigurationVersion: true,
		}
	}
	sub := &subscription.Subscription{AttributePaths: []im.ConcreteAttributePath{flagged(1), flagged(1), flagged(1)}}
	changed := make([]im.ConcreteAttributePath, 0, 10)
	for attr := range uint32(10) {
		changed = append(changed, im.ConcreteAttributePath{Endpoint: 1, Cluster: 6, Attribute: attr, HasEndpoint: true, HasCluster: true, HasAttribute: true})
	}
	_ = b.unskippedChangedPaths(context.Background(), d, sub, changed)
	if n := d.reads.Load(); n != 1 {
		t.Fatalf("one report read the dispatcher %d times, want 1 (the ConfigurationVersion)", n)
	}
}

// TestFlaggedSubscriptionChangeReportOmitsSkippedPaths (review II,
// R2-P6-fsm-0817): a change a subscription's WildcardPathFlags skip is
// not reported, unless another of its paths — here a concrete one —
// covers it; a change no flag skips is.
func TestFlaggedSubscriptionChangeReportOmitsSkippedPaths(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	at := func(ep uint16, cluster, attr uint32) im.ConcreteAttributePath {
		return im.ConcreteAttributePath{Endpoint: ep, Cluster: cluster, Attribute: attr, HasEndpoint: true, HasCluster: true, HasAttribute: true}
	}
	sub := &subscription.Subscription{AttributePaths: []im.ConcreteAttributePath{
		{WildcardPathFlags: im.WildcardSkipRootNode},
		at(0, 0x0028, 0x0005),
	}}
	changed := []im.ConcreteAttributePath{at(0, 0x0028, 0x0005), at(0, 0x0028, 0x0006), at(1, 0x0006, 0x0000)}
	got := b.unskippedChangedPaths(context.Background(), b.Dispatcher(), sub, changed)
	want := []im.ConcreteAttributePath{at(0, 0x0028, 0x0005), at(1, 0x0006, 0x0000)}
	if !slices.Equal(got, want) {
		t.Fatalf("reported changes = %+v, want %+v", got, want)
	}
}
