// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spectest_test

import (
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
	"github.com/SukramJ/go-fabric/contract"
)

// writing is a notifying server whose MatterWrite reports what notify
// says, or refuses with err.
type writing struct {
	*notifying
	notify func(n *notifying, ctx context.Context, id uint32)
	err    error
}

func (w *writing) MatterWrite(ctx context.Context, id uint32, _ any) error {
	if w.err != nil {
		return w.err
	}
	w.notify(w.notifying, ctx, id)
	return nil
}

func newWriting(notify func(n *notifying, ctx context.Context, id uint32), err error) func(tb testing.TB) contract.ClusterServer {
	return func(tb testing.TB) contract.ClusterServer {
		tb.Helper()
		n, ok := newNotifying(tb).(*notifying)
		if !ok {
			tb.Fatal("not the notifying server")
		}
		return &writing{notifying: n, notify: notify, err: err}
	}
}

// TestCheckOriginsWrite: a write that reaches the listener as the written
// attribute from the write passes; one that reports another attribute,
// none, or is refused fails, and so does a controller-write case that
// names no attribute.
func TestCheckOriginsWrite(t *testing.T) {
	t.Parallel()
	asWritten := func(n *notifying, ctx context.Context, id uint32) { n.NotifyFrom(contract.OriginFrom(ctx), id) }
	if errs := run(t, func(tb *recorder) {
		spectest.CheckOrigins(tb, newWriting(asWritten, nil), spectest.Write(0, uint64(1)))
	}); len(errs) != 0 {
		t.Errorf("a conformant write fails: %q", errs)
	}
	wantReport(t, "another attribute", run(t, func(tb *recorder) {
		spectest.CheckOrigins(tb, newWriting(func(n *notifying, ctx context.Context, _ uint32) {
			n.NotifyFrom(contract.OriginFrom(ctx), 7)
		}, nil), spectest.Write(0, uint64(1)))
	}), "no change names attribute 0x0000")
	wantReport(t, "a silent write", run(t, func(tb *recorder) {
		spectest.CheckOrigins(tb, newWriting(func(*notifying, context.Context, uint32) {}, nil), spectest.Write(0, uint64(1)))
	}), "controller write: no change reported")
	wantReport(t, "a refused write", run(t, func(tb *recorder) {
		spectest.CheckOrigins(tb, newWriting(asWritten, errors.New("refused")), spectest.Write(0, uint64(1)))
	}), "write of 0x0000 = 1: refused")
	wantReport(t, "no attribute named", run(t, func(tb *recorder) {
		spectest.CheckOrigins(tb, newWriting(asWritten, nil), spectest.OriginCase{
			Name: "unnamed write", Want: contract.OriginControllerWrite,
			Act: func(tb testing.TB, srv contract.ClusterServer) {
				tb.Helper()
				_ = srv.MatterWrite(contract.WithOrigin(context.Background(), contract.OriginControllerWrite), 0, uint64(1))
			},
		})
	}), "unnamed write: a controller-write case names the attribute it writes")
}
