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

// TestForgetLetsANewDeviceTakeAFormerKeyNoEndpointServes (review II,
// R3-store-3702 edge, decision 575): a key of a forgotten device — a part
// of its reservation, or of its tree once its Forget ran — that no
// endpoint serves any more may be taken by a different device added in
// the same batch, whichever of Add and Forget comes first and also
// through a Replace of that device (Fable B1 of the G5c diff read, now
// allowed). The new device is served after the erasure, the erasure
// leaves the served key's endpoint record, and its number holds across a
// later, unrelated reassembly, as TestForgetAndAddFormerPartKeyInOneBatch
// (R2-C2-store-2401) asserts for the plain Forget-then-Add order.
func TestForgetLetsANewDeviceTakeAFormerKeyNoEndpointServes(t *testing.T) {
	t.Parallel()
	const partKey = "owner/light"
	forgetOwner := func(b *node.Batch) error { return b.Forget(endpoint.StringKey("owner")) }
	for _, tc := range []struct {
		name string
		// removeFirst removes owner in a call before the batch, so its
		// part key is in its reservation, not in the set.
		removeFirst bool
		build       func(t *testing.T) node.Endpoint
		forgetFirst bool
		// replaceWith, when set, is the Replace of fresh that follows the
		// Forget and the Add in the batch.
		replaceWith func(t *testing.T) node.Endpoint
	}{
		{
			name: "part key of the reservation", removeFirst: true, forgetFirst: true,
			build: func(t *testing.T) node.Endpoint { t.Helper(); return bridgedWithParts(t, "fresh", partKey) },
		},
		{
			name: "new device added before the Forget", removeFirst: true, forgetFirst: false,
			build: func(t *testing.T) node.Endpoint { t.Helper(); return bridgedWithParts(t, "fresh", partKey) },
		},
		{
			name: "Replace of a new device added before the Forget", forgetFirst: false,
			build:       func(t *testing.T) node.Endpoint { t.Helper(); return bridgedWithParts(t, "fresh") },
			replaceWith: func(t *testing.T) node.Endpoint { t.Helper(); return bridgedWithParts(t, "fresh", partKey) },
		},
		{
			name: "Replace of a new device added after the Forget", forgetFirst: true,
			build:       func(t *testing.T) node.Endpoint { t.Helper(); return bridgedWithParts(t, "fresh") },
			replaceWith: func(t *testing.T) node.Endpoint { t.Helper(); return bridgedWithParts(t, "fresh", partKey) },
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
			if err := n.Start(ctx); err != nil {
				t.Fatal(err)
			}
			if tc.removeFirst {
				if err := n.Aggregator().Remove(ctx, endpoint.StringKey("owner")); err != nil {
					t.Fatal(err)
				}
			}
			err := n.Aggregator().Batch(ctx, func(b *node.Batch) error {
				var forgetErr, addErr, replaceErr error
				if tc.forgetFirst {
					forgetErr = forgetOwner(b)
					addErr = b.Add(tc.build(t))
				} else {
					addErr = b.Add(tc.build(t))
					forgetErr = forgetOwner(b)
				}
				if tc.replaceWith != nil {
					replaceErr = b.Replace(endpoint.StringKey("fresh"), tc.replaceWith(t))
				}
				return errors.Join(forgetErr, addErr, replaceErr)
			})
			if err != nil {
				t.Fatalf("batch: %v, want nil", err)
			}
			first, ok := servedNumberOf(n, partKey)
			if !ok {
				t.Fatalf("%s is not served after the batch", partKey)
			}
			if _, ok := servedNumberOf(n, "fresh"); !ok {
				t.Error("the new device is not served after the batch")
			}
			if !hasEndpointRecord(t, eps, partKey) {
				t.Errorf("the batch's Forget erased the endpoint record of the served key %s", partKey)
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
