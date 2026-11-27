// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"thingfabric.dev/go-fabric/device"
	"thingfabric.dev/go-fabric/endpoint"
	epmem "thingfabric.dev/go-fabric/endpoint/memstore"
	"thingfabric.dev/go-fabric/node"
	"thingfabric.dev/go-fabric/store"
	"thingfabric.dev/go-fabric/store/memstore"
)

// faultyCreds is a credential store that refuses every read of readKey
// while failRead is set and every write of writeKey while failWrite is.
type faultyCreds struct {
	store.Port
	readKey, writeKey   string
	failRead, failWrite atomic.Bool
}

var errFaulty = errors.New("store refused")

func (s *faultyCreds) GetSetting(ctx context.Context, key string) (value string, found bool, err error) {
	if key == s.readKey && s.failRead.Load() {
		return "", false, errFaulty
	}
	return s.Port.GetSetting(ctx, key)
}

func (s *faultyCreds) SetSetting(ctx context.Context, key, value string) error {
	if key == s.writeKey && s.failWrite.Load() {
		return errFaulty
	}
	return s.Port.SetSetting(ctx, key, value)
}

// parkStores are the stores a parked-erasure test runs two nodes over.
type parkStores struct {
	creds *faultyCreds
	eps   *epmem.Store
}

func newParkStores() parkStores {
	return parkStores{
		creds: &faultyCreds{Port: memstore.New(), readKey: "endpoint_number_reserved.b", writeKey: "aggregator.forget_pending"},
		eps:   epmem.New(),
	}
}

