// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"testing"

	closurecontroldef "thingfabric.dev/go-fabric/cluster/spec/closurecontrol"
	"thingfabric.dev/go-fabric/device"
	"thingfabric.dev/go-fabric/endpoint"
	epmem "thingfabric.dev/go-fabric/endpoint/memstore"
	"thingfabric.dev/go-fabric/node"
	"thingfabric.dev/go-fabric/store/memstore"
	"thingfabric.dev/go-fabric/surface"
)

// batchNode is a started node on memory stores whose default partition
// holds eps, in order; it stops with the test.
func batchNode(t *testing.T, eps ...node.Endpoint) *node.Node {
	t.Helper()
	ctx := t.Context()
	cfg := minimalConfig(nil)
	cfg.Store = node.Store{Credentials: memstore.New(), Endpoints: epmem.New()}
	n := newNode(t, cfg)
	for _, ep := range eps {
		if err := n.Aggregator().Add(ctx, ep); err != nil {
			t.Fatal(err)
		}
	}
	if err := n.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = n.Stop(context.Background()) })
	return n
}

// TestBatchOwnKeyIsJudgedAtCommit (decision 585 (1), completing 582): an
// Add of a key the set holds is judged against the batch's end state like
// a part key another endpoint serves (decision 580): a Remove or Forget of
// the held key in the same batch, before or after the Add, lets the new
// device in; a Replace of the key, before or after, leaves it refused with
// [node.ErrDuplicateKey]. Before, the Add was refused at the call, so
// Add-then-Remove lost the new device while Remove-then-Add served it.
func TestBatchOwnKeyIsJudgedAtCommit(t *testing.T) {
	t.Parallel()
	const key = "k"
	k := endpoint.StringKey(key)
	for _, tc := range []struct {
		name string
		// other is the batch's second call; next is the device the
		// batch's Add takes, repl the Replace's.
		other   func(t *testing.T, b *node.Batch, repl node.Endpoint) error
		wantErr error
		// wantNew: the Add's device is served after the batch; wantRepl:
		// the Replace's.
		wantNew, wantRepl bool
	}{
		{name: "Remove", other: func(t *testing.T, b *node.Batch, _ node.Endpoint) error { t.Helper(); return b.Remove(k) }, wantNew: true},
		{name: "Forget", other: func(t *testing.T, b *node.Batch, _ node.Endpoint) error { t.Helper(); return b.Forget(k) }, wantNew: true},
		{name: "Replace", other: func(t *testing.T, b *node.Batch, repl node.Endpoint) error { t.Helper(); return b.Replace(k, repl) }, wantErr: node.ErrDuplicateKey, wantRepl: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			type outcome struct {
				duplicate, served bool
				number            uint16
				isNew, isRepl     bool
			}
			run := func(addFirst bool) outcome {
				ctx := t.Context()
				n := batchNode(t, kitLight(t, key), kitLight(t, "bystander"))
				next, repl := kitLight(t, key), kitLight(t, key)
				err := n.Aggregator().Batch(ctx, func(b *node.Batch) error {
					if addFirst {
						return errors.Join(b.Add(next), tc.other(t, b, repl))
					}
					return errors.Join(tc.other(t, b, repl), b.Add(next))
				})
				if (tc.wantErr == nil && err != nil) || (tc.wantErr != nil && !errors.Is(err, tc.wantErr)) {
					t.Errorf("add first %v: batch %v, want %v", addFirst, err, tc.wantErr)
				}
				var o outcome
				o.duplicate = errors.Is(err, node.ErrDuplicateKey)
				o.number, o.served = servedNumberOf(n, key)
				eps := n.Aggregator().Endpoints()
				o.isNew = slices.Contains(eps, node.Endpoint(next))
				o.isRepl = slices.Contains(eps, node.Endpoint(repl))
				if o.isNew != tc.wantNew || o.isRepl != tc.wantRepl || !o.served {
					t.Errorf("add first %v: new device held %v (want %v), replacement held %v (want %v), %s served %v",
						addFirst, o.isNew, tc.wantNew, o.isRepl, tc.wantRepl, key, o.served)
				}
				if aerr := n.Aggregator().Add(ctx, kitLight(t, "later")); aerr != nil {
					t.Errorf("add first %v: unrelated Add after the batch: %v", addFirst, aerr)
				}
				return o
			}
			if first, last := run(true), run(false); first != last {
				t.Errorf("the order decides: Add first %+v, Add last %+v", first, last)
			}
		})
	}
}

