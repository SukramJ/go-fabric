// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"errors"
	"slices"
	"testing"

	"thingfabric.dev/go-fabric/endpoint"
	"thingfabric.dev/go-fabric/node"
)

// TestBatchRefusesAPartAddedDuringFnThatAnotherEndpointServes (decision
// 585: no path may wedge; fable-diff-g5g N1, probe Q6): a device the
// batch added or replaced at the call, without a collision, that gains
// during fn a part whose key another endpoint serves is refused at the
// batch's end with [node.ErrDuplicateKey] and changes nothing: the
// endpoint serving the key keeps it at its number, a replaced endpoint
// stays served, and a later unrelated Add reassembles. Before, the batch
// assembled two Specs under one key, and that assembly and every later
// one failed until the device went (decision 580).
func TestBatchRefusesAPartAddedDuringFnThatAnotherEndpointServes(t *testing.T) {
	t.Parallel()
	const served = "other/light"
	for _, tc := range []struct {
		name    string
		replace bool
	}{
		{"Add", false},
		{"Replace", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			n := batchNode(t, bridgedWithParts(t, "other", served), bridgedWithParts(t, "fresh"))
			otherNum, _ := servedNumberOf(n, "other")
			partNum, _ := servedNumberOf(n, served)
			freshNum, _ := servedNumberOf(n, "fresh")
			old := n.Aggregator().Endpoints()
			taken := bridgedWithParts(t, "fresh")
			if !tc.replace {
				taken = bridgedWithParts(t, "late")
			}
			err := n.Aggregator().Batch(ctx, func(b *node.Batch) error {
				var err error
				if tc.replace {
					err = b.Replace(endpoint.StringKey("fresh"), taken)
				} else {
					err = b.Add(taken)
				}
				if err != nil {
					return err
				}
				return taken.AddPart(kitLight(t, served).Device)
			})
			if !errors.Is(err, node.ErrDuplicateKey) {
				t.Fatalf("batch: %v, want %v", err, node.ErrDuplicateKey)
			}
			if slices.Contains(n.Aggregator().Endpoints(), node.Endpoint(taken)) {
				t.Error("the refused device is in the set")
			}
			if tc.replace && !slices.Contains(n.Aggregator().Endpoints(), old[1]) {
				t.Error("the replaced endpoint left the set")
			}
			for key, want := range map[string]uint16{"other": otherNum, served: partNum, "fresh": freshNum} {
				if got, ok := servedNumberOf(n, key); !ok || got != want {
					t.Errorf("%s served at %d (%v), want %d", key, got, ok, want)
				}
			}
			if _, ok := servedNumberOf(n, "late"); ok {
				t.Error("the refused device is served")
			}
			if aerr := n.Aggregator().Add(ctx, kitLight(t, "after")); aerr != nil {
				t.Errorf("unrelated Add after the batch: %v", aerr)
			}
		})
	}
}
