// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"errors"
	"testing"

	"thingfabric.dev/go-fabric/endpoint"
	epmem "thingfabric.dev/go-fabric/endpoint/memstore"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/node"
	"thingfabric.dev/go-fabric/store/memstore"
)

// TestForgetRefusesAPartKeyAnotherEndpointServes (review II, R3-store-3702
// edge, decision 567): a part key forgotten by name while another
// endpoint, of any partition, serves it is refused as a key another
// partition holds is — [node.ErrUnknownKey] — and the call changes
// nothing: the part stays served at its number with its stored values.
func TestForgetRefusesAPartKeyAnotherEndpointServes(t *testing.T) {
	t.Parallel()
	const partKey = "owner/light"
	for _, tc := range []struct {
		name string
		// owner is the partition owner (part partKey) is added to,
		// forget the one the Forget of partKey runs in.
		owner, forget string
	}{
		{"same partition", node.Scope, node.Scope},
		{"owner in another partition", "q", node.Scope},
		{"forgotten in the owner's partition, owner elsewhere", node.Scope, "q"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			creds := &keyRecorder{Port: memstore.New()}
			eps := epmem.New()
			cfg := minimalConfig(nil)
			cfg.Store = node.Store{Credentials: creds, Endpoints: eps}
			n := newNode(t, cfg)
			if err := n.Aggregator().Partition(tc.owner).Add(ctx, bridgedWithParts(t, "owner", partKey)); err != nil {
				t.Fatal(err)
			}
			if tc.forget != tc.owner {
				// The Forget's partition holds an endpoint of its own.
				if err := n.Aggregator().Partition(tc.forget).Add(ctx, kitLight(t, "bystander")); err != nil {
					t.Fatal(err)
				}
			}
			if err := n.Start(ctx); err != nil {
				t.Fatal(err)
			}
			before, ok := servedNumberOf(n, partKey)
			if !ok {
				t.Fatalf("%s is not served before the Forget", partKey)
			}
			mark := creds.mark()
			if st := dispatchWrite(t, n, attrPath(before, 0x0006, 0x4003), uint64(1)); st != im.StatusSuccess {
				t.Fatalf("StartUpOnOff write on %s: %v", partKey, st)
			}
			stored := creds.written(mark)
			err := n.Aggregator().Partition(tc.forget).Forget(ctx, endpoint.StringKey(partKey))
			if !errors.Is(err, node.ErrUnknownKey) {
				t.Fatalf("Forget of the served part key %s: %v, want %v", partKey, err, node.ErrUnknownKey)
			}
			if after, ok := servedNumberOf(n, partKey); !ok || after != before {
				t.Errorf("%s at %d (served %v) after the refused Forget, want %d", partKey, after, ok, before)
			}
			if !hasEndpointRecord(t, eps, partKey) {
				t.Errorf("the refused Forget erased the endpoint record of %s", partKey)
			}
			for _, k := range stored {
				if v, found, err := creds.GetSetting(ctx, k); err != nil || !found || v == "" {
					t.Errorf("stored %s after the refused Forget: %q (found %v, %v), want kept", k, v, found, err)
				}
			}
		})
	}
}

// TestForgetRefusesAKeyANewDeviceTakesInTheBatch (review II, R3-store-3702
// edge, decision 567): a key of a forgotten device — a part of its tree
// or of its reservation — that a different device added in the same
// batch takes, as its own key or a part key, refuses the batch as a key
// another partition holds is: the call that would take it returns the
// error that call returns for a key of another partition, an Add
// [node.ErrDuplicateKey], a Forget [node.ErrUnknownKey]. The new device
// is not served. The forgotten device added again in the batch is no
// different device (TestForgetAndAddInOneBatchStartsNew). The first two
// cases were TestForgetAndAddFormerPartKeyInOneBatch (R2-C2-store-2401),
// which served the new device at the forgotten part's number.
func TestForgetRefusesAKeyANewDeviceTakesInTheBatch(t *testing.T) {
	t.Parallel()
	const partKey = "owner/light"
	forgetOwner := func(b *node.Batch) error { return b.Forget(endpoint.StringKey("owner")) }
	for _, tc := range []struct {
		name string
		// removeFirst removes owner in a call before the batch, so its
		// part key is in its reservation, not in the set.
		removeFirst bool
		newKey      string
		build       func(t *testing.T) node.Endpoint
		forgetFirst bool
		want        error
	}{
		{
			name: "part key of a new device", newKey: "fresh", forgetFirst: true, want: node.ErrDuplicateKey,
			build: func(t *testing.T) node.Endpoint { t.Helper(); return bridgedWithParts(t, "fresh", partKey) },
		},
		{
			name: "own key of a new device", newKey: partKey, forgetFirst: true, want: node.ErrDuplicateKey,
			build: func(t *testing.T) node.Endpoint { t.Helper(); return kitLight(t, partKey) },
		},
		{
			name: "part key of the reservation", removeFirst: true, newKey: "fresh", forgetFirst: true, want: node.ErrDuplicateKey,
			build: func(t *testing.T) node.Endpoint { t.Helper(); return bridgedWithParts(t, "fresh", partKey) },
		},
		{
			name: "new device added before the Forget", removeFirst: true, newKey: "fresh", forgetFirst: false, want: node.ErrUnknownKey,
			build: func(t *testing.T) node.Endpoint { t.Helper(); return bridgedWithParts(t, "fresh", partKey) },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			cfg := minimalConfig(nil)
			cfg.Store = node.Store{Credentials: memstore.New(), Endpoints: epmem.New()}
			n := newNode(t, cfg)
			if err := n.Aggregator().Add(ctx, bridgedWithParts(t, "owner", partKey)); err != nil {
				t.Fatal(err)
			}
			if err := n.Start(ctx); err != nil {
				t.Fatal(err)
			}
			if tc.removeFirst {
				if err := n.Aggregator().Remove(ctx, endpoint.StringKey("owner")); err != nil {
					t.Fatal(err)
				}
			}
			var forgetErr, addErr error
			err := n.Aggregator().Batch(ctx, func(b *node.Batch) error {
				if tc.forgetFirst {
					forgetErr = forgetOwner(b)
					addErr = b.Add(tc.build(t))
				} else {
					addErr = b.Add(tc.build(t))
					forgetErr = forgetOwner(b)
				}
				return errors.Join(forgetErr, addErr)
			})
			if !errors.Is(err, tc.want) {
				t.Fatalf("batch: %v, want %v", err, tc.want)
			}
			refused := addErr
			if !tc.forgetFirst {
				refused = forgetErr
			}
			if !errors.Is(refused, tc.want) {
				t.Errorf("the call that takes %s: %v, want %v (Forget %v, Add %v)", partKey, refused, tc.want, forgetErr, addErr)
			}
			if tc.forgetFirst {
				if _, ok := servedNumberOf(n, tc.newKey); ok {
					t.Errorf("the refused new device %s is served", tc.newKey)
				}
			}
		})
	}
}
