// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"errors"
	"testing"

	"thingfabric.dev/go-fabric/endpoint"
	epmem "thingfabric.dev/go-fabric/endpoint/memstore"
	"thingfabric.dev/go-fabric/node"
	"thingfabric.dev/go-fabric/store/memstore"
)

// TestRemoveKeepsItsReservationWhenThePriorReadFailsP8G (gate c-B1 of
// R5-store-5205): a Remove joins its tree with the reservation the key
// holds; when that read fails, the error comes back and the tree is
// still reserved, un-joined — a later Forget of the removed key erases
// its parts' endpoint records. Before, the failed read dropped the whole
// reservation and the Forget erased the key alone.
func TestRemoveKeepsItsReservationWhenThePriorReadFailsP8G(t *testing.T) {
	ctx := t.Context()
	const partKey = "owner/light"
	eps := epmem.New()
	creds := &failingReadA9{Port: memstore.New(), fail: "endpoint_number_reserved.owner"}
	cfg := minimalConfig(nil)
	cfg.Store = node.Store{Credentials: creds, Endpoints: eps}
	n := newNode(t, cfg)
	agg := n.Aggregator()
	if err := agg.Add(ctx, bridgedWithParts(t, "owner", partKey)); err != nil {
		t.Fatal(err)
	}
	if err := n.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if !hasEndpointRecord(t, eps, partKey) {
		t.Fatalf("%s has no endpoint record once served", partKey)
	}
	err := agg.Batch(ctx, func(b *node.Batch) error {
		if err := b.Remove(endpoint.StringKey("owner")); err != nil {
			return err
		}
		// After fn the Remove's reservation reads what the key holds
		// first: that read fails.
		creds.armed.Store(true)
		return nil
	})
	if !errors.Is(err, errReadA9) {
		t.Fatalf("Batch: %v; want the read error", err)
	}
	if err := agg.Forget(ctx, endpoint.StringKey("owner")); err != nil {
		t.Fatalf("Forget(owner): %v", err)
	}
	for _, k := range []string{"owner", partKey} {
		if hasEndpointRecord(t, eps, k) {
			t.Errorf("the endpoint record of %s survives Forget(owner)", k)
		}
	}
}
