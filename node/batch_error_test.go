// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"thingfabric.dev/go-fabric/endpoint"
	epmem "thingfabric.dev/go-fabric/endpoint/memstore"
	"thingfabric.dev/go-fabric/node"
	"thingfabric.dev/go-fabric/store/memstore"
)

// wantCall is the expected form of one [node.BatchCallError].
type wantCall struct {
	op      node.BatchOp
	key     string
	applied bool
	is      error
}

// checkCalls asserts err is a *node.BatchError whose Calls are want, in
// order, and returns it.
func checkCalls(t *testing.T, err error, want ...wantCall) *node.BatchError {
	t.Helper()
	var be *node.BatchError
	if !errors.As(err, &be) {
		t.Fatalf("Batch: %v (%T); want a *node.BatchError", err, err)
	}
	if len(be.Calls) != len(want) {
		t.Fatalf("Calls %v; want %d entries", be.Calls, len(want))
	}
	for i, w := range want {
		c := be.Calls[i]
		if c.Op != w.op || c.Key == nil || c.Key.String() != w.key || c.Applied != w.applied {
			t.Errorf("Calls[%d] = {%s %v applied=%v}; want {%s %s applied=%v}", i, c.Op, c.Key, c.Applied, w.op, w.key, w.applied)
		}
		if !errors.Is(c, w.is) || !errors.Is(err, w.is) {
			t.Errorf("Calls[%d] %v does not carry %v", i, c, w.is)
		}
	}
	return be
}

// TestBatchErrorNamesTheHeldCallRefusedAtCommit (#616, decision 719/720):
// each held kind — an Add, a Replace, a Forget — answered nil at its call
// and refused once fn returned is named by its call: kind, the call's own
// key (also when the collision is a part key, which the text names), not
// applied, its sentinel reachable.
func TestBatchErrorNamesTheHeldCallRefusedAtCommit(t *testing.T) {
	t.Parallel()
	const part = "a/p"
	cases := []struct {
		name string
		call func(t *testing.T, b *node.Batch) error
		want wantCall
	}{
		{"Add of a held key", func(t *testing.T, b *node.Batch) error {
			t.Helper()
			return b.Add(bridgedWithParts(t, "a"))
		}, wantCall{node.BatchAdd, "a", false, node.ErrDuplicateKey}},
		{"Add of a served part key", func(t *testing.T, b *node.Batch) error {
			t.Helper()
			return b.Add(bridgedWithParts(t, "y", part))
		}, wantCall{node.BatchAdd, "y", false, node.ErrDuplicateKey}},
		{"Replace", func(t *testing.T, b *node.Batch) error {
			t.Helper()
			return b.Replace(endpoint.StringKey("r"), bridgedWithParts(t, "r", part))
		}, wantCall{node.BatchReplace, "r", false, node.ErrDuplicateKey}},
		{"Forget", func(_ *testing.T, b *node.Batch) error {
			return b.Forget(endpoint.StringKey(part))
		}, wantCall{node.BatchForget, part, false, node.ErrUnknownKey}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			n := batchNode(t, bridgedWithParts(t, "a", part), bridgedWithParts(t, "r"))
			err := n.Aggregator().Batch(t.Context(), func(b *node.Batch) error {
				if err := tc.call(t, b); err != nil {
					t.Fatalf("the call: %v; want it held", err)
				}
				return nil
			})
			be := checkCalls(t, err, tc.want)
			if be.Fn != nil || be.Store != nil || be.Reassembly != nil {
				t.Errorf("BatchError %+v; want only Calls", be)
			}
			if tc.name == "Add of a served part key" && !strings.Contains(be.Calls[0].Err.Error(), part) {
				t.Errorf("Err %q does not name the colliding part key %s", be.Calls[0].Err, part)
			}
		})
	}
}

