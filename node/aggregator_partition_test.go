// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/node"
)

// seedPartitions runs a node once with plug:a in partition "ccu1" and
// plug:b in "ccu2", and returns their stored numbers.
func seedPartitions(t *testing.T, path string) (idA, idB uint16) {
	t.Helper()
	ctx := context.Background()
	n := newNode(t, minimalConfig(openDB(t, path)))
	if err := n.Aggregator().Partition("ccu1").Add(ctx, newPlug(t, "plug:a")); err != nil {
		t.Fatal(err)
	}
	if err := n.Aggregator().Partition("ccu2").Add(ctx, newPlug(t, "plug:b")); err != nil {
		t.Fatal(err)
	}
	if err := n.Start(ctx); err != nil {
		t.Fatal(err)
	}
	idA, okA := endpointOf(t, n, "plug:a")
	idB, okB := endpointOf(t, n, "plug:b")
	if !okA || !okB || idA == idB {
		t.Fatalf("seeded numbers %d/%v, %d/%v", idA, okA, idB, okB)
	}
	if err := n.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	return idA, idB
}

// TestPartitionsKeepTheirStoredNumbers: of two partitions after a
// restart, each hands its stored numbers back to the keys that return,
// whenever they return — nothing is collected because a key was absent
// from an assembly (matter.js keeps every stored number,
// packages/node/src/storage/server/ServerEndpointStores.ts:33-45 at
// 862ad75f). It replaces #229's completeness test with v0.13.0.
func TestPartitionsKeepTheirStoredNumbers(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "node.db")
	idA, idB := seedPartitions(t, path)

	m := newNode(t, minimalConfig(openDB(t, path)))
	agg := m.Aggregator()
	first, second := agg.Partition("ccu1"), agg.Partition("ccu1")
	if first != second || agg.Partition("") != agg.Partition(node.Scope) {
		t.Error("Partition does not return the same handle for the same name")
	}
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := agg.Partition("ccu1").Add(ctx, newPlug(t, "plug:other")); err != nil {
		t.Fatal(err) // an assembly of ccu1 without plug:a
	}
	for _, k := range []string{"plug:a", "plug:b"} {
		p := "ccu1"
		if k == "plug:b" {
			p = "ccu2"
		}
		if err := agg.Partition(p).Add(ctx, newPlug(t, k)); err != nil {
			t.Fatal(err)
		}
	}
	if got, _ := endpointOf(t, m, "plug:a"); got != idA {
		t.Errorf("plug:a endpoint %d, want its stored %d", got, idA)
	}
	if got, _ := endpointOf(t, m, "plug:b"); got != idB {
		t.Errorf("plug:b endpoint %d, want its stored %d", got, idB)
	}
	if eps := agg.Partition("ccu2").Endpoints(); len(eps) != 1 || len(agg.Endpoints()) != 0 {
		t.Errorf("ccu2 holds %d, the default %d; want 1, 0", len(eps), len(agg.Endpoints()))
	}
}

// TestPartitionKeysAreUniqueAcrossPartitions: a key of one partition is a
// duplicate in another, and unknown to another's Replace and Remove.
func TestPartitionKeysAreUniqueAcrossPartitions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	n := newNode(t, minimalConfig(openDB(t, filepath.Join(t.TempDir(), "node.db"))))
	agg := n.Aggregator()
	if err := agg.Partition("ccu1").Add(ctx, newPlug(t, "plug:a")); err != nil {
		t.Fatal(err)
	}
	if err := agg.Partition("ccu2").Add(ctx, newPlug(t, "plug:a")); !errors.Is(err, node.ErrDuplicateKey) {
		t.Errorf("Add in ccu2: %v, want ErrDuplicateKey", err)
	}
	if err := agg.Add(ctx, newPlug(t, "plug:a")); !errors.Is(err, node.ErrDuplicateKey) {
		t.Errorf("Add in the default: %v, want ErrDuplicateKey", err)
	}
	if err := agg.Partition("ccu2").Remove(ctx, endpoint.StringKey("plug:a")); !errors.Is(err, node.ErrUnknownKey) {
		t.Errorf("Remove from ccu2: %v, want ErrUnknownKey", err)
	}
	if err := agg.Replace(ctx, endpoint.StringKey("plug:a"), newPlug(t, "plug:a")); !errors.Is(err, node.ErrUnknownKey) {
		t.Errorf("Replace in the default: %v, want ErrUnknownKey", err)
	}
}

