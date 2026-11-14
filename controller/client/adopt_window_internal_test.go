// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

import (
	"context"
	"testing"
)

// storedOnOffVersion is the stored DataVersion of the light's OnOff
// cluster, nil when unknown; ok false when the store holds no such
// cluster.
func storedOnOffVersion(t *testing.T, st Store, id string) (version *uint32, ok bool) {
	t.Helper()
	eps, err := st.Load(id)
	if err != nil {
		t.Fatal(err)
	}
	for _, ep := range eps {
		if ep.Number != 1 {
			continue
		}
		for _, c := range ep.Clusters {
			if c.ID == onOffCluster {
				return c.Version, true
			}
		}
	}
	return nil, false
}

// TestReportWriteQueuedBeforeAnAdoption (review II, R2-P1-ctl-04): a
// subscription report applied just before the cache adopts the other
// fabric filter — its store write collected under the node's lock, the
// adoption's drain finding the queue empty because the write is not queued
// yet — is written after the adoption forgot every version. It stores its
// values, as they are in memory, without its version: the store keeps the
// version unknown under the new filter, as memory does. Before, the write
// stored the old filter's version over the one the adoption cleared, and
// a restart sent it as a known version under the new filter.
//
// The interleaving is applyWith's own, split where the window lies: the
// report is mutated with the write collected (applyWith under the lock),
// the adoption runs, then the node's job queue performs the write.
func TestReportWriteQueuedBeforeAnAdoption(t *testing.T) {
	t.Parallel()
	st := &MemoryStore{}
	n, _, _ := peerImage(t, st)
	if _, err := n.adoptFabricFilter(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	n.mu.Lock()
	var saves []saveJob
	n.persist.collect = &saves
	n.s.mutate(wildcard, &onOffReport(77).r, false, nil)
	n.persist.collect = nil
	n.s.takeEmissions()
	n.mu.Unlock()
	if len(saves) == 0 {
		t.Fatal("the report collected no store write")
	}
	if _, err := n.adoptFabricFilter(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if v, ok := storedOnOffVersion(t, st, n.ID()); !ok || v != nil {
		t.Fatalf("stored version right after the adoption = %v (%v), want unknown", v, ok)
	}
	n.persist.write(saves)
	v, ok := storedOnOffVersion(t, st, n.ID())
	if !ok {
		t.Fatal("the report's values were not stored")
	}
	if v != nil {
		t.Fatalf("stored version %d after the adoption of the other filter, want unknown", *v)
	}
	if val := onOffValue(n); val != true {
		t.Fatalf("OnOff %v, want true", val)
	}
}