// TestBatchErrorCallsAreInCallOrder (#616, decision 720): the commit
// judges the Forgets first, but Calls lists the failed calls in the order
// fn made them.
func TestBatchErrorCallsAreInCallOrder(t *testing.T) {
	t.Parallel()
	n := batchNode(t, bridgedWithParts(t, "a", "a/p"), bridgedWithParts(t, "r"))
	err := n.Aggregator().Batch(t.Context(), func(b *node.Batch) error {
		return errors.Join(
			b.Add(bridgedWithParts(t, "y", "a/p")),
			b.Remove(endpoint.StringKey("r")),
			b.Forget(endpoint.StringKey("a/p")),
			b.Add(bridgedWithParts(t, "a")),
		)
	})
	checkCalls(t, err,
		wantCall{node.BatchAdd, "y", false, node.ErrDuplicateKey},
		wantCall{node.BatchForget, "a/p", false, node.ErrUnknownKey},
		wantCall{node.BatchAdd, "a", false, node.ErrDuplicateKey},
	)
}

// TestBatchErrorNamesALateCollisionUndo (#616): an Add and a Replace
// applied at the call whose device gained during fn a part key another
// endpoint serves are undone and named, not applied.
func TestBatchErrorNamesALateCollisionUndo(t *testing.T) {
	t.Parallel()
	const served = "other/light"
	for _, replace := range []bool{false, true} {
		t.Run(map[bool]string{false: "Add", true: "Replace"}[replace], func(t *testing.T) {
			t.Parallel()
			n := batchNode(t, bridgedWithParts(t, "other", served), bridgedWithParts(t, "fresh"))
			key, op := "late", node.BatchAdd
			if replace {
				key, op = "fresh", node.BatchReplace
			}
			taken := lateSpec(t, key, served)
			err := n.Aggregator().Batch(t.Context(), func(b *node.Batch) error {
				var err error
				if replace {
					err = b.Replace(endpoint.StringKey("fresh"), taken)
				} else {
					err = b.Add(taken)
				}
				taken.k.Store(0) // the part arrives
				return err
			})
			be := checkCalls(t, err, wantCall{op, key, false, node.ErrDuplicateKey})
			if !strings.Contains(be.Calls[0].Err.Error(), served) {
				t.Errorf("Err %q does not name %s", be.Calls[0].Err, served)
			}
		})
	}
}

// storeErrNode is a node, not started, over a credential store that
// refuses one read on demand ([failingReadA9]); "a" was added and removed,
// so its reservation is stored.
func storeErrNode(t *testing.T) (*node.Node, *failingReadA9) {
	t.Helper()
	ctx := t.Context()
	creds := &failingReadA9{Port: memstore.New(), fail: "endpoint_number_reserved.a"}
	cfg := minimalConfig(nil)
	cfg.Store = node.Store{Credentials: creds, Endpoints: epmem.New()}
	n := newNode(t, cfg)
	if err := n.Aggregator().Add(ctx, bridgedWithParts(t, "a", "a/p")); err != nil {
		t.Fatal(err)
	}
	if err := n.Aggregator().Remove(ctx, endpoint.StringKey("a")); err != nil {
		t.Fatal(err)
	}
	return n, creds
}