// TestRetirePartitionCollectsItsRecords: a scope with stored numbers and no
// handle after a restart is collected by RetirePartition.
func TestRetirePartitionCollectsItsRecords(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "node.db")
	idA, idB := seedPartitions(t, path)

	m := newNode(t, minimalConfig(openDB(t, path)))
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.Aggregator().RetirePartition(ctx, "ccu2"); err != nil {
		t.Fatal(err)
	}
	if err := m.Aggregator().Partition("ccu1").Add(ctx, newPlug(t, "plug:a")); err != nil {
		t.Fatal(err)
	}
	if err := m.Aggregator().Partition("ccu3").Add(ctx, newPlug(t, "plug:b")); err != nil {
		t.Fatal(err)
	}
	if got, _ := endpointOf(t, m, "plug:a"); got != idA {
		t.Errorf("plug:a endpoint %d, want its stored %d (ccu1 untouched)", got, idA)
	}
	if got, _ := endpointOf(t, m, "plug:b"); got == idB {
		t.Errorf("plug:b got its stored %d back; retired ccu2 should have collected it", idB)
	}
}

// TestRetirePartitionBeforeStart rides on Start's assembly.
func TestRetirePartitionBeforeStart(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "node.db")
	_, idB := seedPartitions(t, path)

	m := newNode(t, minimalConfig(openDB(t, path)))
	if err := m.Aggregator().RetirePartition(ctx, "ccu2"); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.Aggregator().Add(ctx, newPlug(t, "plug:b")); err != nil {
		t.Fatal(err)
	}
	if got, _ := endpointOf(t, m, "plug:b"); got == idB {
		t.Errorf("plug:b got its stored %d back; ccu2 retired before Start should have been collected", idB)
	}
}

// TestRetirePartitionRefusals: the default partition, and one with an
// endpoint.
func TestRetirePartitionRefusals(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	n := newNode(t, minimalConfig(openDB(t, filepath.Join(t.TempDir(), "node.db"))))
	for _, name := range []string{"", node.Scope} {
		if err := n.Aggregator().RetirePartition(ctx, name); !errors.Is(err, node.ErrRetireDefault) {
			t.Errorf("RetirePartition(%q): %v, want ErrRetireDefault", name, err)
		}
	}
	if err := n.Aggregator().Partition("ccu1").Add(ctx, newPlug(t, "plug:a")); err != nil {
		t.Fatal(err)
	}
	if err := n.Aggregator().RetirePartition(ctx, "ccu1"); !errors.Is(err, node.ErrPartitionNotEmpty) {
		t.Errorf("RetirePartition of a partition with an endpoint: %v, want ErrPartitionNotEmpty", err)
	}
}

// startedAggNode is a started node with an empty Aggregator.
func startedAggNode(t *testing.T) *node.Node {
	t.Helper()
	n := newNode(t, minimalConfig(openDB(t, filepath.Join(t.TempDir(), "node.db"))))
	if err := n.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	return n
}

