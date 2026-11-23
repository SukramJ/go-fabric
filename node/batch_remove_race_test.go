// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"thingfabric.dev/go-fabric/endpoint"
	"thingfabric.dev/go-fabric/node"
)

// TestBatchRemoveRacingTheEndOfFn (fable-diff-g5g N4): a Remove on the
// batch from another goroutine while fn returns either applies before the
// batch ends — its reservation among the batch's — or is [node.ErrBatchDone];
// -race holds that its reservation is never appended while the batch
// reads them. Each key a Remove applied is not served afterwards.
func TestBatchRemoveRacingTheEndOfFn(t *testing.T) {
	t.Parallel()
	const devices = 24
	eps := make([]node.Endpoint, devices)
	for i := range eps {
		eps[i] = kitLight(t, fmt.Sprintf("r%d", i))
	}
	n := batchNode(t, eps...)
	var wg sync.WaitGroup
	results := make([]error, devices)
	// fn returns once the first Remove is in: the rest race its end.
	first := make(chan struct{})
	err := n.Aggregator().Batch(t.Context(), func(b *node.Batch) error {
		wg.Go(func() {
			for i := range devices {
				results[i] = b.Remove(endpoint.StringKey(fmt.Sprintf("r%d", i)))
				if i == 0 {
					close(first)
				}
			}
		})
		<-first
		return nil
	})
	wg.Wait()
	if err != nil {
		t.Fatalf("batch: %v", err)
	}
	for i, rerr := range results {
		key := fmt.Sprintf("r%d", i)
		_, served := servedNumberOf(n, key)
		switch {
		case rerr == nil && served:
			// Applied after the reassembly read the set is impossible:
			// a Remove that returned nil was taken before fn's end.
			t.Errorf("Remove(%s) returned nil but %s is still served", key, key)
		case rerr != nil && !errors.Is(rerr, node.ErrBatchDone):
			t.Errorf("Remove(%s): %v, want nil or %v", key, rerr, node.ErrBatchDone)
		}
	}
}
