// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"thingfabric.dev/go-fabric/endpoint"
	epmem "thingfabric.dev/go-fabric/endpoint/memstore"
	"thingfabric.dev/go-fabric/node"
	"thingfabric.dev/go-fabric/store/memstore"
)

// TestBatchIsJudgedAgainstItsEndState (decision 582, with 567, 575, 580,
// 581, 584): a Batch refuses an Add or a Replace only when the device
// still collides once all of the batch's calls ran — its own key or a part
// key served by another endpoint after the batch — whatever the order of
// the calls. Owner `owner` serves part `owner/light`; the batch takes a
// device declaring `owner/light` and, before or after that call, forgets
// or removes `owner` (no collision left: allowed) or forgets or removes an
// unrelated `bystander` (the owner keeps serving the key: refused with
// [node.ErrDuplicateKey], nothing of the refused device served). Both
// orders end in the same set at the same endpoint numbers. Before, an
// Add or Replace before the Forget of the owner was refused at the call
// (fix-g5e.md), the other order allowed.
func TestBatchIsJudgedAgainstItsEndState(t *testing.T) {
	t.Parallel()
	const partKey = "owner/light"
	type take struct {
		name string
		// existing, when set, is in the set before the batch.
		existing func(t *testing.T) node.Endpoint
		call     func(t *testing.T, b *node.Batch) error
		// fresh is the key of the device the batch takes.
		fresh string
	}
	takes := []take{
		{
			name: "Add with the part key", fresh: "fresh",
			call: func(t *testing.T, b *node.Batch) error {
				t.Helper()
				return b.Add(bridgedWithParts(t, "fresh", partKey))
			},
		},
		{
			name: "Add under the part key", fresh: partKey,
			call: func(t *testing.T, b *node.Batch) error {
				t.Helper()
				return b.Add(kitLight(t, partKey))
			},
		},
		{
			name: "Replace with the part key", fresh: "fresh",
			existing: func(t *testing.T) node.Endpoint { t.Helper(); return bridgedWithParts(t, "fresh") },
			call: func(t *testing.T, b *node.Batch) error {
				t.Helper()
				return b.Replace(endpoint.StringKey("fresh"), bridgedWithParts(t, "fresh", partKey))
			},
		},
	}
	type leave struct {
		name    string
		call    func(b *node.Batch) error
		allowed bool
	}
	leaves := []leave{
		{"Forget of the owner", func(b *node.Batch) error { return b.Forget(endpoint.StringKey("owner")) }, true},
		{"Remove of the owner", func(b *node.Batch) error { return b.Remove(endpoint.StringKey("owner")) }, true},
		{"Forget of a bystander", func(b *node.Batch) error { return b.Forget(endpoint.StringKey("bystander")) }, false},
		{"Remove of a bystander", func(b *node.Batch) error { return b.Remove(endpoint.StringKey("bystander")) }, false},
	}
	type outcome struct {
		duplicate             bool
		owner, part, fresh    uint16
		ownerOK, partOK, frOK bool
	}
	run := func(t *testing.T, tk take, lv leave, takeFirst bool) (outcome, error) {
		t.Helper()
		ctx := t.Context()
		cfg := minimalConfig(nil)
		cfg.Store = node.Store{Credentials: memstore.New(), Endpoints: epmem.New()}
		n := newNode(t, cfg)
		agg := n.Aggregator()
		if err := agg.Add(ctx, bridgedWithParts(t, "owner", partKey)); err != nil {
			t.Fatal(err)
		}
		if err := agg.Add(ctx, kitLight(t, "bystander")); err != nil {
			t.Fatal(err)
		}
		if tk.existing != nil {
			if err := agg.Add(ctx, tk.existing(t)); err != nil {
				t.Fatal(err)
			}
		}
		if err := n.Start(ctx); err != nil {
			t.Fatal(err)
		}
		defer func() { _ = n.Stop(context.Background()) }()
		freshBefore, freshServed := servedNumberOf(n, tk.fresh)
		partBefore, _ := servedNumberOf(n, partKey)
		err := agg.Batch(ctx, func(b *node.Batch) error {
			if takeFirst {
				return errors.Join(tk.call(t, b), lv.call(b))
			}
			return errors.Join(lv.call(b), tk.call(t, b))
		})
		var o outcome
		o.duplicate = errors.Is(err, node.ErrDuplicateKey)
		o.owner, o.ownerOK = servedNumberOf(n, "owner")
		o.part, o.partOK = servedNumberOf(n, partKey)
		o.fresh, o.frOK = servedNumberOf(n, tk.fresh)
		if !lv.allowed {
			// The refused device changed nothing: the owner keeps its
			// part at its number, the fresh key its state from before.
			if !o.ownerOK || !o.partOK || o.part != partBefore {
				t.Errorf("owner served %v, %s at %d (served %v) after the refusal, want served, at %d", o.ownerOK, partKey, o.part, o.partOK, partBefore)
			}
			if tk.fresh != partKey && (o.frOK != freshServed || (o.frOK && o.fresh != freshBefore)) {
				t.Errorf("%s served %v at %d after the refusal, want served %v at %d", tk.fresh, o.frOK, o.fresh, freshServed, freshBefore)
			}
		}
		// The set reassembles afterwards: no wedge either way.
		if aerr := agg.Add(ctx, kitLight(t, "other")); aerr != nil {
			t.Errorf("unrelated Add after the batch: %v", aerr)
		}
		return o, err
	}
	for _, tk := range takes {
		for _, lv := range leaves {
			t.Run(fmt.Sprintf("%s, %s", tk.name, lv.name), func(t *testing.T) {
				t.Parallel()
				first, errFirst := run(t, tk, lv, true)
				second, errSecond := run(t, tk, lv, false)
				for _, r := range []struct {
					order string
					err   error
					o     outcome
				}{{"take first", errFirst, first}, {"take last", errSecond, second}} {
					switch {
					case lv.allowed && r.err != nil:
						t.Errorf("%s: batch %v, want nil", r.order, r.err)
					case lv.allowed && !r.o.frOK:
						t.Errorf("%s: %s is not served after the batch", r.order, tk.fresh)
					case !lv.allowed && !r.o.duplicate:
						t.Errorf("%s: batch %v, want %v", r.order, r.err, node.ErrDuplicateKey)
					}
				}
				if first != second {
					t.Errorf("the order decides: take first %+v, take last %+v", first, second)
				}
			})
		}
	}
}

