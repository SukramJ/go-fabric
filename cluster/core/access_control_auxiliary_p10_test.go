// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"testing"

	"thingfabric.dev/go-fabric/cluster/core"
	"thingfabric.dev/go-fabric/im"
)

// TestAccessControlFailedInitialSyncEmitsOnlyTheChangedFabric (review II,
// P10-err-6102): the initial synchronisation fails, fabric 2 holds an
// entry from before the registration, and a later change of fabric 1
// succeeds. That run applies the never-applied baseline: it fires
// AuxiliaryAccessUpdated for fabric 1, whose change names a node, and none
// for fabric 2, whose entries are as they were at registration — as a
// succeeding initial synchronisation would have it.
func TestAccessControlFailedInitialSyncEmitsOnlyTheChangedFabric(t *testing.T) {
	t.Parallel()
	ctxA := im.WithSubject(im.WithFabricFilter(context.Background(), false, 1), 0xA1, nil)
	for _, failInitial := range []bool{false, true} {
		ac, err := core.NewAccessControl(newFakeStore())
		if err != nil {
			t.Fatal(err)
		}
		ev := &deferAuxEvents{got: map[uint8]*uint64{}}
		ac.SetMatterEventEmitter(ev)
		src := &failOnceAuxSourceA1{deferAuxSource: &deferAuxSource{fn: func(context.Context, uint8) {}}}
		src.add(context.Background(), 2, false) // before the registration
		src.fail.Store(failInitial)
		ac.RegisterAuxiliaryProvider(context.Background(), src)
		src.add(ctxA, 1, true) // fabric 1 changes; fabric 2 does not
		ev.mu.Lock()
		admin, f1 := ev.got[1]
		_, f2 := ev.got[2]
		ev.mu.Unlock()
		if !f1 || admin == nil || *admin != 0xA1 {
			t.Errorf("failInitial=%v: fabric 1's event = %v (admin %v), want one naming 0xA1", failInitial, f1, admin)
		}
		if f2 {
			t.Errorf("failInitial=%v: fabric 2, unchanged since the registration, got an event", failInitial)
		}
	}
}
