// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"testing"

	"thingfabric.dev/go-fabric/cluster/core"
	"thingfabric.dev/go-fabric/internal/facadeseam"
	mstore "thingfabric.dev/go-fabric/store"
)

// owedJobA1 is one delivery the facade's slot queued: its hooks and its
// acknowledgement, run by the test's worker once gate closes.
type owedJobA1 struct {
	ctx       context.Context
	hooks     func(context.Context)
	ack       func(context.Context)
	gate, ran chan struct{}
}

// TestOwedFabricEventAckIsBoundToItsDeliveryA1 (R5-err-5201): two
// run-time rollbacks of one fabric's UpdateNOC, delivered through the
// node facade's slot, whose queue still holds the first delivery's hooks
// when the second rollback lands: the first delivery's acknowledgement
// removes its own owed event only, and the second stays owed until the
// second delivery's hooks ran.
func TestOwedFabricEventAckIsBoundToItsDeliveryA1(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newFakeStore()
	oc, err := core.NewOperationalCredentials(st, core.OpcredsConfig{SupportedFabrics: 5})
	if err != nil {
		t.Fatal(err)
	}
	idx, err := st.AddFabric(ctx, mstore.FabricRecord{FabricID: 1, NodeID: 0x11, RootPublicKey: []byte{4, 1}})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertIdentity(ctx, mstore.IdentityRecord{FabricIndex: idx, NOC: []byte{1}}); err != nil {
		t.Fatal(err)
	}
	jobs := make(chan *owedJobA1, 2)
	if !facadeseam.SetOwedFabricEvent(oc, func(ctx context.Context, hooks, ack func(context.Context)) {
		jobs <- &owedJobA1{ctx: ctx, hooks: hooks, ack: ack, gate: make(chan struct{}), ran: make(chan struct{})}
	}) {
		t.Fatal("the facade's owed-event slot was not set")
	}
	var queued []*owedJobA1
	worker := make(chan *owedJobA1, 2)
	go func() {
		for j := range worker {
			<-j.gate
			j.hooks(j.ctx)
			j.ack(j.ctx)
			close(j.ran)
		}
	}()
	t.Cleanup(func() { close(worker) })
	rollBackAnUpdate := func(nodeID uint64) {
		t.Helper()
		if err := st.UpdateFabricCredentials(ctx, idx, mstore.FabricCredentials{NodeID: nodeID, NOC: []byte{2}}); err != nil {
			t.Fatal(err)
		}
		oc.OnFailSafeExpiry(ctx, idx)
		j := <-jobs
		queued = append(queued, j)
		worker <- j
	}
	owed := func() []uint64 {
		t.Helper()
		evs, err := st.OwedFabricEvents(ctx)
		if err != nil {
			t.Fatal(err)
		}
		ids := make([]uint64, 0, len(evs))
		for i := range evs {
			ids = append(ids, evs[i].ID)
		}
		return ids
	}

	rollBackAnUpdate(0x21) // the first delivery's hooks wait in the queue
	rollBackAnUpdate(0x22)
	if got := owed(); len(got) != 2 {
		t.Fatalf("owed before any delivery ran = %v, want two events", got)
	}
	second := owed()[1]
	close(queued[0].gate)
	<-queued[0].ran
	if got := owed(); len(got) != 1 || got[0] != second {
		t.Errorf("owed after the first delivery = %v, want the second rollback's event %d still owed", got, second)
	}
	close(queued[1].gate)
	<-queued[1].ran
	if got := owed(); len(got) != 0 {
		t.Errorf("owed after both deliveries = %v, want none", got)
	}
}