// TestBatchHeldTakeFollowsLaterCalls (decision 582): an Add or Replace a
// batch holds to its end is changed by the batch's later calls of its key
// as an applied one would be — a Remove or Forget drops it, a Replace
// takes its place — and a second device declaring a key the held one
// declares is held behind it, so of the two the earlier call wins once
// the owner is gone.
func TestBatchHeldTakeFollowsLaterCalls(t *testing.T) {
	t.Parallel()
	const partKey = "owner/light"
	fresh := endpoint.StringKey("fresh")
	for _, tc := range []struct {
		name     string
		existing bool
		fn       func(t *testing.T, b *node.Batch) error
		wantErr  error
		// served and absent are keys served, and not served, after the
		// batch; ownerKeeps: the owner still serves partKey at its number.
		served, absent []string
		ownerKeeps     bool
	}{
		{
			name: "Add then Remove", ownerKeeps: true, absent: []string{"fresh"},
			fn: func(t *testing.T, b *node.Batch) error {
				t.Helper()
				return errors.Join(b.Add(bridgedWithParts(t, "fresh", partKey)), b.Remove(fresh))
			},
		},
		{
			name: "Add then Forget", ownerKeeps: true, absent: []string{"fresh"},
			fn: func(t *testing.T, b *node.Batch) error {
				t.Helper()
				return errors.Join(b.Add(bridgedWithParts(t, "fresh", partKey)), b.Forget(fresh))
			},
		},
		{
			name: "Add then Replace without the key", ownerKeeps: true, served: []string{"fresh", "fresh/x"},
			fn: func(t *testing.T, b *node.Batch) error {
				t.Helper()
				return errors.Join(b.Add(bridgedWithParts(t, "fresh", partKey)), b.Replace(fresh, bridgedWithParts(t, "fresh", "fresh/x")))
			},
		},
		{
			name: "Replace then Replace without the key", existing: true, ownerKeeps: true, served: []string{"fresh", "fresh/x"},
			fn: func(t *testing.T, b *node.Batch) error {
				t.Helper()
				return errors.Join(b.Replace(fresh, bridgedWithParts(t, "fresh", partKey)), b.Replace(fresh, bridgedWithParts(t, "fresh", "fresh/x")))
			},
		},
		{
			name: "two takes of the key, owner forgotten", wantErr: node.ErrDuplicateKey,
			served: []string{"fresh", partKey}, absent: []string{"owner", "second"},
			fn: func(t *testing.T, b *node.Batch) error {
				t.Helper()
				return errors.Join(
					b.Add(bridgedWithParts(t, "fresh", partKey)),
					b.Add(bridgedWithParts(t, "second", partKey)),
					b.Forget(endpoint.StringKey("owner")))
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
			if err := agg.Add(ctx, bridgedWithParts(t, "owner", partKey)); err != nil {
				t.Fatal(err)
			}
			if tc.existing {
				if err := agg.Add(ctx, bridgedWithParts(t, "fresh")); err != nil {
					t.Fatal(err)
				}
			}
			if err := n.Start(ctx); err != nil {
				t.Fatal(err)
			}
			defer func() { _ = n.Stop(context.Background()) }()
			before, _ := servedNumberOf(n, partKey)
			err := agg.Batch(ctx, func(b *node.Batch) error { return tc.fn(t, b) })
			if (tc.wantErr == nil && err != nil) || (tc.wantErr != nil && !errors.Is(err, tc.wantErr)) {
				t.Errorf("batch: %v, want %v", err, tc.wantErr)
			}
			if tc.ownerKeeps {
				if _, ok := servedNumberOf(n, "owner"); !ok {
					t.Error("the owner is not served")
				}
				if got, ok := servedNumberOf(n, partKey); !ok || got != before {
					t.Errorf("%s at %d (served %v), want %d", partKey, got, ok, before)
				}
			}
			for _, k := range tc.served {
				if _, ok := servedNumberOf(n, k); !ok {
					t.Errorf("%s is not served after the batch", k)
				}
			}
			for _, k := range tc.absent {
				if _, ok := servedNumberOf(n, k); ok {
					t.Errorf("%s is served after the batch", k)
				}
			}
			if aerr := agg.Add(ctx, kitLight(t, "other")); aerr != nil {
				t.Errorf("unrelated Add after the batch: %v", aerr)
			}
		})
	}
}
