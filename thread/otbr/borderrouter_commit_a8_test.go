// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package otbr

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"

	"thingfabric.dev/go-fabric/thread/otbr/otbrtest"
)

// A commit takes off only the activations numbered before it began: one
// numbered while the commit's state GET is held — the GET answered
// "attached", the answer not yet seen by the commit — stays uncommitted,
// and the next revert clears its dataset (review II, R5-fsm-5401,
// C5-5401-residue-n1).
func TestCommitKeepsAnActivationThatOverlapsIt(t *testing.T) {
	t.Parallel()
	ds := fakeDataset(t)
	ctx := context.Background()
	h := &hookTransport{held: make(chan struct{}), resume: make(chan struct{})}
	var held atomic.Bool
	h.hold = func(r *http.Request) bool {
		return r.Method == http.MethodGet && r.URL.Path == "/node/state" && held.CompareAndSwap(false, true)
	}
	br, srv := newHookedBR(t, otbrtest.Options{}, h)
	srv.SetState("leader")

	commit := make(chan error, 1)
	go func() { commit <- br.CommitActiveDataset(ctx) }()
	<-h.held // the commit's GET is answered "leader", not yet seen by it

	// An activation onto no dataset, numbered after the commit began.
	if err := br.SetActiveDataset(ctx, ds); err != nil {
		t.Fatal(err)
	}
	h.resume <- struct{}{}
	if err := <-commit; err != nil {
		t.Fatal(err)
	}
	br.mu.Lock()
	configured := br.configured
	br.mu.Unlock()
	if configured {
		t.Error("the commit took off an activation that started after it began")
	}

	if err := br.RevertActiveDataset(ctx); err != nil {
		t.Fatal(err)
	}
	if srv.State() != "disabled" || srv.Active() != nil {
		t.Errorf("the overlapping activation was not reverted: %s, %x", srv.State(), srv.Active())
	}
}
