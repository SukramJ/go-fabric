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

// faultyCreds is a credential store that refuses every read of readKey
// while failRead is set and every write of writeKey while failWrite is.
type faultyCreds struct {
	store.Port
	readKey, writeKey   string
	failRead, failWrite atomic.Bool
}

var errFaulty = errors.New("store refused")

func (s *faultyCreds) GetSetting(ctx context.Context, key string) (string, bool, error) {
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
