// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"thingfabric.dev/go-fabric/device"
	"thingfabric.dev/go-fabric/endpoint"
	epmem "thingfabric.dev/go-fabric/endpoint/memstore"
	"thingfabric.dev/go-fabric/node"
	"thingfabric.dev/go-fabric/store/memstore"
)

// failingLabelReads is an EndpointState whose NodeLabel reads fail while
// fails is positive, one read per count: a reassembly's snapshot reads
// every endpoint's label and stops at the first failure, so a count of
// one fails exactly one reassembly.
type failingLabelReads struct {
	fails atomic.Int32
	reads atomic.Int32
}

var errLabelRead = errors.New("label read refused")

func (*failingLabelReads) SaveNodeLabel(context.Context, device.SourceKey, string) error {
	return nil
}

func (f *failingLabelReads) LoadNodeLabel(context.Context, device.SourceKey) (string, error) {
	f.reads.Add(1)
	if f.fails.Add(-1) >= 0 {
		return "", errLabelRead
	}
	f.fails.Store(0)
	return "", nil
}

func (*failingLabelReads) LoadScenes(context.Context, endpoint.SourceKey) ([]byte, error) {
	return nil, nil
}

func (*failingLabelReads) SaveScenes(context.Context, endpoint.SourceKey, []byte) error { return nil }

// behindNode is a started node over an in-memory store whose label reads
// labels fails on demand, serving one light per key.
func behindNode(t *testing.T, keys ...string) (*node.Node, *failingLabelReads, *epmem.Store, map[string]*device.OnOffLight) {
	t.Helper()
	labels := &failingLabelReads{}
	eps := epmem.New()
	cfg := minimalConfig(nil)
	cfg.Store = node.Store{Credentials: memstore.New(), Endpoints: eps}
	cfg.EndpointState = labels
	n := newNode(t, cfg)
	lights := map[string]*device.OnOffLight{}
	for _, k := range keys {
		l := kitLight(t, k)
		lights[k] = l
		if err := n.Aggregator().Add(t.Context(), l); err != nil {
			t.Fatal(err)
		}
	}
	if err := n.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	return n, labels, eps, lights
}

// TestFailedReassemblyKeepsRemovedDevicesBound (decision 722): a batch
// whose reassembly fails leaves the topology before on the wire, so a
// device it removed is still served — and must still be bound to the node
// and keep what is stored for it until a reassembly takes its endpoint
// off the wire. Measured at b89d1b774 before the fix: the removed device
// was unbound (EndpointNumber false) while still served, and a forgotten
// key's endpoint record was erased while still served.
func TestFailedReassemblyKeepsRemovedDevicesBound(t *testing.T) {
	ctx := t.Context()
	n, labels, eps, lights := behindNode(t, "a", "b", "c")
	agg := n.Aggregator()
	labels.fails.Store(1)
	err := agg.Batch(ctx, func(b *node.Batch) error {
		if err := b.Remove(endpoint.StringKey("a")); err != nil {
			return err
		}
		return b.Forget(endpoint.StringKey("b"))
	})
	if !errors.Is(err, errLabelRead) {
		t.Fatalf("Batch: %v; want the label read's error", err)
	}
	for _, k := range []string{"a", "b"} {
		if _, ok := servedNumberOf(n, k); !ok {
			t.Fatalf("%s is off the wire after the failed reassembly; the topology before must stay served", k)
		}
		if _, ok := lights[k].EndpointNumber(); !ok {
			t.Errorf("%s is served but unbound after the failed reassembly", k)
		}
	}
	if !hasEndpointRecord(t, eps, "b") {
		t.Error("the erasure of b ran while b is still served")
	}
	// The next batch, an empty one, reassembles: a and b leave the wire,
	// then they are unbound and b's records are erased.
	if err := agg.Batch(ctx, func(*node.Batch) error { return nil }); err != nil {
		t.Fatalf("the healing batch: %v", err)
	}
	for _, k := range []string{"a", "b"} {
		if _, ok := servedNumberOf(n, k); ok {
			t.Errorf("%s still served after the healing reassembly", k)
		}
		if _, ok := lights[k].EndpointNumber(); ok {
			t.Errorf("%s still bound after the healing reassembly", k)
		}
	}
	if hasEndpointRecord(t, eps, "b") {
		t.Error("b's record survived the healing reassembly; its erasure did not run")
	}
	if !hasEndpointRecord(t, eps, "a") {
		t.Error("a's record is gone; a Remove keeps it")
	}
	if _, ok := servedNumberOf(n, "c"); !ok {
		t.Error("c is not served")
	}
}

// reassemblyErr asserts err is a *node.BatchError with a reassembly error
// carrying the label read's refusal.
func reassemblyErr(t *testing.T, err error) *node.BatchError {
	t.Helper()
	var be *node.BatchError
	if !errors.As(err, &be) || be.Reassembly == nil || !errors.Is(err, errLabelRead) {
		t.Fatalf("Batch: %v (%T); want a *node.BatchError with the reassembly's error", err, err)
	}
	return be
}

