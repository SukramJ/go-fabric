// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

import (
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec/onoff"
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