// TestBatchReassemblesOnce is #231: 300 adds in one batch are one
// reassembly.
func TestBatchReassemblesOnce(t *testing.T) {
	t.Parallel()
	n := startedAggNode(t)
	before := node.Reassemblies(n.Aggregator())
	err := n.Aggregator().Batch(context.Background(), func(b *node.Batch) error {
		for i := range 300 {
			if err := b.Add(newPlug(t, fmt.Sprintf("plug:%d", i))); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := node.Reassemblies(n.Aggregator()) - before; got != 1 {
		t.Errorf("%d reassemblies for 300 adds, want 1", got)
	}
	if got := len(n.Bridge().Topology().Bridged()); got != 300 {
		t.Errorf("%d bridged endpoints, want 300", got)
	}
}

// TestBatchKeepsWhatApplied: a failing add in the middle leaves the others
// applied, reassembles once and returns the error.
func TestBatchKeepsWhatApplied(t *testing.T) {
	t.Parallel()
	n := startedAggNode(t)
	before := node.Reassemblies(n.Aggregator())
	err := n.Aggregator().Batch(context.Background(), func(b *node.Batch) error {
		errs := make([]error, 0, 3)
		for _, k := range []string{"plug:a", "plug:a", "plug:c"} {
			errs = append(errs, b.Add(newPlug(t, k)))
		}
		return errors.Join(errs...)
	})
	if !errors.Is(err, node.ErrDuplicateKey) {
		t.Errorf("Batch: %v, want ErrDuplicateKey", err)
	}
	if got := node.Reassemblies(n.Aggregator()) - before; got != 1 {
		t.Errorf("%d reassemblies, want 1", got)
	}
	for _, k := range []string{"plug:a", "plug:c"} {
		if _, ok := endpointOf(t, n, k); !ok {
			t.Errorf("%s not served after the batch", k)
		}
	}
}

// TestBatchRemoveUnbindsAfterTheReassembly: a device removed in a batch
// stays bound while fn runs and is unbound once the batch reassembled.
func TestBatchRemoveUnbindsAfterTheReassembly(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	n := startedAggNode(t)
	key := endpoint.StringKey("light")
	light, err := device.NewOnOffLight(device.Options{StableKey: key, Name: "Light"})
	if err != nil {
		t.Fatal(err)
	}
	if err := n.Aggregator().Add(ctx, light); err != nil {
		t.Fatal(err)
	}
	before := node.Reassemblies(n.Aggregator())
	err = n.Aggregator().Batch(ctx, func(b *node.Batch) error {
		if err := b.Remove(key); err != nil {
			return err
		}
		if _, ok := light.EndpointNumber(); !ok {
			t.Error("unbound before the batch's reassembly")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := node.Reassemblies(n.Aggregator()) - before; got != 1 {
		t.Errorf("%d reassemblies, want 1", got)
	}
	if _, ok := light.EndpointNumber(); ok {
		t.Error("still bound after the batch")
	}
	if _, ok := endpointOf(t, n, "light"); ok {
		t.Error("still served after the batch")
	}
}

// TestBatchCollectsNothing: a batch's one reassembly without a stored key
// leaves its number stored for the key's return.
func TestBatchCollectsNothing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "node.db")
	idA, _ := seedPartitions(t, path)
	m := newNode(t, minimalConfig(openDB(t, path)))
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	before := node.Reassemblies(m.Aggregator())
	err := m.Aggregator().Partition("ccu1").Batch(ctx, func(b *node.Batch) error {
		return b.Add(newPlug(t, "plug:x"))
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := node.Reassemblies(m.Aggregator()) - before; got != 1 {
		t.Errorf("%d reassemblies, want 1", got)
	}
	if err := m.Aggregator().Partition("ccu1").Add(ctx, newPlug(t, "plug:a")); err != nil {
		t.Fatal(err)
	}
	if got, _ := endpointOf(t, m, "plug:a"); got != idA {
		t.Errorf("plug:a endpoint %d, want its stored %d", got, idA)
	}
}

// TestBatchBeforeStartOnlyChangesTheSet: no reassembly before Start.
func TestBatchBeforeStartOnlyChangesTheSet(t *testing.T) {
	t.Parallel()
	n := newNode(t, minimalConfig(openDB(t, filepath.Join(t.TempDir(), "node.db"))))
	err := n.Aggregator().Batch(context.Background(), func(b *node.Batch) error {
		return errors.Join(b.Add(newPlug(t, "plug:a")), b.Add(newPlug(t, "plug:b")))
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := node.Reassemblies(n.Aggregator()); got != 0 {
		t.Errorf("%d reassemblies before Start, want 0", got)
	}
	if got := len(n.Aggregator().Endpoints()); got != 2 {
		t.Errorf("%d endpoints, want 2", got)
	}
}

// TestBatchConcurrentWithAdd runs a batch beside Adds from other
// goroutines (make race): every endpoint ends up served.
func TestBatchConcurrentWithAdd(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	n := startedAggNode(t)
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			if err := n.Aggregator().Add(ctx, newPlug(t, fmt.Sprintf("single:%d", i))); err != nil {
				t.Error(err)
			}
		})
	}
	err := n.Aggregator().Batch(ctx, func(b *node.Batch) error {
		for i := range 32 {
			if err := b.Add(newPlug(t, fmt.Sprintf("batch:%d", i))); err != nil {
				return err
			}
		}
		return nil
	})
	wg.Wait()
	if err != nil {
		t.Fatal(err)
	}
	if got := len(n.Bridge().Topology().Bridged()); got != 40 {
		t.Errorf("%d bridged endpoints, want 40", got)
	}
}
