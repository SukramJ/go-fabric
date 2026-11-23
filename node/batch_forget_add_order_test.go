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

// TestBatchForgetAndAddOfOneKeyIgnoreCallOrder (decision 582: "whatever
// the call order"; fable-diff-g5g N2, probes Q1 and Q2): a batch forgets
// the owner of a part, forgets the part's key and adds a device that
// takes that key.
//
// As a part key (Q2) the outcome is the same in every call order: both
// Forgets apply, the new device is served and the key starts new. Before,
// an Add before the part's Forget made the Forget a refusal
// ([node.ErrUnknownKey]) and the key kept the owner's number.
//
// As the device's own key (Q1) a Forget of the key drops the batch's own
// Add made before it, as it drops an applied one ([node.Batch]); the
// outcome follows the order of those two calls alone — never the place of
// the owner's Forget, and never the moment a held call is judged. Before,
// a Forget held to the end of the batch dropped a device added after it.
func TestBatchForgetAndAddOfOneKeyIgnoreCallOrder(t *testing.T) {
	t.Parallel()
	const partKey = "owner/light"
	for _, tc := range []struct {
		name string
		// taker is the device the batch adds; its key is served after.
		taker func(t *testing.T) (node.Endpoint, string)
	}{
		{"own key", func(t *testing.T) (node.Endpoint, string) { t.Helper(); return kitLight(t, partKey), partKey }},
		{"part key", func(t *testing.T) (node.Endpoint, string) {
			t.Helper()
			return bridgedWithParts(t, "fresh", partKey), "fresh"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			type outcome struct {
				err             string
				taker, part     uint16
				takerOK, partOK bool
			}
			calls := []string{"add", "forget part", "forget owner"}
			orders := [][]int{{0, 1, 2}, {0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0}}
			type seenOutcome struct {
				names []string
				o     outcome
			}
			seen := map[bool]seenOutcome{}
			for _, order := range orders {
				ctx := t.Context()
				n := batchNode(t, bridgedWithParts(t, "owner", partKey), kitLight(t, "bystander"))
				ep, takerKey := tc.taker(t)
				err := n.Aggregator().Batch(ctx, func(b *node.Batch) error {
					var errs []error
					for _, c := range order {
						switch c {
						case 0:
							errs = append(errs, b.Add(ep))
						case 1:
							errs = append(errs, b.Forget(endpoint.StringKey(partKey)))
						case 2:
							errs = append(errs, b.Forget(endpoint.StringKey("owner")))
						}
					}
					return errors.Join(errs...)
				})
				var o outcome
				if err != nil {
					o.err = err.Error()
				}
				o.taker, o.takerOK = servedNumberOf(n, takerKey)
				o.part, o.partOK = servedNumberOf(n, partKey)
				names := make([]string, len(order))
				for i, c := range order {
					names[i] = calls[c]
				}
				// Q1: a Forget of the device's own key after its Add drops it.
				addAt, forgetAt := slices.Index(order, 0), slices.Index(order, 1)
				wantServed := tc.name != "own key" || forgetAt < addAt
				if err != nil || o.takerOK != wantServed || o.partOK != wantServed {
					t.Errorf("%v: batch %v, %s served %v, %s served %v; want nil, served %v", names, err, takerKey, o.takerOK, partKey, o.partOK, wantServed)
				}
				if aerr := n.Aggregator().Add(ctx, kitLight(t, "later")); aerr != nil {
					t.Errorf("%v: unrelated Add after the batch: %v", names, aerr)
				}
				if prev, ok := seen[wantServed]; ok && prev.o != o {
					t.Errorf("the order decides: %v gives %+v, %v gives %+v", prev.names, prev.o, names, o)
				}
				seen[wantServed] = seenOutcome{names, o}
			}
		})
	}
}
