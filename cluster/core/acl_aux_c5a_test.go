// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"sync/atomic"
	"testing"

	"thingfabric.dev/go-fabric/cluster/core"
	"thingfabric.dev/go-fabric/im"
	mstore "thingfabric.dev/go-fabric/store"
)

// failOnceAuxSourceA1 is a deferAuxSource whose next AuxiliaryACL, once
// armed, fails.
type failOnceAuxSourceA1 struct {
	*deferAuxSource
	fail atomic.Bool
}

func (s *failOnceAuxSourceA1) AuxiliaryACL(ctx context.Context, f uint8) ([]mstore.ACLEntry, error) {
	if s.fail.CompareAndSwap(true, false) {
		return nil, errInjectedIO
	}
	return s.deferAuxSource.AuxiliaryACL(ctx, f)
}

// TestAuxiliarySyncOwedByAFailedReadRunsAtTheNextReadA1 (R4-err-4055): a
// change of fabric 1's auxiliary entries whose synchronisation fails to
// read the source is owed, not dropped: the next read of the cluster runs
// it — the DataVersion that read serves has moved — and its
// AuxiliaryAccessUpdated names the node of the change's own request, not
// the reader's.
func TestAuxiliarySyncOwedByAFailedReadRunsAtTheNextReadA1(t *testing.T) {
	t.Parallel()
	ac, err := core.NewAccessControl(newFakeStore())
	if err != nil {
		t.Fatal(err)
	}
	ev := &deferAuxEvents{got: map[uint8]*uint64{}}
	ac.SetMatterEventEmitter(ev)
	src := &failOnceAuxSourceA1{deferAuxSource: &deferAuxSource{}}
	ac.RegisterAuxiliaryProvider(context.Background(), src)
	const adminA, reader = 0xA1, 0xB2
	ctxA := im.WithSubject(im.WithFabricFilter(context.Background(), false, 1), adminA, nil)
	src.fail.Store(true)
	src.add(ctxA, 1, true) // the change's synchronisation fails
	ev.mu.Lock()
	_, early := ev.got[1]
	ev.mu.Unlock()
	if early {
		t.Fatal("the failed synchronisation fired an event")
	}
	v0 := ac.MatterDataVersion()

	readCtx := im.WithSubject(im.WithFabricFilter(context.Background(), true, 2), reader, nil)
	if _, ok := ac.MatterReadFiltered(readCtx, 0x0000); !ok {
		t.Fatal("the Acl read failed")
	}
	if v := ac.MatterDataVersion(); v == v0 {
		t.Error("the next read left the DataVersion where the failed synchronisation left it")
	}
	ev.mu.Lock()
	defer ev.mu.Unlock()
	a, fired := ev.got[1]
	switch {
	case !fired:
		t.Error("the owed synchronisation fired no AuxiliaryAccessUpdated for fabric 1")
	case a == nil || *a != adminA:
		t.Errorf("fabric 1's AuxiliaryAccessUpdated names %v, want the change's node %#x", a, adminA)
	}
}