// TestBatchErrorNamesTheStoreWorkOfAnAppliedCall (#616, decision 720):
// the store work after fn of a call the set took fails — an Add's
// release, a Remove's reservation, a Forget's erasure judgement — and
// the call is named as applied, the store's error reachable.
func TestBatchErrorNamesTheStoreWorkOfAnAppliedCall(t *testing.T) {
	t.Parallel()
	t.Run("Add", func(t *testing.T) {
		t.Parallel()
		n, creds := storeErrNode(t)
		creds.armed.Store(true)
		err := n.Aggregator().Batch(t.Context(), func(b *node.Batch) error {
			return b.Add(bridgedWithParts(t, "a", "a/p"))
		})
		checkCalls(t, err, wantCall{node.BatchAdd, "a", true, errReadA9})
		if len(n.Aggregator().Endpoints()) != 1 {
			t.Error("the applied Add left the set")
		}
	})
	t.Run("Remove", func(t *testing.T) {
		t.Parallel()
		n, creds := storeErrNode(t)
		if err := n.Aggregator().Add(t.Context(), bridgedWithParts(t, "a", "a/p")); err != nil {
			t.Fatal(err)
		}
		creds.armed.Store(true)
		err := n.Aggregator().Batch(t.Context(), func(b *node.Batch) error {
			return b.Remove(endpoint.StringKey("a"))
		})
		checkCalls(t, err, wantCall{node.BatchRemove, "a", true, errReadA9})
	})
	t.Run("Forget", func(t *testing.T) {
		t.Parallel()
		n, creds := storeErrNode(t)
		err := n.Aggregator().Batch(t.Context(), func(b *node.Batch) error {
			// The call reads the reservation; the erasure's judgement
			// after fn reads it again, and that read fails.
			creds.skip.Store(1)
			creds.armed.Store(true)
			if err := b.Forget(endpoint.StringKey("a")); err != nil {
				return err
			}
			return b.Add(kitLight(t, "fine"))
		})
		be := checkCalls(t, err, wantCall{node.BatchForget, "a", true, errReadA9})
		if !strings.Contains(be.Calls[0].Err.Error(), "skipped") {
			t.Errorf("Err %q; want the skipped erasure", be.Calls[0].Err)
		}
	})
}

// TestBatchErrorKeepsFnErrorAndStopped (#616, decision 719): fn's error
// alone is returned as it is, beside a call's error it is Fn; a stopped
// node is ErrStopped itself; a single-call Add refused at commit still
// answers errors.Is(ErrDuplicateKey).
func TestBatchErrorKeepsFnErrorAndStopped(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	n := batchNode(t, bridgedWithParts(t, "a"))
	agg := n.Aggregator()
	fnErr := errors.New("host refused")
	if err := agg.Batch(ctx, func(*node.Batch) error { return fnErr }); err != fnErr { //nolint:errorlint // identity is the contract
		t.Errorf("fn's error alone: %v (%T); want it as is", err, err)
	}
	err := agg.Batch(ctx, func(b *node.Batch) error {
		if err := b.Add(bridgedWithParts(t, "a")); err != nil {
			return err
		}
		return fnErr
	})
	be := checkCalls(t, err, wantCall{node.BatchAdd, "a", false, node.ErrDuplicateKey})
	if be.Fn != fnErr || !errors.Is(err, fnErr) { //nolint:errorlint // identity is the contract
		t.Errorf("Fn %v; want fn's error", be.Fn)
	}
	if err := agg.Add(ctx, bridgedWithParts(t, "a")); !errors.Is(err, node.ErrDuplicateKey) {
		t.Errorf("Add of a held key: %v; want ErrDuplicateKey", err)
	}
	if err := n.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := agg.Batch(ctx, func(*node.Batch) error { return nil }); err != node.ErrStopped { //nolint:errorlint // identity is the contract
		t.Errorf("Batch on a stopped node: %v; want ErrStopped itself", err)
	}
}

// TestBatchOpString: the call names.
func TestBatchOpString(t *testing.T) {
	t.Parallel()
	for op, want := range map[node.BatchOp]string{
		node.BatchAdd: "Add", node.BatchReplace: "Replace", node.BatchRemove: "Remove", node.BatchForget: "Forget", 0: "BatchOp(0)",
	} {
		if got := op.String(); got != want {
			t.Errorf("%d.String() = %q; want %q", op, got, want)
		}
	}
	e := &node.BatchCallError{Op: node.BatchAdd, Key: endpoint.StringKey("k"), Err: node.ErrDuplicateKey}
	if !strings.Contains(e.Error(), "Add k") || !errors.Is(e, node.ErrDuplicateKey) {
		t.Errorf("BatchCallError %q", e)
	}
}
