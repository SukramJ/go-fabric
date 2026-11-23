// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"strings"
	"sync/atomic"
	"testing"

	"thingfabric.dev/go-fabric/endpoint"
	epmem "thingfabric.dev/go-fabric/endpoint/memstore"
	"thingfabric.dev/go-fabric/node"
	"thingfabric.dev/go-fabric/store/memstore"
)

// valueEndpoint is a host Endpoint of an uncomparable value type: two of
// them cannot be compared with ==.
type valueEndpoint struct {
	s    endpoint.Spec
	tags []string
}

func (v valueEndpoint) Spec() endpoint.Spec { return v.s }

// ifaceEndpoint is a host Endpoint whose type is comparable (its Spec is
// held by pointer) but whose
// value is not when meta holds a slice: == on two of them panics
// (review II, R4-api-4703, the Fable diff read's B1).
type ifaceEndpoint struct {
	s    *endpoint.Spec
	meta any
}

func (v ifaceEndpoint) Spec() endpoint.Spec { return *v.s }

// uncomparableEndpoints are the host Endpoints whose values cannot be
// compared: one by its type, one by the value an interface field holds.
var uncomparableEndpoints = []struct {
	name string
	mk   func(s endpoint.Spec, tag string) node.Endpoint
}{
	{"uncomparable type", func(s endpoint.Spec, tag string) node.Endpoint { return valueEndpoint{s: s, tags: []string{tag}} }},
	{"interface field holding a slice", func(s endpoint.Spec, tag string) node.Endpoint { return ifaceEndpoint{s: &s, meta: []string{tag}} }},
}

// noPanic runs fn and turns a panic into a test failure naming what.
func noPanic(t *testing.T, what string, fn func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("%s panicked: %v\n%s", what, r, debug.Stack())
		}
	}()
	fn()
}

// TestBatchUncomparableEndpoint (review II, R4-api-4703): an Endpoint of
// an uncomparable value type is replaced as a pointer type is — directly,
// through a held Replace, and by a withheld declaration — and never
// panics on the host's goroutine; the new endpoint's Spec is served.
func TestBatchUncomparableEndpoint(t *testing.T) {
	for _, tc := range uncomparableEndpoints {
		t.Run(tc.name, func(t *testing.T) { testUncomparableEndpoint(t, tc.mk) })
	}
}

func testUncomparableEndpoint(t *testing.T, mk func(endpoint.Spec, string) node.Endpoint) {
	t.Helper()
	t.Run("Add", func(t *testing.T) {
		ctx := t.Context()
		cfg := minimalConfig(nil)
		cfg.Store = node.Store{Credentials: memstore.New(), Endpoints: epmem.New()}
		n := newNode(t, cfg)
		agg := n.Aggregator()
		if err := n.Start(ctx); err != nil {
			t.Fatal(err)
		}
		noPanic(t, "Add", func() {
			for _, ep := range []node.Endpoint{mk(bridgedWithParts(t, "u", "up").Spec(), "a"), mk(kitLight(t, "v").Spec(), "b")} {
				if err := agg.Add(ctx, ep); err != nil {
					t.Fatalf("Add: %v", err)
				}
			}
		})
		for _, k := range []string{"u", "up", "v"} {
			if _, ok := servedNumberOf(n, k); !ok {
				t.Fatalf("after Add: %s not served", k)
			}
		}
	})
	t.Run("Replace", func(t *testing.T) {
		ctx := t.Context()
		cfg := minimalConfig(nil)
		cfg.Store = node.Store{Credentials: memstore.New(), Endpoints: epmem.New()}
		n := newNode(t, cfg)
		agg := n.Aggregator()
		if err := agg.Add(ctx, mk(kitLight(t, "u").Spec(), "a")); err != nil {
			t.Fatal(err)
		}
		if err := n.Start(ctx); err != nil {
			t.Fatal(err)
		}
		spec := bridgedWithParts(t, "u", "up").Spec()
		noPanic(t, "Replace", func() {
			if err := agg.Replace(ctx, endpoint.StringKey("u"), mk(spec, "b")); err != nil {
				t.Fatalf("Replace: %v", err)
			}
		})
		if _, ok := servedNumberOf(n, "up"); !ok {
			t.Fatal("after Replace: part up not served; want the replacement served")
		}
	})
	t.Run("held Replace", func(t *testing.T) {
		ctx := t.Context()
		cfg := minimalConfig(nil)
		cfg.Store = node.Store{Credentials: memstore.New(), Endpoints: epmem.New()}
		n := newNode(t, cfg)
		agg := n.Aggregator()
		for _, ep := range []node.Endpoint{mk(kitLight(t, "u").Spec(), "a"), bridgedWithParts(t, "o", "p")} {
			if err := agg.Add(ctx, ep); err != nil {
				t.Fatal(err)
			}
		}
		if err := n.Start(ctx); err != nil {
			t.Fatal(err)
		}
		var err error
		noPanic(t, "held Replace", func() {
			err = agg.Batch(ctx, func(b *node.Batch) error {
				// Held: o serves p; applied once o's Remove moved it away.
				if err := b.Replace(endpoint.StringKey("u"), mk(bridgedWithParts(t, "u", "p").Spec(), "c")); err != nil {
					return err
				}
				return b.Remove(endpoint.StringKey("o"))
			})
		})
		if err != nil {
			t.Fatalf("Batch: %v", err)
		}
		if _, ok := servedNumberOf(n, "p"); !ok {
			t.Fatal("after the batch: p not served; want the held replacement applied")
		}
	})
	t.Run("held Replace refused", func(t *testing.T) {
		ctx := t.Context()
		cfg := minimalConfig(nil)
		cfg.Store = node.Store{Credentials: memstore.New(), Endpoints: epmem.New()}
		n := newNode(t, cfg)
		agg := n.Aggregator()
		// Two endpoints of one uncomparable type: the one replaced is
		// left out of the collision, the other is not.
		for _, ep := range []node.Endpoint{
			mk(kitLight(t, "u").Spec(), "a"),
			mk(bridgedWithParts(t, "o", "p").Spec(), "b"),
		} {
			if err := agg.Add(ctx, ep); err != nil {
				t.Fatal(err)
			}
		}
		var err error
		noPanic(t, "Replace colliding with an endpoint of the same type", func() {
			err = agg.Replace(ctx, endpoint.StringKey("u"), mk(bridgedWithParts(t, "u", "p").Spec(), "c"))
		})
		if !errors.Is(err, node.ErrDuplicateKey) {
			t.Fatalf("Replace whose part another endpoint of its type serves: %v; want ErrDuplicateKey", err)
		}
	})
}