// TestBatchForgetOfAServedPartKeyIsJudgedAtCommit (decision 585 (2),
// completing 582 for 567): a Forget of a part key another endpoint serves
// is judged against the batch's end state: a Remove or Forget of the
// owner in the same batch, before or after, lets it through; a Remove of
// a bystander leaves it refused with [node.ErrUnknownKey] and the owner
// serving its part at its number. Before, Forget(owner/light) then
// Remove(owner) was refused at the call, the reverse order allowed.
func TestBatchForgetOfAServedPartKeyIsJudgedAtCommit(t *testing.T) {
	t.Parallel()
	const partKey = "owner/light"
	part := endpoint.StringKey(partKey)
	for _, tc := range []struct {
		name    string
		leave   func(b *node.Batch) error
		allowed bool
	}{
		{"Remove of the owner", func(b *node.Batch) error { return b.Remove(endpoint.StringKey("owner")) }, true},
		{"Forget of the owner", func(b *node.Batch) error { return b.Forget(endpoint.StringKey("owner")) }, true},
		{"Remove of a bystander", func(b *node.Batch) error { return b.Remove(endpoint.StringKey("bystander")) }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			type outcome struct {
				unknown                 bool
				owner, part             uint16
				ownerOK, partOK         bool
				backOwner, backPart     uint16
				backOwnerOK, backPartOK bool
			}
			run := func(forgetFirst bool) outcome {
				ctx := t.Context()
				n := batchNode(t, bridgedWithParts(t, "owner", partKey), kitLight(t, "bystander"))
				agg := n.Aggregator()
				partBefore, _ := servedNumberOf(n, partKey)
				err := agg.Batch(ctx, func(b *node.Batch) error {
					if forgetFirst {
						return errors.Join(b.Forget(part), tc.leave(b))
					}
					return errors.Join(tc.leave(b), b.Forget(part))
				})
				var o outcome
				o.unknown = errors.Is(err, node.ErrUnknownKey)
				o.owner, o.ownerOK = servedNumberOf(n, "owner")
				o.part, o.partOK = servedNumberOf(n, partKey)
				switch {
				case tc.allowed && err != nil:
					t.Errorf("forget first %v: batch %v, want nil", forgetFirst, err)
				case !tc.allowed && !o.unknown:
					t.Errorf("forget first %v: batch %v, want %v", forgetFirst, err, node.ErrUnknownKey)
				case !tc.allowed && (!o.partOK || o.part != partBefore):
					t.Errorf("forget first %v: %s at %d (served %v) after the refusal, want %d", forgetFirst, partKey, o.part, o.partOK, partBefore)
				}
				if tc.allowed {
					// The owner returns: the numbers it gets show what the
					// batch kept and what it erased.
					if aerr := agg.Add(ctx, bridgedWithParts(t, "owner", partKey)); aerr != nil {
						t.Errorf("forget first %v: owner back: %v", forgetFirst, aerr)
					}
					o.backOwner, o.backOwnerOK = servedNumberOf(n, "owner")
					o.backPart, o.backPartOK = servedNumberOf(n, partKey)
				}
				if aerr := agg.Add(ctx, kitLight(t, "later")); aerr != nil {
					t.Errorf("forget first %v: unrelated Add after the batch: %v", forgetFirst, aerr)
				}
				return o
			}
			if first, last := run(true), run(false); first != last {
				t.Errorf("the order decides: Forget first %+v, Forget last %+v", first, last)
			}
		})
	}
}

// TestBatchCommitJudgesTheDeviceAsItIs (decision 585 (3), fable-diff-g5f
// N1, probe P1): a held Add is judged at commit by the device as it is
// then, not by its surface at the call — a part added to the still
// unbound device during fn under a key another endpoint serves refuses it
// with [node.ErrDuplicateKey], and the set is not wedged. Before, the
// commit used the call's entries, took the device in, and every later
// reassembly failed on two Specs under one key.
func TestBatchCommitJudgesTheDeviceAsItIs(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	n := batchNode(t, bridgedWithParts(t, "owner", "owner/light"), bridgedWithParts(t, "other", "other/light"))
	agg := n.Aggregator()
	otherAt, _ := servedNumberOf(n, "other/light")
	fresh := bridgedWithParts(t, "fresh", "owner/light")
	err := agg.Batch(ctx, func(b *node.Batch) error {
		if err := b.Add(fresh); err != nil {
			return err
		}
		if err := fresh.AddPart(kitLight(t, "other/light").Device); err != nil {
			return err
		}
		return b.Forget(endpoint.StringKey("owner"))
	})
	if !errors.Is(err, node.ErrDuplicateKey) {
		t.Errorf("batch: %v, want %v", err, node.ErrDuplicateKey)
	}
	if _, ok := servedNumberOf(n, "fresh"); ok {
		t.Error("fresh is served though another endpoint serves its part other/light")
	}
	if got, ok := servedNumberOf(n, "other/light"); !ok || got != otherAt {
		t.Errorf("other/light at %d (served %v), want %d", got, ok, otherAt)
	}
	if aerr := agg.Add(ctx, kitLight(t, "later")); aerr != nil {
		t.Errorf("unrelated Add after the batch: %v (wedged)", aerr)
	}
}