// node is a node over the stores whose label reads labels fails on
// demand; started with one light per key when keys are given.
func (s parkStores) node(t *testing.T, labels *failingLabelReads, keys ...string) *node.Node {
	t.Helper()
	cfg := minimalConfig(nil)
	cfg.Store = node.Store{Credentials: s.creds, Endpoints: s.eps}
	cfg.EndpointState = labels
	n := newNode(t, cfg)
	for _, k := range keys {
		if err := n.Aggregator().Add(t.Context(), kitLight(t, k)); err != nil {
			t.Fatal(err)
		}
	}
	if len(keys) > 0 {
		if err := n.Start(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	return n
}

// pendingForget is the store's record of the forgets in progress.
func (s parkStores) pendingForget(t *testing.T) string {
	t.Helper()
	v, _, err := s.creds.Port.GetSetting(t.Context(), "aggregator.forget_pending")
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// parkForgetB fails the reassembly of a batch that forgets b: its erasure
// is parked.
func parkForgetB(t *testing.T, n *node.Node, labels *failingLabelReads) error {
	t.Helper()
	labels.fails.Store(1)
	return n.Aggregator().Batch(t.Context(), func(b *node.Batch) error {
		return b.Forget(endpoint.StringKey("b"))
	})
}

// TestParkedErasureIsAForgetInProgress (decision 726; Opus H1's probe): a
// Forget whose batch's reassembly failed is recorded as a forget in
// progress; Stop runs it once nothing is served, and a node made over the
// same stores finds it done.
func TestParkedErasureIsAForgetInProgress(t *testing.T) {
	s := newParkStores()
	labels := &failingLabelReads{}
	n := s.node(t, labels, "a", "b", "c")
	reassemblyErr(t, parkForgetB(t, n, labels))
	if !hasEndpointRecord(t, s.eps, "b") {
		t.Fatal("b's record is gone while b is served")
	}
	if rec := s.pendingForget(t); !strings.Contains(rec, `"b"`) {
		t.Fatalf("forget in progress %q; want b recorded at parking", rec)
	}
	if err := n.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if hasEndpointRecord(t, s.eps, "b") {
		t.Error("b's record survived Stop")
	}
	s.node(t, &failingLabelReads{})
	if hasEndpointRecord(t, s.eps, "b") {
		t.Error("b's record survived the next New")
	}
	if rec := s.pendingForget(t); rec != "" {
		t.Errorf("forget in progress %q after the next New; want none", rec)
	}
}

// TestParkedErasureWhileTheBridgeStops (decision 726): the batch's
// reassembly fails because the node stops during fn — no store fault —
// and the Forget's erasure still runs.
func TestParkedErasureWhileTheBridgeStops(t *testing.T) {
	s := newParkStores()
	n := s.node(t, &failingLabelReads{}, "a", "b")
	err := n.Aggregator().Batch(t.Context(), func(b *node.Batch) error {
		if err := b.Forget(endpoint.StringKey("b")); err != nil {
			return err
		}
		return n.Stop(context.Background())
	})
	t.Logf("batch: %v", err)
	if hasEndpointRecord(t, s.eps, "b") {
		t.Error("b's record survived the batch and Stop")
	}
	s.node(t, &failingLabelReads{})
	if hasEndpointRecord(t, s.eps, "b") {
		t.Error("b's record survived the next New")
	}
	if rec := s.pendingForget(t); rec != "" {
		t.Errorf("forget in progress %q; want none", rec)
	}
}

// TestParkedErasureSkippedAgainFinishesAtNextNew (decision 726): the
// later batch heals the topology but cannot read b's reservation, so the
// erasure is skipped — its error in that batch's Store, the erasure
// parked again, its record kept; Stop cannot read it either; the next
// New finishes it.
func TestParkedErasureSkippedAgainFinishesAtNextNew(t *testing.T) {
	s := newParkStores()
	labels := &failingLabelReads{}
	n := s.node(t, labels, "a", "b", "c")
	reassemblyErr(t, parkForgetB(t, n, labels))
	s.creds.failRead.Store(true)
	err := n.Aggregator().Batch(t.Context(), func(*node.Batch) error { return nil })
	var be *node.BatchError
	if !errors.As(err, &be) || be.Store == nil || !errors.Is(be.Store, errFaulty) || be.Calls != nil || be.Reassembly != nil {
		t.Fatalf("healing batch: %v; want the skipped erasure in Store alone", err)
	}
	if _, ok := servedNumberOf(n, "b"); ok {
		t.Fatal("b is served after the healing batch")
	}
	if !hasEndpointRecord(t, s.eps, "b") {
		t.Fatal("b's record is gone although its erasure was skipped")
	}
	if err := n.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !hasEndpointRecord(t, s.eps, "b") {
		t.Fatal("b's record is gone although Stop could not read its reservation")
	}
	if rec := s.pendingForget(t); !strings.Contains(rec, `"b"`) {
		t.Fatalf("forget in progress %q; want b still recorded", rec)
	}
	s.creds.failRead.Store(false)
	s.node(t, &failingLabelReads{})
	if hasEndpointRecord(t, s.eps, "b") {
		t.Error("b's record survived the next New")
	}
	if rec := s.pendingForget(t); rec != "" {
		t.Errorf("forget in progress %q after the next New; want none", rec)
	}
}

// TestParkedErasureRecordGoesWithTheHeal (decision 726): the next batch
// heals, erases b and drops the record of its forget in progress.
func TestParkedErasureRecordGoesWithTheHeal(t *testing.T) {
	s := newParkStores()
	labels := &failingLabelReads{}
	n := s.node(t, labels, "a", "b")
	reassemblyErr(t, parkForgetB(t, n, labels))
	if err := n.Aggregator().Batch(t.Context(), func(*node.Batch) error { return nil }); err != nil {
		t.Fatalf("healing batch: %v", err)
	}
	if hasEndpointRecord(t, s.eps, "b") {
		t.Error("b's record survived the heal")
	}
	if rec := s.pendingForget(t); rec != "" {
		t.Errorf("forget in progress %q after the heal; want none", rec)
	}
}

// TestParkedErasureUnrecordedNamesTheForget (decision 726): when the
// record cannot be written at parking, the failing batch names the
// Forget, applied — the host makes it again; the parked erasure still
// runs at the heal.
func TestParkedErasureUnrecordedNamesTheForget(t *testing.T) {
	s := newParkStores()
	labels := &failingLabelReads{}
	n := s.node(t, labels, "a", "b")
	s.creds.failWrite.Store(true)
	err := parkForgetB(t, n, labels)
	s.creds.failWrite.Store(false)
	be := checkCalls(t, err, wantCall{node.BatchForget, "b", true, errFaulty})
	if be.Reassembly == nil {
		t.Error("Reassembly is nil")
	}
	if err := n.Aggregator().Batch(t.Context(), func(*node.Batch) error { return nil }); err != nil {
		t.Fatalf("healing batch: %v", err)
	}
	if hasEndpointRecord(t, s.eps, "b") {
		t.Error("b's record survived the heal")
	}
}

// TestRestoredEndpointsKeepTheTopologyBehind (Fable F2, Opus L3): a
// concurrent reassembly that succeeds between a batch's failed one and
// its restore of the endpoints it held back clears the failure record;
// the restore records the topology behind again, so the next empty batch
// serves them even when the parked erasure cannot be judged.
func TestRestoredEndpointsKeepTheTopologyBehind(t *testing.T) {
	s := newParkStores()
	s.creds.readKey = "endpoint_number_reserved.k"
	labels := &failingLabelReads{}
	n := s.node(t, labels, "a", "k")
	agg := n.Aggregator()
	node.SetAfterFailedReassembly(agg, func() {
		node.SetAfterFailedReassembly(agg, nil)
		if err := node.Reassemble(t.Context(), agg); err != nil {
			t.Errorf("the concurrent reassembly: %v", err)
		}
	})
	labels.fails.Store(1)
	reassemblyErr(t, agg.Batch(t.Context(), func(b *node.Batch) error {
		// k forgotten and added again: held out of the reassembly.
		if err := b.Forget(endpoint.StringKey("k")); err != nil {
			return err
		}
		return b.Add(kitLight(t, "k"))
	}))
	if _, ok := servedNumberOf(n, "k"); ok {
		t.Fatal("k is served; the concurrent reassembly ran with k held back")
	}
	s.creds.failRead.Store(true)
	err := agg.Batch(t.Context(), func(*node.Batch) error { return nil })
	if !errors.Is(err, errFaulty) {
		t.Fatalf("empty batch: %v; want the skipped erasure's error", err)
	}
	if _, ok := servedNumberOf(n, "k"); !ok {
		t.Error("k is not served after the empty batch: the restore left the topology behind unrecorded")
	}
}

// TestSameInstanceAddedAgainBeforeTheHeal (decision 727): a device removed
// in a batch whose reassembly failed is still served and bound, so adding
// the same instance again in the next batch is refused at the call with
// device.ErrAddedElsewhere; that batch heals and unbinds it, and the one
// after it adds it.
func TestSameInstanceAddedAgainBeforeTheHeal(t *testing.T) {
	ctx := t.Context()
	n, labels, _, lights := behindNode(t, "a", "b")
	agg := n.Aggregator()
	labels.fails.Store(1)
	reassemblyErr(t, agg.Batch(ctx, func(b *node.Batch) error {
		return b.Remove(endpoint.StringKey("a"))
	}))
	addA := func(b *node.Batch) error { return b.Add(lights["a"]) }
	if err := agg.Batch(ctx, addA); !errors.Is(err, device.ErrAddedElsewhere) {
		t.Fatalf("the first Add of the same instance: %v; want ErrAddedElsewhere", err)
	}
	if _, ok := servedNumberOf(n, "a"); ok {
		t.Fatal("a is served after the healing batch")
	}
	if err := agg.Batch(ctx, addA); err != nil {
		t.Fatalf("the second Add of the same instance: %v", err)
	}
	if _, ok := lights["a"].EndpointNumber(); !ok {
		t.Error("a is not served and bound after the second Add")
	}
}

// TestRetirePartitionErrorsArePlain (decision 725; Opus L1): a failed
// reassembly of RetirePartition itself is a plain error and the next
// empty batch heals it; the work a failed batch left that fails in
// RetirePartition is returned plain too, never a *BatchError.
func TestRetirePartitionErrorsArePlain(t *testing.T) {
	t.Run("own reassembly", func(t *testing.T) {
		ctx := t.Context()
		n, labels, _, _ := behindNode(t, "a")
		agg := n.Aggregator()
		labels.fails.Store(1)
		err := agg.RetirePartition(ctx, "old")
		if !errors.Is(err, errLabelRead) || errors.As(err, new(*node.BatchError)) {
			t.Fatalf("RetirePartition: %v (%T); want the reassembly's error plain", err, err)
		}
		before := node.Reassemblies(agg)
		if err := agg.Batch(ctx, func(*node.Batch) error { return nil }); err != nil {
			t.Fatalf("the healing batch: %v", err)
		}
		if node.Reassemblies(agg)-before != 1 {
			t.Error("the empty batch after RetirePartition's failure did not reassemble")
		}
	})
	t.Run("work left", func(t *testing.T) {
		s := newParkStores()
		labels := &failingLabelReads{}
		n := s.node(t, labels, "a", "b")
		reassemblyErr(t, parkForgetB(t, n, labels))
		s.creds.failRead.Store(true)
		err := n.Aggregator().RetirePartition(t.Context(), "old")
		if !errors.Is(err, errFaulty) || errors.As(err, new(*node.BatchError)) {
			t.Fatalf("RetirePartition: %v (%T); want the left work's error plain", err, err)
		}
	})
}

// TestCancelForgetErrorIsStore (Opus L1): the cancel of a forget in
// progress for a key forgotten and added again in the batch is no call's
// store work: its error is BatchError.Store.
func TestCancelForgetErrorIsStore(t *testing.T) {
	ctx := t.Context()
	creds := &failingReadA9{Port: memstore.New(), fail: "aggregator.forget_pending"}
	cfg := minimalConfig(nil)
	cfg.Store = node.Store{Credentials: creds, Endpoints: epmem.New()}
	n := newNode(t, cfg)
	agg := n.Aggregator()
	if err := agg.Add(ctx, kitLight(t, "k")); err != nil {
		t.Fatal(err)
	}
	// The forgets in progress are read by the new Add's release, twice by
	// the erasure, then by the cancel: that read fails.
	creds.skip.Store(3)
	creds.armed.Store(true)
	err := agg.Batch(ctx, func(b *node.Batch) error {
		if err := b.Forget(endpoint.StringKey("k")); err != nil {
			return err
		}
		return b.Add(kitLight(t, "k"))
	})
	var be *node.BatchError
	if !errors.As(err, &be) || be.Calls != nil || !errors.Is(be.Store, errReadA9) {
		t.Fatalf("Batch: %v; want the cancel's read error in Store alone", err)
	}
}

// TestParkedErasureRunsEveryRecordedKey (Fable R1, unit level): the run
// of a parked erasure covers every key its record names, even one whose
// reservation under the forgotten key is gone by the run. The store is
// written directly to make the case: b's reservation grows to x between
// the Forget's call and the parking (in the field an undone take's joined
// reserve, refuseLateCollisionsLocked) and is released before the run (in
// the field an Add of b in the next batch). Without the record's keys in
// the op the run erased b alone and cleared the record with x unerased.
func TestParkedErasureRunsEveryRecordedKey(t *testing.T) {
	ctx := t.Context()
	s := newParkStores()
	labels := &failingLabelReads{}
	n := s.node(t, labels, "a", "b", "x")
	agg := n.Aggregator()
	if err := agg.Batch(ctx, func(b *node.Batch) error {
		return b.Remove(endpoint.StringKey("x"))
	}); err != nil {
		t.Fatal(err)
	}
	if !hasEndpointRecord(t, s.eps, "x") {
		t.Fatal("removed x has no record; the case needs one")
	}
	node.SetAfterFailedReassembly(agg, func() {
		node.SetAfterFailedReassembly(agg, nil)
		if err := s.creds.Port.SetSetting(ctx, "endpoint_number_reserved.b", `["b","x"]`); err != nil {
			t.Error(err)
		}
	})
	reassemblyErr(t, parkForgetB(t, n, labels))
	if rec := s.pendingForget(t); !strings.Contains(rec, `"x"`) {
		t.Fatalf("forget in progress %q; want x recorded at parking", rec)
	}
	if err := s.creds.Port.SetSetting(ctx, "endpoint_number_reserved.b", ""); err != nil {
		t.Fatal(err)
	}
	if err := agg.Batch(ctx, func(*node.Batch) error { return nil }); err != nil {
		t.Fatalf("healing batch: %v", err)
	}
	if hasEndpointRecord(t, s.eps, "x") {
		t.Error("x's record survived the run of the erasure that recorded it")
	}
	if rec := s.pendingForget(t); rec != "" {
		t.Errorf("forget in progress %q after the heal; want none", rec)
	}
}