// specFlip serves with for the first k Spec calls and without after: a
// host Endpoint whose live Spec loses a part while a batch commits.
type specFlip struct {
	with, without endpoint.Spec
	k             atomic.Int64
	calls         atomic.Int64
}

func (f *specFlip) Spec() endpoint.Spec {
	if f.calls.Add(1) <= f.k.Load() {
		return f.with
	}
	return f.without
}

// TestBatchCommitFinalRoundJudgesNothingAgain (review II, R4-fsm-4701):
// whenever the owner's surface changes during the commit, the commit
// neither panics nor drops a held call: every held call is applied or
// refused, one error each.
func TestBatchCommitFinalRoundJudgesNothingAgain(t *testing.T) {
	for k := int64(1); k <= 40; k++ {
		t.Run(fmt.Sprint("k=", k), func(t *testing.T) {
			ctx := context.Background()
			cfg := minimalConfig(nil)
			cfg.Store = node.Store{Credentials: memstore.New(), Endpoints: epmem.New()}
			n := newNode(t, cfg)
			agg := n.Aggregator()
			o := &specFlip{with: bridgedWithParts(t, "o", "p").Spec(), without: bridgedWithParts(t, "o").Spec()}
			o.k.Store(1 << 40)
			if err := agg.Add(ctx, o); err != nil {
				t.Fatal(err)
			}
			if err := agg.Add(ctx, kitLight(t, "q")); err != nil {
				t.Fatal(err)
			}
			var err error
			noPanic(t, "Batch", func() {
				err = agg.Batch(ctx, func(b *node.Batch) error {
					o.calls.Store(0)
					o.k.Store(k)
					return errors.Join(
						b.Add(bridgedWithParts(t, "p", "q")), // held: o serves p, the set holds q
						b.Forget(endpoint.StringKey("p")),    // held: o serves p
						b.Add(kitLight(t, "q")),              // held: the set holds q
					)
				})
			})
			// The Add of q is refused whatever o serves (the set holds q).
			// Either the Forget applied before the Add of p was judged and
			// dropped it (one error), or neither applied and each held call
			// carries its own refusal (three) — never a skipped one.
			msg := fmt.Sprint(err)
			dup, unknown := strings.Count(msg, node.ErrDuplicateKey.Error()), strings.Count(msg, node.ErrUnknownKey.Error())
			if (dup != 1 || unknown != 0) && (dup != 2 || unknown != 1) {
				t.Fatalf("Batch: %v; want one refusal (the Forget applied) or three (none applied), got %d ErrDuplicateKey + %d ErrUnknownKey", err, dup, unknown)
			}
			if err := n.Start(ctx); err != nil {
				t.Fatalf("Start after the batch: %v", err)
			}
		})
	}
}