// TestBatchUsedAfterFnIsRefused (decision 585 (4), fable-diff-g5f N2,
// probe P4): a Batch kept past its fn refuses every call with
// [node.ErrBatchDone] and changes nothing. Before, a colliding Add
// returned nil and was never judged, and a plain Add entered the set
// unreassembled, served by whichever reassembly came next.
func TestBatchUsedAfterFnIsRefused(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	n := batchNode(t, bridgedWithParts(t, "owner", "owner/light"), kitLight(t, "k"))
	agg := n.Aggregator()
	var kept *node.Batch
	if err := agg.Batch(ctx, func(b *node.Batch) error { kept = b; return nil }); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name string
		call func() error
	}{
		{"Add", func() error { return kept.Add(kitLight(t, "late")) }},
		{"colliding Add", func() error { return kept.Add(bridgedWithParts(t, "late2", "owner/light")) }},
		{"Replace", func() error { return kept.Replace(endpoint.StringKey("k"), kitLight(t, "k")) }},
		{"Remove", func() error { return kept.Remove(endpoint.StringKey("k")) }},
		{"Forget", func() error { return kept.Forget(endpoint.StringKey("k")) }},
	} {
		if err := c.call(); !errors.Is(err, node.ErrBatchDone) {
			t.Errorf("%s after fn: %v, want %v", c.name, err, node.ErrBatchDone)
		}
	}
	if aerr := agg.Add(ctx, kitLight(t, "later")); aerr != nil {
		t.Fatal(aerr)
	}
	for _, key := range []string{"late", "late2"} {
		if _, ok := servedNumberOf(n, key); ok {
			t.Errorf("%s is served after a call on a finished batch", key)
		}
	}
	if _, ok := servedNumberOf(n, "k"); !ok {
		t.Error("k is not served after calls on a finished batch")
	}
	if got := len(agg.Endpoints()); got != 3 {
		t.Errorf("the set holds %d endpoints, want 3 (owner, k, later)", got)
	}
}

// TestBatchRemoveOfAHeldAddDropsItsWithheldRecord (decision 585 (5),
// fable-diff-g5f N5): a Remove of an Add the batch holds drops a withheld
// record of the key from before the batch, as it does after an applied
// Add (whose served declaration clears the record) and as Forget does.
func TestBatchRemoveOfAHeldAddDropsItsWithheldRecord(t *testing.T) {
	low := surface.Policies{Level: "1.4.2"}
	const key = "k"
	bridged := func(k string, parts ...string) node.Endpoint {
		t.Helper()
		d, err := device.NewBridgedNode(device.Options{StableKey: endpoint.StringKey(k), Name: k, Surface: low})
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range parts {
			if err := d.AddPart(light(t, p, &low).Device); err != nil {
				t.Fatal(err)
			}
		}
		return d
	}
	db := openDB(t, filepath.Join(t.TempDir(), "held-withheld.db"))
	cfg := minimalConfig(db)
	cfg.Surface = low
	n := newNode(t, cfg)
	ctx := context.Background()
	agg := n.Aggregator()
	if err := agg.Add(ctx, bridged("owner", "owner/light")); err != nil {
		t.Fatal(err)
	}
	closure, err := device.NewClosure(device.Options{StableKey: endpoint.StringKey(key), Name: key, Surface: low},
		closurecontroldef.Declare{Features: closurecontroldef.FeaturePositioning})
	if closure == nil {
		t.Fatalf("closure: %v", err)
	}
	if err := agg.Add(ctx, closure); !errors.Is(err, surface.ErrWithheld) {
		t.Fatalf("Add of a withheld declaration: %v, want ErrWithheld", err)
	}
	if err := n.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = n.Stop(ctx) }()
	withheld := func() bool {
		for _, w := range n.Surface().Withheld {
			if w.Endpoint == key {
				return true
			}
		}
		return false
	}
	if !withheld() {
		t.Fatal("the key is not listed as withheld before the batch")
	}
	err = agg.Batch(ctx, func(b *node.Batch) error {
		return errors.Join(b.Add(bridged(key, "owner/light")), b.Remove(endpoint.StringKey(key)))
	})
	if err != nil {
		t.Fatalf("batch: %v", err)
	}
	if withheld() {
		t.Error("the key is still listed as withheld after the Remove of its held Add")
	}
	if _, ok := servedNumberOf(n, key); ok {
		t.Error("the removed key is served")
	}
}
