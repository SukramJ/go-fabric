// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"thingfabric.dev/go-fabric/device"
	"thingfabric.dev/go-fabric/endpoint"
	epmem "thingfabric.dev/go-fabric/endpoint/memstore"
	"thingfabric.dev/go-fabric/node"
	"thingfabric.dev/go-fabric/store/memstore"
)

// TestTreeHoldingOneKeyTwiceIsRefused (review II, R4-api-4790, decision
// 617): an endpoint whose parts tree holds one StableKey twice is refused
// with ErrDuplicateKey at its insertion — Add, Replace, Batch.Add, and
// Device.AddPart on a device the node serves — nothing of it taken, and
// the node's later changes are served. Before, the set took it and its
// reassembly, and every later one, failed ("two Specs share this
// StableKey").
func TestTreeHoldingOneKeyTwiceIsRefused(t *testing.T) {
	insertions := []struct {
		name string
		do   func(ctx context.Context, agg *node.Aggregator, x node.Endpoint) error
	}{
		{"Add", func(ctx context.Context, agg *node.Aggregator, _ node.Endpoint) error {
			return agg.Add(ctx, bridgedWithParts(t, "m", "q", "q"))
		}},
		{"Replace", func(ctx context.Context, agg *node.Aggregator, _ node.Endpoint) error {
			return agg.Replace(ctx, endpoint.StringKey("x"), bridgedWithParts(t, "x", "q", "q"))
		}},
		{"Batch.Add", func(ctx context.Context, agg *node.Aggregator, _ node.Endpoint) error {
			return agg.Batch(ctx, func(b *node.Batch) error { return b.Add(bridgedWithParts(t, "m", "q", "q")) })
		}},
		{"AddPart", func(_ context.Context, _ *node.Aggregator, x node.Endpoint) error {
			return x.(*device.BridgedNode).AddPart(bridgedWithParts(t, "m", "q", "q").Device)
		}},
	}
	for _, in := range insertions {
		for _, started := range []bool{false, true} {
			t.Run(fmt.Sprint(in.name, "/started=", started), func(t *testing.T) {
				ctx := t.Context()
				cfg := minimalConfig(nil)
				cfg.Store = node.Store{Credentials: memstore.New(), Endpoints: epmem.New()}
				n := newNode(t, cfg)
				agg := n.Aggregator()
				x := bridgedWithParts(t, "x", "xp")
				if err := agg.Add(ctx, x); err != nil {
					t.Fatal(err)
				}
				if started {
					if err := n.Start(ctx); err != nil {
						t.Fatal(err)
					}
				}
				var err error
				noPanic(t, in.name, func() { err = in.do(ctx, agg, x) })
				if !errors.Is(err, node.ErrDuplicateKey) {
					t.Fatalf("%s of a tree holding q twice: %v; want ErrDuplicateKey", in.name, err)
				}
				if len(x.Parts()) != 1 {
					t.Fatalf("after the refusal: x has %d parts; want nothing moved", len(x.Parts()))
				}
				if err := agg.Add(ctx, kitLight(t, "u1")); err != nil {
					t.Fatalf("unrelated Add after the refusal: %v", err)
				}
				if !started {
					if err := n.Start(ctx); err != nil {
						t.Fatalf("Start: %v", err)
					}
				}
				if _, ok := servedNumberOf(n, "u1"); !ok {
					t.Fatal("u1 not served")
				}
				if _, ok := servedNumberOf(n, "xp"); !ok {
					t.Fatal("x's part xp not served: the refused call changed x")
				}
			})
		}
	}
}

// TestBatchTakeGrowingOneKeyTwiceIsUndone (review II, R4-api-4790,
// decision 617): an endpoint a Batch added whose tree, during fn, came to
// hold one key twice is refused with ErrDuplicateKey once fn returned and
// undone, as a take colliding with another endpoint is (decision 580), so
// the node's later changes are served.
func TestBatchTakeGrowingOneKeyTwiceIsUndone(t *testing.T) {
	for _, started := range []bool{false, true} {
		t.Run(fmt.Sprint("started=", started), func(t *testing.T) {
			ctx := t.Context()
			cfg := minimalConfig(nil)
			cfg.Store = node.Store{Credentials: memstore.New(), Endpoints: epmem.New()}
			n := newNode(t, cfg)
			agg := n.Aggregator()
			if started {
				if err := n.Start(ctx); err != nil {
					t.Fatal(err)
				}
			}
			spec := bridgedWithParts(t, "h", "q").Spec()
			h := ifaceEndpoint{s: &spec}
			var err error
			noPanic(t, "Batch", func() {
				err = agg.Batch(ctx, func(b *node.Batch) error {
					if err := b.Add(h); err != nil {
						return err
					}
					spec.Parts = append(spec.Parts, spec.Parts[0]) // q twice
					return nil
				})
			})
			if !errors.Is(err, node.ErrDuplicateKey) {
				t.Fatalf("Batch whose Add came to hold q twice: %v; want ErrDuplicateKey", err)
			}
			if err := agg.Add(ctx, kitLight(t, "u1")); err != nil {
				t.Fatalf("unrelated Add after the refusal: %v", err)
			}
			if !started {
				if err := n.Start(ctx); err != nil {
					t.Fatalf("Start: %v", err)
				}
			}
			if _, ok := servedNumberOf(n, "u1"); !ok {
				t.Fatal("u1 not served")
			}
			if _, ok := servedNumberOf(n, "h"); ok {
				t.Fatal("h served: the refused take was not undone")
			}
		})
	}
	// A Replace whose replacement came to hold one key twice is undone
	// the same way: the endpoint it replaced is served again (review II,
	// R5-test-5202, the duplicateKey arm of the late judgement).
	t.Run("Replace", func(t *testing.T) {
		ctx := t.Context()
		cfg := minimalConfig(nil)
		cfg.Store = node.Store{Credentials: memstore.New(), Endpoints: epmem.New()}
		n := newNode(t, cfg)
		agg := n.Aggregator()
		if err := agg.Add(ctx, bridgedWithParts(t, "x", "xp")); err != nil {
			t.Fatal(err)
		}
		if err := n.Start(ctx); err != nil {
			t.Fatal(err)
		}
		spec := bridgedWithParts(t, "x", "q").Spec()
		h := ifaceEndpoint{s: &spec}
		var err error
		noPanic(t, "Batch", func() {
			err = agg.Batch(ctx, func(b *node.Batch) error {
				if err := b.Replace(endpoint.StringKey("x"), h); err != nil {
					return err
				}
				spec.Parts = append(spec.Parts, spec.Parts[0]) // q twice
				return nil
			})
		})
		if !errors.Is(err, node.ErrDuplicateKey) {
			t.Fatalf("Batch whose Replace came to hold q twice: %v; want ErrDuplicateKey", err)
		}
		if _, ok := servedNumberOf(n, "xp"); !ok {
			t.Fatal("xp not served: the replaced endpoint was not put back")
		}
		if _, ok := servedNumberOf(n, "q"); ok {
			t.Fatal("q served: the refused Replace was not undone")
		}
		if err := agg.Add(ctx, kitLight(t, "u1")); err != nil {
			t.Fatalf("unrelated Add after the refusal: %v", err)
		}
		if _, ok := servedNumberOf(n, "u1"); !ok {
			t.Fatal("u1 not served")
		}
	})
}
