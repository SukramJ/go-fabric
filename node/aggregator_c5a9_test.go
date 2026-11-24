// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"thingfabric.dev/go-fabric/endpoint"
	epmem "thingfabric.dev/go-fabric/endpoint/memstore"
	"thingfabric.dev/go-fabric/node"
	"thingfabric.dev/go-fabric/store"
	"thingfabric.dev/go-fabric/store/memstore"
)

// TestBatchReplaceUndoKeepsItsPartsReserved (review II, R5-store-5205): a
// Replace the commit undoes — its live Spec gained, during fn, a part key
// another endpoint serves — after a reassembly during fn served it and
// stored a number for the replacement's own part leaves that part
// reserved under the key: a later Forget of the key erases the part's
// endpoint record too, directly or after a Remove, and the colliding
// endpoint's part is kept.
func TestBatchReplaceUndoKeepsItsPartsReserved(t *testing.T) {
	for _, removeFirst := range []bool{false, true} {
		name := "Forget"
		if removeFirst {
			name = "Remove then Forget"
		}
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()
			eps := epmem.New()
			cfg := minimalConfig(nil)
			cfg.Store = node.Store{Credentials: memstore.New(), Endpoints: eps}
			n := newNode(t, cfg)
			agg := n.Aggregator()
			for _, ep := range []node.Endpoint{bridgedWithParts(t, "o", "p"), bridgedWithParts(t, "x", "xp")} {
				if err := agg.Add(ctx, ep); err != nil {
					t.Fatal(err)
				}
			}
			if err := n.Start(ctx); err != nil {
				t.Fatal(err)
			}
			r := &specFlip{with: bridgedWithParts(t, "x", "xq").Spec(), without: bridgedWithParts(t, "x", "xq", "p").Spec()}
			r.k.Store(1 << 40)
			err := agg.Batch(ctx, func(b *node.Batch) error {
				if err := b.Replace(endpoint.StringKey("x"), r); err != nil {
					return err
				}
				// An Add of another goroutine's kind reassembles on its own:
				// the replacement is served and xq gets its number.
				if err := agg.Add(ctx, kitLight(t, "other")); err != nil {
					return err
				}
				if !hasEndpointRecord(t, eps, "xq") {
					t.Fatal("xq has no endpoint record during fn; the reassembly did not serve the replacement")
				}
				r.k.Store(0) // the replacement gains o's part p
				return nil
			})
			if !errors.Is(err, node.ErrDuplicateKey) {
				t.Fatalf("Batch whose Replace came to collide: %v; want ErrDuplicateKey", err)
			}
			if _, ok := servedNumberOf(n, "xp"); !ok {
				t.Fatal("xp not served: the Replace was not undone")
			}
			if removeFirst {
				if err := agg.Remove(ctx, endpoint.StringKey("x")); err != nil {
					t.Fatalf("Remove(x): %v", err)
				}
			}
			if err := agg.Forget(ctx, endpoint.StringKey("x")); err != nil {
				t.Fatalf("Forget(x): %v", err)
			}
			for _, k := range []string{"x", "xp", "xq"} {
				if hasEndpointRecord(t, eps, k) {
					t.Errorf("the endpoint record of %s survives Forget(x)", k)
				}
			}
			if !hasEndpointRecord(t, eps, "p") {
				t.Error("Forget(x) erased the record of p, which o serves")
			}
		})
	}
}

