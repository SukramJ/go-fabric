// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"slices"
	"testing"

	"thingfabric.dev/go-fabric/device"
	"thingfabric.dev/go-fabric/endpoint"
	epmem "thingfabric.dev/go-fabric/endpoint/memstore"
	"thingfabric.dev/go-fabric/node"
	"thingfabric.dev/go-fabric/store/memstore"
)

// bridgedWithParts is a bridged node under key with one light per part key.
func bridgedWithParts(t *testing.T, key string, parts ...string) *device.BridgedNode {
	t.Helper()
	d, err := device.NewBridgedNode(device.Options{StableKey: endpoint.StringKey(key), Name: key})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range parts {
		if err := d.AddPart(kitLight(t, p).Device); err != nil {
			t.Fatal(err)
		}
	}
	return d
}

// hasEndpointRecord reports whether the endpoint store holds a record of key.
func hasEndpointRecord(t *testing.T, ctx context.Context, eps *epmem.Store, key string) bool {
	t.Helper()
	recs, err := eps.ListEndpoints(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range recs {
		if r.Key != nil && r.Key.String() == key {
			return true
		}
	}
	return false
}

// TestForgetSparesEveryServedEndpoint (review II, R3-store-3702): a Forget's
// erasure never takes what an endpoint serves, whichever way the endpoint
// came to serve a key of the forgotten tree — a Batch.Replace in the same
// batch, an Add of an earlier call, or an endpoint of another partition.
// The served endpoint is held out of the reassembly before the erasure and
// served again after it, so its endpoint record exists and its number holds
// across a later, unrelated reassembly.
func TestForgetSparesEveryServedEndpoint(t *testing.T) {
	t.Parallel()
	const partKey = "owner/light"
	for _, tc := range []struct {
		name string
		// setup runs after Start, with owner (part partKey) served.
		setup func(t *testing.T, ctx context.Context, n *node.Node)
		// forget forgets owner, in whatever batch the case needs.
		forget func(t *testing.T, ctx context.Context, n *node.Node) error
	}{
		{
			name:  "replace in the same batch",
			setup: func(t *testing.T, ctx context.Context, n *node.Node) {},
			forget: func(t *testing.T, ctx context.Context, n *node.Node) error {
				return n.Aggregator().Batch(ctx, func(b *node.Batch) error {
					if err := b.Forget(endpoint.StringKey("owner")); err != nil {
						return err
					}
					return b.Replace(endpoint.StringKey("x"), bridgedWithParts(t, "x", partKey))
				})
			},
		},
		{
			name: "added by an earlier call",
			setup: func(t *testing.T, ctx context.Context, n *node.Node) {
				if err := n.Aggregator().Remove(ctx, endpoint.StringKey("owner")); err != nil {
					t.Fatal(err)
				}
				if err := n.Aggregator().Add(ctx, bridgedWithParts(t, "y", partKey)); err != nil {
					t.Fatal(err)
				}
			},
			forget: func(t *testing.T, ctx context.Context, n *node.Node) error {
				return n.Aggregator().Forget(ctx, endpoint.StringKey("owner"))
			},
		},
		{
			name: "served in another partition",
			setup: func(t *testing.T, ctx context.Context, n *node.Node) {
				if err := n.Aggregator().Remove(ctx, endpoint.StringKey("owner")); err != nil {
					t.Fatal(err)
				}
				if err := n.Aggregator().Partition("q").Add(ctx, bridgedWithParts(t, "z", partKey)); err != nil {
					t.Fatal(err)
				}
			},
			forget: func(t *testing.T, ctx context.Context, n *node.Node) error {
				return n.Aggregator().Forget(ctx, endpoint.StringKey("owner"))
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			eps := epmem.New()
			cfg := minimalConfig(nil)
			cfg.Store = node.Store{Credentials: memstore.New(), Endpoints: eps}
			n := newNode(t, cfg)
			if err := n.Aggregator().Add(ctx, bridgedWithParts(t, "owner", partKey)); err != nil {
				t.Fatal(err)
			}
			if err := n.Aggregator().Add(ctx, bridgedWithParts(t, "x")); err != nil {
				t.Fatal(err)
			}
			if err := n.Start(ctx); err != nil {
				t.Fatal(err)
			}
			tc.setup(t, ctx, n)
			if err := tc.forget(t, ctx, n); err != nil {
				t.Fatal(err)
			}
			first, ok := servedNumberOf(n, partKey)
			if !ok {
				t.Fatalf("%s is not served after the Forget", partKey)
			}
			if !hasEndpointRecord(t, ctx, eps, partKey) {
				t.Errorf("the Forget erased the endpoint record of the served key %s", partKey)
			}
			if err := n.Aggregator().Add(ctx, kitLight(t, "other")); err != nil {
				t.Fatal(err)
			}
			if second, _ := servedNumberOf(n, partKey); second != first {
				t.Errorf("served endpoint %s moved from %d to %d at an unrelated reassembly", partKey, first, second)
			}
		})
	}
}

// TestForgetBatchKeepsRetiredPartitionRetired (review II, R3-store-3701):
// a partition retired inside a batch that forgets its last endpoint stays
// retired after the batch, started or not; before Start, Start's assembly
// then erases what the retired scope stored.
func TestForgetBatchKeepsRetiredPartitionRetired(t *testing.T) {
	t.Parallel()
	for _, started := range []bool{false, true} {
		ctx := t.Context()
		cfg := minimalConfig(nil)
		cfg.Store = node.Store{Credentials: memstore.New(), Endpoints: epmem.New()}
		n := newNode(t, cfg)
		p := n.Aggregator().Partition("p")
		if err := p.Add(ctx, kitLight(t, "p/one")); err != nil {
			t.Fatal(err)
		}
		if started {
			if err := n.Start(ctx); err != nil {
				t.Fatal(err)
			}
		}
		err := p.Batch(ctx, func(b *node.Batch) error {
			if err := b.Forget(endpoint.StringKey("p/one")); err != nil {
				return err
			}
			return n.Aggregator().RetirePartition(ctx, "p")
		})
		if err != nil {
			t.Fatalf("started=%v: %v", started, err)
		}
		if parts := n.Aggregator().Partitions(); slices.Contains(parts, "p") {
			t.Errorf("started=%v: retired partition p listed again after the batch: %v", started, parts)
		}
	}
}

// TestForgetBatchBeforeStartKeepsTheRetirement (review II, R3-store-3701):
// on a node not yet started, a retirement inside a Forget batch still
// reaches Start's assembly, which erases the numbers the scope stored.
func TestForgetBatchBeforeStartKeepsTheRetirement(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	eps := epmem.New()
	creds := memstore.New()
	cfg := minimalConfig(nil)
	cfg.Store = node.Store{Credentials: creds, Endpoints: eps}
	first := newNode(t, cfg)
	p := first.Aggregator().Partition("p")
	for _, k := range []string{"p/one", "p/two"} {
		if err := p.Add(ctx, kitLight(t, k)); err != nil {
			t.Fatal(err)
		}
	}
	if err := first.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := first.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if !hasEndpointRecord(t, ctx, eps, "p/two") {
		t.Fatal("no record of p/two after the first run")
	}
	cfg2 := minimalConfig(nil)
	cfg2.Store = node.Store{Credentials: creds, Endpoints: eps}
	n := newNode(t, cfg2)
	err := n.Aggregator().Partition("p").Batch(ctx, func(b *node.Batch) error {
		if err := b.Forget(endpoint.StringKey("p/one")); err != nil {
			return err
		}
		return n.Aggregator().RetirePartition(ctx, "p")
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := n.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if hasEndpointRecord(t, ctx, eps, "p/two") {
		t.Error("the retired scope's record p/two survived Start's assembly")
	}
}
