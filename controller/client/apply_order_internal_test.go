// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

import (
	"slices"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/cluster/spec/onoff"
	"github.com/SukramJ/go-fabric/im"
)

// TestDeferredReportsPersistInApplyOrder: subscription reports applied
// concurrently queue their store writes in the order they changed the
// image, so the store ends on the version the image holds (review II,
// R2-P2-store-09).
func TestDeferredReportsPersistInApplyOrder(t *testing.T) {
	t.Parallel()
	st := &MemoryStore{}
	n, _, _ := testNode(t, st)
	n.apply(wildcard, &bridge(1).r, true)
	for round := range 300 {
		var wg sync.WaitGroup
		start := make(chan struct{})
		for k := range 2 {
			version := uint32(10 + 2*round + k) //nolint:gosec // a small test count
			r := (&report{}).attr(2, onoff.ClusterID, 0, boolNode(k == 0), version)
			wg.Go(func() {
				<-start
				n.applyDeferred(wildcard, &r.r, true, nil)
			})
		}
		close(start)
		wg.Wait()
		n.jobs.wait()
		mem, _ := n.Endpoint(2).Cluster(onoff.ClusterID).Version()
		eps, _ := st.Load("n")
		var stored uint32
		for _, e := range eps {
			for _, c := range e.Clusters {
				if e.Number == 2 && c.ID == onoff.ClusterID && c.Version != nil {
					stored = *c.Version
				}
			}
		}
		if stored != mem {
			t.Fatalf("round %d: the store holds version %d, the image %d", round, stored, mem)
		}
	}
}

// TestReadAppliesInTheReportOrder: a read's result applied while a
// subscription report's job is still queued writes the store and runs its
// hooks after that job, in the order the two changed the image, so the
// store ends on the version the image holds and the hooks deliver the
// newer report last (review II, R2-P2-dr-ctl-02). Before, the read wrote
// through and delivered at once, and the queued job then stored the older
// version and delivered the older event last.
func TestReadAppliesInTheReportOrder(t *testing.T) {
	t.Parallel()
	st := &MemoryStore{}
	n, _, _ := testNode(t, st)
	n.apply(wildcard, &bridge(1).r, true)
	n.jobs.wait()
	var mu sync.Mutex
	var events []uint64
	n.cfg.Hooks.Event = func(_ uint16, _, _ uint32, e EventData, _ any) {
		mu.Lock()
		events = append(events, e.Number)
		mu.Unlock()
	}
	ev := func(number uint64) interaction.EventData {
		return interaction.EventData{Number: number, Path: im.ConcreteEventPath{Endpoint: 2, Cluster: onoff.ClusterID, HasEndpoint: true, HasCluster: true, HasEvent: true}}
	}

	hold := make(chan struct{})
	n.jobs.add(func() { <-hold }) // a host hook of an earlier report, still running
	sub := (&report{}).attr(2, onoff.ClusterID, 0, boolNode(true), 10)
	sub.r.Events = []interaction.EventData{ev(1)}
	n.applyDeferred(wildcard, &sub.r, true, nil) // a subscription report: queued
	read := (&report{}).attr(2, onoff.ClusterID, 0, boolNode(false), 11)
	read.r.Events = []interaction.EventData{ev(2)}
	n.applyIn(wildcard, &read.r, true, nil) // a read's result
	close(hold)
	n.jobs.wait()

	mem, _ := n.Endpoint(2).Cluster(onoff.ClusterID).Version()
	eps, _ := st.Load("n")
	var stored uint32
	for _, e := range eps {
		for _, c := range e.Clusters {
			if e.Number == 2 && c.ID == onoff.ClusterID && c.Version != nil {
				stored = *c.Version
			}
		}
	}
	if stored != mem || mem != 11 {
		t.Errorf("the store holds version %d, the image %d, want 11 in both", stored, mem)
	}
	mu.Lock()
	defer mu.Unlock()
	if !slices.Equal(events, []uint64{1, 2}) {
		t.Errorf("events delivered %v, want [1 2]", events)
	}
}
