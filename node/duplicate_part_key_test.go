// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"errors"
	"testing"

	"thingfabric.dev/go-fabric/endpoint"
	epmem "thingfabric.dev/go-fabric/endpoint/memstore"
	"thingfabric.dev/go-fabric/node"
	"thingfabric.dev/go-fabric/store/memstore"
)

// TestAddOrReplaceRefusesAKeyAnotherEndpointServes (decision 580): an
// Add or a Replace of a device whose part key — or whose own key — another
// endpoint, of any partition, serves as a part is refused up front with
// [node.ErrDuplicateKey], as an Add of a served key is, and changes
// nothing. Before, the set took the device and the reassembly failed on
// the two Specs sharing the key — that one and every later one, until the
// device went (G5d probe). The served part stays at its number, and a
// later, unrelated Add reassembles. A key no endpoint serves may still be
// taken (decisions 575, 581): TestForgetLetsANewDeviceTakeAFormerKeyNoEndpointServes.
func TestAddOrReplaceRefusesAKeyAnotherEndpointServes(t *testing.T) {
	t.Parallel()
	const partKey = "owner/light"
	for _, tc := range []struct {
		name string
		// owner is the partition owner (part partKey) is added to, scope
		// the one the refused call runs in.
		owner, scope string
		// existing, when set, is added in a call before the refused one.
		existing func(t *testing.T) node.Endpoint
		call     func(ctx context.Context, t *testing.T, p *node.Partition) error
		// fresh is the key of the refused device; served after the
		// refusal only when it was served before.
		fresh string
	}{
		{
			name: "Add, same partition", owner: node.Scope, scope: node.Scope, fresh: "fresh",
			call: func(ctx context.Context, t *testing.T, p *node.Partition) error {
				t.Helper()
				return p.Add(ctx, bridgedWithParts(t, "fresh", partKey))
			},
		},
		{
			name: "Add, owner in another partition", owner: "q", scope: node.Scope, fresh: "fresh",
			call: func(ctx context.Context, t *testing.T, p *node.Partition) error {
				t.Helper()
				return p.Add(ctx, bridgedWithParts(t, "fresh", partKey))
			},
		},
		{
			name: "Add of a device under the served part key", owner: node.Scope, scope: "q", fresh: partKey,
			call: func(ctx context.Context, t *testing.T, p *node.Partition) error {
				t.Helper()
				return p.Add(ctx, kitLight(t, partKey))
			},
		},
		{
			name: "Batch.Add before a Forget of the owner", owner: node.Scope, scope: node.Scope, fresh: "fresh",
			call: func(ctx context.Context, t *testing.T, p *node.Partition) error {
				t.Helper()
				return p.Batch(ctx, func(b *node.Batch) error {
					if err := b.Add(bridgedWithParts(t, "fresh", partKey)); err != nil {
						return err
					}
					return b.Forget(endpoint.StringKey("owner"))
				})
			},
		},
		{
			name: "Replace, same partition", owner: node.Scope, scope: node.Scope, fresh: "fresh",
			existing: func(t *testing.T) node.Endpoint { t.Helper(); return bridgedWithParts(t, "fresh") },
			call: func(ctx context.Context, t *testing.T, p *node.Partition) error {
				t.Helper()
				return p.Replace(ctx, endpoint.StringKey("fresh"), bridgedWithParts(t, "fresh", partKey))
			},
		},
		{
			name: "Replace, owner in another partition", owner: "q", scope: node.Scope, fresh: "fresh",
			existing: func(t *testing.T) node.Endpoint { t.Helper(); return bridgedWithParts(t, "fresh") },
			call: func(ctx context.Context, t *testing.T, p *node.Partition) error {
				t.Helper()
				return p.Replace(ctx, endpoint.StringKey("fresh"), bridgedWithParts(t, "fresh", partKey))
			},
		},
		{
			name: "Batch.Replace before a Forget of the owner", owner: node.Scope, scope: node.Scope, fresh: "fresh",
			existing: func(t *testing.T) node.Endpoint { t.Helper(); return bridgedWithParts(t, "fresh") },
			call: func(ctx context.Context, t *testing.T, p *node.Partition) error {
				t.Helper()
				return p.Batch(ctx, func(b *node.Batch) error {
					if err := b.Replace(endpoint.StringKey("fresh"), bridgedWithParts(t, "fresh", partKey)); err != nil {
						return err
					}
					return b.Forget(endpoint.StringKey("owner"))
				})
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			cfg := minimalConfig(nil)
			cfg.Store = node.Store{Credentials: memstore.New(), Endpoints: epmem.New()}
			n := newNode(t, cfg)
			agg := n.Aggregator()
			if err := agg.Partition(tc.owner).Add(ctx, bridgedWithParts(t, "owner", partKey)); err != nil {
				t.Fatal(err)
			}
			if tc.existing != nil {
				if err := agg.Partition(tc.scope).Add(ctx, tc.existing(t)); err != nil {
					t.Fatal(err)
				}
			}
			if err := n.Start(ctx); err != nil {
				t.Fatal(err)
			}
			defer func() { _ = n.Stop(context.Background()) }()
			before, ok := servedNumberOf(n, partKey)
			if !ok {
				t.Fatalf("%s is not served before the call", partKey)
			}
			freshBefore, freshServed := servedNumberOf(n, tc.fresh)
			if tc.fresh == partKey {
				freshServed = false
			}
			if err := tc.call(ctx, t, agg.Partition(tc.scope)); !errors.Is(err, node.ErrDuplicateKey) {
				t.Errorf("call: %v, want %v", err, node.ErrDuplicateKey)
			}
			if after, ok := servedNumberOf(n, partKey); !ok || after != before {
				t.Errorf("%s at %d (served %v) after the refusal, want %d", partKey, after, ok, before)
			}
			if _, ok := servedNumberOf(n, "owner"); !ok {
				t.Error("the owner is not served after the refusal")
			}
			if tc.fresh != partKey {
				got, ok := servedNumberOf(n, tc.fresh)
				if ok != freshServed || (ok && got != freshBefore) {
					t.Errorf("%s served %v at %d after the refusal, want served %v at %d", tc.fresh, ok, got, freshServed, freshBefore)
				}
			}
			if err := agg.Add(ctx, kitLight(t, "other")); err != nil {
				t.Fatalf("unrelated Add after the refusal: %v", err)
			}
			if _, ok := servedNumberOf(n, "other"); !ok {
				t.Error("the unrelated device is not served")
			}
			if after, ok := servedNumberOf(n, partKey); !ok || after != before {
				t.Errorf("%s at %d (served %v) after the unrelated Add, want %d", partKey, after, ok, before)
			}
		})
	}
}