// TestFailedReassemblyIsRedoneByTheNextBatch (issue #633, decision 718): a
// store that fails one reassembly's read fails the batch with its error
// and leaves the topology before on the wire; the next batch, an empty
// one, reassembles and serves the set. A failure repeated is returned by
// that empty batch too, and the one after it heals.
func TestFailedReassemblyIsRedoneByTheNextBatch(t *testing.T) {
	for _, repeats := range []int32{1, 2} {
		t.Run(map[int32]string{1: "once", 2: "twice"}[repeats], func(t *testing.T) {
			ctx := t.Context()
			n, labels, _, _ := behindNode(t, "a")
			agg := n.Aggregator()
			labels.fails.Store(repeats)
			be := reassemblyErr(t, agg.Batch(ctx, func(b *node.Batch) error {
				return b.Add(kitLight(t, "new"))
			}))
			if be.Fn != nil || be.Calls != nil || be.Store != nil {
				t.Errorf("BatchError %+v; want only the reassembly's error", be)
			}
			if _, ok := servedNumberOf(n, "new"); ok {
				t.Fatal("new is served after the failed reassembly")
			}
			if len(agg.Endpoints()) != 2 {
				t.Fatalf("the set holds %d endpoints; the Add stays applied", len(agg.Endpoints()))
			}
			empty := func(*node.Batch) error { return nil }
			for range repeats - 1 {
				reassemblyErr(t, agg.Batch(ctx, empty))
				if _, ok := servedNumberOf(n, "new"); ok {
					t.Fatal("new is served after a failed reassembly")
				}
			}
			before := node.Reassemblies(agg)
			if err := agg.Batch(ctx, empty); err != nil {
				t.Fatalf("the healing batch: %v", err)
			}
			if _, ok := servedNumberOf(n, "new"); !ok {
				t.Error("new is not served after the healing batch")
			}
			if err := agg.Batch(ctx, empty); err != nil {
				t.Fatalf("an empty batch after the healing one: %v", err)
			}
			if got := node.Reassemblies(agg) - before; got != 1 {
				t.Errorf("%d reassemblies for the healing batch and an empty one after it; want 1", got)
			}
		})
	}
}

// TestConcurrentEmptyBatchesReassembleOnce (decision 718): two empty
// batches that both find the topology behind reassemble once — the second
// judges again under the reassembly's lock and finds it healed.
func TestConcurrentEmptyBatchesReassembleOnce(t *testing.T) {
	ctx := t.Context()
	n, labels, _, _ := behindNode(t, "a")
	agg := n.Aggregator()
	for i := range 20 {
		labels.fails.Store(1)
		reassemblyErr(t, agg.Batch(ctx, func(b *node.Batch) error {
			return b.Add(kitLight(t, "k"+string(rune('a'+i))+"x"))
		}))
		before := node.Reassemblies(agg)
		var ready sync.WaitGroup
		ready.Add(2)
		errs := make(chan error, 2)
		for range 2 {
			go func() {
				errs <- agg.Batch(ctx, func(*node.Batch) error {
					// Both are inside fn before either reassembles.
					ready.Done()
					ready.Wait()
					return nil
				})
			}()
		}
		for range 2 {
			if err := <-errs; err != nil {
				t.Fatalf("round %d: empty batch: %v", i, err)
			}
		}
		if got := node.Reassemblies(agg) - before; got != 1 {
			t.Fatalf("round %d: %d reassemblies for two concurrent empty batches; want 1", i, got)
		}
	}
}

// TestRetirePartitionRedoesAFailedReassembly (decision 718, 722):
// RetirePartition's reassembly heals a failed one and runs the unbinding
// the failed batch left.
func TestRetirePartitionRedoesAFailedReassembly(t *testing.T) {
	ctx := t.Context()
	n, labels, _, lights := behindNode(t, "a", "b")
	agg := n.Aggregator()
	if err := agg.Partition("old").Add(ctx, kitLight(t, "o")); err != nil {
		t.Fatal(err)
	}
	if err := agg.Partition("old").Remove(ctx, endpoint.StringKey("o")); err != nil {
		t.Fatal(err)
	}
	labels.fails.Store(1)
	reassemblyErr(t, agg.Batch(ctx, func(b *node.Batch) error {
		if err := b.Remove(endpoint.StringKey("a")); err != nil {
			return err
		}
		return b.Add(kitLight(t, "new"))
	}))
	if _, ok := lights["a"].EndpointNumber(); !ok {
		t.Fatal("a is unbound while served")
	}
	if err := agg.RetirePartition(ctx, "old"); err != nil {
		t.Fatalf("RetirePartition: %v", err)
	}
	if _, ok := servedNumberOf(n, "new"); !ok {
		t.Error("new is not served after RetirePartition")
	}
	if _, ok := servedNumberOf(n, "a"); ok {
		t.Error("a is served after RetirePartition")
	}
	if _, ok := lights["a"].EndpointNumber(); ok {
		t.Error("a is still bound after RetirePartition")
	}
}

// TestStopUnbindsWhatAFailedBatchLeft (decision 722): a device a batch
// whose reassembly failed removed is unbound at Stop, as every held one
// is.
func TestStopUnbindsWhatAFailedBatchLeft(t *testing.T) {
	ctx := t.Context()
	// b stays, so the snapshot reads a label and fails.
	n, labels, _, lights := behindNode(t, "a", "b")
	labels.fails.Store(1)
	reassemblyErr(t, n.Aggregator().Batch(ctx, func(b *node.Batch) error {
		return b.Remove(endpoint.StringKey("a"))
	}))
	if err := n.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := lights["a"].EndpointNumber(); ok {
		t.Error("a is still bound after Stop")
	}
}
