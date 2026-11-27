// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client_test

import (
	"slices"
	"sync"
	"testing"

	"thingfabric.dev/go-fabric/controller/client"
	"thingfabric.dev/go-fabric/fabrictest"
	"thingfabric.dev/go-fabric/node"
)

// TestICDReportDrivenLITFlipKeepsSubscriptionLive guards F-b25ed960-C6
// end to end (a harness test, not a port): a report-driven SIT→LIT flip of
// a registered peer leaves the peer awake at the mode edge and the default
// sustained subscription live, with no SubscriptionStatusChanged(false),
// as matter.js's IcdClient sets requiresAwait and notes the peer active
// with no await between
// (packages/node/src/behavior/system/icd/IcdClient.ts:166-178 at db91886c).
// The same holds on the two-step form this replaced: the report's own
// arrival notes the peer active before the mode hook runs, so on this path
// the window is armed either way. The one-step state at the mode edge is
// held by icd's TestPeerWakefulnessSetOperatingModeIsOneStep.
func TestICDReportDrivenLITFlipKeepsSubscriptionLive(t *testing.T) {
	var (
		mu       sync.Mutex
		statuses []bool
	)
	record := func(active bool) { mu.Lock(); statuses = append(statuses, active); mu.Unlock() }
	p := newICDPair(t, icdPairOptions{
		cfg: icdLITConfig(true),
		controller: fabrictest.ControllerOptions{Registry: func(rc *client.RegistryConfig) {
			rc.Node.Hooks.SubscriptionStatusChanged = record
		}},
	})
	forceSIT(t, p)
	p.register(t, uncovered)
	w := p.wakefulness(t)
	if w.RequiresAwait() {
		t.Fatal("awaited before the flip")
	}
	var (
		edgeMu      sync.Mutex
		awakeAtEdge []bool
	)
	w.OnOperatingModeChanged(func(lit bool) {
		if lit {
			edgeMu.Lock()
			awakeAtEdge = append(awakeAtEdge, w.Awake())
			edgeMu.Unlock()
		}
	})
	p.settle(t)
	mu.Lock()
	statuses = nil
	mu.Unlock()
	if err := p.device().ICD().SetOperatingMode(node.ICDModeLIT); err != nil {
		t.Fatal(err)
	}
	p.eventually(t, "the flip", w.RequiresAwait)
	p.settle(t)
	edgeMu.Lock()
	defer edgeMu.Unlock()
	if !slices.Equal(awakeAtEdge, []bool{true}) {
		t.Fatalf("awake at the mode edge %v, want [true]", awakeAtEdge)
	}
	mu.Lock()
	defer mu.Unlock()
	if slices.Contains(statuses, false) || !p.node.SubscriptionActive() {
		t.Fatalf("subscription statuses after the flip %v, active %v", statuses, p.node.SubscriptionActive())
	}
}
