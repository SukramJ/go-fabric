// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package otbr

import (
	"bytes"
	"context"
	"net/http"
	"sync/atomic"
	"testing"

	"thingfabric.dev/go-fabric/thread/otbr/otbrtest"
)

// Two overlapping activations onto no dataset, both refused with a 4xx —
// the first's answer held until the second's refusal has returned — wrote
// nothing, so together they leave nothing armed: the dataset is configured,
// and a network another writer forms afterwards survives the next revert
// (review II, R5-fsm-5401).
func TestTwoOverlappingRefusedActivationsLeaveNothingArmed(t *testing.T) {
	t.Parallel()
	ds := fakeDataset(t)
	ctx := context.Background()
	h := &hookTransport{held: make(chan struct{}), resume: make(chan struct{})}
	var held atomic.Bool
	h.hold = func(r *http.Request) bool {
		return isPut(r, "/node/dataset/active") && held.CompareAndSwap(false, true)
	}
	br, srv := newHookedBR(t, otbrtest.Options{}, h)
	srv.Fail(http.MethodPut, "/node/dataset/active", http.StatusBadRequest)

	first := make(chan error, 1)
	go func() { first <- br.SetActiveDataset(ctx, ds) }()
	<-h.held // the first activation's 400 is back, not yet seen by it

	if err := br.SetActiveDataset(ctx, ds); err == nil {
		t.Fatal("the second refused activation succeeded")
	}
	h.resume <- struct{}{}
	if err := <-first; err == nil {
		t.Fatal("the first refused activation succeeded")
	}
	srv.Fail(http.MethodPut, "/node/dataset/active", 0)

	br.mu.Lock()
	configured := br.configured
	br.mu.Unlock()
	if !configured {
		t.Error("two refused activations left the dataset not configured")
	}

	formOutOfBand(t, br, srv, ds)
	if err := br.RevertActiveDataset(ctx); err != nil {
		t.Fatal(err)
	}
	if srv.State() != "leader" || !bytes.Equal(srv.Active(), ds) {
		t.Errorf("the revert touched another writer's network: %s, %x, want leader with the dataset", srv.State(), srv.Active())
	}
}