// TestBatchAddUndoKeepsItsPartsReserved (review II, R4-store-4702): an
// Add the commit undoes after a concurrent reassembly served it keeps the
// reservation of its tree, so a later Forget of its key erases its
// parts' numbers too.
func TestBatchAddUndoKeepsItsPartsReserved(t *testing.T) {
	ctx := t.Context()
	eps := epmem.New()
	cfg := minimalConfig(nil)
	cfg.Store = node.Store{Credentials: memstore.New(), Endpoints: eps}
	n := newNode(t, cfg)
	agg := n.Aggregator()
	if err := agg.Add(ctx, bridgedWithParts(t, "o", "p")); err != nil {
		t.Fatal(err)
	}
	if err := n.Start(ctx); err != nil {
		t.Fatal(err)
	}
	// x gains the part p, which o serves, once fn returned.
	x := &specFlip{with: bridgedWithParts(t, "x", "xp").Spec(), without: bridgedWithParts(t, "x", "xp", "p").Spec()}
	x.k.Store(1 << 40)
	err := agg.Batch(ctx, func(b *node.Batch) error {
		if err := b.Add(x); err != nil {
			return err
		}
		// A concurrent reassembly serves the take (fn may call the
		// Aggregator).
		if err := agg.Add(ctx, kitLight(t, "other")); err != nil {
			return err
		}
		if _, ok := servedNumberOf(n, "xp"); !ok {
			t.Fatal("during fn: xp not served")
		}
		x.k.Store(0)
		return nil
	})
	if !errors.Is(err, node.ErrDuplicateKey) {
		t.Fatalf("Batch: %v; want the late collision refused", err)
	}
	if err := agg.Forget(ctx, endpoint.StringKey("x")); err != nil {
		t.Fatalf("Forget(x): %v", err)
	}
	if hasEndpointRecord(t, eps, "x") || hasEndpointRecord(t, eps, "xp") {
		t.Fatalf("after Forget(x): records x=%v xp=%v; want neither", hasEndpointRecord(t, eps, "x"), hasEndpointRecord(t, eps, "xp"))
	}
	if !hasEndpointRecord(t, eps, "p") {
		t.Fatal("after Forget(x): o's part p lost its record")
	}
}

// TestAddPartCollidingOnABoundDeviceIsRefused (review II, R4-err-4704,
// decision 599): Device.AddPart of a part whose key another endpoint of
// the node serves is refused at the call with ErrDuplicateKey, nothing
// moved, and the node's later changes are served.
func TestAddPartCollidingOnABoundDeviceIsRefused(t *testing.T) {
	for _, started := range []bool{false, true} {
		t.Run(fmt.Sprint("started=", started), func(t *testing.T) {
			ctx := t.Context()
			cfg := minimalConfig(nil)
			cfg.Store = node.Store{Credentials: memstore.New(), Endpoints: epmem.New()}
			n := newNode(t, cfg)
			agg := n.Aggregator()
			x := bridgedWithParts(t, "x", "xp")
			for _, ep := range []node.Endpoint{bridgedWithParts(t, "o", "p"), x} {
				if err := agg.Add(ctx, ep); err != nil {
					t.Fatal(err)
				}
			}
			if started {
				if err := n.Start(ctx); err != nil {
					t.Fatal(err)
				}
			}
			p := kitLight(t, "p")
			if err := x.AddPart(p.Device); !errors.Is(err, node.ErrDuplicateKey) {
				t.Fatalf("AddPart of a part whose key o serves: %v; want ErrDuplicateKey", err)
			}
			if len(x.Parts()) != 1 {
				t.Fatalf("after the refusal: x has %d parts; want nothing moved", len(x.Parts()))
			}
			if err := agg.Add(ctx, kitLight(t, "u1")); err != nil {
				t.Fatalf("unrelated Add: %v", err)
			}
			if !started {
				if err := n.Start(ctx); err != nil {
					t.Fatalf("Start: %v", err)
				}
			}
			if _, ok := servedNumberOf(n, "u1"); !ok {
				t.Fatal("u1 not served")
			}
			// A part of no other endpoint's key still moves in.
			if err := x.AddPart(kitLight(t, "xq").Device); err != nil {
				t.Fatalf("AddPart xq: %v", err)
			}
		})
	}
}