// TestKeylessPartRefusedAtAdd (review II, R5-err-5203): an endpoint whose
// tree holds a part whose key renders empty is refused at Add with
// ErrNoStableKey — one such part as two — and the node starts.
func TestKeylessPartRefusedAtAdd(t *testing.T) {
	for _, keyless := range []int{1, 2} {
		t.Run(strings.Repeat("keyless part ", keyless), func(t *testing.T) {
			ctx := t.Context()
			cfg := minimalConfig(nil)
			cfg.Store = node.Store{Credentials: memstore.New(), Endpoints: epmem.New()}
			n := newNode(t, cfg)
			spec := bridgedWithParts(t, "k", "a", "b").Spec()
			for i := range keyless {
				spec.Parts[i].StableKey = nil
			}
			if err := n.Aggregator().Add(ctx, ifaceEndpoint{s: &spec}); !errors.Is(err, node.ErrNoStableKey) {
				t.Fatalf("Add with %d key-less parts: %v; want ErrNoStableKey", keyless, err)
			}
			if err := n.Start(ctx); err != nil {
				t.Fatalf("Start after the refusal: %v", err)
			}
		})
	}
	t.Run("key rendering empty", func(t *testing.T) {
		ctx := t.Context()
		cfg := minimalConfig(nil)
		cfg.Store = node.Store{Credentials: memstore.New(), Endpoints: epmem.New()}
		n := newNode(t, cfg)
		spec := bridgedWithParts(t, "k", "a").Spec()
		spec.Parts[0].StableKey = endpoint.StringKey("")
		if err := n.Aggregator().Add(ctx, ifaceEndpoint{s: &spec}); !errors.Is(err, node.ErrNoStableKey) {
			t.Fatalf("Add with a part whose key renders empty: %v; want ErrNoStableKey", err)
		}
	})
}

// failingReadA9 is a credential store that refuses one read of the key
// named by fail, once armed: the read after the skip reads it lets pass.
type failingReadA9 struct {
	store.Port
	fail  string
	skip  atomic.Int32
	armed atomic.Bool
}

var errReadA9 = errors.New("read refused")

func (s *failingReadA9) GetSetting(ctx context.Context, key string) (value string, found bool, err error) {
	if key == s.fail && s.armed.Load() {
		if s.skip.Add(-1) < 0 && s.armed.CompareAndSwap(true, false) {
			return "", false, errReadA9
		}
	}
	return s.Port.GetSetting(ctx, key)
}

// TestForgetReadErrorErasesNothing (review II, R3-store-3703): when the
// reservation read a batch's erasure judges the served endpoints by fails,
// the Forget's erasure is skipped and the error returned — it does not
// run with a smaller set and take the record of an endpoint the set
// serves. Here a removed owner's part key is served by another endpoint
// the same batch added, and the owner's reservation is first written by
// that batch, so only the read after fn names the part.
func TestForgetReadErrorErasesNothing(t *testing.T) {
	ctx := t.Context()
	const partKey = "owner/light"
	eps := epmem.New()
	creds := &failingReadA9{Port: memstore.New(), fail: "endpoint_number_reserved.owner"}
	cfg := minimalConfig(nil)
	cfg.Store = node.Store{Credentials: creds, Endpoints: eps}
	n := newNode(t, cfg)
	agg := n.Aggregator()
	for _, ep := range []node.Endpoint{bridgedWithParts(t, "owner", partKey), bridgedWithParts(t, "x")} {
		if err := agg.Add(ctx, ep); err != nil {
			t.Fatal(err)
		}
	}
	if err := n.Start(ctx); err != nil {
		t.Fatal(err)
	}
	before, ok := servedNumberOf(n, partKey)
	if !ok {
		t.Fatalf("%s not served", partKey)
	}
	err := agg.Batch(ctx, func(b *node.Batch) error {
		if err := b.Remove(endpoint.StringKey("owner")); err != nil {
			return err
		}
		if err := b.Add(bridgedWithParts(t, "y", partKey)); err != nil {
			return err
		}
		if err := b.Forget(endpoint.StringKey("owner")); err != nil {
			return err
		}
		// After fn the Remove's reservation reads the owner's first (it
		// joins what the key holds), then the erasure's judgement reads
		// it: that second read fails.
		creds.skip.Store(1)
		creds.armed.Store(true)
		return nil
	})
	if !errors.Is(err, errReadA9) {
		t.Fatalf("Batch: %v; want the read error", err)
	}
	if !hasEndpointRecord(t, eps, partKey) {
		t.Fatalf("the erasure took the record of %s, which y serves", partKey)
	}
	if !hasEndpointRecord(t, eps, "owner") {
		t.Error("the erasure ran: owner's record is gone")
	}
	if err := agg.Add(ctx, kitLight(t, "other")); err != nil {
		t.Fatal(err)
	}
	if after, ok := servedNumberOf(n, partKey); !ok || after != before {
		t.Errorf("%s served at %d (%v), want %d", partKey, after, ok, before)
	}
}
